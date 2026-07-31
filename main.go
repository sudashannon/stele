package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"comet-ui/chat"
	"comet-ui/internal/source"
	"comet-ui/internal/todo"
	"comet-ui/wiki"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

//go:embed web/dist
var webDist embed.FS

// staticHandler serves the embedded SPA. The embed FS reports a zero ModTime,
// so http.FileServer emits neither Last-Modified nor ETag: without explicit
// cache directives a browser is free to keep serving a previously loaded
// index.html, which pins an already-open tab to the old asset hashes and makes
// a freshly deployed binary look like it changed nothing. Vite fingerprints
// everything under /assets/, so those are safe to cache forever while the
// entry document must always be revalidated.
func staticHandler() http.Handler {
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

// coalescedRebuilder runs rebuilds one at a time. Requests arriving during a
// rebuild collapse into one additional pass, which observes the latest live
// workspace registry instead of allowing an older pass to publish last.
type coalescedRebuilder struct {
	mu        sync.Mutex
	run       func() error
	complete  func(error, bool)
	running   bool
	requested bool
	notify    bool
}

func newCoalescedRebuilder(run func() error, complete func(error, bool)) *coalescedRebuilder {
	return &coalescedRebuilder{run: run, complete: complete}
}

// Request schedules work and returns without waiting for the rebuild. start,
// when non-nil, runs after the request is recorded but before any completion
// event can be emitted, preserving the SSE started-before-completed order.
func (r *coalescedRebuilder) Request(start func()) {
	r.mu.Lock()
	r.requested = true
	if start != nil {
		r.notify = true
		start()
	}
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()
	go r.loop()
}

func (r *coalescedRebuilder) loop() {
	var err error
	for {
		r.mu.Lock()
		if !r.requested {
			r.running = false
			notify := r.notify
			r.notify = false
			if r.complete != nil {
				// Completion is emitted only after the coalesced latest-state
				// pass, never between an old pass and its queued successor.
				r.complete(err, notify)
			}
			r.mu.Unlock()
			return
		}
		r.requested = false
		r.mu.Unlock()
		err = r.run()
	}
}

// sessionPollInterval is how often transcripts are re-checked. Sessions are
// appended to many times per second while an agent runs, and a digest only
// matters once the work has settled, so this is deliberately slow.
const sessionPollInterval = 60 * time.Second

// pollSessions keeps the session layer current without adding transcript
// churn to the document watcher: each pass tail-parses only the transcripts
// whose size or mtime moved, re-grafts them onto the live graph, and notifies
// clients when something changed.
func pollSessions(index *wiki.SessionsIndex, api *wiki.API, hub *wiki.SSEHub, interval time.Duration) {
	if index == nil {
		return
	}
	for range time.Tick(interval) {
		changed, err := index.Refresh()
		if err != nil {
			log.Printf("wiki sessions refresh failed (non-fatal): %v", err)
			continue
		}
		if changed == 0 {
			continue
		}
		api.ApplySessions()
		if hub != nil {
			hub.BroadcastNamed("sessions-updated", fmt.Sprintf(`{"changed":%d}`, changed))
		}
	}
}

func main() {
	port := flag.Int("port", 8989, "port to listen on")
	bind := flag.String("bind", "localhost", "address to bind to (use 0.0.0.0 for LAN access)")
	shareURL := flag.String("share-url", "", "public base URL for share links (default: auto-detect or localhost)")
	baseDir := flag.String("dir", "openspec", "path to an OpenSpec, Trellis, or Superpowers workspace")
	sessionsDir := flag.String("sessions-dir", "", "agent session transcript directory (default: ~/.omp/agent/sessions; empty dir disables the session layer)")
	flag.Parse()

	mux := http.NewServeMux()

	reg, err := NewWorkspaceRegistry(filepath.Join(os.Getenv("HOME"), ".comet-panel", "workspaces.yaml"))
	if err != nil {
		log.Fatalf("workspace registry: %v", err)
	}

	// Share URL precedence: --share-url flag > auto-detected LAN IP > localhost.
	shareBaseURL := fmt.Sprintf("http://localhost:%d", *port)
	if *shareURL != "" {
		shareBaseURL = *shareURL
	} else if ip := detectLANIP(); ip != "" {
		shareBaseURL = fmt.Sprintf("http://%s:%d", ip, *port)
	}
	// Always prefer explicit --share-url; auto-detection is unreliable on WSL2.
	shareManager := NewShareManager(shareBaseURL)

	mux.HandleFunc("/api/sync", handleSync(reg))
	mux.HandleFunc("/api/sync/config", handleSyncConfig(reg))

	mux.HandleFunc("/api/bookmarks", handleBookmarks)

	// --- Todo Store (shared by REST and MCP) ---
	todoStore, err := todo.NewStore(todo.StorePath(), nil)
	if err != nil {
		log.Fatalf("todo store: %v", err)
	}

	// --- MCP write token ---
	mcpToken, err := todo.EnsureToken()
	if err != nil {
		log.Fatalf("mcp write token: %v", err)
	}

	wikiCacheDir := filepath.Join(os.Getenv("HOME"), ".comet-panel", "wiki")
	runtimeWorkspaces := workspacesForRuntime(reg.List(), *baseDir)
	wikiAPI := wiki.NewAPIWithWorkspacesAsync(toWikiWorkspaces(runtimeWorkspaces), wikiCacheDir)
	sseHub := wiki.NewSSEHub()
	wikiAPI.SSE = sseHub
	// SSE callback: broadcast todos-updated on every successful mutation.
	todoStore.SetOnChange(func(rev int64) {
		sseHub.BroadcastNamed("todos-updated", fmt.Sprintf(`{"revision":%d}`, rev))
	})
	// Rebuilds use the live registry and retain the --dir fallback until the
	// first explicit workspace is registered.
	// Wire Todo store and MCP token into the wiki API atomically.
	wikiAPI.SetTodoStore(todoStore, mcpToken)
	wikiAPI.SetLister(registryLister{reg: reg, defaultDir: *baseDir})
	// The initial index build scans the whole workspace tree and can take
	// tens of seconds on a large repo. Run it in the background so
	// ListenAndServe below binds immediately instead of leaving the
	// dashboard unreachable for the whole scan; HandleIndex/HandleLint
	mux.HandleFunc("/api/wiki/fix-dead-links", wikiAPI.HandleFixDeadLinks)
	// serve `[]` off the empty graph from NewAPIWithWorkspacesAsync until
	// this swaps in the built one.
	watcher := wiki.NewWatcher(wikiAPI, "scripts/embed.ts")
	mirrorDir := filepath.Join(os.Getenv("HOME"), ".comet-panel", "knowledge-repo")
	syncCfg := reg.Sync()
	if syncCfg.Enabled {
		mirror := wiki.NewMirror(mirrorDir, syncCfg.Remote)
		if err := mirror.Init(); err != nil {
			log.Printf("knowledge mirror init failed (non-fatal): %v", err)
		} else {
			watcher.SetMirror(mirror)
		}
	}
	// --- Agent session layer ---
	// Transcripts live outside every registered workspace and can reach
	// hundreds of megabytes, so they are never scanned as a workspace source:
	// a dedicated index parses them incrementally and grafts session nodes and
	// session→document edges onto the graph after each rebuild.
	agentDir := filepath.Join(os.Getenv("HOME"), ".omp", "agent")
	sessionsRoot := *sessionsDir
	if sessionsRoot == "" {
		sessionsRoot = filepath.Join(agentDir, "sessions")
	}
	sessionsIndex := wiki.NewSessionsIndex(sessionsRoot, filepath.Join(wikiCacheDir, "sessions.json"))
	wikiAPI.SetMemoryDir(filepath.Join(agentDir, "memories"))
	rebuilder := newCoalescedRebuilder(func() error {
		// Digests refresh before the rebuild so the replacement graph is
		// grafted with current session activity in one pass.
		if _, err := sessionsIndex.Refresh(); err != nil {
			log.Printf("wiki sessions refresh failed (non-fatal): %v", err)
		}
		if err := wikiAPI.Rebuild(); err != nil {
			return err
		}
		watcher.SyncMirror()
		return nil
	}, func(err error, notify bool) {
		if err != nil {
			log.Printf("wiki index rebuild failed (non-fatal, dashboard still serves): %v", err)
			return
		}
		if notify {
			sseHub.Broadcast(`{"changed":1}`)
		}
	})
	wikiAPI.SetSessionsIndex(sessionsIndex)
	rebuilder.Request(nil)
	// Transcripts are appended to continuously by live agents. Polling on a
	// long interval (instead of watching them with the document watcher) keeps
	// the 5s document debounce and the 30s community re-detection from
	// thrashing on every tool call, and tail-parsing keeps each pass cheap.
	go pollSessions(sessionsIndex, wikiAPI, sseHub, sessionPollInterval)
	var watchPaths []string
	for _, workspace := range runtimeWorkspaces {
		watchPaths = append(watchPaths, source.WatchRoots(workspace)...)
	}
	if err := watcher.Start(watchPaths); err != nil {
		log.Printf("wiki watcher start failed (non-fatal): %v", err)
	} else {
		defer watcher.Stop()
	}
	mux.HandleFunc("/api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		handleWorkspaces(w, r, reg, func() {
			var paths []string
			for _, workspace := range workspacesForRuntime(reg.List(), *baseDir) {
				paths = append(paths, source.WatchRoots(workspace)...)
			}
			watcher.ResetPaths(paths)
			rebuilder.Request(func() {
				sseHub.BroadcastNamed("indexing-started", `{"changed":1}`)
			})
		})
	})

	mux.HandleFunc("/api/wiki/index", wikiAPI.HandleIndex)
	mux.HandleFunc("/api/wiki/graph", wikiAPI.HandleGraph)
	mux.HandleFunc("/api/wiki/recent", wikiAPI.HandleRecent)
	mux.HandleFunc("/api/wiki/component/", wikiAPI.HandleComponent)
	mux.HandleFunc("/api/wiki/rebuild", wikiAPI.HandleRebuild)
	mux.HandleFunc("/api/wiki/lint", wikiAPI.HandleLint)
	mux.HandleFunc("/api/wiki/summarize", wikiAPI.HandleSummarize)
	mux.HandleFunc("/api/wiki/summary", wikiAPI.HandleCachedSummary)
	mux.HandleFunc("/api/wiki/overview", wikiAPI.HandleOverview)
	mux.HandleFunc("/api/wiki/search-semantic", wikiAPI.HandleSemanticSearch)
	mux.HandleFunc("/api/wiki/calendar/month", wikiAPI.HandleCalendarMonth)
	mux.HandleFunc("/api/wiki/calendar/day", wikiAPI.HandleCalendarDay)
	mux.HandleFunc("/api/wiki/sessions", wikiAPI.HandleSessions)
	mux.HandleFunc("/api/wiki/sessions/refresh", wikiAPI.HandleSessionsRefresh)
	mux.HandleFunc("/api/wiki/session", wikiAPI.HandleSession)
	mux.HandleFunc("/api/wiki/context", wikiAPI.HandleContext)
	mux.HandleFunc("/mcp", wikiAPI.HandleMCP)
	mux.Handle("/api/wiki/events", sseHub)

	// --- Todo REST ---
	todoHandler := newTodoAPI(todoStore, wikiAPI)
	mux.HandleFunc("/api/todos", todoHandler.ServeHTTP)
	mux.HandleFunc("/api/todos/", todoHandler.ServeHTTP)

	mux.HandleFunc("/api/changes", func(w http.ResponseWriter, r *http.Request) {
		handleListChangesMultiWorkspace(w, r, *baseDir, reg)
	})
	transitionLock := NewTransitionLock()
	mux.HandleFunc("/api/changes/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transition") {
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/changes/"), "/transition")
			handleTransition(w, r, name, *baseDir, transitionLock, reg)
			return
		}
		handleGetChange(w, r, *baseDir, reg) // existing GET behavior, unchanged
	})
	mux.HandleFunc("/api/artifact", func(w http.ResponseWriter, r *http.Request) {
		handleGetArtifact(w, r, *baseDir, reg)
	})

	mux.HandleFunc("/api/chat/message", chat.HandleMessage(*baseDir, *baseDir, wikiAPI))
	mux.HandleFunc("/api/chat/session", chat.HandleSession)
	mux.HandleFunc("/api/chat/config", chat.HandleConfig)
	mux.HandleFunc("/api/chat/providers", chat.HandleProviders)

	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) { handleReport(w, r, wikiAPI) })
	mux.HandleFunc("/api/reports", handleListReports)
	mux.HandleFunc("/api/reports/get", handleGetReport)
	mux.HandleFunc("/api/share/create", func(w http.ResponseWriter, r *http.Request) { handleCreateShare(w, r, shareManager) })
	mux.HandleFunc("/api/share/list", func(w http.ResponseWriter, r *http.Request) { handleListShares(w, r, shareManager) })
	mux.HandleFunc("/api/share/revoke", func(w http.ResponseWriter, r *http.Request) { handleRevokeShare(w, r, shareManager) })
	mux.HandleFunc("/share/", func(w http.ResponseWriter, r *http.Request) { handleSharePage(w, r, shareManager) })

	mux.Handle("/", staticHandler())

	addr := fmt.Sprintf("%s:%d", *bind, *port)
	url := fmt.Sprintf("http://%s:%d", *bind, *port)
	fmt.Printf("Comet UI Dashboard → %s\n", url)

	go openBrowser(url)

	log.Fatal(http.ListenAndServe(addr, mux))
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return
	}
	cmd.Start()
}

// WorkspaceConfig and wiki.WorkspaceConfig are aliases of the same shared
// source.Workspace contract, so crossing the package boundary needs no copy.
func toWikiWorkspaces(ws []WorkspaceConfig) []wiki.WorkspaceConfig {
	return ws
}

// workspacesForRuntime preserves --dir-only deployments for the wiki index
// and watcher. An explicit registry replaces this fallback as a clean cutover.
func workspacesForRuntime(configured []WorkspaceConfig, defaultDir string) []WorkspaceConfig {
	if len(configured) > 0 {
		return configured
	}
	absolute, err := filepath.Abs(defaultDir)
	if err != nil {
		return nil
	}
	fallback, err := normalizeWorkspaceConfig(WorkspaceConfig{Path: absolute})
	if err != nil {
		return nil
	}
	return []WorkspaceConfig{fallback}
}

// registryLister exposes runtime additions and the legacy --dir fallback.
type registryLister struct {
	reg        *WorkspaceRegistry
	defaultDir string
}

func (l registryLister) List() []wiki.WorkspaceConfig {
	return toWikiWorkspaces(workspacesForRuntime(l.reg.List(), l.defaultDir))
}

func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func getDir(r *http.Request, defaultDir string) string {
	d := r.URL.Query().Get("dir")
	if d == "" {
		return defaultDir
	}
	return d
}

// resolveWorkspaceConfig resolves the typed workspace for a request that may
// carry a `?workspace=<alias>` query param. Precedence: `?workspace=`
// (looked up against the live registry) wins over the legacy `?dir=` param,
// which in turn wins over defaultDir. An unregistered alias is a hard
// error — it must never silently fall back to defaultDir, since that would
// let a client unknowingly operate on the wrong (or a shared default)
// workspace.
func resolveWorkspaceConfig(r *http.Request, defaultDir string, reg *WorkspaceRegistry) (WorkspaceConfig, error) {
	alias := r.URL.Query().Get("workspace")
	if alias == "" {
		cfg := WorkspaceConfig{Path: getDir(r, defaultDir), Type: source.KindOpenSpec}
		if kind, err := source.ResolveKind(cfg.Path, ""); err == nil {
			cfg.Type = kind
		}
		return cfg, nil
	}
	if reg != nil {
		for _, ws := range reg.List() {
			if ws.Alias == alias {
				return ws, nil
			}
		}
	}
	return WorkspaceConfig{}, fmt.Errorf("unknown workspace %q", alias)
}

func handleWorkspaces(w http.ResponseWriter, r *http.Request, reg *WorkspaceRegistry, onChanged func()) {
	switch r.Method {
	case http.MethodGet:
		handleListWorkspaces(w, r, reg)
	case http.MethodPost:
		if added := handleAddWorkspace(w, r, reg); added != nil && onChanged != nil {
			onChanged()
		}
	case http.MethodDelete:
		if handleDeleteWorkspace(w, r, reg) && onChanged != nil {
			onChanged()
		}
	default:
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleDeleteWorkspace(w http.ResponseWriter, r *http.Request, reg *WorkspaceRegistry) bool {
	alias := r.URL.Query().Get("alias")
	if alias == "" {
		writeJSONError(w, "alias is required", http.StatusBadRequest)
		return false
	}
	if err := reg.Remove(alias); err != nil {
		if errors.Is(err, ErrWorkspaceNotFound) {
			writeJSONError(w, err.Error(), http.StatusNotFound)
			return false
		}
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}

func handleListWorkspaces(w http.ResponseWriter, r *http.Request, reg *WorkspaceRegistry) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reg.List())
}

func handleAddWorkspace(w http.ResponseWriter, r *http.Request, reg *WorkspaceRegistry) *WorkspaceConfig {
	var cfg WorkspaceConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSONError(w, "invalid body", 400)
		return nil
	}
	if cfg.Alias == "" || cfg.Path == "" {
		writeJSONError(w, "alias and path are required", 400)
		return nil
	}
	normalized, err := normalizeWorkspaceConfig(cfg)
	if err != nil {
		writeJSONError(w, err.Error(), 400)
		return nil
	}
	if err := reg.Add(normalized); err != nil {
		writeJSONError(w, err.Error(), 409)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(normalized)
	return &normalized
}

// handleListChangesMultiWorkspace replaces handleListChanges as the /api/changes
// entry point. If no workspaces are registered, it preserves the original
// single-directory behavior (scanAllChanges against the --dir flag value) so
// existing deployments keep working without a workspaces.yaml migration.
func handleListChangesMultiWorkspace(w http.ResponseWriter, r *http.Request, defaultDir string, reg *WorkspaceRegistry) {
	w.Header().Set("Content-Type", "application/json")

	registered := reg.List()
	if len(registered) == 0 {
		dir := getDir(r, defaultDir)
		workspace := WorkspaceConfig{Path: dir, Type: source.KindOpenSpec}
		if kind, detectErr := source.ResolveKind(dir, ""); detectErr == nil {
			workspace.Type = kind
		}
		adapter, resolved, sourceErr := changeSourceFor(workspace)
		if sourceErr != nil {
			writeJSONError(w, sourceErr.Error(), 500)
			return
		}
		changes, scanErr := adapter.List(resolved)
		if scanErr != nil {
			writeJSONError(w, scanErr.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"changes": changes, "dir": dir})
		return
	}
	filterAlias := r.URL.Query().Get("workspace")
	changes, failedWorkspaces := scanAllWorkspaces(registered)
	if filterAlias != "" {
		var filtered []ChangeSummary
		for _, c := range changes {
			if c.Workspace == filterAlias {
				filtered = append(filtered, c)
			}
		}
		changes = filtered
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"changes": changes, "failedWorkspaces": failedWorkspaces})
}

func handleGetChange(w http.ResponseWriter, r *http.Request, baseDir string, reg *WorkspaceRegistry) {
	workspace, err := resolveWorkspaceConfig(r, baseDir, reg)
	if err != nil {
		writeJSONError(w, err.Error(), 400)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/changes/")
	name = filepath.Clean(name)
	if name == "" || name == "." {
		writeJSONError(w, "missing name", 400)
		return
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		writeJSONError(w, "invalid name", 400)
		return
	}

	adapter, resolved, sourceErr := changeSourceFor(workspace)
	if sourceErr != nil {
		writeJSONError(w, sourceErr.Error(), 400)
		return
	}
	detail, scanErr := adapter.Detail(resolved, name)
	if scanErr != nil {
		writeJSONError(w, scanErr.Error(), 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail)
}

func handleTransition(w http.ResponseWriter, r *http.Request, changeName, defaultDir string, lock *TransitionLock, reg *WorkspaceRegistry) {
	var body struct {
		TargetPhase string `json:"targetPhase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TargetPhase == "" {
		writeJSONError(w, "invalid body: targetPhase required", 400)
		return
	}

	// Validate changeName — mirror handleGetChange's own validation
	if changeName == "" || changeName == "." || strings.Contains(changeName, "..") || strings.Contains(changeName, "/") {
		writeJSONError(w, "invalid change name", 400)
		return
	}
	workspace, err := resolveWorkspaceConfig(r, defaultDir, reg)
	if err != nil {
		writeJSONError(w, err.Error(), 400)
		return
	}
	runner, resolved, err := transitionSourceFor(workspace)
	if err != nil {
		writeJSONError(w, err.Error(), 400)
		return
	}
	if err := runner.ValidateTarget(body.TargetPhase); err != nil {
		writeJSONError(w, err.Error(), 400)
		return
	}

	lockKey := changeName
	if resolved.Alias != "" {
		lockKey = resolved.Alias + ":" + changeName
	}
	if !lock.TryAcquire(lockKey) {
		writeJSONError(w, fmt.Sprintf("a transition for %q is already in progress", changeName), 409)
		return
	}
	defer lock.Release(lockKey)
	if err := runner.Preflight(resolved, changeName, body.TargetPhase); err != nil {
		writeJSONError(w, err.Error(), 400)
		return
	}

	output, err := runner.Trigger(r.Context(), resolved, changeName, body.TargetPhase)
	if err != nil {
		writeJSONError(w, err.Error(), 500)
		return
	}
	defer output.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, "streaming not supported", 500)
		return
	}

	buf := make([]byte, 4096)
	for {
		n, readErr := output.Read(buf)
		if n > 0 {
			fmt.Fprintf(w, "data: %s\n\n", string(buf[:n]))
			flusher.Flush()
		}
		if readErr != nil {
			// A clean io.EOF means the guard process exited 0 (success).
			// Any other error (from cmd.Run() via pw.CloseWithError in
			// TriggerTransition) means it exited non-zero or failed to
			// start. Emit an explicit final marker — the raw output
			// stream alone gives the client no way to tell these apart.
			if readErr == io.EOF {
				fmt.Fprintf(w, "data: __GUARD_EXIT__:0\n\n")
			} else {
				fmt.Fprintf(w, "data: __GUARD_EXIT__:1:%s\n\n", readErr.Error())
			}
			flusher.Flush()
			break
		}
	}
}

func handleGetArtifact(w http.ResponseWriter, r *http.Request, baseDir string, reg *WorkspaceRegistry) {
	workspace, err := resolveWorkspaceConfig(r, baseDir, reg)
	if err != nil {
		writeJSONError(w, err.Error(), 400)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSONError(w, "missing path", 400)
		return
	}

	absPath, absErr := filepath.Abs(path)
	if absErr != nil {
		writeJSONError(w, "invalid path", 400)
		return
	}
	// Derive authorization from the resolved source. OpenSpec and Trellis
	// preserve their project-root boundary; standalone Superpowers may serve
	// only its four durable docs/superpowers roots, including after symlink
	// resolution.
	if !artifactPathAllowed(workspace, absPath) {
		writeJSONError(w, "path outside allowed artifact roots", 403)
		return
	}

	content, readErr := os.ReadFile(absPath)
	if readErr != nil {
		writeJSONError(w, "file not found", 404)
		return
	}

	ext := filepath.Ext(absPath)
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Write(content)
}

func artifactPathAllowed(workspace WorkspaceConfig, path string) bool {
	kind, err := source.ResolveKind(workspace.Path, workspace.Type)
	if err != nil {
		return false
	}
	if kind != source.KindSuperpowers {
		root, err := filepath.Abs(source.ProjectRoot(workspace))
		return err == nil && pathWithinRoot(path, root)
	}

	roots := source.SuperpowersRoots(workspace.Path)
	lexicallyAllowed := false
	for _, root := range roots {
		absoluteRoot, err := filepath.Abs(root)
		if err == nil && pathWithinRoot(path, absoluteRoot) {
			lexicallyAllowed = true
			break
		}
	}
	if !lexicallyAllowed {
		return false
	}

	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return os.IsNotExist(err)
	}
	for _, root := range roots {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err == nil && pathWithinRoot(resolvedPath, resolvedRoot) {
			return true
		}
	}
	return false
}

func pathWithinRoot(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// handleCreateShare generates a share token for a document and returns the link.
func handleCreateShare(w http.ResponseWriter, r *http.Request, mgr *ShareManager) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", 405)
		return
	}
	var req struct {
		Path      string `json:"path"`
		Workspace string `json:"workspace"`
		TTL       int    `json:"ttl"` // seconds; 0 = no expiry
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid body", 400)
		return
	}
	if req.Path == "" {
		writeJSONError(w, "missing path", 400)
		return
	}
	var ttl time.Duration
	if req.TTL > 0 {
		ttl = time.Duration(req.TTL) * time.Second
	}
	_, url, err := mgr.CreateShare(req.Path, req.Workspace, ttl)
	if err != nil {
		writeJSONError(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": url})
}

// handleListShares returns all active share tokens.
func handleListShares(w http.ResponseWriter, r *http.Request, mgr *ShareManager) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", 405)
		return
	}
	shares := mgr.ListShares()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shares)
}

// handleRevokeShare removes a share token.
func handleRevokeShare(w http.ResponseWriter, r *http.Request, mgr *ShareManager) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", 405)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		writeJSONError(w, "missing token", 400)
		return
	}
	if err := mgr.RevokeShare(token); err != nil {
		writeJSONError(w, err.Error(), 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

// handleSharePage serves a minimal read-only HTML page for a shared document.
func handleSharePage(w http.ResponseWriter, r *http.Request, mgr *ShareManager) {
	token := strings.TrimPrefix(r.URL.Path, "/share/")
	if token == "" {
		writeJSONError(w, "missing token", 400)
		return
	}
	entry, err := mgr.ValidateShare(token)
	if err != nil {
		w.WriteHeader(404)
		w.Write([]byte(`<html><body><h1>链接已失效</h1><p>该分享链接不存在或已过期。</p>
<script src="//cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
<script>document.querySelectorAll("code.language-mermaid").forEach(c=>{c.parentElement.className="mermaid";c.parentElement.innerHTML=c.textContent});mermaid.initialize({startOnLoad:true,theme:"neutral"})</script>
</body></html>`))
		return
	}
	content, readErr := os.ReadFile(entry.Path)
	if readErr != nil {
		w.WriteHeader(404)
		w.Write([]byte(`<html><body><h1>文档不可用</h1><p>原文档已被移动或删除。</p>
<script src="//cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
<script>document.querySelectorAll("code.language-mermaid").forEach(c=>{c.parentElement.className="mermaid";c.parentElement.innerHTML=c.textContent});mermaid.initialize({startOnLoad:true,theme:"neutral"})</script>
</body></html>`))
		return
	}
	html := renderMarkdownToHTML(entry.Path, string(content))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// renderMarkdownToHTML converts markdown source to a styled HTML page using goldmark.
func renderMarkdownToHTML(path, src string) string {
	var buf bytes.Buffer
	title := filepath.Base(path)
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := md.Convert([]byte(src), &buf); err != nil {
		log.Printf("share render: goldmark error for %s: %v", path, err)
		return fmt.Sprintf(`<html><body><pre>%s</pre>
<script src="//cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
<script>document.querySelectorAll("code.language-mermaid").forEach(c=>{c.parentElement.className="mermaid";c.parentElement.innerHTML=c.textContent});mermaid.initialize({startOnLoad:true,theme:"neutral"})</script>
</body></html>`, html.EscapeString(src))
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>

<style>
  :root{--paper:#fafaf8;--ink:#1d1d1f;--grey-3:#6e6e73;--accent:#002FA7;--border:#e8e8ed}
  *{box-sizing:border-box;margin:0;padding:0}
  body{background:var(--paper);color:var(--ink);font-family:"Inter","PingFang SC",system-ui,sans-serif;padding:48px;max-width:860px;margin:0 auto;line-height:1.75}
  h1{font-size:28px;font-weight:800;margin-bottom:24px;padding-bottom:12px;border-bottom:2px solid var(--ink)}
  h2{font-size:20px;font-weight:700;margin:32px 0 12px}
  h3{font-size:16px;font-weight:600;margin:24px 0 8px}
  p,li{font-size:15px;margin-bottom:8px}
  ul,ol{padding-left:24px;margin-bottom:12px}
  blockquote{border-left:3px solid var(--border);padding-left:14px;color:var(--grey-3);margin:12px 0}
  code{background:#f0f0ee;padding:1px 4px;border-radius:3px;font-size:13px}
  pre{background:#f0f0ee;padding:14px;border-radius:6px;overflow-x:auto;font-size:13px;margin:12px 0}
  pre code{background:none;padding:0}
  table{border-collapse:collapse;width:100%%;margin:12px 0}
  th,td{border:1px solid var(--border);padding:8px 12px;text-align:left}
  th{background:#f5f5f7;font-weight:600}
  img{max-width:100%%;border-radius:6px}
  a{color:var(--accent)}
  hr{border:none;border-top:1px solid var(--border);margin:24px 0}
  footer{margin-top:48px;padding-top:12px;border-top:1px solid var(--border);font-size:12px;color:var(--grey-3)}
</style>
</head>
<body>
<div class="content">%s</div>
<footer>通过 Comet-Panel 分享 · 原文路径: %s</footer>

<script src="//cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
<script>document.querySelectorAll("code.language-mermaid").forEach(c=>{c.parentElement.className="mermaid";c.parentElement.innerHTML=c.textContent});mermaid.initialize({startOnLoad:true,theme:"neutral"})</script>
</body>
</html>`, html.EscapeString(title), buf.String(), html.EscapeString(path))
}
