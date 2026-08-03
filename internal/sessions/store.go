package sessions

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// storeSchemaVersion guards the on-disk digest cache. A future format change
// bumps it and older files are discarded rather than misread, mirroring the
// todo store's migration policy.
//
// Every bump is a change to what a digest *derives*, not just to how it is
// stored, because an unchanged transcript is never re-parsed: a stale cache
// would silently serve digests missing the new field forever. Adding a field
// without bumping is the one mistake this comment exists to prevent - it has
// happened twice, and both times the symptom was a feature that looked broken
// for every session except the handful that changed after the upgrade.
//
//	v2  split created paths (`write`) out of patched paths (`edit`)
//	v3  grouped a session's transcripts into a unit (primary + folded
//	    subagent parts) and recorded the producing provider
//	v4  replayed the session's task tracker into Todos/TodosCompleted
//	v5  per-day Activity, blocker reasons on tracker items, and an intent
//	    window that keeps the most recent calls instead of the first
const storeSchemaVersion = 5

// unitCache is one session's cached state: the digest of every transcript in
// the unit, keyed by path. Parts are kept individually rather than pre-merged
// so an appended subagent transcript resumes from its own offset instead of
// forcing the whole unit to be re-read.
type unitCache struct {
	Source  string             `json:"source"`
	Primary string             `json:"primary"`
	Parts   map[string]*Digest `json:"parts,omitempty"`
}

type storeFile struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Sessions      map[string]*unitCache `json:"sessions"`
}

// Store keeps one entry per session and persists them so a restart does not
// re-read the whole transcript corpus (measured at 388 MB across 74 files). It
// is safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	path     string
	units    map[string]*unitCache
	loadFail bool
}

// NewStore loads the cache at path when present. A corrupt or
// wrong-version cache is reported and treated as empty: digests are derived
// data and a full re-parse is always correct.
func NewStore(path string) *Store {
	store := &Store{path: path, units: map[string]*unitCache{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return store
	}
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		log.Printf("sessions: ignoring unreadable cache %s: %v", path, err)
		store.loadFail = true
		return store
	}
	if file.SchemaVersion != storeSchemaVersion {
		log.Printf("sessions: ignoring cache %s with schema version %d (want %d)", path, file.SchemaVersion, storeSchemaVersion)
		store.loadFail = true
		return store
	}
	for key, unit := range file.Sessions {
		if unit != nil && unit.Primary != "" {
			store.units[key] = unit
		}
	}
	return store
}

// unitKey identifies a session across providers, so two runtimes pointed at
// overlapping roots cannot collide in the cache.
func unitKey(source, primary string) string {
	return source + "\x00" + primary
}

// Refresh discovers every source's sessions and (re)parses the transcripts
// whose size or mtime changed. It returns the sessions whose merged digest
// changed plus the number still present, and drops sessions whose files
// disappeared.
func (s *Store) Refresh(sources []Source) (changed []*Digest, total int, err error) {
	seen := make(map[string]struct{})
	for _, source := range sources {
		if source.Provider == nil || source.Root == "" {
			continue
		}
		units, discoverErr := source.Provider.Discover(source.Root)
		if discoverErr != nil {
			return nil, 0, discoverErr
		}
		name := source.Provider.Name()
		for _, unit := range units {
			key := unitKey(name, unit.Primary)
			seen[key] = struct{}{}
			if merged, ok := s.refreshUnit(source.Provider, key, unit); ok {
				changed = append(changed, merged)
			}
		}
	}

	s.mu.Lock()
	for key := range s.units {
		if _, ok := seen[key]; !ok {
			delete(s.units, key)
		}
	}
	total = len(s.units)
	s.mu.Unlock()
	return changed, total, nil
}

// refreshUnit re-parses the unit's stale transcripts and reports the merged
// digest when anything changed. A part that vanished drops out; a part that
// fails to parse keeps its previous digest rather than erasing the session.
func (s *Store) refreshUnit(provider Provider, key string, unit Unit) (*Digest, bool) {
	s.mu.RLock()
	prev := s.units[key]
	s.mu.RUnlock()

	next := &unitCache{Source: provider.Name(), Primary: unit.Primary, Parts: map[string]*Digest{}}
	dirty := prev == nil
	paths := append([]string{unit.Primary}, unit.Parts...)
	for _, path := range paths {
		var previous *Digest
		if prev != nil {
			previous = prev.Parts[path]
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			dirty = true
			continue
		}
		if previous != nil && previous.Size == info.Size() && previous.ModTime.Equal(info.ModTime()) {
			next.Parts[path] = previous
			continue
		}
		digest, parseErr := ParseFile(path, previous)
		if parseErr != nil {
			log.Printf("sessions: skipping %s: %v", path, parseErr)
			if previous != nil {
				next.Parts[path] = previous
			}
			continue
		}
		digest.Source = provider.Name()
		next.Parts[path] = digest
		dirty = true
	}
	if prev != nil && len(prev.Parts) != len(next.Parts) {
		dirty = true
	}
	if next.Parts[unit.Primary] == nil {
		// Without the primary there is no session identity to attribute.
		s.mu.Lock()
		delete(s.units, key)
		s.mu.Unlock()
		return nil, false
	}

	s.mu.Lock()
	s.units[key] = next
	s.mu.Unlock()
	if !dirty {
		return nil, false
	}
	merged := next.merged()
	return &merged, true
}

// merged folds the unit's parts into its primary digest.
func (u *unitCache) merged() Digest {
	primary := u.Parts[u.Primary]
	if primary == nil {
		return Digest{}
	}
	parts := make([]Digest, 0, len(u.Parts))
	for path, digest := range u.Parts {
		if path == u.Primary || digest == nil {
			continue
		}
		parts = append(parts, *digest)
	}
	return Merge(*primary, parts)
}

// List returns every known session's merged digest, newest activity first.
func (s *Store) List() []Digest {
	s.mu.RLock()
	out := make([]Digest, 0, len(s.units))
	for _, unit := range s.units {
		out = append(out, unit.merged())
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Get returns one merged digest by primary transcript path, whichever provider
// produced it.
func (s *Store) Get(path string) (Digest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, unit := range s.units {
		if unit.Primary == path {
			return unit.merged(), true
		}
	}
	return Digest{}, false
}

// Save writes the cache atomically. Callers treat failures as non-fatal.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	file := storeFile{SchemaVersion: storeSchemaVersion, Sessions: make(map[string]*unitCache, len(s.units))}
	for key, unit := range s.units {
		file.Sessions[key] = unit
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		temp.Close()
		os.Remove(tempPath)
	}()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}
