package wiki

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanComponents_FindsMarkdownFilesAndExtractsTitle(t *testing.T) {
	root := t.TempDir()
	changesDir := filepath.Join(root, "changes", "my-change")
	os.MkdirAll(changesDir, 0755)
	os.WriteFile(filepath.Join(changesDir, "proposal.md"), []byte("# My Change Proposal\n\nBody text.\n"), 0644)
	os.WriteFile(filepath.Join(changesDir, "design.md"), []byte("# Design Doc\n\nBody.\n"), 0644)

	components, err := ScanComponents(root, "miao")
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d: %+v", len(components), components)
	}

	byTitle := map[string]Component{}
	for _, c := range components {
		byTitle[c.Title] = c
	}
	prop, ok := byTitle["My Change Proposal"]
	if !ok {
		t.Fatalf("expected a component titled 'My Change Proposal', got %+v", components)
	}
	if prop.Type != TypeProposal {
		t.Fatalf("expected TypeProposal, got %v", prop.Type)
	}
	if prop.Workspace != "miao" {
		t.Fatalf("expected workspace 'miao', got %q", prop.Workspace)
	}
}

func TestScanComponents_FallsBackToFilenameWhenNoHeading(t *testing.T) {
	root := t.TempDir()
	// must be under a recognized directory ("specs") to be classified —
	// classifyPath does not recognize arbitrary directory names like "docs"
	dir := filepath.Join(root, "docs", "superpowers", "specs")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte("no heading here, just text\n"), 0644)

	components, err := ScanComponents(root, "miao")
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || components[0].Title != "notes" {
		t.Fatalf("expected fallback title 'notes', got %+v", components)
	}
}

func TestScanComponents_SkipsMalformedFileWithoutAbortingWholeScan(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "superpowers", "specs")
	os.MkdirAll(dir, 0755)
	// malformed frontmatter: unclosed YAML flow sequence — yaml.Unmarshal will error
	os.WriteFile(filepath.Join(dir, "broken.md"), []byte("---\ntags: [unclosed\n---\n# Broken\n"), 0644)
	os.WriteFile(filepath.Join(dir, "good.md"), []byte("# Good Doc\n"), 0644)

	components, err := ScanComponents(root, "miao")
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || components[0].Title != "Good Doc" {
		t.Fatalf("expected the malformed file to be skipped and the good file still indexed, got %+v", components)
	}
}

func TestScanComponents_SkipsPermissionDeniedPathWithoutAbortingWholeScan(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks bypassed as root")
	}

	root := t.TempDir()
	dir := filepath.Join(root, "docs", "superpowers", "specs")
	os.MkdirAll(dir, 0755)

	// "a_restricted" sorts lexically before "z_good.md" so filepath.Walk
	// (which visits directory entries in sorted order) hits the
	// permission-denied directory BEFORE the readable file — this is what
	// makes the pre-fix bug (abort-whole-walk) actually lose the readable
	// component instead of merely returning it alongside a non-nil error.
	restrictedDir := filepath.Join(dir, "a_restricted")
	os.MkdirAll(restrictedDir, 0755)
	os.WriteFile(filepath.Join(restrictedDir, "secret.md"), []byte("# Secret Doc\n"), 0644)
	if err := os.Chmod(restrictedDir, 0000); err != nil {
		t.Fatalf("could not chmod restrictedDir to 0000: %v", err)
	}
	// Restore permissions before t.TempDir()'s cleanup runs, otherwise a
	// 0000 directory may not be removable.
	t.Cleanup(func() {
		os.Chmod(restrictedDir, 0755)
	})

	os.WriteFile(filepath.Join(dir, "z_good.md"), []byte("# Good Doc\n"), 0644)

	components, err := ScanComponents(root, "miao")
	if err != nil {
		t.Fatalf("expected no error (permission-denied paths must be skipped, not fatal), got: %v", err)
	}
	if len(components) != 1 || components[0].Title != "Good Doc" {
		t.Fatalf("expected the readable file past the restricted dir to still be found, got %+v", components)
	}
}

func TestScanComponents_SkipsExcludedDirectories(t *testing.T) {
	root := t.TempDir()
	// Create files that WOULD be classified if not excluded:
	for _, dir := range []string{".git/specs", "node_modules/specs", ".hidden/specs", "rootfs/usr/share/doc/specs"} {
		d := filepath.Join(root, dir)
		os.MkdirAll(d, 0755)
		os.WriteFile(filepath.Join(d, "should-be-skipped.md"), []byte("# Skipped\n"), 0644)
	}
	// One file that SHOULD be found (not under an excluded dir):
	goodDir := filepath.Join(root, "docs", "superpowers", "specs")
	os.MkdirAll(goodDir, 0755)
	os.WriteFile(filepath.Join(goodDir, "real-spec.md"), []byte("# Real Spec\n"), 0644)

	components, err := ScanComponents(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || components[0].Title != "Real Spec" {
		t.Fatalf("expected only the non-excluded spec, got %d: %+v", len(components), components)
	}
}

func TestScanComponents_ParsesFrontmatter(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "superpowers", "specs")
	os.MkdirAll(dir, 0755)
	content := "---\ntags: [rx101, secure-boot]\nreviewed: true\n---\n# Titled Doc\n"
	os.WriteFile(filepath.Join(dir, "doc.md"), []byte(content), 0644)

	components, err := ScanComponents(root, "miao")
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(components))
	}
	fm := components[0].Frontmatter
	if fm["reviewed"] != true {
		t.Fatalf("expected frontmatter reviewed=true, got %+v", fm)
	}
}

func TestScanComponentsSkipsSymlinkedDocuments(t *testing.T) {
	root := t.TempDir()
	specsDir := filepath.Join(root, "docs", "superpowers", "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("# Outside Secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(specsDir, "secret.md")); err != nil {
		t.Fatal(err)
	}

	components, err := ScanComponents(root, "ideas")
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 0 {
		t.Fatalf("symlinked documents must not cross the scan root: %+v", components)
	}
}
