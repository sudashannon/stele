package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"stele/internal/source"
)

func TestLoadWorkspaces(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workspaces.yaml")
	content := `
workspaces:
  - alias: miao
    path: /home/shanl/workspace/miao/openspec
    color: "#0063f8"
  - alias: wan2_2_deploy
    path: /home/shanl/workspace/wan2_2_deploy/openspec
    color: "#16a34a"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ws, err := LoadWorkspaces(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(ws))
	}
	if ws[0].Alias != "miao" || ws[0].Path != "/home/shanl/workspace/miao/openspec" || ws[0].Color != "#0063f8" {
		t.Fatalf("workspace[0] mismatch: %+v", ws[0])
	}
}

func TestLoadWorkspaces_MissingFileReturnsEmpty(t *testing.T) {
	ws, err := LoadWorkspaces(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("expected no error for missing config, got %v", err)
	}
	if len(ws) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(ws))
	}
}

func TestWorkspaceRegistry_AddPersistsAndUpdatesMemory(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workspaces.yaml")

	reg, err := NewWorkspaceRegistry(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.List()) != 0 {
		t.Fatalf("expected empty registry, got %d", len(reg.List()))
	}

	miaoPath := filepath.Join(t.TempDir(), "miao", "openspec")
	if err := os.MkdirAll(filepath.Join(miaoPath, "changes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: "miao", Path: miaoPath, Color: "#0063f8"}); err != nil {
		t.Fatal(err)
	}

	// in-memory reflects the addition immediately
	if len(reg.List()) != 1 || reg.List()[0].Alias != "miao" {
		t.Fatalf("expected in-memory registry to contain 'miao', got %+v", reg.List())
	}

	// a fresh load from disk also reflects it (proves it was persisted)
	reloaded, err := LoadWorkspaces(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded) != 1 || reloaded[0].Alias != "miao" {
		t.Fatalf("expected persisted config to contain 'miao', got %+v", reloaded)
	}
}

func TestWorkspaceRegistry_AddDuplicateAliasRejected(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))
	pathX := filepath.Join(t.TempDir(), "x")
	pathY := filepath.Join(t.TempDir(), "y")
	os.MkdirAll(filepath.Join(pathX, "changes"), 0755)
	os.MkdirAll(filepath.Join(pathY, "changes"), 0755)
	if err := reg.Add(WorkspaceConfig{Alias: "miao", Path: pathX, Color: "#000"}); err != nil {
		t.Fatalf("expected first Add to succeed, got %v", err)
	}
	err := reg.Add(WorkspaceConfig{Alias: "miao", Path: pathY, Color: "#111"})
	if err == nil {
		t.Fatal("expected an error when adding a duplicate alias")
	}
}

func TestWorkspaceRegistry_RemovePersistsAndPreservesSync(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "workspaces.yaml")
	content := `workspaces:
  - alias: keep
    path: /tmp/keep
  - alias: remove
    path: /tmp/remove
sync:
  enabled: true
  remote: git@example.com:team/knowledge.git
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewWorkspaceRegistry(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := reg.Remove("remove"); err != nil {
		t.Fatal(err)
	}
	if got := reg.List(); len(got) != 1 || got[0].Alias != "keep" {
		t.Fatalf("unexpected in-memory workspaces after removal: %+v", got)
	}
	persisted, err := LoadWorkspaces(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].Alias != "keep" {
		t.Fatalf("unexpected persisted workspaces after removal: %+v", persisted)
	}
	syncCfg, err := LoadSyncConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !syncCfg.Enabled || syncCfg.Remote != "git@example.com:team/knowledge.git" {
		t.Fatalf("sync config was not preserved: %+v", syncCfg)
	}
}

func TestWorkspaceRegistry_RemoveUnknownAlias(t *testing.T) {
	reg, err := NewWorkspaceRegistry(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove("missing"); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("expected ErrWorkspaceNotFound, got %v", err)
	}
}

func TestWorkspaceRegistry_RemovePersistenceFailureLeavesMemoryUnchanged(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := &WorkspaceRegistry{
		workspaces: []WorkspaceConfig{{Alias: "keep"}, {Alias: "remove"}},
		configPath: filepath.Join(blocker, "workspaces.yaml"),
		syncCfg:    SyncConfig{Enabled: true, Remote: "origin"},
	}

	if err := reg.Remove("remove"); err == nil {
		t.Fatal("expected persistence failure")
	}
	got := reg.List()
	if len(got) != 2 || got[0].Alias != "keep" || got[1].Alias != "remove" {
		t.Fatalf("registry changed despite persistence failure: %+v", got)
	}
}

func TestWorkspaceRegistry_RemoveConcurrent(t *testing.T) {
	const count = 16
	configPath := filepath.Join(t.TempDir(), "workspaces.yaml")
	workspaces := make([]WorkspaceConfig, count)
	for i := range workspaces {
		workspaces[i] = WorkspaceConfig{Alias: fmt.Sprintf("workspace-%d", i), Path: "/tmp"}
	}
	if err := persistWorkspaces(configPath, workspaces, SyncConfig{Enabled: true, Remote: "origin"}); err != nil {
		t.Fatal(err)
	}
	reg, err := NewWorkspaceRegistry(configPath)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := range workspaces {
		wg.Add(1)
		go func(alias string) {
			defer wg.Done()
			errs <- reg.Remove(alias)
		}(workspaces[i].Alias)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Remove failed: %v", err)
		}
	}
	if got := reg.List(); len(got) != 0 {
		t.Fatalf("expected empty in-memory registry, got %+v", got)
	}
	persisted, err := LoadWorkspaces(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 0 {
		t.Fatalf("expected empty persisted registry, got %+v", persisted)
	}
	syncCfg, err := LoadSyncConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !syncCfg.Enabled || syncCfg.Remote != "origin" {
		t.Fatalf("sync config was not preserved: %+v", syncCfg)
	}
}

func TestValidateWorkspacePathRejectsDirWithoutSupportedSource(t *testing.T) {
	// An existing, absolute, non-root directory with no supported source
	// layout must be rejected at registration time.
	dir := filepath.Join(t.TempDir(), "empty-workspace")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	err := validateWorkspacePath(dir)
	if err == nil {
		t.Fatal("expected an error for a workspace with no supported source layout")
	}
	if !strings.Contains(err.Error(), "OpenSpec, Trellis, or Superpowers") {
		t.Fatalf("expected error to list supported source layouts, got: %v", err)
	}
}

func TestValidateWorkspacePath_AcceptsDirWithChangesSubdir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "flat-workspace")
	if err := os.MkdirAll(filepath.Join(dir, "changes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspacePath(dir); err != nil {
		t.Fatalf("expected nil error for a dir with changes/, got %v", err)
	}
}

func TestValidateWorkspacePath_AcceptsRepoRootWithOpenspecChangesSubdir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo-root-workspace")
	if err := os.MkdirAll(filepath.Join(dir, "openspec", "changes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspacePath(dir); err != nil {
		t.Fatalf("expected nil error for a dir with openspec/changes/, got %v", err)
	}
}

func TestWorkspaceRegistry_AddTrellisDetectsAndPersistsType(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewWorkspaceRegistry(filepath.Join(dir, "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "trellis-project")
	if err := os.MkdirAll(filepath.Join(project, ".trellis", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: "trellis", Path: project, Color: "#123456"}); err != nil {
		t.Fatal(err)
	}
	got := reg.List()
	if len(got) != 1 || got[0].Type != "trellis" {
		t.Fatalf("expected detected Trellis type, got %+v", got)
	}
	reloaded, err := LoadWorkspaces(filepath.Join(dir, "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded) != 1 || reloaded[0].Type != "trellis" {
		t.Fatalf("expected persisted Trellis type, got %+v", reloaded)
	}
}

func TestValidateWorkspacePath_AcceptsTrellisRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trellis-project")
	if err := os.MkdirAll(filepath.Join(dir, ".trellis", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspacePath(dir); err != nil {
		t.Fatalf("expected Trellis root to be accepted, got %v", err)
	}
}

func TestWorkspaceRegistry_AddSuperpowersDetectsAndPersistsType(t *testing.T) {
	configDir := t.TempDir()
	reg, err := NewWorkspaceRegistry(filepath.Join(configDir, "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "superpowers-project")
	if err := os.MkdirAll(filepath.Join(project, "docs", "superpowers", "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(WorkspaceConfig{Alias: "superpowers", Path: project, Color: "#123456"}); err != nil {
		t.Fatal(err)
	}
	got := reg.List()
	if len(got) != 1 || got[0].Type != "superpowers" {
		t.Fatalf("expected detected Superpowers type, got %+v", got)
	}
	reloaded, err := LoadWorkspaces(filepath.Join(configDir, "workspaces.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded) != 1 || reloaded[0].Type != "superpowers" {
		t.Fatalf("expected persisted Superpowers type, got %+v", reloaded)
	}
}

func TestNormalizeWorkspaceConfigRejectsSuperpowersMixedWithOpenSpec(t *testing.T) {
	project := filepath.Join(t.TempDir(), "mixed-project")
	for _, dir := range []string{"openspec/changes", "docs/superpowers/specs"} {
		if err := os.MkdirAll(filepath.Join(project, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := normalizeWorkspaceConfig(WorkspaceConfig{
		Alias: "ambiguous",
		Path:  project,
		Type:  source.KindSuperpowers,
	}); err == nil {
		t.Fatal("expected an explicitly Superpowers mixed project to be rejected")
	}
}

func TestNormalizeWorkspaceConfigRejectsSuperpowersSubdirectory(t *testing.T) {
	project := filepath.Join(t.TempDir(), "superpowers-project")
	docsRoot := filepath.Join(project, "docs", "superpowers")
	if err := os.MkdirAll(filepath.Join(docsRoot, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeWorkspaceConfig(WorkspaceConfig{
		Alias: "not-project-root",
		Path:  docsRoot,
		Type:  source.KindSuperpowers,
	}); err == nil {
		t.Fatal("expected docs/superpowers itself to be rejected")
	}
}
