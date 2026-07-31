package wiki

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"stele/internal/pathresolve"
)

// ExtractYAMLLinks reads .comet.yaml in changeDir and builds Edges for its
// design_doc/plan/verification_report references — the highest-confidence
// link layer, reusing the exact path resolution rule scanner.go uses.
func ExtractYAMLLinks(changeDir, root string) ([]Edge, error) {
	yamlPath := filepath.Join(changeDir, ".comet.yaml")
	data, err := os.ReadFile(yamlPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var edges []Edge
	fieldToKind := map[string]string{
		"design_doc":          "implements",
		"plan":                "implements",
		"verification_report": "references",
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		kind, ok := fieldToKind[key]
		if !ok || val == "" || val == "null" || val == "~" {
			continue
		}
		// Skip values that don't look like file paths — prose like "see tasks.md"
		// or "TBD" would create garbage edges. A valid ref is either a bare .md
		// filename (a single token, no spaces) or a path containing "/". Note a
		// plain HasSuffix(val, ".md") check is not enough: "see tasks.md" also
		// ends in ".md" as a whole string, so the space check is required to
		// reject that prose pattern while still accepting "design.md".
		looksLikePath := strings.Contains(val, "/") ||
			(!strings.Contains(val, " ") && strings.HasSuffix(val, ".md"))
		if !looksLikePath {
			continue
		}
		target := pathresolve.ResolveArtifactPath(val, root, changeDir)
		edges = append(edges, Edge{
			From:   yamlPath,
			To:     target,
			Kind:   kind,
			Source: "yaml",
		})
	}
	return edges, nil
}

// rewriteYAMLArtifactReferences updates artifact fields that resolve to
// oldPath, using the same workspace-relative convention as ExtractYAMLLinks.
func rewriteYAMLArtifactReferences(content, root, changeDir, oldPath, newPath string) (string, bool) {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	fields := map[string]bool{
		"design_doc":          true,
		"plan":                true,
		"verification_report": true,
	}

	var rewritten strings.Builder
	rewritten.Grow(len(content))
	changed := false
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimSpace(line)
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 || !fields[strings.TrimSpace(parts[0])] {
			rewritten.WriteString(line)
			continue
		}
		value := strings.TrimSpace(parts[1])
		if value == "" || value == "null" || value == "~" ||
			filepath.Clean(pathresolve.ResolveArtifactPath(value, root, changeDir)) != oldPath {
			rewritten.WriteString(line)
			continue
		}
		valueOffset := strings.Index(line, value)
		if valueOffset < 0 {
			rewritten.WriteString(line)
			continue
		}
		rewritten.WriteString(line[:valueOffset])
		rewritten.WriteString(yamlArtifactReference(value, root, changeDir, newPath))
		rewritten.WriteString(line[valueOffset+len(value):])
		changed = true
	}
	if !changed {
		return content, false
	}
	return rewritten.String(), true
}

func yamlArtifactReference(oldReference, root, changeDir, newPath string) string {
	if !strings.Contains(oldReference, "/") && filepath.Dir(newPath) == changeDir {
		return filepath.Base(newPath)
	}
	relative, err := filepath.Rel(root, newPath)
	if err != nil {
		return oldReference
	}
	return filepath.ToSlash(relative)
}

// ExtractMarkdownLinks parses [text](path) links and ![alt](path) images out
// of a component's source file and resolves relative paths against the
// file's own directory — standard markdown semantics. filepath.Join +
// filepath.Clean correctly collapse multi-level "../" (Go's stdlib does
// this right; do not hand-roll this resolution).
//
// goldmark represents links and images as distinct concrete AST types
// (*ast.Link and *ast.Image) that both embed an unexported baseLink struct
// independently — they are sibling types, not parent/child — so a single
// type assertion on *ast.Link alone would silently skip every image
// reference (e.g. diagram embeds like "![diagram](../../diagrams/x.svg)").
// Both node types promote the same Destination field from baseLink, so it
// is read the same way for either.
func ExtractMarkdownLinks(component Component) ([]Edge, error) {
	data, err := os.ReadFile(component.Path)
	if err != nil {
		return nil, err
	}

	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(data))

	var edges []Edge
	fileDir := filepath.Dir(component.Path)

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var dest []byte
		switch v := n.(type) {
		case *ast.Link:
			dest = v.Destination
		case *ast.Image:
			dest = v.Destination
		default:
			return ast.WalkContinue, nil
		}
		target, ok := resolveMarkdownDestination(fileDir, string(dest))
		if !ok {
			return ast.WalkContinue, nil
		}
		// Only create edges for wiki-tracked targets: .md files, directories
		// (may contain .comet.yaml), and embedded images. .yaml/.json config
		// files and source code are outside the knowledge graph's scope.
		ext := strings.ToLower(filepath.Ext(target))
		if ext != "" && ext != ".md" && ext != ".png" && ext != ".jpg" && ext != ".svg" && ext != ".webp" && ext != ".pdf" {
			return ast.WalkContinue, nil
		}
		edges = append(edges, Edge{
			From:   component.Path,
			To:     target,
			Kind:   "references",
			Source: "markdown-link",
		})
		return ast.WalkContinue, nil
	})

	return edges, nil
}

func resolveMarkdownDestination(fileDir, destination string) (string, bool) {
	d := destination
	if d == "" || strings.HasPrefix(d, "http://") || strings.HasPrefix(d, "https://") ||
		strings.HasPrefix(d, "#") || strings.HasPrefix(d, "mailto:") {
		return "", false
	}
	if idx := strings.IndexByte(d, '#'); idx > 0 {
		d = d[:idx]
	}
	if strings.Contains(d, "%") {
		if decoded, err := url.PathUnescape(d); err == nil {
			d = decoded
		}
	}
	if strings.HasPrefix(d, "file:///") {
		d = d[len("file://"):]
	} else if strings.HasPrefix(d, "file://") {
		d = d[len("file://"):]
	} else if strings.HasPrefix(d, "file:") {
		d = d[len("file:"):]
	}
	if filepath.IsAbs(d) {
		return filepath.Clean(d), true
	}
	return filepath.Clean(filepath.Join(fileDir, d)), true
}

// rewriteMarkdownLinkDestinations updates only link destinations whose
// resolved target is oldPath. It leaves prose and inline code untouched.
func rewriteMarkdownLinkDestinations(sourcePath, content, oldPath, newPath string) (string, bool) {
	fileDir := filepath.Dir(sourcePath)
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)

	var rewritten strings.Builder
	rewritten.Grow(len(content))
	start, cursor := 0, 0
	changed := false
	for cursor < len(content) {
		linkOffset := strings.Index(content[cursor:], "](")
		codeOffset := strings.IndexByte(content[cursor:], '`')
		if codeOffset >= 0 && (linkOffset < 0 || codeOffset < linkOffset) {
			codeStart := cursor + codeOffset
			runLength := 1
			for codeStart+runLength < len(content) && content[codeStart+runLength] == '`' {
				runLength++
			}
			closeOffset := strings.Index(content[codeStart+runLength:], strings.Repeat("`", runLength))
			if closeOffset < 0 {
				break
			}
			cursor = codeStart + runLength + closeOffset + runLength
			continue
		}
		if linkOffset < 0 {
			break
		}
		open := cursor + linkOffset
		valueStart, valueEnd, close, ok := markdownLinkDestinationBounds(content, open+2)
		if !ok {
			cursor = open + 2
			continue
		}
		target, resolves := resolveMarkdownDestination(fileDir, content[valueStart:valueEnd])
		if !resolves || target != oldPath {
			cursor = close + 1
			continue
		}
		rewritten.WriteString(content[start:valueStart])
		rewritten.WriteString(markdownDestinationForTarget(content[valueStart:valueEnd], fileDir, newPath))
		start = valueEnd
		cursor = close + 1
		changed = true
	}
	if !changed {
		return content, false
	}
	rewritten.WriteString(content[start:])
	return rewritten.String(), true
}

// markdownLinkDestinationBounds returns the destination's byte range and the
// closing parenthesis for the basic inline-link forms emitted by this project.
func markdownLinkDestinationBounds(content string, start int) (valueStart, valueEnd, close int, ok bool) {
	if start >= len(content) {
		return 0, 0, 0, false
	}
	if content[start] == '<' {
		end := strings.IndexByte(content[start+1:], '>')
		if end < 0 {
			return 0, 0, 0, false
		}
		valueStart, valueEnd = start+1, start+1+end
		close = valueEnd + 1
		for close < len(content) && (content[close] == ' ' || content[close] == '\t') {
			close++
		}
		return valueStart, valueEnd, close, close < len(content) && content[close] == ')'
	}

	depth := 0
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '\\':
			i++
		case '\n', '\r', ' ', '\t':
			return 0, 0, 0, false
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return start, i, i, true
			}
			depth--
		}
	}
	return 0, 0, 0, false
}

func markdownDestinationForTarget(raw, fileDir, newPath string) string {
	prefix, path := "", raw
	switch {
	case strings.HasPrefix(path, "file:///"):
		prefix, path = "file://", path[len("file://"):]
	case strings.HasPrefix(path, "file://"):
		prefix, path = "file://", path[len("file://"):]
	case strings.HasPrefix(path, "file:"):
		prefix, path = "file:", path[len("file:"):]
	}

	var destination string
	if prefix != "" || filepath.IsAbs(path) {
		destination = newPath
	} else {
		relative, err := filepath.Rel(fileDir, newPath)
		if err != nil {
			return raw
		}
		destination = filepath.ToSlash(relative)
		if strings.HasPrefix(path, "./") && destination != "." && !strings.HasPrefix(destination, "../") {
			destination = "./" + destination
		}
	}
	if containsPercentEscape(raw) {
		destination = (&url.URL{Path: destination}).EscapedPath()
	}
	return prefix + destination
}

func containsPercentEscape(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '%' && isHexDigit(s[i+1]) && isHexDigit(s[i+2]) {
			return true
		}
	}
	return false
}

func isHexDigit(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

var taskArtifactRe = regexp.MustCompile(`^task-(\d+)-`)

// ExtractArtifactConventionLinks links every task-NN-*.md file in
// artifactsDir back to tasksComponent, by filename convention alone (no
// content parsing needed — the number in "task-NN-" is authoritative).
func ExtractArtifactConventionLinks(tasksComponent Component, artifactsDir string) ([]Edge, error) {
	entries, err := os.ReadDir(artifactsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var edges []Edge
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if !taskArtifactRe.MatchString(e.Name()) {
			continue
		}
		edges = append(edges, Edge{
			From:   tasksComponent.Path,
			To:     filepath.Join(artifactsDir, e.Name()),
			Kind:   "generates",
			Source: "markdown-link",
		})
	}
	return edges, nil
}
