package todo

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store is the single shared persistence layer for Todos, used by both the
// REST handlers and the MCP tools. It is goroutine-safe.
type Store struct {
	mu       sync.Mutex
	items    []Todo
	revision int64
	path     string
	onChange func(int64)
}

// NewStore creates a Store that persists to the given file path.
func NewStore(path string, onChange func(int64)) (*Store, error) {
	s := &Store{
		path:     path,
		onChange: onChange,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewStoreForTest creates a Store with a real temp file for testing.
func NewStoreForTest(onChange func(int64)) (*Store, string, error) {
	f, err := os.CreateTemp("", "comet-todo-test-*.json")
	if err != nil {
		return nil, "", err
	}
	path := f.Name()
	f.Close()
	os.Remove(path)
	s, err := NewStore(path, onChange)
	return s, path, err
}

// List returns a filtered, safe copy of all items plus counts and revision.
func (s *Store) List(f Filter) ([]Todo, Counts, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	qLower := ""
	if f.Q != "" {
		qLower = strings.ToLower(f.Q)
	}
	hasFilter := f.Status != "" || f.Workspace != "" || f.Change != "" || f.WikiComponentID != "" || f.Q != ""
	if !hasFilter {
		items := snapshotItems(s.items)
		return items, counts(items), s.revision
	}

	var result []Todo
	for i := range s.items {
		if matchesFilter(s.items[i], f, qLower) {
			result = append(result, s.items[i])
		}
	}
	result = snapshotItems(result)
	return result, counts(result), s.revision
}

// Create adds a new Todo and persists.
func (s *Store) Create(in CreateInput) (Todo, error) {
	if err := ValidateCreate(&in); err != nil {
		return Todo{}, err
	}

	if in.Status == "" {
		in.Status = StatusOpen
	}
	if in.Priority == "" {
		in.Priority = PriorityNormal
	}
	if in.Metadata.Source == "" {
		in.Metadata.Source = SourceUI
	}

	id, err := newID()
	if err != nil {
		return Todo{}, err
	}

	now := utcNow()
	item := Todo{
		ID:        id,
		Workspace: in.Workspace,
		Title:     in.Title,
		Notes:     in.Notes,
		Status:    in.Status,
		Priority:  in.Priority,
		DueAt:     in.DueAt,
		Change:    in.Change,
		WikiRefs:  safeWikiRefs(in.WikiRefs),
		Metadata:  in.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if item.Status == StatusDone {
		item.CompletedAt = now
	}

	s.mu.Lock()
	cb := s.onChange
	s.items = append(s.items, item)
	s.revision++
	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return Todo{}, err
	}
	rev := s.revision
	s.mu.Unlock()
	if cb != nil {
		cb(rev)
	}

	return item, nil
}

// Update applies a partial update to the item with the given id.
func (s *Store) Update(id string, in UpdateInput) (Todo, error) {
	if err := ValidateUpdate(in); err != nil {
		return Todo{}, err
	}

	s.mu.Lock()
	cb := s.onChange
	idx := s.indexOf(id)
	if idx < 0 {
		s.mu.Unlock()
		return Todo{}, ErrNotFound
	}

	changed := applyUpdate(&s.items[idx], in)
	if !changed {
		item := s.items[idx]
		s.mu.Unlock()
		return item, nil
	}

	s.items[idx].UpdatedAt = utcNow()
	s.revision++
	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return Todo{}, err
	}
	item := s.items[idx]
	rev := s.revision
	s.mu.Unlock()
	if cb != nil {
		cb(rev)
	}

	return item, nil
}

// Delete removes the item with the given id.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	cb := s.onChange
	idx := s.indexOf(id)
	if idx < 0 {
		s.mu.Unlock()
		return ErrNotFound
	}

	s.items = append(s.items[:idx], s.items[idx+1:]...)
	s.revision++
	if err := s.persist(); err != nil {
		s.mu.Unlock()
		return err
	}
	rev := s.revision
	s.mu.Unlock()
	if cb != nil {
		cb(rev)
	}

	return nil
}

// Revision returns the current revision number.
func (s *Store) Revision() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision
}

// SetOnChange replaces the post-mutation callback.
func (s *Store) SetOnChange(fn func(int64)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// --- internal ---

func (s *Store) indexOf(id string) int {
	for i := range s.items {
		if s.items[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.items = []Todo{}
		s.revision = 0
		return nil
	}
	if err != nil {
		return err
	}
	var env storeEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	if env.SchemaVersion < 1 {
		s.items = []Todo{}
		s.revision = 0
		return nil
	}
	if env.Items == nil {
		env.Items = []Todo{}
	}
	s.items = env.Items
	s.revision = env.Revision
	return nil
}

func (s *Store) persist() error {
	if dir := filepath.Dir(s.path); dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	env := storeEnvelope{
		SchemaVersion: 1,
		Revision:      s.revision,
		Items:         s.items,
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".comet-todo-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	return nil
}

func snapshotItems(src []Todo) []Todo {
	if len(src) == 0 {
		return []Todo{}
	}
	dst := make([]Todo, len(src))
	copy(dst, src)
	return dst
}

// --- MCP token management ---

func TokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".comet-panel", "mcp-write-token")
}

func EnsureToken() ([]byte, error) {
	path := TokenPath()
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return bytesTrimRight(data, "\n\r"), nil
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	token := []byte(hex.EncodeToString(b[:]))

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, token, 0600); err != nil {
		return nil, err
	}
	return token, nil
}

func bytesTrimRight(data []byte, cutset string) []byte {
	for len(data) > 0 {
		last := data[len(data)-1]
		found := false
		for i := 0; i < len(cutset); i++ {
			if last == cutset[i] {
				found = true
				break
			}
		}
		if !found {
			break
		}
		data = data[:len(data)-1]
	}
	return data
}

func EqualToken(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
