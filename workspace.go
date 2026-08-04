package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"stele/internal/source"

	"gopkg.in/yaml.v3"
)

type WorkspaceConfig = source.Workspace

type workspacesFile struct {
	Workspaces []WorkspaceConfig `yaml:"workspaces"`
	Sync       SyncConfig        `yaml:"sync"`
}

var ErrWorkspaceNotFound = errors.New("workspace not found")

// SyncConfig configures the optional knowledge-mirror git repository: a
// single git repo at <data dir>/knowledge-repo mirroring all indexed
// wiki documents from every workspace, auto-committed on file changes.
// Enabled defaults to false (opt-in); Remote, if set, is pushed to after
// each commit.
type SyncConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Remote  string `yaml:"remote" json:"remote"`
}

// LoadSyncConfig reads the top-level `sync:` section of the workspace
// registry config. A missing file or missing section is not an error —
// it means mirroring is disabled.
func LoadSyncConfig(configPath string) (SyncConfig, error) {
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return SyncConfig{}, nil
	}
	if err != nil {
		return SyncConfig{}, err
	}
	var f workspacesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return SyncConfig{}, err
	}
	return f.Sync, nil
}

// LoadWorkspaces reads the workspace registry config. A missing file is not
// an error — it means no workspaces are registered yet.
func LoadWorkspaces(configPath string) ([]WorkspaceConfig, error) {
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return []WorkspaceConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f workspacesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Workspaces == nil {
		return []WorkspaceConfig{}, nil
	}
	return f.Workspaces, nil
}

type WorkspaceRegistry struct {
	mu         sync.RWMutex
	workspaces []WorkspaceConfig
	configPath string
	syncCfg    SyncConfig
}

func NewWorkspaceRegistry(configPath string) (*WorkspaceRegistry, error) {
	ws, err := LoadWorkspaces(configPath)
	if err != nil {
		return nil, err
	}
	for i := range ws {
		if _, parseErr := source.ParseKind(ws[i].Type); parseErr != nil {
			return nil, parseErr
		}
		if kind, resolveErr := source.ResolveKind(ws[i].Path, ws[i].Type); resolveErr == nil {
			ws[i].Type = kind
		}
	}
	syncCfg, err := LoadSyncConfig(configPath)
	if err != nil {
		return nil, err
	}
	return &WorkspaceRegistry{workspaces: ws, configPath: configPath, syncCfg: syncCfg}, nil
}

// Sync returns the knowledge-mirror sync configuration read from the
// registry's config file (the top-level `sync:` section).
func (r *WorkspaceRegistry) Sync() SyncConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.syncCfg
}

// SetSyncRemote updates the mirror's git remote URL and persists it to the
// registry's config file. Sync is enabled automatically once a non-empty
// remote is set, and disabled when the remote is cleared -- there is no
// separate enabled toggle exposed to the UI, so the remote field alone
// drives whether GET /api/sync attempts a push/pull.
func (r *WorkspaceRegistry) SetSyncRemote(remote string) (SyncConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	updated := r.syncCfg
	updated.Remote = remote
	updated.Enabled = remote != ""

	if err := persistWorkspaces(r.configPath, r.workspaces, updated); err != nil {
		return SyncConfig{}, err
	}
	r.syncCfg = updated
	return updated, nil
}

func (r *WorkspaceRegistry) List() []WorkspaceConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkspaceConfig, len(r.workspaces))
	copy(out, r.workspaces)
	return out
}

func (r *WorkspaceRegistry) Add(cfg WorkspaceConfig) error {
	normalized, err := normalizeWorkspaceConfig(cfg)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, w := range r.workspaces {
		if w.Alias == normalized.Alias {
			return fmt.Errorf("workspace alias %q already registered", normalized.Alias)
		}
	}

	updated := append(r.workspaces, normalized)
	if err := persistWorkspaces(r.configPath, updated, r.syncCfg); err != nil {
		return err
	}
	r.workspaces = updated
	return nil
}

func (r *WorkspaceRegistry) Remove(alias string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	index := -1
	for i := range r.workspaces {
		if r.workspaces[i].Alias == alias {
			index = i
			break
		}
	}
	if index == -1 {
		return fmt.Errorf("%w: %q", ErrWorkspaceNotFound, alias)
	}

	updated := make([]WorkspaceConfig, 0, len(r.workspaces)-1)
	updated = append(updated, r.workspaces[:index]...)
	updated = append(updated, r.workspaces[index+1:]...)
	if err := persistWorkspaces(r.configPath, updated, r.syncCfg); err != nil {
		return err
	}
	r.workspaces = updated
	return nil
}

// validateWorkspacePath preserves the legacy auto-detecting validation entry
// point used by tests and callers that do not yet provide a source type.
func validateWorkspacePath(path string) error {
	_, err := normalizeWorkspaceConfig(WorkspaceConfig{Path: path})
	return err
}

func normalizeWorkspaceConfig(cfg WorkspaceConfig) (WorkspaceConfig, error) {
	if !filepath.IsAbs(cfg.Path) {
		return WorkspaceConfig{}, fmt.Errorf("workspace path %q must be an absolute path", cfg.Path)
	}

	info, err := os.Stat(cfg.Path)
	if err != nil {
		return WorkspaceConfig{}, fmt.Errorf("workspace path %q is not accessible: %w", cfg.Path, err)
	}
	if !info.IsDir() {
		return WorkspaceConfig{}, fmt.Errorf("workspace path %q is not a directory", cfg.Path)
	}

	clean := filepath.Clean(cfg.Path)
	segments := strings.FieldsFunc(clean, func(c rune) bool { return c == filepath.Separator })
	if len(segments) < 2 {
		return WorkspaceConfig{}, fmt.Errorf("workspace path %q must not be the filesystem root or a direct child of it", cfg.Path)
	}

	kind, err := source.ResolveKind(clean, cfg.Type)
	if err != nil {
		return WorkspaceConfig{}, err
	}
	switch kind {
	case source.KindOpenSpec:
		if !source.HasOpenSpecLayout(clean) {
			return WorkspaceConfig{}, fmt.Errorf("workspace path %q 下未找到 openspec/changes 目录，不是有效的 OpenSpec 工作区", cfg.Path)
		}
	case source.KindTrellis:
		if !source.HasTrellisLayout(clean) {
			return WorkspaceConfig{}, fmt.Errorf("workspace path %q 下未找到 .trellis/tasks、.trellis/spec 或 .trellis/workspace，不是有效的 Trellis 工作区", cfg.Path)
		}
	case source.KindSuperpowers:
		if !source.HasSuperpowersLayout(clean) {
			return WorkspaceConfig{}, fmt.Errorf("workspace path %q 下未找到 docs/superpowers 持久产物目录，不是有效的 Superpowers 项目根目录", cfg.Path)
		}
	case source.KindDocs:
		if !source.HasDocsLayout(clean) {
			return WorkspaceConfig{}, fmt.Errorf("workspace path %q 下未找到可索引的 markdown 文档（已排除 vendored 依赖、构建产物、与源码同目录的文档和模块 README）", cfg.Path)
		}
	}

	cfg.Path = clean
	cfg.Type = kind
	return cfg, nil
}

func persistWorkspaces(configPath string, ws []WorkspaceConfig, syncCfg SyncConfig) error {
	f := workspacesFile{Workspaces: ws, Sync: syncCfg}
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}

	dir := filepath.Dir(configPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(configPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, configPath)
}
