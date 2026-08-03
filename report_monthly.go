package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"stele/chat"
	"stele/chat/provider"
	"stele/wiki"
)

var reportEmbedClusterDigests = func(components []wiki.Component) (map[string][]float32, error) {
	return wiki.ComputeEmbeddings(components, wiki.EmbeddingScriptPath())
}

// reportEvidenceMentionRE matches standalone evidence IDs like D27 in prose.
// Word boundaries ensure D27x, ADD27, 2027, and bare D are not matched.
var reportEvidenceMentionRE = regexp.MustCompile(`\bD\d+\b`)

type monthlySourceTheme struct {
	DigestID    string
	PeriodStart string
	PeriodEnd   string
	Theme       reportThemeDigest
}

type monthlyMacroSource struct {
	PeriodStart string                `json:"periodStart"`
	PeriodEnd   string                `json:"periodEnd"`
	Label       string                `json:"label"`
	Summary     reportEvidenceClaim   `json:"summary"`
	Claims      []reportEvidenceClaim `json:"claims"`
}

type monthlyMacroPrompt struct {
	Start          string                 `json:"start"`
	End            string                 `json:"end"`
	Workspace      string                 `json:"workspace"`
	ThemeID        string                 `json:"themeId"`
	SuggestedLabel string                 `json:"suggestedLabel"`
	Independent    bool                   `json:"independent"`
	Sources        []monthlyMacroSource   `json:"weeklyClusterDigests"`
	Documents      []reportDigestDocument `json:"evidenceDocuments"`
}

func generateMonthlyDocumentReport(ctx context.Context, fullCorpus *reportCorpus, sessionWork []wiki.SessionWorkItem, reportsDir, start, end string, pcfg chat.ProviderConfig, p provider.Provider) ([]byte, reportPeriodDigest, error) {
	reused, err := loadContainedWeeklyDigests(reportsDir, start, end, fullCorpus.Workspace)
	if err != nil {
		return nil, reportPeriodDigest{}, fmt.Errorf("读取周报摘要失败: %w", err)
	}
	reused = selectNonOverlappingWeeklyDigests(reused)
	periods, err := missingReportPeriods(start, end, reused)
	if err != nil {
		return nil, reportPeriodDigest{}, err
	}

	sources := append([]reportPeriodDigest(nil), reused...)
	generatedSlices := make([]reportPeriod, 0, len(periods))
	for _, period := range periods {
		generatedSlices = append(generatedSlices, period)
		periodStart, periodEnd, parseErr := parseInclusiveReportDates(period.Start, period.End)
		if parseErr != nil {
			return nil, reportPeriodDigest{}, parseErr
		}
		slice := subsetReportCorpus(fullCorpus, periodStart, periodEnd.AddDate(0, 0, 1))
		if len(slice.Documents) == 0 {
			continue
		}
		// Same grouping as a weekly report: a month must not reorganise a week
		// just because that week was never generated on its own.
		attribution := attributeReportCorpus(&slice, subsetSessionWork(sessionWork, periodStart, periodEnd.AddDate(0, 0, 1)))
		themes := buildSessionThemes(&slice, &attribution)
		slice.Counts.Themes = len(themes)
		themeDigests, summarizeErr := summarizeReportThemes(ctx, &slice, themes, pcfg, p)
		if summarizeErr != nil {
			return nil, reportPeriodDigest{}, summarizeErr
		}
		digest := newReportPeriodDigest("slice", period.Start, period.End, fullCorpus.Workspace, &slice, themeDigests, p.Name(), pcfg.Model)
		attachReportEffort(&digest, &slice, attribution)
		sources = append(sources, digest)
	}

	canonicalCorpus, sourceThemes := canonicalizeMonthlySources(sources, fullCorpus)
	macroCorpus, orderedSources := buildMonthlyThemeCorpus(sourceThemes, &canonicalCorpus, start, end, fullCorpus.Workspace)
	// Group by the session that did the work, not by how its summary reads.
	macroThemes := groupMonthlyWorkItems(orderedSources, &macroCorpus)
	monthlyThemes, err := summarizeMonthlyThemes(ctx, &canonicalCorpus, &macroCorpus, orderedSources, macroThemes, pcfg, p)
	if err != nil {
		return nil, reportPeriodDigest{}, err
	}
	canonicalCorpus.Counts.Themes = len(monthlyThemes)
	canonicalCorpus.Coverage.ClusteringMode = macroCorpus.Coverage.ClusteringMode
	canonicalCorpus.Coverage.MissingEmbeddings = macroCorpus.Coverage.MissingEmbeddings

	digest := newReportPeriodDigest("monthly", start, end, fullCorpus.Workspace, &canonicalCorpus, monthlyThemes, p.Name(), pcfg.Model)
	attachMonthlyEffort(&digest, sources)
	for _, source := range reused {
		digest.SourceReportIDs = append(digest.SourceReportIDs, source.DigestID)
	}
	sort.Strings(digest.SourceReportIDs)
	digest.GeneratedSlices = generatedSlices
	digest.DigestID = reportDigestID(digest)
	monthly := monthlyJSONFromDigest(digest)
	raw, err := json.Marshal(monthly)
	if err != nil {
		return nil, reportPeriodDigest{}, err
	}
	body, err := renderMonthlyFromJSON(raw)
	if err != nil {
		return nil, reportPeriodDigest{}, err
	}
	return body, digest, nil
}

func selectNonOverlappingWeeklyDigests(input []reportPeriodDigest) []reportPeriodDigest {
	candidates := append([]reportPeriodDigest(nil), input...)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Start != candidates[j].Start {
			return candidates[i].Start < candidates[j].Start
		}
		if candidates[i].End != candidates[j].End {
			return candidates[i].End > candidates[j].End
		}
		return candidates[i].GeneratedAt > candidates[j].GeneratedAt
	})
	covered := make(map[string]bool)
	selected := make([]reportPeriodDigest, 0, len(candidates))
	for _, candidate := range candidates {
		start, end, err := parseInclusiveReportDates(candidate.Start, candidate.End)
		if err != nil {
			continue
		}
		overlaps := false
		for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
			if covered[day.Format("2006-01-02")] {
				overlaps = true
				break
			}
		}
		if overlaps {
			continue
		}
		selected = append(selected, candidate)
		for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
			covered[day.Format("2006-01-02")] = true
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Start < selected[j].Start })
	return selected
}

func missingReportPeriods(start, end string, reused []reportPeriodDigest) ([]reportPeriod, error) {
	startDate, endDate, err := parseInclusiveReportDates(start, end)
	if err != nil {
		return nil, err
	}
	covered := make(map[string]bool)
	for _, digest := range reused {
		digestStart, digestEnd, parseErr := parseInclusiveReportDates(digest.Start, digest.End)
		if parseErr != nil {
			continue
		}
		for day := digestStart; !day.After(digestEnd); day = day.AddDate(0, 0, 1) {
			covered[day.Format("2006-01-02")] = true
		}
	}
	var periods []reportPeriod
	var open time.Time
	for day := startDate; !day.After(endDate); day = day.AddDate(0, 0, 1) {
		isCovered := covered[day.Format("2006-01-02")]
		if !isCovered && open.IsZero() {
			open = day
		}
		if !open.IsZero() && (isCovered || day.Equal(endDate)) {
			closeDay := day.AddDate(0, 0, -1)
			if !isCovered && day.Equal(endDate) {
				closeDay = day
			}
			periods = append(periods, splitReportPeriodIntoWeeks(open, closeDay)...)
			open = time.Time{}
		}
	}
	return periods, nil
}

func subsetReportCorpus(full *reportCorpus, start, end time.Time) reportCorpus {
	primary := make(map[string]bool)
	contextDocuments := make(map[string]bool)
	connectors := make(map[string]bool)
	documentBySource := make(map[string]reportDocument, len(full.Documents))
	for _, document := range full.Documents {
		documentBySource[document.SourceID] = document
		if !document.ContextOnly && !document.ActivityAt.Before(start) && document.ActivityAt.Before(end) {
			primary[document.SourceID] = true
		}
	}
	connectorSet := make(map[string]bool, len(full.Connectors))
	for _, connector := range full.Connectors {
		connectorSet[connector.ID] = true
	}
	frontier := make(map[string]bool, len(primary))
	for id := range primary {
		frontier[id] = true
	}
	for depth := 0; depth < 2 && len(frontier) > 0; depth++ {
		next := make(map[string]bool)
		for _, edge := range full.Edges {
			for _, pair := range [][2]string{{edge.From, edge.To}, {edge.To, edge.From}} {
				if !frontier[pair[0]] {
					continue
				}
				if connectorSet[pair[1]] {
					if !connectors[pair[1]] {
						connectors[pair[1]] = true
						next[pair[1]] = true
					}
					continue
				}
				if document, ok := documentBySource[pair[1]]; ok && document.ContextOnly {
					contextDocuments[pair[1]] = true
				}
			}
		}
		frontier = next
	}

	included := make(map[string]bool, len(primary)+len(contextDocuments)+len(connectors))
	for id := range primary {
		included[id] = true
	}
	for id := range contextDocuments {
		included[id] = true
	}
	for id := range connectors {
		included[id] = true
	}
	edges := make([]wiki.Edge, 0)
	for _, edge := range full.Edges {
		if included[edge.From] && included[edge.To] {
			edges = append(edges, edge)
		}
	}

	corpus := reportCorpus{
		Start: start, End: end, Workspace: full.Workspace,
		Edges:    edges,
		Counts:   documentReportCounts{Types: make(map[string]int)},
		Coverage: reportCoverage{FailedWorkspaces: append([]string(nil), full.Coverage.FailedWorkspaces...)},
	}
	workspaceSet := make(map[string]struct{})
	for _, document := range full.Documents {
		if !primary[document.SourceID] && !contextDocuments[document.SourceID] {
			continue
		}
		copyDocument := document
		copyDocument.EvidenceID = fmt.Sprintf("D%d", len(corpus.Documents)+1)
		copyDocument.ContextOnly = contextDocuments[document.SourceID]
		copyDocument.RelationIDs = snapshotRelationIDs(copyDocument.SourceID, edges)
		corpus.Documents = append(corpus.Documents, copyDocument)
		corpus.Coverage.ReadableDocuments++
		if copyDocument.Metadata.Truncated {
			corpus.Coverage.TruncatedDocuments++
		}
		if len(copyDocument.Vector) == 0 {
			corpus.Coverage.MissingEmbeddings++
		}
		if copyDocument.ContextOnly {
			corpus.Coverage.ContextDocuments++
			continue
		}
		corpus.Coverage.SourceDocuments++
		corpus.Counts.Documents++
		corpus.Counts.Types[string(copyDocument.Type)]++
		if copyDocument.Type == wiki.TypeReport {
			corpus.Counts.Reports++
		}
		workspaceSet[copyDocument.Workspace] = struct{}{}
	}
	corpus.Counts.Workspaces = len(workspaceSet)
	for _, connector := range full.Connectors {
		if connectors[connector.ID] {
			corpus.Connectors = append(corpus.Connectors, connector)
		}
	}
	return corpus
}

func canonicalizeMonthlySources(sources []reportPeriodDigest, full *reportCorpus) (reportCorpus, []monthlySourceTheme) {
	type documentEntry struct {
		Key      string
		Document reportDigestDocument
	}
	byKey := make(map[string]reportDigestDocument)
	for _, source := range sources {
		for _, document := range source.Documents {
			key := document.SourceID + "\x00" + document.ContentHash
			if _, exists := byKey[key]; !exists {
				byKey[key] = document
			}
		}
	}
	entries := make([]documentEntry, 0, len(byKey))
	for key, document := range byKey {
		entries = append(entries, documentEntry{Key: key, Document: document})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	canonicalID := make(map[string]string, len(entries))
	corpus := reportCorpus{
		Start: full.Start, End: full.End, Workspace: full.Workspace,
		Counts:   documentReportCounts{Types: make(map[string]int)},
		Coverage: reportCoverage{FailedWorkspaces: append([]string(nil), full.Coverage.FailedWorkspaces...), SkippedDocuments: append([]reportSkippedDocument(nil), full.Coverage.SkippedDocuments...)},
	}
	workspaceSet := make(map[string]struct{})
	for index, entry := range entries {
		document := entry.Document
		evidenceID := fmt.Sprintf("D%d", index+1)
		canonicalID[entry.Key] = evidenceID
		activityAt, _ := time.Parse(time.RFC3339, document.ActivityAt)
		if activityAt.IsZero() && len(document.ActivityAt) >= 10 {
			activityAt, _ = time.ParseInLocation("2006-01-02", document.ActivityAt[:10], time.Local)
		}
		corpus.Documents = append(corpus.Documents, reportDocument{
			EvidenceID:   evidenceID,
			SourceID:     document.SourceID,
			Path:         document.Path,
			Title:        document.Title,
			Type:         document.Type,
			Workspace:    document.Workspace,
			ActivityAt:   activityAt,
			ContentHash:  document.ContentHash,
			ContextOnly:  document.ContextOnly,
			Metadata:     document.Metadata,
			RelationIDs:  append([]string(nil), document.RelationIDs...),
			SemanticText: strings.Join(append(append([]string(nil), document.Metadata.Headings...), document.Metadata.KeyParagraphs...), "\n"),
		})
		corpus.Coverage.ReadableDocuments++
		if document.Metadata.Truncated {
			corpus.Coverage.TruncatedDocuments++
		}
		if document.ContextOnly {
			corpus.Coverage.ContextDocuments++
			continue
		}
		corpus.Coverage.SourceDocuments++
		corpus.Counts.Documents++
		corpus.Counts.Types[string(document.Type)]++
		if document.Type == wiki.TypeReport {
			corpus.Counts.Reports++
		}
		workspaceSet[document.Workspace] = struct{}{}
	}
	corpus.Counts.Workspaces = len(workspaceSet)
	for _, source := range sources {
		corpus.Counts.WorkItems += source.Counts.WorkItems
	}

	var sourceThemes []monthlySourceTheme
	for _, source := range sources {
		localMap := make(map[string]string, len(source.Documents))
		for _, document := range source.Documents {
			localMap[document.EvidenceID] = canonicalID[document.SourceID+"\x00"+document.ContentHash]
		}
		for _, theme := range source.Themes {
			remapped := theme
			// Clone Claims so we don't mutate the source digest's backing array.
			remapped.Claims = make([]reportEvidenceClaim, len(theme.Claims))
			copy(remapped.Claims, theme.Claims)
			remapped.EvidenceIDs = remapReportEvidenceIDs(theme.EvidenceIDs, localMap)
			remapped.ContextEvidenceIDs = remapReportEvidenceIDs(theme.ContextEvidenceIDs, localMap)
			remapped.RepresentativeIDs = remapReportEvidenceIDs(theme.RepresentativeIDs, localMap)
			remapped.Summary.EvidenceIDs = remapReportEvidenceIDs(theme.Summary.EvidenceIDs, localMap)
			remapped.Summary.Text = remapReportEvidenceMentions(remapped.Summary.Text, localMap)
			for index := range remapped.Claims {
				remapped.Claims[index].EvidenceIDs = remapReportEvidenceIDs(remapped.Claims[index].EvidenceIDs, localMap)
				remapped.Claims[index].Text = remapReportEvidenceMentions(remapped.Claims[index].Text, localMap)
			}
			sourceThemes = append(sourceThemes, monthlySourceTheme{DigestID: source.DigestID, PeriodStart: source.Start, PeriodEnd: source.End, Theme: remapped})
		}
	}
	return corpus, sourceThemes
}

func remapReportEvidenceIDs(ids []string, mapping map[string]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		mapped := mapping[id]
		if mapped == "" {
			continue
		}
		if _, duplicate := seen[mapped]; duplicate {
			continue
		}
		seen[mapped] = struct{}{}
		out = append(out, mapped)
	}
	sort.Slice(out, func(i, j int) bool { return reportEvidenceNumber(out[i]) < reportEvidenceNumber(out[j]) })
	return out
}

// remapReportEvidenceMentions rewrites standalone D<digits> tokens in prose
// using the same mapping used for ID lists. Tokens that cannot be mapped are
// removed (with at most one adjacent space) rather than left stale, because a
// wrong ID in prose is worse than losing a redundant one — the bracket citation
// is the machine-checked truth.
func remapReportEvidenceMentions(text string, mapping map[string]string) string {
	spans := reportEvidenceMentionRE.FindAllStringIndex(text, -1)
	if len(spans) == 0 {
		return text
	}
	out := make([]byte, 0, len(text))
	cursor := 0
	for _, span := range spans {
		out = append(out, text[cursor:span[0]]...)
		cursor = span[1]
		if mapped, ok := mapping[text[span[0]:span[1]]]; ok {
			out = append(out, mapped...)
			continue
		}
		// An unmappable token is dropped rather than left stale: the bracket
		// citation is the machine-checked truth, so a wrong ID in prose is worse
		// than a missing one. Absorb one adjacent space - the one before the token
		// if there is one, otherwise the one after - so the sentence keeps no gap.
		if trimmed := len(out) - 1; trimmed >= 0 && out[trimmed] == ' ' {
			out = out[:trimmed]
		} else if cursor < len(text) && text[cursor] == ' ' {
			cursor++
		}
	}
	out = append(out, text[cursor:]...)
	return strings.TrimSpace(string(out))
}

func buildMonthlyThemeCorpus(sourceThemes []monthlySourceTheme, canonical *reportCorpus, start, end, workspace string) (reportCorpus, []monthlySourceTheme) {
	startDate, _ := time.ParseInLocation("2006-01-02", start, time.Local)
	endDate, _ := time.ParseInLocation("2006-01-02", end, time.Local)
	corpus := reportCorpus{
		Start: startDate, End: endDate.AddDate(0, 0, 1), Workspace: workspace,
		Counts: documentReportCounts{Types: make(map[string]int)},
	}
	ordered := append([]monthlySourceTheme(nil), sourceThemes...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].PeriodStart != ordered[j].PeriodStart {
			return ordered[i].PeriodStart < ordered[j].PeriodStart
		}
		if ordered[i].DigestID != ordered[j].DigestID {
			return ordered[i].DigestID < ordered[j].DigestID
		}
		return ordered[i].Theme.ID < ordered[j].Theme.ID
	})
	documentWorkspace := make(map[string]string, len(canonical.Documents))
	for _, document := range canonical.Documents {
		documentWorkspace[document.EvidenceID] = document.Workspace
	}
	components := make([]wiki.Component, 0, len(ordered))
	for index, source := range ordered {
		text := monthlySourceThemeText(source)
		hash := sha256.Sum256([]byte(text))
		activityAt, _ := time.ParseInLocation("2006-01-02", source.PeriodEnd, time.Local)
		sourceID := source.DigestID + "/" + source.Theme.ID
		corpus.Documents = append(corpus.Documents, reportDocument{
			EvidenceID:   fmt.Sprintf("C%d", index+1),
			SourceID:     sourceID,
			Title:        source.Theme.Label,
			Type:         wiki.TypeReport,
			Workspace:    monthlyThemeWorkspace(source.Theme, documentWorkspace),
			ActivityAt:   activityAt,
			ContentHash:  hex.EncodeToString(hash[:]),
			SemanticText: text,
		})
		components = append(components, wiki.Component{ID: sourceID, Title: text, Type: wiki.TypeReport, Workspace: workspace})
	}
	if len(components) > 0 {
		vectors, err := reportEmbedClusterDigests(components)
		if err == nil {
			for index := range corpus.Documents {
				corpus.Documents[index].Vector = append([]float32(nil), vectors[corpus.Documents[index].SourceID]...)
			}
		}
	}
	for _, document := range corpus.Documents {
		corpus.Coverage.SourceDocuments++
		corpus.Coverage.ReadableDocuments++
		corpus.Counts.Documents++
		corpus.Counts.Reports++
		corpus.Counts.Types[string(wiki.TypeReport)]++
		if len(document.Vector) == 0 {
			corpus.Coverage.MissingEmbeddings++
		}
	}
	if len(corpus.Documents) > 0 {
		corpus.Counts.Workspaces = 1
	}
	return corpus, ordered
}

func monthlySourceThemeText(source monthlySourceTheme) string {
	var text strings.Builder
	fmt.Fprintf(&text, "%s\n%s\n%s %s\n", source.PeriodStart, source.PeriodEnd, source.Theme.Label, source.Theme.Summary.Text)
	for _, claim := range source.Theme.Claims {
		fmt.Fprintf(&text, "%s: %s\n", claim.Kind, claim.Text)
	}
	return text.String()
}

func summarizeMonthlyThemes(ctx context.Context, canonical *reportCorpus, macroCorpus *reportCorpus, sources []monthlySourceTheme, themes []reportTheme, pcfg chat.ProviderConfig, p provider.Provider) ([]reportThemeDigest, error) {
	if len(themes) == 0 {
		return []reportThemeDigest{}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]reportThemeDigest, len(themes))
	errs := make([]error, len(themes))
	semaphore := make(chan struct{}, reportClusterConcurrency)
	var wait sync.WaitGroup
	for index := range themes {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				errs[index] = ctx.Err()
				return
			}
			result, err := summarizeMonthlyTheme(ctx, canonical, macroCorpus, sources, themes[index], pcfg, p)
			if err != nil {
				errs[index] = err
				cancel()
				return
			}
			results[index] = result
		}()
	}
	wait.Wait()
	for index, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("月报主题 %s 归并失败: %w", themes[index].ID, err)
		}
	}
	return results, nil
}

func summarizeMonthlyTheme(ctx context.Context, canonical, macroCorpus *reportCorpus, sources []monthlySourceTheme, theme reportTheme, pcfg chat.ProviderConfig, p provider.Provider) (reportThemeDigest, error) {
	selectedSources := make([]monthlyMacroSource, 0, len(theme.DocumentIndexes))
	allowedIDs := make(map[string]struct{})
	for _, index := range theme.DocumentIndexes {
		source := sources[index]
		selectedSources = append(selectedSources, monthlyMacroSource{
			PeriodStart: source.PeriodStart,
			PeriodEnd:   source.PeriodEnd,
			Label:       source.Theme.Label,
			Summary:     source.Theme.Summary,
			Claims:      source.Theme.Claims,
		})
		for _, id := range append(append([]string(nil), source.Theme.EvidenceIDs...), source.Theme.ContextEvidenceIDs...) {
			allowedIDs[id] = struct{}{}
		}
	}
	canonicalIndex := make(map[string]int, len(canonical.Documents))
	for index, document := range canonical.Documents {
		canonicalIndex[document.EvidenceID] = index
	}
	ids := make([]string, 0, len(allowedIDs))
	for id := range allowedIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return reportEvidenceNumber(ids[i]) < reportEvidenceNumber(ids[j]) })
	// Carry the effort axis into validation so the monthly digest keeps it: the
	// section's weight is what justifies its place, and validateReportThemeResponse
	// also keeps a session's own title instead of letting the model rename it.
	synthetic := reportTheme{
		ID: theme.ID, Label: theme.Label, Independent: theme.Independent,
		SessionID: theme.SessionID, SessionPath: theme.SessionPath,
		Effort: theme.Effort, Unattributed: theme.Unattributed,
	}
	promptDocuments := make([]reportDigestDocument, 0, len(ids))
	for _, id := range ids {
		index, ok := canonicalIndex[id]
		if !ok {
			continue
		}
		synthetic.DocumentIndexes = append(synthetic.DocumentIndexes, index)
		document := canonical.Documents[index]
		if document.ContextOnly {
			synthetic.ContextEvidenceIDs = append(synthetic.ContextEvidenceIDs, id)
		} else {
			synthetic.EvidenceIDs = append(synthetic.EvidenceIDs, id)
		}
		promptDocuments = append(promptDocuments, reportDigestDocument{
			EvidenceID:  id,
			SourceID:    document.SourceID,
			Path:        document.Path,
			Title:       document.Title,
			Type:        document.Type,
			Workspace:   document.Workspace,
			ActivityAt:  document.ActivityAt.Format(time.RFC3339),
			ContentHash: document.ContentHash,
			ContextOnly: document.ContextOnly,
			Metadata:    document.Metadata,
		})
	}
	if len(synthetic.EvidenceIDs) > 3 {
		synthetic.RepresentativeIDs = append([]string(nil), synthetic.EvidenceIDs[:3]...)
	} else {
		synthetic.RepresentativeIDs = append([]string(nil), synthetic.EvidenceIDs...)
	}
	payload := monthlyMacroPrompt{
		Start:          canonical.Start.Format("2006-01-02"),
		End:            canonical.End.AddDate(0, 0, -1).Format("2006-01-02"),
		Workspace:      canonical.Workspace,
		ThemeID:        theme.ID,
		SuggestedLabel: theme.Label,
		Independent:    theme.Independent,
		Sources:        selectedSources,
		Documents:      promptDocuments,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return reportThemeDigest{}, err
	}
	var digest reportThemeDigest
	err = requestStructuredReportJSON(ctx, p, pcfg, monthlyThemeSystemPrompt(), string(encoded), func(raw string) error {
		var response reportThemeResponse
		if err := decodeStrictReportJSON(strings.TrimSpace(raw), &response); err != nil {
			return err
		}
		validated, err := validateReportThemeResponse(response, synthetic, canonical)
		if err != nil {
			return err
		}
		digest = validated
		return nil
	})
	if err != nil {
		return reportThemeDigest{}, err
	}
	return digest, nil
}

func monthlyThemeSystemPrompt() string {
	return `你是工程月报的分层归并器。输入是多个已由原始文档证据验证的周级 ClusterDigest。只输出一个严格 JSON 对象，不得输出 Markdown 代码块或对象之外的文字。

输出 schema：
{
  "label": "不超过 30 字的月度主题名",
  "summary": {"text": "1-3 句月度演进摘要", "evidenceIds": ["D1"]},
  "claims": [
    {"kind": "outcome|decision|progress|risk|next|background", "text": "合并去重后的单一事实", "evidenceIds": ["D1"]}
  ]
}

硬性规则：
1. 只能重组 weeklyClusterDigests 中已有事实；每条内容必须引用 evidenceDocuments 中的 ID。
2. 保留跨周演进、结论变化、negative/abandoned 和风险；不得把背景或计划写成已完成成果。
3. contextOnly 文档不能单独支撑 outcome、decision、progress、risk 或 next。
4. kind=next 只能复用输入中已有 next 事实或有明确未完成清单的证据。
5. 禁止编造日期、数量、状态、Git/MR、会议或工时。
6. independent=true 时使用“独立事项”，逐项列出，不虚构共同主线。
7. summary 不超过 200 字；claims 最多 8 条，每条 text 不超过 160 字。
8. evidenceIds 只保留支撑当前事实所需的文档；确保 JSON 完整闭合。
9. summary 与 claims 的正文里禁止出现证据编号（如 D12、D355）：编号只放在 evidenceIds 数组里。
   月报会重新编号文档，正文里写的编号会与括号里的引用自相矛盾；正文要写事实本身，不要写"D80 为……"。`
}

// monthlyJSONFromDigest converts a monthly digest into render-ready JSON.
//
// Every fact appears at most once, achieved by giving each section a disjoint
// source rather than a priority order over one pool. Overview and Mainline are
// deterministic; the effort table and open-work list never see the model.
func monthlyJSONFromDigest(digest reportPeriodDigest) monthlyJSON {
	// Active: prefer Sessions count when tracked, fall back to WorkItems.
	// A session IS the work item in this report's vocabulary; labelling the same
	// number "会话" in one place and "工作项" in another reads like two metrics.
	active := digest.Counts.WorkItems
	if digest.Counts.Sessions > 0 {
		active = digest.Counts.Sessions
	}
	activeLabel := "工作项"

	monthly := monthlyJSON{
		Title:       fmt.Sprintf("工程文档月报（%s ~ %s）", digest.Start, digest.End),
		Total:       digest.Counts.Documents,
		Active:      active,
		Themes:      digest.Counts.Themes,
		Reports:     digest.Counts.Reports,
		ActiveLabel: activeLabel,
	}

	// Overview: deterministic counts only, no model prose.
	monthly.Overview = buildMonthlyOverview(digest)

	// Mainline: top 3 themes by Effort.Events, label + effort only.
	monthly.Mainline = buildMonthlyMainline(digest.Themes)

	// Sections draw from DISJOINT sources instead of competing over one pool.
	// A priority queue over a single claim list starved whatever ran last: with
	// milestones and theme cards taking everything, 重点主题 and 关键成果 rendered
	// as headings with nothing under them. Splitting the source guarantees that
	// each section has content and that no claim is printed twice.
	//
	//   theme cards  -> every theme's summary (never a claim)
	//   focus        -> the top two work items' claims
	//   milestones   -> the remaining themes' dated claims, spread over the period
	// A shared text filter runs on top of the disjoint sources. It catches the one
	// case the split cannot: the model emitting byte-identical text under two
	// different work items. It cannot starve a section the way the old priority
	// queue did, because each section still has its own source of items.
	seen := make(map[string]bool)
	focus := monthlyFocusThemeIDs(digest.Themes, 2)
	monthly.ThemesDetail = buildMonthlyThemesDetail(digest.Themes, seen)
	monthly.FocusProjects = buildMonthlyFocusProjects(digest.Themes, focus, seen)
	monthly.Milestones = buildMonthlyMilestones(digest.Themes, focus, seen)

	// Deterministic sections: effort table and open work.
	monthly.SessionsHTML = buildMonthlySessionsHTML(digest)
	monthly.OpenWorkHTML = buildMonthlyOpenWorkHTML(digest)

	return monthly
}

// claimKey returns the normalized claim text used for cross-section dedup.
func claimKey(claim reportEvidenceClaim) string { return strings.TrimSpace(claim.Text) }

// citeClaim returns claim text with its evidence citation appended.
func citeClaim(claim reportEvidenceClaim) string {
	return claim.Text + " " + reportClaimCitation(claim.EvidenceIDs)
}

// buildMonthlyOverview composes a deterministic overview from counts only;
// no theme prose enters this section.
func buildMonthlyOverview(digest reportPeriodDigest) string {
	// Deterministic facts only. This used to be the first three themes' summaries
	// joined by a semicolon, which produced one 600-character run-on sentence that
	// then rendered a second time as the mainline.
	var parts []string
	parts = append(parts, fmt.Sprintf("本月纳入 %d 份活动文档，覆盖 %d 个 Workspace；其中报告类文档 %d 份。",
		digest.Counts.Documents, digest.Counts.Workspaces, digest.Counts.Reports))
	if len(digest.Sessions) > 0 {
		events := 0
		for _, session := range digest.Sessions {
			events += session.Events
		}
		days := reportActiveDayCount(digest.Sessions)
		// Same vocabulary as the weekly report: a session is a work item, and the
		// count is already the work-item count, so it is not repeated separately.
		parts = append(parts, fmt.Sprintf("本月有 %d 个工作项在推进，累计 %d 次记录事件，覆盖 %d 个活跃日。",
			len(digest.Sessions), events, days))
	} else if digest.Counts.WorkItems > 0 {
		parts = append(parts, fmt.Sprintf("本月归并出 %d 个工作项。", digest.Counts.WorkItems))
	}
	if digest.Counts.BulkImportDocuments > 0 {
		share := 0
		if digest.Counts.Documents > 0 {
			share = digest.Counts.BulkImportDocuments * 100 / digest.Counts.Documents
		}
		imported := 0
		for _, group := range digest.BulkImports {
			if group.Kind != "churn" {
				imported += group.Count
			}
		}
		// Two different stories share this bucket: a tree that landed in one day,
		// and documents that merely changed with no session record. Calling the
		// second an "import" would be wrong.
		sentence := fmt.Sprintf("其中 %d 份文档（占 %d%%）无会话产出记录，已单列计数而不计入工作项",
			digest.Counts.BulkImportDocuments, share)
		if imported > 0 {
			sentence += fmt.Sprintf("，其中 %d 份为同日批量导入", imported)
		}
		parts = append(parts, sentence+"。")
	}
	return strings.Join(parts, " ")
}

// buildMonthlyMainline returns the top 3 themes by Effort.Events (desc,
// tie-break by label). Only themes with recorded effort are eligible.
// Output is label plus effort summary; no claim text.
func buildMonthlyMainline(themes []reportThemeDigest) string {
	type candidate struct {
		label  string
		events int
		days   int
	}
	var candidates []candidate
	for _, t := range themes {
		if t.Effort.Events > 0 {
			candidates = append(candidates, candidate{t.Label, t.Effort.Events, t.Effort.ActiveDays})
		}
	}
	if len(candidates) == 0 {
		return "暂无投入记录。"
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].events != candidates[j].events {
			return candidates[i].events > candidates[j].events
		}
		return candidates[i].label < candidates[j].label
	})
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}
	var lines []string
	for _, c := range candidates {
		lines = append(lines, fmt.Sprintf("%s（%d天，%d次记录）", c.label, c.days, c.events))
	}
	return strings.Join(lines, "；")
}

// buildMonthlyFocusProjects picks the top 2 themes by Effort.Events (desc,
// tie-break by ID). Unlike the old code, it does NOT rank by document count.
// Points skip claims already rendered earlier.

// buildMonthlySessionsHTML renders the effort table from digest.Sessions,
// ordered by Events desc. At most 12 rows; excess is aggregated into one
// remainder row. Returns empty string when there are no sessions.
func buildMonthlySessionsHTML(digest reportPeriodDigest) string {
	if len(digest.Sessions) == 0 {
		return ""
	}
	sessions := append([]reportDigestSession(nil), digest.Sessions...)
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Events != sessions[j].Events {
			return sessions[i].Events > sessions[j].Events
		}
		return sessions[i].ID < sessions[j].ID
	})
	var b strings.Builder
	b.WriteString(`<h2>投入概览</h2>`)
	b.WriteString(`<table class="effort-table"><thead><tr>`)
	b.WriteString(`<th>工作项</th><th>Workspace</th><th class="n">活跃天</th><th class="n">记录事件</th><th class="n">产出文档</th>`)
	b.WriteString(`</tr></thead><tbody>`)
	limit := 12
	if len(sessions) <= limit {
		for _, s := range sessions {
			writeSessionRow(&b, s, false)
		}
	} else {
		for i := 0; i < limit; i++ {
			writeSessionRow(&b, sessions[i], false)
		}
		// Aggregate remainder.
		var agg reportDigestSession
		agg.Title = fmt.Sprintf("其余 %d 个工作项", len(sessions)-limit)
		for i := limit; i < len(sessions); i++ {
			agg.Events += sessions[i].Events
			agg.ActiveDays += sessions[i].ActiveDays
			agg.Documents += sessions[i].Documents
		}
		writeSessionRow(&b, agg, true)
	}
	b.WriteString(`</tbody></table>`)
	b.WriteString(`<p class="caption">投入口径：会话记录中落在本月的用户轮次与工具调用次数；活跃天为有记录的日期数，非连续天数。</p>`)
	return b.String()
}

func writeSessionRow(b *strings.Builder, s reportDigestSession, agg bool) {
	rowClass := ""
	if agg {
		rowClass = ` class="agg"`
	}
	fmt.Fprintf(b, `<tr%s><td>%s</td><td>%s</td><td class="n">%d</td><td class="n">%d</td><td class="n">%d</td></tr>`,
		rowClass,
		html.EscapeString(reportSessionTitle(s)),
		html.EscapeString(s.Workspace),
		s.ActiveDays, s.Events, s.Documents)
}

// buildMonthlyOpenWorkHTML renders blocked todos, then open todos, then bulk
// import lines. Returns empty string when there are no todos and no imports.
func buildMonthlyOpenWorkHTML(digest reportPeriodDigest) string {
	// Same grouping as the weekly report, so one reason blocking three tasks reads
	// as one line, and a task keeps the name of the work item that recorded it.
	groups := groupReportOpenWork(digest.Sessions, 10)
	if len(groups) == 0 && len(digest.BulkImports) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<h2>未完成与批量导入</h2>`)
	if len(groups) > 0 {
		out.WriteString(`<table class="open-table"><tbody>`)
		for _, group := range groups {
			tasks := html.EscapeString(strings.Join(group.Tasks, "；"))
			if group.Blocker == "" {
				fmt.Fprintf(&out, `<tr><td><span class="who">%s</span>%s</td></tr>`,
					html.EscapeString(group.Session), tasks)
				continue
			}
			fmt.Fprintf(&out, `<tr><td><span class="who">%s</span>%s <span class="blocker">阻塞原因：%s</span></td></tr>`,
				html.EscapeString(group.Session), tasks, html.EscapeString(group.Blocker))
		}
		out.WriteString(`</tbody></table>`)
		out.WriteString(`<p class="caption">来源：各工作项自身的任务记录，未经模型改写。</p>`)
	}
	for _, group := range digest.BulkImports {
		note := "批量导入，仅计数，不生成结论"
		if group.Kind == "churn" {
			note = "本期有更新但无会话产出记录，仅计数"
		}
		fmt.Fprintf(&out, `<p class="import-row">%s（%s）：%d 份文档，%s。</p>`,
			html.EscapeString(group.Directory), html.EscapeString(group.Date), group.Count, note)
	}
	return out.String()
}
func reportEvidenceNumber(id string) int {
	var number int
	for _, r := range id {
		if r >= '0' && r <= '9' {
			number = number*10 + int(r-'0')
		}
	}
	return number
}

// splitReportPeriodIntoWeeks chunks a gap into week-sized slices.
//
// A gap used to be generated as one period, so a month with no reusable weekly
// digests was processed as a single 31-day window. That changes the report's
// content, not just its packaging: over 31 days the documents no session authored
// pile into one pool far past what a section can narrate, so 490 of 583 documents
// ended up merely counted. Processing the same days as weeks makes a month's
// sections match the weeks it is made of, and makes the output independent of
// which weekly reports happen to exist already.
func splitReportPeriodIntoWeeks(start, end time.Time) []reportPeriod {
	periods := make([]reportPeriod, 0, 5)
	for open := start; !open.After(end); open = open.AddDate(0, 0, 7) {
		closeDay := open.AddDate(0, 0, 6)
		if closeDay.After(end) {
			closeDay = end
		}
		periods = append(periods, reportPeriod{
			Start: open.Format("2006-01-02"),
			End:   closeDay.Format("2006-01-02"),
		})
	}
	return periods
}

// monthlyFocusThemeIDs picks the work items that get a detailed section: the
// heaviest by recorded effort, which is the same ordering the rest of the report
// uses.
// monthlyMilestoneLimit keeps the timeline readable. Entries are sampled across
// the period, so this caps how many days are shown rather than how far into the
// month the list reaches.
const monthlyMilestoneLimit = 12

func monthlyFocusThemeIDs(themes []reportThemeDigest, limit int) map[string]bool {
	sorted := append([]reportThemeDigest(nil), themes...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Effort.Events != sorted[j].Effort.Events {
			return sorted[i].Effort.Events > sorted[j].Effort.Events
		}
		return sorted[i].ID < sorted[j].ID
	})
	picked := make(map[string]bool, limit)
	for _, theme := range sorted {
		if len(picked) >= limit || theme.Effort.Events == 0 {
			break
		}
		picked[theme.ID] = true
	}
	return picked
}

// buildMonthlyThemesDetail renders one card per work item carrying only its
// summary. Cards used to also absorb claims, which is what left the sections below
// them empty.
func buildMonthlyThemesDetail(themes []reportThemeDigest, seen map[string]bool) []struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Items []string `json:"items"`
} {
	detail := make([]struct {
		Name  string   `json:"name"`
		Count int      `json:"count"`
		Items []string `json:"items"`
	}, 0, len(themes))
	for _, theme := range themes {
		if strings.TrimSpace(theme.Summary.Text) == "" || seen[claimKey(theme.Summary)] {
			continue
		}
		seen[claimKey(theme.Summary)] = true
		detail = append(detail, struct {
			Name  string   `json:"name"`
			Count int      `json:"count"`
			Items []string `json:"items"`
		}{Name: theme.Label, Count: len(theme.EvidenceIDs), Items: []string{citeClaim(theme.Summary)}})
	}
	return detail
}

// buildMonthlyFocusProjects details the heaviest work items using their claims -
// the cards above already carry every summary, so repeating one here would be the
// duplication this layout exists to avoid.
func buildMonthlyFocusProjects(themes []reportThemeDigest, focus map[string]bool, seen map[string]bool) []struct {
	Name   string   `json:"name"`
	Points []string `json:"points"`
} {
	projects := make([]struct {
		Name   string   `json:"name"`
		Points []string `json:"points"`
	}, 0, len(focus))
	for _, theme := range themes {
		if !focus[theme.ID] {
			continue
		}
		points := make([]string, 0, 6)
		for _, claim := range theme.Claims {
			if len(points) >= 6 {
				break
			}
			if claim.Kind == "next" || seen[claimKey(claim)] {
				continue
			}
			seen[claimKey(claim)] = true
			points = append(points, citeClaim(claim))
		}
		if len(points) == 0 {
			// A named section for a top work item with nothing under it is the
			// defect this layout was rewritten to remove. If every claim was
			// already printed elsewhere, repeat the first one here: the same fact
			// recorded under two work items is a property of the data, while an
			// empty heading is a rendering bug.
			for _, claim := range theme.Claims {
				if claim.Kind == "next" {
					continue
				}
				points = append(points, citeClaim(claim))
				break
			}
		}
		if len(points) == 0 {
			continue
		}
		projects = append(projects, struct {
			Name   string   `json:"name"`
			Points []string `json:"points"`
		}{Name: theme.Label, Points: points})
	}
	return projects
}

// buildMonthlyMilestones is the month's timeline, drawn from the work items that
// do not get a detailed section.
//
// Entries are sampled ACROSS the period rather than truncated after sorting: a cap
// applied to a date-sorted list showed only 2026-07-02 through 07-13 and hid the
// last eighteen days of the month, even though dated claims ran to 07-31.
func buildMonthlyMilestones(themes []reportThemeDigest, focus map[string]bool, seen map[string]bool) []struct {
	Date string `json:"date"`
	Text string `json:"text"`
} {
	type entry struct {
		Date string `json:"date"`
		Text string `json:"text"`
	}
	byDate := make(map[string][]entry)
	dates := make([]string, 0)
	for _, theme := range themes {
		if focus[theme.ID] {
			continue
		}
		for _, claim := range theme.Claims {
			if claim.Date == "" || claim.Kind == "next" || claim.Kind == "background" {
				continue
			}
			if seen[claimKey(claim)] {
				continue
			}
			seen[claimKey(claim)] = true
			if _, seen := byDate[claim.Date]; !seen {
				dates = append(dates, claim.Date)
			}
			byDate[claim.Date] = append(byDate[claim.Date], entry{Date: claim.Date, Text: citeClaim(claim)})
		}
	}
	sort.Strings(dates)
	for _, date := range dates {
		sort.Slice(byDate[date], func(i, j int) bool { return byDate[date][i].Text < byDate[date][j].Text })
	}
	// One pass per date first, then a second round for dates with more, so every
	// active day appears before any day contributes twice.
	out := make([]entry, 0, monthlyMilestoneLimit)
	for round := 0; len(out) < monthlyMilestoneLimit; round++ {
		added := false
		for _, date := range dates {
			if round >= len(byDate[date]) {
				continue
			}
			out = append(out, byDate[date][round])
			added = true
			if len(out) >= monthlyMilestoneLimit {
				break
			}
		}
		if !added {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Text < out[j].Text
	})
	result := make([]struct {
		Date string `json:"date"`
		Text string `json:"text"`
	}, 0, len(out))
	for _, item := range out {
		result = append(result, struct {
			Date string `json:"date"`
			Text string `json:"text"`
		}{Date: item.Date, Text: item.Text})
	}
	return result
}
