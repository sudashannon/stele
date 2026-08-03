package sessions

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Unit is one session as a provider sees it: a primary transcript plus any
// nested transcripts whose work belongs to the same session.
//
// Grouping is the provider's call because it encodes a runtime's on-disk
// layout. Folding is not: merging a group into one digest is runtime-agnostic
// (see Merge), so every provider gets the same semantics for free.
type Unit struct {
	Primary string
	// Parts are the nested transcripts, keyed in discovery order. The name
	// under which each was dispatched is its file name; a runtime without
	// subagents simply returns none.
	Parts []string
}

// Provider adapts one agent runtime's transcripts to the digest pipeline.
// Everything downstream of Digest - workspace attribution, graph grafting,
// isolation, summaries, REST, MCP and the panel - is runtime-agnostic already,
// so a new runtime needs exactly these three methods and nothing else.
type Provider interface {
	// Name identifies the runtime in cache keys, API payloads and the UI.
	Name() string
	// Discover groups a root's transcripts into sessions. A missing root is
	// not an error: the panel runs on machines without this runtime.
	Discover(root string) ([]Unit, error)
	// Parse distills one transcript file. prev, when it describes the same
	// file, allows an append-only resume instead of a full re-read.
	Parse(path string, prev *Digest) (*Digest, error)
}

// Source binds a provider to the directory it should read.
type Source struct {
	Provider Provider
	Root     string
}

// providers is the registry a configuration string resolves against.
var providers = map[string]Provider{
	ompProvider{}.Name():        ompProvider{},
	claudeCodeProvider{}.Name(): claudeCodeProvider{},
}

// ProviderByName resolves a configured runtime name.
func ProviderByName(name string) (Provider, bool) {
	provider, ok := providers[strings.ToLower(strings.TrimSpace(name))]
	return provider, ok
}

// OMPProvider returns the provider for OMP transcripts, the default runtime.
func OMPProvider() Provider { return ompProvider{} }

// ProviderNames lists the registered runtimes, for error messages and help
// text.
func ProviderNames() []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ompProvider reads OMP transcripts.
//
// Layout: `<root>/<cwd-slug>/<ISO>_<uuid>.jsonl` (and loose transcripts at the
// root). Each transcript may sit beside a same-named directory holding that
// session's artifacts - tool logs and reports, which are not transcripts - plus
// one `.jsonl` per subagent it dispatched, nested one more level when the
// subagent dispatched its own. Only `.jsonl` files are read, at any depth under
// that directory.
type ompProvider struct{}

func (ompProvider) Name() string { return "omp" }

func (ompProvider) Parse(path string, prev *Digest) (*Digest, error) {
	return ParseFile(path, prev)
}

func (p ompProvider) Discover(root string) ([]Unit, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sessions: read %s: %w", root, err)
	}

	var units []Unit
	units = append(units, unitsIn(root, entries)...)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		bucket := filepath.Join(root, entry.Name())
		inner, readErr := os.ReadDir(bucket)
		if readErr != nil {
			log.Printf("sessions: skipping %s: %v", bucket, readErr)
			continue
		}
		units = append(units, unitsIn(bucket, inner)...)
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Primary < units[j].Primary })
	return units, nil
}

// unitsIn pairs every transcript in dir with the parts nested in its
// same-named sibling directory.
func unitsIn(dir string, entries []os.DirEntry) []Unit {
	directories := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			directories[entry.Name()] = struct{}{}
		}
	}
	var units []Unit
	for _, entry := range entries {
		if entry.IsDir() || !isTranscript(entry.Name()) {
			continue
		}
		unit := Unit{Primary: filepath.Join(dir, entry.Name())}
		stem := strings.TrimSuffix(entry.Name(), ".jsonl")
		if _, ok := directories[stem]; ok {
			unit.Parts = transcriptsUnder(filepath.Join(dir, stem))
		}
		units = append(units, unit)
	}
	return units
}

// transcriptsUnder collects every transcript below dir. A session's artifact
// directory also holds tool logs and reports, so the extension filter is what
// keeps non-transcripts out.
func transcriptsUnder(dir string) []string {
	var found []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && isTranscript(entry.Name()) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		log.Printf("sessions: skipping %s: %v", dir, err)
	}
	sort.Strings(found)
	return found
}

func isTranscript(name string) bool {
	return strings.HasSuffix(name, ".jsonl")
}
