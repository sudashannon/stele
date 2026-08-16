package wiki

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ── Low-quality detection ────────────────────────────────────────────
//
// One rule with named signals, rather than four independent rules, because the
// question a reader asks is "should this document exist" and the answer needs
// every signal at once. Thresholds are calibrated against the real corpus
// (1448 documents): substantive length has a median of 2448 characters and a 5th
// percentile of 438, so the pre-existing 200-character low-content rule only ever
// reached the bottom 1.8%.

// fenceRE matches a fenced code block. Masking it is load-bearing for every
// signal below: a shell comment inside a fence (`# 预期: Counter=0x0b`) parses as
// an h1 otherwise. Measured on this corpus, scanning unmasked text reported the
// six best-written documents as having 21-59 empty headings each - a 19919-char
// design document read as an unfilled skeleton - and flagged 36.9% of the corpus.
// Masking brought that to 1.5% and those six documents to zero.
var fenceRE = regexp.MustCompile("(?s)```.*?```")

var (
	headingRE      = regexp.MustCompile(`(?m)^(#{1,6})\s+\S.*$`)
	tableRowRE     = regexp.MustCompile(`(?m)^\s*\|.+\|`)
	bulletRE       = regexp.MustCompile(`(?m)^\s*[-*+]\s+\S`)
	orderedRE      = regexp.MustCompile(`(?m)^\s*\d+\.\s+\S`)
	imageRE        = regexp.MustCompile(`(?m)^\s*!\[`)
	frontmatterSrc = regexp.MustCompile(`(?m)^\s*source:\s*"?https?://`)
	// A short document that points elsewhere is deliberate, not unfinished.
	pointerRE = regexp.MustCompile(`(?i)(见|详见|参见|移至|已迁移|见下|\bsee\b|\brefer to\b|\bmoved to\b|\bsuperseded by\b)`)
)

// maskFences blanks fenced code while preserving line structure, so offsets and
// line-anchored patterns still line up with the original text.
func maskFences(body string) string {
	return fenceRE.ReplaceAllStringFunc(body, func(block string) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' {
				return '\n'
			}
			return ' '
		}, block)
	})
}

// readRaw returns a document's full text including frontmatter. readBody strips
// frontmatter, but the imported-source check has to look at it.
func readRaw(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// splitFrontmatter separates a leading YAML block from the body. An unterminated
// block is treated as body: a document that opens with `---` and never closes it
// has no frontmatter, and consuming the whole file would report it as empty.
func splitFrontmatter(raw string) (frontmatter, body string) {
	if !strings.HasPrefix(raw, "---\n") {
		return "", raw
	}
	idx := strings.Index(raw[4:], "\n---")
	if idx < 0 {
		return "", raw
	}
	end := 4 + idx + len("\n---")
	// Skip the rest of the closing line.
	if nl := strings.IndexByte(raw[end:], '\n'); nl >= 0 {
		end += nl + 1
	} else {
		end = len(raw)
	}
	return raw[4 : 4+idx], raw[end:]
}

// Thresholds, each one tied to a measurement rather than taste.
const (
	// p5 of substantive length is 438 chars; 500 keeps that boundary.
	lowQualityShortChars = 500
	// A long document can legitimately carry few headings, so structure only
	// counts as missing when the document is also short.
	lowQualityUnstructuredChars = 1500
	lowQualityMaxHeadings       = 1
	// Empty headings past this are an outline nobody filled in.
	lowQualityEmptyHeadings = 3
	// Density p95 among documents that contain any placeholder is 2.70/1k.
	lowQualityPlaceholders    = 3
	lowQualityPlaceholderRate = 2.0
	// A pointer document is short by design; beyond this it is prose again.
	pointerMaxChars = 400
)

// LowQualitySignals is why a document was flagged, and the measurements behind
// each reason so the panel can show them instead of an opaque verdict.
type LowQualitySignals struct {
	Chars         int      `json:"chars"`
	Headings      int      `json:"headings"`
	EmptyHeadings int      `json:"emptyHeadings"`
	Placeholders  int      `json:"placeholders"`
	Signals       []string `json:"signals"`
	// Imported is set for a document carrying an upstream `source:` URL. It is
	// reported rather than hidden: the content is genuinely thin, but it was not
	// authored here and deleting it only lasts until the next import.
	Imported bool `json:"imported"`
}

// emptyHeadingCount counts headings whose own section carries no content.
//
// A parent heading followed by a deeper one is normal structure, so a section
// ends at the next heading of the same or shallower level. Code, tables, lists
// and images all count as content: a section of pure commands documents its
// subject perfectly well, and treating it as empty is what made the manuals in
// this corpus look like skeletons.
func emptyHeadingCount(body string) (empty int, headings int) {
	masked := maskFences(body)
	locs := headingRE.FindAllStringSubmatchIndex(masked, -1)
	headings = len(locs)
	for i, loc := range locs {
		level := loc[3] - loc[2]
		end := len(masked)
		for j := i + 1; j < len(locs); j++ {
			if locs[j][3]-locs[j][2] <= level {
				end = locs[j][0]
				break
			}
		}
		section := headingRE.ReplaceAllString(body[loc[1]:end], "")
		if !sectionHasContent(section) {
			empty++
		}
	}
	return empty, headings
}

func sectionHasContent(section string) bool {
	if strings.Contains(section, "```") ||
		tableRowRE.MatchString(section) ||
		bulletRE.MatchString(section) ||
		orderedRE.MatchString(section) ||
		imageRE.MatchString(section) {
		return true
	}
	return len(stripMarkup(section)) >= 15
}

// evaluateLowQuality measures one document. It returns nil when the document is
// fine or when it is deliberately short. `phase` folds in the rule this replaces:
// a finished document should carry no placeholder at all, because an unresolved
// TODO in an archived or verified document is a defect at any density.
func evaluateLowQuality(frontmatter, body, phase string) *LowQualitySignals {
	masked := maskFences(body)
	chars := len(stripMarkup(body))
	empty, headings := emptyHeadingCount(body)
	// Placeholders inside a fence are examples, not unfinished prose.
	placeholders := len(placeholderRE.FindAllString(masked, -1))

	// A pointer document is the one short shape that is correct: "见
	// openspec/changes/…", "See tasks.md". Its content lives elsewhere, and
	// deleting it breaks the trail to it.
	if chars > 0 && chars <= pointerMaxChars && pointerRE.MatchString(masked) {
		return nil
	}

	var signals []string
	if chars < lowQualityShortChars {
		signals = append(signals, "short")
	}
	if headings <= lowQualityMaxHeadings && chars < lowQualityUnstructuredChars {
		signals = append(signals, "unstructured")
	}
	if empty >= lowQualityEmptyHeadings {
		signals = append(signals, "unfilled-outline")
	}
	if placeholders >= lowQualityPlaceholders && chars > 0 &&
		float64(placeholders)*1000/float64(chars) >= lowQualityPlaceholderRate {
		signals = append(signals, "placeholder-dense")
	}
	// A finished document with any unresolved marker is a defect on its own: the
	// work is declared done while the text still says it is not.
	if placeholders > 0 && (phase == "archive" || phase == "verify") {
		signals = append(signals, "unresolved-in-finished")
	}
	if len(signals) == 0 {
		return nil
	}
	return &LowQualitySignals{
		Chars:         chars,
		Headings:      headings,
		EmptyHeadings: empty,
		Placeholders:  placeholders,
		Signals:       signals,
		Imported:      frontmatterSrc.MatchString(frontmatter),
	}
}

func (g *Graph) lintLowQuality() []LintIssue {
	var issues []LintIssue
	for id, c := range g.components {
		if !lintableBody(c) {
			continue
		}
		// Quality is a property of prose. A change's .comet.yaml is generated
		// metadata with no headings by construction, so all 27 of them in this
		// corpus reported as "unstructured" and none of them can be deleted.
		if !strings.HasSuffix(strings.ToLower(c.Path), ".md") {
			continue
		}
		if !fileExists(c.Path) {
			// A missing file is dead-link's problem, not a quality verdict.
			continue
		}
		frontmatter, body := splitFrontmatter(readRaw(c.Path))
		phase, _ := c.Frontmatter["phase"].(string)
		signals := evaluateLowQuality(frontmatter, body, phase)
		if signals == nil {
			continue
		}
		detail := fmt.Sprintf("%s · %d 字 · %d 标题",
			strings.Join(signals.Signals, ","), signals.Chars, signals.Headings)
		if signals.EmptyHeadings > 0 {
			detail += fmt.Sprintf(" · %d 空标题", signals.EmptyHeadings)
		}
		if signals.Placeholders > 0 {
			detail += fmt.Sprintf(" · %d 占位符", signals.Placeholders)
		}
		if signals.Imported {
			detail += " · 上游导入"
		}
		issues = append(issues, LintIssue{
			Rule:        "low-quality",
			ComponentID: id,
			Detail:      detail,
			LowQuality:  signals,
		})
	}
	return issues
}
