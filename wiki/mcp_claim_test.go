package wiki

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"stele/internal/claims"
)

const claimTestToken = "claim-test-token"

func helperMCPClaimsAPI(t *testing.T, ws ...string) (*API, *claims.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := claims.NewStore(filepath.Join(dir, "claims.json"))
	if err != nil {
		t.Fatalf("claims.NewStore: %v", err)
	}
	workspace := "rx101"
	if len(ws) > 0 {
		workspace = ws[0]
	}
	workspaces := []WorkspaceConfig{{Alias: workspace, Path: filepath.Join(dir, workspace)}}
	api, err := NewAPIWithWorkspaces(workspaces, filepath.Join(dir, "wiki"))
	if err != nil {
		t.Fatalf("NewAPIWithWorkspaces: %v", err)
	}
	api.SetTodoStore(nil, []byte(claimTestToken))
	api.SetClaimsStore(store)
	return api, store
}

func claimUpsertRPC(t *testing.T, tool string, args map[string]any, token string) *http.Request {
	t.Helper()
	return mcpTodoRPC(t, float64(1), tool, args, token)
}

func claimUpsertArgs() map[string]any {
	return map[string]any{
		"workspace": "rx101",
		"claims": []any{
			map[string]any{
				"id":     "claim.rx101-pcie",
				"kind":   "fact",
				"truth":  "code_verified",
				"intent": "intended",
				"text":   "PCIe 链路速率锁定为 Gen3 x4",
			},
		},
	}
}

func TestMCP_ClaimUpsertRequiresToken(t *testing.T) {
	api, _ := helperMCPClaimsAPI(t)
	req := claimUpsertRPC(t, "wiki_claim_upsert", claimUpsertArgs(), "")
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)
	text := mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if !strings.Contains(text, "write access denied") {
		t.Fatalf("want write access denied, got %q", text)
	}
}

func TestMCP_ClaimUpsertIdempotent(t *testing.T) {
	api, store := helperMCPClaimsAPI(t)
	req := claimUpsertRPC(t, "wiki_claim_upsert", claimUpsertArgs(), claimTestToken)
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)
	text := mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if !strings.Contains(text, `"applied": 1`) {
		t.Fatalf("want applied 1, got %q", text)
	}
	if len(store.All()) != 1 {
		t.Fatalf("store should hold 1 claim, got %d", len(store.All()))
	}

	// Same id again: overwrite, not duplicate. Fresh request body each time.
	rec2 := httptest.NewRecorder()
	api.HandleMCP(rec2, claimUpsertRPC(t, "wiki_claim_upsert", claimUpsertArgs(), claimTestToken))
	text = mcpContentText(t, parseMCPResult(t, rec2.Body.Bytes()))
	if !strings.Contains(text, `"applied": 1`) {
		t.Fatalf("re-upsert should apply 1, got %q", text)
	}
	if len(store.All()) != 1 {
		t.Fatalf("re-upsert must not duplicate, got %d claims", len(store.All()))
	}
}

func TestMCP_ClaimUpsertRejectsUnknownWorkspace(t *testing.T) {
	api, _ := helperMCPClaimsAPI(t)
	args := claimUpsertArgs()
	args["workspace"] = "nope"
	req := claimUpsertRPC(t, "wiki_claim_upsert", args, claimTestToken)
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)
	text := mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if !strings.Contains(text, "unknown workspace") {
		t.Fatalf("want unknown workspace, got %q", text)
	}
}

func TestMCP_ClaimUpsertRejectsBadEnum(t *testing.T) {
	api, store := helperMCPClaimsAPI(t)
	args := claimUpsertArgs()
	claimsList := args["claims"].([]any)
	item := claimsList[0].(map[string]any)
	item["kind"] = "banana"
	req := claimUpsertRPC(t, "wiki_claim_upsert", args, claimTestToken)
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)
	text := mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if !strings.Contains(text, "upsert failed") {
		t.Fatalf("want upsert failure, got %q", text)
	}
	if len(store.All()) != 0 {
		t.Fatalf("invalid claim must not be written, got %d", len(store.All()))
	}
}

func TestMCP_ClaimListFiltersByWorkspace(t *testing.T) {
	api, _ := helperMCPClaimsAPI(t)
	req := claimUpsertRPC(t, "wiki_claim_upsert", claimUpsertArgs(), claimTestToken)
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status %d", rec.Code)
	}

	listReq := claimUpsertRPC(t, "wiki_claims", map[string]any{"workspace": "rx101"}, "")
	rec = httptest.NewRecorder()
	api.HandleMCP(rec, listReq)
	text := mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if !strings.Contains(text, "claim.rx101-pcie") {
		t.Fatalf("list should contain the claim: %q", text)
	}

	other := claimUpsertRPC(t, "wiki_claims", map[string]any{"workspace": "other"}, "")
	rec = httptest.NewRecorder()
	api.HandleMCP(rec, other)
	text = mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if strings.Contains(text, "claim.rx101-pcie") {
		t.Fatalf("workspace filter must exclude the claim: %q", text)
	}
}

func TestMCP_ClaimGet(t *testing.T) {
	api, _ := helperMCPClaimsAPI(t)
	req := claimUpsertRPC(t, "wiki_claim_upsert", claimUpsertArgs(), claimTestToken)
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	getReq := claimUpsertRPC(t, "wiki_claim_get", map[string]any{"id": "claim.rx101-pcie"}, "")
	rec = httptest.NewRecorder()
	api.HandleMCP(rec, getReq)
	text := mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if !strings.Contains(text, "claim.rx101-pcie") || !strings.Contains(text, "PCIe 链路速率锁定为 Gen3 x4") {
		t.Fatalf("get should return the claim: %q", text)
	}

	missing := claimUpsertRPC(t, "wiki_claim_get", map[string]any{"id": "claim.nope"}, "")
	rec = httptest.NewRecorder()
	api.HandleMCP(rec, missing)
	text = mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if !strings.Contains(text, "claim not found") {
		t.Fatalf("want not found, got %q", text)
	}
}

func TestMCP_ClaimSearchLexicalFallback(t *testing.T) {
	api, _ := helperMCPClaimsAPI(t)
	req := claimUpsertRPC(t, "wiki_claim_upsert", claimUpsertArgs(), claimTestToken)
	rec := httptest.NewRecorder()
	api.HandleMCP(rec, req)

	// No embedding script in the test environment: the search must fall back
	// to the substring match and still find the claim.
	searchReq := claimUpsertRPC(t, "wiki_claim_search", map[string]any{"query": "PCIe 链路速率"}, "")
	rec = httptest.NewRecorder()
	api.HandleMCP(rec, searchReq)
	text := mcpContentText(t, parseMCPResult(t, rec.Body.Bytes()))
	if !strings.Contains(text, "claim.rx101-pcie") {
		t.Fatalf("lexical fallback should find the claim: %q", text)
	}
}

// staleClaimFixture writes a workspace document, upserts a doc-backed claim,
// verifies it fresh, then mutates the document so the evidence version drifts.
// Returns the absolute document path (the claim's DocID).
func staleClaimFixture(t *testing.T) (*API, *claims.Store, string) {
	t.Helper()
	api, store := helperMCPClaimsAPI(t)
	doc := filepath.Join(api.ws[0].Path, "a.md")
	if err := os.MkdirAll(api.ws[0].Path, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(doc, []byte("v1 content\n"), 0644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	claim := claims.Claim{
		ID: "claim.rx101-a", Workspace: "rx101", DocID: doc,
		Kind: claims.KindFact, Truth: claims.TruthCodeVerified, Intent: claims.IntentIntended,
		Text:     "a.md records the bringup result",
		Status:   claims.StatusActive,
		Evidence: []claims.Evidence{{Resource: "doc://rx101/a.md"}},
	}
	if _, err := store.Upsert([]claims.Claim{claim}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// First pass resolves and records the evidence version: the claim is fresh.
	if findings := api.CheckAllClaims(); len(findings) != 0 {
		t.Fatalf("fresh claim must not be stale: %+v", findings)
	}
	if err := os.WriteFile(doc, []byte("v2 content\n"), 0644); err != nil {
		t.Fatalf("mutate doc: %v", err)
	}
	return api, store, doc
}

func TestClaimLint_StaleClaimSurfaces(t *testing.T) {
	api, _, doc := staleClaimFixture(t)

	rec := httptest.NewRecorder()
	api.HandleLint(rec, httptest.NewRequest("GET", "/api/wiki/lint", nil))
	var issues []LintIssue
	if err := json.NewDecoder(rec.Body).Decode(&issues); err != nil {
		t.Fatalf("decode lint response: %v", err)
	}
	for _, issue := range issues {
		if issue.Rule == "stale-claim" && issue.ComponentID == doc {
			return
		}
	}
	t.Fatalf("want stale-claim issue for %s, got %+v", doc, issues)
}

func TestContextPacket_IncludesStaleClaims(t *testing.T) {
	api, _, _ := staleClaimFixture(t)
	// The fixture left the document mutated; a freshness pass flips the claim.
	findings := api.CheckAllClaims()
	if len(findings) != 1 || findings[0].Claim.ID != "claim.rx101-a" {
		t.Fatalf("want exactly the fixture claim stale, got %+v", findings)
	}

	packet := api.BuildContextPacket("bringup", nil, 5)
	if len(packet.Claims) != 1 {
		t.Fatalf("want exactly 1 stale-claim hit in the recall packet, got %+v", packet.Claims)
	}
	hit := packet.Claims[0]
	if hit.ID != "claim.rx101-a" || hit.Workspace != "rx101" || hit.StaleReason != claims.StaleReasonVersionChanged {
		t.Fatalf("unexpected stale-claim hit: %+v", hit)
	}
	if len(hit.Evidence) != 1 || !strings.Contains(hit.Evidence[0], "a.md") {
		t.Fatalf("evidence resource missing from hit: %+v", hit.Evidence)
	}
}

func TestWorkspaceKeyForFile_NestedWorkspaceWins(t *testing.T) {
	dir := t.TempDir()
	outer := filepath.Join(dir, "outer")
	inner := filepath.Join(dir, "outer", "inner")
	// Both resolve as OpenSpec project roots (ProjectRoot -> parent of openspec/).
	os.MkdirAll(filepath.Join(outer, "openspec", "changes"), 0755)
	os.MkdirAll(filepath.Join(inner, "openspec", "changes"), 0755)
	api, err := NewAPIWithWorkspaces(
		[]WorkspaceConfig{
			{Alias: "outer", Path: outer, Type: "openspec"},
			{Alias: "inner", Path: inner, Type: "openspec"},
		},
		filepath.Join(dir, "wiki"),
	)
	if err != nil {
		t.Fatalf("NewAPIWithWorkspaces: %v", err)
	}
	// A file inside the nested workspace must attribute to the nested one,
	// even though the outer root is registered first and is also a prefix.
	ws, rel := api.workspaceKeyForFile(filepath.Join(inner, "knowledge", "a.md"))
	if ws != "inner" || rel != filepath.Join("knowledge", "a.md") {
		t.Fatalf("nested file attributed to %q/%q, want inner/%s", ws, rel, filepath.Join("knowledge", "a.md"))
	}
	// A file that only lies under the outer root still attributes to outer.
	ws, rel = api.workspaceKeyForFile(filepath.Join(outer, "docs", "b.md"))
	if ws != "outer" || rel != filepath.Join("docs", "b.md") {
		t.Fatalf("outer file attributed to %q/%q, want outer/%s", ws, rel, filepath.Join("docs", "b.md"))
	}
}
