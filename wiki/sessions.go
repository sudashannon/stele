package wiki

import (
	"log"
	"path/filepath"
	"sort"
	"time"

	"stele/internal/sessions"
)

// SessionsIndex holds the agent-session layer: transcript digests plus the
// components and edges grafted onto the document graph.
//
// Sessions are deliberately not a workspace source. Transcripts live outside
// every registered workspace, can reach hundreds of megabytes, and change
// while an agent is running, so they never travel through the document
// pipeline (scan → embed → tag → mirror → community). Instead the layer keeps
// its own digest cache and re-applies itself to the graph after each rebuild.
//
// One index serves several agent runtimes: each source pairs a provider (which
// knows a runtime's layout and transcript format) with the directory to read.
type SessionsIndex struct {
	sources []sessions.Source
	store   *sessions.Store
}

// NewSessionsIndex prepares the layer over one or more runtime sources, with
// cachePath holding the digest cache. No usable source disables the layer; a
// missing directory is not an error, so the panel behaves identically on
// machines without an agent runtime.
func NewSessionsIndex(cachePath string, sources ...sessions.Source) *SessionsIndex {
	usable := make([]sessions.Source, 0, len(sources))
	for _, source := range sources {
		if source.Provider == nil || source.Root == "" {
			continue
		}
		usable = append(usable, sessions.Source{Provider: source.Provider, Root: filepath.Clean(source.Root)})
	}
	if len(usable) == 0 {
		return nil
	}
	return &SessionsIndex{sources: usable, store: sessions.NewStore(cachePath)}
}

// Sources reports the configured runtime sources.
func (s *SessionsIndex) Sources() []sessions.Source {
	if s == nil {
		return nil
	}
	return append([]sessions.Source(nil), s.sources...)
}

// Root reports the first configured transcript directory. Kept for callers
// that only need somewhere to point a watcher or a diagnostic at.
func (s *SessionsIndex) Root() string {
	if s == nil || len(s.sources) == 0 {
		return ""
	}
	return s.sources[0].Root
}

// Refresh re-reads transcripts whose size or mtime changed and persists the
// digest cache. It returns how many digests changed.
//
// The duration is logged because a cold pass is not cheap and is otherwise
// invisible: a measured corpus of 77 sessions (368 transcripts, 724.7 MB) takes
// ~20s at 36 MB/s, and every cache-schema bump forces exactly that on the next
// start. Steady-state passes only re-read the tail of what grew.
func (s *SessionsIndex) Refresh() (int, error) {
	if s == nil {
		return 0, nil
	}
	started := time.Now()
	changed, total, err := s.store.Refresh(s.sources)
	if err != nil {
		return 0, err
	}
	if len(changed) > 0 {
		log.Printf("wiki sessions: %d/%d transcript digest(s) updated in %s",
			len(changed), total, time.Since(started).Round(time.Millisecond))
		if saveErr := s.store.Save(); saveErr != nil {
			log.Printf("wiki sessions: failed to persist digest cache: %v", saveErr)
		}
	}
	return len(changed), nil
}

// Digests returns every known session's merged digest, newest activity first.
func (s *SessionsIndex) Digests() []sessions.Digest {
	if s == nil {
		return nil
	}
	return s.store.List()
}

// Digest returns one digest by transcript path.
func (s *SessionsIndex) Digest(path string) (sessions.Digest, bool) {
	if s == nil {
		return sessions.Digest{}, false
	}
	return s.store.Get(path)
}

// Apply grafts sessions onto an already-built document graph: one component
// per transcript attributed to a registered workspace, plus session→document
// edges for the documents it read or edited. Transcripts whose working
// directory is outside every registered workspace are dropped, which is what
// keeps throwaway sessions (e.g. /tmp) out of the graph.
//
// Apply is idempotent and cheap relative to a rebuild, so every rebuild can
// re-run it instead of trying to preserve grafted state across graph swaps.
func (s *SessionsIndex) Apply(g *Graph, workspaces []WorkspaceConfig) (components int, edges int) {
	if s == nil || g == nil {
		return 0, 0
	}
	documents := g.Components()
	for _, digest := range s.store.List() {
		component, ok := SessionComponent(digest, workspaces)
		if !ok {
			continue
		}
		sessionEdges := SessionEdges(digest, component, documents)
		// Drop this session's previous edges first: AddEdges appends, so a
		// re-apply on a live graph would otherwise duplicate every edge. A
		// session's only outgoing edges are its own, so this is exact.
		g.RemoveEdgesFrom(component.ID)
		g.AddComponent(component)
		if len(sessionEdges) > 0 {
			g.AddEdges(sessionEdges)
		}
		components++
		edges += len(sessionEdges)
	}
	return components, edges
}

// SessionComponent converts a digest into a graph component. Attribution uses
// the transcript's working directory against the live workspace scopes, so a
// nested workspace wins over its parent.
func SessionComponent(digest sessions.Digest, workspaces []WorkspaceConfig) (Component, bool) {
	if digest.Path == "" || digest.Cwd == "" {
		return Component{}, false
	}
	workspace, ok := WorkspaceForPath(workspaces, digest.Cwd)
	if !ok {
		return Component{}, false
	}
	updated := digest.UpdatedAt
	if updated.IsZero() {
		updated = digest.ModTime
	}
	return Component{
		ID:        digest.Path,
		Type:      TypeSession,
		Title:     digest.Title,
		Path:      digest.Path,
		Workspace: workspace.Alias,
		UpdatedAt: updated,
	}, true
}

// SessionEdges links a session to the indexed documents it touched. Only paths
// that already resolve to a component become edges: the session layer never
// invents components, so a session that only touched source code or untracked
// files simply contributes fewer edges.
//
// Produced (`write`) and patched (`edit`) paths share the `edits` edge kind —
// both mean "this session changed this document" — while the digest keeps them
// apart for display.
func SessionEdges(digest sessions.Digest, component Component, documents map[string]Component) []Edge {
	total := len(digest.Writes) + len(digest.Edits) + len(digest.Reads)
	edges := make([]Edge, 0, total)
	seen := make(map[string]struct{}, total)
	// Write/edit paths are applied first so a document both changed and read in
	// the same session keeps the stronger relationship.
	for _, group := range []struct {
		kind  string
		paths []string
	}{
		{EdgeKindEdits, digest.Writes},
		{EdgeKindEdits, digest.Edits},
		{EdgeKindReads, digest.Reads},
	} {
		for _, path := range group.paths {
			target, ok := documents[path]
			if !ok || target.Type == TypeSession || path == component.ID {
				continue
			}
			if _, duplicate := seen[path]; duplicate {
				continue
			}
			seen[path] = struct{}{}
			edges = append(edges, Edge{
				From:   component.ID,
				To:     path,
				Kind:   group.kind,
				Source: SourceSession,
			})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		return edges[i].To < edges[j].To
	})
	return edges
}

// documentComponents returns only workspace documents, dropping synthetic
// session components. Used by the document-only stages (tag corpus, tag
// inheritance) so agent activity cannot shift document analysis.
func documentComponents(all []Component) []Component {
	filtered := make([]Component, 0, len(all))
	for _, component := range all {
		if component.Type == TypeSession {
			continue
		}
		filtered = append(filtered, component)
	}
	return filtered
}

// SessionSummary is the API shape for one session.
type SessionSummary struct {
	ID string `json:"id"`
	// Source names the agent runtime this session came from.
	Source    string         `json:"source,omitempty"`
	Path      string         `json:"path"`
	Workspace string         `json:"workspace"`
	Title     string         `json:"title"`
	Cwd       string         `json:"cwd"`
	StartedAt time.Time      `json:"startedAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	UserTurns int            `json:"userTurns"`
	ToolCalls map[string]int `json:"toolCalls,omitempty"`
	Writes    []string       `json:"writes"`
	Edits     []string       `json:"edits"`
	Reads     []string       `json:"reads"`
	Intents   []string       `json:"intents"`
	// Subagents names the nested transcripts folded into this session, so a
	// reader can tell aggregated totals from a single agent's own work.
	Subagents []string `json:"subagents,omitempty"`
	// Todos is the session's task list as it stood when the transcript ended,
	// and TodosCompleted the tasks it finished under earlier lists - together
	// the only surviving record of what it set out to do. Both are served by
	// the single-session endpoint only (see sessionSummaries).
	Todos          []sessions.TodoItem `json:"todos,omitempty"`
	TodosCompleted []string            `json:"todosCompleted,omitempty"`
	// TodoOpen / TodoTotal / TodoDone are the counts every caller needs even
	// when the lists themselves are withheld from the list endpoint: they are
	// what "this session left work unfinished" is filtered and sorted on.
	TodoOpen  int `json:"todoOpen,omitempty"`
	TodoTotal int `json:"todoTotal,omitempty"`
	TodoDone  int `json:"todoDone,omitempty"`
	// Activity counts each active day's turns and tool calls, keyed by local
	// date. A resumed session spans days, so its work cannot be placed in time
	// from StartedAt/UpdatedAt alone.
	Activity         map[string]int `json:"activity,omitempty"`
	TodoReplans      int            `json:"todoReplans,omitempty"`
	TodosTruncated   bool           `json:"todosTruncated,omitempty"`
	IntentsTruncated bool           `json:"intentsTruncated,omitempty"`
	PathsTruncated   bool           `json:"pathsTruncated,omitempty"`
}

// SessionSummaryOf projects a digest for the API, keeping only the touched
// paths that resolve to indexed documents so clients can link every entry.
func SessionSummaryOf(digest sessions.Digest, component Component, documents map[string]Component) SessionSummary {
	open, done := 0, len(digest.TodosCompleted)
	for _, item := range digest.Todos {
		switch item.Status {
		case sessions.TodoCompleted:
			done++
		case sessions.TodoDropped:
		default:
			// Pending, in progress and blocked are all work the session left
			// on the table - that is the number worth surfacing.
			open++
		}
	}
	return SessionSummary{
		ID:               digest.ID,
		Source:           digest.Source,
		Path:             digest.Path,
		Workspace:        component.Workspace,
		Title:            digest.Title,
		Cwd:              digest.Cwd,
		StartedAt:        digest.StartedAt,
		UpdatedAt:        digest.UpdatedAt,
		UserTurns:        digest.UserTurns,
		ToolCalls:        digest.ToolCalls,
		Writes:           indexedOnly(digest.Writes, documents),
		Edits:            indexedOnly(digest.Edits, documents),
		Reads:            indexedOnly(digest.Reads, documents),
		Intents:          digest.Intents,
		Subagents:        digest.Subagents,
		Todos:            digest.Todos,
		TodosCompleted:   digest.TodosCompleted,
		TodoOpen:         open,
		TodoTotal:        len(digest.Todos),
		TodoDone:         done,
		Activity:         digest.Activity,
		TodoReplans:      digest.TodoReplans,
		TodosTruncated:   digest.TodosTruncated,
		IntentsTruncated: digest.IntentsTruncated,
		PathsTruncated:   digest.PathsTruncated,
	}
}

func indexedOnly(paths []string, documents map[string]Component) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if target, ok := documents[path]; ok && target.Type != TypeSession {
			out = append(out, path)
		}
	}
	return out
}
