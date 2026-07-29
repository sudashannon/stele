package wiki

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	defaultSemanticRuneBudget = 4096
	maxSemanticHeadings       = 32
	maxSemanticParagraphRunes = 320
)

// SemanticText is a deterministic, bounded projection of one Markdown
// document used by both embedding and report evidence extraction.
type SemanticText struct {
	Text            string
	Headings        []string
	KeyParagraphs   []string
	Truncated       bool
	OmittedSections int
	ChecklistDone   int
	ChecklistTotal  int
}

// ExtractSemanticText preserves source order while bounding every document's
// contribution. maxRunes <= 0 selects the shared default budget.
func ExtractSemanticText(title string, content []byte, maxRunes int) SemanticText {
	if maxRunes <= 0 {
		maxRunes = defaultSemanticRuneBudget
	}
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	result := SemanticText{}

	var out strings.Builder
	remaining := maxRunes
	appendText := func(text string) string {
		text = strings.TrimSpace(text)
		if text == "" || remaining <= 0 {
			return ""
		}
		runes := []rune(text)
		if len(runes) > remaining {
			runes = runes[:remaining]
			result.Truncated = true
		}
		emitted := string(runes)
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(emitted)
		remaining -= len(runes)
		return emitted
	}

	appendText("Title: " + title)
	if result.ChecklistTotal > 0 {
		appendText(fmt.Sprintf("Checklist: %d/%d completed", result.ChecklistDone, result.ChecklistTotal))
	}

	inFrontmatter := false
	frontmatterChecked := false
	inFence := false
	headings := 0
	needParagraph := true
	var paragraph strings.Builder
	flushParagraph := func() {
		if paragraph.Len() == 0 || !needParagraph {
			paragraph.Reset()
			return
		}
		text := strings.TrimSpace(paragraph.String())
		runes := []rune(text)
		if len(runes) > maxSemanticParagraphRunes {
			runes = runes[:maxSemanticParagraphRunes]
			result.Truncated = true
		}
		if emitted := appendText(string(runes)); emitted != "" {
			result.KeyParagraphs = append(result.KeyParagraphs, emitted)
		}
		paragraph.Reset()
		needParagraph = false
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if !frontmatterChecked {
			frontmatterChecked = true
			if line == "---" {
				inFrontmatter = true
				continue
			}
		}
		if inFrontmatter {
			if line == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			flushParagraph()
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "- [x]") {
			result.ChecklistDone++
			result.ChecklistTotal++
		} else if strings.HasPrefix(lower, "- [ ]") {
			result.ChecklistTotal++
		}
		if isSemanticHeading(line) {
			flushParagraph()
			if headings >= maxSemanticHeadings || remaining <= 0 {
				result.OmittedSections++
				result.Truncated = true
				needParagraph = false
				continue
			}
			headings++
			if emitted := appendText(line); emitted != "" {
				result.Headings = append(result.Headings, emitted)
			}
			needParagraph = true
			continue
		}
		if line == "" {
			flushParagraph()
			continue
		}
		if !needParagraph || remaining <= 0 {
			continue
		}
		if paragraph.Len() > 0 {
			paragraph.WriteByte(' ')
		}
		paragraph.WriteString(line)
	}
	flushParagraph()

	if remaining <= 0 {
		result.Truncated = true
	}
	result.Text = out.String()
	return result
}

func isSemanticHeading(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	return level >= 1 && level <= 3 && level < len(line) && line[level] == ' '
}

// truncateRunes is shared by report prompt construction where byte length is
// not a safe proxy for Chinese text size.
func truncateRunes(text string, limit int) (string, bool) {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text, false
	}
	runes := []rune(text)
	return string(runes[:limit]), true
}
