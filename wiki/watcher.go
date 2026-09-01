package wiki

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const communityDirtyThreshold = 5

// Watcher watches a set of workspace directories for changes to markdown
// and .comet.yaml files and triggers incremental wiki index updates.
//
// File-change events are debounced by `debounce` before triggering a
// rebuild, and community detection (more expensive: Louvain over the whole
// graph) is further debounced by `communityDelay` after any structural
// change so a burst of edits during a save-heavy edit session only pays
// for community re-detection once, after things settle.
type Watcher struct {
	api            *API
	scriptPath     string
	debounce       time.Duration
	communityDelay time.Duration
	watcher        *fsnotify.Watcher
	stop           chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
	pathsMu        sync.Mutex
	roots          map[string]struct{}
	closed         bool
	mirror         *Mirror
}

// NewWatcher constructs a Watcher bound to api. IncrementalUpdate resolves
// the embedding script through the same findEmbedScript path as BuildIndex;
// scriptPath remains available for callers that override watcher wiring.
func NewWatcher(api *API, scriptPath string) *Watcher {
	return &Watcher{
		api:            api,
		scriptPath:     scriptPath,
		debounce:       5 * time.Second,
		communityDelay: 30 * time.Second,
		stop:           make(chan struct{}),
	}
}

// Start begins watching the given directory paths (recursively) for .md and
// .comet.yaml changes. It spawns a background goroutine and returns
// immediately; it never blocks the caller (e.g. the HTTP server).
func (w *Watcher) Start(paths []string) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.pathsMu.Lock()
	if w.closed || w.watcher != nil {
		w.pathsMu.Unlock()
		_ = fw.Close()
		return fmt.Errorf("wiki watcher cannot be started")
	}
	w.watcher = fw
	w.roots = cleanWatchRoots(paths)
	for root := range w.roots {
		w.addWatchTreeLocked(root)
	}

	log.Printf("wiki watcher: watching %d paths", len(paths))
	w.wg.Add(1)
	go w.loop(fw)
	w.pathsMu.Unlock()
	return nil
}
func cleanWatchRoots(paths []string) map[string]struct{} {
	roots := make(map[string]struct{}, len(paths))
	for _, root := range paths {
		if root == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		roots[filepath.Clean(absolute)] = struct{}{}
	}
	return roots
}

// addWatchTree installs watches for a newly created directory that is still
// beneath an existing root. Events outside the current roots (including stale
// events queued before ResetPaths) are intentionally ignored.
func (w *Watcher) addWatchTree(root string) {
	w.pathsMu.Lock()
	defer w.pathsMu.Unlock()
	if w.watcher == nil || !w.isWithinWatchRootsLocked(root) {
		return
	}
	w.addWatchTreeLocked(root)
}

func (w *Watcher) addWatchTreeLocked(root string) {
	w.walkWatchTree(root, func(path string) {
		if addErr := w.watcher.Add(path); addErr != nil {
			log.Printf("wiki watcher: failed to watch %s: %v", path, addErr)
		}
	})
}

func (w *Watcher) walkWatchTree(root string, visit func(string)) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		name := info.Name()
		switch {
		case name == ".git" || name == "node_modules" || name == "rootfs":
			return filepath.SkipDir
		case strings.HasPrefix(name, ".") && name != "." && name != ".." &&
			!(path == root && name == ".trellis"):
			return filepath.SkipDir
		case name == "orin_bsp" || name == "qcom_bsp":
			return filepath.SkipDir
		case name == "argos-sdk" || name == "x5_sdk":
			return filepath.SkipDir
		case name == "mondo-ai":
			return filepath.SkipDir
		case (strings.Contains(name, "_sdk") || strings.Contains(name, "_bsp")) && filepath.Dir(path) != root:
			return filepath.SkipDir
		}
		visit(filepath.Clean(path))
		return nil
	})
}

func (w *Watcher) isWithinWatchRootsLocked(path string) bool {
	path = filepath.Clean(path)
	for root := range w.roots {
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ResetPaths replaces the watched roots as one synchronized update. Desired
// trees are installed before stale watches are removed, so overlapping roots
// retained by another workspace never lose their watches.
func (w *Watcher) ResetPaths(paths []string) {
	w.pathsMu.Lock()
	defer w.pathsMu.Unlock()
	if w.closed || w.watcher == nil {
		return
	}

	w.roots = cleanWatchRoots(paths)
	desired := make(map[string]struct{})
	for root := range w.roots {
		w.walkWatchTree(root, func(path string) {
			desired[path] = struct{}{}
		})
	}

	current := make(map[string]struct{})
	for _, path := range w.watcher.WatchList() {
		current[filepath.Clean(path)] = struct{}{}
	}
	for path := range desired {
		if _, exists := current[path]; exists {
			continue
		}
		if err := w.watcher.Add(path); err != nil {
			log.Printf("wiki watcher: failed to watch %s: %v", path, err)
		}
	}
	for path := range current {
		if _, keep := desired[path]; keep {
			continue
		}
		if err := w.watcher.Remove(path); err != nil {
			log.Printf("wiki watcher: failed to remove watch %s: %v", path, err)
		}
	}
}

// Stop shuts down the watcher and blocks until its goroutine has exited.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		w.pathsMu.Lock()
		w.closed = true
		fw := w.watcher
		w.watcher = nil
		w.pathsMu.Unlock()

		close(w.stop)
		if fw != nil {
			_ = fw.Close()
		}
	})
	w.wg.Wait()
}

func (w *Watcher) loop(fw *fsnotify.Watcher) {
	defer w.wg.Done()

	var pending []string
	var pendingNotified bool
	var timer *time.Timer
	var communityTimer *time.Timer

	resetTimer := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(w.debounce, func() {
			files := pending
			pending = nil
			pendingNotified = false
			if len(files) == 0 {
				return
			}
			w.processBatch(files)
			if w.api.DirtyCount() > communityDirtyThreshold {
				if communityTimer != nil {
					communityTimer.Stop()
				}
				communityTimer = time.AfterFunc(w.communityDelay, func() {
					w.redetectCommunities()
				})
			}
		})
	}

	for {
		select {
		case <-w.stop:
			return
		case event, ok := <-fw.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					w.addWatchTree(event.Name)
					if !pendingNotified && w.api.SSE != nil {
						w.api.SSE.BroadcastNamed("indexing-started", `{"changed":1}`)
						pendingNotified = true
					}
					pending = append(pending, event.Name)
					resetTimer()
					continue
				}
			}
			if !isWikiFile(event.Name) {
				continue
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if !pendingNotified && w.api.SSE != nil {
				w.api.SSE.BroadcastNamed("indexing-started", `{"changed":1}`)
				pendingNotified = true
			}
			pending = append(pending, event.Name)
			resetTimer()
		case err, ok := <-fw.Errors:
			if !ok {
				return
			}
			log.Printf("wiki watcher error: %v", err)
		}
	}
}

// isWikiFile reports whether path is a source document or durable workflow
// metadata file that can change the graph.
func isWikiFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".md") ||
		base == ".comet.yaml" ||
		base == "task.json" ||
		base == "implement.jsonl" ||
		base == "check.jsonl" ||
		base == "debug.jsonl"
}

// processBatch handles a debounced batch with a per-file graph update. A full
// rebuild remains the correctness fallback if classification, embedding, link
// extraction, or cache persistence fails.
func (w *Watcher) processBatch(files []string) {
	log.Printf("wiki watcher: %d file(s) changed, updating index", len(files))
	if requiresFullRebuild(files) {
		if err := w.api.Rebuild(); err != nil {
			log.Printf("wiki watcher: Trellis rebuild failed: %v", err)
			return
		}
	} else if err := w.api.IncrementalUpdate(files); err != nil {
		log.Printf("wiki watcher: incremental update failed, falling back to full rebuild: %v", err)
		if rebuildErr := w.api.Rebuild(); rebuildErr != nil {
			log.Printf("wiki watcher: fallback rebuild failed: %v", rebuildErr)
			return
		}
	}
	if n := w.api.CheckClaimsForFiles(files); n > 0 {
		log.Printf("wiki watcher: %d claim(s) went stale after file change", n)
		if w.api.SSE != nil {
			w.api.SSE.BroadcastNamed("claims-updated", fmt.Sprintf(`{"stale":%d}`, n))
		}
	}
	if w.api.SSE != nil {
		w.api.SSE.Broadcast(fmt.Sprintf(`{"changed":%d}`, len(files)))
	}
	w.SyncMirror()
}

func requiresFullRebuild(files []string) bool {
	separator := string(filepath.Separator)
	trellisMarker := separator + ".trellis" + separator
	superpowersMarker := separator + filepath.Join("docs", "superpowers") + separator
	for _, path := range files {
		clean := filepath.Clean(path)
		if strings.Contains(clean, trellisMarker) || strings.Contains(clean, superpowersMarker) {
			return true
		}
	}
	return false
}

// SetMirror wires a knowledge-mirror sync target into the watcher. When
// set, every successful rebuild (whether triggered by a debounced file
// change or the initial startup scan) mirrors the complete current set of
// indexed components into the mirror's git repo.
func (w *Watcher) SetMirror(m *Mirror) {
	w.mirror = m
}

// SyncMirror mirrors the API's current graph contents into the configured
// Mirror (see SetMirror). It is safe to call even when no mirror is
// configured (no-op) or before any rebuild has run; callers include
// processBatch after every debounced rebuild and main.go after the initial
// startup rebuild.
func (w *Watcher) SyncMirror() {
	if w.mirror == nil {
		return
	}
	w.api.mu.RLock()
	components := w.api.graph.Components()
	ws := w.api.ws
	lister := w.api.lister
	w.api.mu.RUnlock()
	if lister != nil {
		ws = lister.List()
	}
	w.mirror.SyncAll(components, ws)
}

// redetectCommunities re-runs community detection (and re-derives
// community labels) on the current graph. It is called on a longer
// debounce than processBatch since Louvain community detection is more
// expensive than a single rescan and doesn't need to run on every edit.
func (w *Watcher) redetectCommunities() {
	if w.api.DirtyCount() <= communityDirtyThreshold {
		return
	}
	w.api.mu.Lock()
	defer w.api.mu.Unlock()
	g := w.api.graph
	g.SetCommunities(DetectCommunities(g))
	components := g.Components()
	comps := make([]Component, 0, len(components))
	for _, c := range components {
		comps = append(comps, c)
	}
	g.SetCommunityLabels(CommunityLabels(comps, g.Communities()))
	w.api.ResetDirty()
	log.Printf("wiki watcher: communities re-detected")
}
