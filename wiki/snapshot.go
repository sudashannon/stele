package wiki

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
