package wiki

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"stele/internal/claims"
)

// mcpClaimUnavailable is the standard reply when the claims layer is not
// wired (no store): it tells the caller the feature is off, not that the
// search simply found nothing.
func mcpClaimUnavailable() mcpToolResult {
	return mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: "claims store not available (stele service not wired with a claims store)"}},
		IsError: true,
	}
}

func mcpClaimStatusMarker(c claims.Claim) string {
	switch c.Status {
	case claims.StatusStale:
		return " [stale:" + c.StaleReason + "]"
	case claims.StatusRetracted:
		return " [retracted]"
	}
	return ""
}

// mcpClaimSearch implements wiki_claim_search.
func (a *API) mcpClaimSearch(args map[string]any) mcpToolResult {
	if a.ClaimsStoreSnapshot() == nil {
		return mcpClaimUnavailable()
	}
	query, _ := args["query"].(string)
	if query == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "query is required"}}, IsError: true}
	}
	workspace, _ := args["workspace"].(string)
	kind, _ := args["kind"].(string)
	limit := 5
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	hits := a.claimSearch(query, workspace, kind, limit)
	if len(hits) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "未找到匹配的断言。"}}}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d claims for %q:\n", len(hits), query)
	for i, hit := range hits {
		c := hit.Claim
		fmt.Fprintf(&sb, "%d. [%s] %s (workspace: %s, kind: %s, similarity: %s%%)%s\n",
			i+1, c.ID, c.Text, c.Workspace, c.Kind,
			strconv.FormatFloat(hit.Similarity*100, 'f', 0, 64), mcpClaimStatusMarker(c))
	}
	sb.WriteString("\nUse wiki_claim_get for full evidence and version state.\n")
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

// mcpClaimGet implements wiki_claim_get.
func (a *API) mcpClaimGet(args map[string]any) mcpToolResult {
	store := a.ClaimsStoreSnapshot()
	if store == nil {
		return mcpClaimUnavailable()
	}
	id, _ := args["id"].(string)
	if id == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "id is required"}}, IsError: true}
	}
	var found *claims.Claim
	for _, c := range store.All() {
		if c.ID == id {
			found = &c
			break
		}
	}
	if found == nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "claim not found: " + id}}, IsError: true}
	}
	c := *found
	var sb strings.Builder
	fmt.Fprintf(&sb, "claim: %s\n", c.ID)
	fmt.Fprintf(&sb, "workspace: %s\n", c.Workspace)
	if c.DocID != "" {
		fmt.Fprintf(&sb, "doc: %s\n", c.DocID)
	}
	fmt.Fprintf(&sb, "kind: %s, truth: %s, intent: %s\n", c.Kind, c.Truth, c.Intent)
	fmt.Fprintf(&sb, "status: %s", c.Status)
	if c.StaleReason != "" {
		fmt.Fprintf(&sb, " (stale since %s, reason: %s)", c.StaleSince, c.StaleReason)
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "text: %s\n", c.Text)
	if len(c.CodeAnchors) > 0 {
		fmt.Fprintf(&sb, "code anchors:\n")
		for _, anchor := range c.CodeAnchors {
			fmt.Fprintf(&sb, "  - %s\n", anchor)
		}
	}
	if len(c.Evidence) > 0 {
		fmt.Fprintf(&sb, "evidence:\n")
		for _, ev := range c.Evidence {
			fmt.Fprintf(&sb, "  - %s", ev.Resource)
			if ev.Version != "" {
				fmt.Fprintf(&sb, " (verified at %s)", ev.Version)
			}
			sb.WriteString("\n")
		}
	}
	if len(c.Tags) > 0 {
		fmt.Fprintf(&sb, "tags: %s\n", strings.Join(c.Tags, ", "))
	}
	fmt.Fprintf(&sb, "updated: %s\n", c.UpdatedAt)
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

// mcpClaimsList implements wiki_claims.
func (a *API) mcpClaimsList(args map[string]any) mcpToolResult {
	store := a.ClaimsStoreSnapshot()
	if store == nil {
		return mcpClaimUnavailable()
	}
	filter := claims.Filter{}
	workspace, _ := args["workspace"].(string)
	status, _ := args["status"].(string)
	kind, _ := args["kind"].(string)
	doc, _ := args["doc"].(string)
	filter.Workspace = workspace
	filter.Status = status
	filter.Kind = kind
	limit := 20
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var out []claims.Claim
	for _, c := range store.List(filter) {
		if doc != "" && c.DocID != doc {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "no claims match the filters."}}}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d claims (showing %d):\n", len(store.All()), len(out))
	for i, c := range out {
		fmt.Fprintf(&sb, "%d. [%s] %s (workspace: %s, kind: %s, status: %s)%s\n",
			i+1, c.ID, c.Text, c.Workspace, c.Kind, c.Status, mcpClaimStatusMarker(c))
	}
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: sb.String()}}}
}

// claimInput is the wire shape of one claim in wiki_claim_upsert.
type claimInput struct {
	ID          string   `json:"id"`
	DocID       string   `json:"docId"`
	Kind        string   `json:"kind"`
	Truth       string   `json:"truth"`
	Intent      string   `json:"intent"`
	Text        string   `json:"text"`
	CodeAnchors []string `json:"codeAnchors"`
	Evidence    []struct {
		Resource string `json:"resource"`
	} `json:"evidence"`
	Tags   []string `json:"tags"`
	Status string   `json:"status"`
}

// mcpClaimUpsert implements wiki_claim_upsert (write, loopback + Bearer).
func (a *API) mcpClaimUpsert(r *http.Request, args map[string]any) mcpToolResult {
	store := a.ClaimsStoreSnapshot()
	if store == nil {
		return mcpClaimUnavailable()
	}
	a.mu.RLock()
	token := a.todoToken
	a.mu.RUnlock()
	if !a.mcpAuth(r, token) {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "write access denied: loopback + Bearer token required"}}, IsError: true}
	}

	workspace, _ := args["workspace"].(string)
	if workspace == "" {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "workspace is required"}}, IsError: true}
	}
	// Workspace must be a registered alias (same rule as todo writes).
	registered := false
	for _, ws := range a.workspacesSnapshot() {
		if ws.Alias == workspace {
			registered = true
			break
		}
	}
	if !registered {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "unknown workspace: " + workspace}}, IsError: true}
	}

	raw, ok := args["claims"].([]any)
	if !ok || len(raw) == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "claims must be a non-empty array"}}, IsError: true}
	}
	in := make([]claims.Claim, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("claims[%d] must be an object", i)}}, IsError: true}
		}
		var ci claimInput
		data, _ := json.Marshal(obj)
		if err := json.Unmarshal(data, &ci); err != nil {
			return mcpToolResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("claims[%d]: %v", i, err)}}, IsError: true}
		}
		c := claims.Claim{
			ID:          ci.ID,
			Workspace:   workspace,
			DocID:       ci.DocID,
			Kind:        claims.Kind(ci.Kind),
			Truth:       claims.Truth(ci.Truth),
			Intent:      claims.Intent(ci.Intent),
			Text:        ci.Text,
			CodeAnchors: ci.CodeAnchors,
			Tags:        ci.Tags,
			Status:      claims.Status(ci.Status),
		}
		for _, ev := range ci.Evidence {
			c.Evidence = append(c.Evidence, claims.Evidence{Resource: ev.Resource})
		}
		in = append(in, c)
	}

	applied, err := store.Upsert(in)
	if err != nil {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "upsert failed: " + err.Error()}}, IsError: true}
	}
	if applied == 0 {
		return mcpToolResult{Content: []mcpContent{{Type: "text", Text: "no claims applied"}}, IsError: true}
	}
	// Refresh vectors for the written claims (non-fatal on failure).
	ids := make([]string, 0, applied)
	for _, c := range in {
		ids = append(ids, c.ID)
	}
	a.refreshClaimVectors(ids)
	if a.SSE != nil {
		a.SSE.BroadcastNamed("claims-updated", fmt.Sprintf(`{"applied":%d}`, applied))
	}
	data, _ := json.MarshalIndent(map[string]any{"applied": applied, "workspace": workspace}, "", "  ")
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: string(data)}}}
}
