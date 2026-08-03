package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRemapReportEvidenceMentions_RewritesMappedToken(t *testing.T) {
	mapping := map[string]string{"D1": "D9"}
	got := remapReportEvidenceMentions("D1 记录实验 E01–E03", mapping)
	if !strings.Contains(got, "D9") {
		t.Fatalf("expected D9 in output, got: %q", got)
	}
	if strings.Contains(got, "D1") {
		t.Fatalf("expected D1 replaced, got: %q", got)
	}
}

func TestRemapReportEvidenceMentions_DropsUnmappedTokenWithoutEdgeSpaces(t *testing.T) {
	mapping := map[string]string{} // empty — no token maps
	got := remapReportEvidenceMentions("D99 记录了实验结果", mapping)
	if strings.Contains(got, "D99") {
		t.Fatalf("expected D99 removed, got: %q", got)
	}
	if got != "记录了实验结果" {
		t.Fatalf("expected no leading space, got: %q", got)
	}
}

func TestRemapReportEvidenceMentions_DropsUnmappedTokenWithoutTrailingSpace(t *testing.T) {
	mapping := map[string]string{}
	got := remapReportEvidenceMentions("实验结果 D99", mapping)
	if strings.Contains(got, "D99") {
		t.Fatalf("expected D99 removed, got: %q", got)
	}
	if got != "实验结果" {
		t.Fatalf("expected no trailing space, got: %q", got)
	}
}

func TestRemapReportEvidenceMentions_DropsUnmappedTokenMiddleWithoutDoubleSpace(t *testing.T) {
	mapping := map[string]string{}
	got := remapReportEvidenceMentions("开始 D99 结束", mapping)
	if strings.Contains(got, "D99") {
		t.Fatalf("expected D99 removed, got: %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("expected no double space, got: %q", got)
	}
	if got != "开始 结束" {
		t.Fatalf("expected single space between words, got: %q", got)
	}
}

func TestRemapReportEvidenceMentions_IgnoresNonTokenPatterns(t *testing.T) {
	mapping := map[string]string{}
	input := "D27x 验证 ADD27 模型 2027 年 D 独立"
	got := remapReportEvidenceMentions(input, mapping)
	// None of these are standalone D<digits> tokens, so input should be unchanged
	if got != input {
		t.Fatalf("expected unchanged, got: %q", got)
	}
}

func TestRemapReportEvidenceMentions_HandlesMultipleTokens(t *testing.T) {
	mapping := map[string]string{"D1": "D9", "D7": "D99"}
	got := remapReportEvidenceMentions("D1 和 D7 以及 D3 都处理", mapping)
	if !strings.Contains(got, "D9") {
		t.Fatalf("expected D9, got: %q", got)
	}
	if !strings.Contains(got, "D99") {
		t.Fatalf("expected D99, got: %q", got)
	}
	if strings.Contains(got, "D1") || strings.Contains(got, "D7") {
		t.Fatalf("expected old IDs replaced, got: %q", got)
	}
	if strings.Contains(got, "D3") {
		t.Fatalf("expected unmapped D3 removed, got: %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("expected no double space, got: %q", got)
	}
}

func TestCanonicalizeMonthlySources_RemapsClaimTextAndPreservesSource(t *testing.T) {
	// Eight dummy documents with keys that sort before the real one, placed in a
	// separate source. The real source's document (EvidenceID D1) then becomes
	// canonical D9, triggering the prose remap D1→D9.
	var dummyDocs []reportDigestDocument
	for i := range 8 {
		dummyDocs = append(dummyDocs, reportDigestDocument{
			EvidenceID:  fmt.Sprintf("D%d", i+1),
			SourceID:    fmt.Sprintf("/docs/0%d.md", i),
			Path:        fmt.Sprintf("/docs/0%d.md", i),
			Title:       fmt.Sprintf("Dummy %d", i),
			Type:        "design",
			Workspace:   "miao",
			ActivityAt:  "2026-06-01T00:00:00Z",
			ContentHash: fmt.Sprintf("dummy%d", i),
		})
	}
	dummySource := reportPeriodDigest{
		DigestID:  "w0",
		Start:     "2026-06-01",
		End:       "2026-06-07",
		Documents: dummyDocs,
		Counts:    documentReportCounts{WorkItems: 1},
	}

	realDoc := reportDigestDocument{
		EvidenceID:  "D1",
		SourceID:    "/docs/a.md",
		Path:        "/docs/a.md",
		Title:       "A",
		Type:        "design",
		Workspace:   "miao",
		ActivityAt:  "2026-06-03T00:00:00Z",
		ContentHash: "abc123",
	}
	theme := reportThemeDigest{
		ID:    "T1",
		Label: "Test Theme",
		Summary: reportEvidenceClaim{
			Kind:        "outcome",
			Text:        "D1 记录软件侧已完成的实验",
			EvidenceIDs: []string{"D1"},
		},
		Claims: []reportEvidenceClaim{
			{Kind: "progress", Text: "基于 D1 验证完成", EvidenceIDs: []string{"D1"}},
		},
		EvidenceIDs:       []string{"D1"},
		RepresentativeIDs: []string{"D1"},
	}
	realSource := reportPeriodDigest{
		DigestID:  "w1",
		Start:     "2026-06-08",
		End:       "2026-06-14",
		Documents: []reportDigestDocument{realDoc},
		Themes:    []reportThemeDigest{theme},
		Counts:    documentReportCounts{WorkItems: 1},
	}

	full := reportCorpus{
		Start: time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local),
		End:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local),
	}
	_, remapped := canonicalizeMonthlySources([]reportPeriodDigest{dummySource, realSource}, &full)
	if len(remapped) != 1 {
		t.Fatalf("expected 1 remapped theme (dummy source had none), got %d", len(remapped))
	}
	realRemapped := remapped[0]
	remappedTheme := realRemapped.Theme

	// The remapped theme should have D9 in prose, not D1.
	if strings.Contains(remappedTheme.Summary.Text, "D1") {
		t.Fatalf("remapped summary still contains D1: %q", remappedTheme.Summary.Text)
	}
	if !strings.Contains(remappedTheme.Summary.Text, "D9") {
		t.Fatalf("remapped summary missing D9: %q", remappedTheme.Summary.Text)
	}
	if len(remappedTheme.Claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(remappedTheme.Claims))
	}
	if strings.Contains(remappedTheme.Claims[0].Text, "D1") {
		t.Fatalf("remapped claim still contains D1: %q", remappedTheme.Claims[0].Text)
	}
	if !strings.Contains(remappedTheme.Claims[0].Text, "D9") {
		t.Fatalf("remapped claim missing D9: %q", remappedTheme.Claims[0].Text)
	}
	// The source digest's claim text must be unchanged (proving the slice clone).
	if theme.Claims[0].Text != "基于 D1 验证完成" {
		t.Fatalf("source claim text was mutated: %q", theme.Claims[0].Text)
	}
	if theme.Summary.Text != "D1 记录软件侧已完成的实验" {
		t.Fatalf("source summary text was mutated: %q", theme.Summary.Text)
	}
	// Source EvidenceIDs must also be unchanged (proving the ID remap doesn't mutate source).
	if len(theme.Claims[0].EvidenceIDs) != 1 || theme.Claims[0].EvidenceIDs[0] != "D1" {
		t.Fatalf("source claim EvidenceIDs mutated: %v", theme.Claims[0].EvidenceIDs)
	}
}
