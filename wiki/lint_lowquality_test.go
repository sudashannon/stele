package wiki

import (
	"strings"
	"testing"
)

// Every case here is a mistake made while calibrating this rule against the real
// corpus. They are the reason the thresholds are what they are.

// The decisive one. A well-written manual is mostly commands, and a shell comment
// inside a fence parses as an h1. Scanning unmasked text reported the six
// best-written documents in the corpus as having 21-59 empty headings each, and
// flagged 36.9% of everything; masking brought it to 1.5%.
func TestLowQualityIgnoresHeadingsInsideCodeFences(t *testing.T) {
	body := "# 部署手册\n\n" +
		"实际步骤如下，每一步都给了可复制的命令。\n\n" +
		"## 0. 部署 TA\n\n" +
		"```bash\n" +
		"# 预期: Counter=0x0000000b 或更高\n" +
		"# 编辑: ta/customer/keymanager/acl.c 第 93 行\n" +
		"#    将: return 0;\n" +
		"#    改为: return 1;\n" +
		"adb shell \"mount -o remount,rw /\"\n" +
		"```\n\n" +
		"## 1. 验证\n\n" +
		"```bash\nrun-test --all\n```\n"

	empty, headings := emptyHeadingCount(body)
	if headings != 3 {
		t.Fatalf("headings = %d, want 3: the four `# …` lines inside the fence are comments, not headings", headings)
	}
	if empty != 0 {
		t.Fatalf("emptyHeadings = %d, want 0: every section holds a code block, which is content", empty)
	}
}

// A section whose body is a code block, a table or a list is documented. Treating
// only prose as content is what made command manuals look like skeletons.
func TestLowQualityCountsCodeTablesAndListsAsContent(t *testing.T) {
	for name, section := range map[string]string{
		"code":  "```go\nfmt.Println(1)\n```\n",
		"table": "| a | b |\n|---|---|\n| 1 | 2 |\n",
		"list":  "- first\n- second\n",
		"order": "1. first\n2. second\n",
		"image": "![diagram](./d.png)\n",
	} {
		t.Run(name, func(t *testing.T) {
			empty, _ := emptyHeadingCount("## Section\n\n" + section)
			if empty != 0 {
				t.Fatalf("emptyHeadings = %d for a section containing %s, want 0", empty, name)
			}
		})
	}
	empty, _ := emptyHeadingCount("## Section\n\n### Nested\n\nreal prose lives here\n")
	if empty != 0 {
		t.Fatalf("emptyHeadings = %d, want 0: a parent heading followed by a deeper one is structure", empty)
	}
}

// A short document that points elsewhere is deliberate. Three of them exist in
// this corpus ("# Plan: TDD xchip hardening / See tasks.md"), and deleting one
// breaks the trail to the content it points at.
func TestLowQualitySkipsPointerDocuments(t *testing.T) {
	for _, body := range []string{
		"# Plan: TDD xchip hardening\nSee tasks.md. Phase 1 red tests → Phase 2 green fixes.\n",
		"# md_bus xchip hardening — 技术设计\n\n见 openspec/changes/2026-06-17-md-bus-xchip-hardening/design.md\n",
		"# Stress hardening\n\n详见 openspec/changes/…/proposal.md\n",
	} {
		if got := evaluateLowQuality("", body, ""); got != nil {
			t.Errorf("flagged a pointer document as %v: %q", got.Signals, strings.SplitN(body, "\n", 2)[0])
		}
	}
}

func TestLowQualityFlagsTheShapesItShould(t *testing.T) {
	cases := map[string]struct {
		frontmatter, body, phase string
		want                     string
	}{
		"empty file":     {body: "", want: "short"},
		"title only":     {body: "# Kernel\n\nThis topic discusses aspects of the kernel.\n", want: "unstructured"},
		"unfilled":       {body: "# T\n\n## A\n\n## B\n\n## C\n\n## D\n\n" + strings.Repeat("prose. ", 200), want: "unfilled-outline"},
		"placeholders":   {body: "# T\n\n## A\n\nTODO TBD FIXME\n", want: "placeholder-dense"},
		"done with todo": {body: "# T\n\n## A\n\n" + strings.Repeat("real content here. ", 100) + "\n\nTODO one thing\n", phase: "archive", want: "unresolved-in-finished"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := evaluateLowQuality(c.frontmatter, c.body, c.phase)
			if got == nil {
				t.Fatalf("not flagged, want signal %q", c.want)
			}
			for _, s := range got.Signals {
				if s == c.want {
					return
				}
			}
			t.Fatalf("signals = %v, want to include %q", got.Signals, c.want)
		})
	}
}

// A long document with few headings is prose, not a defect. The structure signal
// only fires together with shortness for exactly this reason.
func TestLowQualityLeavesLongProseAlone(t *testing.T) {
	body := "# 设计\n\n" + strings.Repeat("这是一段真实的设计说明，讲清了取舍与理由。", 120)
	if got := evaluateLowQuality("", body, ""); got != nil {
		t.Fatalf("flagged long prose as %v", got.Signals)
	}
}

// An upstream import is reported, not hidden: the content really is thin, but it
// was not authored here and deleting it lasts until the next import.
func TestLowQualityMarksUpstreamImports(t *testing.T) {
	fm := "title: \"PyHSL\"\nsource: \"https://docs.nvidia.com/jetson/archives/r39.2/DeveloperGuide/SD/Camera.html\"\n"
	got := evaluateLowQuality(fm, "# PyHSL\n", "")
	if got == nil {
		t.Fatal("not flagged, want a thin imported stub to be reported")
	}
	if !got.Imported {
		t.Fatal("Imported = false, want true: frontmatter carries an upstream source URL")
	}
	if local := evaluateLowQuality("title: mine\n", "# Mine\n", ""); local == nil || local.Imported {
		t.Fatal("a locally authored stub must be flagged but not marked as imported")
	}
}

// Placeholders inside a fence are examples in documentation about placeholders,
// not unfinished prose.
func TestLowQualityIgnoresPlaceholdersInsideFences(t *testing.T) {
	body := "# 规范\n\n## 占位符约定\n\n" + strings.Repeat("说明文字。", 60) +
		"\n\n```md\nTODO: 待补充\nTBD\nFIXME\n```\n"
	got := evaluateLowQuality("", body, "")
	if got != nil {
		for _, s := range got.Signals {
			if s == "placeholder-dense" {
				t.Fatalf("signals = %v: the markers live inside a fenced example", got.Signals)
			}
		}
	}
}

func TestSplitFrontmatterHandlesAnUnterminatedBlock(t *testing.T) {
	fm, body := splitFrontmatter("---\ntitle: x\nno closing delimiter\n")
	if fm != "" {
		t.Fatalf("frontmatter = %q, want empty: the block never closes", fm)
	}
	if !strings.Contains(body, "no closing delimiter") {
		t.Fatalf("body = %q, want the whole text kept", body)
	}
	fm2, body2 := splitFrontmatter("---\ntitle: x\n---\n# Doc\n")
	if strings.TrimSpace(fm2) != "title: x" || strings.TrimSpace(body2) != "# Doc" {
		t.Fatalf("split = (%q, %q)", fm2, body2)
	}
}
