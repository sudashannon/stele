package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"comet-ui/internal/source"
	"gopkg.in/yaml.v3"
)

type incrementalChange struct {
	path      string
	component *Component
	wsPath    string
}

// IncrementalUpdate processes a batch of changed file paths and updates the
// graph in-place without a full workspace scan.
func (a *API) IncrementalUpdate(changedFiles []string) error {
	if a.sourceRequiresFullRebuild(changedFiles) {
		return a.Rebuild()
	}
	changes, err := a.classifyChanges(changedFiles)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}

	a.mu.RLock()
	embeddings := make(map[string][]float32, len(a.graph.Embeddings()))
	for id, vector := range a.graph.Embeddings() {
		embeddings[id] = vector
	}
	a.mu.RUnlock()

	changedComponents := make([]Component, 0, len(changes))
	for _, change := range changes {
		if change.component == nil {
			delete(embeddings, change.path)
			continue
		}
		changedComponents = append(changedComponents, *change.component)
	}

	changedEmbeddingEntries, err := ComputeEmbeddingEntries(changedComponents, findEmbedScript())
	if err != nil {
		return fmt.Errorf("incremental embedding: %w", err)
	}
	for id, entry := range changedEmbeddingEntries {
		embeddings[id] = entry.Vector
	}

	vectorEdges := ComputeVectorSimilarityEdges(embeddings, 3, 0.5)
	vectorEdgesBySource := groupEdgesBySource(vectorEdges)

	changedEdges := make(map[string][]Edge, len(changedComponents))
	conventionSources := make(map[string]struct{})
	conventionEdges := make(map[string][]Edge)
	processedChangeDirs := make(map[string]struct{})
	for _, change := range changes {
		if change.component == nil {
			continue
		}
		edges, err := extractIncrementalLinks(*change.component, change.wsPath)
		if err != nil {
			return fmt.Errorf("extract links for %s: %w", change.path, err)
		}
		changedEdges[change.path] = edges

		if changeDir := incrementalChangeDir(*change.component); changeDir != "" {
			if _, processed := processedChangeDirs[changeDir]; processed {
				continue
			}
			processedChangeDirs[changeDir] = struct{}{}
			for _, name := range []string{"proposal.md", "design.md", "tasks.md"} {
				conventionSources[filepath.Join(changeDir, name)] = struct{}{}
			}
			for _, edge := range ExtractChangeInternalLinks(changeDir) {
				conventionEdges[edge.From] = append(conventionEdges[edge.From], edge)
			}
		}
	}

	dirty := 0
	a.mu.Lock()
	for _, change := range changes {
		if change.component == nil {
			if _, exists := a.graph.Component(change.path); !exists {
				continue
			}
			incident := edgeSet(a.graph.Forward(change.path))
			for key := range edgeSet(a.graph.Backlinks(change.path)) {
				incident[key] = struct{}{}
			}
			dirty += 1 + len(incident)
			a.graph.RemoveComponent(change.path)
			continue
		}

		if _, existed := a.graph.Component(change.path); !existed {
			dirty++
		}
		a.graph.AddComponent(*change.component)
		if entry, ok := changedEmbeddingEntries[change.path]; ok {
			a.graph.UpdateEmbeddingEntry(entry)
		} else {
			a.graph.RemoveEmbedding(change.path)
		}
	}

	plannedEdges := make(map[string][]Edge, len(a.graph.Components()))
	for source, edges := range changedEdges {
		plannedEdges[source] = append([]Edge(nil), edges...)
	}
	for source := range conventionSources {
		if _, exists := a.graph.Component(source); !exists {
			continue
		}
		base, planned := plannedEdges[source]
		if !planned {
			base = append([]Edge(nil), a.graph.Forward(source)...)
		}
		base = withoutEdgeSource(base, "convention-internal")
		plannedEdges[source] = append(base, conventionEdges[source]...)
	}
	for source := range a.graph.Components() {
		base, planned := plannedEdges[source]
		if !planned {
			base = append([]Edge(nil), a.graph.Forward(source)...)
		}
		base = withoutEdgeSource(base, "vector")
		plannedEdges[source] = append(base, vectorEdgesBySource[source]...)
	}
	for source, edges := range plannedEdges {
		oldEdges := append([]Edge(nil), a.graph.Forward(source)...)
		edgeChanges := changedEdgeCount(oldEdges, edges)
		if edgeChanges == 0 {
			continue
		}
		dirty += edgeChanges
		a.graph.RemoveEdgesFrom(source)
		a.graph.AddEdges(edges)
	}

	cacheSnapshot := make(map[string]EmbeddingEntry, len(a.graph.EmbeddingEntries()))
	for id, entry := range a.graph.EmbeddingEntries() {
		entry.Vector = append([]float32(nil), entry.Vector...)
		cacheSnapshot[id] = entry
	}
	a.AddDirty(dirty)
	a.mu.Unlock()

	if a.indexCacheDir != "" {
		cachePath := filepath.Join(a.indexCacheDir, "embeddings.bin")
		if err := SaveEmbeddingCache(cachePath, cacheSnapshot); err != nil {
			return fmt.Errorf("save embeddings: %w", err)
		}
	}
	return nil
}

func (a *API) classifyChanges(changedFiles []string) ([]incrementalChange, error) {
	seen := make(map[string]struct{}, len(changedFiles))
	changes := make([]incrementalChange, 0, len(changedFiles))
	for _, changedPath := range changedFiles {
		path, err := filepath.Abs(changedPath)
		if err != nil {
			return nil, fmt.Errorf("resolve changed path %s: %w", changedPath, err)
		}
		path = filepath.Clean(path)
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				changes = append(changes, incrementalChange{path: path})
				continue
			}
			return nil, fmt.Errorf("stat changed path %s: %w", path, err)
		}
		if info.IsDir() || !isWikiFile(path) {
			continue
		}

		alias, wsPath := a.resolveWorkspace(path)
		if alias == "" {
			return nil, fmt.Errorf("changed path is outside configured workspaces: %s", path)
		}
		component, err := componentFromPath(path, alias, info)
		if err != nil {
			return nil, err
		}
		changes = append(changes, incrementalChange{path: path, component: component, wsPath: wsPath})
	}
	return changes, nil
}

func componentFromPath(path, workspace string, info os.FileInfo) (*Component, error) {
	if filepath.Base(path) == ".comet.yaml" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		frontmatter := map[string]any{}
		if err := yaml.Unmarshal(data, &frontmatter); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		frontmatter["_source"] = "openspec"
		return &Component{
			ID:          path,
			Type:        TypeChange,
			Title:       filepath.Base(filepath.Dir(path)),
			Path:        path,
			Workspace:   workspace,
			Frontmatter: frontmatter,
			UpdatedAt:   info.ModTime(),
		}, nil
	}

	typ := classifyPath(path)
	frontmatter, title, err := parseFrontmatterAndTitle(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if typ == "" {
		typ = classifyByFrontmatter(frontmatter)
		if typ == "" {
			return nil, nil
		}
	}
	frontmatter["_source"] = "openspec"
	return &Component{
		ID:          path,
		Type:        typ,
		Title:       title,
		Path:        path,
		Workspace:   workspace,
		Frontmatter: frontmatter,
		UpdatedAt:   info.ModTime(),
	}, nil
}

func (a *API) sourceRequiresFullRebuild(paths []string) bool {
	for _, path := range paths {
		workspace, ok := a.resolveWorkspaceConfig(path)
		if !ok {
			continue
		}
		kind, err := source.ResolveKind(workspace.Path, workspace.Type)
		if err == nil && kind != source.KindOpenSpec {
			return true
		}
	}
	return false
}

// resolveWorkspace returns the live workspace whose scan scope most
// specifically contains path. The returned path is the configured workspace
// path, not the expanded project scan scope.
func (a *API) resolveWorkspace(path string) (alias string, wsPath string) {
	workspace, ok := a.resolveWorkspaceConfig(path)
	if !ok {
		return "", ""
	}
	return workspace.Alias, workspace.Path
}

func (a *API) resolveWorkspaceConfig(path string) (WorkspaceConfig, bool) {
	a.mu.RLock()
	workspaces := a.ws
	lister := a.lister
	a.mu.RUnlock()
	if lister != nil {
		workspaces = lister.List()
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return WorkspaceConfig{}, false
	}
	bestLength := -1
	var best WorkspaceConfig
	for _, workspace := range workspaces {
		configuredPath, err := filepath.Abs(workspace.Path)
		if err != nil {
			continue
		}
		configuredPath = filepath.Clean(configuredPath)
		scopes := []string{configuredPath}
		if dirExists(filepath.Join(configuredPath, "changes")) || filepath.Base(configuredPath) == "openspec" {
			scopes = append(scopes, filepath.Dir(configuredPath))
		}
		for _, scope := range scopes {
			if !pathWithin(absolutePath, scope) || len(scope) <= bestLength {
				continue
			}
			bestLength = len(scope)
			best = workspace
			best.Path = configuredPath
		}
	}
	return best, bestLength >= 0
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func extractIncrementalLinks(component Component, wsPath string) ([]Edge, error) {
	if component.Type == TypeChange {
		return ExtractYAMLLinks(filepath.Dir(component.Path), projectRootForWorkspace(wsPath))
	}

	edges, err := ExtractMarkdownLinks(component)
	if err != nil {
		return nil, err
	}
	if component.Type == TypeTasks {
		projectRoot := projectRootForWorkspace(wsPath)
		yamlEdges, _ := ExtractYAMLLinks(filepath.Dir(component.Path), projectRoot)
		for _, edge := range yamlEdges {
			if !strings.Contains(edge.To, string(filepath.Separator)+"plans"+string(filepath.Separator)) {
				continue
			}
			slug := strings.TrimSuffix(filepath.Base(edge.To), ".md")
			artifactsDir := filepath.Join(projectRoot, "docs", "superpowers", "artifacts", slug)
			artifactEdges, extractErr := ExtractArtifactConventionLinks(component, artifactsDir)
			if extractErr != nil {
				return nil, extractErr
			}
			edges = append(edges, artifactEdges...)
		}
	}
	return edges, nil
}

func incrementalChangeDir(component Component) string {
	var candidate string
	switch component.Type {
	case TypeChange, TypeProposal, TypeDesign, TypeTasks:
		candidate = filepath.Dir(component.Path)
	case TypeSpec:
		dir := filepath.Dir(component.Path)
		for {
			if filepath.Base(dir) == "specs" {
				candidate = filepath.Dir(dir)
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return ""
			}
			dir = parent
		}
	default:
		return ""
	}
	if hasPathComponent(candidate, "changes") {
		return candidate
	}
	return ""
}

func hasPathComponent(path, component string) bool {
	for {
		if filepath.Base(path) == component {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func groupEdgesBySource(edges []Edge) map[string][]Edge {
	grouped := make(map[string][]Edge)
	for _, edge := range edges {
		grouped[edge.From] = append(grouped[edge.From], edge)
	}
	return grouped
}

func withoutEdgeSource(edges []Edge, source string) []Edge {
	filtered := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		if edge.Source != source {
			filtered = append(filtered, edge)
		}
	}
	return filtered
}

func projectRootForWorkspace(wsPath string) string {
	if dirExists(filepath.Join(wsPath, "changes")) || filepath.Base(wsPath) == "openspec" {
		return filepath.Dir(wsPath)
	}
	return wsPath
}

func changedEdgeCount(before, after []Edge) int {
	beforeSet := edgeSet(before)
	afterSet := edgeSet(after)
	changed := 0
	for key := range beforeSet {
		if _, ok := afterSet[key]; !ok {
			changed++
		}
	}
	for key := range afterSet {
		if _, ok := beforeSet[key]; !ok {
			changed++
		}
	}
	return changed
}

func edgeSet(edges []Edge) map[Edge]struct{} {
	set := make(map[Edge]struct{}, len(edges))
	for _, edge := range edges {
		set[edge] = struct{}{}
	}
	return set
}
