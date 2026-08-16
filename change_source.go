package main

import (
	"fmt"

	"stele/internal/source"
)

// changeSource isolates the dashboard change/list/detail contract from the
// on-disk workflow format. Wiki indexing has its own thin adapter in package
// wiki so neither package imports package main.
type changeSource interface {
	List(workspace WorkspaceConfig) ([]ChangeSummary, error)
	Detail(workspace WorkspaceConfig, name string) (*ChangeDetail, error)
}

type openSpecChangeSource struct{}
type trellisChangeSource struct{}
type superpowersChangeSource struct{}

// docsChangeSource is empty by construction: a plain documentation tree carries
// documents, never changes. It exists so the dispatch has somewhere to send
// KindDocs other than the OpenSpec reader.
type docsChangeSource struct{}

func (docsChangeSource) List(WorkspaceConfig) ([]ChangeSummary, error) {
	return nil, nil
}

func (docsChangeSource) Detail(_ WorkspaceConfig, name string) (*ChangeDetail, error) {
	return nil, fmt.Errorf("workspace has no changes: %q", name)
}

func changeSourceFor(workspace WorkspaceConfig) (changeSource, WorkspaceConfig, error) {
	kind, err := source.ResolveKind(workspace.Path, workspace.Type)
	if err != nil {
		return nil, workspace, err
	}
	workspace.Type = kind
	switch kind {
	case source.KindTrellis:
		return trellisChangeSource{}, workspace, nil
	case source.KindSuperpowers:
		return superpowersChangeSource{}, workspace, nil
	case source.KindDocs:
		// A plain docs tree has no changes at all. Falling through to OpenSpec
		// made it read <root>/changes, which does not exist, so every dashboard
		// refresh logged the workspace as unreadable and the frontend showed it
		// in the "workspace 无法读取" banner.
		return docsChangeSource{}, workspace, nil
	default:
		return openSpecChangeSource{}, workspace, nil
	}
}

func (openSpecChangeSource) List(workspace WorkspaceConfig) ([]ChangeSummary, error) {
	summaries, err := scanAllChanges(workspace.Path)
	if err != nil {
		return nil, err
	}
	for i := range summaries {
		summaries[i].Workspace = workspace.Alias
		summaries[i].SourceType = source.KindOpenSpec
	}
	return summaries, nil
}

func (openSpecChangeSource) Detail(workspace WorkspaceConfig, name string) (*ChangeDetail, error) {
	detail, err := scanChangeDetail(workspace.Path, name)
	if err != nil {
		return nil, err
	}
	detail.Workspace = workspace.Alias
	detail.SourceType = source.KindOpenSpec
	return detail, nil
}

func (trellisChangeSource) List(workspace WorkspaceConfig) ([]ChangeSummary, error) {
	return scanTrellisWorkspaceChanges(workspace)
}

func (trellisChangeSource) Detail(workspace WorkspaceConfig, name string) (*ChangeDetail, error) {
	return scanTrellisChangeDetail(workspace, name)
}

func (superpowersChangeSource) List(workspace WorkspaceConfig) ([]ChangeSummary, error) {
	return scanSuperpowersWorkspaceChanges(workspace)
}

func (superpowersChangeSource) Detail(workspace WorkspaceConfig, name string) (*ChangeDetail, error) {
	return scanSuperpowersChangeDetail(workspace, name)
}
