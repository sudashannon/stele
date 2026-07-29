package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type LintIssue struct {
	Rule        string `json:"rule"` // orphan | dead-link | duplicate | task-artifact-missing | design-no-plan | stale-active | low-content | placeholder-heavy | missing-sections | low-link-density
	ComponentID string `json:"componentId"`
	Detail      string `json:"detail"`
}

// Lint runs the orphan, dead-link, duplicate-title, and lifecycle-gap
// (design-no-plan, stale-active — see lintLifecycleGaps) checks. Root-level
// "change" components and components under an "/archive/" path segment are
// excluded from orphan detection — change nodes are expected to be hubs with
// only outgoing edges in small workspaces, and archived artifacts are
// expected to be disconnected from the active graph, so flagging either has
// no actionable value for a working dashboard.
func (g *Graph) Lint() []LintIssue {
	var issues []LintIssue

	for id, c := range g.components {
		if c.Type == TypeChange || strings.Contains(c.Path, "/archive/") {
			continue
		}
		if len(g.forward[id]) == 0 && len(g.backward[id]) == 0 {
			issues = append(issues, LintIssue{Rule: "orphan", ComponentID: id, Detail: c.Title})
		}
	}

	for from, edges := range g.forward {
		for _, e := range edges {
			if _, ok := g.components[e.To]; !ok {
				// If the target file exists on disk but just isn't indexed as a
				// Component (e.g. source code .c/.py/.go files referenced from design
				// docs), don't report it as dead — the link is valid, just not a
				// wiki-tracked artifact.
				if _, err := os.Stat(e.To); err == nil {
					continue
				}
				detail := fmt.Sprintf("link to %s has no matching component", e.To)
				if suggestion := suggestArchivedTarget(e.To, g); suggestion != "" {
					detail += "; " + suggestion
				}
				issues = append(issues, LintIssue{
					Rule: "dead-link", ComponentID: from,
					Detail: detail,
				})
			}
		}
	}

	byTitle := map[string][]string{}
	for id, c := range g.components {
		byTitle[c.Title] = append(byTitle[c.Title], id)
	}
	for title, ids := range byTitle {
		if len(ids) > 1 {
			// Skip groups where every component's title is just its filename fallback
			allFallback := true
			for _, id := range ids {
				c := g.components[id]
				if c.Title != strings.TrimSuffix(filepath.Base(c.Path), ".md") {
					allFallback = false
					break
				}
			}
			if allFallback {
				continue
			}
			for _, id := range ids {
				issues = append(issues, LintIssue{Rule: "duplicate", ComponentID: id, Detail: title})
			}
		}
	}

	issues = append(issues, g.lintLowContent()...)
	issues = append(issues, g.lintPlaceholderHeavy()...)
	issues = append(issues, g.lintMissingSections()...)
	issues = append(issues, g.lintLowLinkDensity()...)
	issues = append(issues, g.lintLifecycleGaps()...)
	return issues
}

// frontmatterTime reads a frontmatter value as time.Time. yaml.Unmarshal
// parses bare "YYYY-MM-DD" scalars into time.Time directly when the field
// comes from a real .comet.yaml; test fixtures instead set the field as a
// plain string, so both representations must be accepted.
func frontmatterTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
// ── Content-quality rules ────────────────────────────────────────────
//
var placeholderRE = regexp.MustCompile(`(?i)\b(TODO|TBD|FIXME|HACK|WIP)\b|待定|待补充|待实现|暂未`)
//
func readBody(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(data)
	if strings.HasPrefix(text, "---\n") {
		if idx := strings.Index(text[4:], "\n---\n"); idx >= 0 {
			text = text[4+idx+5:]
		}
	}
	return text
}
//
func stripMarkup(body string) string {
	body = regexp.MustCompile("(?s)```[^`]*```").ReplaceAllString(body, "")
	body = regexp.MustCompile("`[^`]+`").ReplaceAllString(body, "")
	body = regexp.MustCompile(`\[([^\]]*)\]\([^)]+\)`).ReplaceAllString(body, "$1")
	body = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?m)^#{1,6}\s*`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?m)^>\s*`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?m)^\s*[-*+]\s+`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?m)^\s*\d+\.\s+`).ReplaceAllString(body, "")
	body = strings.TrimSpace(body)
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, body)
}
//
func (g *Graph) lintLowContent() []LintIssue {
	var issues []LintIssue
	for id, c := range g.components {
		if strings.Contains(c.Path, "/archive/") {
			continue
		}
		body := readBody(c.Path)
		if body == "" {
			continue
		}
		stripped := stripMarkup(body)
		if len(stripped) < 200 {
			issues = append(issues, LintIssue{
				Rule:        "low-content",
				ComponentID: id,
				Detail:      fmt.Sprintf("body has %d substantive chars (threshold: 200)", len(stripped)),
			})
		}
	}
	return issues
}
//
func (g *Graph) lintPlaceholderHeavy() []LintIssue {
	var issues []LintIssue
	for id, c := range g.components {
		if strings.Contains(c.Path, "/archive/") {
			continue
		}
		body := readBody(c.Path)
		if body == "" {
			continue
		}
		count := len(placeholderRE.FindAllString(body, -1))
		if count == 0 {
			continue
		}
		phase, _ := c.Frontmatter["phase"].(string)
		isDone := phase == "archive" || phase == "verify"
		if isDone {
			issues = append(issues, LintIssue{
				Rule:        "placeholder-heavy",
				ComponentID: id,
				Detail:      fmt.Sprintf("%d placeholder(s) in a completed (%s) document", count, phase),
			})
			continue
		}
		if count > 3 {
			issues = append(issues, LintIssue{
				Rule:        "placeholder-heavy",
				ComponentID: id,
				Detail:      fmt.Sprintf("%d placeholders in active document", count),
			})
		}
	}
	return issues
}
//
var requiredSectionGroups = map[ComponentType][][]string{
	TypeProposal: {
		{"why", "背景", "动机"},
		{"what change", "方案", "改动"},
	},
	TypeDesign: {
		{"goal", "decision", "risk", "目标", "决策", "风险", "trade"},
	},
	TypeReport: {
		{"pass", "fail", "验证", "结论"},
	},
}
//
func (g *Graph) lintMissingSections() []LintIssue {
	var issues []LintIssue
	for id, c := range g.components {
		groups, ok := requiredSectionGroups[c.Type]
		if !ok {
			continue
		}
		if strings.Contains(c.Path, "/archive/") {
			continue
		}
		body := readBody(c.Path)
		if body == "" {
			continue
		}
		bodyLower := strings.ToLower(body)
		var missingGroups []string
		for _, group := range groups {
			found := false
			for _, kw := range group {
				if strings.Contains(bodyLower, strings.ToLower(kw)) {
					found = true
					break
				}
			}
			if !found {
				missingGroups = append(missingGroups, strings.Join(group, "|"))
			}
		}
		if len(missingGroups) > 0 {
			issues = append(issues, LintIssue{
				Rule:        "missing-sections",
				ComponentID: id,
				Detail:      fmt.Sprintf("type %s missing: %s", c.Type, strings.Join(missingGroups, "; ")),
			})
		}
	}
	return issues
}
//
func (g *Graph) lintLowLinkDensity() []LintIssue {
	var issues []LintIssue
	for id, c := range g.components {
		if c.Type == TypeChange || strings.Contains(c.Path, "/archive/") {
			continue
		}
		fwd := len(g.forward[id])
		bwd := len(g.backward[id])
		if fwd == 0 && bwd == 0 {
			continue // caught by orphan
		}
		if fwd > 0 {
			continue
		}
		hasFormalBacklink := false
		for _, e := range g.backward[id] {
			if e.Source == "yaml" || e.Source == "convention-internal" {
				hasFormalBacklink = true
				break
			}
		}
		if !hasFormalBacklink {
			issues = append(issues, LintIssue{
				Rule:        "low-link-density",
				ComponentID: id,
				Detail:      fmt.Sprintf("0 outgoing references, %d incoming (no formal backlinks)", bwd),
			})
		}
	}
	return issues
}
//
// suggestArchivedTarget checks whether a dead-link target might have been
// archived or moved. For /changes/ paths, it looks for a matching change
// under "/archive/". As a fallback, it matches by filename against all
// graph components — catching directory restructures like
// "knowledge/secure/v2/foo.md" → "knowledge/foo.md".
func suggestArchivedTarget(target string, g *Graph) string {
	// Archive check: /changes/<name>/... → /archive/.../<name>/...
	if idx := strings.Index(target, "/changes/"); idx >= 0 {
		rest := target[idx+len("/changes/"):]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			if cn := rest[:slash]; cn != "" {
				for id := range g.components {
					if strings.Contains(id, "/archive/") && strings.Contains(id, cn) {
						if ai := strings.Index(id, "/archive/"); ai >= 0 {
							return "possibly archived as " + id[ai+1:]
						}
						return "possibly archived as " + id
					}
				}
			}
		}
	}
	// Filename fallback: the file may have moved within the same workspace.
	base := filepath.Base(target)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	for id, c := range g.components {
		if id == target {
			continue
		}
		if filepath.Base(c.Path) == base {
			if ai := strings.Index(id, "/archive/"); ai >= 0 {
				return "possibly at " + id[ai+1:]
			}
			return "possibly at " + id
		}
	}
	return ""
}
//

// lintLifecycleGaps flags workflow-lifecycle gaps that the link-based checks
// above cannot see:
//   - design-no-plan: a design.md exists (>3 days old, by its change's
//     created_at) but the change's .comet.yaml has no outgoing edge whose
//     target path contains "plans" — i.e. no plan has been written yet.
//   - stale-active: a change is not archived (phase != "archive"/"") and its
//     created_at is >14 days old — it has been sitting active too long.
//
// Both skip anything under an "/archive/" path segment, since archived
// changes are expected to be old and done.
func (g *Graph) lintLifecycleGaps() []LintIssue {
	var issues []LintIssue
	now := time.Now()

	for id, c := range g.components {
		if strings.Contains(id, "/archive/") {
			continue
		}

		if c.Type == TypeChange {
			createdAt, ok := frontmatterTime(c.Frontmatter["created_at"])
			if !ok {
				continue
			}
			phase, _ := c.Frontmatter["phase"].(string)
			sourceType, _ := c.Frontmatter["_source"].(string)
			age := now.Sub(createdAt)
			isComplete := phase == "archive" ||
				(sourceType == "trellis" && (phase == "completed" || phase == "rejected"))

			if !isComplete && phase != "" && age > 14*24*time.Hour {
				issues = append(issues, LintIssue{
					Rule:        "stale-active",
					ComponentID: id,
					Detail:      fmt.Sprintf("phase=%s, created %s (%d days ago)", phase, createdAt.Format("2006-01-02"), int(age.Hours()/24)),
				})
			}
		}

		if c.Type == TypeDesign {
			changeDir := filepath.Dir(id)
			yamlID := filepath.Join(changeDir, ".comet.yaml")
			changeComp, ok := g.components[yamlID]
			if !ok {
				continue
			}
			createdAt, ok := frontmatterTime(changeComp.Frontmatter["created_at"])
			if !ok || now.Sub(createdAt) <= 3*24*time.Hour {
				continue
			}

			hasPlan := false
			for _, e := range g.Forward(yamlID) {
				if strings.Contains(e.To, "plans") {
					hasPlan = true
					break
				}
			}
			if !hasPlan {
				issues = append(issues, LintIssue{
					Rule:        "design-no-plan",
					ComponentID: id,
					Detail:      fmt.Sprintf("design exists since %s but no plan reference found", createdAt.Format("2006-01-02")),
				})
			}
		}
	}
	return issues
}

var artifactTaskNumRe = regexp.MustCompile(`^task-(\d+)-`)

// LintTaskArtifacts checks, per task number (1..totalTasks), whether at
// least one task-NN-*.md file exists in artifactsDir. It counts distinct
// task numbers present, NOT raw file count — a task with multiple role
// files (implementer, oracle-review, ...) must not inflate the count.
func (g *Graph) LintTaskArtifacts(tasksComponent Component, artifactsDir string, totalTasks int) []LintIssue {
	present := map[int]bool{}
	entries, err := os.ReadDir(artifactsDir)
	if err == nil {
		for _, e := range entries {
			m := artifactTaskNumRe.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			var n int
			fmt.Sscanf(m[1], "%d", &n)
			present[n] = true
		}
	}

	var issues []LintIssue
	for n := 1; n <= totalTasks; n++ {
		if !present[n] {
			issues = append(issues, LintIssue{
				Rule:        "task-artifact-missing",
				ComponentID: tasksComponent.ID,
				Detail:      fmt.Sprintf("task %d", n),
			})
		}
	}
	return issues
}
