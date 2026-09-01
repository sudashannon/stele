// Package claims stores experiential assertions (decisions, pitfalls,
// constraints, risks) that agent sessions and documents establish about a
// workspace. Unlike wiki components, claims are not documents: a claim is one
// atomic proposition plus versioned evidence pointing at the exact source
// material (code lines, document content, session transcript) that supports
// it. When the evidence changes, the claim is stale and must be re-verified
// before an agent may rely on it.
package claims

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Kind classifies what a claim asserts.
type Kind string

const (
	KindFact        Kind = "fact"
	KindDecision    Kind = "decision"
	KindRequirement Kind = "requirement"
	KindTask        Kind = "task"
	KindQuestion    Kind = "question"
	KindRisk        Kind = "risk"
)

var validKinds = map[Kind]bool{
	KindFact: true, KindDecision: true, KindRequirement: true,
	KindTask: true, KindQuestion: true, KindRisk: true,
}

// Truth records how a claim was established.
type Truth string

const (
	TruthCodeVerified   Truth = "code_verified"
	TruthSourceVerified Truth = "source_verified"
	TruthUnknown        Truth = "unknown"
)

var validTruths = map[Truth]bool{
	TruthCodeVerified: true, TruthSourceVerified: true, TruthUnknown: true,
}

// Intent records whether the asserted fact is deliberate or accidental.
type Intent string

const (
	IntentIntended   Intent = "intended"
	IntentAccidental Intent = "accidental"
	IntentUnknown    Intent = "unknown"
)

var validIntents = map[Intent]bool{
	IntentIntended: true, IntentAccidental: true, IntentUnknown: true,
}

// Status is the verification lifecycle state of a claim.
type Status string

const (
	// StatusActive means every piece of evidence resolved successfully and
	// matched the stored version at the last check.
	StatusActive Status = "active"
	// StatusStale means at least one evidence resource changed or vanished
	// since the claim was last verified. The claim text is kept, but agents
	// must re-verify it before relying on it.
	StatusStale Status = "stale"
	// StatusRetracted means the claim was explicitly withdrawn. Retracted
	// claims are retained for history and excluded from search and lint.
	StatusRetracted Status = "retracted"
)

var validStatuses = map[Status]bool{
	StatusActive: true, StatusStale: true, StatusRetracted: true,
}

// EvidenceKind identifies which resolver owns a resource URI.
type EvidenceKind string

const (
	// EvidenceCode points at a file inside a workspace: ws://<workspace>/<rel>#L<from>-L<to>
	EvidenceCode EvidenceKind = "code"
	// EvidenceDoc points at a workspace document's full content: doc://<workspace>/<rel>
	EvidenceDoc EvidenceKind = "doc"
	// EvidenceSession points at an agent session transcript: session://<id>
	EvidenceSession EvidenceKind = "session"
)

// Evidence is one versioned source reference supporting a claim.
type Evidence struct {
	// Resource is a canonical URI: ws://, doc:// or session://.
	Resource string `json:"resource"`
	// Version is the deterministic token observed when the claim was last
	// verified against this resource. Empty until first resolution.
	Version string `json:"version,omitempty"`
	// VerifiedAt is the RFC3339 time of the last successful resolution.
	VerifiedAt string `json:"verifiedAt,omitempty"`
}

// Claim is one atomic factual proposition.
type Claim struct {
	ID string `json:"id"` // "claim.<slug>", stable idempotency key
	// Workspace is the workspace alias the claim belongs to.
	Workspace string `json:"workspace"`
	// DocID optionally attaches the claim to one indexed document
	// (Component.ID). Empty means the claim is workspace-level.
	DocID  string `json:"docId,omitempty"`
	Kind   Kind   `json:"kind"`
	Truth  Truth  `json:"truth"`
	Intent Intent `json:"intent"`
	// Text is the assertion itself: one fact, written to be reusable.
	Text string `json:"text"`
	// CodeAnchors are workspace-relative file paths for navigation. They are
	// not versioned; use Evidence for claims whose freshness matters.
	CodeAnchors []string `json:"codeAnchors,omitempty"`
	// Evidence is the versioned source material the claim rests on.
	Evidence []Evidence `json:"evidence,omitempty"`
	Tags     []string   `json:"tags,omitempty"`
	Status   Status     `json:"status"`
	// StaleSince is set when the claim first went stale and cleared on
	// successful re-verification.
	StaleSince string `json:"staleSince,omitempty"`
	// StaleReason records why the last check failed: "version-changed",
	// "evidence-missing" or "resolution-error".
	StaleReason string `json:"staleReason,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

var idRE = regexp.MustCompile(`^claim\.[a-z0-9._-]+$`)

const maxTextBytes = 4096

// Validate checks structural correctness of a claim. Callers pass the status
// they intend to persist; a retracted claim only needs a valid id and
// workspace.
func Validate(c Claim) error {
	if !idRE.MatchString(c.ID) {
		return fmt.Errorf("id %q must match %s", c.ID, idRE.String())
	}
	if c.Workspace == "" {
		return fmt.Errorf("workspace is required")
	}
	if len(c.Text) > maxTextBytes {
		return fmt.Errorf("text exceeds %d bytes", maxTextBytes)
	}
	if c.Status == StatusRetracted {
		return nil
	}
	if !validKinds[c.Kind] {
		return fmt.Errorf("kind %q is not one of fact, decision, requirement, task, question, risk", c.Kind)
	}
	if !validTruths[c.Truth] {
		return fmt.Errorf("truth %q is not one of code_verified, source_verified, unknown", c.Truth)
	}
	if !validIntents[c.Intent] {
		return fmt.Errorf("intent %q is not one of intended, accidental, unknown", c.Intent)
	}
	if c.Text == "" {
		return fmt.Errorf("text is required")
	}
	for _, ev := range c.Evidence {
		if _, _, err := ParseResource(ev.Resource); err != nil {
			return fmt.Errorf("evidence %q: %v", ev.Resource, err)
		}
	}
	return nil
}

// ParsedResource is a decoded evidence resource URI.
type ParsedResource struct {
	Kind EvidenceKind
	// Workspace is the workspace alias (code and doc resources).
	Workspace string
	// Rel is the workspace-relative path (code and doc resources).
	Rel string
	// SessionID identifies the transcript (session resources).
	SessionID string
	// LineFrom / LineTo are the inclusive cited line range (code resources).
	// Zero values mean the whole file.
	LineFrom int
	LineTo   int
}

var lineRangeRE = regexp.MustCompile(`^L([1-9][0-9]*)-L([1-9][0-9]*)$`)

// ParseResource decodes and validates a canonical evidence resource URI.
// It refuses absolute or escaping paths and anything touching .git so a
// claim can never pin evidence outside the workspace it names.
func ParseResource(uri string) (ParsedResource, EvidenceKind, error) {
	var out ParsedResource
	switch {
	case strings.HasPrefix(uri, "ws://"):
		out.Kind = EvidenceCode
		rest := strings.TrimPrefix(uri, "ws://")
		rel, rangePart, found := strings.Cut(rest, "#")
		if !found {
			return out, EvidenceCode, fmt.Errorf("missing #L<from>-L<to> line range")
		}
		m := lineRangeRE.FindStringSubmatch(rangePart)
		if m == nil {
			return out, EvidenceCode, fmt.Errorf("invalid line range %q (want #L<from>-L<to>)", rangePart)
		}
		from, err1 := atoiRange(m[1])
		to, err2 := atoiRange(m[2])
		if err1 != nil || err2 != nil {
			return out, EvidenceCode, fmt.Errorf("invalid line range %q", rangePart)
		}
		if from > to {
			return out, EvidenceCode, fmt.Errorf("line range start %d exceeds end %d", from, to)
		}
		out.LineFrom, out.LineTo = from, to
		if err := splitWorkspacePath(rel, &out.Workspace, &out.Rel); err != nil {
			return out, EvidenceCode, err
		}
	case strings.HasPrefix(uri, "doc://"):
		out.Kind = EvidenceDoc
		if err := splitWorkspacePath(strings.TrimPrefix(uri, "doc://"), &out.Workspace, &out.Rel); err != nil {
			return out, EvidenceDoc, err
		}
	case strings.HasPrefix(uri, "session://"):
		out.Kind = EvidenceSession
		id := strings.TrimPrefix(uri, "session://")
		if id == "" || strings.ContainsAny(id, "/\\") || id == ".." {
			return out, EvidenceSession, fmt.Errorf("invalid session id %q", id)
		}
		out.SessionID = id
	default:
		return out, "", fmt.Errorf("unsupported resource scheme %q (want ws://, doc:// or session://)", uri)
	}
	return out, out.Kind, nil
}

// splitWorkspacePath splits "<workspace>/<rel>" and validates both halves.
func splitWorkspacePath(s string, workspace, rel *string) error {
	ws, r, found := strings.Cut(s, "/")
	if !found || ws == "" || r == "" {
		return fmt.Errorf("resource %q must look like <workspace>/<path>", s)
	}
	if ws == ".." || strings.Contains(ws, "/") {
		return fmt.Errorf("invalid workspace alias %q", ws)
	}
	clean := filepath.ToSlash(filepath.Clean(r))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path %q escapes the workspace", r)
	}
	if strings.Contains(r, `..`+string(filepath.Separator)) || strings.Contains(r, "/../") {
		return fmt.Errorf("path %q escapes the workspace", r)
	}
	parts := strings.Split(clean, "/")
	for _, p := range parts {
		if p == ".git" {
			return fmt.Errorf("path %q references .git metadata", r)
		}
	}
	*workspace = ws
	*rel = clean
	return nil
}

func atoiRange(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(c-'0')
		if n > 1000000 {
			return 0, fmt.Errorf("line number out of bounds: %q", s)
		}
	}
	return n, nil
}
