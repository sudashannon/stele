package wiki

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMirror_Init(t *testing.T) {
	dir := t.TempDir()
	m := NewMirror(dir, "")
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Error("expected .git dir")
	}
	// Second init should be idempotent
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
}

func TestMirror_CopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.md")
	if err := os.WriteFile(src, []byte("# Hello"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "repo", "ws", "src.md")
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# Hello" {
		t.Errorf("content mismatch: %s", content)
	}
}

func TestMirror_CopyFile_MissingSourceIsNoop(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "gone.md")
	dst := filepath.Join(dir, "repo", "ws", "gone.md")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("expected nil error for missing source, got %v", err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("expected dst not to be created")
	}
}

// flushNow runs the debounced flush synchronously, bypassing the timer, so
// tests don't need to sleep for the 30s debounce window.
func flushNow(m *Mirror) {
	m.mu.Lock()
	if m.timer != nil {
		m.timer.Stop()
	}
	m.mu.Unlock()
	m.flush()
}

func TestMirror_SyncFile_CopiesAndCommits(t *testing.T) {
	srcDir := t.TempDir()
	repoDir := t.TempDir()

	src := filepath.Join(srcDir, "doc.md")
	if err := os.WriteFile(src, []byte("# Doc"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewMirror(repoDir, "")
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}

	m.SyncFile("myws", src, "doc.md")
	flushNow(m)

	dst := filepath.Join(repoDir, "myws", "doc.md")
	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("expected mirrored file: %v", err)
	}
	if string(content) != "# Doc" {
		t.Errorf("content mismatch: %s", content)
	}

	// A commit should now exist.
	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %v (%s)", err, out)
	}
	if len(out) == 0 {
		t.Error("expected at least one commit")
	}
}

func TestMirror_Flush_NoChangesSkipsCommit(t *testing.T) {
	srcDir := t.TempDir()
	repoDir := t.TempDir()

	src := filepath.Join(srcDir, "doc.md")
	if err := os.WriteFile(src, []byte("# Doc"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewMirror(repoDir, "")
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}

	m.SyncFile("myws", src, "doc.md")
	flushNow(m)

	countCommits := func() int {
		cmd := exec.Command("git", "log", "--oneline")
		cmd.Dir = repoDir
		out, _ := cmd.CombinedOutput()
		if len(out) == 0 {
			return 0
		}
		lines := 0
		for _, b := range out {
			if b == '\n' {
				lines++
			}
		}
		return lines
	}

	first := countCommits()
	if first == 0 {
		t.Fatal("expected first commit")
	}

	// Sync the same unchanged file again — no diff, so no new commit.
	m.SyncFile("myws", src, "doc.md")
	flushNow(m)

	second := countCommits()
	if second != first {
		t.Errorf("expected no new commit for unchanged content: before=%d after=%d", first, second)
	}
}

func TestMirror_SyncAll_PreservesWorkspaceStructure(t *testing.T) {
	wsRoot := t.TempDir()
	openspecDir := filepath.Join(wsRoot, "openspec")
	if err := os.MkdirAll(openspecDir, 0755); err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(wsRoot, "docs", "a.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	repoDir := t.TempDir()
	m := NewMirror(repoDir, "")
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}

	components := map[string]Component{
		docPath: {ID: docPath, Path: docPath, Workspace: "myws"},
	}
	workspaces := []WorkspaceConfig{
		{Alias: "myws", Path: openspecDir},
	}

	m.SyncAll(components, workspaces)
	flushNow(m)

	dst := filepath.Join(repoDir, "myws", "docs", "a.md")
	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("expected mirrored file at %s: %v", dst, err)
	}
	if string(content) != "content" {
		t.Errorf("content mismatch: %s", content)
	}
}

func TestRelativeToWorkspace_TrellisUsesRegisteredRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".trellis", "spec", "backend", "index.md")
	got := relativeToWorkspace(path, "trellis", []WorkspaceConfig{{Alias: "trellis", Path: root, Type: "trellis"}})
	want := filepath.Join(".trellis", "spec", "backend", "index.md")
	if got != want {
		t.Fatalf("relative path = %q, want %q", got, want)
	}
}

func TestRelativeToWorkspaceSuperpowersUsesProjectRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "superpowers", "specs", "2026-07-20-cache-design.md")
	got := relativeToWorkspace(path, "ideas", []WorkspaceConfig{{Alias: "ideas", Path: root, Type: "superpowers"}})
	want := filepath.Join("docs", "superpowers", "specs", "2026-07-20-cache-design.md")
	if got != want {
		t.Fatalf("relative path = %q, want %q", got, want)
	}
}
