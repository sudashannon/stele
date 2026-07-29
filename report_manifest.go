package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"comet-ui/wiki"
)

const (
	reportManifestSchemaVersion = 1
	reportPromptVersion         = "document-report-v1"
	reportClusteringVersion     = "relation-centroid-v1"
)

type reportEvidenceClaim struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidenceIds"`
	Date        string   `json:"date,omitempty"`
}

type reportThemeDigest struct {
	ID                 string                `json:"id"`
	Label              string                `json:"label"`
	Summary            reportEvidenceClaim   `json:"summary"`
	Claims             []reportEvidenceClaim `json:"claims"`
	EvidenceIDs        []string              `json:"evidenceIds"`
	ContextEvidenceIDs []string              `json:"contextEvidenceIds,omitempty"`
	RepresentativeIDs  []string              `json:"representativeIds"`
	Independent        bool                  `json:"independent,omitempty"`
}

type reportDigestDocument struct {
	EvidenceID  string                 `json:"evidenceId"`
	SourceID    string                 `json:"sourceId"`
	Path        string                 `json:"path"`
	Title       string                 `json:"title"`
	Type        wiki.ComponentType     `json:"type"`
	Workspace   string                 `json:"workspace"`
	ActivityAt  string                 `json:"activityAt"`
	ContentHash string                 `json:"contentHash"`
	ContextOnly bool                   `json:"contextOnly"`
	Metadata    reportEvidenceMetadata `json:"metadata"`
	RelationIDs []string               `json:"relationIds,omitempty"`
}

type reportDigestRelation struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

type reportGeneratorMetadata struct {
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	PromptVersion     string `json:"promptVersion"`
	EmbeddingVersion  uint16 `json:"embeddingVersion"`
	ClusteringVersion string `json:"clusteringVersion"`
}

type reportPeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type reportPeriodDigest struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	DigestID          string                  `json:"digestId"`
	Type              string                  `json:"type"`
	Start             string                  `json:"start"`
	End               string                  `json:"end"`
	Workspace         string                  `json:"workspace"`
	InputSnapshotHash string                  `json:"inputSnapshotHash"`
	Documents         []reportDigestDocument  `json:"documents"`
	Relations         []reportDigestRelation  `json:"relations,omitempty"`
	Themes            []reportThemeDigest     `json:"themes"`
	Counts            documentReportCounts    `json:"counts"`
	Coverage          reportCoverage          `json:"coverage"`
	SourceReportIDs   []string                `json:"sourceReportIds,omitempty"`
	GeneratedSlices   []reportPeriod          `json:"generatedSlices,omitempty"`
	Generator         reportGeneratorMetadata `json:"generator"`
	GeneratedAt       string                  `json:"generatedAt"`
	ReportFile        string                  `json:"reportFile"`
}

func newReportPeriodDigest(type_, start, end, workspace string, corpus *reportCorpus, themes []reportThemeDigest, providerName, model string) reportPeriodDigest {
	documents := make([]reportDigestDocument, 0, len(corpus.Documents))
	for _, document := range corpus.Documents {
		documents = append(documents, reportDigestDocument{
			EvidenceID:  document.EvidenceID,
			SourceID:    document.SourceID,
			Path:        document.Path,
			Title:       document.Title,
			Type:        document.Type,
			Workspace:   document.Workspace,
			ActivityAt:  document.ActivityAt.Format(time.RFC3339),
			ContentHash: document.ContentHash,
			ContextOnly: document.ContextOnly,
			Metadata:    document.Metadata,
			RelationIDs: append([]string(nil), document.RelationIDs...),
		})
		sort.Strings(documents[len(documents)-1].RelationIDs)
	}
	relations := make([]reportDigestRelation, 0, len(corpus.Edges))
	for _, edge := range corpus.Edges {
		relations = append(relations, reportDigestRelation{
			From: edge.From, To: edge.To, Kind: edge.Kind, Source: edge.Source,
		})
	}
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].From != relations[j].From {
			return relations[i].From < relations[j].From
		}
		if relations[i].To != relations[j].To {
			return relations[i].To < relations[j].To
		}
		if relations[i].Source != relations[j].Source {
			return relations[i].Source < relations[j].Source
		}
		return relations[i].Kind < relations[j].Kind
	})
	digest := reportPeriodDigest{
		SchemaVersion: reportManifestSchemaVersion,
		Type:          type_, Start: start, End: end, Workspace: workspace,
		Documents: documents,
		Relations: relations,
		Themes:    themes,
		Counts:    corpus.Counts,
		Coverage:  corpus.Coverage,
		Generator: reportGeneratorMetadata{
			Provider:          providerName,
			Model:             model,
			PromptVersion:     reportPromptVersion,
			EmbeddingVersion:  wiki.EmbeddingInputVersion,
			ClusteringVersion: reportClusteringVersion,
		},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	digest.InputSnapshotHash = reportInputSnapshotHash(type_, start, end, workspace, documents, relations)
	digest.DigestID = reportDigestID(digest)
	return digest
}

func reportInputSnapshotHash(type_, start, end, workspace string, documents []reportDigestDocument, relations []reportDigestRelation) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00", type_, start, end, workspace)
	ordered := append([]reportDigestDocument(nil), documents...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SourceID != ordered[j].SourceID {
			return ordered[i].SourceID < ordered[j].SourceID
		}
		return ordered[i].ContentHash < ordered[j].ContentHash
	})
	for _, document := range ordered {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00",
			document.SourceID, document.ContentHash, document.ActivityAt, document.Workspace,
			document.Type, document.Title, document.ContextOnly)
		relationIDs := append([]string(nil), document.RelationIDs...)
		sort.Strings(relationIDs)
		for _, relationID := range relationIDs {
			fmt.Fprintf(hash, "%s\x00", relationID)
		}
	}
	for _, relation := range relations {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00", relation.From, relation.To, relation.Kind, relation.Source)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func reportDigestID(digest reportPeriodDigest) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s",
		digest.Type, digest.Start, digest.End, digest.Workspace, digest.InputSnapshotHash,
		digest.Generator.Provider, digest.Generator.Model, digest.Generator.EmbeddingVersion,
		digest.Generator.PromptVersion, digest.Generator.ClusteringVersion)
	for _, id := range digest.SourceReportIDs {
		fmt.Fprintf(hash, "\x00source:%s", id)
	}
	for _, period := range digest.GeneratedSlices {
		fmt.Fprintf(hash, "\x00slice:%s:%s", period.Start, period.End)
	}
	return hex.EncodeToString(hash.Sum(nil))[:20]
}

// saveReportBundle persists the rendered report and its machine-readable
// PeriodDigest sidecar as one logical operation. If the sidecar rename fails,
// the newly written report is removed instead of leaving an untraceable file.
func saveReportBundle(dir string, digest *reportPeriodDigest, body []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s_%s-%d.%s", digest.Type, digest.Start, digest.End, time.Now().UnixNano(), ext(digest.Type))
	manifestName := name + ".manifest.json"
	digest.ReportFile = name
	manifest, err := json.MarshalIndent(digest, "", "  ")
	if err != nil {
		return "", err
	}
	bodyTemp, err := writeReportTemp(dir, ".report-body-*", body)
	if err != nil {
		return "", err
	}
	defer os.Remove(bodyTemp)
	manifestTemp, err := writeReportTemp(dir, ".report-manifest-*", manifest)
	if err != nil {
		return "", err
	}
	defer os.Remove(manifestTemp)

	bodyPath := filepath.Join(dir, name)
	manifestPath := filepath.Join(dir, manifestName)
	if err := os.Rename(bodyTemp, bodyPath); err != nil {
		return "", err
	}
	if err := os.Rename(manifestTemp, manifestPath); err != nil {
		_ = os.Remove(bodyPath)
		return "", err
	}
	return name, nil
}

func writeReportTemp(dir, pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Chmod(0o644); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

func loadContainedWeeklyDigests(dir, start, end, workspace string) ([]reportPeriodDigest, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	startDate, endDate, err := parseInclusiveReportDates(start, end)
	if err != nil {
		return nil, err
	}
	latestByPeriod := make(map[string]reportPeriodDigest)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".manifest.json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			continue
		}
		var digest reportPeriodDigest
		if json.Unmarshal(data, &digest) != nil || digest.SchemaVersion != reportManifestSchemaVersion || digest.Type != "weekly" || digest.Workspace != workspace {
			continue
		}
		digestStart, digestEnd, parseErr := parseInclusiveReportDates(digest.Start, digest.End)
		if parseErr != nil || digestStart.Before(startDate) || digestEnd.After(endDate) {
			continue
		}
		key := digest.Start + "\x00" + digest.End + "\x00" + digest.Workspace
		current, exists := latestByPeriod[key]
		if !exists || digest.GeneratedAt > current.GeneratedAt || (digest.GeneratedAt == current.GeneratedAt && digest.ReportFile > current.ReportFile) {
			latestByPeriod[key] = digest
		}
	}
	digests := make([]reportPeriodDigest, 0, len(latestByPeriod))
	for _, digest := range latestByPeriod {
		digests = append(digests, digest)
	}
	sort.Slice(digests, func(i, j int) bool {
		if digests[i].Start != digests[j].Start {
			return digests[i].Start < digests[j].Start
		}
		return digests[i].DigestID < digests[j].DigestID
	})
	return digests, nil
}

func parseInclusiveReportDates(start, end string) (time.Time, time.Time, error) {
	startDate, err := time.ParseInLocation("2006-01-02", start, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start: %w", err)
	}
	endDate, err := time.ParseInLocation("2006-01-02", end, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end: %w", err)
	}
	if endDate.Before(startDate) {
		return time.Time{}, time.Time{}, fmt.Errorf("end before start")
	}
	return startDate, endDate, nil
}
