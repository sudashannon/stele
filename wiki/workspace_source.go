package wiki

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"comet-ui/internal/source"
	sp "comet-ui/internal/superpowers"
	"comet-ui/internal/trellis"

	"gopkg.in/yaml.v3"
)

type workspaceIndexSource interface {
	Scan(workspace WorkspaceConfig) ([]Component, []Edge, error)
}

type openSpecIndexSource struct{}
type trellisIndexSource struct{}
type superpowersIndexSource struct{}

func indexSourceFor(workspace WorkspaceConfig) (workspaceIndexSource, WorkspaceConfig, error) {
	kind, err := source.ResolveKind(workspace.Path, workspace.Type)
	if err != nil {
		return nil, workspace, err
	}
	workspace.Type = kind
	switch kind {
	case source.KindTrellis:
		return trellisIndexSource{}, workspace, nil
	case source.KindSuperpowers:
		return superpowersIndexSource{}, workspace, nil
	default:
		return openSpecIndexSource{}, workspace, nil
	}
}

func (openSpecIndexSource) Scan(workspace WorkspaceConfig) ([]Component, []Edge, error) {
	openspecDir := source.OpenSpecPath(workspace.Path)
	projectRoot := source.ProjectRoot(workspace)
	roots := source.ScanRoots(workspace)

	var components []Component
	for _, root := range roots {
		cs, err := ScanComponents(root, workspace.Alias)
		if err != nil {
			log.Printf("wiki index: workspace %q scan %s had errors: %v", workspace.Alias, root, err)
		}
		for i := range cs {
			stampComponentSource(&cs[i], source.KindOpenSpec)
		}
		components = append(components, cs...)
	}

	changesDir := filepath.Join(openspecDir, "changes")
	changeDirs := collectChangeDirs(changesDir)
	for _, dir := range changeDirs {
		yamlPath := filepath.Join(dir, ".comet.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		fm := map[string]any{}
		if err := yaml.Unmarshal(data, &fm); err != nil {
			log.Printf("wiki index: %s: failed to parse .comet.yaml frontmatter: %v", yamlPath, err)
			fm = map[string]any{}
		}
		fm["_source"] = string(source.KindOpenSpec)
		changeID := yamlPath
		title := filepath.Base(dir)
		if v, ok := fm["title"].(string); ok && v != "" {
			title = v
		}
		component := Component{
			ID:          changeID,
			Type:        TypeChange,
			Title:       title,
			Path:        changeID,
			Workspace:   workspace.Alias,
			Frontmatter: fm,
		}
		if info, statErr := os.Stat(yamlPath); statErr == nil {
			component.UpdatedAt = info.ModTime()
		}
		components = append(components, component)
	}

	var edges []Edge
	for _, dir := range changeDirs {
		yamlEdges, err := ExtractYAMLLinks(dir, projectRoot)
		if err == nil {
			edges = append(edges, yamlEdges...)
		}
		internalEdges := ExtractChangeInternalLinks(dir)
		edges = append(edges, internalEdges...)
		tasksPath := filepath.Join(dir, "tasks.md")
		if _, statErr := os.Stat(tasksPath); statErr == nil {
			tasksComponent := Component{ID: tasksPath, Path: tasksPath, Type: TypeTasks, Workspace: workspace.Alias}
			for _, edge := range yamlEdges {
				if !strings.Contains(edge.To, "plans") {
					continue
				}
				slug := strings.TrimSuffix(filepath.Base(edge.To), ".md")
				artifactsDir := filepath.Join(projectRoot, "docs", "superpowers", "artifacts", slug)
				artifactEdges, _ := ExtractArtifactConventionLinks(tasksComponent, artifactsDir)
				edges = append(edges, artifactEdges...)
			}
		}
	}
	return components, edges, nil
}

func (superpowersIndexSource) Scan(workspace WorkspaceConfig) ([]Component, []Edge, error) {
	records, err := sp.Scan(workspace.Path)
	if err != nil {
		return nil, nil, err
	}

	var components []Component
	for _, root := range source.ScanRoots(workspace) {
		scanned, scanErr := ScanComponents(root, workspace.Alias)
		if scanErr != nil {
			log.Printf("wiki index: Superpowers workspace %q scan %s had errors: %v", workspace.Alias, root, scanErr)
		}
		for i := range scanned {
			stampComponentSource(&scanned[i], source.KindSuperpowers)
		}
		components = append(components, scanned...)
	}

	componentIDs := make(map[string]string, len(components))
	for _, component := range components {
		componentIDs[filepath.Clean(component.Path)] = component.ID
	}
	var edges []Edge
	for _, record := range records {
		for _, plan := range record.Plans {
			planID := componentIDs[filepath.Clean(plan.Path)]
			if planID == "" {
				continue
			}
			for _, spec := range record.Specs {
				if specID := componentIDs[filepath.Clean(spec.Path)]; specID != "" {
					edges = append(edges, Edge{From: planID, To: specID, Kind: "implements", Source: "superpowers-convention"})
				}
			}
			for _, artifact := range record.Artifacts {
				if artifactID := componentIDs[filepath.Clean(artifact.Path)]; artifactID != "" {
					edges = append(edges, Edge{From: planID, To: artifactID, Kind: "generates", Source: "superpowers-convention"})
				}
			}
		}
		for _, report := range record.Reports {
			reportID := componentIDs[filepath.Clean(report.Path)]
			if reportID == "" {
				continue
			}
			for _, plan := range record.Plans {
				if planID := componentIDs[filepath.Clean(plan.Path)]; planID != "" {
					edges = append(edges, Edge{From: reportID, To: planID, Kind: "traces-back", Source: "superpowers-convention"})
				}
			}
			for _, artifact := range record.Artifacts {
				if artifactID := componentIDs[filepath.Clean(artifact.Path)]; artifactID != "" {
					edges = append(edges, Edge{From: reportID, To: artifactID, Kind: "traces-back", Source: "superpowers-convention"})
				}
			}
		}
	}
	return components, deduplicateEdges(edges), nil
}

func (trellisIndexSource) Scan(workspace WorkspaceConfig) ([]Component, []Edge, error) {
	records, err := trellis.Scan(workspace.Path)
	if err != nil {
		return nil, nil, err
	}

	var components []Component
	for _, root := range source.ScanRoots(workspace) {
		cs, scanErr := ScanComponents(root, workspace.Alias)
		if scanErr != nil {
			log.Printf("wiki index: Trellis workspace %q scan %s had errors: %v", workspace.Alias, root, scanErr)
		}
		for i := range cs {
			stampComponentSource(&cs[i], source.KindTrellis)
		}
		components = append(components, cs...)
	}

	componentByPath := make(map[string]Component, len(components)+len(records))
	for _, component := range components {
		componentByPath[filepath.Clean(component.Path)] = component
	}
	taskByDirName := make(map[string]string, len(records))
	for _, record := range records {
		frontmatter := map[string]any{
			"_source":  string(source.KindTrellis),
			"status":   record.Task.Status,
			"phase":    record.Task.Status,
			"archived": record.Archived,
			"priority": record.Task.Priority,
			"parent":   record.Task.Parent,
			"children": append([]string(nil), record.Task.Children...),
		}
		if record.Task.CreatedAt != "" {
			frontmatter["created_at"] = record.Task.CreatedAt
		}
		component := Component{
			ID:          record.TaskJSON,
			Type:        TypeChange,
			Title:       record.Task.Title,
			Path:        record.TaskJSON,
			Workspace:   workspace.Alias,
			Frontmatter: frontmatter,
			UpdatedAt:   record.UpdatedAt,
		}
		components = append(components, component)
		componentByPath[filepath.Clean(component.Path)] = component
		taskByDirName[record.DirName] = component.ID
	}

	var edges []Edge
	for _, record := range records {
		taskID := record.TaskJSON
		artifactIDs := make(map[string]string)
		for _, artifact := range trellis.Artifacts(record) {
			clean := filepath.Clean(artifact.Path)
			component, ok := componentByPath[clean]
			if !ok {
				continue
			}
			if component.ID == taskID {
				continue
			}
			artifactIDs[artifact.File] = component.ID
			edges = append(edges, Edge{From: taskID, To: component.ID, Kind: "generates", Source: "convention-internal"})
		}
		if prdID := artifactIDs["prd.md"]; prdID != "" {
			if designID := artifactIDs["design.md"]; designID != "" {
				edges = append(edges, Edge{From: prdID, To: designID, Kind: "implements", Source: "convention-internal"})
			}
		}
		if designID := artifactIDs["design.md"]; designID != "" {
			if implementID := artifactIDs["implement.md"]; implementID != "" {
				edges = append(edges, Edge{From: designID, To: implementID, Kind: "implements", Source: "convention-internal"})
			}
		}

		if parentID := resolveTrellisTaskRef(record.Task.Parent, taskByDirName); parentID != "" {
			edges = append(edges, Edge{From: parentID, To: taskID, Kind: "parent-of", Source: "task-json"})
		}
		for _, child := range record.Task.Children {
			if childID := resolveTrellisTaskRef(child, taskByDirName); childID != "" {
				edges = append(edges, Edge{From: taskID, To: childID, Kind: "parent-of", Source: "task-json"})
			}
		}

		refs, contextErr := trellis.ContextReferences(record)
		if contextErr != nil {
			log.Printf("wiki index: Trellis task %q context unreadable: %v", record.DirName, contextErr)
		} else {
			edges = append(edges, trellisContextEdges(taskID, trellis.ProjectRoot(record), refs, componentByPath)...)
		}
	}
	return components, deduplicateEdges(edges), nil
}

func deduplicateEdges(edges []Edge) []Edge {
	seen := make(map[Edge]struct{}, len(edges))
	out := edges[:0]
	for _, edge := range edges {
		if _, exists := seen[edge]; exists {
			continue
		}
		seen[edge] = struct{}{}
		out = append(out, edge)
	}
	return out
}

func trellisContextEdges(taskID, projectRoot string, refs []string, components map[string]Component) []Edge {
	var edges []Edge
	for _, ref := range refs {
		target, valid := trellis.ResolveReference(projectRoot, ref)
		if !valid {
			continue
		}
		component, ok := components[target]
		if !ok {
			continue
		}
		edges = append(edges, Edge{From: taskID, To: component.ID, Kind: "references", Source: "task-context"})
	}
	return edges
}

func stampComponentSource(component *Component, kind source.Kind) {
	if component.Frontmatter == nil {
		component.Frontmatter = make(map[string]any, 1)
	}
	component.Frontmatter["_source"] = string(kind)
}

func resolveTrellisTaskRef(ref string, taskByDirName map[string]string) string {
	ref = strings.TrimSpace(strings.Trim(ref, "/"))
	if ref == "" {
		return ""
	}
	if id := taskByDirName[ref]; id != "" {
		return id
	}
	for dirName, id := range taskByDirName {
		if strings.HasSuffix(dirName, "-"+ref) {
			return id
		}
	}
	return ""
}
