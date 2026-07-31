package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

func generateMonthlyDocumentReport(ctx context.Context, fullCorpus *reportCorpus, reportsDir, start, end string, pcfg chat.ProviderConfig, p provider.Provider) ([]byte, reportPeriodDigest, error) {
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
		themes := clusterReportCorpus(&slice)
		if len(slice.Documents) == 0 {
			continue
		}
		themeDigests, summarizeErr := summarizeReportThemes(ctx, &slice, themes, pcfg, p)
		if summarizeErr != nil {
			return nil, reportPeriodDigest{}, summarizeErr
		}
		digest := newReportPeriodDigest("slice", period.Start, period.End, fullCorpus.Workspace, &slice, themeDigests, p.Name(), pcfg.Model)
		sources = append(sources, digest)
	}

	canonicalCorpus, sourceThemes := canonicalizeMonthlySources(sources, fullCorpus)
	macroCorpus, orderedSources := buildMonthlyThemeCorpus(sourceThemes, start, end, fullCorpus.Workspace)
	macroThemes := clusterReportCorpus(&macroCorpus)
	monthlyThemes, err := summarizeMonthlyThemes(ctx, &canonicalCorpus, &macroCorpus, orderedSources, macroThemes, pcfg, p)
	if err != nil {
		return nil, reportPeriodDigest{}, err
	}
	canonicalCorpus.Counts.Themes = len(monthlyThemes)
	canonicalCorpus.Coverage.ClusteringMode = macroCorpus.Coverage.ClusteringMode
	canonicalCorpus.Coverage.MissingEmbeddings = macroCorpus.Coverage.MissingEmbeddings

	digest := newReportPeriodDigest("monthly", start, end, fullCorpus.Workspace, &canonicalCorpus, monthlyThemes, p.Name(), pcfg.Model)
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
			periods = append(periods, reportPeriod{Start: open.Format("2006-01-02"), End: closeDay.Format("2006-01-02")})
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
			remapped.EvidenceIDs = remapReportEvidenceIDs(theme.EvidenceIDs, localMap)
			remapped.ContextEvidenceIDs = remapReportEvidenceIDs(theme.ContextEvidenceIDs, localMap)
			remapped.RepresentativeIDs = remapReportEvidenceIDs(theme.RepresentativeIDs, localMap)
			remapped.Summary.EvidenceIDs = remapReportEvidenceIDs(theme.Summary.EvidenceIDs, localMap)
			for index := range remapped.Claims {
				remapped.Claims[index].EvidenceIDs = remapReportEvidenceIDs(remapped.Claims[index].EvidenceIDs, localMap)
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

func buildMonthlyThemeCorpus(sourceThemes []monthlySourceTheme, start, end, workspace string) (reportCorpus, []monthlySourceTheme) {
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
			Workspace:    workspace,
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
	synthetic := reportTheme{ID: theme.ID, Label: theme.Label, Independent: theme.Independent}
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
8. evidenceIds 只保留支撑当前事实所需的文档；确保 JSON 完整闭合。`
}

func monthlyJSONFromDigest(digest reportPeriodDigest) monthlyJSON {
	monthly := monthlyJSON{
		Title:   fmt.Sprintf("工程文档月报（%s ~ %s）", digest.Start, digest.End),
		Total:   digest.Counts.Documents,
		Active:  digest.Counts.WorkItems,
		Themes:  digest.Counts.Themes,
		Reports: digest.Counts.Reports,
	}
	var overviewParts []string
	for index, theme := range digest.Themes {
		if index < 3 {
			overviewParts = append(overviewParts, theme.Summary.Text+" "+reportClaimCitation(theme.Summary.EvidenceIDs))
		}
		items := []string{theme.Summary.Text + " " + reportClaimCitation(theme.Summary.EvidenceIDs)}
		for _, claim := range theme.Claims {
			if len(items) >= 6 {
				break
			}
			items = append(items, claim.Text+" "+reportClaimCitation(claim.EvidenceIDs))
		}
		monthly.ThemesDetail = append(monthly.ThemesDetail, struct {
			Name  string   `json:"name"`
			Count int      `json:"count"`
			Items []string `json:"items"`
		}{Name: theme.Label, Count: len(theme.EvidenceIDs), Items: items})
		for _, claim := range theme.Claims {
			if len(monthly.Highlights) < 8 && claim.Kind != "next" && claim.Kind != "background" {
				monthly.Highlights = append(monthly.Highlights, claim.Text+" "+reportClaimCitation(claim.EvidenceIDs))
			}
			if claim.Date != "" && claim.Kind != "next" && claim.Kind != "background" {
				monthly.Milestones = append(monthly.Milestones, struct {
					Date string `json:"date"`
					Text string `json:"text"`
				}{Date: claim.Date, Text: claim.Text + " " + reportClaimCitation(claim.EvidenceIDs)})
			}
		}
	}
	if len(overviewParts) == 0 {
		monthly.Overview = "本时间窗没有可读的活动文档。"
	} else {
		monthly.Overview = strings.Join(overviewParts, "；")
	}
	monthly.Mainline = monthly.Overview
	sort.Slice(monthly.Milestones, func(i, j int) bool {
		if monthly.Milestones[i].Date != monthly.Milestones[j].Date {
			return monthly.Milestones[i].Date < monthly.Milestones[j].Date
		}
		return monthly.Milestones[i].Text < monthly.Milestones[j].Text
	})
	if len(monthly.Milestones) > 9 {
		monthly.Milestones = monthly.Milestones[:9]
	}
	themes := append([]reportThemeDigest(nil), digest.Themes...)
	sort.Slice(themes, func(i, j int) bool {
		if len(themes[i].EvidenceIDs) != len(themes[j].EvidenceIDs) {
			return len(themes[i].EvidenceIDs) > len(themes[j].EvidenceIDs)
		}
		return themes[i].ID < themes[j].ID
	})
	for _, theme := range themes {
		if len(monthly.FocusProjects) >= 2 {
			break
		}
		points := []string{theme.Summary.Text + " " + reportClaimCitation(theme.Summary.EvidenceIDs)}
		for _, claim := range theme.Claims {
			if len(points) >= 6 {
				break
			}
			points = append(points, claim.Text+" "+reportClaimCitation(claim.EvidenceIDs))
		}
		monthly.FocusProjects = append(monthly.FocusProjects, struct {
			Name   string   `json:"name"`
			Points []string `json:"points"`
		}{Name: theme.Label, Points: points})
	}
	return monthly
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
