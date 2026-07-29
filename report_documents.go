package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"time"

	"comet-ui/wiki"
)

const reportEvidenceRuneBudget = 4096

type reportEvidenceMetadata struct {
	Headings      []string `json:"headings,omitempty"`
	KeyParagraphs []string `json:"keyParagraphs,omitempty"`
	ChecklistDone int      `json:"checklistDone"`
	ChecklistOpen int      `json:"checklistOpen"`
	Truncated     bool     `json:"truncated"`
}

type reportDocument struct {
	EvidenceID   string                 `json:"evidenceId"`
	SourceID     string                 `json:"sourceId"`
	Path         string                 `json:"path"`
	Title        string                 `json:"title"`
	Type         wiki.ComponentType     `json:"type"`
	Workspace    string                 `json:"workspace"`
	ActivityAt   time.Time              `json:"activityAt"`
	ContentHash  string                 `json:"contentHash"`
	RelationIDs  []string               `json:"relationIds,omitempty"`
	ContextOnly  bool                   `json:"contextOnly"`
	Metadata     reportEvidenceMetadata `json:"metadata"`
	SemanticText string                 `json:"-"`
	Vector       []float32              `json:"-"`
}

type reportSkippedDocument struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type reportCoverage struct {
	SourceDocuments    int                     `json:"sourceDocuments"`
	ContextDocuments   int                     `json:"contextDocuments"`
	ReadableDocuments  int                     `json:"readableDocuments"`
	TruncatedDocuments int                     `json:"truncatedDocuments"`
	MissingEmbeddings  int                     `json:"missingEmbeddings"`
	FailedWorkspaces   []string                `json:"failedWorkspaces,omitempty"`
	SkippedDocuments   []reportSkippedDocument `json:"skippedDocuments,omitempty"`
	ClusteringMode     string                  `json:"clusteringMode,omitempty"`
}

type documentReportCounts struct {
	Documents  int            `json:"documents"`
	WorkItems  int            `json:"workItems"`
	Themes     int            `json:"themes"`
	Workspaces int            `json:"workspaces"`
	Reports    int            `json:"reports"`
	Types      map[string]int `json:"types"`
}

type reportCorpus struct {
	Start      time.Time
	End        time.Time
	Workspace  string
	Documents  []reportDocument
	Edges      []wiki.Edge
	Connectors []wiki.SnapshotConnector
	Counts     documentReportCounts
	Coverage   reportCoverage
}

// extractReportCorpus performs the only filesystem reads in report generation.
// The snapshot is already detached from the live Wiki graph; each source path
// is then read once, hashed, and reduced to deterministic bounded evidence.
func extractReportCorpus(snapshot wiki.DocumentWindowSnapshot, start, end time.Time, workspace string) reportCorpus {
	documents := append([]wiki.SnapshotDocument(nil), snapshot.Documents...)
	sort.Slice(documents, func(i, j int) bool { return documents[i].ID < documents[j].ID })

	corpus := reportCorpus{
		Start:      start,
		End:        end,
		Workspace:  workspace,
		Edges:      append([]wiki.Edge(nil), snapshot.Edges...),
		Connectors: append([]wiki.SnapshotConnector(nil), snapshot.Connectors...),
		Counts:     documentReportCounts{Types: make(map[string]int)},
		Coverage: reportCoverage{
			FailedWorkspaces: append([]string(nil), snapshot.FailedWorkspaces...),
		},
	}
	workspaceSet := make(map[string]struct{})
	readableIndex := 0
	for _, source := range documents {
		if source.ContextOnly {
			corpus.Coverage.ContextDocuments++
		} else {
			corpus.Coverage.SourceDocuments++
		}

		content, err := os.ReadFile(source.Path)
		if err != nil {
			corpus.Coverage.SkippedDocuments = append(corpus.Coverage.SkippedDocuments, reportSkippedDocument{
				Path:  source.Path,
				Error: err.Error(),
			})
			continue
		}
		readableIndex++
		semantic := wiki.ExtractSemanticText(source.Title, content, reportEvidenceRuneBudget)
		hash := sha256.Sum256(content)
		relations := snapshotRelationIDs(source.ID, snapshot.Edges)
		document := reportDocument{
			EvidenceID:  fmt.Sprintf("D%d", readableIndex),
			SourceID:    source.ID,
			Path:        source.Path,
			Title:       source.Title,
			Type:        source.Type,
			Workspace:   source.Workspace,
			ActivityAt:  source.UpdatedAt,
			ContentHash: hex.EncodeToString(hash[:]),
			RelationIDs: relations,
			ContextOnly: source.ContextOnly,
			Metadata: reportEvidenceMetadata{
				Headings:      append([]string(nil), semantic.Headings...),
				KeyParagraphs: append([]string(nil), semantic.KeyParagraphs...),
				ChecklistDone: semantic.ChecklistDone,
				ChecklistOpen: semantic.ChecklistTotal - semantic.ChecklistDone,
				Truncated:     semantic.Truncated,
			},
			SemanticText: semantic.Text,
			Vector:       append([]float32(nil), snapshot.Embeddings[source.ID]...),
		}
		corpus.Documents = append(corpus.Documents, document)
		corpus.Coverage.ReadableDocuments++
		if semantic.Truncated {
			corpus.Coverage.TruncatedDocuments++
		}
		if len(document.Vector) == 0 {
			corpus.Coverage.MissingEmbeddings++
		}
		if !source.ContextOnly {
			corpus.Counts.Documents++
			corpus.Counts.Types[string(source.Type)]++
			if source.Type == wiki.TypeReport {
				corpus.Counts.Reports++
			}
			workspaceSet[source.Workspace] = struct{}{}
		}
	}
	corpus.Counts.Workspaces = len(workspaceSet)
	sort.Slice(corpus.Coverage.FailedWorkspaces, func(i, j int) bool {
		return corpus.Coverage.FailedWorkspaces[i] < corpus.Coverage.FailedWorkspaces[j]
	})
	return corpus
}

func snapshotRelationIDs(documentID string, edges []wiki.Edge) []string {
	seen := make(map[string]struct{})
	for _, edge := range edges {
		var other string
		switch documentID {
		case edge.From:
			other = edge.To
		case edge.To:
			other = edge.From
		default:
			continue
		}
		seen[other] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
