package superpowers

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDoc(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func recordsByName(records []Record) map[string]Record {
	out := make(map[string]Record, len(records))
	for _, record := range records {
		out[record.Name] = record
	}
	return out
}

func TestScanGroupsExactArtifactsAndInfersBuildProgress(t *testing.T) {
	root := t.TempDir()
	designPath := writeDoc(t, root, "docs/superpowers/specs/2026-07-20-cache-redesign-design.md", `---
superpowers_id: cache-redesign
---
# Cache Redesign

## Goal
Reduce cache misses.
`)
	writeDoc(t, root, "docs/superpowers/plans/2026-07-21-cache-redesign.md", `---
design-doc: docs/superpowers/specs/2026-07-20-cache-redesign-design.md
---
# Cache Redesign Implementation Plan

- [x] Add cache key
- [ ] Cover eviction

`+"```markdown\n- [x] example only\n```\n")
	writeDoc(t, root, "docs/superpowers/artifacts/cache-redesign/verify-scope-review.md", "# Scope Review\n")

	records, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one grouped work item, got %+v", records)
	}
	record := records[0]
	if record.Name != "cache-redesign" || record.Title != "Cache Redesign" {
		t.Fatalf("unexpected identity: %+v", record)
	}
	if record.Phase != PhaseBuild || record.Archived {
		t.Fatalf("expected active build phase, got phase=%q archived=%v", record.Phase, record.Archived)
	}
	if record.TasksCompleted != 1 || record.TasksTotal != 2 {
		t.Fatalf("expected plan checkbox progress 1/2, got %d/%d", record.TasksCompleted, record.TasksTotal)
	}
	if len(record.Specs) != 1 || len(record.Plans) != 1 || len(record.Artifacts) != 1 || len(record.Reports) != 0 {
		t.Fatalf("unexpected grouped documents: %+v", record)
	}
	if record.AnchorPath != designPath {
		t.Fatalf("expected design document anchor %q, got %q", designPath, record.AnchorPath)
	}
}

func TestScanPassCompletesAndFailRemainsInVerify(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"pass-item", "fail-item"} {
		writeDoc(t, root, "docs/superpowers/specs/2026-07-20-"+name+"-design.md", "# "+name+"\n")
		writeDoc(t, root, "docs/superpowers/plans/2026-07-21-"+name+".md", "# Plan\n\n- [x] Implement\n")
	}
	writeDoc(t, root, "docs/superpowers/reports/2026-07-22-pass-item-verify.md", "# Verification\n\n## 结论：PASS\n")
	writeDoc(t, root, "docs/superpowers/reports/2026-07-22-fail-item-verify.md", "# Verification\n\n## Result: FAIL\n")

	records, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	byName := recordsByName(records)
	passed := byName["pass-item"]
	if passed.VerifyResult != VerifyPass || passed.Phase != PhaseCompleted || !passed.Archived {
		t.Fatalf("PASS report must complete the item: %+v", passed)
	}
	failed := byName["fail-item"]
	if failed.VerifyResult != VerifyFail || failed.Phase != PhaseVerify || failed.Archived {
		t.Fatalf("FAIL report must remain active in verify: %+v", failed)
	}
}

func TestScanUsesExplicitDesignReferenceWithoutFuzzyMerging(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/superpowers/specs/2026-07-20-alpha-design.md", "# Alpha\n")
	writeDoc(t, root, "docs/superpowers/plans/2026-07-21-renamed-plan.md", `---
design-doc: docs/superpowers/specs/2026-07-20-alpha-design.md
---
# Renamed Plan
`)
	writeDoc(t, root, "docs/superpowers/plans/2026-07-21-alpha-extra.md", "# Alpha Extra\n")

	records, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	byName := recordsByName(records)
	if len(byName) != 2 {
		t.Fatalf("expected exact alpha and alpha-extra records, got %+v", records)
	}
	if len(byName["alpha"].Specs) != 1 || len(byName["alpha"].Plans) != 1 {
		t.Fatalf("design-doc reference must join renamed plan to alpha: %+v", byName["alpha"])
	}
	if len(byName["alpha-extra"].Plans) != 1 {
		t.Fatalf("similar slug must remain separate: %+v", byName["alpha-extra"])
	}
}

func TestScanSkipsSymlinkedDocuments(t *testing.T) {
	root := t.TempDir()
	specsDir := filepath.Join(root, "docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := writeDoc(t, t.TempDir(), "secret-design.md", "# Outside Secret\n")
	if err := os.Symlink(outside, filepath.Join(specsDir, "2026-07-20-secret-design.md")); err != nil {
		t.Fatal(err)
	}

	records, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("symlinked documents must not cross the Superpowers root: %+v", records)
	}
}

func TestScanRejectsSuperpowersTreeOutsideProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeDoc(t, outside, "specs/2026-07-20-secret-design.md", "# Outside Secret\n")
	if err := os.Symlink(outside, filepath.Join(root, "docs", "superpowers")); err != nil {
		t.Fatal(err)
	}

	if _, err := Scan(root); err == nil {
		t.Fatal("Superpowers tree resolving outside the project root must be rejected")
	}
}

func TestScanPreservesExplicitSuperpowersIdentityVerbatim(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/superpowers/specs/2026-07-20-unrelated-design.md", `---
superpowers_id: cache-design
---
# Cache Design
`)
	writeDoc(t, root, "docs/superpowers/plans/2026-07-21-unrelated.md", `---
superpowers_id: cache-design
---
# Cache Plan
`)

	records, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Name != "cache-design" {
		t.Fatalf("explicit Superpowers identity must be stable and exact: %+v", records)
	}
}

func TestScanExecutionEvidenceAdvancesItemToBuild(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/superpowers/specs/2026-07-20-cache-design.md", "# Cache\n")
	writeDoc(t, root, "docs/superpowers/artifacts/cache/review.md", "# Execution Review\n")

	records, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Phase != PhaseBuild {
		t.Fatalf("execution evidence must place the work item in build: %+v", records)
	}
}
