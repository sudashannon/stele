package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMermaidFenceProblems_ValidBlocks(t *testing.T) {
	valid := []string{
		"graph TD\n  A[Start] --> B[End]\n",
		"sequenceDiagram\n  Alice->>Bob: Hello\n  Bob-->>Alice: Hi\n",
		"flowchart LR\n  a(\"label with [brackets] inside\") --> b\n",
		"%%{init: {'theme':'dark'}}%%\ngraph TD\n  A --> B\n",
		"pie\ntitle Shares\n  \"A\" : 30\n  \"B\" : 70\n",
	}
	for i, block := range valid {
		if problem := validateMermaidBlock(strings.Split(block, "\n")); problem != "" {
			t.Errorf("valid block %d flagged: %s", i, problem)
		}
	}
}

func TestMermaidFenceProblems_BrokenBlocks(t *testing.T) {
	cases := []struct {
		block  string
		needle string
	}{
		{"\n  \n", "empty diagram"},
		{"banana chart\n  A --> B", "unknown diagram type"},
		{`graph TD
  A["unterminated --> B`, "unbalanced double quotes"},
		{`graph TD
  A[unterminated --> B`, "unbalanced [ ]"},
		{`graph TD
  A(open --> B`, "unbalanced ( )"},
		{`graph TD
  A{choice --> B`, "unbalanced { }"},
		{`graph TD
  A --> B
  B --> A
  extra ] stray`, "unbalanced [ ]"},
	}
	for i, tc := range cases {
		problem := validateMermaidBlock(strings.Split(tc.block, "\n"))
		if !strings.Contains(problem, tc.needle) {
			t.Errorf("case %d: want %q in problem, got %q", i, tc.needle, problem)
		}
	}
}

func TestLintMermaidFences_ReportsBrokenFence(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	bad := filepath.Join(dir, "bad.md")
	os.WriteFile(good, []byte("# Good\n\n```mermaid\ngraph TD\n  A --> B\n```\n"), 0o644)
	os.WriteFile(bad, []byte("# Bad\n\n```mermaid\ngraph TD\n  A[unterminated --> B\n```\n\n```\ncode fence is not mermaid\n```\n"), 0o644)

	g := BuildGraph([]Component{
		{ID: good, Path: good, Title: "Good", Type: TypeKnowledge},
		{ID: bad, Path: bad, Title: "Bad", Type: TypeKnowledge},
	}, nil)
	issues := g.lintMermaidFences()
	if len(issues) != 1 {
		t.Fatalf("want exactly 1 mermaid issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].Rule != "mermaid-syntax" || issues[0].ComponentID != bad {
		t.Fatalf("wrong issue: %+v", issues[0])
	}
	if !strings.Contains(issues[0].Detail, "unbalanced") {
		t.Fatalf("detail should name the defect: %s", issues[0].Detail)
	}
}

func TestLintMermaidFences_UnclosedFence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "unclosed.md")
	os.WriteFile(p, []byte("# U\n\n```mermaid\ngraph TD\n  A --> B\n"), 0o644)
	g := BuildGraph([]Component{{ID: p, Path: p, Title: "U", Type: TypeKnowledge}}, nil)
	issues := g.lintMermaidFences()
	if len(issues) != 1 || !strings.Contains(issues[0].Detail, "unclosed fence") {
		t.Fatalf("want unclosed fence issue, got %+v", issues)
	}
}
