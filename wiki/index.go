package wiki

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"stele/internal/source"
)

// WorkspaceConfig is shared with the HTTP workspace registry through the
// source-neutral internal/source package.
type WorkspaceConfig = source.Workspace

// BuildIndex scans every registered workspace through its source adapter,
// extracts metadata, Markdown, convention, and vector links, and returns a
// queryable Graph. Individual file errors are skipped so one malformed
// document does not abort the aggregate index.
//
// OpenSpec accepts either an openspec/ directory or its project root and
// also indexes durable sibling docs. Trellis is registered at the project
// root and indexes .trellis/tasks, .trellis/spec, and .trellis/workspace.
// Standalone Superpowers indexes only docs/superpowers/{specs,plans,artifacts,reports}.
// Transient .trellis/.runtime state and paths outside source roots are never indexed.
//
// After building, the graph is also persisted to indexCacheDir as
// index.json + graph.json (design doc: "索引存储：.wiki/index.json +
// .wiki/graph.json"). These files are a debugging/inspection artifact —
// BuildIndex always rebuilds from source on every call; nothing reads
// these files back in this plan. indexCacheDir="" skips persistence
// (used by tests that don't care about the on-disk artifact).
func BuildIndex(workspaces []WorkspaceConfig, indexCacheDir string) (*Graph, error) {
	var allComponents []Component
	var allEdges []Edge
	var failedWorkspaces []string

	for _, workspace := range workspaces {
		adapter, resolved, err := indexSourceFor(workspace)
		if err != nil {
			log.Printf("wiki index: workspace %q source detection failed: %v", workspace.Alias, err)
			failedWorkspaces = append(failedWorkspaces, workspace.Alias)
			continue
		}
		components, edges, scanErr := adapter.Scan(resolved)
		if scanErr != nil {
			log.Printf("wiki index: workspace %q scan failed: %v", workspace.Alias, scanErr)
			failedWorkspaces = append(failedWorkspaces, workspace.Alias)
			continue
		}
		allComponents = append(allComponents, components...)
		allEdges = append(allEdges, edges...)

		for _, component := range components {
			if !strings.EqualFold(filepath.Ext(component.Path), ".md") {
				continue
			}
			mdEdges, err := ExtractMarkdownLinks(component)
			if err != nil {
				continue
			}
			allEdges = append(allEdges, mdEdges...)
		}
	}

	// Vector similarity edges — the durable cache is valid only when both the
	// source-content hash and semantic-input version match.
	scriptPath := findEmbedScript()
	cachePath := ""
	cachedEntries := map[string]EmbeddingEntry{}
	if indexCacheDir != "" {
		cachePath = filepath.Join(indexCacheDir, "embeddings.bin")
		if cached, err := LoadEmbeddingCache(cachePath); err == nil {
			cachedEntries = cached
		}
	}
	embeddingEntries := make(map[string]EmbeddingEntry, len(allComponents))
	missing := make([]Component, 0)
	for _, component := range allComponents {
		entry, ok := cachedEntries[component.ID]
		if ok && EmbeddingEntryMatches(component, entry) {
			embeddingEntries[component.ID] = entry
			continue
		}
		missing = append(missing, component)
	}
	if len(missing) > 0 {
		log.Printf("wiki index: embedding %d new/changed components (cache has %d valid)", len(missing), len(embeddingEntries))
		newEntries, err := ComputeEmbeddingEntries(missing, scriptPath)
		if err != nil {
			log.Printf("wiki: embedding computation failed (non-fatal): %v", err)
		} else {
			for id, entry := range newEntries {
				embeddingEntries[id] = entry
			}
		}
	}
	embeddings := EmbeddingVectors(embeddingEntries)
	var simEdges []Edge
	if len(embeddings) > 0 {
		simEdges = ComputeVectorSimilarityEdges(embeddings, 3, 0.5)
	}
	if cachePath != "" {
		if err := SaveEmbeddingCache(cachePath, embeddingEntries); err != nil {
			log.Printf("wiki index: failed to cache embeddings: %v", err)
		}
	}
	allEdges = append(allEdges, simEdges...)

	taxonomy := LoadTaxonomy()
	EnrichComponentTags(allComponents, allEdges, taxonomy)
	allEdges = append(allEdges, ComputeTagEdges(allComponents, taxonomy)...)
	g := BuildGraph(allComponents, allEdges)
	g.SetEmbeddingEntries(embeddingEntries)
	g.SetCommunities(DetectCommunities(g))
	g.SetCommunityLabels(CommunityLabels(allComponents, g.Communities()))
	g.SetFailedWorkspaces(failedWorkspaces)
	if indexCacheDir != "" {
		persistIndexCache(indexCacheDir, allComponents, allEdges) // best-effort, errors logged not returned
	}
	return g, nil
}

// findEmbedScript locates scripts/embed.ts. It tries, in order: relative to
// this source file (works under `go test`, where CWD is the package dir and
// os.Args[0] is a throwaway test binary in a temp dir), relative to the
// running executable (production: the binary ships next to scripts/), and
// finally relative to the current working directory (dev `go run` from repo
// root). Returns the first candidate that exists on disk, or the CWD-relative
// path as a last resort so callers get a descriptive "not found" error.
func findEmbedScript() string {
	if _, thisFile, _, ok := runtime.Caller(0); ok {
		// thisFile is .../wiki/index.go; repo root is one level up.
		candidate := filepath.Join(filepath.Dir(thisFile), "..", "scripts", "embed.ts")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	candidate := filepath.Join(filepath.Dir(os.Args[0]), "scripts", "embed.ts")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return "scripts/embed.ts"
}

// EmbeddingScriptPath exposes the index's single script-resolution convention
// to report reducers that embed structured weekly cluster summaries.
func EmbeddingScriptPath() string {
	return findEmbedScript()
}

// collectChangeDirs lists all change directories: direct children of
// changesDir (excluding "archive" itself) plus one-level children of
// changesDir/archive/. This ensures archived changes get YAML edge
// extraction too.
func collectChangeDirs(changesDir string) []string {
	var dirs []string
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "archive" {
			archiveDir := filepath.Join(changesDir, "archive")
			archiveEntries, err := os.ReadDir(archiveDir)
			if err == nil {
				for _, ae := range archiveEntries {
					if ae.IsDir() {
						dirs = append(dirs, filepath.Join(archiveDir, ae.Name()))
					}
				}
			}
			continue
		}
		dirs = append(dirs, filepath.Join(changesDir, e.Name()))
	}
	return dirs
}

func persistIndexCache(dir string, components []Component, edges []Edge) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("wiki: could not create index cache dir %s: %v", dir, err)
		return
	}
	cacheComponents := make([]Component, len(components))
	for i, component := range components {
		cacheComponents[i] = stripSyntheticTags(component)
	}
	indexPath := filepath.Join(dir, "index.json")
	if data, err := json.MarshalIndent(cacheComponents, "", "  "); err != nil {
		log.Printf("wiki: could not marshal index cache %s: %v", indexPath, err)
	} else if err := os.WriteFile(indexPath, data, 0644); err != nil {
		log.Printf("wiki: could not write index cache %s: %v", indexPath, err)
	}
	graphPath := filepath.Join(dir, "graph.json")
	if data, err := json.MarshalIndent(edges, "", "  "); err != nil {
		log.Printf("wiki: could not marshal graph cache %s: %v", graphPath, err)
	} else if err := os.WriteFile(graphPath, data, 0644); err != nil {
		log.Printf("wiki: could not write graph cache %s: %v", graphPath, err)
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// FindDocsDirs returns directories under parent that should be scanned for
// wiki content: "design_docs" and any directory ending in "_docs" (e.g.
// nv_docs, qcom_docs, x5_docs). These are sibling to openspec/docs/knowledge
// but were previously not included in scan roots.
func FindDocsDirs(parent string) []string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "design_docs" || (strings.HasSuffix(name, "_docs") && len(name) > 5) {
			dirs = append(dirs, filepath.Join(parent, name))
		}
	}
	return dirs
}
