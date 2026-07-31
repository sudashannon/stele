package wiki

import (
	"context"
	"errors"
	"os"
	"testing"

	"stele/chat"
)

func TestOverviewCacheKey(t *testing.T) {
	members := []Component{{ID: "b"}, {ID: "a"}, {ID: "c"}}
	key1 := overviewCacheKey(members)

	// Same members in different order should produce the same key.
	membersReordered := []Component{{ID: "c"}, {ID: "a"}, {ID: "b"}}
	key2 := overviewCacheKey(membersReordered)
	if key1 != key2 {
		t.Errorf("keys differ for same members in different order: %s vs %s", key1, key2)
	}

	// Different membership should produce a different key.
	membersChanged := []Component{{ID: "a"}, {ID: "b"}, {ID: "d"}}
	key3 := overviewCacheKey(membersChanged)
	if key1 == key3 {
		t.Error("keys should differ for different members")
	}
}

func TestGenerateOverview_TooSmall(t *testing.T) {
	_, err := GenerateOverview(context.Background(), 1, []Component{{ID: "a"}, {ID: "b"}}, t.TempDir())
	if err == nil {
		t.Error("expected error for <3 members")
	}
}

func TestGenerateOverview_CacheHit(t *testing.T) {
	dir := t.TempDir()
	members := []Component{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	key := overviewCacheKey(members)
	cachePath := overviewCachePath(dir, 42, key)

	cached := overviewPrefix + "cached overview body"
	if err := os.WriteFile(cachePath, []byte(cached), 0644); err != nil {
		t.Fatalf("failed to seed cache: %v", err)
	}

	got, err := GenerateOverview(context.Background(), 42, members, dir)
	if err != nil {
		t.Fatalf("unexpected error reading cache: %v", err)
	}
	if got != cached {
		t.Errorf("expected cached body %q, got %q", cached, got)
	}
}

func TestGenerateOverview_CacheMissOnMembershipChange(t *testing.T) {
	prevLoadConfig := chat.LoadConfig
	t.Cleanup(func() { chat.LoadConfig = prevLoadConfig })
	chat.LoadConfig = func() (*chat.Config, error) {
		return nil, errors.New("no config in test environment")
	}

	dir := t.TempDir()
	oldMembers := []Component{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	oldKey := overviewCacheKey(oldMembers)
	oldPath := overviewCachePath(dir, 7, oldKey)
	if err := os.WriteFile(oldPath, []byte(overviewPrefix+"stale"), 0644); err != nil {
		t.Fatalf("failed to seed stale cache: %v", err)
	}

	newMembers := []Component{{ID: "a"}, {ID: "b"}, {ID: "d"}}
	newKey := overviewCacheKey(newMembers)
	if newKey == oldKey {
		t.Fatal("test setup invalid: keys should differ")
	}
	newPath := overviewCachePath(dir, 7, newKey)

	// No cache entry exists for the new membership, so GenerateOverview
	// must attempt to generate. With chat.LoadConfig mocked to fail, it
	// must surface that error rather than silently returning stale data
	// or writing a partial cache entry.
	_, err := GenerateOverview(context.Background(), 7, newMembers, dir)
	if err == nil {
		t.Fatal("expected error when chat.LoadConfig fails")
	}
	if _, statErr := os.Stat(newPath); statErr == nil {
		t.Error("did not expect a cache file to be written on generation failure")
	}
	if _, statErr := os.Stat(oldPath); statErr != nil {
		t.Error("stale cache for the OLD membership should remain untouched when generation fails before cleanup runs")
	}
}
