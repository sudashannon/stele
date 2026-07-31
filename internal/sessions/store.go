package sessions

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// storeSchemaVersion guards the on-disk digest cache. A future format change
// bumps it and older files are discarded rather than misread, mirroring the
// todo store's migration policy. v2 split created paths (`write`) out of the
// patched paths (`edit`), so a v1 cache would report every produced document
// as merely edited.
const storeSchemaVersion = 2

type storeFile struct {
	SchemaVersion int                `json:"schemaVersion"`
	Sessions      map[string]*Digest `json:"sessions"`
}

// Store keeps one digest per transcript and persists them so a restart does
// not re-read the whole transcript corpus (measured at 388 MB across 74
// files). It is safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	path     string
	digests  map[string]*Digest
	loadFail bool
}

// NewStore loads the cache at path when present. A corrupt or
// wrong-version cache is reported and treated as empty: digests are derived
// data and a full re-parse is always correct.
func NewStore(path string) *Store {
	store := &Store{path: path, digests: map[string]*Digest{}}
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
	for key, digest := range file.Sessions {
		if digest != nil {
			store.digests[key] = digest
		}
	}
	return store
}

// Refresh walks root for transcripts and (re)parses the ones whose size or
// mtime changed. It returns the digests that changed plus the number of
// transcripts still present, and drops digests whose file disappeared.
func (s *Store) Refresh(root string) (changed []*Digest, total int, err error) {
	if root == "" {
		return nil, 0, nil
	}
	files, err := discover(root)
	if err != nil {
		return nil, 0, err
	}

	seen := make(map[string]struct{}, len(files))
	for _, path := range files {
		seen[path] = struct{}{}
		s.mu.RLock()
		prev := s.digests[path]
		s.mu.RUnlock()

		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		if prev != nil && prev.Size == info.Size() && prev.ModTime.Equal(info.ModTime()) {
			continue
		}
		digest, parseErr := ParseFile(path, prev)
		if parseErr != nil {
			log.Printf("sessions: skipping %s: %v", path, parseErr)
			continue
		}
		s.mu.Lock()
		s.digests[path] = digest
		s.mu.Unlock()
		changed = append(changed, digest)
	}

	s.mu.Lock()
	for path := range s.digests {
		if _, ok := seen[path]; !ok {
			delete(s.digests, path)
		}
	}
	total = len(s.digests)
	s.mu.Unlock()
	return changed, total, nil
}

// List returns every known digest, newest activity first.
func (s *Store) List() []Digest {
	s.mu.RLock()
	out := make([]Digest, 0, len(s.digests))
	for _, digest := range s.digests {
		out = append(out, *digest.clone())
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

// Get returns one digest by transcript path.
func (s *Store) Get(path string) (Digest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	digest, ok := s.digests[path]
	if !ok {
		return Digest{}, false
	}
	return *digest.clone(), true
}

// Save writes the cache atomically. Callers treat failures as non-fatal.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	file := storeFile{SchemaVersion: storeSchemaVersion, Sessions: make(map[string]*Digest, len(s.digests))}
	for key, digest := range s.digests {
		file.Sessions[key] = digest
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

// discover lists transcripts under root. Layout is
// `<root>/<cwd-slug>/<ISO>_<uuid>.jsonl`; each transcript may sit beside a
// same-named directory of tool artifacts, which is not descended into.
func discover(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sessions: read %s: %w", root, err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			if isTranscript(entry.Name()) {
				files = append(files, filepath.Join(root, entry.Name()))
			}
			continue
		}
		bucket := filepath.Join(root, entry.Name())
		inner, err := os.ReadDir(bucket)
		if err != nil {
			log.Printf("sessions: skipping %s: %v", bucket, err)
			continue
		}
		for _, candidate := range inner {
			if candidate.IsDir() || !isTranscript(candidate.Name()) {
				continue
			}
			files = append(files, filepath.Join(bucket, candidate.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func isTranscript(name string) bool {
	return strings.HasSuffix(name, ".jsonl")
}
