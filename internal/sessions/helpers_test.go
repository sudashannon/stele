package sessions

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// mustTime parses an RFC3339 instant for a fixture.
func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed.UTC()
}

func minute(n int) time.Duration { return time.Duration(n) * time.Minute }

func fmtName(n int) string { return fmt.Sprintf("part-%02d", n) }

// appendLines grows a transcript the way a running agent does.
func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, line := range lines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// storedPart reaches into the cache for one transcript's own digest, which is
// where per-file resume state lives after a merge.
func storedPart(t *testing.T, store *Store, primary, path string) *Digest {
	t.Helper()
	store.mu.RLock()
	defer store.mu.RUnlock()
	for _, unit := range store.units {
		if unit.Primary != primary {
			continue
		}
		digest, ok := unit.Parts[path]
		if !ok {
			t.Fatalf("no cached digest for %s", path)
		}
		return digest
	}
	t.Fatalf("no cached unit for %s", primary)
	return nil
}
