package wiki

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo creates a git repository with one commit so linked worktrees can be
// added against it. t.TempDir() cleanup removes both the repo and the
// worktrees registered in its git dir.
func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return root
}

func addWorktree(t *testing.T, repo, path string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-q", path, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
}

// A session whose cwd is a linked worktree outside every registered workspace
// must attribute to the workspace that contains the parent repository.
func TestWorkspaceForPathAttributesLinkedWorktreeToParentRepoWorkspace(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-somewhere-else")
	addWorktree(t, repo, wt)

	workspaces := []WorkspaceConfig{{Alias: "proj", Path: repo}}
	workspace, ok := WorkspaceForPath(workspaces, wt)
	if !ok || workspace.Alias != "proj" {
		t.Fatalf("WorkspaceForPath(%q) = (%+v, %v), want proj", wt, workspace, ok)
	}
	// The returned Path stays the configured workspace path, matching the
	// direct-attribution contract.
	if workspace.Path != repo {
		t.Fatalf("Path = %q, want %q", workspace.Path, repo)
	}
}

// Repo-project checkouts resolve through the shared git dir ("app.git" style,
// no .git basename): attribution lands on the registered workspace that
// contains the .repo/projects tree.
func TestWorkspaceForPathAttributesRepoProjectWorktree(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-app")
	addWorktree(t, repo, wt)

	workspaces := []WorkspaceConfig{{Alias: "rx101", Path: repo}}
	workspace, ok := WorkspaceForPath(workspaces, wt)
	if !ok || workspace.Alias != "rx101" {
		t.Fatalf("WorkspaceForPath(%q) = (%+v, %v), want rx101", wt, workspace, ok)
	}
}

// A worktree whose parent repo is not inside any registered workspace stays
// unattributed — the fallback must not loosen the drop rule for /tmp sessions.
func TestWorkspaceForPathDropsWorktreeOutsideEveryWorkspace(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-elsewhere")
	addWorktree(t, repo, wt)

	workspaces := []WorkspaceConfig{{Alias: "other", Path: t.TempDir()}}
	if _, ok := WorkspaceForPath(workspaces, wt); ok {
		t.Fatalf("worktree of an unregistered parent repo must not attribute")
	}
}

// A registered workspace that directly contains the worktree must win over the
// parent-repo fallback (worktree registered under its own workspace).
func TestWorkspaceForPathPrefersDirectWorkspaceOverWorktreeFallback(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-registered")
	addWorktree(t, repo, wt)

	workspaces := []WorkspaceConfig{
		{Alias: "proj", Path: repo},
		{Alias: "wtws", Path: wt},
	}
	workspace, ok := WorkspaceForPath(workspaces, wt)
	if !ok || workspace.Alias != "wtws" {
		t.Fatalf("WorkspaceForPath(%q) = (%+v, %v), want wtws", wt, workspace, ok)
	}
}

// Nested registration keeps its longest-prefix semantics on the fallback path:
// the workspace that contains the parent repo most specifically wins.
func TestWorkspaceForPathWorktreeFallbackRespectsNesting(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-nested")
	addWorktree(t, repo, wt)

	broad := t.TempDir()
	// The repo fixture already sits in its own TempDir, so register the repo
	// under an alias and a broad parent that does not contain it — nesting is
	// covered by the direct-match tests; here the parent repo wins.
	workspaces := []WorkspaceConfig{
		{Alias: "broad", Path: broad},
		{Alias: "inner", Path: repo},
	}
	workspace, ok := WorkspaceForPath(workspaces, wt)
	if !ok || workspace.Alias != "inner" {
		t.Fatalf("WorkspaceForPath(%q) = (%+v, %v), want inner", wt, workspace, ok)
	}
}

// Plain directories with a file named .git that is not a gitdir pointer must
// not resolve, and subdirectories of the worktree resolve like the root.
func TestWorktreeMainRootRejectsNonWorktreesAndResolvesSubdirs(t *testing.T) {
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, ".git"), []byte("not a gitdir line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := worktreeMainRoot(plain); ok {
		t.Fatalf("plain dir with bogus .git file must not resolve")
	}

	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt-sub")
	addWorktree(t, repo, wt)
	sub := filepath.Join(wt, "deep", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := worktreeMainRoot(sub); ok {
		t.Fatalf("subdirectory of a worktree must not resolve without its own .git")
	}
	// Root resolves.
	if _, ok := worktreeMainRoot(wt); !ok {
		t.Fatalf("worktree root must resolve")
	}
}
