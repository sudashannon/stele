package wiki

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"comet-ui/internal/source"
)

// Mirror manages a single git repository that mirrors all wiki-indexed
// documents from all workspaces. On file changes, documents are copied
// into <repoDir>/<workspace-alias>/<relative-path> and auto-committed
// after a debounce period so a burst of edits collapses into one commit.
// Original files are never modified — this is a one-way copy.
type Mirror struct {
	repoDir  string // e.g. ~/.comet-panel/knowledge-repo
	remote   string // optional push remote (empty disables push)
	debounce time.Duration

	mu      sync.Mutex
	pending map[string]string // source path -> dest path relative to repoDir
	timer   *time.Timer
}

// NewMirror constructs a Mirror rooted at repoDir. remote, if non-empty, is
// pushed to (as "origin") after every commit.
func NewMirror(repoDir, remote string) *Mirror {
	return &Mirror{
		repoDir:  repoDir,
		remote:   remote,
		debounce: 30 * time.Second,
		pending:  make(map[string]string),
	}
}

// Init ensures the mirror repo directory exists and is a git repository.
// It is idempotent — calling it against an already-initialized repo is a
// no-op.
func (m *Mirror) Init() error {
	if err := os.MkdirAll(m.repoDir, 0755); err != nil {
		return err
	}
	gitDir := filepath.Join(m.repoDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return nil // already initialized
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = m.repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SyncFile queues a single file for mirroring, keyed by its source path.
// workspace is the workspace alias and relPath is the file's path relative
// to that workspace's root; together they determine the mirrored file's
// location under repoDir. Queuing resets the debounce timer so a burst of
// changes collapses into a single commit once things settle.
//
// relPath must already be workspace-relative. An absolute or escaping
// relPath is refused rather than mirrored: the mirror repo is pushed to a
// remote, so a path outside the workspace would publish a file the user
// never registered.
func (m *Mirror) SyncFile(workspace, srcPath, relPath string) {
	if !mirrorableRelPath(relPath) {
		log.Printf("wiki mirror: refusing to mirror %s (path escapes workspace %q)", srcPath, workspace)
		return
	}
	destRel := filepath.Join(workspace, relPath)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[srcPath] = destRel
	m.resetTimerLocked()
}

// SyncAll queues every currently indexed component for mirroring. It is
// called after a full index rebuild so the mirror reflects the complete,
// current set of documents rather than only ones the watcher happened to
// see change (e.g. the initial sync after startup).
//
// Components that are not workspace documents are skipped: session
// components point at raw agent transcripts (hundreds of MB, containing
// tool output and credentials) that must never reach the mirror's remote.
func (m *Mirror) SyncAll(components map[string]Component, workspaces []WorkspaceConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	queued := 0
	for _, c := range components {
		if !MirrorableComponent(c) {
			continue
		}
		rel := relativeToWorkspace(c.Path, c.Workspace, workspaces)
		if !mirrorableRelPath(rel) {
			log.Printf("wiki mirror: refusing to mirror %s (path escapes workspace %q)", c.Path, c.Workspace)
			continue
		}
		m.pending[c.Path] = filepath.Join(c.Workspace, rel)
		queued++
	}
	if queued > 0 {
		m.resetTimerLocked()
	}
}

// MirrorableComponent reports whether a component is a workspace document
// that belongs in the knowledge mirror. Synthetic components derived from
// agent state are excluded.
func MirrorableComponent(c Component) bool {
	return c.Type != TypeSession
}

// mirrorableRelPath fails closed on anything that is not a plain relative
// path inside the workspace root.
func mirrorableRelPath(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// relativeToWorkspace computes a component path relative to the source's
// stable project root. OpenSpec uses the parent of openspec/; Trellis uses
// the registered repository root. It returns "" when the path lies outside
// that root so callers fail closed instead of mirroring an absolute path.
func relativeToWorkspace(path, alias string, workspaces []WorkspaceConfig) string {
	for _, workspace := range workspaces {
		if workspace.Alias != alias {
			continue
		}
		root := source.MirrorRoot(workspace)
		rel, err := filepath.Rel(root, path)
		if err == nil && mirrorableRelPath(rel) {
			return rel
		}
		break
	}
	return ""
}

// resetTimerLocked (re)starts the debounce timer. Caller must hold m.mu.
func (m *Mirror) resetTimerLocked() {
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(m.debounce, m.flush)
}

// flush copies every pending file into the mirror repo and, if anything
// was copied, commits (and optionally pushes) the result.
func (m *Mirror) flush() {
	m.mu.Lock()
	batch := m.pending
	m.pending = make(map[string]string)
	m.mu.Unlock()

	if len(batch) == 0 {
		return
	}

	copied := 0
	for src, destRel := range batch {
		dest := filepath.Join(m.repoDir, destRel)
		if err := copyFile(src, dest); err != nil {
			log.Printf("mirror: copy %s -> %s failed: %v", src, destRel, err)
			continue
		}
		copied++
	}

	if copied == 0 {
		return
	}

	if err := gitCmd(m.repoDir, "add", "-A"); err != nil {
		return
	}
	// Nothing staged (files copied but content unchanged from last commit)
	// — skip the commit rather than create an empty one.
	if !hasStagedChanges(m.repoDir) {
		return
	}

	log.Printf("mirror: committing %d file(s)", copied)
	msg := fmt.Sprintf("sync: %d file(s) updated", copied)
	if err := gitCmd(m.repoDir, "commit", "-m", msg); err != nil {
		return
	}

	if m.remote == "" {
		return
	}
	gitCmd(m.repoDir, "remote", "remove", "origin") // ignore error: may not exist yet
	if err := gitCmd(m.repoDir, "remote", "add", "origin", m.remote); err != nil {
		return
	}
	if err := gitCmd(m.repoDir, "push", "-u", "origin", "HEAD"); err != nil {
		log.Printf("mirror: push failed: %v", err)
	}
}

// hasStagedChanges reports whether the mirror repo's index differs from
// HEAD (or, for a brand-new repo with no commits yet, whether anything is
// staged at all).
func hasStagedChanges(dir string) bool {
	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return false // no diff
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode() == 1
	}
	// Unexpected failure (e.g. no HEAD yet on a brand-new repo): fall back
	// to checking the index directly.
	lsCmd := exec.Command("git", "diff", "--cached", "--stat")
	lsCmd.Dir = dir
	out, lsErr := lsCmd.Output()
	if lsErr != nil {
		return true // be conservative and attempt the commit
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// copyFile copies src to dst, creating dst's parent directories as needed.
// A missing src (e.g. the file was deleted between the change event and
// the debounced flush) is treated as a no-op, not an error.
func copyFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// gitCmd runs `git <args...>` in dir, logging (and returning) any failure.
func gitCmd(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("mirror: git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return err
}
