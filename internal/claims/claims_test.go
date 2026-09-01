package claims

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validClaim() Claim {
	return Claim{
		ID:        "claim.rx101-pcie",
		Workspace: "rx101",
		Kind:      KindFact,
		Truth:     TruthCodeVerified,
		Intent:    IntentIntended,
		Text:      "PCIe link is forced up at 30s by the bringup script.",
		Evidence: []Evidence{
			{Resource: "ws://rx101/scripts/bringup.sh#L10-L20"},
		},
		Status: StatusActive,
	}
}

func TestValidate(t *testing.T) {
	c := validClaim()
	if err := Validate(c); err != nil {
		t.Fatalf("valid claim rejected: %v", err)
	}
	cases := []struct {
		name    string
		mutate  func(*Claim)
		wantErr string
	}{
		{"bad id", func(c *Claim) { c.ID = "claim.Bad-ID!" }, "must match"},
		{"no workspace", func(c *Claim) { c.Workspace = "" }, "workspace is required"},
		{"bad kind", func(c *Claim) { c.Kind = "opinion" }, "kind"},
		{"bad truth", func(c *Claim) { c.Truth = "vibes" }, "truth"},
		{"bad intent", func(c *Claim) { c.Intent = "maybe" }, "intent"},
		{"empty text", func(c *Claim) { c.Text = "" }, "text is required"},
		{"huge text", func(c *Claim) { c.Text = strings.Repeat("x", maxTextBytes+1) }, "exceeds"},
		{"bad resource scheme", func(c *Claim) { c.Evidence = []Evidence{{Resource: "http://x"}} }, "unsupported resource scheme"},
		{"escaping path", func(c *Claim) { c.Evidence = []Evidence{{Resource: "ws://rx101/../etc/passwd#L1-L2"}} }, "escapes"},
		{"git metadata", func(c *Claim) { c.Evidence = []Evidence{{Resource: "ws://rx101/.git/config#L1-L2"}} }, ".git"},
		{"bad range", func(c *Claim) { c.Evidence = []Evidence{{Resource: "ws://rx101/x.sh#L5-L2"}} }, "start 5 exceeds end 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validClaim()
			tc.mutate(&c)
			err := Validate(c)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateRetractedSkipsContentChecks(t *testing.T) {
	c := Claim{ID: "claim.x", Workspace: "w", Status: StatusRetracted}
	if err := Validate(c); err != nil {
		t.Fatalf("retracted claim should skip content validation: %v", err)
	}
}

func TestParseResource(t *testing.T) {
	res, kind, err := ParseResource("ws://rx101/src/a.cpp#L40-L82")
	if err != nil {
		t.Fatal(err)
	}
	if kind != EvidenceCode || res.Workspace != "rx101" || res.Rel != "src/a.cpp" || res.LineFrom != 40 || res.LineTo != 82 {
		t.Fatalf("unexpected parse: %+v", res)
	}
	if _, _, err := ParseResource("ws://rx101/src/a.cpp"); err == nil {
		t.Fatal("code resource without range must fail")
	}
	res, kind, err = ParseResource("doc://rx101/docs/design.md")
	if err != nil || kind != EvidenceDoc || res.Rel != "docs/design.md" {
		t.Fatalf("doc parse: %+v %v", res, err)
	}
	res, kind, err = ParseResource("session://abc123")
	if err != nil || kind != EvidenceSession || res.SessionID != "abc123" {
		t.Fatalf("session parse: %+v %v", res, err)
	}
	if _, _, err := ParseResource("doc://rx101/.."); err == nil {
		t.Fatal("escaping doc path must fail")
	}
	if _, _, err := ParseResource("session://a/b"); err == nil {
		t.Fatal("session id with slash must fail")
	}
}

// ── Evidence resolution ────────────────────────────────────────────────

func writeFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func testResolver(root string) Resolver {
	return Resolver{
		WorkspaceRoot: func(alias string) string {
			if alias == "wx" {
				return root
			}
			return ""
		},
		SessionPath: func(id string) string { return "" },
	}
}

func TestCodeVersionStableAndSensitive(t *testing.T) {
	root := t.TempDir()
	body := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13\nline14\nline15\n"
	writeFile(t, root, "src/a.cpp", body)
	r := testResolver(root)

	v1, err := r.ResolveEvidence(Evidence{Resource: "ws://wx/src/a.cpp#L5-L8"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v1, "lines-v1:") {
		t.Fatalf("unexpected token %q", v1)
	}
	// Same bytes, same token.
	v2, _ := r.ResolveEvidence(Evidence{Resource: "ws://wx/src/a.cpp#L5-L8"})
	if v1 != v2 {
		t.Fatal("version not deterministic")
	}
	// Editing a cited line changes the token.
	writeFile(t, root, "src/a.cpp", "line1\nline2\nline3\nline4\nlineX\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13\nline14\nline15\n")
	v3, _ := r.ResolveEvidence(Evidence{Resource: "ws://wx/src/a.cpp#L5-L8"})
	if v3 == v1 {
		t.Fatal("cited-line edit did not change version")
	}
	// Editing the 3-line context changes the token.
	writeFile(t, root, "src/a.cpp", "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9X\nline10\nline11\nline12\nline13\nline14\nline15\n")
	v4, _ := r.ResolveEvidence(Evidence{Resource: "ws://wx/src/a.cpp#L5-L8"})
	if v4 == v1 {
		t.Fatal("context edit did not change version")
	}
	// Editing beyond the context window (L8+3=L11) leaves the token alone.
	writeFile(t, root, "src/a.cpp", "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13X\nline14\nline15\n")
	v5, _ := r.ResolveEvidence(Evidence{Resource: "ws://wx/src/a.cpp#L5-L8"})
	if v5 != v1 {
		t.Fatal("edit beyond context window must not change version")
	}
}

func TestDocAndSessionVersions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/a.md", "# hello\n")
	r := testResolver(root)
	d1, err := r.ResolveEvidence(Evidence{Resource: "doc://wx/docs/a.md"})
	if err != nil || !strings.HasPrefix(d1, "doc-v1:") {
		t.Fatalf("doc version: %q %v", d1, err)
	}
	writeFile(t, root, "docs/a.md", "# hello v2\n")
	d2, _ := r.ResolveEvidence(Evidence{Resource: "doc://wx/docs/a.md"})
	if d1 == d2 {
		t.Fatal("doc edit did not change version")
	}

	sessionFile := writeFile(t, root, "sessions/s1.jsonl", "a\n")
	r2 := Resolver{
		WorkspaceRoot: r.WorkspaceRoot,
		SessionPath: func(id string) string {
			if id == "s1" {
				return sessionFile
			}
			return ""
		},
	}
	s1, err := r2.ResolveEvidence(Evidence{Resource: "session://s1"})
	if err != nil || !strings.HasPrefix(s1, "session-v1:") {
		t.Fatalf("session version: %q %v", s1, err)
	}
	// Append to the transcript.
	if f, err := os.OpenFile(sessionFile, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		f.WriteString("b\n")
		f.Close()
	}
	s2, _ := r2.ResolveEvidence(Evidence{Resource: "session://s1"})
	if s1 == s2 {
		t.Fatal("session append did not change version")
	}
}

func TestMissingEvidence(t *testing.T) {
	r := testResolver(t.TempDir())
	_, err := r.ResolveEvidence(Evidence{Resource: "ws://wx/gone.cpp#L1-L5"})
	if !errors.Is(err, ErrEvidenceMissing) {
		t.Fatalf("want ErrEvidenceMissing, got %v", err)
	}
	_, err = r.ResolveEvidence(Evidence{Resource: "ws://unknown/a.cpp#L1-L5"})
	if err == nil || strings.Contains(err.Error(), "evidence missing") {
		t.Fatalf("unknown workspace should not be evidence-missing: %v", err)
	}
}

// ── Store ──────────────────────────────────────────────────────────────

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreUpsertIdempotent(t *testing.T) {
	s := newTestStore(t)
	n, err := s.Upsert([]Claim{validClaim()})
	if err != nil || n != 1 {
		t.Fatalf("upsert: %d %v", n, err)
	}
	created := s.List(claimsFilter("rx101"))[0].CreatedAt
	// Same id again: no duplicate, CreatedAt preserved.
	n, err = s.Upsert([]Claim{validClaim()})
	if err != nil || n != 1 {
		t.Fatalf("re-upsert: %d %v", n, err)
	}
	got := s.List(claimsFilter("rx101"))
	if len(got) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(got))
	}
	if got[0].CreatedAt != created {
		t.Fatalf("CreatedAt changed on re-upsert: %q -> %q", created, got[0].CreatedAt)
	}
}

func claimsFilter(workspace string) Filter {
	return Filter{Workspace: workspace}
}

func TestStoreUpsertAllOrNothing(t *testing.T) {
	s := newTestStore(t)
	bad := validClaim()
	bad.ID = "claim.bad-kind"
	bad.Kind = "opinion"
	_, err := s.Upsert([]Claim{validClaim(), bad})
	if err == nil {
		t.Fatal("expected batch rejection")
	}
	if got := s.List(Filter{}); len(got) != 0 {
		t.Fatalf("rejected batch must not persist anything, got %d claims", len(got))
	}
}

func TestStoreUpsertClearsStale(t *testing.T) {
	s := newTestStore(t)
	s.Upsert([]Claim{validClaim()})
	if err := s.MarkStale("claim.rx101-pcie", "rx101", StaleReasonVersionChanged, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	c := s.List(claimsFilter("rx101"))[0]
	if c.Status != StatusStale || c.StaleSince == "" || c.StaleReason != StaleReasonVersionChanged {
		t.Fatalf("not stale: %+v", c)
	}
	// Agent re-verified: upsert with active status clears the flags.
	c2 := validClaim()
	s.Upsert([]Claim{c2})
	c = s.List(claimsFilter("rx101"))[0]
	if c.Status != StatusActive || c.StaleSince != "" || c.StaleReason != "" {
		t.Fatalf("stale flags not cleared on re-verify: %+v", c)
	}
}

func TestStoreRetract(t *testing.T) {
	s := newTestStore(t)
	s.Upsert([]Claim{validClaim()})
	n, err := s.Retract("rx101", []string{"claim.rx101-pcie", "claim.unknown"})
	if err != nil || n != 1 {
		t.Fatalf("retract: %d %v", n, err)
	}
	c := s.List(claimsFilter("rx101"))[0]
	if c.Status != StatusRetracted {
		t.Fatalf("status = %s", c.Status)
	}
	// Retracted claims are excluded from Touching.
	if ids := s.Touching("rx101/scripts/bringup.sh"); len(ids) != 0 {
		t.Fatalf("retracted claim must not be touched: %v", ids)
	}
}

func TestStoreTouching(t *testing.T) {
	s := newTestStore(t)
	s.Upsert([]Claim{
		validClaim(),
		Claim{
			ID: "claim.doc-a", Workspace: "rx101", Kind: KindDecision,
			Truth: TruthSourceVerified, Intent: IntentIntended,
			Text: "design doc says X", Evidence: []Evidence{{Resource: "doc://rx101/docs/a.md"}},
			Status: StatusActive,
		},
	})
	if ids := s.Touching("rx101/scripts/bringup.sh"); len(ids) != 1 || ids[0] != "claim.rx101-pcie" {
		t.Fatalf("code touching: %v", ids)
	}
	if ids := s.Touching("rx101/docs/a.md"); len(ids) != 1 || ids[0] != "claim.doc-a" {
		t.Fatalf("doc touching: %v", ids)
	}
	if ids := s.Touching("rx101/other.md"); len(ids) != 0 {
		t.Fatalf("unexpected touching: %v", ids)
	}
}

func TestStoreListFilters(t *testing.T) {
	s := newTestStore(t)
	s.Upsert([]Claim{
		validClaim(),
		Claim{ID: "claim.q", Workspace: "wx", Kind: KindQuestion, Truth: TruthUnknown, Intent: IntentUnknown,
			Text: "why?", DocID: "/abs/a.md", Status: StatusActive},
	})
	if got := s.List(Filter{Workspace: "wx"}); len(got) != 1 || got[0].ID != "claim.q" {
		t.Fatalf("workspace filter: %+v", got)
	}
	if got := s.List(Filter{Kind: "question"}); len(got) != 1 {
		t.Fatalf("kind filter: %+v", got)
	}
	if got := s.List(Filter{DocID: "/abs/a.md"}); len(got) != 1 {
		t.Fatalf("doc filter: %+v", got)
	}
	s.MarkStale("claim.q", "wx", StaleReasonEvidenceMissing, nil)
	if got := s.List(Filter{Status: "stale"}); len(got) != 1 || got[0].ID != "claim.q" {
		t.Fatalf("status filter: %+v", got)
	}
}

func TestStorePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claims.json")
	s1, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s1.Upsert([]Claim{validClaim()})
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.List(claimsFilter("rx101")); len(got) != 1 || got[0].Text != validClaim().Text {
		t.Fatalf("reload lost data: %+v", got)
	}
}

func TestStoreRejectsBadSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":99,"claims":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path); !errors.Is(err, ErrBadSchema) {
		t.Fatalf("want ErrBadSchema, got %v", err)
	}
}

func TestCheckClaimDetectsDrift(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/a.cpp", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	r := testResolver(root)
	c := Claim{
		ID: "claim.c", Workspace: "wx", Kind: KindFact, Truth: TruthCodeVerified,
		Intent: IntentIntended, Text: "t", Status: StatusActive,
		Evidence: []Evidence{{Resource: "ws://wx/src/a.cpp#L3-L5"}},
	}
	// Establish the version.
	v, err := r.ResolveEvidence(c.Evidence[0])
	if err != nil {
		t.Fatal(err)
	}
	c.Evidence[0].Version = v
	out := r.CheckClaim(c)
	if out.Stale {
		t.Fatalf("fresh claim reported stale: %+v", out)
	}
	// Drift the cited line.
	writeFile(t, root, "src/a.cpp", "1\n2\n3X\n4\n5\n6\n7\n8\n9\n10\n")
	out = r.CheckClaim(c)
	if !out.Stale || out.Reason != StaleReasonVersionChanged {
		t.Fatalf("drift not detected: %+v", out)
	}
	// Missing file.
	os.Remove(filepath.Join(root, "src/a.cpp"))
	out = r.CheckClaim(c)
	if !out.Stale || out.Reason != StaleReasonEvidenceMissing {
		t.Fatalf("missing evidence not detected: %+v", out)
	}
	// No evidence: trivially fresh.
	c.Evidence = nil
	if out := r.CheckClaim(c); out.Stale {
		t.Fatalf("evidence-less claim must be fresh: %+v", out)
	}
}
