package todo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func helperStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "todos.json")
	s, err := NewStore(path, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, path
}

func validCreate(title string) CreateInput {
	return CreateInput{
		Workspace: "test-ws",
		Title:     title,
	}
}

func TestNewStore_MissingFile(t *testing.T) {
	s, _ := helperStore(t)
	items, _, rev := s.List(Filter{})
	if rev != 0 {
		t.Fatalf("expected rev 0, got %d", rev)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty, got %d", len(items))
	}
}

func TestNewStore_PersistsEnvelope(t *testing.T) {
	s, path := helperStore(t)
	_, err := s.Create(validCreate("hello"))
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env storeEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env.SchemaVersion != 1 {
		t.Fatalf("expected schema version 1, got %d", env.SchemaVersion)
	}
	if env.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", env.Revision)
	}
	if len(env.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(env.Items))
	}
}

func TestStore_AtomicWrite(t *testing.T) {
	s, path := helperStore(t)
	items, _, _ := s.List(Filter{})
	if len(items) != 0 {
		t.Fatal("expected empty")
	}
	_, err := s.Create(validCreate("item1"))
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".comet-todo-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	s2, err := NewStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	items, _, rev := s2.List(Filter{})
	if rev != 1 {
		t.Fatalf("expected rev 1, got %d", rev)
	}
	if len(items) != 1 || items[0].Title != "item1" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestStore_FileModeIs0600(t *testing.T) {
	s, path := helperStore(t)
	_, err := s.Create(validCreate("x"))
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600, got %o", fi.Mode().Perm())
	}
}

func TestCreate_DefaultStatusAndPriority(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(validCreate("test"))
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusOpen {
		t.Fatalf("expected open, got %s", item.Status)
	}
	if item.Priority != PriorityNormal {
		t.Fatalf("expected normal, got %s", item.Priority)
	}
	if item.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if item.CreatedAt == "" || item.UpdatedAt == "" {
		t.Fatal("expected timestamps")
	}
	if _, err := time.Parse(time.RFC3339, item.CreatedAt); err != nil {
		t.Fatalf("invalid createdAt: %v", err)
	}
	if item.Workspace != "test-ws" {
		t.Fatalf("expected workspace test-ws, got %s", item.Workspace)
	}
	if item.Metadata.Source != SourceUI {
		t.Fatalf("expected default source=ui, got %s", item.Metadata.Source)
	}
}

func TestCreate_RequiresWorkspace(t *testing.T) {
	s, _ := helperStore(t)
	_, err := s.Create(CreateInput{Title: "no workspace"})
	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("expected workspace validation error, got %v", err)
	}
}

func TestCreate_DoneSetsCompletedAt(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(CreateInput{Workspace: "ws", Title: "done task", Status: StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if item.CompletedAt == "" {
		t.Fatal("expected completedAt when creating as done")
	}
}

func TestUpdate_StatusTransitionClearsCompletedAt(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(CreateInput{Workspace: "ws", Title: "first", Status: StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if item.CompletedAt == "" {
		t.Fatal("expected completedAt")
	}

	reopen := StatusOpen
	item, err = s.Update(item.ID, UpdateInput{Status: &reopen})
	if err != nil {
		t.Fatal(err)
	}
	if item.CompletedAt != "" {
		t.Fatalf("expected empty completedAt after reopen, got %s", item.CompletedAt)
	}
	if item.Status != StatusOpen {
		t.Fatalf("expected open, got %s", item.Status)
	}
}

func TestUpdate_SetsCompletedAtOnDone(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(validCreate("a"))
	if err != nil {
		t.Fatal(err)
	}
	if item.CompletedAt != "" {
		t.Fatal("expected no completedAt on open")
	}

	done := StatusDone
	item, err = s.Update(item.ID, UpdateInput{Status: &done})
	if err != nil {
		t.Fatal(err)
	}
	if item.CompletedAt == "" {
		t.Fatal("expected completedAt after done")
	}
}

func TestUpdate_UnchangedDoesNotBumpRevision(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(validCreate("x"))
	if err != nil {
		t.Fatal(err)
	}
	rev := s.Revision()

	same := item.Title
	_, err = s.Update(item.ID, UpdateInput{Title: &same})
	if err != nil {
		t.Fatal(err)
	}
	if s.Revision() != rev {
		t.Fatalf("expected revision to stay at %d, got %d", rev, s.Revision())
	}
}

func TestDelete_NotFound(t *testing.T) {
	s, _ := helperStore(t)
	err := s.Delete("nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	s, _ := helperStore(t)
	_, err := s.Update("nonexistent", UpdateInput{})
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreate_Validation(t *testing.T) {
	s, _ := helperStore(t)
	_, err := s.Create(CreateInput{Workspace: "ws", Title: ""})
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("expected title validation error, got %v", err)
	}
	_, err = s.Create(CreateInput{Workspace: "ws", Title: "x", Status: "bogus"})
	if err == nil {
		t.Fatal("expected status validation error")
	}
	_, err = s.Create(CreateInput{Workspace: "ws", Title: "x", DueAt: "not-rfc3339"})
	if err == nil {
		t.Fatal("expected dueAt validation error")
	}
	_, err = s.Create(CreateInput{Workspace: "ws", Title: "x", Metadata: Metadata{Source: "bogus"}})
	if err == nil {
		t.Fatal("expected metadata source validation error")
	}
}

func TestCreate_ValidDueAt(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(CreateInput{Workspace: "ws", Title: "x", DueAt: "2026-12-31T23:59:59Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(item.DueAt, "2026-12-31") {
		t.Fatalf("expected dueAt to contain date, got %s", item.DueAt)
	}
}

func TestCreate_ChangeRefValidation(t *testing.T) {
	s, _ := helperStore(t)
	_, err := s.Create(CreateInput{Workspace: "ws", Title: "x", Change: &ChangeRef{Workspace: "", Name: "ch1"}})
	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("expected change.workspace validation, got %v", err)
	}
	_, err = s.Create(CreateInput{Workspace: "ws", Title: "x", Change: &ChangeRef{Workspace: "ws", Name: ""}})
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected change.name validation, got %v", err)
	}
}

func TestCreate_WikiRefValidation(t *testing.T) {
	s, _ := helperStore(t)
	_, err := s.Create(CreateInput{Workspace: "ws", Title: "x", WikiRefs: []WikiRef{{ComponentID: "", Workspace: "ws"}}})
	if err == nil || !strings.Contains(err.Error(), "componentId") {
		t.Fatalf("expected componentId validation, got %v", err)
	}
	_, err = s.Create(CreateInput{Workspace: "ws", Title: "x", WikiRefs: []WikiRef{{ComponentID: "comp", Workspace: ""}}})
	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("expected wikiref workspace validation, got %v", err)
	}
}

func TestList_FilterByStatus(t *testing.T) {
	s, _ := helperStore(t)
	s.Create(validCreate("open1"))
	s.Create(CreateInput{Workspace: "ws", Title: "done1", Status: StatusDone})

	items, _, _ := s.List(Filter{Status: StatusOpen})
	if len(items) != 1 || items[0].Title != "open1" {
		t.Fatalf("expected only open1, got %+v", items)
	}

	items, _, _ = s.List(Filter{Status: StatusDone})
	if len(items) != 1 || items[0].Title != "done1" {
		t.Fatalf("expected only done1, got %+v", items)
	}
}

func TestList_FilterByWorkspace(t *testing.T) {
	s, _ := helperStore(t)
	s.Create(CreateInput{Workspace: "ws-a", Title: "a"})
	s.Create(CreateInput{Workspace: "ws-b", Title: "b"})

	items, _, _ := s.List(Filter{Workspace: "ws-a"})
	if len(items) != 1 || items[0].Title != "a" {
		t.Fatalf("expected only a, got %+v", items)
	}
}

func TestList_FilterByChange(t *testing.T) {
	s, _ := helperStore(t)
	s.Create(CreateInput{Workspace: "ws1", Title: "a", Change: &ChangeRef{Workspace: "ws1", Name: "ch1"}})
	s.Create(CreateInput{Workspace: "ws1", Title: "b", Change: &ChangeRef{Workspace: "ws1", Name: "ch2"}})

	items, _, _ := s.List(Filter{Change: "ch2"})
	if len(items) != 1 || items[0].Title != "b" {
		t.Fatalf("expected only b, got %+v", items)
	}
}

func TestList_FilterByWikiComponentID(t *testing.T) {
	s, _ := helperStore(t)
	s.Create(CreateInput{Workspace: "ws", Title: "w1", WikiRefs: []WikiRef{{ComponentID: "comp-a", Workspace: "ws"}}})
	s.Create(CreateInput{Workspace: "ws", Title: "w2", WikiRefs: []WikiRef{{ComponentID: "comp-b", Workspace: "ws"}}})

	items, _, _ := s.List(Filter{WikiComponentID: "comp-a"})
	if len(items) != 1 || items[0].Title != "w1" {
		t.Fatalf("expected only w1, got %+v", items)
	}
}

func TestList_FilterByQ_SearchesTitleAndNotes(t *testing.T) {
	s, _ := helperStore(t)
	s.Create(CreateInput{Workspace: "ws", Title: "Hello World"})
	s.Create(CreateInput{Workspace: "ws", Title: "Task", Notes: "hello in notes"})
	s.Create(CreateInput{Workspace: "ws", Title: "Goodbye"})

	items, _, _ := s.List(Filter{Q: "hello"})
	if len(items) != 2 {
		t.Fatalf("expected 2 items matching hello, got %d: %+v", len(items), items)
	}
}

func TestList_Counts(t *testing.T) {
	s, _ := helperStore(t)
	s.Create(validCreate("o1"))
	s.Create(validCreate("o2"))
	s.Create(CreateInput{Workspace: "ws", Title: "d1", Status: StatusDone})

	_, c, _ := s.List(Filter{})
	if c.Open != 2 || c.Done != 1 || c.Total != 3 {
		t.Fatalf("unexpected counts: %+v", c)
	}
}

func TestUpdate_ClearChange(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(CreateInput{Workspace: "ws", Title: "with change", Change: &ChangeRef{Workspace: "ws", Name: "ch"}})
	if err != nil {
		t.Fatal(err)
	}
	if item.Change == nil {
		t.Fatal("expected change to be set")
	}

	// Clear via presence-aware update.
	item, err = s.Update(item.ID, UpdateInput{ChangeSet: true, Change: nil})
	if err != nil {
		t.Fatal(err)
	}
	if item.Change != nil {
		t.Fatalf("expected change to be cleared, got %+v", item.Change)
	}
}

func TestUpdate_ClearWikiRefs(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(CreateInput{Workspace: "ws", Title: "with refs", WikiRefs: []WikiRef{{ComponentID: "c", Workspace: "ws", TitleSnapshot: "t"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(item.WikiRefs) != 1 {
		t.Fatal("expected 1 wikiref")
	}

	item, err = s.Update(item.ID, UpdateInput{WikiRefsSet: true, WikiRefs: nil})
	if err != nil {
		t.Fatal(err)
	}
	if len(item.WikiRefs) != 0 {
		t.Fatalf("expected empty wikirefs, got %d", len(item.WikiRefs))
	}
}

func TestOnChange_CalledAfterMutations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todos.json")

	var revisions []int64
	s, err := NewStore(path, func(rev int64) {
		revisions = append(revisions, rev)
	})
	if err != nil {
		t.Fatal(err)
	}

	item, _ := s.Create(validCreate("t1"))
	if len(revisions) != 1 || revisions[0] != 1 {
		t.Fatalf("expected revision 1, got %v", revisions)
	}

	newTitle := "t1-updated"
	_, _ = s.Update(item.ID, UpdateInput{Title: &newTitle})
	if len(revisions) != 2 || revisions[1] != 2 {
		t.Fatalf("expected revision 2, got %v", revisions)
	}

	_ = s.Delete(item.ID)
	if len(revisions) != 3 || revisions[2] != 3 {
		t.Fatalf("expected revision 3, got %v", revisions)
	}
}

func TestChangeRefKey(t *testing.T) {
	key := ChangeRefKey(ChangeRef{Workspace: "ws", Name: "ch"})
	if key != "ws/ch" {
		t.Fatalf("expected ws/ch, got %s", key)
	}
}

func TestEqualToken(t *testing.T) {
	if !EqualToken([]byte("abc"), []byte("abc")) {
		t.Fatal("expected equal")
	}
	if EqualToken([]byte("abc"), []byte("abd")) {
		t.Fatal("expected not equal")
	}
	if EqualToken([]byte("abc"), []byte("ab")) {
		t.Fatal("expected not equal for different lengths")
	}
}

func TestEnsureToken_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	tok1, err := EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok1) != 64 {
		t.Fatalf("expected 64-char hex token, got %d", len(tok1))
	}
	tok2, err := EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	if string(tok1) != string(tok2) {
		t.Fatal("expected same token on second call")
	}
}

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		origin, host string
		want         bool
	}{
		{"", "localhost:8989", true},
		{"http://localhost:8989", "localhost:8989", true},
		{"http://localhost:9999", "localhost:8989", false},
		{"http://otherhost:8989", "localhost:8989", false},
		{"http://localhost", "localhost:80", true},
	}
	for _, tt := range tests {
		got := SameOrigin(tt.origin, tt.host)
		if got != tt.want {
			t.Errorf("SameOrigin(%q, %q) = %v, want %v", tt.origin, tt.host, got, tt.want)
		}
	}
}

func TestDueAt_RFC3339NormalizedToUTC(t *testing.T) {
	s, _ := helperStore(t)
	// Input with +08:00 timezone offset.
	item, err := s.Create(CreateInput{Workspace: "ws", Title: "x", DueAt: "2026-12-31T23:59:59+08:00"})
	if err != nil {
		t.Fatal(err)
	}
	// Must be normalized to UTC.
	if !strings.HasSuffix(item.DueAt, "Z") {
		t.Fatalf("expected dueAt normalized to UTC (suffix Z), got %s", item.DueAt)
	}
}

func TestCreate_MetadataMCPSource(t *testing.T) {
	s, _ := helperStore(t)
	item, err := s.Create(CreateInput{Workspace: "ws", Title: "x", Metadata: Metadata{Source: SourceMCP}})
	if err != nil {
		t.Fatal(err)
	}
	if item.Metadata.Source != SourceMCP {
		t.Fatalf("expected mcp source, got %s", item.Metadata.Source)
	}
}

func TestList_Q_MixedCase(t *testing.T) {
	s, _ := helperStore(t)
	s.Create(CreateInput{Workspace: "ws", Title: "Hello World"})
	s.Create(CreateInput{Workspace: "ws", Title: "Goodbye"})

	items, _, _ := s.List(Filter{Q: "HELLO"})
	if len(items) != 1 || items[0].Title != "Hello World" {
		t.Fatalf("expected 1 match for mixed-case Q, got %d", len(items))
	}
}

func TestValidateUpdate_RejectsIncompleteChangeRef(t *testing.T) {
	err := ValidateUpdate(UpdateInput{ChangeSet: true, Change: &ChangeRef{Workspace: ""}})
	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("expected workspace validation error, got %v", err)
	}
	err = ValidateUpdate(UpdateInput{ChangeSet: true, Change: &ChangeRef{Workspace: "w", Name: ""}})
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected name validation error, got %v", err)
	}
	// Clearing via ChangeSet with nil Change is valid.
	if err := ValidateUpdate(UpdateInput{ChangeSet: true, Change: nil}); err != nil {
		t.Fatalf("expected nil change to be valid clear, got %v", err)
	}
}

func TestUpdate_SetDueAt(t *testing.T) {
	s, _ := helperStore(t)
	item, _ := s.Create(CreateInput{Workspace: "ws", Title: "x", DueAt: "2026-01-01T00:00:00Z"})

	newDue := "2026-12-31T23:59:59Z"
	item, err := s.Update(item.ID, UpdateInput{DueAtSet: true, DueAt: &newDue})
	if err != nil {
		t.Fatal(err)
	}
	if item.DueAt == "" || !strings.Contains(item.DueAt, "2026-12-31") {
		t.Fatalf("expected dueAt updated, got %s", item.DueAt)
	}
}

func TestUpdate_ClearDueAt(t *testing.T) {
	s, _ := helperStore(t)
	item, _ := s.Create(CreateInput{Workspace: "ws", Title: "x", DueAt: "2026-01-01T00:00:00Z"})

	item, err := s.Update(item.ID, UpdateInput{DueAtSet: true, DueAt: nil})
	if err != nil {
		t.Fatal(err)
	}
	if item.DueAt != "" {
		t.Fatalf("expected dueAt cleared, got %s", item.DueAt)
	}
}
