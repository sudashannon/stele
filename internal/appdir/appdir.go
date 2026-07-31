// Package appdir resolves the panel's own data directory.
//
// The directory holds the panel's configuration and derived state:
// workspaces.yaml, todos.json, the MCP write token, the wiki index cache and
// the knowledge mirror. It was ~/.comet-panel before the project was renamed
// to stele, so the first run after the rename moves the old directory once.
// Nothing else in the tree may hardcode either name.
package appdir

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// currentName is the directory the panel uses now.
	currentName = ".stele"
	// legacyName is the pre-rename directory, migrated on first use.
	legacyName = ".comet-panel"
	// EnvDataDir overrides the location entirely, for installs that keep state
	// outside the home directory.
	EnvDataDir = "STELE_DATA_DIR"
)

// warnOnce keeps the migration diagnostics to one line per process; Dir is
// called per request, not just at startup.
var warnOnce sync.Once

// Dir returns the data directory. It follows $HOME (and the STELE_DATA_DIR
// override) on every call rather than caching, so a process that changes its
// environment - notably a test - observes the change.
func Dir() string {
	if override := strings.TrimSpace(os.Getenv(EnvDataDir)); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return currentName
	}
	return Resolve(home, warnf)
}

// Path joins elements onto the data directory.
func Path(elem ...string) string {
	return filepath.Join(append([]string{Dir()}, elem...)...)
}

func warnf(format string, args ...any) {
	warnOnce.Do(func() { log.Printf(format, args...) })
}

// Resolve picks the data directory under home and performs the one-time
// migration from the pre-rename location. It is separate from Dir so the
// decision table is testable without touching a real home directory.
//
// The migration is a rename, so it is atomic within one filesystem. If it
// fails, the legacy directory is returned unchanged: continuing with the user's
// existing configuration is always better than starting from an empty one.
func Resolve(home string, logf func(string, ...any)) string {
	current := filepath.Join(home, currentName)
	legacy := filepath.Join(home, legacyName)

	if isDir(current) {
		if isDir(legacy) {
			logf("appdir: using %s; the legacy %s is left untouched", current, legacy)
		}
		return current
	}
	if !isDir(legacy) {
		return current
	}
	if err := os.Rename(legacy, current); err != nil {
		logf("appdir: could not migrate %s to %s (%v); continuing with the legacy directory", legacy, current, err)
		return legacy
	}
	log.Printf("appdir: migrated data directory %s -> %s", legacy, current)
	return current
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
