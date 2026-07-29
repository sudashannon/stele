package main

import (
	"fmt"
	"path/filepath"

	"comet-ui/internal/source"
	"comet-ui/internal/trellis"
)

func trellisLifecycle(status string) []LifecycleStep {
	if status == trellis.StatusRejected {
		return []LifecycleStep{
			{Key: trellis.StatusPlanning, Label: "规划"},
			{Key: trellis.StatusRejected, Label: "已拒绝"},
		}
	}
	return []LifecycleStep{
		{Key: trellis.StatusPlanning, Label: "规划"},
		{Key: trellis.StatusInProgress, Label: "执行"},
		{Key: trellis.StatusCompleted, Label: "完成"},
	}
}

func trellisNextTransition(record trellis.Record, completed, total int) *TransitionAction {
	script := filepath.Join(trellis.ProjectRoot(record), ".trellis", "scripts", "task.py")
	switch record.Task.Status {
	case trellis.StatusPlanning:
		return &TransitionAction{
			Target:  trellis.StatusInProgress,
			Label:   "开始执行",
			Command: fmt.Sprintf("python3 %s start %s", script, record.DirName),
		}
	case trellis.StatusInProgress:
		action := &TransitionAction{
			Target:  trellis.StatusCompleted,
			Label:   "完成并归档",
			Command: fmt.Sprintf("python3 %s archive %s --no-commit", script, record.DirName),
		}
		if total > 0 && completed < total {
			action.BlockedReason = fmt.Sprintf("验收项未全部完成 (%d/%d)，无法归档", completed, total)
		}
		return action
	case trellis.StatusCompleted:
		if !record.Archived {
			return &TransitionAction{
				Target:  trellis.StatusCompleted,
				Label:   "归档任务",
				Command: fmt.Sprintf("python3 %s archive %s --no-commit", script, record.DirName),
			}
		}
	}
	return nil
}

func scanTrellisWorkspaceChanges(ws WorkspaceConfig) ([]ChangeSummary, error) {
	records, err := trellis.Scan(ws.Path)
	if err != nil {
		return nil, err
	}
	changes := make([]ChangeSummary, 0, len(records))
	for _, record := range records {
		changes = append(changes, trellisChangeSummary(record, records, ws.Alias))
	}
	return changes, nil
}

func trellisChangeSummary(record trellis.Record, records []trellis.Record, workspace string) ChangeSummary {
	completed, total := trellis.Progress(record, records)
	artifacts := map[string]bool{
		"prd":              false,
		"design":           false,
		"implement":        false,
		"research":         false,
		"implementContext": false,
		"checkContext":     false,
	}
	for _, artifact := range trellis.Artifacts(record) {
		switch artifact.File {
		case "prd.md":
			artifacts["prd"] = artifact.Exists
		case "design.md":
			artifacts["design"] = artifact.Exists
		case "implement.md":
			artifacts["implement"] = artifact.Exists
		case "implement.jsonl":
			artifacts["implementContext"] = artifact.Exists
		case "check.jsonl":
			artifacts["checkContext"] = artifact.Exists
		default:
			if artifact.Phase == trellis.StatusPlanning && filepath.Dir(artifact.Path) == filepath.Join(record.Dir, "research") {
				artifacts["research"] = true
			}
		}
	}

	verifyResult := "pending"
	if value, ok := record.Task.Meta["verify_result"].(string); ok && value != "" {
		verifyResult = value
	}
	createdAt := record.Task.CreatedAt
	if createdAt == "" && !record.UpdatedAt.IsZero() {
		createdAt = record.UpdatedAt.Format("2006-01-02")
	}

	summary := ChangeSummary{
		Name:           record.DirName,
		Title:          record.Task.Title,
		ComponentID:    record.TaskJSON,
		Workspace:      workspace,
		SourceType:     source.KindTrellis,
		Workflow:       "trellis",
		Phase:          record.Task.Status,
		Archived:       record.Archived,
		TasksCompleted: completed,
		TasksTotal:     total,
		VerifyResult:   verifyResult,
		CreatedAt:      createdAt,
		Artifacts:      artifacts,
		VerifiedAt:     record.Task.CompletedAt,
		BuildMode:      record.Task.DevType,
		StateWarning:   trellisStateWarning(record),
		Lifecycle:      trellisLifecycle(record.Task.Status),
		ProposalPath:   filepath.Join(record.Dir, "prd.md"),
	}
	summary.NextTransition = trellisNextTransition(record, completed, total)
	return summary
}

func trellisStateWarning(record trellis.Record) string {
	if record.Archived && record.Task.Status != trellis.StatusCompleted && record.Task.Status != trellis.StatusRejected {
		return fmt.Sprintf("任务已归档但 status=%s（状态不一致）", record.Task.Status)
	}
	if !record.Archived && record.Task.Status == trellis.StatusCompleted {
		return "status=completed 但任务仍在 active tasks 目录，尚未归档"
	}
	return ""
}

func scanTrellisChangeDetail(ws WorkspaceConfig, name string) (*ChangeDetail, error) {
	records, err := trellis.Scan(ws.Path)
	if err != nil {
		return nil, err
	}
	var record *trellis.Record
	for i := range records {
		if records[i].DirName == name {
			record = &records[i]
			break
		}
	}
	if record == nil {
		resolved, findErr := trellis.Find(ws.Path, name)
		if findErr != nil {
			return nil, findErr
		}
		record = &resolved
	}

	summary := trellisChangeSummary(*record, records, ws.Alias)
	lifecycle := trellisLifecycle(record.Task.Status)
	artifactGroups := make(map[string][]ArtifactInfo, len(lifecycle))
	for _, artifact := range trellis.Artifacts(*record) {
		artifactGroups[artifact.Phase] = append(artifactGroups[artifact.Phase], ArtifactInfo{
			File:   artifact.File,
			Label:  artifact.Label,
			Exists: artifact.Exists,
			Path:   artifact.Path,
		})
	}

	phases := make([]PhaseInfo, 0, len(lifecycle))
	for _, step := range lifecycle {
		artifacts := artifactGroups[step.Key]
		if artifacts == nil {
			artifacts = []ArtifactInfo{}
		}
		phases = append(phases, PhaseInfo{
			Key:       step.Key,
			Label:     step.Label,
			Status:    lifecyclePhaseStatus(lifecycle, record.Task.Status, step.Key),
			Artifacts: artifacts,
		})
	}
	return &ChangeDetail{ChangeSummary: summary, Phases: phases}, nil
}

func lifecyclePhaseStatus(lifecycle []LifecycleStep, current, target string) string {
	currentIndex := -1
	targetIndex := -1
	for i, step := range lifecycle {
		if step.Key == current {
			currentIndex = i
		}
		if step.Key == target {
			targetIndex = i
		}
	}
	if currentIndex < 0 || targetIndex < 0 {
		return "unknown"
	}
	if targetIndex < currentIndex {
		return "completed"
	}
	if targetIndex == currentIndex {
		return "current"
	}
	return "pending"
}
