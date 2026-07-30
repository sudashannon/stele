package todo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status represents the lifecycle state of a Todo item.
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusBlocked    Status = "blocked"
	StatusDropped    Status = "dropped"
)

// Priority represents the urgency level of a Todo item.
type Priority string

const (
	PriorityUrgent Priority = "urgent"
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// ChangeRef identifies a Change by its stable business key: (workspace, name).
type ChangeRef struct {
	Workspace string `json:"workspace"`
	Name      string `json:"name"`
}

// ChangeRefKey returns the canonical single-string key for a ChangeRef.
func ChangeRefKey(ref ChangeRef) string {
	return ref.Workspace + "/" + ref.Name
}

// WikiRef references a Wiki document by its stable Component.ID, the workspace
// it belongs to, plus a titleSnapshot for offline/fallback display.
type WikiRef struct {
	ComponentID   string `json:"componentId"`
	Workspace     string `json:"workspace"`
	TitleSnapshot string `json:"titleSnapshot"`
}

// MetadataSource records which channel created the Todo.
type MetadataSource string

const (
	SourceUI  MetadataSource = "ui"
	SourceMCP MetadataSource = "mcp"
	SourceOMP MetadataSource = "omp"
)

// Metadata carries provenance information.
type Metadata struct {
	Source MetadataSource `json:"source"`
}

// ExternalSystem identifies a supported external Todo source.
type ExternalSystem string

const ExternalSystemOMP ExternalSystem = "omp"

// ExternalRef identifies a Todo projected from an external task system.
// The tuple (system, sessionId, taskKey) is the stable external identity.
type ExternalRef struct {
	System    ExternalSystem `json:"system"`
	SessionID string         `json:"sessionId"`
	TaskKey   string         `json:"taskKey"`
	Phase     string         `json:"phase"`
	Blocker   string         `json:"blocker"`
}

// Todo is the core domain model for a single todo item.
type Todo struct {
	ID          string       `json:"id"`
	Workspace   string       `json:"workspace"`
	Title       string       `json:"title"`
	Notes       string       `json:"notes,omitempty"`
	Status      Status       `json:"status"`
	Priority    Priority     `json:"priority"`
	DueAt       string       `json:"dueAt,omitempty"` // UTC RFC3339 or empty
	Change      *ChangeRef   `json:"change,omitempty"`
	WikiRefs    []WikiRef    `json:"wikiRefs,omitempty"`
	Metadata    Metadata     `json:"metadata"`
	ExternalRef *ExternalRef `json:"externalRef,omitempty"`
	CreatedAt   string       `json:"createdAt"`             // UTC RFC3339
	UpdatedAt   string       `json:"updatedAt"`             // UTC RFC3339
	CompletedAt string       `json:"completedAt,omitempty"` // UTC RFC3339, set when status=done
}

// CreateInput is the shape accepted by Store.Create.
type CreateInput struct {
	Workspace string     `json:"workspace"`
	Title     string     `json:"title"`
	Notes     string     `json:"notes,omitempty"`
	Status    Status     `json:"status,omitempty"`
	Priority  Priority   `json:"priority,omitempty"`
	DueAt     string     `json:"dueAt,omitempty"`
	Change    *ChangeRef `json:"change,omitempty"`
	WikiRefs  []WikiRef  `json:"wikiRefs,omitempty"`
	Metadata  Metadata   `json:"metadata,omitempty"`
}

// UpdateInput is the shape accepted by Store.Update. All pointer fields use
// presence-aware semantics: a non-nil pointer means "update to this value".
// presence-aware: DueAtSet/ChangeSet/WikiRefsSet distinguish absent (no-op)
// from present-with-null/nil (clear).
type UpdateInput struct {
	Workspace   *string    `json:"workspace,omitempty"`
	Title       *string    `json:"title,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
	Status      *Status    `json:"status,omitempty"`
	Priority    *Priority  `json:"priority,omitempty"`
	DueAtSet    bool       `json:"-"`
	DueAt       *string    `json:"dueAt,omitempty"`
	ChangeSet   bool       `json:"-"`
	Change      *ChangeRef `json:"change,omitempty"`
	WikiRefsSet bool       `json:"-"`
	WikiRefs    []WikiRef  `json:"wikiRefs,omitempty"`
}

// UnmarshalJSON detects presence of dueAt, change, and wikiRefs keys.
func (u *UpdateInput) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["dueAt"]; ok {
		u.DueAtSet = true
	}
	if _, ok := raw["change"]; ok {
		u.ChangeSet = true
	}
	if _, ok := raw["wikiRefs"]; ok {
		u.WikiRefsSet = true
	}
	type alias UpdateInput
	return json.Unmarshal(data, (*alias)(u))
}

// Counts is the status-breakdown summary returned alongside the item list.
type Counts struct {
	Open       int `json:"open"`
	InProgress int `json:"inProgress"`
	Done       int `json:"done"`
	Blocked    int `json:"blocked"`
	Dropped    int `json:"dropped"`
	Total      int `json:"total"`
}

// Filter restricts which items List returns.
type Filter struct {
	Status          Status `json:"status,omitempty"`
	Workspace       string `json:"workspace,omitempty"`
	Change          string `json:"change,omitempty"`
	WikiComponentID string `json:"wikiComponentId,omitempty"`
	Q               string `json:"q,omitempty"`
}

// OMPSyncMode controls whether missing OMP-owned Todos are retained or removed.
type OMPSyncMode string

const (
	OMPSyncUpsert    OMPSyncMode = "upsert"
	OMPSyncReconcile OMPSyncMode = "reconcile"
)

// OMPSyncTodo is one item in a complete OMP Todo snapshot.
type OMPSyncTodo struct {
	TaskKey string `json:"taskKey"`
	Phase   string `json:"phase"`
	Title   string `json:"title"`
	Status  Status `json:"status"`
	Blocker string `json:"blocker,omitempty"`
}

// OMPSyncInput is the atomic OMP snapshot synchronization request.
type OMPSyncInput struct {
	Workspace   string        `json:"workspace"`
	SessionID   string        `json:"sessionId"`
	SnapshotSeq int64         `json:"snapshotSeq"`
	Mode        OMPSyncMode   `json:"mode"`
	Todos       []OMPSyncTodo `json:"todos"`
}

// OMPSyncResult reports an accepted or stale OMP snapshot.
type OMPSyncResult struct {
	Applied     bool        `json:"applied"`
	Stale       bool        `json:"stale"`
	SnapshotSeq int64       `json:"snapshotSeq"`
	ServerSeq   int64       `json:"serverSeq"`
	Mode        OMPSyncMode `json:"mode"`
	Revision    int64       `json:"revision"`
	Created     int         `json:"created"`
	Updated     int         `json:"updated"`
	Removed     int         `json:"removed"`
	Items       []Todo      `json:"items"`
}

// storeEnvelope is the on-disk JSON shape.
type storeEnvelope struct {
	SchemaVersion int              `json:"schemaVersion"`
	Revision      int64            `json:"revision"`
	Items         []Todo           `json:"items"`
	SyncCursors   map[string]int64 `json:"syncCursors,omitempty"`
}

// Validation helpers.

var validStatuses = map[Status]bool{
	StatusOpen: true, StatusInProgress: true, StatusDone: true,
	StatusBlocked: true, StatusDropped: true,
}
var validPriorities = map[Priority]bool{
	PriorityUrgent: true, PriorityHigh: true, PriorityNormal: true, PriorityLow: true,
}
var validMetadataSources = map[MetadataSource]bool{
	SourceUI: true, SourceMCP: true, SourceOMP: true,
}

// ValidateCreate checks a CreateInput for structural correctness. Accepts a
// pointer so dueAt normalization propagates to the caller.
func ValidateCreate(in *CreateInput) error {
	if strings.TrimSpace(in.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(in.Workspace) == "" {
		return errors.New("workspace is required")
	}
	if in.Status != "" && !validStatuses[in.Status] {
		return fmt.Errorf("invalid status: %s", in.Status)
	}
	if in.Priority != "" && !validPriorities[in.Priority] {
		return fmt.Errorf("invalid priority: %s", in.Priority)
	}
	if in.DueAt != "" {
		if t, err := time.Parse(time.RFC3339, in.DueAt); err != nil {
			return fmt.Errorf("invalid dueAt: %s (expected RFC3339)", in.DueAt)
		} else {
			in.DueAt = t.UTC().Format(time.RFC3339)
		}
	}
	if in.Change != nil && in.Change.Workspace == "" {
		return errors.New("change.workspace is required when change is set")
	}
	if in.Change != nil && in.Change.Name == "" {
		return errors.New("change.name is required when change is set")
	}
	for i, wr := range in.WikiRefs {
		if wr.ComponentID == "" {
			return fmt.Errorf("wikiRefs[%d].componentId is required", i)
		}
		if wr.Workspace == "" {
			return fmt.Errorf("wikiRefs[%d].workspace is required", i)
		}
	}
	src := in.Metadata.Source
	if src != "" && !validMetadataSources[src] {
		return fmt.Errorf("invalid metadata.source: %s", src)
	}
	return nil
}

// ValidateUpdate checks an UpdateInput for structural correctness.
func ValidateUpdate(in UpdateInput) error {
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return errors.New("title must not be empty")
	}
	if in.Workspace != nil && strings.TrimSpace(*in.Workspace) == "" {
		return errors.New("workspace must not be empty")
	}
	if in.Status != nil && !validStatuses[*in.Status] {
		return fmt.Errorf("invalid status: %s", *in.Status)
	}
	if in.Priority != nil && !validPriorities[*in.Priority] {
		return fmt.Errorf("invalid priority: %s", *in.Priority)
	}
	if in.DueAt != nil && *in.DueAt != "" {
		if t, err := time.Parse(time.RFC3339, *in.DueAt); err != nil {
			return fmt.Errorf("invalid dueAt: %s (expected RFC3339)", *in.DueAt)
		} else {
			normalized := t.UTC().Format(time.RFC3339)
			*in.DueAt = normalized
		}
	}
	for i, wr := range in.WikiRefs {
		if wr.ComponentID == "" {
			return fmt.Errorf("wikiRefs[%d].componentId is required", i)
		}
		if wr.Workspace == "" {
			return fmt.Errorf("wikiRefs[%d].workspace is required", i)
		}
	}
	// Validate ChangeRef when ChangeSet is true and Change is non-nil.
	if in.ChangeSet && in.Change != nil {
		if in.Change.Workspace == "" {
			return errors.New("change.workspace is required when change is set")
		}
		if in.Change.Name == "" {
			return errors.New("change.name is required when change is set")
		}
	}
	return nil
}

// ErrNotFound is returned when an ID does not match any stored item.
var ErrNotFound = errors.New("todo not found")

// newID generates a cryptographically random 16-byte hex ID.
// Returns an error instead of panicking.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("crypto/rand read: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// utcNow returns the current time in UTC formatted as RFC3339.
func utcNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// counts computes Counts from a slice of Todos.
func counts(items []Todo) Counts {
	var c Counts
	for _, item := range items {
		switch item.Status {
		case StatusOpen:
			c.Open++
		case StatusInProgress:
			c.InProgress++
		case StatusDone:
			c.Done++
		case StatusBlocked:
			c.Blocked++
		case StatusDropped:
			c.Dropped++
		}
	}
	c.Total = len(items)
	return c
}

// matchesFilter reports whether item passes the given filter.
func matchesFilter(item Todo, f Filter, qLower string) bool {
	if f.Status != "" && item.Status != f.Status {
		return false
	}
	// workspace filter targets Todo.Workspace (top-level), not Change.Workspace.
	if f.Workspace != "" && !strings.EqualFold(item.Workspace, f.Workspace) {
		return false
	}
	if f.Change != "" {
		if item.Change == nil || item.Change.Name != f.Change {
			return false
		}
	}
	if f.WikiComponentID != "" {
		found := false
		for _, wr := range item.WikiRefs {
			if wr.ComponentID == f.WikiComponentID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if qLower != "" {
		if !strings.Contains(strings.ToLower(item.Title), qLower) &&
			!strings.Contains(strings.ToLower(item.Notes), qLower) {
			return false
		}
	}
	return true
}

// applyUpdate mutates item in-place according to the non-nil fields of in.
// Returns true if any field actually changed.
func applyUpdate(item *Todo, in UpdateInput) bool {
	changed := false
	if in.Workspace != nil && *in.Workspace != item.Workspace {
		item.Workspace = *in.Workspace
		changed = true
	}
	if in.Title != nil && *in.Title != item.Title {
		item.Title = *in.Title
		changed = true
	}
	if in.Notes != nil && *in.Notes != item.Notes {
		item.Notes = *in.Notes
		changed = true
	}
	if in.Priority != nil && *in.Priority != item.Priority {
		item.Priority = *in.Priority
		changed = true
	}
	if in.DueAtSet {
		newVal := ""
		if in.DueAt != nil {
			newVal = *in.DueAt
		}
		if newVal != item.DueAt {
			item.DueAt = newVal
			changed = true
		}
	}
	if in.ChangeSet {
		if !changeRefEqual(item.Change, in.Change) {
			item.Change = in.Change
			changed = true
		}
	}
	if in.WikiRefsSet {
		if !wikiRefsEqual(item.WikiRefs, in.WikiRefs) {
			item.WikiRefs = safeWikiRefs(in.WikiRefs)
			changed = true
		}
	}
	if in.Status != nil {
		newStatus := *in.Status
		if newStatus != item.Status {
			item.Status = newStatus
			if newStatus == StatusDone {
				item.CompletedAt = utcNow()
			} else {
				item.CompletedAt = ""
			}
			changed = true
		}
	}
	return changed
}

func changeRefEqual(a, b *ChangeRef) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Workspace == b.Workspace && a.Name == b.Name
}

func wikiRefsEqual(a, b []WikiRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ComponentID != b[i].ComponentID ||
			a[i].Workspace != b[i].Workspace ||
			a[i].TitleSnapshot != b[i].TitleSnapshot {
			return false
		}
	}
	return true
}

// SameOrigin checks if the Origin header matches the request Host.
func SameOrigin(origin, host string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := u.Hostname()
	originPort := u.Port()

	reqHost, reqPort, _ := netutilSplit(host)

	if !strings.EqualFold(originHost, reqHost) {
		return false
	}
	if originPort == "" {
		originPort = schemeDefaultPort(u.Scheme)
	}
	if reqPort == "" {
		reqPort = "80"
	}
	return originPort == reqPort
}

// StorePath returns the default Todo store path.
func StorePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".comet-panel", "todos.json")
}

func schemeDefaultPort(scheme string) string {
	switch scheme {
	case "https":
		return "443"
	default:
		return "80"
	}
}

// netutilSplit splits host:port.
func netutilSplit(hostPort string) (host string, port string, err error) {
	i := strings.LastIndex(hostPort, ":")
	if i < 0 {
		return hostPort, "", nil
	}
	if strings.Count(hostPort, ":") > 1 && !strings.HasPrefix(hostPort, "[") {
		return hostPort, "", nil
	}
	return hostPort[:i], hostPort[i+1:], nil
}

// safeWikiRefs returns a nil-safe copy.
func safeWikiRefs(refs []WikiRef) []WikiRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]WikiRef, len(refs))
	copy(out, refs)
	return out
}
