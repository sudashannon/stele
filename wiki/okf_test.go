package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"stele/internal/claims"
)

func parseOKFFrontmatter(t *testing.T, doc string) (map[string]any, string) {
	t.Helper()
	fmText, body := splitFrontmatter(doc)
	if fmText == "" {
		t.Fatalf("no frontmatter block in:\n%s", doc)
	}
	fm := map[string]any{}
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		t.Fatalf("frontmatter does not parse (OKF conformance): %v", err)
	}
	return fm, body
}

func TestOKFProject_RequiredFieldsWithoutFrontmatter(t *testing.T) {
	p := NewOKFProjector(nil)
	out := p.Project("# Title\n\nBody text.\n", "knowledge", "/ws/knowledge/x.md")
	fm, body := parseOKFFrontmatter(t, out)
	if typ, _ := fm["type"].(string); typ == "" {
		t.Fatalf("type must be non-empty (OKF required): %v", fm)
	} else if typ != "knowledge" {
		t.Fatalf("type = %q, want projected component type", typ)
	}
	gen, ok := fm["generated"].(map[string]any)
	if !ok || gen["by"] != okfGenerator || gen["at"] == "" {
		t.Fatalf("generated metadata missing or wrong: %v", fm["generated"])
	}
	if !strings.Contains(body, "# Title") || !strings.Contains(body, "Body text.") {
		t.Fatalf("body must be preserved: %q", body)
	}
}

func TestOKFProject_MergesExistingFrontmatter(t *testing.T) {
	p := NewOKFProjector(nil)
	raw := "---\ntitle: My Doc\ntags: [a, b]\n---\n\n# My Doc\n"
	out := p.Project(raw, "design", "/ws/docs/x.md")
	fm, body := parseOKFFrontmatter(t, out)
	if fm["title"] != "My Doc" {
		t.Fatalf("existing title lost: %v", fm["title"])
	}
	if tags, ok := fm["tags"].([]any); !ok || len(tags) != 2 {
		t.Fatalf("existing tags lost: %v", fm["tags"])
	}
	if fm["type"] != "design" {
		t.Fatalf("type not projected: %v", fm["type"])
	}
	if !strings.Contains(body, "# My Doc") {
		t.Fatalf("body not preserved: %q", body)
	}
}

func TestOKFProject_KeepsAuthorType(t *testing.T) {
	p := NewOKFProjector(nil)
	out := p.Project("---\ntype: BigQuery Table\n---\nbody\n", "knowledge", "/x.md")
	fm, _ := parseOKFFrontmatter(t, out)
	if fm["type"] != "BigQuery Table" {
		t.Fatalf("author type must win: %v", fm["type"])
	}
}

func okfTestStore(t *testing.T, doc string) *claims.Store {
	t.Helper()
	s, err := claims.NewStore(filepath.Join(t.TempDir(), "claims.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	in := []claims.Claim{
		{
			ID: "claim.rx101-pcie", Workspace: "rx101", DocID: doc,
			Kind: claims.KindFact, Truth: claims.TruthCodeVerified, Intent: claims.IntentIntended,
			Text:   "PCIe 链路速率锁定为 Gen3 x4",
			Status: claims.StatusActive,
			Evidence: []claims.Evidence{
				{Resource: "ws://rx101/scripts/bringup.sh#L10-L20"},
			},
		},
		{
			ID: "claim.rx101-stale", Workspace: "rx101", DocID: doc,
			Kind: claims.KindFact, Truth: claims.TruthUnknown, Intent: claims.IntentUnknown,
			Text:   "已过期断言",
			Status: claims.StatusStale,
			Evidence: []claims.Evidence{
				{Resource: "doc://rx101/knowledge/x.md"},
			},
		},
		{
			ID: "claim.rx101-retracted", Workspace: "rx101", DocID: doc,
			Kind: claims.KindFact, Truth: claims.TruthUnknown, Intent: claims.IntentUnknown,
			Text: "已撤回断言", Status: claims.StatusRetracted,
		},
	}
	if _, err := s.Upsert(in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	return s
}

func TestOKFProject_ClaimsSourcesAndLifecycle(t *testing.T) {
	const doc = "/ws/knowledge/x.md"
	s := okfTestStore(t, doc)
	p := NewOKFProjector(s)
	out := p.Project("# X\nbody\n", "knowledge", doc)
	fm, _ := parseOKFFrontmatter(t, out)

	sources, ok := fm["sources"].([]any)
	if !ok || len(sources) != 2 {
		t.Fatalf("want 2 sources (active + stale, retracted excluded), got %v", fm["sources"])
	}
	var ids []string
	for _, item := range sources {
		m := item.(map[string]any)
		ids = append(ids, m["id"].(string))
		if m["resource"] == "" || m["title"] == "" {
			t.Fatalf("source entry missing resource/title: %v", m)
		}
	}
	if strings.Join(ids, ",") != "claim.rx101-pcie,claim.rx101-stale" {
		t.Fatalf("sources ids = %v", ids)
	}
	if fm["status"] != "stale" {
		t.Fatalf("stale claim must set status: stale, got %v", fm["status"])
	}
	if _, hasVerified := fm["verified"]; hasVerified {
		t.Fatalf("a stale document must not also be verified: %v", fm)
	}
}

func TestOKFProject_VerifiedWhenAllFresh(t *testing.T) {
	const doc = "/ws/knowledge/x.md"
	s, err := claims.NewStore(filepath.Join(t.TempDir(), "claims.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := s.Upsert([]claims.Claim{{
		ID: "claim.rx101-pcie", Workspace: "rx101", DocID: doc,
		Kind: claims.KindFact, Truth: claims.TruthCodeVerified, Intent: claims.IntentIntended,
		Text: "PCIe 链路速率锁定为 Gen3 x4", Status: claims.StatusActive,
	}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	p := NewOKFProjector(s)
	out := p.Project("# X\nbody\n", "knowledge", doc)
	fm, _ := parseOKFFrontmatter(t, out)
	if v, _ := fm["verified"].(bool); !v {
		t.Fatalf("fresh-claim-backed document must be verified: %v", fm)
	}
	if _, hasStatus := fm["status"]; hasStatus {
		t.Fatalf("no stale claim, no status expected: %v", fm)
	}
}

func TestOKFIndex_DeclaresVersion(t *testing.T) {
	out := OKFIndex([]string{"rx101/knowledge/a.md", "miao/openspec/changes/b/proposal.md"})
	fm, body := parseOKFFrontmatter(t, out)
	if fm["okf_version"] != "0.2" {
		t.Fatalf("okf_version = %v, want 0.2", fm["okf_version"])
	}
	if !strings.Contains(body, "rx101/knowledge/a.md") || !strings.Contains(body, "miao/openspec/changes/b/proposal.md") {
		t.Fatalf("index must list concepts: %q", body)
	}
}

func TestMirrorFlushOKF_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "knowledge-repo")
	m := NewMirror(repo, "")
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	srcDir := filepath.Join(dir, "src")
	src := filepath.Join(srcDir, "x.md")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(src, []byte("# X\nbody\n"), 0644)

	s := okfTestStore(t, src)

	m.SetOKF(NewOKFProjector(s))

	// SyncFile is the watcher path: no component type, derived at write time.
	m.SyncFile("rx101", src, "knowledge/x.md")
	m.flush()

	dest := filepath.Join(repo, "rx101", "knowledge", "x.md")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("mirrored file missing: %v", err)
	}
	fm, _ := parseOKFFrontmatter(t, string(data))
	if typ, _ := fm["type"].(string); typ == "" {
		t.Fatalf("mirrored concept lacks required type: %v", fm)
	}
	if fm["status"] != "stale" {
		t.Fatalf("mirrored concept should carry the stale lifecycle: %v", fm)
	}

	indexData, err := os.ReadFile(filepath.Join(repo, "index.md"))
	if err != nil {
		t.Fatalf("index.md missing: %v", err)
	}
	if !strings.Contains(string(indexData), `okf_version: "0.2"`) {
		t.Fatalf("index.md must declare the OKF version: %q", indexData)
	}
	if _, err := os.Stat(filepath.Join(repo, "log.md")); err != nil {
		t.Fatalf("log.md missing: %v", err)
	}
}

func TestOKFProject_TitleTruncationIsValidUTF8(t *testing.T) {
	const doc = "/ws/knowledge/x.md"
	s := okfTestStore(t, doc)
	// Replace a fixture claim's text with a long Chinese string: its byte
	// length far exceeds 80, and a byte-level 80-cut would land mid-rune.
	long := "生产加固方案的关键结论记录在文档中"
	for len(long) < 300 {
		long += long
	}
	if _, err := s.Upsert([]claims.Claim{{
		ID: "claim.rx101-long", Workspace: "rx101", DocID: doc,
		Kind: claims.KindFact, Truth: claims.TruthUnknown, Intent: claims.IntentUnknown,
		Text: long, Status: claims.StatusActive,
		Evidence: []claims.Evidence{{Resource: "doc://rx101/knowledge/x.md"}},
	}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	p := NewOKFProjector(s)
	out := p.Project("# X\nbody\n", "knowledge", doc)
	if strings.Contains(out, "!!binary") {
		t.Fatalf("frontmatter contains !!binary (invalid UTF-8 title):\n%s", out)
	}
	fm, _ := parseOKFFrontmatter(t, out)
	sources, _ := fm["sources"].([]any)
	if len(sources) == 0 {
		t.Fatalf("no sources projected: %v", fm)
	}
	title := sources[0].(map[string]any)["title"].(string)
	if !utf8.ValidString(title) {
		t.Fatalf("title is not valid UTF-8: %q", title)
	}
	if len([]rune(title)) > 80 {
		t.Fatalf("title not truncated to 80 runes: %d runes", len([]rune(title)))
	}
}
