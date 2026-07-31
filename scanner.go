package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"stele/internal/pathresolve"
	"stele/internal/source"
)

type ChangeSummary struct {
	Name           string            `json:"name"`
	Title          string            `json:"title,omitempty"`
	ComponentID    string            `json:"componentId,omitempty"`
	Workspace      string            `json:"workspace,omitempty"`
	SourceType     source.Kind       `json:"sourceType,omitempty"`
	Workflow       string            `json:"workflow"`
	Phase          string            `json:"phase"`
	Archived       bool              `json:"archived"`
	TasksCompleted int               `json:"tasksCompleted"`
	TasksTotal     int               `json:"tasksTotal"`
	VerifyResult   string            `json:"verifyResult"`
	CreatedAt      string            `json:"createdAt"`
	Artifacts      map[string]bool   `json:"artifacts"`
	Visualized     bool              `json:"visualized"`
	DesignReviewed bool              `json:"designReviewed"`
	VerifyReviewed bool              `json:"verifyReviewed"`
	VerifiedAt     string            `json:"verifiedAt"`
	BuildMode      string            `json:"buildMode"`
	ReviewMode     string            `json:"reviewMode"`
	TddMode        string            `json:"tddMode"`
	AutoTransition bool              `json:"autoTransition"`
	StateWarning   string            `json:"stateWarning,omitempty"`
	Lifecycle      []LifecycleStep   `json:"lifecycle,omitempty"`
	NextTransition *TransitionAction `json:"nextTransition,omitempty"`
	ProposalPath   string            `json:"-"`
}

type LifecycleStep struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type TransitionAction struct {
	Target        string `json:"target"`
	Label         string `json:"label"`
	Command       string `json:"command"`
	BlockedReason string `json:"blockedReason,omitempty"`
}

type ChangeDetail struct {
	ChangeSummary
	Phases []PhaseInfo `json:"phases"`
}

type PhaseInfo struct {
	Key       string         `json:"key"`
	Label     string         `json:"label"`
	Status    string         `json:"status"`
	Artifacts []ArtifactInfo `json:"artifacts"`
}

type ArtifactInfo struct {
	File     string `json:"file"`
	Label    string `json:"label"`
	Exists   bool   `json:"exists"`
	Path     string `json:"path,omitempty"`
	External bool   `json:"external,omitempty"`
	IsTasks  bool   `json:"isTasks,omitempty"`
}

type cometYAML struct {
	Workflow           string
	Phase              string
	VerifyResult       string
	DesignDoc          string
	Plan               string
	VerificationReport string
	Archived           bool
	Visualized         bool
	DesignReviewed     bool
	VerifyReviewed     bool
	CreatedAt          string
	VerifiedAt         string
	BuildMode          string
	ReviewMode         string
	TddMode            string
	AutoTransition     bool
}

func parseCometYAML(path string) (*cometYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &cometYAML{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if val == "null" || val == "~" {
			val = ""
		}
		switch key {
		case "workflow":
			c.Workflow = val
		case "phase":
			c.Phase = val
		case "verify_result":
			c.VerifyResult = val
		case "design_doc":
			c.DesignDoc = val
		case "plan":
			c.Plan = val
		case "verification_report":
			c.VerificationReport = val
		case "archived":
			c.Archived = val == "true"
		case "visualized":
			c.Visualized = val == "true"
		case "design_reviewed":
			c.DesignReviewed = val == "true"
		case "verify_reviewed":
			c.VerifyReviewed = val == "true"
		case "created_at":
			c.CreatedAt = val
		case "verified_at":
			c.VerifiedAt = val
		case "build_mode":
			c.BuildMode = val
		case "review_mode":
			c.ReviewMode = val
		case "tdd_mode":
			c.TddMode = val
		case "auto_transition":
			c.AutoTransition = val == "true"
		}
	}
	return c, nil
}

var taskCheckboxRe = regexp.MustCompile(`^\s*- \[(.)\]`)

func countTasks(tasksPath string) (completed, total int) {
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		m := taskCheckboxRe.FindStringSubmatch(line)
		if m != nil {
			total++
			if strings.ToLower(m[1]) == "x" {
				completed++
			}
		}
	}
	return completed, total
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func phaseStatus(actualPhase, targetPhase string) string {
	phases := []string{"open", "design", "build", "verify", "archive"}
	actualIdx := -1
	targetIdx := -1
	for i, p := range phases {
		if p == actualPhase {
			actualIdx = i
		}
		if p == targetPhase {
			targetIdx = i
		}
	}
	if actualIdx < 0 {
		// actualPhase is missing/unrecognized (e.g. no .comet.yaml) — don't
		// fabricate "open" (misleading for a change that's actually far
		// along but just lacks metadata); signal unknown for every phase.
		return "unknown"
	}
	if targetIdx < actualIdx {
		return "completed"
	}
	if targetIdx == actualIdx {
		return "current"
	}
	return "pending"
}

func computeStateWarning(archived bool, phase string) string {
	if archived && phase != "archive" && phase != "" {
		return fmt.Sprintf("archived=true 但 phase=%s（状态不一致）", phase)
	}
	if !archived && phase == "archive" {
		return "phase=archive 但 archived=false（状态不一致）"
	}
	return ""
}

func extractDate(dirName string) string {
	if len(dirName) >= 10 {
		return dirName[:10]
	}
	return ""
}

func scanAllChanges(baseDir string) ([]ChangeSummary, error) {
	// Tolerate a workspace path registered as the repo ROOT (e.g.
	// /home/shanl/workspace/rx101) instead of its openspec dir: if there's no
	// changes/ directly under baseDir but there is an openspec/changes/, descend
	// into openspec/. This lets users register the natural repo root without the
	// scan silently finding nothing.
	if !isDir(filepath.Join(baseDir, "changes")) && isDir(filepath.Join(baseDir, "openspec", "changes")) {
		baseDir = filepath.Join(baseDir, "openspec")
	}
	changesDir := filepath.Join(baseDir, "changes")
	projectRoot := filepath.Join(baseDir, "..")
	var results []ChangeSummary

	entries, err := os.ReadDir(changesDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "archive" {
			continue
		}
		ch := scanChange(changesDir, e.Name(), false, projectRoot)
		results = append(results, ch)
	}

	archiveDir := filepath.Join(changesDir, "archive")
	archEntries, err := os.ReadDir(archiveDir)
	if err == nil {
		// reverse to get newest first (dirs are YYYY-MM-DD-name, alpha order = date asc)
		for i, j := 0, len(archEntries)-1; i < j; i, j = i+1, j-1 {
			archEntries[i], archEntries[j] = archEntries[j], archEntries[i]
		}
		for _, e := range archEntries {
			if !e.IsDir() {
				continue
			}
			ch := scanChange(archiveDir, e.Name(), true, projectRoot)
			results = append(results, ch)
		}
	}

	return results, nil
}

func scanWorkspaceChanges(ws WorkspaceConfig) ([]ChangeSummary, error) {
	adapter, resolved, err := changeSourceFor(ws)
	if err != nil {
		return nil, err
	}
	return adapter.List(resolved)
}

// scanAllWorkspaces aggregates changes across every registered workspace.
// An unreadable workspace path is skipped (logged) rather than failing the
// whole aggregation — one bad path shouldn't take down the dashboard.
// failedAliases is returned (not just logged) so the HTTP layer can surface
// a warning banner to the frontend (design doc error table requirement).
func scanAllWorkspaces(registry []WorkspaceConfig) (all []ChangeSummary, failedAliases []string) {
	for _, ws := range registry {
		summaries, err := scanWorkspaceChanges(ws)
		if err != nil {
			log.Printf("workspace %q (%s) unreadable, skipping: %v", ws.Alias, ws.Path, err)
			failedAliases = append(failedAliases, ws.Alias)
			continue
		}
		all = append(all, summaries...)
	}
	return all, failedAliases
}

func openSpecLifecycle() []LifecycleStep {
	return []LifecycleStep{
		{Key: "open", Label: "启动"},
		{Key: "design", Label: "设计"},
		{Key: "build", Label: "构建"},
		{Key: "verify", Label: "验证"},
		{Key: "archive", Label: "归档"},
	}
}

func openSpecNextTransition(name, phase string, completed, total int) *TransitionAction {
	order := []string{"open", "design", "build", "verify", "archive"}
	labels := map[string]string{
		"design":  "进入设计",
		"build":   "进入构建",
		"verify":  "进入验证",
		"archive": "归档",
	}
	for i, current := range order {
		if current != phase || i == len(order)-1 {
			continue
		}
		target := order[i+1]
		action := &TransitionAction{
			Target:  target,
			Label:   labels[target],
			Command: fmt.Sprintf("comet-guard %s %s --apply", name, target),
		}
		if phase == "build" && target == "verify" && !(completed == total && total > 0) {
			action.BlockedReason = fmt.Sprintf("任务未全部完成 (%d/%d)，无法进入验证", completed, total)
		}
		return action
	}
	return nil
}

func scanChange(parentDir, name string, archived bool, base string) ChangeSummary {
	dir := filepath.Join(parentDir, name)

	cy, _ := parseCometYAML(filepath.Join(dir, ".comet.yaml"))

	completed, total := countTasks(filepath.Join(dir, "tasks.md"))

	artifacts := map[string]bool{
		"proposal":     fileExists(filepath.Join(dir, "proposal.md")),
		"design":       fileExists(filepath.Join(dir, "design.md")),
		"tasks":        fileExists(filepath.Join(dir, "tasks.md")),
		"plan":         false,
		"verifyReport": false,
	}

	if cy != nil {
		if cy.Plan != "" {
			artifacts["plan"] = fileExists(filepath.Join(base, cy.Plan))
		}
		if cy.VerificationReport != "" {
			artifacts["verifyReport"] = fileExists(filepath.Join(base, cy.VerificationReport))
		}
	}

	phase := ""
	workflow := ""
	verifyResult := "pending"
	if cy != nil {
		phase = cy.Phase
		workflow = cy.Workflow
		if cy.VerifyResult != "" {
			verifyResult = cy.VerifyResult
		}
	}

	createdAt := ""
	if archived {
		createdAt = extractDate(name)
	} else if cy != nil && cy.CreatedAt != "" {
		createdAt = cy.CreatedAt
	}

	var visualized, designReviewed, verifyReviewed, autoTransition bool
	var verifiedAt, buildMode, reviewMode, tddMode string
	if cy != nil {
		visualized = cy.Visualized
		designReviewed = cy.DesignReviewed
		verifyReviewed = cy.VerifyReviewed
		verifiedAt = cy.VerifiedAt
		buildMode = cy.BuildMode
		reviewMode = cy.ReviewMode
		tddMode = cy.TddMode
		autoTransition = cy.AutoTransition
	}

	return ChangeSummary{
		Name:           name,
		Title:          name,
		ComponentID:    filepath.Join(dir, ".comet.yaml"),
		SourceType:     source.KindOpenSpec,
		Workflow:       workflow,
		Phase:          phase,
		Archived:       archived,
		TasksCompleted: completed,
		TasksTotal:     total,
		VerifyResult:   verifyResult,
		CreatedAt:      createdAt,
		Artifacts:      artifacts,
		Visualized:     visualized,
		DesignReviewed: designReviewed,
		VerifyReviewed: verifyReviewed,
		VerifiedAt:     verifiedAt,
		BuildMode:      buildMode,
		ReviewMode:     reviewMode,
		TddMode:        tddMode,
		AutoTransition: autoTransition,
		StateWarning:   computeStateWarning(archived, phase),
		Lifecycle:      openSpecLifecycle(),
		NextTransition: openSpecNextTransition(name, phase, completed, total),
		ProposalPath:   filepath.Join(dir, "proposal.md"),
	}
}

func scanChangeDetail(baseDir, name string) (*ChangeDetail, error) {
	// Same repo-root tolerance as scanAllChanges: if baseDir is a repo root
	// (no changes/ but has openspec/changes/), descend into openspec/ so the
	// detail/artifacts view resolves the change dir correctly.
	if !isDir(filepath.Join(baseDir, "changes")) && isDir(filepath.Join(baseDir, "openspec", "changes")) {
		baseDir = filepath.Join(baseDir, "openspec")
	}
	changesDir := filepath.Join(baseDir, "changes")

	dir := filepath.Join(changesDir, name)
	archived := false
	if !fileExists(filepath.Join(dir, ".comet.yaml")) {
		archiveDir := filepath.Join(changesDir, "archive")
		entries, err := os.ReadDir(archiveDir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			trimmed := e.Name()
			if trimmed == name || (len(trimmed) > 11 && trimmed[11:] == name) {
				dir = filepath.Join(archiveDir, e.Name())
				archived = true
				break
			}
		}
		if !archived {
			return nil, os.ErrNotExist
		}
	}

	root := filepath.Join(baseDir, "..")

	cy, _ := parseCometYAML(filepath.Join(dir, ".comet.yaml"))

	completed, total := countTasks(filepath.Join(dir, "tasks.md"))

	phase := ""
	workflow := ""
	verifyResult := "pending"
	designDoc := ""
	plan := ""
	verifyReport := ""

	if cy != nil {
		phase = cy.Phase
		workflow = cy.Workflow
		if cy.VerifyResult != "" {
			verifyResult = cy.VerifyResult
		}
		designDoc = cy.DesignDoc
		plan = cy.Plan
		verifyReport = cy.VerificationReport
	}

	createdAt := ""
	displayName := name
	if archived {
		createdAt = extractDate(filepath.Base(dir))
		displayName = filepath.Base(dir)
	}

	phases := buildPhases(root, dir, phase, completed, total, designDoc, plan, verifyReport)

	summary := scanChange(filepath.Dir(dir), filepath.Base(dir), archived, root)
	summary.Name = displayName
	summary.Title = displayName
	summary.Workflow = workflow
	summary.Phase = phase
	summary.Archived = archived
	summary.TasksCompleted = completed
	summary.TasksTotal = total
	summary.VerifyResult = verifyResult
	if createdAt != "" {
		summary.CreatedAt = createdAt
	}
	summary.NextTransition = openSpecNextTransition(displayName, phase, completed, total)

	return &ChangeDetail{
		ChangeSummary: summary,
		Phases:        phases,
	}, nil
}

func buildPhases(root, dir, phase string, completed, total int, designDoc, plan, verifyReport string) []PhaseInfo {
	phases := []struct{ key, label string }{
		{"open", "1. Open"},
		{"design", "2. Design"},
		{"build", "3. Build"},
		{"verify", "4. Verify"},
		{"archive", "5. Archive"},
	}
	var result []PhaseInfo
	for _, p := range phases {
		status := phaseStatus(phase, p.key)
		artifacts := buildPhaseArtifacts(root, dir, p.key, completed, total, designDoc, plan, verifyReport)
		result = append(result, PhaseInfo{
			Key:       p.key,
			Label:     p.label,
			Status:    status,
			Artifacts: artifacts,
		})
	}
	return result
}

func buildPhaseArtifacts(root, dir, phase string, completed, total int, designDoc, plan, verifyReport string) []ArtifactInfo {
	switch phase {
	case "open":
		return []ArtifactInfo{
			makeArtifact("proposal.md", "proposal.md", filepath.Join(dir, "proposal.md")),
			makeArtifact("design.md", "design.md (初稿)", filepath.Join(dir, "design.md")),
			makeArtifact("tasks.md", "tasks.md (骨架)", filepath.Join(dir, "tasks.md")),
		}
	case "design":
		var specs []ArtifactInfo
		specsDir := filepath.Join(dir, "specs")
		if entries, err := os.ReadDir(specsDir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					specFile := filepath.Join(specsDir, e.Name(), "spec.md")
					specs = append(specs, makeArtifact("spec:"+e.Name(), "spec: "+e.Name(), specFile))
				}
			}
		}
		designArtifacts := []ArtifactInfo{
			makeArtifactExt("design_doc", "design doc", designDoc, root, dir),
		}
		designArtifacts = append(designArtifacts, specs...)
		handoffPath := filepath.Join(dir, ".comet", "handoff", "design-context.md")
		designArtifacts = append(designArtifacts, makeArtifact("handoff", "handoff/context", handoffPath))
		return designArtifacts
	case "build":
		tasksLabel := "tasks.md"
		if total > 0 {
			tasksLabel = fmt.Sprintf("tasks.md (%d 项)", total)
		}
		buildArts := []ArtifactInfo{
			makeArtifactExt("plan", "plan", plan, root, dir),
			{File: "tasks.md", Label: tasksLabel, Exists: fileExists(filepath.Join(dir, "tasks.md")), Path: filepath.Join(dir, "tasks.md"), IsTasks: true},
		}
		// scan docs/superpowers/artifacts/<plan-slug>/ by convention
		if plan != "" {
			slug := strings.TrimSuffix(filepath.Base(plan), ".md")
			artifactsDir := filepath.Join(root, "docs", "superpowers", "artifacts", slug)
			if entries, err := os.ReadDir(artifactsDir); err == nil {
				for _, e := range entries {
					if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
						continue
					}
					artPath := filepath.Join(artifactsDir, e.Name())
					buildArts = append(buildArts, makeArtifact("artifact:"+e.Name(), "📝 "+e.Name(), artPath))
				}
			}
		}
		return buildArts
	case "verify":
		return []ArtifactInfo{
			makeArtifactExt("verify_report", "verify report", verifyReport, root, dir),
		}
	default:
		return []ArtifactInfo{}
	}
}

func makeArtifact(file, label, path string) ArtifactInfo {
	return ArtifactInfo{File: file, Label: label, Exists: fileExists(path), Path: path}
}

func makeArtifactExt(file, label, ref, root, changeDir string) ArtifactInfo {
	p := pathresolve.ResolveArtifactPath(ref, root, changeDir)
	if p == "" {
		return ArtifactInfo{File: file, Label: label, Exists: false}
	}
	return ArtifactInfo{File: file, Label: label, Exists: fileExists(p), Path: p, External: true}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
