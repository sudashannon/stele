package todo

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"stele/internal/appdir"
)

const supportedSchemaVersion = 2

// Store is the single shared persistence layer for Todos, used by both the
// REST handlers and the MCP tools. It is goroutine-safe.
type Store struct {
	mu          sync.Mutex
	items       []Todo
	syncCursors map[string]int64
	revision    int64
	path        string
	onChange    func(int64)
}

// NewStore creates a Store that persists to the given file path.
func NewStore(path string, onChange func(int64)) (*Store, error) {
	s := &Store{
		path:        path,
		onChange:    onChange,
		syncCursors: make(map[string]int64),
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

// SyncOMP atomically applies a complete OMP Todo snapshot. Equal or older
// snapshots are successful stale no-ops and cannot roll the projection back.
func (s *Store) SyncOMP(in OMPSyncInput) (OMPSyncResult, error) {
	if err := validateOMPSync(in); err != nil {
		return OMPSyncResult{}, err
	}

	s.mu.Lock()
	cursorKey := in.SessionID
	serverSeq, hasCursor := s.syncCursors[cursorKey]
	if hasCursor && in.SnapshotSeq <= serverSeq {
		result := s.ompSyncResultLocked(in, false, true, serverSeq, 0, 0, 0)
		s.mu.Unlock()
		return result, nil
	}

	oldItems := s.items
	oldCursors := s.syncCursors
	oldRevision := s.revision
	nextItems := snapshotItems(s.items)
	now := utcNow()
	byTaskKey := make(map[string]int)
	for i := range nextItems {
		if isOMPSessionTodo(nextItems[i], in.SessionID) {
			byTaskKey[nextItems[i].ExternalRef.TaskKey] = i
		}
	}

	created, updated := 0, 0
	present := make(map[string]bool, len(in.Todos))
	for _, projected := range in.Todos {
		present[projected.TaskKey] = true
		ref := &ExternalRef{
			System:    ExternalSystemOMP,
			SessionID: in.SessionID,
			TaskKey:   projected.TaskKey,
			Phase:     projected.Phase,
			Blocker:   projected.Blocker,
		}
		if idx, ok := byTaskKey[projected.TaskKey]; ok {
			item := &nextItems[idx]
			changed := item.Workspace != in.Workspace ||
				item.Title != projected.Title ||
				item.Status != projected.Status ||
				item.Metadata.Source != SourceOMP ||
				!externalRefEqual(item.ExternalRef, ref)
			if changed {
				item.Workspace = in.Workspace
				item.Title = projected.Title
				item.Metadata.Source = SourceOMP
				item.ExternalRef = ref
				if item.Status != projected.Status {
					item.Status = projected.Status
					if projected.Status == StatusDone {
						item.CompletedAt = now
					} else {
						item.CompletedAt = ""
					}
				}
				item.UpdatedAt = now
				updated++
			}
			continue
		}

		id, err := newID()
		if err != nil {
			s.mu.Unlock()
			return OMPSyncResult{}, err
		}
		item := Todo{
			ID:          id,
			Workspace:   in.Workspace,
			Title:       projected.Title,
			Status:      projected.Status,
			Priority:    PriorityNormal,
			Metadata:    Metadata{Source: SourceOMP},
			ExternalRef: ref,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if projected.Status == StatusDone {
			item.CompletedAt = now
		}
		nextItems = append(nextItems, item)
		created++
	}

	removed := 0
	if in.Mode == OMPSyncReconcile {
		kept := nextItems[:0]
		for i := range nextItems {
			item := nextItems[i]
			if isOMPSessionTodo(item, in.SessionID) && !present[item.ExternalRef.TaskKey] {
				removed++
				continue
			}
			kept = append(kept, item)
		}
		nextItems = kept
	}

	nextCursors := make(map[string]int64, len(s.syncCursors)+1)
	for key, seq := range s.syncCursors {
		nextCursors[key] = seq
	}
	nextCursors[cursorKey] = in.SnapshotSeq
	s.items = nextItems
	s.syncCursors = nextCursors
	s.revision++
	if err := s.persist(); err != nil {
		s.items = oldItems
		s.syncCursors = oldCursors
		s.revision = oldRevision
		s.mu.Unlock()
		return OMPSyncResult{}, err
	}
	result := s.ompSyncResultLocked(in, true, false, in.SnapshotSeq, created, updated, removed)
	cb := s.onChange
	revision := s.revision
	s.mu.Unlock()
	if cb != nil {
		cb(revision)
	}
	return result, nil
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

func validateOMPSync(in OMPSyncInput) error {
	if strings.TrimSpace(in.Workspace) == "" {
		return fmt.Errorf("workspace is required")
	}
	if strings.TrimSpace(in.SessionID) == "" {
		return fmt.Errorf("sessionId is required")
	}
	if in.SnapshotSeq < 0 {
		return fmt.Errorf("snapshotSeq must be non-negative")
	}
	if in.Mode != OMPSyncUpsert && in.Mode != OMPSyncReconcile {
		return fmt.Errorf("invalid mode: %s", in.Mode)
	}
	seen := make(map[string]bool, len(in.Todos))
	for i, item := range in.Todos {
		if strings.TrimSpace(item.TaskKey) == "" {
			return fmt.Errorf("todos[%d].taskKey is required", i)
		}
		if seen[item.TaskKey] {
			return fmt.Errorf("duplicate todos[%d].taskKey: %s", i, item.TaskKey)
		}
		seen[item.TaskKey] = true
		if strings.TrimSpace(item.Phase) == "" {
			return fmt.Errorf("todos[%d].phase is required", i)
		}
		if strings.TrimSpace(item.Title) == "" {
			return fmt.Errorf("todos[%d].title is required", i)
		}
		if !validStatuses[item.Status] {
			return fmt.Errorf("invalid todos[%d].status: %s", i, item.Status)
		}
	}
	return nil
}

func isOMPSessionTodo(item Todo, sessionID string) bool {
	return item.Metadata.Source == SourceOMP &&
		item.ExternalRef != nil &&
		item.ExternalRef.System == ExternalSystemOMP &&
		item.ExternalRef.SessionID == sessionID
}

func externalRefEqual(a, b *ExternalRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.System == b.System &&
		a.SessionID == b.SessionID &&
		a.TaskKey == b.TaskKey &&
		a.Phase == b.Phase &&
		a.Blocker == b.Blocker
}

func (s *Store) ompSyncResultLocked(in OMPSyncInput, applied, stale bool, serverSeq int64, created, updated, removed int) OMPSyncResult {
	items := make([]Todo, 0)
	for i := range s.items {
		if isOMPSessionTodo(s.items[i], in.SessionID) {
			items = append(items, s.items[i])
		}
	}
	return OMPSyncResult{
		Applied:     applied,
		Stale:       stale,
		SnapshotSeq: in.SnapshotSeq,
		ServerSeq:   serverSeq,
		Mode:        in.Mode,
		Revision:    s.revision,
		Created:     created,
		Updated:     updated,
		Removed:     removed,
		Items:       snapshotItems(items),
	}
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.items = []Todo{}
		s.syncCursors = make(map[string]int64)
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
	if env.SchemaVersion < 1 || env.SchemaVersion > supportedSchemaVersion {
		return fmt.Errorf("unsupported todo store schema version %d (supported: 1-%d)", env.SchemaVersion, supportedSchemaVersion)
	}
	if env.Items == nil {
		env.Items = []Todo{}
	}
	if env.SyncCursors == nil {
		env.SyncCursors = make(map[string]int64)
	}
	s.items = env.Items
	s.syncCursors = env.SyncCursors
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
		SchemaVersion: supportedSchemaVersion,
		Revision:      s.revision,
		Items:         s.items,
		SyncCursors:   s.syncCursors,
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
	return appdir.Path("mcp-write-token")
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
