package main

import (
	"os"
	"path/filepath"
	"testing"

	"stele/internal/sessions"
)

func TestResolveSessionSourcesDefaultsToTheOMPLocation(t *testing.T) {
	root := t.TempDir()
	resolved, err := resolveSessionSources("", &repeatedFlag{}, root)
	if err != nil {
		t.Fatalf("resolveSessionSources: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Root != root || resolved[0].Provider.Name() != "omp" {
		t.Fatalf("resolved = %+v, want the OMP default", resolved)
	}
}

// --sessions-dir stays the OMP shorthand so an existing deployment keeps
// working after the multi-runtime change.
func TestResolveSessionSourcesHonoursTheOMPShorthand(t *testing.T) {
	custom := t.TempDir()
	resolved, err := resolveSessionSources(custom, &repeatedFlag{}, t.TempDir())
	if err != nil {
		t.Fatalf("resolveSessionSources: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Root != custom {
		t.Fatalf("resolved = %+v, want the flag's directory", resolved)
	}
}

func TestResolveSessionSourcesAcceptsSeveralRuntimes(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	flags := &repeatedFlag{}
	if err := flags.Set("omp=" + first); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set(" omp = " + second + " "); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveSessionSources("", flags, t.TempDir())
	if err != nil {
		t.Fatalf("resolveSessionSources: %v", err)
	}
	if len(resolved) != 2 || resolved[0].Root != first || resolved[1].Root != second {
		t.Fatalf("resolved = %+v, want both sources in order", resolved)
	}
}

// A directory that is not there must be silent: the panel runs on machines
// without a given runtime installed.
func TestResolveSessionSourcesSkipsMissingDirectories(t *testing.T) {
	resolved, err := resolveSessionSources("", &repeatedFlag{}, filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("resolveSessionSources: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("resolved = %+v, want none", resolved)
	}
}

func TestResolveSessionSourcesSkipsAFileMasqueradingAsARoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveSessionSources(file, &repeatedFlag{}, t.TempDir())
	if err != nil {
		t.Fatalf("resolveSessionSources: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("resolved = %+v, want none", resolved)
	}
}

// A misspelled runtime is a configuration error, not something to guess at.
func TestResolveSessionSourcesRejectsUnknownRuntimeAndBadSyntax(t *testing.T) {
	unknown := &repeatedFlag{}
	// A name no provider will ever answer to: registering claude-code proved the
	// abstraction, so the unknown case needs a genuinely unknown runtime.
	if err := unknown.Set("no-such-runtime=" + t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSessionSources("", unknown, t.TempDir()); err == nil {
		t.Fatal("an unregistered runtime must fail")
	}

	for _, raw := range []string{"omp", "=/tmp", "omp=", "  =  "} {
		malformed := &repeatedFlag{}
		if err := malformed.Set(raw); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveSessionSources("", malformed, t.TempDir()); err == nil {
			t.Fatalf("--sessions-source %q must fail", raw)
		}
	}
}

func TestRepeatedFlagStringJoinsValues(t *testing.T) {
	flags := &repeatedFlag{}
	if got := flags.String(); got != "" {
		t.Fatalf("String() = %q, want empty", got)
	}
	if err := flags.Set("omp=/a"); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set("omp=/b"); err != nil {
		t.Fatal(err)
	}
	if got := flags.String(); got != "omp=/a,omp=/b" {
		t.Fatalf("String() = %q", got)
	}
}

// The registry is what a configuration string resolves against; main must not
// keep its own list of runtime names.
func TestSessionProviderNamesComeFromTheRegistry(t *testing.T) {
	if _, ok := sessions.ProviderByName("omp"); !ok {
		t.Fatal("omp must be registered")
	}
	names := sessions.ProviderNames()
	if len(names) == 0 {
		t.Fatal("ProviderNames must not be empty")
	}
}
