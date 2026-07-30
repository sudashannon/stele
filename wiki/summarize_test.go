package wiki

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSummarize_ReturnsCachedResultWhenFresh(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "doc.md")
	os.WriteFile(srcPath, []byte("# Doc\ncontent"), 0644)

	cacheDir := filepath.Join(root, ".wiki", "summaries")
	os.MkdirAll(cacheDir, 0755)

	comp := Component{ID: srcPath, Path: srcPath, Title: "Doc", UpdatedAt: time.Now()}
	cachePath := summaryCachePath(cacheDir, comp.ID)
	os.WriteFile(cachePath, []byte("cached summary text"), 0644)
	// ensure cache mtime is newer than source
	future := time.Now().Add(time.Hour)
	os.Chtimes(cachePath, future, future)

	got, err := Summarize(context.Background(), comp, cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "cached summary text" {
		t.Fatalf("expected cached summary, got %q", got)
	}
}

func TestSummaryCachePath_IsStableAndFilenameSafe(t *testing.T) {
	p1 := summaryCachePath("/cache", "/some/path/design.md")
	p2 := summaryCachePath("/cache", "/some/path/design.md")
	if p1 != p2 {
		t.Fatal("expected the same component ID to always produce the same cache path")
	}
	if filepath.Dir(p1) != "/cache" {
		t.Fatalf("expected cache path under /cache, got %q", p1)
	}
}

// CachedSummary is the read-only half of the cache: the viewer probes it on
// open, so a miss must never fall through to generation (which would bill an
// LLM call for merely looking at a document).
func TestCachedSummary(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "doc.md")
	if err := os.WriteFile(srcPath, []byte("# Doc\ncontent"), 0644); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(root, ".wiki", "summaries")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	comp := Component{ID: srcPath, Path: srcPath, Title: "Doc", UpdatedAt: time.Now()}

	if _, ok := CachedSummary(comp, cacheDir); ok {
		t.Fatal("expected a miss before anything is cached")
	}

	cachePath := summaryCachePath(cacheDir, comp.ID)
	if err := os.WriteFile(cachePath, []byte("cached summary text"), 0644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	os.Chtimes(cachePath, future, future)

	got, ok := CachedSummary(comp, cacheDir)
	if !ok || got != "cached summary text" {
		t.Fatalf("CachedSummary = (%q, %v), want the cached text", got, ok)
	}

	// Editing the document must invalidate the cache rather than serve a stale
	// summary, matching Summarize's mtime rule.
	stale := time.Now().Add(2 * time.Hour)
	os.Chtimes(srcPath, stale, stale)
	if _, ok := CachedSummary(comp, cacheDir); ok {
		t.Fatal("expected a miss once the source is newer than the cache")
	}
}
