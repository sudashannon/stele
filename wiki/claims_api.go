package wiki

import (
	"fmt"
	"sort"
	"strings"

	"stele/internal/claims"
	"stele/internal/source"
)

// SetClaimsStore wires the claims layer. Like the Todo store, the claims
// store is shared by REST and MCP; writes authenticate with the same MCP
// token. Nil disables the layer (tools report "claims store not available").
func (a *API) SetClaimsStore(store *claims.Store) {
	a.mu.Lock()
	a.claimsStore = store
	a.mu.Unlock()
	if store == nil {
		return
	}
	// Restore cached vectors for the current claim texts (hash-checked).
	texts := map[string]string{}
	for _, c := range store.All() {
		texts[c.ID] = claimEmbedText(c)
	}
	a.loadClaimVectors(texts)
}
func (a *API) ClaimsStoreSnapshot() *claims.Store {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.claimsStore
}

// workspacesSnapshot returns the live workspace list without holding a.mu
// across the lister call (same pattern as sessionsSnapshot).
func (a *API) workspacesSnapshot() []WorkspaceConfig {
	a.mu.RLock()
	ws := a.ws
	lister := a.lister
	a.mu.RUnlock()
	if lister != nil {
		return lister.List()
	}
	return ws
}

// claimsResolver builds the evidence resolver from the live workspace list
// and session index. Session ids are the stable digest ids shown by
// wiki_sessions; the transcript path is looked up through the index.
func (a *API) claimsResolver() claims.Resolver {
	workspaces := a.workspacesSnapshot()
	roots := map[string]string{}
	for _, ws := range workspaces {
		roots[ws.Alias] = source.MirrorRoot(ws)
	}
	index := a.SessionsIndexSnapshot()
	return claims.Resolver{
		WorkspaceRoot: func(alias string) string {
			return roots[alias]
		},
		SessionPath: func(id string) string {
			if index == nil {
				return ""
			}
			for _, digest := range index.Digests() {
				if digest.ID == id {
					return digest.Path
				}
			}
			return ""
		},
	}
}

// workspaceKeyForFile maps a changed absolute file path to the
// "<workspace>/<rel>" evidence-index key. It returns "" when the path is not
// inside a registered workspace root. Nested workspaces (a project root that
// contains another registered workspace, e.g. miao/ openspec above
// miao/rx101) resolve to the most specific root: the longest matching
// prefix wins, so a change inside the nested workspace is attributed to it
// rather than to the outer one.
func (a *API) workspaceKeyForFile(path string) (workspace, rel string) {
	bestRoot := -1
	for _, ws := range a.workspacesSnapshot() {
		root := source.MirrorRoot(ws)
		if !strings.HasPrefix(path, root+"/") {
			continue
		}
		if len(root) > bestRoot {
			bestRoot = len(root)
			workspace = ws.Alias
			rel = strings.TrimPrefix(path, root+"/")
		}
	}
	return workspace, rel
}

// CheckClaimsForFiles re-verifies the claims whose evidence cites any of the
// changed files. It is called by the watcher after a successful index update:
// the watcher only sees document files, so this covers doc:// evidence;
// code:// evidence is checked lazily by lint and wiki_context. Returns the
// number of claims that flipped to stale.
func (a *API) CheckClaimsForFiles(files []string) int {
	store := a.ClaimsStoreSnapshot()
	if store == nil {
		return 0
	}
	resolver := a.claimsResolver()
	checked := map[string]bool{}
	staled := 0
	for _, file := range files {
		ws, rel := a.workspaceKeyForFile(file)
		if ws == "" {
			continue
		}
		for _, id := range store.Touching(ws + "/" + rel) {
			if checked[id] {
				continue
			}
			checked[id] = true
			if a.checkOneClaim(store, resolver, ws, id) > 0 {
				staled++
			}
		}
	}
	return staled
}

// checkOneClaim re-resolves one claim's evidence and applies the outcome to
// the store. Returns 1 when the claim went stale.
func (a *API) checkOneClaim(store *claims.Store, resolver claims.Resolver, workspace, id string) int {
	claim, ok := store.ByKey(workspace, id)
	if !ok {
		return 0
	}
	outcome := resolver.CheckClaim(claim)
	if outcome.Stale {
		_ = store.MarkStale(id, workspace, outcome.Reason, outcome.Versions)
		return 1
	}
	_ = store.MarkVerified(id, workspace, outcome.Versions)
	return 0
}

// StaleFindings is the result of a full freshness pass over every active
// claim (wiki_lint).
type StaleFinding struct {
	Claim    claims.Claim
	Reason   string
	Resource string // the evidence resource that failed, when known
}

// CheckAllClaims re-resolves the evidence of every non-retracted claim and
// applies the outcomes. Claims with no evidence are skipped (nothing to
// verify). Returns the claims that are now stale, sorted by workspace and id.
func (a *API) CheckAllClaims() []StaleFinding {
	store := a.ClaimsStoreSnapshot()
	if store == nil {
		return nil
	}
	resolver := a.claimsResolver()
	var findings []StaleFinding
	for _, c := range store.All() {
		if c.Status == claims.StatusRetracted || len(c.Evidence) == 0 {
			continue
		}
		outcome := resolver.CheckClaim(c)
		if outcome.Stale {
			_ = store.MarkStale(c.ID, c.Workspace, outcome.Reason, outcome.Versions)
			findings = append(findings, StaleFinding{Claim: c, Reason: outcome.Reason})
		} else {
			_ = store.MarkVerified(c.ID, c.Workspace, outcome.Versions)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Claim.Workspace != findings[j].Claim.Workspace {
			return findings[i].Claim.Workspace < findings[j].Claim.Workspace
		}
		return findings[i].Claim.ID < findings[j].Claim.ID
	})
	return findings
}

// StaleClaimsForDocs returns the non-retracted claims attached to the given
// documents (Component.IDs), newest first, capped at limit. Used by the
// recall packet so an agent sees which facts about the matched documents may
// need re-verification.
func (a *API) StaleClaimsForDocs(docIDs []string, limit int) []claims.Claim {
	store := a.ClaimsStoreSnapshot()
	if store == nil || len(docIDs) == 0 {
		return nil
	}
	ids := map[string]bool{}
	for _, id := range docIDs {
		ids[id] = true
	}
	var out []claims.Claim
	for _, c := range store.List(claims.Filter{}) {
		if c.Status == claims.StatusRetracted || !ids[c.DocID] {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// claimLintIssues converts the claim layer's state into lint issues: every
// stale claim is a "stale-claim" issue, plus claims whose evidence cannot be
// resolved at all. The pass itself (CheckAllClaims) performs the freshness
// check, so lint output doubles as the scheduled verification OpenWiki calls
// preflight.
func (a *API) claimLintIssues(base []LintIssue) []LintIssue {
	store := a.ClaimsStoreSnapshot()
	if store == nil {
		return base
	}
	findings := a.CheckAllClaims()
	for _, f := range findings {
		c := f.Claim
		detail := fmt.Sprintf("%s: %s (workspace: %s, reason: %s, stale since: %s)",
			c.ID, c.Text, c.Workspace, f.Reason, orNow(c.StaleSince))
		if c.DocID != "" {
			detail += ", doc: " + c.DocID
		}
		base = append(base, LintIssue{Rule: "stale-claim", ComponentID: c.DocID, Detail: detail})
	}
	return base
}

func orNow(s string) string {
	if s == "" {
		return "just now"
	}
	return s
}
