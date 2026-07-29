package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kind identifies the workflow format stored in a workspace.
type Kind string

const (
	KindOpenSpec    Kind = "openspec"
	KindTrellis     Kind = "trellis"
	KindSuperpowers Kind = "superpowers"
)

// Workspace is the source-neutral workspace configuration shared by the HTTP
// layer and the wiki indexer. Type is optional on disk for backward
// compatibility; ResolveKind fills it from the workspace layout.
type Workspace struct {
	Alias string `yaml:"alias" json:"alias"`
	Path  string `yaml:"path" json:"path"`
	Color string `yaml:"color" json:"color"`
	Type  Kind   `yaml:"type,omitempty" json:"type,omitempty"`
}

// ParseKind validates an explicitly configured source kind.
func ParseKind(value Kind) (Kind, error) {
	switch value {
	case KindOpenSpec, KindTrellis, KindSuperpowers:
		return value, nil
	case "":
		return "", nil
	default:
		return "", fmt.Errorf("unsupported workspace type %q: must be openspec, trellis, or superpowers", value)
	}
}

// ResolveKind returns the explicit kind or detects it from the filesystem.
// Automatic detection is ownership-ordered: OpenSpec first, then Trellis,
// then standalone Superpowers. An explicitly selected Superpowers workspace
// remains strict: it must be a pure project root, never a docs subdirectory
// or a mixed workflow repository.
func ResolveKind(path string, configured Kind) (Kind, error) {
	kind, err := ParseKind(configured)
	if err != nil {
		return "", err
	}
	if kind != "" {
		if kind == KindSuperpowers {
			if !HasSuperpowersLayout(path) {
				return "", fmt.Errorf("workspace path %q does not contain a project-root docs/superpowers layout", path)
			}
			if HasOpenSpecLayout(path) || HasTrellisLayout(path) {
				return "", fmt.Errorf("workspace path %q mixes Superpowers with OpenSpec or Trellis", path)
			}
		}
		return kind, nil
	}

	hasOpenSpec := HasOpenSpecLayout(path)
	hasTrellis := HasTrellisLayout(path)
	hasSuperpowers := HasSuperpowersLayout(path)
	switch {
	case hasOpenSpec:
		return KindOpenSpec, nil
	case hasTrellis:
		return KindTrellis, nil
	case hasSuperpowers:
		return KindSuperpowers, nil
	default:
		return "", fmt.Errorf("workspace path %q contains no OpenSpec, Trellis, or Superpowers data", path)
	}
}

// HasOpenSpecLayout accepts both a directly registered openspec directory and
// the natural repository root that contains openspec/changes.
func HasOpenSpecLayout(path string) bool {
	return isDir(filepath.Join(path, "changes")) || isDir(filepath.Join(path, "openspec", "changes"))
}

// HasTrellisLayout requires the project-level .trellis directory and at least
// one durable data tree. Runtime-only .trellis/.runtime is intentionally not
// sufficient.
func HasTrellisLayout(path string) bool {
	trellisDir := filepath.Join(path, ".trellis")
	if !isDir(trellisDir) {
		return false
	}
	return isDir(filepath.Join(trellisDir, "tasks")) ||
		isDir(filepath.Join(trellisDir, "spec")) ||
		isDir(filepath.Join(trellisDir, "workspace"))
}

// HasSuperpowersLayout accepts only a project root that owns at least one
// durable docs/superpowers content directory. Passing docs/superpowers itself
// intentionally fails so a Comet artifact subtree cannot be registered as a
// second, standalone workflow.
func HasSuperpowersLayout(path string) bool {
	roots, safe := resolvedSuperpowersRoots(path)
	return safe && len(roots) > 0
}

// SuperpowersRoots returns the four allowlisted workflow roots plus the
// project-level knowledge directory when it exists.
func SuperpowersRoots(path string) []string {
	roots, safe := resolvedSuperpowersRoots(path)
	if !safe {
		return nil
	}
	return roots
}

func resolvedSuperpowersRoots(path string) ([]string, bool) {
	projectRoot := filepath.Clean(path)
	root := filepath.Join(projectRoot, "docs", "superpowers")
	if !isDir(root) || !resolvedPathWithin(root, projectRoot) {
		return nil, false
	}
	candidates := []string{
		filepath.Join(root, "specs"),
		filepath.Join(root, "plans"),
		filepath.Join(root, "artifacts"),
		filepath.Join(root, "reports"),
	}
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if !isDir(candidate) {
			continue
		}
		if !resolvedPathWithin(candidate, projectRoot) {
			return nil, false
		}
		roots = append(roots, filepath.Clean(candidate))
	}
	if len(roots) > 0 {
		knowledgeRoot := filepath.Join(projectRoot, "knowledge")
		if isDir(knowledgeRoot) {
			if !resolvedPathWithin(knowledgeRoot, projectRoot) {
				return nil, false
			}
			roots = append(roots, filepath.Clean(knowledgeRoot))
		}
	}
	return roots, true
}

func resolvedPathWithin(path, root string) bool {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// OpenSpecPath normalizes a registered repo root or openspec directory to the
// directory that directly owns changes/. Paths without an OpenSpec layout are
// returned unchanged so existing plain-document workspaces remain scannable.
func OpenSpecPath(path string) string {
	clean := filepath.Clean(path)
	if !isDir(filepath.Join(clean, "changes")) && isDir(filepath.Join(clean, "openspec", "changes")) {
		return filepath.Join(clean, "openspec")
	}
	return clean
}

// ProjectRoot returns the root against which source-relative paths resolve.
func ProjectRoot(workspace Workspace) string {
	if workspace.Type == KindTrellis || workspace.Type == KindSuperpowers {
		return filepath.Clean(workspace.Path)
	}
	openspecPath := OpenSpecPath(workspace.Path)
	if isDir(filepath.Join(openspecPath, "changes")) || filepath.Base(openspecPath) == "openspec" {
		return filepath.Dir(openspecPath)
	}
	return filepath.Clean(workspace.Path)
}

// ScanRoots returns durable content roots for initial indexing. Trellis
// runtime/session state is excluded; standalone Superpowers is restricted to
// its workflow artifact directories and project-level knowledge directory.
func ScanRoots(workspace Workspace) []string {
	kind, err := ResolveKind(workspace.Path, workspace.Type)
	if err == nil {
		workspace.Type = kind
	}
	switch workspace.Type {
	case KindTrellis:
		root := filepath.Join(filepath.Clean(workspace.Path), ".trellis")
		return existingDirs(
			filepath.Join(root, "tasks"),
			filepath.Join(root, "spec"),
			filepath.Join(root, "workspace"),
		)
	case KindSuperpowers:
		return SuperpowersRoots(workspace.Path)
	}

	openspecPath := OpenSpecPath(workspace.Path)
	if !isDir(filepath.Join(openspecPath, "changes")) {
		if isDir(openspecPath) {
			return []string{openspecPath}
		}
		return nil
	}

	roots := []string{openspecPath}
	projectRoot := filepath.Dir(openspecPath)
	roots = append(roots, existingDirs(
		filepath.Join(projectRoot, "docs"),
		filepath.Join(projectRoot, "knowledge"),
	)...)
	entries, err := os.ReadDir(projectRoot)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == "design_docs" || (strings.HasSuffix(name, "_docs") && len(name) > len("_docs")) {
				roots = append(roots, filepath.Join(projectRoot, name))
			}
		}
	}
	return uniquePaths(roots)
}

// WatchRoots returns the trigger surface for incremental updates. It watches
// each workflow's durable parent so new content directories are discovered.
func WatchRoots(workspace Workspace) []string {
	kind, err := ResolveKind(workspace.Path, workspace.Type)
	if err == nil {
		workspace.Type = kind
	}
	switch workspace.Type {
	case KindTrellis:
		root := filepath.Join(filepath.Clean(workspace.Path), ".trellis")
		if isDir(root) {
			return []string{root}
		}
		return nil
	case KindSuperpowers:
		projectRoot := filepath.Clean(workspace.Path)
		superpowersRoot := filepath.Join(projectRoot, "docs", "superpowers")
		if len(SuperpowersRoots(workspace.Path)) == 0 || !resolvedPathWithin(superpowersRoot, projectRoot) {
			return nil
		}
		roots := []string{superpowersRoot}
		knowledgeRoot := filepath.Join(projectRoot, "knowledge")
		if isDir(knowledgeRoot) && resolvedPathWithin(knowledgeRoot, projectRoot) {
			roots = append(roots, knowledgeRoot)
		}
		return roots
	default:
		return ScanRoots(workspace)
	}
}

// MirrorRoot is the directory against which mirrored component paths are
// made relative. Every project-root source mirrors from its registered root;
// OpenSpec also normalizes a directly registered openspec/ directory.
func MirrorRoot(workspace Workspace) string {
	if workspace.Type == KindTrellis || workspace.Type == KindSuperpowers {
		return filepath.Clean(workspace.Path)
	}
	return ProjectRoot(workspace)
}

func existingDirs(paths ...string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if isDir(path) {
			out = append(out, filepath.Clean(path))
		}
	}
	return out
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
