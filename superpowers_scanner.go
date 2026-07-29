package main

import (
	"comet-ui/internal/source"
	sp "comet-ui/internal/superpowers"
)

func superpowersLifecycle() []LifecycleStep {
	return []LifecycleStep{
		{Key: sp.PhaseDesign, Label: "设计"},
		{Key: sp.PhasePlan, Label: "计划"},
		{Key: sp.PhaseBuild, Label: "执行"},
		{Key: sp.PhaseVerify, Label: "验证"},
		{Key: sp.PhaseCompleted, Label: "完成"},
	}
}

func scanSuperpowersWorkspaceChanges(workspace WorkspaceConfig) ([]ChangeSummary, error) {
	records, err := sp.Scan(workspace.Path)
	if err != nil {
		return nil, err
	}
	changes := make([]ChangeSummary, 0, len(records))
	for _, record := range records {
		changes = append(changes, superpowersChangeSummary(record, workspace))
	}
	return changes, nil
}

func scanSuperpowersChangeDetail(workspace WorkspaceConfig, name string) (*ChangeDetail, error) {
	record, err := sp.Find(workspace.Path, name)
	if err != nil {
		return nil, err
	}
	summary := superpowersChangeSummary(record, workspace)
	phases := []PhaseInfo{
		{
			Key:       sp.PhaseDesign,
			Label:     "1. Design",
			Status:    superpowersPhaseStatus(record.Phase, sp.PhaseDesign),
			Artifacts: superpowersArtifacts(record.Specs, false),
		},
		{
			Key:       sp.PhasePlan,
			Label:     "2. Plan",
			Status:    superpowersPhaseStatus(record.Phase, sp.PhasePlan),
			Artifacts: superpowersArtifacts(record.Plans, true),
		},
		{
			Key:       sp.PhaseBuild,
			Label:     "3. Build",
			Status:    superpowersPhaseStatus(record.Phase, sp.PhaseBuild),
			Artifacts: superpowersArtifacts(record.Artifacts, false),
		},
		{
			Key:       sp.PhaseVerify,
			Label:     "4. Verify",
			Status:    superpowersPhaseStatus(record.Phase, sp.PhaseVerify),
			Artifacts: superpowersArtifacts(record.Reports, false),
		},
		{
			Key:       sp.PhaseCompleted,
			Label:     "5. Completed",
			Status:    superpowersPhaseStatus(record.Phase, sp.PhaseCompleted),
			Artifacts: []ArtifactInfo{},
		},
	}
	return &ChangeDetail{ChangeSummary: summary, Phases: phases}, nil
}

func superpowersChangeSummary(record sp.Record, workspace WorkspaceConfig) ChangeSummary {
	return ChangeSummary{
		Name:           record.Name,
		Title:          record.Title,
		ComponentID:    record.AnchorPath,
		Workspace:      workspace.Alias,
		SourceType:     source.KindSuperpowers,
		Workflow:       "superpowers",
		Phase:          record.Phase,
		Archived:       record.Archived,
		TasksCompleted: record.TasksCompleted,
		TasksTotal:     record.TasksTotal,
		VerifyResult:   record.VerifyResult,
		CreatedAt:      record.CreatedAt,
		Artifacts: map[string]bool{
			"design":       len(record.Specs) > 0,
			"plan":         len(record.Plans) > 0,
			"execution":    len(record.Artifacts) > 0,
			"verifyReport": len(record.Reports) > 0,
		},
		Lifecycle:    superpowersLifecycle(),
		ProposalPath: record.AnchorPath,
	}
}

func superpowersArtifacts(documents []sp.Document, tasks bool) []ArtifactInfo {
	artifacts := make([]ArtifactInfo, 0, len(documents))
	for _, document := range documents {
		label := document.Title
		if label == "" {
			label = document.Label
		}
		artifacts = append(artifacts, ArtifactInfo{
			File:    document.File,
			Label:   label,
			Exists:  true,
			Path:    document.Path,
			IsTasks: tasks,
		})
	}
	return artifacts
}

func superpowersPhaseStatus(current, target string) string {
	order := []string{sp.PhaseDesign, sp.PhasePlan, sp.PhaseBuild, sp.PhaseVerify, sp.PhaseCompleted}
	currentIndex, targetIndex := -1, -1
	for i, phase := range order {
		if phase == current {
			currentIndex = i
		}
		if phase == target {
			targetIndex = i
		}
	}
	if currentIndex == -1 || targetIndex == -1 {
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
