package wiki

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stele/internal/sessions"
)

// ErrIndexNotReady distinguishes an asynchronously-building Wiki index from a
// valid, ready index whose selected report window simply contains no documents.
var ErrIndexNotReady = fmt.Errorf("wiki index is not ready")

// DocumentWindowFilter selects report evidence by the document's last indexed
// update time. Start is inclusive and End is exclusive.
type DocumentWindowFilter struct {
	Start               time.Time
	End                 time.Time
	Workspace           string
	IncludeContext      bool
	MaxContextDocuments int
}

// SnapshotDocument is an immutable scalar projection of an indexed Markdown
// component. It intentionally omits Component.Frontmatter so callers cannot
// retain a live map owned by the graph.
type SnapshotDocument struct {
	ID          string
	Type        ComponentType
	Title       string
	Path        string
	Workspace   string
	UpdatedAt   time.Time
	ContextOnly bool
}

// SnapshotConnector is a non-evidence graph node retained only so ownership
// relationships can connect documents without treating the node as report
// input. Today these are synthetic TypeChange components.
type SnapshotConnector struct {
	ID   string
	Type ComponentType
}

// DocumentWindowSnapshot owns every slice and vector it exposes. None of its
// values alias the live graph after SnapshotDocuments releases API.mu.
type DocumentWindowSnapshot struct {
	Documents        []SnapshotDocument
	Connectors       []SnapshotConnector
	Edges            []Edge
	Embeddings       map[string][]float32
	FailedWorkspaces []string
}

// SessionWorkItem is one session's contribution to a report window: how much
// work happened inside it and which indexed documents it touched.
//
// Sessions are not report evidence - a digest is derived, not authored prose, and
// SnapshotDocuments deliberately excludes transcripts. This is the effort axis
// instead: document counts say how much was written, not how much was done, and
// a week where one bulk import produced 189 files while six engineering sessions
// produced a dozen each cannot be ordered by document count without lying.
type SessionWorkItem struct {
	ID         string
	Path       string
	Title      string
	Workspace  string
	StartedAt  time.Time
	UpdatedAt  time.Time
	ActiveDays int
	// Events counts the turns and tool calls recorded inside the window only, so
	// a long-running session contributes what it did that week.
	Events    int
	UserTurns int
	Subagents int
	// Produced and Edited are indexed document paths, already narrowed to the
	// window's workspace filter. Read paths are omitted: reading a document is
	// not authorship, and attributing a report section by reads would credit the
	// session that merely consulted a document.
	Produced []string
	Edited   []string
	// OpenTodos are the tasks the session left unfinished, with the reason it
	// recorded for the blocked ones.
	OpenTodos []SessionTodoSnapshot
	// Activity holds only the in-window days, so a caller can narrow to a
	// sub-window (a monthly report's missing weeks) without another index pass.
	Activity map[string]int
}

// SessionTodoSnapshot is one unfinished task carried into a report.
type SessionTodoSnapshot struct {
	Phase   string
	Content string
	Status  string
	Blocker string
}

// SnapshotSessionWork returns the sessions active inside [start, end) with their
// in-window effort and the documents they touched. It never returns transcript
// paths as documents.
func (a *API) SnapshotSessionWork(start, end time.Time, workspace string) []SessionWorkItem {
	if end.IsZero() || !end.After(start) {
		return nil
	}
	index, workspaces := a.sessionsSnapshot()
	if index == nil {
		return nil
	}
	a.mu.RLock()
	documents := a.graph.Components()
	a.mu.RUnlock()

	items := make([]SessionWorkItem, 0)
	for _, digest := range index.Digests() {
		component, ok := SessionComponent(digest, workspaces)
		if !ok {
			continue
		}
		if workspace != "" && component.Workspace != workspace {
			continue
		}
		days, events := 0, 0
		activity := make(map[string]int)
		for day, count := range digest.Activity {
			parsed, err := time.ParseInLocation(sessions.ActivityDateLayout, day, time.Local)
			if err != nil || parsed.Before(start) || !parsed.Before(end) {
				continue
			}
			days++
			events += count
			activity[day] = count
		}
		if days == 0 {
			continue
		}
		item := SessionWorkItem{
			ID:         digest.ID,
			Path:       digest.Path,
			Title:      digest.Title,
			Workspace:  component.Workspace,
			StartedAt:  digest.StartedAt,
			UpdatedAt:  digest.UpdatedAt,
			ActiveDays: days,
			Events:     events,
			UserTurns:  digest.UserTurns,
			Subagents:  len(digest.Subagents),
			Activity:   activity,
			Produced:   indexedDocumentPaths(digest.Writes, documents, workspace),
			Edited:     indexedDocumentPaths(digest.Edits, documents, workspace),
		}
		for _, todo := range digest.Todos {
			if todo.Status == sessions.TodoCompleted || todo.Status == sessions.TodoDropped {
				continue
			}
			item.OpenTodos = append(item.OpenTodos, SessionTodoSnapshot{
				Phase: todo.Phase, Content: todo.Content, Status: todo.Status, Blocker: todo.Blocker,
			})
		}
		items = append(items, item)
	}
	// Effort order, so the caller renders the week by what it cost rather than
	// by what it happened to write down.
	sort.Slice(items, func(i, j int) bool {
		if items[i].Events != items[j].Events {
			return items[i].Events > items[j].Events
		}
		return items[i].Path < items[j].Path
	})
	return items
}

// indexedDocumentPaths keeps only paths that resolve to an indexed Markdown
// document in scope, so a session cannot pull an unindexed or foreign-workspace
// file into a report.
func indexedDocumentPaths(paths []string, documents map[string]Component, workspace string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		component, ok := documents[path]
		if !ok || component.Type == TypeSession {
			continue
		}
		if workspace != "" && component.Workspace != workspace {
			continue
		}
		if !strings.EqualFold(filepath.Ext(component.Path), ".md") {
			continue
		}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

type contextCandidate struct {
	component Component
	priority  int
}

// SnapshotDocuments captures a stable report input view without holding the
// Wiki graph lock across filesystem reads or LLM calls.
func (a *API) SnapshotDocuments(filter DocumentWindowFilter) (DocumentWindowSnapshot, error) {
	if filter.End.IsZero() || !filter.End.After(filter.Start) {
		return DocumentWindowSnapshot{}, fmt.Errorf("invalid document window")
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.ready {
		return DocumentWindowSnapshot{}, ErrIndexNotReady
	}

	components := a.graph.Components()
	primary := make(map[string]Component)
	for id, component := range components {
		if !snapshotDocumentEligible(component, filter.Workspace) {
			continue
		}
		if component.UpdatedAt.Before(filter.Start) || !component.UpdatedAt.Before(filter.End) {
			continue
		}
		primary[id] = component
	}

	allEdges := make([]Edge, 0)
	for _, edges := range a.graph.forward {
		allEdges = append(allEdges, edges...)
	}

	connectors := make(map[string]Component)
	candidates := make(map[string]contextCandidate)
	if filter.IncludeContext && len(primary) > 0 {
		for _, edge := range allEdges {
			if !contextRelationEligible(edge) {
				continue
			}
			_, fromPrimary := primary[edge.From]
			_, toPrimary := primary[edge.To]
			if !fromPrimary && !toPrimary {
				continue
			}
			otherID := edge.To
			if toPrimary {
				otherID = edge.From
			}
			collectContextNeighbor(components, otherID, filter, 0, connectors, candidates)
		}

		// A synthetic change/task connector can own several durable documents.
		// Project one additional relationship hop without making the connector
		// itself report evidence.
		for _, edge := range allEdges {
			if !contextRelationEligible(edge) {
				continue
			}
			if _, ok := connectors[edge.From]; ok {
				collectContextNeighbor(components, edge.To, filter, 1, connectors, candidates)
			}
			if _, ok := connectors[edge.To]; ok {
				collectContextNeighbor(components, edge.From, filter, 1, connectors, candidates)
			}
		}
	}

	contextDocuments := make([]contextCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		contextDocuments = append(contextDocuments, candidate)
	}
	sort.Slice(contextDocuments, func(i, j int) bool {
		if contextDocuments[i].priority != contextDocuments[j].priority {
			return contextDocuments[i].priority < contextDocuments[j].priority
		}
		if !contextDocuments[i].component.UpdatedAt.Equal(contextDocuments[j].component.UpdatedAt) {
			return contextDocuments[i].component.UpdatedAt.After(contextDocuments[j].component.UpdatedAt)
		}
		return contextDocuments[i].component.ID < contextDocuments[j].component.ID
	})
	if filter.MaxContextDocuments >= 0 && len(contextDocuments) > filter.MaxContextDocuments {
		contextDocuments = contextDocuments[:filter.MaxContextDocuments]
	}

	includedDocuments := make(map[string]Component, len(primary)+len(contextDocuments))
	for id, component := range primary {
		includedDocuments[id] = component
	}
	contextIDs := make(map[string]bool, len(contextDocuments))
	for _, candidate := range contextDocuments {
		includedDocuments[candidate.component.ID] = candidate.component
		contextIDs[candidate.component.ID] = true
	}

	includedIDs := make(map[string]bool, len(includedDocuments)+len(connectors))
	for id := range includedDocuments {
		includedIDs[id] = true
	}
	for id := range connectors {
		includedIDs[id] = true
	}

	result := DocumentWindowSnapshot{
		Documents:        make([]SnapshotDocument, 0, len(includedDocuments)),
		Connectors:       make([]SnapshotConnector, 0, len(connectors)),
		Edges:            make([]Edge, 0),
		Embeddings:       make(map[string][]float32, len(includedDocuments)),
		FailedWorkspaces: append([]string(nil), a.graph.FailedWorkspaces()...),
	}
	for id, component := range includedDocuments {
		result.Documents = append(result.Documents, SnapshotDocument{
			ID:          id,
			Type:        component.Type,
			Title:       component.Title,
			Path:        component.Path,
			Workspace:   component.Workspace,
			UpdatedAt:   component.UpdatedAt,
			ContextOnly: contextIDs[id],
		})
		if vector, ok := a.graph.embeddings[id]; ok {
			result.Embeddings[id] = append([]float32(nil), vector...)
		}
	}
	for id, component := range connectors {
		result.Connectors = append(result.Connectors, SnapshotConnector{ID: id, Type: component.Type})
	}
	for _, edge := range allEdges {
		if edge.Source == "vector" || !includedIDs[edge.From] || !includedIDs[edge.To] {
			continue
		}
		result.Edges = append(result.Edges, edge)
	}

	sort.Slice(result.Documents, func(i, j int) bool { return result.Documents[i].ID < result.Documents[j].ID })
	sort.Slice(result.Connectors, func(i, j int) bool { return result.Connectors[i].ID < result.Connectors[j].ID })
	sort.Slice(result.Edges, func(i, j int) bool {
		a, b := result.Edges[i], result.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Kind < b.Kind
	})
	return result, nil
}

func snapshotDocumentEligible(component Component, workspace string) bool {
	return component.Type != TypeChange &&
		component.Path != "" &&
		strings.EqualFold(filepath.Ext(component.Path), ".md") &&
		(workspace == "" || component.Workspace == workspace)
}

func contextRelationEligible(edge Edge) bool {
	return edge.Source != "vector" && edge.Source != "markdown-link" && edge.Source != "slug-match"
}

func collectContextNeighbor(
	components map[string]Component,
	id string,
	filter DocumentWindowFilter,
	priority int,
	connectors map[string]Component,
	candidates map[string]contextCandidate,
) {
	component, ok := components[id]
	if !ok {
		return
	}
	if component.Type == TypeChange {
		connectors[id] = component
		return
	}
	if !snapshotDocumentEligible(component, filter.Workspace) || !component.UpdatedAt.Before(filter.Start) {
		return
	}
	if existing, ok := candidates[id]; !ok || priority < existing.priority {
		candidates[id] = contextCandidate{component: component, priority: priority}
	}
}
