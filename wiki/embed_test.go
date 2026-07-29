package wiki

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddingCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")

	original := map[string]EmbeddingEntry{
		"a": {ID: "a", InputVersion: EmbeddingInputVersion, Vector: make([]float32, 384)},
		"b": {ID: "b", InputVersion: EmbeddingInputVersion, Vector: make([]float32, 384)},
	}
	original["a"].Vector[0] = 1.0
	original["a"].Vector[383] = -0.5
	original["b"].Vector[100] = 0.7
	originalA := original["a"]
	originalA.ContentHash[0] = 42
	original["a"] = originalA

	if err := SaveEmbeddingCache(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadEmbeddingCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded))
	}
	if loaded["a"].Vector[0] != 1.0 || loaded["a"].Vector[383] != -0.5 {
		t.Error("vector a mismatch")
	}
	if loaded["a"].ContentHash[0] != 42 || loaded["a"].InputVersion != EmbeddingInputVersion {
		t.Error("entry a metadata mismatch")
	}
	if loaded["b"].Vector[100] != 0.7 {
		t.Error("vector b mismatch")
	}
}

func TestComputeEmbeddingEntriesEmpty(t *testing.T) {
	result, err := ComputeEmbeddingEntries(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}

func TestEmbeddingEntryMatchesContentAndInputVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "design.md")
	if err := os.WriteFile(path, []byte("# Design\n\nfirst body"), 0o644); err != nil {
		t.Fatal(err)
	}
	component := Component{ID: path, Path: path, Title: "Design"}
	entry := EmbeddingEntry{
		ID:           path,
		ContentHash:  EmbeddingFingerprint(component),
		InputVersion: EmbeddingInputVersion,
		Vector:       []float32{1},
	}
	if !EmbeddingEntryMatches(component, entry) {
		t.Fatal("fresh entry should match")
	}
	if err := os.WriteFile(path, []byte("# Design\n\nchanged body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if EmbeddingEntryMatches(component, entry) {
		t.Fatal("changed content reused a stale vector")
	}
	entry.ContentHash = EmbeddingFingerprint(component)
	entry.InputVersion--
	if EmbeddingEntryMatches(component, entry) {
		t.Fatal("old semantic input version reused a stale vector")
	}
}

func TestLoadEmbeddingCacheRejectsLegacyFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.bin")
	if err := os.WriteFile(path, []byte("CPE1legacy-cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEmbeddingCache(path); !errors.Is(err, ErrIncompatibleEmbeddingCache) {
		t.Fatalf("want ErrIncompatibleEmbeddingCache, got %v", err)
	}
}
