package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"stele/chat"
	"stele/chat/provider"
)

const (
	reportClusterPromptRuneBudget = 32000
	reportClusterConcurrency      = 3
	reportStructuredMaxTokens     = 4096
	reportStructuredAttempts      = 2
)

type reportPromptDocument struct {
	EvidenceID    string   `json:"evidenceId"`
	Title         string   `json:"title"`
	Type          string   `json:"type"`
	Workspace     string   `json:"workspace"`
	ActivityDate  string   `json:"activityDate"`
	ContextOnly   bool     `json:"contextOnly"`
	ChecklistDone int      `json:"checklistDone"`
	ChecklistOpen int      `json:"checklistOpen"`
	Headings      []string `json:"headings,omitempty"`
	Text          string   `json:"text"`
}

type reportPromptClaim struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidenceIds"`
}

type reportPromptSummary struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidenceIds"`
}

type reportThemeResponse struct {
	Label   string              `json:"label"`
	Summary reportPromptSummary `json:"summary"`
	Claims  []reportPromptClaim `json:"claims"`
}

type reportThemePrompt struct {
	Start          string                 `json:"start"`
	End            string                 `json:"end"`
	Workspace      string                 `json:"workspace"`
	ThemeID        string                 `json:"themeId"`
	SuggestedLabel string                 `json:"suggestedLabel"`
	Independent    bool                   `json:"independent"`
	Documents      []reportPromptDocument `json:"documents"`
}

func generateWeeklyDocumentReport(ctx context.Context, corpus *reportCorpus, themes []reportTheme, start, end string, pcfg chat.ProviderConfig, p provider.Provider) ([]byte, reportPeriodDigest, error) {
	themeDigests, err := summarizeReportThemes(ctx, corpus, themes, pcfg, p)
	if err != nil {
		return nil, reportPeriodDigest{}, err
	}
	digest := newReportPeriodDigest("weekly", start, end, corpus.Workspace, corpus, themeDigests, p.Name(), pcfg.Model)
	return renderWeeklyDocumentReport(digest), digest, nil
}

func summarizeReportThemes(ctx context.Context, corpus *reportCorpus, themes []reportTheme, pcfg chat.ProviderConfig, p provider.Provider) ([]reportThemeDigest, error) {
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
			result, err := summarizeReportTheme(ctx, corpus, themes[index], pcfg, p)
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
		if err != nil && !errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("主题 %s 摘要失败: %w", themes[index].ID, err)
		}
	}
	for index, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("主题 %s 摘要失败: %w", themes[index].ID, err)
		}
	}
	return results, nil
}

func summarizeReportTheme(ctx context.Context, corpus *reportCorpus, theme reportTheme, pcfg chat.ProviderConfig, p provider.Provider) (reportThemeDigest, error) {
	prompt := buildReportThemePrompt(corpus, theme)
	payload, err := json.Marshal(prompt)
	if err != nil {
		return reportThemeDigest{}, err
	}
	var digest reportThemeDigest
	err = requestStructuredReportJSON(ctx, p, pcfg, reportThemeSystemPrompt(), string(payload), func(raw string) error {
		var response reportThemeResponse
		if err := decodeStrictReportJSON(strings.TrimSpace(raw), &response); err != nil {
			return err
		}
		validated, err := validateReportThemeResponse(response, theme, corpus)
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

func requestStructuredReportJSON(ctx context.Context, p provider.Provider, pcfg chat.ProviderConfig, systemPrompt, userText string, decode func(string) error) error {
	effective := pcfg
	if strings.TrimSpace(effective.Model) == "" {
		models := p.Models()
		if len(models) > 0 {
			effective.Model = models[0]
		}
	}
	effective.Temperature = 0.1
	effective.Thinking = "disabled"
	if effective.MaxTokens < reportStructuredMaxTokens {
		effective.MaxTokens = reportStructuredMaxTokens
	}

	var lastErr error
	for attempt := range reportStructuredAttempts {
		prompt := systemPrompt
		if attempt > 0 {
			prompt += "\n\n重试要求：上一次响应被截断、不是合法 JSON，或未通过 evidence/next 校验。重新核对每个 evidenceId；没有明确未完成清单或 Next/Follow-up/TODO 原文时不得输出 kind=next。仅返回一个完整、紧凑的 JSON 对象；summary 不超过 160 字；claims 最多 8 条，每条不超过 160 字；必须补齐所有括号和引号。"
		}
		raw, err := chatStreamDrain(ctx, p, effective, prompt, userText)
		if err == nil {
			err = decode(raw)
		}
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fmt.Errorf("模型结构化响应失败（已自动重试 1 次）: %w", lastErr)
}

func reportThemeSystemPrompt() string {
	return `你是工程文档证据归纳器。只输出一个严格 JSON 对象，不得输出 Markdown 代码块或对象之外的文字。

输出 schema：
{
  "label": "不超过 30 字的主题名",
  "summary": {"text": "1-2 句事实摘要", "evidenceIds": ["D1"]},
  "claims": [
    {"kind": "outcome|decision|progress|risk|next|background", "text": "单一可验证事实", "evidenceIds": ["D1"]}
  ]
}

硬性规则：
1. 每个 summary 和 claim 必须引用输入中真实存在的 evidenceId；禁止外部知识、推测和补全。
2. contextOnly 文档只能解释背景，不能单独支撑 outcome、decision、progress、risk 或 next。
3. kind=next 只能来自未完成 checklist，或原文明确的 Next/Follow-up/TODO/下一步/后续/待办。
4. 不得编造 phase、状态、验证结果、日期、Git/MR、会议或工时。
5. independent=true 时，主题名必须是“独立事项”，summary 不得虚构共同主线；逐项写 claims。
6. 合并重复表述，但保留 negative、abandoned、失败或风险结论。
7. summary 不超过 160 字；claims 最多 8 条，每条 text 不超过 160 字。
8. evidenceIds 只保留支撑当前事实所需的文档；确保 JSON 完整闭合。`
}

func buildReportThemePrompt(corpus *reportCorpus, theme reportTheme) reportThemePrompt {
	indexes := append([]int(nil), theme.DocumentIndexes...)
	sort.Ints(indexes)
	perDocumentBudget := reportClusterPromptRuneBudget / maxInt(1, len(indexes))
	if perDocumentBudget > 1600 {
		perDocumentBudget = 1600
	}
	if perDocumentBudget < 160 {
		perDocumentBudget = 160
	}
	documents := make([]reportPromptDocument, 0, len(indexes))
	for _, index := range indexes {
		document := corpus.Documents[index]
		documents = append(documents, reportPromptDocument{
			EvidenceID:    document.EvidenceID,
			Title:         document.Title,
			Type:          string(document.Type),
			Workspace:     document.Workspace,
			ActivityDate:  document.ActivityAt.Format("2006-01-02"),
			ContextOnly:   document.ContextOnly,
			ChecklistDone: document.Metadata.ChecklistDone,
			ChecklistOpen: document.Metadata.ChecklistOpen,
			Headings:      append([]string(nil), document.Metadata.Headings...),
			Text:          truncateReportRunes(document.SemanticText, perDocumentBudget),
		})
	}
	return reportThemePrompt{
		Start:          corpus.Start.Format("2006-01-02"),
		End:            corpus.End.AddDate(0, 0, -1).Format("2006-01-02"),
		Workspace:      corpus.Workspace,
		ThemeID:        theme.ID,
		SuggestedLabel: theme.Label,
		Independent:    theme.Independent,
		Documents:      documents,
	}
}

func validateReportThemeResponse(response reportThemeResponse, theme reportTheme, corpus *reportCorpus) (reportThemeDigest, error) {
	allowed := make(map[string]reportDocument, len(theme.DocumentIndexes))
	order := make(map[string]int, len(theme.DocumentIndexes))
	for _, index := range theme.DocumentIndexes {
		document := corpus.Documents[index]
		allowed[document.EvidenceID] = document
		order[document.EvidenceID] = index
	}
	label := strings.TrimSpace(response.Label)
	if theme.Independent {
		label = "独立事项"
	} else if label == "" {
		label = theme.Label
	}
	label = truncateReportRunes(label, 80)
	if strings.TrimSpace(response.Summary.Text) == "" {
		return reportThemeDigest{}, errors.New("summary.text 为空")
	}
	summaryIDs, err := validateReportEvidenceIDs(response.Summary.EvidenceIDs, allowed, order)
	if err != nil {
		return reportThemeDigest{}, fmt.Errorf("summary: %w", err)
	}
	if !reportEvidenceIncludesPrimary(summaryIDs, allowed) {
		return reportThemeDigest{}, errors.New("summary 仅由 contextOnly 文档支撑")
	}
	digest := reportThemeDigest{
		ID:    theme.ID,
		Label: label,
		Summary: reportEvidenceClaim{
			Kind:        "summary",
			Text:        strings.TrimSpace(response.Summary.Text),
			EvidenceIDs: summaryIDs,
			Date:        reportClaimDate(summaryIDs, allowed),
		},
		EvidenceIDs:        append([]string(nil), theme.EvidenceIDs...),
		ContextEvidenceIDs: append([]string(nil), theme.ContextEvidenceIDs...),
		RepresentativeIDs:  append([]string(nil), theme.RepresentativeIDs...),
		Independent:        theme.Independent,
	}
	allowedKinds := map[string]bool{"outcome": true, "decision": true, "progress": true, "risk": true, "next": true, "background": true}
	for index, claim := range response.Claims {
		claim.Kind = strings.TrimSpace(claim.Kind)
		claim.Text = strings.TrimSpace(claim.Text)
		if !allowedKinds[claim.Kind] {
			return reportThemeDigest{}, fmt.Errorf("claims[%d].kind 无效", index)
		}
		if claim.Text == "" {
			return reportThemeDigest{}, fmt.Errorf("claims[%d].text 为空", index)
		}
		ids, idsErr := validateReportEvidenceIDs(claim.EvidenceIDs, allowed, order)
		if idsErr != nil {
			return reportThemeDigest{}, fmt.Errorf("claims[%d]: %w", index, idsErr)
		}
		if claim.Kind != "background" && !reportEvidenceIncludesPrimary(ids, allowed) {
			return reportThemeDigest{}, fmt.Errorf("claims[%d] 仅由 contextOnly 文档支撑", index)
		}
		if claim.Kind == "next" && !reportEvidenceSupportsNext(ids, allowed) {
			continue
		}
		digest.Claims = append(digest.Claims, reportEvidenceClaim{
			Kind:        claim.Kind,
			Text:        claim.Text,
			EvidenceIDs: ids,
			Date:        reportClaimDate(ids, allowed),
		})
	}
	return digest, nil
}

func validateReportEvidenceIDs(ids []string, allowed map[string]reportDocument, order map[string]int) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if _, ok := allowed[id]; !ok {
			return nil, fmt.Errorf("未知 evidenceId %q", id)
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, errors.New("evidenceIds 为空")
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out, nil
}

func reportEvidenceIncludesPrimary(ids []string, documents map[string]reportDocument) bool {
	for _, id := range ids {
		if !documents[id].ContextOnly {
			return true
		}
	}
	return false
}

func reportEvidenceSupportsNext(ids []string, documents map[string]reportDocument) bool {
	for _, id := range ids {
		document := documents[id]
		if document.ContextOnly {
			continue
		}
		if document.Metadata.ChecklistOpen > 0 {
			return true
		}
		text := strings.ToLower(document.SemanticText)
		for _, marker := range []string{"next", "follow-up", "follow up", "todo", "下一步", "后续", "待办"} {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	return false
}

func reportClaimDate(ids []string, documents map[string]reportDocument) string {
	var latest string
	for _, id := range ids {
		date := documents[id].ActivityAt.Format("2006-01-02")
		if date > latest {
			latest = date
		}
	}
	return latest
}

func decodeStrictReportJSON(raw string, target any) error {
	if raw == "" {
		return errors.New("empty response")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func renderWeeklyDocumentReport(digest reportPeriodDigest) []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "# 本周工作周报（%s ~ %s）\n\n", digest.Start, digest.End)
	scope := digest.Workspace
	if scope == "" {
		scope = "全部 workspace"
	}
	out.WriteString("## 概述\n\n")
	fmt.Fprintf(&out,
		"本周纳入 %d 份活动文档，归并为 %d 个逻辑工作项和 %d 个主题，覆盖 %d 个 Workspace；其中报告类文档 %d 份。\n\n",
		digest.Counts.Documents, digest.Counts.WorkItems, digest.Counts.Themes, digest.Counts.Workspaces, digest.Counts.Reports)
	fmt.Fprintf(&out, "> 统计口径：Wiki 索引中文档的最后更新时间；范围：%s。\n\n", escapeMarkdownCell(scope))

	documentsByID := make(map[string]reportDigestDocument, len(digest.Documents))
	for _, document := range digest.Documents {
		documentsByID[document.EvidenceID] = document
	}
	if len(digest.Themes) == 0 {
		out.WriteString("本时间窗没有可读的活动文档。\n\n")
	}
	out.WriteString("---\n\n")
	for index, theme := range digest.Themes {
		fmt.Fprintf(&out, "## %s、%s\n\n", reportThemeOrdinal(index), theme.Label)
		fmt.Fprintf(&out, "%s %s\n\n", theme.Summary.Text, reportClaimCitation(theme.Summary.EvidenceIDs))

		milestones := make(map[int]struct{})
		for claimIndex, claim := range theme.Claims {
			if len(milestones) >= 3 {
				break
			}
			if claim.Kind != "outcome" && claim.Kind != "decision" {
				continue
			}
			milestones[claimIndex] = struct{}{}
		}
		if len(milestones) > 0 {
			out.WriteString("### 本周里程碑\n\n")
			for claimIndex, claim := range theme.Claims {
				if _, ok := milestones[claimIndex]; !ok {
					continue
				}
				fmt.Fprintf(&out, "- **%s**：%s %s\n", reportClaimKindLabel(claim.Kind), claim.Text, reportClaimCitation(claim.EvidenceIDs))
			}
			out.WriteByte('\n')
		}

		themeDocuments := reportThemeDocuments(theme, documentsByID)
		fmt.Fprintf(&out, "### 完成与推进项（%d 项）\n\n", len(themeDocuments))
		if len(themeDocuments) == 0 {
			out.WriteString("本主题没有独立的活动文档，内容仅用于上下文说明。\n\n")
		} else {
			out.WriteString("| 工作项 | 日期 | 状态 | 要点 | 证据 |\n")
			out.WriteString("|---|---|---|---|---|\n")
			for _, document := range themeDocuments {
				fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n",
					escapeMarkdownCell(document.Title),
					reportDigestDocumentDate(document),
					reportDigestDocumentStatus(document),
					escapeMarkdownCell(reportDocumentKeyPoint(document.EvidenceID, theme)),
					document.EvidenceID)
			}
			out.WriteByte('\n')
		}

		out.WriteString("### 关键成果\n\n")
		keyResultCount := 0
		for claimIndex, claim := range theme.Claims {
			if claim.Kind == "next" {
				continue
			}
			if _, isMilestone := milestones[claimIndex]; isMilestone {
				continue
			}
			keyResultCount++
			fmt.Fprintf(&out, "- **%s**：%s %s\n", reportClaimKindLabel(claim.Kind), claim.Text, reportClaimCitation(claim.EvidenceIDs))
		}
		if keyResultCount == 0 {
			fmt.Fprintf(&out, "- **主题概览**：%s %s\n", theme.Summary.Text, reportClaimCitation(theme.Summary.EvidenceIDs))
		}
		out.WriteByte('\n')

		outputDocuments := reportThemeOutputDocuments(themeDocuments)
		if len(outputDocuments) >= 3 {
			out.WriteString("### 产出清单\n\n")
			for _, document := range outputDocuments {
				fmt.Fprintf(&out, "- %s（%s，%s）\n",
					escapeMarkdownCell(document.Title), document.Type, document.EvidenceID)
			}
			out.WriteByte('\n')
		}
		out.WriteString("---\n\n")
	}

	out.WriteString("## 下周计划\n\n")
	nextCount := 0
	for _, theme := range digest.Themes {
		for _, claim := range theme.Claims {
			if claim.Kind != "next" {
				continue
			}
			nextCount++
			fmt.Fprintf(&out, "%d. **%s**：%s %s\n", nextCount, theme.Label, claim.Text, reportClaimCitation(claim.EvidenceIDs))
		}
	}
	if nextCount == 0 {
		out.WriteString("未发现带明确待办、Next、Follow-up 或未完成清单的证据。\n")
	}

	out.WriteString("\n---\n\n## 附录：文档来源\n\n")
	out.WriteString("| 证据 | 日期 | 类型 | Workspace | 文档 | 口径 |\n")
	out.WriteString("|---|---|---|---|---|---|\n")
	for _, document := range digest.Documents {
		contextLabel := "活动"
		if document.ContextOnly {
			contextLabel = "仅背景"
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s | %s |\n",
			document.EvidenceID,
			reportDigestDocumentDate(document),
			escapeMarkdownCell(string(document.Type)),
			escapeMarkdownCell(document.Workspace),
			escapeMarkdownCell(document.Title),
			contextLabel)
	}
	renderReportCoverage(&out, digest.Coverage)
	return []byte(strings.TrimSpace(out.String()) + "\n")
}

func reportThemeOrdinal(index int) string {
	ordinals := [...]string{"一", "二", "三", "四", "五", "六", "七", "八"}
	if index >= 0 && index < len(ordinals) {
		return ordinals[index]
	}
	return fmt.Sprintf("%d", index+1)
}

func reportThemeDocuments(theme reportThemeDigest, documentsByID map[string]reportDigestDocument) []reportDigestDocument {
	documents := make([]reportDigestDocument, 0, len(theme.EvidenceIDs))
	for _, evidenceID := range theme.EvidenceIDs {
		document, ok := documentsByID[evidenceID]
		if !ok || document.ContextOnly {
			continue
		}
		documents = append(documents, document)
	}
	return documents
}

func reportThemeOutputDocuments(documents []reportDigestDocument) []reportDigestDocument {
	outputs := make([]reportDigestDocument, 0, len(documents))
	for _, document := range documents {
		switch string(document.Type) {
		case "artifact", "report", "design", "spec", "plan":
			outputs = append(outputs, document)
		}
	}
	return outputs
}

func reportDigestDocumentDate(document reportDigestDocument) string {
	if len(document.ActivityAt) >= 10 {
		return document.ActivityAt[:10]
	}
	return document.ActivityAt
}

func reportDigestDocumentStatus(document reportDigestDocument) string {
	if document.Metadata.ChecklistOpen > 0 {
		return "推进中"
	}
	if document.Metadata.ChecklistDone > 0 {
		return "已完成"
	}
	switch string(document.Type) {
	case "artifact", "report":
		return "已产出"
	default:
		return "已更新"
	}
}

func reportDocumentKeyPoint(evidenceID string, theme reportThemeDigest) string {
	for _, claim := range theme.Claims {
		if claim.Kind != "next" && reportClaimHasEvidence(claim, evidenceID) {
			return claim.Text
		}
	}
	return theme.Summary.Text
}

func reportClaimHasEvidence(claim reportEvidenceClaim, evidenceID string) bool {
	for _, id := range claim.EvidenceIDs {
		if id == evidenceID {
			return true
		}
	}
	return false
}

func renderReportCoverage(out *strings.Builder, coverage reportCoverage) {
	if len(coverage.FailedWorkspaces) == 0 && len(coverage.SkippedDocuments) == 0 && coverage.MissingEmbeddings == 0 && coverage.TruncatedDocuments == 0 {
		return
	}
	out.WriteString("\n## 覆盖与降级\n\n")
	fmt.Fprintf(out, "- 聚类模式：%s\n", coverage.ClusteringMode)
	if coverage.MissingEmbeddings > 0 {
		fmt.Fprintf(out, "- %d 份文档缺少向量，已使用确定性词法相似度降级。\n", coverage.MissingEmbeddings)
	}
	if coverage.TruncatedDocuments > 0 {
		fmt.Fprintf(out, "- %d 份文档的证据文本达到单文档预算；标题和章节覆盖仍保留。\n", coverage.TruncatedDocuments)
	}
	if len(coverage.FailedWorkspaces) > 0 {
		fmt.Fprintf(out, "- 索引失败的 workspace：%s。\n", strings.Join(coverage.FailedWorkspaces, ", "))
	}
	for _, skipped := range coverage.SkippedDocuments {
		fmt.Fprintf(out, "- 未读取 `%s`：%s\n", skipped.Path, skipped.Error)
	}
}

func reportClaimKindLabel(kind string) string {
	switch kind {
	case "outcome":
		return "成果"
	case "decision":
		return "决策"
	case "progress":
		return "进展"
	case "risk":
		return "风险"
	case "background":
		return "背景"
	default:
		return "事实"
	}
}

func reportClaimCitation(ids []string) string {
	return "[" + strings.Join(ids, ", ") + "]"
}

func truncateReportRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}

func escapeMarkdownCell(text string) string {
	text = strings.ReplaceAll(text, "|", "\\|")
	text = strings.ReplaceAll(text, "\n", " ")
	return text
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
