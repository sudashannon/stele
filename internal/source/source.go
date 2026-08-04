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
	// KindDocs is a plain documentation tree: a repository that holds
	// engineering markdown without any workflow layout to key off. It is never
	// auto-detected, because "contains markdown" describes almost every
	// directory and would make detection order meaningless; it must be chosen.
	KindDocs Kind = "docs"
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
	case KindOpenSpec, KindTrellis, KindSuperpowers, KindDocs:
		return value, nil
	case "":
		return "", nil
	default:
		return "", fmt.Errorf("unsupported workspace type %q: must be openspec, trellis, superpowers, or docs", value)
	}
}

// ResolveKind returns the explicit kind or detects it from the filesystem.
// Automatic detection is ownership-ordered: OpenSpec first, then Trellis,
// then standalone Superpowers. An explicitly selected Superpowers workspace
// remains strict: it must be a pure project root, never a docs subdirectory
// or a mixed workflow repository. KindDocs is accepted only when asked for and
// only when the tree actually holds indexable markdown.
func ResolveKind(path string, configured Kind) (Kind, error) {
	kind, err := ParseKind(configured)
	if err != nil {
		return "", err
	}
	if kind != "" {
		switch kind {
		case KindSuperpowers:
			if !HasSuperpowersLayout(path) {
				return "", fmt.Errorf("workspace path %q does not contain a project-root docs/superpowers layout", path)
			}
			if HasOpenSpecLayout(path) || HasTrellisLayout(path) {
				return "", fmt.Errorf("workspace path %q mixes Superpowers with OpenSpec or Trellis", path)
			}
		case KindDocs:
			if !HasDocsLayout(path) {
				return "", fmt.Errorf("workspace path %q 下未找到可索引的 markdown 文档", path)
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
		return "", fmt.Errorf("workspace path %q contains no OpenSpec, Trellis, or Superpowers data; register it as type \"docs\" to index a plain documentation tree", path)
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

// vendoredDirNames are dependency trees copied into a repository. Their
// markdown is upstream documentation, not this project's engineering record:
// measured on one model-deployment tree, allowing them pulled in the whole of
// llama.cpp's docs/ (backend guides for SYCL, CANN, OpenVINO and so on).
var vendoredDirNames = map[string]struct{}{
	"3rdparty": {}, "third_party": {}, "thirdparty": {}, "vendor": {}, "vendors": {},
	"external": {}, "extern": {}, "deps": {}, "_deps": {}, "subprojects": {},
	"submodules": {}, "site-packages": {}, "dist-packages": {},
}

// buildOutputDirNames are generated trees. They also carry fetched dependencies
// (build-x86-native/_deps/highway-src/g3doc), so excluding them removes both the
// generated and the vendored markdown underneath.
var buildOutputDirNames = map[string]struct{}{
	"build": {}, "_build": {}, "out": {}, "target": {}, "dist": {},
}

// IsExcludedDocsDir reports whether a directory name is a vendored dependency
// tree or a build output, neither of which holds this project's own documents.
func IsExcludedDocsDir(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := vendoredDirNames[lower]; ok {
		return true
	}
	if _, ok := buildOutputDirNames[lower]; ok {
		return true
	}
	// "build-x86-native", "cmake-build-release": the same generated trees under
	// a toolchain-specific name.
	return strings.HasPrefix(lower, "build-") || strings.HasPrefix(lower, "cmake-build-")
}

// sourceFileExts and sourceFileNames identify a directory that holds code.
var sourceFileExts = map[string]struct{}{
	".c": {}, ".cc": {}, ".cpp": {}, ".cxx": {}, ".h": {}, ".hpp": {}, ".hxx": {},
	".cu": {}, ".cuh": {}, ".py": {}, ".go": {}, ".rs": {}, ".ts": {}, ".tsx": {},
	".js": {}, ".jsx": {}, ".java": {}, ".kt": {}, ".swift": {}, ".m": {}, ".mm": {},
	".sh": {}, ".cmake": {}, ".proto": {}, ".gradle": {},
}
var sourceFileNames = map[string]struct{}{
	"cmakelists.txt": {}, "makefile": {}, "setup.py": {}, "pyproject.toml": {},
	"cargo.toml": {}, "package.json": {}, "go.mod": {},
}

// DirHoldsSourceCode reports whether any entry is a source file, which is what
// separates module documentation from a documentation directory.
//
// This is the load-bearing rule for a plain docs tree, and it is a measurement
// rather than a name list: markdown sitting beside .cpp/.py files documents that
// code unit, while a directory of only markdown is a documentation directory.
// Measured on one model-deployment tree, this rule alone cut the candidate set
// from 432 files to 233, and with the vendored and build exclusions to 79.
func DirHoldsSourceCode(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if _, ok := sourceFileNames[name]; ok {
			return true
		}
		if _, ok := sourceFileExts[filepath.Ext(name)]; ok {
			return true
		}
	}
	return false
}

// IsModuleReadme reports whether a path is a README nested below the tree's own
// top level. A project's own README is an overview worth indexing; one several
// directories down describes a code module, and there were twelve of those
// against fourteen real documents in the measured tree.
func IsModuleReadme(root, path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if !strings.HasPrefix(name, "readme") {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return strings.Count(rel, string(filepath.Separator)) > 1
}

// HasDocsLayout accepts any tree that holds at least one markdown file the docs
// scanner would keep. Walking stops at the first hit, so registering a large
// repository does not pay for a full scan twice.
func HasDocsLayout(path string) bool {
	root := filepath.Clean(path)
	if !isDir(root) {
		return false
	}
	found := false
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || IsExcludedDocsDir(name)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		if IsModuleReadme(root, p) {
			return nil
		}
		entries, readErr := os.ReadDir(filepath.Dir(p))
		if readErr == nil && DirHoldsSourceCode(entries) {
			return nil
		}
		found = true
		return filepath.SkipAll
	})
	return found
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
	if workspace.Type == KindTrellis || workspace.Type == KindSuperpowers || workspace.Type == KindDocs {
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
	case KindDocs:
		// The whole registered tree is the content root; the scanner applies the
		// vendored, build-output and source-adjacency exclusions per directory.
		return existingDirs(workspace.Path)
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
	case KindDocs:
		// Watch the registered tree itself: a docs workspace has no fixed
		// content subdirectory, so new documents can appear anywhere in it.
		return existingDirs(workspace.Path)
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
	if workspace.Type == KindTrellis || workspace.Type == KindSuperpowers || workspace.Type == KindDocs {
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
