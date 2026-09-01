package claims

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"stele/internal/appdir"
)

const supportedSchemaVersion = 1

// ErrBadSchema is returned when the on-disk file uses an unsupported schema.
// The store refuses to load rather than silently dropping data.
var ErrBadSchema = errors.New("claims: unsupported schema version")

// StorePath is the default persistence location, next to todos.json.
func StorePath() string {
	return appdir.Path("claims.json")
}

// Store is the single shared persistence layer for claims, used by both the
// REST handlers and the MCP tools. It is goroutine-safe.
type Store struct {
	mu     sync.RWMutex
	path   string
	claims []Claim

	// byKey maps workspace+\x00+id to the claim index; rebuilt on every
	// mutation.
	byKey map[string]int
	// evidenceIndex maps "<workspace>/<rel>" (code and doc resources) and
	// "session://<id>" to the claim keys citing that resource. It lets the
	// watcher re-check exactly the claims a changed file supports.
	evidenceIndex map[string][]string
}

// NewStore loads claims from path (an empty store when the file does not
// exist yet) and persists through atomic temp-file+rename writes.

func NewStore(path string) (*Store, error) {
	s := &Store{
		path:          path,
		claims:        []Claim{},
		byKey:         map[string]int{},
		evidenceIndex: map[string][]string{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Filter restricts Store.List.
type Filter struct {
	Workspace string
	DocID     string
	Kind      string
	Status    string
}

type storeEnvelope struct {
	SchemaVersion int     `json:"schemaVersion"`
	Claims        []Claim `json:"claims"`
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var env storeEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("claims: parse %s: %w", s.path, err)
	}
	if env.SchemaVersion != supportedSchemaVersion {
		return fmt.Errorf("%w: file has %d, store supports %d", ErrBadSchema, env.SchemaVersion, supportedSchemaVersion)
	}
	s.claims = env.Claims
	s.rebuildIndexes()
	return nil
}

func (s *Store) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	env := storeEnvelope{SchemaVersion: supportedSchemaVersion, Claims: s.claims}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".claims-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

func (s *Store) rebuildIndexes() {
	s.byKey = map[string]int{}
	s.evidenceIndex = map[string][]string{}
	for i, c := range s.claims {
		ck := claimKey(c.Workspace, c.ID)
		s.byKey[ck] = i
		for _, ev := range c.Evidence {
			key := evidenceKey(ev.Resource)
			if key == "" {
				continue
			}
			s.evidenceIndex[key] = append(s.evidenceIndex[key], ck)
		}
	}
}

func claimKey(workspace, id string) string { return workspace + "\x00" + id }

// evidenceKey is the inverted-index key for a resource URI: the file path it
// protects (workspace/rel for code and doc resources) or the session uri.
func evidenceKey(uri string) string {
	res, kind, err := ParseResource(uri)
	if err != nil {
		return ""
	}
	switch kind {
	case EvidenceCode, EvidenceDoc:
		return res.Workspace + "/" + res.Rel
	case EvidenceSession:
		return "session://" + res.SessionID
	}
	return ""
}

// All returns a copy of every claim, sorted deterministically.
func (s *Store) All() []Claim {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Claim, len(s.claims))
	copy(out, s.claims)
	SortClaims(out)
	return out
}

// List returns a filtered copy, sorted deterministically.
func (s *Store) List(f Filter) []Claim {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Claim
	for _, c := range s.claims {
		if f.Workspace != "" && c.Workspace != f.Workspace {
			continue
		}
		if f.DocID != "" && c.DocID != f.DocID {
			continue
		}
		if f.Kind != "" && string(c.Kind) != f.Kind {
			continue
		}
		if f.Status != "" && string(c.Status) != f.Status {
			continue
		}
		out = append(out, c)
	}
	SortClaims(out)
	return out
}

// ByKey returns one claim by workspace + id.
func (s *Store) ByKey(workspace, id string) (Claim, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx, ok := s.byKey[claimKey(workspace, id)]
	if !ok {
		return Claim{}, false
	}
	return s.claims[idx], true
}

// Touching returns the ids of non-retracted claims whose evidence cites the
// given key: "<workspace>/<rel>" for code and doc resources, or the full
// "session://<id>" uri.
func (s *Store) Touching(key string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.evidenceIndex[key]))
	for _, ck := range s.evidenceIndex[key] {
		idx, ok := s.byKey[ck]
		if !ok || s.claims[idx].Status == StatusRetracted {
			continue
		}
		out = append(out, s.claims[idx].ID)
	}
	return out
}

// Upsert inserts or replaces claims, keyed by (workspace, id). Existing
// claims keep their CreatedAt; re-upserting a stale or active claim refreshes
// UpdatedAt and its evidence versions as supplied. Returns the number of
// claims applied. Validation is all-or-nothing: one invalid claim rejects
// the whole batch.
func (s *Store) Upsert(in []Claim) (int, error) {
	if len(in) == 0 {
		return 0, nil
	}
	for _, c := range in {
		if err := Validate(c); err != nil {
			return 0, err
		}
	}
	now := utcNow()
	s.mu.Lock()
	defer s.mu.Unlock()
	applied := 0
	for _, c := range in {
		c.Workspace = c.Workspace
		if c.Status == "" {
			c.Status = StatusActive
		}
		if idx, ok := s.byKey[claimKey(c.Workspace, c.ID)]; ok {
			existing := s.claims[idx]
			c.CreatedAt = existing.CreatedAt
			if existing.Status == StatusStale && c.Status == StatusActive {
				c.StaleSince = ""
				c.StaleReason = ""
			}
		}
		c.UpdatedAt = now
		if c.CreatedAt == "" {
			c.CreatedAt = now
		}
		s.upsertLocked(c)
		applied++
	}
	if err := s.persist(); err != nil {
		return 0, err
	}
	return applied, nil
}

// Retract marks the given claims (in one workspace) as retracted. Unknown
// ids are ignored; returns how many were actually retracted.
func (s *Store) Retract(workspace string, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := utcNow()
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, id := range ids {
		idx, ok := s.byKey[claimKey(workspace, id)]
		if !ok || s.claims[idx].Status == StatusRetracted {
			continue
		}
		s.claims[idx].Status = StatusRetracted
		s.claims[idx].UpdatedAt = now
		n++
	}
	if n == 0 {
		return 0, nil
	}
	if err := s.persist(); err != nil {
		return 0, err
	}
	return n, nil
}

// MarkStale applies a failed freshness check to one claim: status flips to
// stale, the resolved versions (when any) are recorded, and the reason is
// kept. A claim that is already stale only has its reason refreshed.
func (s *Store) MarkStale(claimID, workspace, reason string, versions map[string]string) error {
	now := utcNow()
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.byKey[claimKey(workspace, claimID)]
	if !ok {
		return nil
	}
	c := &s.claims[idx]
	if c.Status != StatusRetracted {
		if c.Status != StatusStale {
			c.Status = StatusStale
			c.StaleSince = now
		}
		c.StaleReason = reason
		for i, ev := range c.Evidence {
			if v, ok := versions[ev.Resource]; ok {
				c.Evidence[i].Version = v
			}
		}
		c.UpdatedAt = now
	}
	return s.persist()
}

// MarkVerified applies a successful freshness check: the claim returns to
// active with refreshed evidence versions and verification timestamps.
func (s *Store) MarkVerified(claimID, workspace string, versions map[string]string) error {
	now := utcNow()
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.byKey[claimKey(workspace, claimID)]
	if !ok {
		return nil
	}
	c := &s.claims[idx]
	if c.Status == StatusRetracted {
		return nil
	}
	for i, ev := range c.Evidence {
		if v, ok := versions[ev.Resource]; ok {
			c.Evidence[i].Version = v
			c.Evidence[i].VerifiedAt = now
		}
	}
	c.Status = StatusActive
	c.StaleSince = ""
	c.StaleReason = ""
	c.UpdatedAt = now
	return s.persist()
}

func (s *Store) upsertLocked(c Claim) {
	if idx, ok := s.byKey[claimKey(c.Workspace, c.ID)]; ok {
		// Replacement: the claim's evidence set may have changed, so
		// rebuild both indexes rather than patching them incrementally.
		s.claims[idx] = c
		s.rebuildIndexes()
		return
	}
	s.claims = append(s.claims, c)
	s.byKey[claimKey(c.Workspace, c.ID)] = len(s.claims) - 1
	for _, ev := range c.Evidence {
		key := evidenceKey(ev.Resource)
		if key == "" {
			continue
		}
		s.evidenceIndex[key] = append(s.evidenceIndex[key], claimKey(c.Workspace, c.ID))
	}
}
