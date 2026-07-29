package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveKindAndRoots(t *testing.T) {
	openRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(openRoot, "openspec", "changes"), 0o755); err != nil {
		t.Fatal(err)
	}
	kind, err := ResolveKind(openRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindOpenSpec {
		t.Fatalf("expected OpenSpec detection, got %q", kind)
	}
	if got := OpenSpecPath(openRoot); got != filepath.Join(openRoot, "openspec") {
		t.Fatalf("unexpected OpenSpec path: %q", got)
	}

	trellisRoot := t.TempDir()
	for _, dir := range []string{"tasks", "spec", "workspace", ".runtime"} {
		if err := os.MkdirAll(filepath.Join(trellisRoot, ".trellis", dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	kind, err = ResolveKind(trellisRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindTrellis {
		t.Fatalf("expected Trellis detection, got %q", kind)
	}
	roots := ScanRoots(Workspace{Path: trellisRoot, Type: KindTrellis})
	if len(roots) != 3 {
		t.Fatalf("expected tasks/spec/workspace roots only, got %v", roots)
	}
	for _, root := range roots {
		if filepath.Base(root) == ".runtime" {
			t.Fatalf("runtime state must not be indexed: %v", roots)
		}
	}
	watchRoots := WatchRoots(Workspace{Path: trellisRoot, Type: KindTrellis})
	if len(watchRoots) != 1 || watchRoots[0] != filepath.Join(trellisRoot, ".trellis") {
		t.Fatalf("expected .trellis watcher root, got %v", watchRoots)
	}
}

func TestResolveKindPrefersLegacyOpenSpecUnlessExplicit(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"openspec/changes", ".trellis/tasks"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	kind, err := ResolveKind(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindOpenSpec {
		t.Fatalf("legacy auto-detection must prefer OpenSpec, got %q", kind)
	}
	kind, err = ResolveKind(root, KindTrellis)
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindTrellis {
		t.Fatalf("explicit Trellis type must win, got %q", kind)
	}
}

func TestResolveKindDetectsStrictSuperpowersWorkspace(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"specs", "plans", "artifacts", "reports"} {
		if err := os.MkdirAll(filepath.Join(root, "docs", "superpowers", dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	knowledgeRoot := filepath.Join(root, "knowledge")
	if err := os.MkdirAll(knowledgeRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	kind, err := ResolveKind(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindSuperpowers {
		t.Fatalf("expected Superpowers detection, got %q", kind)
	}

	workspace := Workspace{Path: root, Type: KindSuperpowers}
	roots := ScanRoots(workspace)
	if len(roots) != 5 {
		t.Fatalf("expected four workflow roots plus knowledge, got %v", roots)
	}
	expectedRoots := map[string]bool{
		filepath.Join(root, "docs", "superpowers", "specs"):     true,
		filepath.Join(root, "docs", "superpowers", "plans"):     true,
		filepath.Join(root, "docs", "superpowers", "artifacts"): true,
		filepath.Join(root, "docs", "superpowers", "reports"):   true,
		knowledgeRoot: true,
	}
	for _, scanRoot := range roots {
		if !expectedRoots[scanRoot] {
			t.Fatalf("unexpected Superpowers scan root %q", scanRoot)
		}
	}
	watchRoots := WatchRoots(workspace)
	wantSuperpowersWatchRoot := filepath.Join(root, "docs", "superpowers")
	if len(watchRoots) != 2 || watchRoots[0] != wantSuperpowersWatchRoot || watchRoots[1] != knowledgeRoot {
		t.Fatalf("expected Superpowers and knowledge watcher roots, got %v", watchRoots)
	}
	if ProjectRoot(workspace) != root || MirrorRoot(workspace) != root {
		t.Fatalf("Superpowers roots must stay at project root: project=%q mirror=%q", ProjectRoot(workspace), MirrorRoot(workspace))
	}
}

func TestResolveKindNeverTreatsMixedWorkflowRootAsSuperpowers(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"openspec/changes", "docs/superpowers/specs"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	kind, err := ResolveKind(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if kind != KindOpenSpec {
		t.Fatalf("OpenSpec must own a mixed workflow root, got %q", kind)
	}
	if _, err := ResolveKind(root, KindSuperpowers); err == nil {
		t.Fatal("explicit Superpowers must reject a root that also contains OpenSpec")
	}

	docsPath := filepath.Join(root, "docs", "superpowers")
	if _, err := ResolveKind(docsPath, KindSuperpowers); err == nil {
		t.Fatal("docs/superpowers itself must not be accepted as a project root")
	}
}

func TestResolveKindRejectsSuperpowersRootsThatEscapeThroughSymlinks(t *testing.T) {
	root := t.TempDir()
	superpowersDir := filepath.Join(root, "docs", "superpowers")
	if err := os.MkdirAll(superpowersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideSpecs := filepath.Join(t.TempDir(), "specs")
	if err := os.MkdirAll(outsideSpecs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSpecs, filepath.Join(superpowersDir, "specs")); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveKind(root, KindSuperpowers); err == nil {
		t.Fatal("Superpowers source roots must not escape the project root through symlinks")
	}
	workspace := Workspace{Path: root, Type: KindSuperpowers}
	if roots := ScanRoots(workspace); len(roots) != 0 {
		t.Fatalf("escaping source roots must not be scanned: %v", roots)
	}
	if roots := WatchRoots(workspace); len(roots) != 0 {
		t.Fatalf("escaping source roots must not be watched: %v", roots)
	}
}
