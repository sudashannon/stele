package claims

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// contextLines is the number of lines hashed on each side of a cited code
// range. A claim about lines 40-82 goes stale when the cited lines change OR
// when three surrounding lines change, which catches near-miss edits (a new
// guard clause inserted just above the cited block) while staying cheap.
const contextLines = 3

// Resolver resolves evidence resources against the live filesystem. The
// owning API builds it from the registered workspace list; the claims package
// itself never touches workspace configuration.
type Resolver struct {
	// WorkspaceRoot returns the project root against which a workspace's
	// relative paths resolve; empty string when the alias is unknown.
	WorkspaceRoot func(alias string) string
	// SessionPath returns the transcript path for a session id; empty when
	// unknown.
	SessionPath func(id string) string
}

// ErrEvidenceMissing is returned when an evidence resource cannot be found.
// Callers treat it as staleness, not as a configuration error: the claim may
// predate the deletion, and re-verification is the actionable outcome.
var ErrEvidenceMissing = fmt.Errorf("evidence missing")

// ErrResolutionError wraps unexpected resolution failures (IO errors).
type ErrResolutionError struct{ Err error }

func (e *ErrResolutionError) Error() string { return "resolution error: " + e.Err.Error() }
func (e *ErrResolutionError) Unwrap() error { return e.Err }

// ResolveEvidence computes the current version token for one evidence
// resource. The token is deterministic and model-free, so two processes
// observing the same bytes always produce the same string.
//
// Token schemes:
//   - code:    lines-v1:<sha256 of cited lines + 3-line context each side>
//   - doc:     doc-v1:<sha256 of full file content>
//   - session: session-v1:<size>:<mtime-unix> of the transcript (transcripts
//     are append-only and can be hundreds of MB; size+mtime catches appends
//     without re-reading the file)
func (r Resolver) ResolveEvidence(ev Evidence) (string, error) {
	res, kind, err := ParseResource(ev.Resource)
	if err != nil {
		return "", err
	}
	switch kind {
	case EvidenceCode:
		root := r.WorkspaceRoot(res.Workspace)
		if root == "" {
			return "", fmt.Errorf("unknown workspace %q", res.Workspace)
		}
		return r.resolveCodeVersion(root, res.Rel, res.LineFrom, res.LineTo)
	case EvidenceDoc:
		root := r.WorkspaceRoot(res.Workspace)
		if root == "" {
			return "", fmt.Errorf("unknown workspace %q", res.Workspace)
		}
		return r.resolveDocVersion(root, res.Rel)
	case EvidenceSession:
		path := r.SessionPath(res.SessionID)
		if path == "" {
			return "", fmt.Errorf("%w: unknown session %q", ErrEvidenceMissing, res.SessionID)
		}
		return r.resolveSessionVersion(path)
	}
	return "", fmt.Errorf("unsupported evidence kind %q", kind)
}

func (r Resolver) resolveCodeVersion(root, rel string, from, to int) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s/%s", ErrEvidenceMissing, root, rel)
		}
		return "", &ErrResolutionError{Err: err}
	}
	lines := strings.Split(string(data), "\n")
	if from < 1 || from > len(lines) {
		return "", fmt.Errorf("%w: line range L%d starts beyond EOF (%d lines)", ErrEvidenceMissing, from, len(lines))
	}
	end := to
	if end > len(lines) {
		end = len(lines)
	}
	// The cited region or its immediate context changed: re-verify.
	cited := strings.Join(lines[from-1:end], "\n")
	ctxFrom := from - 1 - contextLines
	if ctxFrom < 0 {
		ctxFrom = 0
	}
	ctxTo := end + contextLines
	if ctxTo > len(lines) {
		ctxTo = len(lines)
	}
	var context strings.Builder
	for i, line := range lines[ctxFrom:ctxTo] {
		n := i + ctxFrom + 1
		if n >= from && n <= end {
			continue // cited lines already hashed above
		}
		fmt.Fprintf(&context, "%d:%s\n", n, line)
	}
	sum := sha256.Sum256([]byte(cited + "\x00" + context.String()))
	return "lines-v1:" + hex.EncodeToString(sum[:]), nil
}

func (r Resolver) resolveDocVersion(root, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s/%s", ErrEvidenceMissing, root, rel)
		}
		return "", &ErrResolutionError{Err: err}
	}
	sum := sha256.Sum256(data)
	return "doc-v1:" + hex.EncodeToString(sum[:]), nil
}

func (r Resolver) resolveSessionVersion(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrEvidenceMissing, path)
		}
		return "", &ErrResolutionError{Err: err}
	}
	return fmt.Sprintf("session-v1:%d:%d", info.Size(), info.ModTime().Unix()), nil
}

// utcNow is the canonical timestamp format for claim lifecycle fields.
func utcNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// StaleReason constants shared by store and API layers.
const (
	StaleReasonVersionChanged  = "version-changed"
	StaleReasonEvidenceMissing = "evidence-missing"
	StaleReasonResolutionError = "resolution-error"
)

// CheckOutcome describes one claim's freshness after a resolution pass.
type CheckOutcome struct {
	ClaimID string
	// Stale is false when every evidence resource resolved to the stored
	// version. Claims with no evidence are trivially fresh.
	Stale bool
	// Reason is one of the StaleReason* constants when Stale.
	Reason string
	// Versions maps each evidence resource to the newly resolved version.
	Versions map[string]string
}

// CheckClaim re-resolves every evidence resource of one claim and reports
// whether it is still fresh. It never mutates the store; the caller applies
// the outcome.
func (r Resolver) CheckClaim(c Claim) CheckOutcome {
	out := CheckOutcome{ClaimID: c.ID, Versions: map[string]string{}}
	if c.Status == StatusRetracted || len(c.Evidence) == 0 {
		return out
	}
	for _, ev := range c.Evidence {
		version, err := r.ResolveEvidence(ev)
		if err != nil {
			var resolutionErr *ErrResolutionError
			switch {
			case errors.Is(err, ErrEvidenceMissing):
				out.Stale = true
				out.Reason = StaleReasonEvidenceMissing
				return out
			case errors.As(err, &resolutionErr):
				out.Stale = true
				out.Reason = StaleReasonResolutionError
				return out
			}
			// Unknown workspace/session or unparseable resource: a
			// configuration problem, not source drift. Report it as a
			// resolution error so the claim is visible in lint instead of
			// silently treated as fresh.
			out.Stale = true
			out.Reason = StaleReasonResolutionError
			return out
		}
		out.Versions[ev.Resource] = version
		if ev.Version != "" && version != ev.Version {
			out.Stale = true
			if out.Reason == "" {
				out.Reason = StaleReasonVersionChanged
			}
		}
	}
	return out
}

// SortClaims gives deterministic ordering for API responses: by workspace,
// then id.
func SortClaims(claims []Claim) {
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Workspace != claims[j].Workspace {
			return claims[i].Workspace < claims[j].Workspace
		}
		return claims[i].ID < claims[j].ID
	})
}
