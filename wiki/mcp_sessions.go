package wiki

import (
	"fmt"
	"strings"
)

// mcpContextLimit / mcpSessionsLimit bound what one tool call can return, so a
// packet stays small enough to paste into an agent's context.
const (
	mcpContextLimitDefault  = 5
	mcpContextLimitMax      = 20
	mcpSessionsLimitDefault = 10
	mcpSessionsLimitMax     = 50
)

// mcpWikiContext is the single-entry recall tool: documents plus the sessions
// that worked on them plus matching agent-memory artifacts, rendered as
// Markdown the caller can act on without a second round trip.
func (a *API) mcpWikiContext(args map[string]any) mcpToolResult {
	query, ok := normalizeContextQuery(stringArg(args, "query"))
	if !ok {
		return mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("query is required and must be at most %d bytes", contextQueryMaxBytes)}},
			IsError: true,
		}
	}
	limit := intArg(args, "limit", mcpContextLimitDefault, mcpContextLimitMax)
	packet := a.BuildContextPacket(query, a.embedQuery(query), limit)
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: MarkdownContextPacket(packet)}}}
}

// mcpWikiSessions lists session digests. It never returns transcript content.
func (a *API) mcpWikiSessions(args map[string]any) mcpToolResult {
	workspace := strings.TrimSpace(stringArg(args, "workspace"))
	limit := intArg(args, "limit", mcpSessionsLimitDefault, mcpSessionsLimitMax)

	summaries := a.sessionSummaries()
	var sb strings.Builder
	shown := 0
	for _, summary := range summaries {
		if workspace != "" && summary.Workspace != workspace {
			continue
		}
		if shown >= limit {
			break
		}
		shown++
		fmt.Fprintf(&sb, "## %s\n", summary.Title)
		fmt.Fprintf(&sb, "- workspace: %s\n- updated: %s\n- user turns: %d\n",
			summary.Workspace, summary.UpdatedAt.Format("2006-01-02 15:04"), summary.UserTurns)
		if tools := formatToolCalls(summary.ToolCalls); tools != "" {
			fmt.Fprintf(&sb, "- tools: %s\n", tools)
		}
		if len(summary.Writes) > 0 {
			fmt.Fprintf(&sb, "- produced: %s\n", strings.Join(summary.Writes, ", "))
		}
		if len(summary.Edits) > 0 {
			fmt.Fprintf(&sb, "- edited: %s\n", strings.Join(summary.Edits, ", "))
		}
		if len(summary.Reads) > 0 {
			fmt.Fprintf(&sb, "- read: %s\n", strings.Join(summary.Reads, ", "))
		}
		if len(summary.Intents) > 0 {
			sample := summary.Intents
			if len(sample) > contextIntentSample {
				sample = sample[:contextIntentSample]
			}
			fmt.Fprintf(&sb, "- intents: %s\n", strings.Join(sample, " · "))
		}
		fmt.Fprintf(&sb, "- session: %s\n\n", summary.Path)
	}
	if shown == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "No indexed agent sessions."}}}
	}
	header := fmt.Sprintf("# Agent sessions (%d shown of %d indexed)\n\n", shown, len(summaries))
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: header + sb.String()}}}
}

// formatToolCalls renders the busiest tools first so the line reads as "what
// this session mostly did".
func formatToolCalls(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	type pair struct {
		name  string
		count int
	}
	pairs := make([]pair, 0, len(counts))
	for name, count := range counts {
		pairs = append(pairs, pair{name, count})
	}
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0; j-- {
			left, right := pairs[j-1], pairs[j]
			if right.count > left.count || (right.count == left.count && right.name < left.name) {
				pairs[j-1], pairs[j] = right, left
				continue
			}
			break
		}
	}
	parts := make([]string, 0, len(pairs))
	for _, entry := range pairs {
		parts = append(parts, fmt.Sprintf("%s×%d", entry.name, entry.count))
	}
	return strings.Join(parts, " ")
}

// intArg reads a JSON number argument and returns a positive value no larger
// than max. A missing, non-numeric or non-positive argument yields fallback,
// which is itself clamped so the result is always in [1, max].
func intArg(args map[string]any, key string, fallback, max int) int {
	if fallback < 1 {
		fallback = 1
	}
	if max < fallback {
		max = fallback
	}
	raw, ok := args[key].(float64)
	if !ok {
		return fallback
	}
	value := int(raw)
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}
