package wiki

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// lintMermaidFences flags mermaid code fences that the frontend renderer will
// reject. The panel degrades a broken fence to raw source (no more red error
// overlay), but a document that ships a fence mermaid cannot parse is still a
// documentation defect worth surfacing.
//
// Validation is deliberately conservative: it catches the failures that
// actually happen in practice (empty fences, unknown diagram types, unbalanced
// quotes or brackets in node labels) without pretending to be a full mermaid
// parser. A fence that passes here may still fail in mermaid.js; a fence that
// fails here will almost certainly fail there.
func (g *Graph) lintMermaidFences() []LintIssue {
	var issues []LintIssue
	for id, c := range g.components {
		if !lintableBody(c) || !strings.HasSuffix(filepath.Clean(c.Path), ".md") {
			continue
		}
		body := readBody(c.Path)
		for i, problem := range mermaidFenceProblems(body) {
			detail := fmt.Sprintf("mermaid fence %d: %s", i+1, problem)
			issues = append(issues, LintIssue{Rule: "mermaid-syntax", ComponentID: id, Detail: detail})
		}
	}
	return issues
}

// mermaidFenceProblems extracts every ```mermaid fence from body and returns
// one problem string per fence that fails the conservative checks (an empty
// slice when every fence passes).
func mermaidFenceProblems(body string) []string {
	var problems []string
	lines := strings.Split(body, "\n")
	var fence []string
	inFence := false
	fenceIdx := 0
	flush := func() {
		if problem := validateMermaidBlock(fence); problem != "" {
			problems = append(problems, problem)
		}
		fenceIdx++
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				info := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
				if strings.EqualFold(info, "mermaid") {
					inFence = true
					fence = nil
				}
				continue
			}
			inFence = false
			flush()
			continue
		}
		if inFence {
			fence = append(fence, line)
		}
	}
	// An unclosed fence at EOF is itself a defect (the renderer never sees
	// the closing backticks).
	if inFence {
		if problem := validateMermaidBlock(fence); problem != "" {
			problems = append(problems, problem)
		} else {
			problems = append(problems, "unclosed fence")
		}
	}
	return problems
}

// mermaidDiagramTypes are the first tokens the renderer accepts as a diagram
// type. Unknown first lines are the most common "big red error" cause.
var mermaidDiagramTypes = map[string]bool{
	"graph": true, "flowchart": true, "sequenceDiagram": true, "classDiagram": true,
	"classDiagram-v2": true, "stateDiagram": true, "stateDiagram-v2": true,
	"erDiagram": true, "gantt": true, "pie": true, "journey": true,
	"gitGraph": true, "mindmap": true, "timeline": true, "quadrantChart": true,
	"sankey-beta": true, "xychart-beta": true, "requirementDiagram": true,
	"C4Context": true, "C4Container": true, "C4Component": true, "C4Dynamic": true,
	"C4Deployment": true, "block-beta": true, "packet-beta": true, "kanban": true,
	"zenuml": true, "architecture": true, "radar-beta": true, "treemap": true,
}

// mermaidDirectiveRE matches the %%{...}%% directives mermaid allows at the
// top of a diagram (theme, init, etc.).
var mermaidDirectiveRE = regexp.MustCompile(`^%%\{.*\}%%$`)

// validateMermaidBlock returns "" when the block looks renderable, or a short
// human-readable problem when it does not.
func validateMermaidBlock(lines []string) string {
	// First meaningful line: skip blanks and %% comments/directives.
	first := ""
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		first = t
		break
	}
	if first == "" {
		return "empty diagram"
	}
	if mermaidDirectiveRE.MatchString(first) {
		// Directive-first blocks must be followed by a diagram type; without
		// one the renderer has nothing to draw.
		for _, line := range lines {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "%%") {
				continue
			}
			first = t
			break
		}
		if first == "" {
			return "only directives, no diagram"
		}
	}
	words := strings.Fields(first)
	if len(words) == 0 {
		return "empty diagram"
	}
	if !mermaidDiagramTypes[strings.ToLower(words[0])] && !mermaidDiagramTypes[words[0]] {
		return fmt.Sprintf("unknown diagram type %q", words[0])
	}

	// Quote balance: an odd number of double quotes means a node label or
	// message text is never closed, which mermaid rejects.
	quotes := 0
	for _, line := range lines {
		quotes += strings.Count(line, "\"")
	}
	if quotes%2 != 0 {
		return "unbalanced double quotes"
	}

	// Bracket balance per line: node shapes in flowcharts ([...], (...),
	// {...}) and sequence participant aliases must close on the same line.
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		if !balanced(line, '[', ']') {
			return "unbalanced [ ] on line: " + shortLine(t)
		}
		if !balanced(line, '(', ')') {
			return "unbalanced ( ) on line: " + shortLine(t)
		}
		if !balanced(line, '{', '}') {
			return "unbalanced { } on line: " + shortLine(t)
		}
	}
	return ""
}

// balanced reports whether open/close characters are properly paired in s,
// ignoring any inside double quotes (quoted labels may contain brackets).
func balanced(s string, open, close byte) bool {
	depth := 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch ch {
		case open:
			depth++
		case close:
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func shortLine(t string) string {
	if len(t) > 60 {
		return t[:57] + "..."
	}
	return t
}
