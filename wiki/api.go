package wiki

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"comet-ui/chat"
	"comet-ui/internal/todo"
)

type API struct {
	mu              sync.RWMutex
	graph           *Graph
	ws              []WorkspaceConfig
	indexCacheDir   string
	lister          WorkspaceLister
	ready           bool
	dirtyStructural int32
	SSE             *SSEHub
	todoStore       *todo.Store // set via SetTodoStore; nil until wired
	todoToken       []byte      // MCP write token; not logged
}

// WorkspaceLister exposes the CURRENT workspace registry, decoupling
// HandleRebuild from the []WorkspaceConfig slice captured once at
// construction time. Implementations (e.g. main.go's workspace registry)
// return the live set of configured workspaces on every call.
type WorkspaceLister interface {
	List() []WorkspaceConfig
}

func NewAPI(g *Graph) *API {
	return &API{graph: g, ready: true}
}

func NewAPIWithWorkspaces(ws []WorkspaceConfig, indexCacheDir string) (*API, error) {
	g, err := BuildIndex(ws, indexCacheDir)
	if err != nil {
		return nil, err
	}
	return &API{graph: g, ws: ws, indexCacheDir: indexCacheDir, ready: true}, nil
}

// NewAPIWithWorkspacesAsync constructs an API immediately with an empty,
// non-nil graph — it never blocks on scanning the workspace tree. Callers
// that need the initial index populated must call Rebuild themselves,
// typically in a background goroutine, so the HTTP server can bind and
// start serving (HandleIndex/HandleLint return `[]` until the build
// completes) instead of waiting tens of seconds for a large tree to scan.
func NewAPIWithWorkspacesAsync(ws []WorkspaceConfig, indexCacheDir string) *API {
	return &API{graph: BuildGraph(nil, nil), ws: ws, indexCacheDir: indexCacheDir}
}

// SetLister wires a live WorkspaceLister so HandleRebuild rebuilds from the
// current workspace registry instead of the construction-time snapshot in
// a.ws. Passing nil restores the a.ws fallback behavior.
func (a *API) SetLister(lister WorkspaceLister) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lister = lister
}

// SetTodoStore atomically sets the shared Todo store and MCP write token.
func (a *API) SetTodoStore(store *todo.Store, token []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.todoStore = store
	a.todoToken = token
}

// TodoStoreSnapshot returns the current store and token under RLock,
// ensuring callers never hold a.mu across Store operations or title
// resolution.
func (a *API) TodoStoreSnapshot() (*todo.Store, []byte) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.todoStore, a.todoToken
}

// DirtyCount returns the number of structural graph changes accumulated
// since the last community detection pass.
func (a *API) DirtyCount() int {
	return int(atomic.LoadInt32(&a.dirtyStructural))
}

// ResetDirty marks the current graph structure as reflected in communities.
func (a *API) ResetDirty() {
	atomic.StoreInt32(&a.dirtyStructural, 0)
}

// AddDirty records structural graph changes for deferred community detection.
func (a *API) AddDirty(n int) {
	atomic.AddInt32(&a.dirtyStructural, int32(n))
}

type componentResponse struct {
	Component Component `json:"component"`
	Forward   []Edge    `json:"forward"`
	Backlinks []Edge    `json:"backlinks"`
}

func (a *API) HandleComponent(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	id := r.URL.Query().Get("id")
	c, ok := a.graph.Component(id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "component not found"})
		return
	}
	// Forward/Backlinks normalize a nil edge slice to an empty one before
	// encoding, same reason as HandleLint above: a component with zero
	// backlinks is common (e.g. a change's own TypeChange node — nothing
	// currently links TO a .comet.yaml) and (*Graph).Backlinks/Forward
	// return the unmodified nil slice on a map miss. encoding/json would
	// serialize that nil as `null`, and BacklinksPanel.tsx's
	// useState<WikiEdge[] | null>(null) treats a `null` backlinks value as
	// "not yet fetched" — so a real, legitimate zero-backlinks component
	// would render nothing forever instead of "暂无反向引用".
	forward := a.graph.Forward(id)
	if forward == nil {
		forward = []Edge{}
	}
	backlinks := a.graph.Backlinks(id)
	if backlinks == nil {
		backlinks = []Edge{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(componentResponse{
		Component: c,
		Forward:   forward,
		Backlinks: backlinks,
	})
}

func (a *API) HandleIndex(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	all := make([]Component, 0)
	for id := range a.graph.components {
		c, _ := a.graph.Component(id)
		all = append(all, c)
	}
	json.NewEncoder(w).Encode(all)
}

// recentItem is the wire shape for HandleRecent — a lightweight projection
// of Component tailored to a "recent changes" list (no Frontmatter payload).
type recentItem struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Type      ComponentType `json:"type"`
	Workspace string        `json:"workspace"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Path      string        `json:"path"`
}

// HandleRecent returns the 50 most recently updated components, newest
// first, for the sidebar's "Recent Changes" view.
// Accepts optional ?offset= and ?limit= query params for pagination.
func (a *API) HandleRecent(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	all := make([]Component, 0, len(a.graph.components))
	for id := range a.graph.components {
		c, _ := a.graph.Component(id)
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(all) {
		all = nil
	} else {
		end := offset + limit
		if end > len(all) {
			end = len(all)
		}
		all = all[offset:end]
	}
	items := make([]recentItem, len(all))
	for i, c := range all {
		items[i] = recentItem{
			ID:        c.ID,
			Title:     c.Title,
			Type:      c.Type,
			Workspace: c.Workspace,
			UpdatedAt: c.UpdatedAt,
			Path:      c.Path,
		}
	}
	json.NewEncoder(w).Encode(items)
}

// HandleCalendarMonth returns a map of days that have artifacts for a given month.
func (a *API) HandleCalendarMonth(w http.ResponseWriter, r *http.Request) {
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	if year == 0 || month < 1 || month > 12 {
		today := time.Now()
		year, month = today.Year(), int(today.Month())
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	days := make(map[string]int)
	for id := range a.graph.components {
		c, _ := a.graph.Component(id)
		y, m, d := c.UpdatedAt.Date()
		if y == year && m == time.Month(month) {
			key := fmt.Sprintf("%04d-%02d-%02d", y, m, d)
			days[key]++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"year":  year,
		"month": month,
		"days":  days,
	})
}

// HandleCalendarDay returns artifacts for a specific day, grouped by type.
func (a *API) HandleCalendarDay(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		http.Error(w, "missing date", 400)
		return
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		http.Error(w, "invalid date format, use YYYY-MM-DD", 400)
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	type item struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Type      string    `json:"type"`
		Workspace string    `json:"workspace"`
		Path      string    `json:"path"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	var items []item
	for id := range a.graph.components {
		c, _ := a.graph.Component(id)
		y, m, d := c.UpdatedAt.Date()
		if y == t.Year() && m == t.Month() && d == t.Day() {
			items = append(items, item{
				ID: id, Title: c.Title, Type: string(c.Type),
				Workspace: c.Workspace, Path: c.Path, UpdatedAt: c.UpdatedAt,
			})
		}
	}
	// Sort by type priority (same order as search)
	typeOrder := map[string]int{
		"knowledge": 0, "report": 1, "design": 2, "spec": 3,
		"plan": 4, "proposal": 5, "tasks": 6, "change": 7,
		"artifact": 8, "diagram": 9,
	}
	sort.Slice(items, func(i, j int) bool {
		oi := typeOrder[items[i].Type]
		oj := typeOrder[items[j].Type]
		if oi != oj {
			return oi < oj
		}
		return items[i].Title < items[j].Title
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// graphResponse mirrors index.json+graph.json's on-disk shape (see
// persistIndexCache in index.go) but served live over HTTP so the frontend
// graph view can render actual relationship edges instead of only nodes.
type graphResponse struct {
	Components      []Component    `json:"components"`
	Edges           []Edge         `json:"edges"`
	Communities     map[string]int `json:"communities"`
	CommunityLabels map[int]string `json:"communityLabels"`
}

// HandleGraph returns every component alongside every edge in the graph.
// Edges are enumerated by flattening a.graph.forward's values: BuildGraph
// (graph.go) appends each edge to forward[e.From] exactly once, so summing
// those slices yields every edge in the graph with no duplication — the
// same enumeration persistIndexCache's allEdges slice captures at build
// time, just read back from the live *Graph instead of threaded through
// as a second return value.
func (a *API) HandleGraph(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	components := make([]Component, 0, len(a.graph.components))
	for id := range a.graph.components {
		c, _ := a.graph.Component(id)
		components = append(components, c)
	}
	edges := make([]Edge, 0)
	for _, es := range a.graph.forward {
		edges = append(edges, es...)
	}
	json.NewEncoder(w).Encode(graphResponse{Components: components, Edges: edges, Communities: a.graph.Communities(), CommunityLabels: a.graph.CommunityLabels()})
}

func (a *API) HandleSearch(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	q := strings.ToLower(r.URL.Query().Get("q"))
	w.Header().Set("Content-Type", "application/json")
	var matches []Component
	for id := range a.graph.components {
		c, _ := a.graph.Component(id)
		if strings.Contains(strings.ToLower(c.Title), q) {
			matches = append(matches, c)
		}
	}
	json.NewEncoder(w).Encode(matches)
}

// semanticSearchRequest is the POST /api/wiki/search-semantic request body:
// a free-text query plus how many ranked results to return.
type semanticSearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"topK"`
}

// semanticSearchResult is one ranked hit: enough metadata for the frontend
// to render a result row plus the cosine similarity score it was ranked by.
type semanticSearchResult struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Workspace  string  `json:"workspace"`
	Type       string  `json:"type"`
	Similarity float64 `json:"similarity"`
}

type semanticScoredResult struct {
	id          string
	score       float64
	lexicalRank int
	typeOrder   int
}

// HandleSemanticSearch combines filename/title matching with semantic
// similarity. Lexical matches remain searchable even when a component has no
// embedding or its cosine similarity falls below the semantic noise floor.
func (a *API) HandleSemanticSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req semanticSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		json.NewEncoder(w).Encode([]semanticSearchResult{})
		return
	}

	// Embed the query using the same script the offline corpus build uses.
	// If embedding fails, exact lexical matches can still be returned.
	scriptPath := findEmbedScript()
	queryComps := []Component{{ID: "__query__", Title: query, Path: ""}}
	embedResult, err := ComputeEmbeddings(queryComps, scriptPath)
	var queryVec []float32
	embedFailure := ""
	if err != nil {
		embedFailure = "embedding failed: " + err.Error()
	} else {
		var ok bool
		queryVec, ok = embedResult["__query__"]
		if !ok || len(queryVec) == 0 {
			embedFailure = "embedding produced no vector"
		}
	}

	a.mu.RLock()
	embeddings := a.graph.Embeddings()
	components := a.graph.Components()
	a.mu.RUnlock()

	results := rankSemanticSearch(query, queryVec, components, embeddings)
	if embedFailure != "" && len(results) == 0 {
		http.Error(w, embedFailure, 500)
		return
	}

	limit := len(results)
	if req.TopK > 0 && req.TopK < limit {
		limit = req.TopK
	}

	w.Header().Set("Content-Type", "application/json")
	out := make([]semanticSearchResult, 0, limit)
	for _, result := range results[:limit] {
		component := components[result.id]
		out = append(out, semanticSearchResult{
			ID:         result.id,
			Title:      component.Title,
			Workspace:  component.Workspace,
			Type:       string(component.Type),
			Similarity: result.score,
		})
	}
	json.NewEncoder(w).Encode(out)
}

const (
	semanticExactFilenameMatch = iota
	semanticExactTitleMatch
	semanticFilenamePrefixMatch
	semanticTitlePrefixMatch
	semanticFilenameSubstringMatch
	semanticTitleSubstringMatch
	semanticNoLexicalMatch
)

const semanticSimilarityFloor = 0.12

func rankSemanticSearch(query string, queryVec []float32, components map[string]Component, embeddings map[string][]float32) []semanticScoredResult {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	queryNorm := vecNorm(queryVec)
	results := make([]semanticScoredResult, 0, len(components))

	for id, component := range components {
		lexicalRank, lexicalBoost := semanticLexicalMatch(component, queryLower)
		semanticScore := 0.0
		if vector, ok := embeddings[id]; ok && len(queryVec) > 0 && len(vector) == len(queryVec) {
			semanticScore = cosineSim(queryVec, vector, queryNorm, vecNorm(vector))
		}
		if lexicalRank == semanticNoLexicalMatch && semanticScore < semanticSimilarityFloor {
			continue
		}

		score := math.Max(0, semanticScore) + lexicalBoost
		score = math.Min(1, score)
		results = append(results, semanticScoredResult{
			id:          id,
			score:       score,
			lexicalRank: lexicalRank,
			typeOrder:   semanticTypeOrder(component.Type),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].lexicalRank != results[j].lexicalRank {
			return results[i].lexicalRank < results[j].lexicalRank
		}
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		if results[i].typeOrder != results[j].typeOrder {
			return results[i].typeOrder < results[j].typeOrder
		}
		return results[i].id < results[j].id
	})
	return results
}

func semanticLexicalMatch(component Component, query string) (int, float64) {
	filename := strings.ToLower(filepath.Base(component.Path))
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	title := strings.ToLower(strings.TrimSpace(component.Title))

	switch {
	case query == filename || query == stem:
		return semanticExactFilenameMatch, 1
	case query == title:
		return semanticExactTitleMatch, 0.9
	case strings.HasPrefix(stem, query):
		return semanticFilenamePrefixMatch, 0.8
	case strings.HasPrefix(title, query):
		return semanticTitlePrefixMatch, 0.7
	case strings.Contains(stem, query):
		return semanticFilenameSubstringMatch, 0.7
	case strings.Contains(title, query):
		return semanticTitleSubstringMatch, 0.6
	default:
		return semanticNoLexicalMatch, 0
	}
}

func semanticTypeOrder(componentType ComponentType) int {
	switch componentType {
	case TypeKnowledge:
		return 0
	case TypeReport:
		return 1
	case TypeDesign:
		return 2
	case TypeSpec:
		return 3
	case TypePlan:
		return 4
	case TypeProposal:
		return 5
	case TypeTasks:
		return 6
	case TypeChange:
		return 7
	case TypeArtifact:
		return 8
	case TypeDiagram:
		return 9
	default:
		return 10
	}
}
func vecNorm(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

func cosineSim(a, b []float32, normA, normB float64) float64 {
	if normA == 0 || normB == 0 {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot / (normA * normB)
}

// HandleLint normalizes a nil Lint() result to an empty slice before
// encoding. (*Graph).Lint() returns `var issues []LintIssue` unmodified when
// there are zero issues, which is a nil slice — encoding/json serializes nil
// slices as the JSON literal `null`, not `[]`. LintPanel.tsx relies on
// distinguishing "not yet fetched" (state stays null) from "fetched, zero
// issues" (state becomes []), so a raw `null` response for the clean-graph
// case would be indistinguishable from the loading state and the panel would
// never render. This mirrors HandleIndex's existing `make([]Component, 0)`
// pattern above.
func (a *API) HandleLint(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	issues := a.graph.Lint()
	if issues == nil {
		issues = []LintIssue{}
	}
	json.NewEncoder(w).Encode(issues)
}

// fixDeadLinkRequest is one link to repair.
type fixDeadLinkRequest struct {
	SourceID string `json:"sourceId"`
	OldPath  string `json:"oldPath"`
	NewPath  string `json:"newPath"`
}

type fixDeadLinkResult struct {
	SourceID string `json:"sourceId"`
	Fixed    bool   `json:"fixed"`
	Error    string `json:"error,omitempty"`
}

// HandleFixDeadLinks rewrites exact Markdown link destinations and updates
// their graph edges synchronously, so subsequent lint requests see the repair.
func (a *API) HandleFixDeadLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var reqs []fixDeadLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	results := make([]fixDeadLinkResult, 0, len(reqs))
	for _, req := range reqs {
		results = append(results, a.fixDeadLink(req))
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (a *API) fixDeadLink(req fixDeadLinkRequest) fixDeadLinkResult {
	res := fixDeadLinkResult{SourceID: req.SourceID}
	if req.SourceID == "" || req.OldPath == "" || req.NewPath == "" {
		res.Error = "sourceId, oldPath, and newPath are required"
		return res
	}

	a.mu.RLock()
	component, exists := a.graph.Component(req.SourceID)
	a.mu.RUnlock()
	if !exists || filepath.Clean(component.Path) != filepath.Clean(req.SourceID) {
		res.Error = "source component not found"
		return res
	}

	data, err := os.ReadFile(component.Path)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	updated, changed, err := a.rewriteDeadLinkSource(component, string(data), req.OldPath, req.NewPath)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if !changed {
		res.Error = "matching link reference not found in source"
		return res
	}
	info, err := os.Stat(component.Path)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if err := os.WriteFile(component.Path, []byte(updated), info.Mode().Perm()); err != nil {
		res.Error = err.Error()
		return res
	}
	if err := a.refreshRepairedLinkEdges(component); err != nil {
		res.Error = fmt.Sprintf("link repaired but graph refresh failed: %v", err)
		return res
	}
	res.Fixed = true
	return res
}

func (a *API) rewriteDeadLinkSource(component Component, content, oldPath, newPath string) (string, bool, error) {
	if component.Type != TypeChange {
		updated, changed := rewriteMarkdownLinkDestinations(component.Path, content, oldPath, newPath)
		return updated, changed, nil
	}
	_, workspacePath := a.resolveWorkspace(component.Path)
	if workspacePath == "" {
		return content, false, fmt.Errorf("source component is outside configured workspaces")
	}
	updated, changed := rewriteYAMLArtifactReferences(
		content,
		projectRootForWorkspace(workspacePath),
		filepath.Dir(component.Path),
		oldPath,
		newPath,
	)
	return updated, changed, nil
}

// refreshRepairedLinkEdges updates the edge layer that the repaired source
// owns, preserving generated artifact and structural edges.
func (a *API) refreshRepairedLinkEdges(component Component) error {
	source := "markdown-link"
	var edges []Edge
	var err error
	if component.Type == TypeChange {
		_, workspacePath := a.resolveWorkspace(component.Path)
		if workspacePath == "" {
			return fmt.Errorf("source component is outside configured workspaces")
		}
		source = "yaml"
		edges, err = ExtractYAMLLinks(
			filepath.Dir(component.Path),
			projectRootForWorkspace(workspacePath),
		)
	} else {
		edges, err = ExtractMarkdownLinks(component)
	}
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.graph.Component(component.ID); !exists {
		return nil
	}
	current := append([]Edge(nil), a.graph.Forward(component.ID)...)
	next := make([]Edge, 0, len(current)+len(edges))
	for _, edge := range current {
		if edge.Source != source {
			next = append(next, edge)
		}
	}
	next = append(next, edges...)
	if changed := changedEdgeCount(current, next); changed > 0 {
		a.graph.RemoveEdgesFrom(component.ID)
		a.graph.AddEdges(next)
		a.AddDirty(changed)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Rebuild reruns BuildIndex against the current workspace set (preferring
// the live lister set via SetLister over the construction-time snapshot in
// a.ws) and swaps the result into a.graph under lock. It is safe to call
// from a background goroutine — e.g. main.go kicks off the initial index
// build this way right after NewAPIWithWorkspacesAsync so the HTTP server
// can bind without waiting for a full workspace scan.
func (a *API) Rebuild() error {
	a.mu.RLock()
	lister := a.lister
	ws := a.ws
	a.mu.RUnlock()

	if lister != nil {
		ws = lister.List()
	}

	newGraph, err := BuildIndex(ws, a.indexCacheDir)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.graph = newGraph
	a.ready = true
	a.ResetDirty()
	a.mu.Unlock()
	return nil
}

func (a *API) HandleRebuild(w http.ResponseWriter, r *http.Request) {
	if err := a.Rebuild(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "rebuilt"})
}

// HandleSummarize returns an opt-in LLM summary for a component, using a
// single centralized cache directory (~/.comet-panel/wiki/summaries) rather
// than one derived from the component's own path. Deriving the cache dir
// from filepath.Dir(id) would scatter summaries across inconsistent
// locations depending on how deeply nested the component is (e.g. a
// change's design.md vs. a top-level spec vs. a nested artifact would each
// land in a different directory) — this mirrors the centralized
// ~/.comet-panel/wiki/ convention persistIndexCache already established for
// the index cache.
func (a *API) HandleSummarize(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	a.mu.RLock()
	c, ok := a.graph.Component(id)
	a.mu.RUnlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	cacheDir := filepath.Join(os.Getenv("HOME"), ".comet-panel", "wiki", "summaries")
	summary, err := Summarize(r.Context(), c, cacheDir)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"summary": summary})
}

// HandleOverview returns an opt-in LLM-generated overview for a community
// of 3+ members, cached under a single centralized directory
// (~/.comet-panel/wiki/overviews) keyed by membership hash — mirroring the
// HandleSummarize/Summarize convention above, but at the community rather
// than the single-component granularity.
func (a *API) HandleOverview(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("community")
	communityID, err := strconv.Atoi(idStr)
	if idStr == "" || err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	a.mu.RLock()
	communities := a.graph.Communities()
	components := a.graph.Components()
	a.mu.RUnlock()

	var members []Component
	for id, commID := range communities {
		if commID == communityID {
			if c, ok := components[id]; ok {
				members = append(members, c)
			}
		}
	}
	if len(members) < 3 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	cacheDir := filepath.Join(os.Getenv("HOME"), ".comet-panel", "wiki", "overviews")
	body, err := GenerateOverview(r.Context(), communityID, members, cacheDir)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"body": body})
}

// Neighborhood implements chat.WikiGraphAccessor: it returns changeID's
// direct (1-hop) neighbors — both forward edges and backlinks, so a change
// that is referenced-by another component shows up alongside one it
// references itself — plus the titles of their neighbors (2-hop), capped
// at 20 to keep the injected prompt section bounded.
func (a *API) Neighborhood(changeID string) ([]chat.NeighborInfo, []string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var direct []chat.NeighborInfo
	seen := map[string]bool{changeID: true}
	for _, e := range a.graph.Forward(changeID) {
		if c, ok := a.graph.Component(e.To); ok && !seen[e.To] {
			direct = append(direct, chat.NeighborInfo{ID: e.To, Title: c.Title, Kind: e.Kind})
			seen[e.To] = true
		}
	}
	for _, e := range a.graph.Backlinks(changeID) {
		if c, ok := a.graph.Component(e.From); ok && !seen[e.From] {
			direct = append(direct, chat.NeighborInfo{ID: e.From, Title: c.Title, Kind: e.Kind})
			seen[e.From] = true
		}
	}

	var secondHop []string
outer:
	for _, n := range direct {
		for _, e := range a.graph.Forward(n.ID) {
			if seen[e.To] {
				continue
			}
			if c, ok := a.graph.Component(e.To); ok {
				secondHop = append(secondHop, c.Title)
				seen[e.To] = true
				if len(secondHop) >= 20 {
					break outer
				}
			}
		}
	}
	return direct, secondHop
}

// CommunityOverview implements chat.WikiGraphAccessor: it returns the
// cached LLM overview for changeID's community, or "" when the change has
// no community, the community is too small to have one, or no overview has
// been generated yet. It never triggers generation itself (unlike
// HandleOverview) — this is a read-only cache lookup so injecting graph
// context into a chat request never blocks on an LLM call.
func (a *API) CommunityOverview(changeID string) string {
	a.mu.RLock()
	communities := a.graph.Communities()
	components := a.graph.Components()
	a.mu.RUnlock()

	communityID, ok := communities[changeID]
	if !ok {
		return ""
	}

	var members []Component
	for id, commID := range communities {
		if commID == communityID {
			if c, ok := components[id]; ok {
				members = append(members, c)
			}
		}
	}
	if len(members) < 3 {
		return ""
	}

	cacheDir := filepath.Join(a.indexCacheDir, "overviews")
	key := overviewCacheKey(members)
	data, err := os.ReadFile(overviewCachePath(cacheDir, communityID, key))
	if err != nil {
		return ""
	}
	return string(data)
}

// ComponentByID is a public accessor for the live graph. It returns the
// Component and true when the ID is in the graph, hiding the internal
// *Graph type from callers (e.g. the Todo REST handler) that only need
// the wiki.API contract.
func (a *API) ComponentByID(id string) (Component, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.graph.Component(id)
}
