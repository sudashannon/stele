package wiki

import "time"

type ComponentType string

const (
	TypeChange    ComponentType = "change"
	TypeProposal  ComponentType = "proposal"
	TypeDesign    ComponentType = "design"
	TypeTasks     ComponentType = "tasks"
	TypeSpec      ComponentType = "spec"
	TypePlan      ComponentType = "plan"
	TypeArtifact  ComponentType = "artifact"
	TypeDiagram   ComponentType = "diagram"
	TypeReport    ComponentType = "report"
	TypeKnowledge ComponentType = "knowledge"
)

type Component struct {
	ID          string         `json:"id"` // absolute, canonicalized path — stable identity
	Type        ComponentType  `json:"type"`
	Title       string         `json:"title"`
	Path        string         `json:"path"`
	Workspace   string         `json:"workspace"`
	Frontmatter map[string]any `json:"frontmatter"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type Edge struct {
	From   string  `json:"from"`
	To     string  `json:"to"`     // Component.ID
	Kind   string  `json:"kind"`   // references | implements | generates | traces-back | supersedes
	Source string  `json:"source"` // "yaml" (highest confidence) | "markdown-link" | "slug-match" (lint-only)
	Weight float64 `json:"weight,omitempty"`
}
