package wiki

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"

	"stele/internal/source"

	"gopkg.in/yaml.v3"
)

// ScanComponents walks workspaceRoot for markdown files and builds a
// Component for each one. Classification by ComponentType happens by
// filename convention (proposal.md, design.md, tasks.md) or by directory
// convention (docs/superpowers/specs/, docs/superpowers/plans/,
// docs/superpowers/artifacts/, diagrams/) — anything else is skipped.
//
// A single malformed file must not abort the whole scan (design doc error
// table: "遇到格式错误的 markdown → 跳过+记录日志，不中断整体索引"). Only a
// directory-read failure from filepath.Walk itself propagates as a real
// error; a per-file parse failure is logged and skipped. Permission-denied
// paths (e.g. a restricted file/dir inside a vendored rootfs tree) extend
// this same principle from content-errors to access-errors: they are
// logged and skipped (SkipDir for a directory, skip-this-entry for a
// file) rather than aborting the entire walk.
//
// Certain directories are never descended into, regardless of contents:
// .git, node_modules, any dotdir (name starting with "."), and rootfs
// (a vendored OS root filesystem tree, as found on embedded/Tegra-style
// workspaces). These are skipped for both correctness (avoid indexing
// vendored/unrelated markdown that happens to match a classifyPath
// substring) and performance (avoid traversing huge unrelated trees).
func ScanComponents(workspaceRoot, workspaceAlias string) ([]Component, error) {
	return scanComponents(workspaceRoot, workspaceAlias, false)
}

// ScanDocsComponents scans a plain documentation tree, where there is no
// workflow layout to classify by. Two rules differ from ScanComponents:
//
// Unclassified markdown is indexed as knowledge instead of requiring a
// `wiki: true` opt-in. That opt-in exists so a workflow workspace does not
// absorb unrelated markdown; a docs workspace is the markdown, so demanding it
// would index nothing (measured on one tree: 48 of 480 files classified, 0
// opted in).
//
// Vendored trees, build outputs, markdown sitting beside source code, and
// nested module READMEs are excluded. Without them the same tree offered 432
// extra files, most of it upstream documentation and per-module READMEs; with
// them it offers 79, which is the project's own engineering record.
func ScanDocsComponents(workspaceRoot, workspaceAlias string) ([]Component, error) {
	return scanComponents(workspaceRoot, workspaceAlias, true)
}

func scanComponents(workspaceRoot, workspaceAlias string, docsMode bool) ([]Component, error) {
	var components []Component
	// One ReadDir per directory, not per markdown file: a docs tree can hold
	// hundreds of files under a handful of directories.
	dirHasSource := make(map[string]bool)

	err := filepath.Walk(workspaceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				log.Printf("wiki scan: permission denied, skipping %s: %v", path, err)
				if info != nil && info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return err // other traversal errors remain genuinely fatal
		}
		if info.IsDir() {
			name := filepath.Base(path)
			switch {
			case name == ".git" || name == "node_modules" || name == "rootfs":
				return filepath.SkipDir
			case strings.HasPrefix(name, ".") && name != ".":
				return filepath.SkipDir
			// Skip large SDK/BSP/vendor trees with no wiki-relevant content.
			case name == "orin_bsp" || name == "qcom_bsp":
				return filepath.SkipDir
			case name == "argos-sdk" || name == "x5_sdk":
				return filepath.SkipDir
			case name == "mondo-ai":
				return filepath.SkipDir
			case (strings.Contains(name, "_sdk") || strings.Contains(name, "_bsp")) && path != workspaceRoot:
				return filepath.SkipDir
			case docsMode && path != workspaceRoot && source.IsExcludedDocsDir(name):
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			log.Printf("wiki scan: skipping non-regular file %s", path)
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		if docsMode {
			// A README several levels down documents a code module, not the
			// project; and markdown beside source files documents that source.
			if source.IsModuleReadme(workspaceRoot, path) {
				return nil
			}
			dir := filepath.Dir(path)
			hasSource, known := dirHasSource[dir]
			if !known {
				entries, readErr := os.ReadDir(dir)
				hasSource = readErr == nil && source.DirHoldsSourceCode(entries)
				dirHasSource[dir] = hasSource
			}
			if hasSource {
				return nil
			}
		}
		typ := classifyPath(path)
		// For files not in a known directory, check frontmatter for wiki:true opt-in
		var fm map[string]any
		var title string
		var parseErr error
		if typ == "" {
			fm, title, parseErr = parseFrontmatterAndTitle(path)
			if parseErr != nil {
				return nil // can't parse, skip silently
			}
			typ = classifyByFrontmatter(fm)
			if typ == "" {
				if !docsMode {
					return nil // not classified and no opt-in
				}
				// A docs workspace IS its markdown; the opt-in would index nothing.
				typ = TypeKnowledge
			}
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			log.Printf("wiki scan: skipping %s, could not resolve absolute path: %v", path, err)
			return nil
		}
		// Parse frontmatter if not already done (classified by path, not yet parsed)
		if fm == nil {
			fm, title, parseErr = parseFrontmatterAndTitle(path)
			if parseErr != nil {
				log.Printf("wiki scan: skipping %s, parse error: %v", path, parseErr)
				return nil
			}
		}
		components = append(components, Component{
			ID:          absPath,
			Type:        typ,
			Title:       title,
			Path:        absPath,
			Workspace:   workspaceAlias,
			Frontmatter: fm,
			UpdatedAt:   info.ModTime(),
		})
		return nil
	})
	return components, err
}

func classifyPath(path string) ComponentType {
	base := filepath.Base(path)
	switch base {
	case "proposal.md", "prd.md":
		return TypeProposal
	case "design.md":
		return TypeDesign
	case "tasks.md", "implement.md":
		return TypeTasks
	}
	sep := string(filepath.Separator)
	switch {
	case strings.Contains(path, sep+"specs"+sep),
		strings.Contains(path, sep+".trellis"+sep+"spec"+sep):
		return TypeSpec
	case strings.Contains(path, sep+"plans"+sep):
		return TypePlan
	case strings.Contains(path, sep+"research"+sep):
		return TypeKnowledge
	case strings.Contains(path, sep+"artifacts"+sep),
		strings.Contains(path, sep+".trellis"+sep+"tasks"+sep):
		return TypeArtifact
	case strings.Contains(path, sep+"diagrams"+sep):
		return TypeDiagram
	case strings.Contains(path, sep+"reports"+sep):
		return TypeReport
	case strings.Contains(path, sep+"knowledge"+sep),
		strings.Contains(path, sep+".trellis"+sep+"workspace"+sep):
		return TypeKnowledge
	case strings.Contains(path, sep+"design_docs"+sep):
		return TypeKnowledge
	case containsDocsSuffix(path, sep):
		return TypeKnowledge
	}
	return ""
}

// containsDocsSuffix returns true if path contains a directory component ending
// in "_docs" (e.g. nv_docs/, qcom_docs/, x5_docs/). These are platform/vendor
// documentation directories that should be indexed as knowledge.
func containsDocsSuffix(path, sep string) bool {
	parts := strings.Split(path, sep)
	for _, p := range parts {
		if strings.HasSuffix(p, "_docs") && len(p) > 5 {
			return true
		}
	}
	return false
}

// classifyByFrontmatter checks if a file's frontmatter opts into wiki
// tracking via `wiki: true`. Called as a fallback when classifyPath returns "".
func classifyByFrontmatter(fm map[string]any) ComponentType {
	if fm == nil {
		return ""
	}
	if v, ok := fm["wiki"]; ok {
		if b, ok := v.(bool); ok && b {
			return TypeKnowledge
		}
	}
	return ""
}

// parseFrontmatterAndTitle reads a leading "---\n...\n---\n" YAML block (if
// present) and the first "# heading" line. A non-empty frontmatter `title`
// outranks the heading — the author's declared document title wins over what
// is often a section heading (e.g. "# 结论"). Falls back to the filename
// (without extension) when neither is found.
func parseFrontmatterAndTitle(path string) (map[string]any, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	fm := map[string]any{}
	title := ""

	firstLine := true
	inFrontmatter := false
	var fmLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if firstLine && strings.TrimSpace(line) == "---" {
			inFrontmatter = true
			firstLine = false
			continue
		}
		firstLine = false
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				if err := yaml.Unmarshal([]byte(strings.Join(fmLines, "\n")), &fm); err != nil {
					return nil, "", err
				}
				if t, ok := fm["title"].(string); ok && strings.TrimSpace(t) != "" {
					title = strings.TrimSpace(t)
				}
				continue
			}
			fmLines = append(fmLines, line)
			continue
		}
		if title == "" && strings.HasPrefix(strings.TrimSpace(line), "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	// If title is a generic word (spec/proposal/tasks/design/plan), enrich it
	// from the parent directory name which typically carries the change/spec name.
	// e.g. "specs/rx101-snn-spinal-residual-control/spec.md" → "spec: rx101-snn-spinal-residual-control"
	if isGenericTitle(title) {
		parentName := filepath.Base(filepath.Dir(path))
		if parentName != "." && parentName != "/" && !isGenericTitle(parentName) {
			title = title + ": " + parentName
		}
	}
	return fm, title, nil
}

func isGenericTitle(t string) bool {
	switch strings.ToLower(t) {
	case "spec", "proposal", "prd", "design", "tasks", "implement", "plan", "report", "readme", "index":
		return true
	}
	return false
}
