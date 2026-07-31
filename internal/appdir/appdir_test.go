package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func silent(string, ...any) {}

func TestResolveMigratesTheLegacyDirectoryOnce(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, legacyName)
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	// A file that must survive: losing the token would lock every MCP writer out.
	if err := os.WriteFile(filepath.Join(legacy, "mcp-write-token"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Resolve(home, silent)
	want := filepath.Join(home, currentName)
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
	data, err := os.ReadFile(filepath.Join(want, "mcp-write-token"))
	if err != nil || string(data) != "secret" {
		t.Fatalf("migration must carry existing state: %q %v", data, err)
	}
	if isDir(legacy) {
		t.Fatalf("the legacy directory must be gone after a successful migration")
	}
	// Second call is a no-op that keeps returning the new directory.
	if again := Resolve(home, silent); again != want {
		t.Fatalf("second Resolve = %q, want %q", again, want)
	}
}

func TestResolvePrefersTheCurrentDirectoryAndKeepsLegacy(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(home, currentName)
	legacy := filepath.Join(home, legacyName)
	for _, dir := range []string{current, legacy} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, "todos.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}

	var warned bool
	got := Resolve(home, func(string, ...any) { warned = true })
	if got != current {
		t.Fatalf("Resolve = %q, want %q", got, current)
	}
	if !isDir(legacy) {
		t.Fatalf("an existing current directory must never consume the legacy one")
	}
	if !warned {
		t.Fatalf("a leftover legacy directory must be reported")
	}
}

func TestResolveReturnsCurrentWhenNothingExists(t *testing.T) {
	home := t.TempDir()
	if got, want := Resolve(home, silent), filepath.Join(home, currentName); got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveFallsBackToLegacyWhenMigrationFails(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, legacyName)
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	// A regular file where the new directory should go makes the rename fail.
	if err := os.WriteFile(filepath.Join(home, currentName), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}

	var warned bool
	got := Resolve(home, func(string, ...any) { warned = true })
	if got != legacy {
		t.Fatalf("a failed migration must keep serving the legacy directory, got %q", got)
	}
	if !warned {
		t.Fatalf("a failed migration must be reported")
	}
}

func TestPathJoinsUnderTheDataDirectory(t *testing.T) {
	if got, want := Path("wiki", "index.json"), filepath.Join(Dir(), "wiki", "index.json"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestDirFollowsHomeAndTheEnvOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvDataDir, "")
	// Dir must re-derive per call: tests and any process that rewrites its
	// environment rely on it, and a cached value would silently point a test at
	// the real home directory.
	if got, want := Dir(), filepath.Join(home, currentName); got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}

	custom := t.TempDir()
	t.Setenv(EnvDataDir, custom)
	if got := Dir(); got != custom {
		t.Fatalf("Dir = %q, want the override %q", got, custom)
	}
	if got, want := Path("wiki", "index.json"), filepath.Join(custom, "wiki", "index.json"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}
