package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"stele/internal/source"
	"stele/internal/trellis"
)

type transitionSource interface {
	ValidateTarget(target string) error
	Preflight(workspace WorkspaceConfig, changeName, target string) error
	Trigger(ctx context.Context, workspace WorkspaceConfig, changeName, target string) (io.ReadCloser, error)
}

type openSpecTransitionSource struct{}
type trellisTransitionSource struct{}
type superpowersTransitionSource struct{}

func transitionSourceFor(workspace WorkspaceConfig) (transitionSource, WorkspaceConfig, error) {
	kind, err := source.ResolveKind(workspace.Path, workspace.Type)
	if err != nil {
		return nil, workspace, err
	}
	workspace.Type = kind
	switch kind {
	case source.KindTrellis:
		return trellisTransitionSource{}, workspace, nil
	case source.KindSuperpowers:
		return superpowersTransitionSource{}, workspace, nil
	default:
		return openSpecTransitionSource{}, workspace, nil
	}
}

func (openSpecTransitionSource) ValidateTarget(target string) error {
	switch target {
	case "open", "design", "build", "verify", "archive":
		return nil
	default:
		return fmt.Errorf("invalid targetPhase: must be one of open/design/build/verify/archive")
	}
}

func (openSpecTransitionSource) Preflight(_ WorkspaceConfig, _, _ string) error {
	_, _, err := resolveCometGuard()
	return err
}

func (openSpecTransitionSource) Trigger(ctx context.Context, workspace WorkspaceConfig, changeName, target string) (io.ReadCloser, error) {
	return TriggerTransition(ctx, changeName, target, workspace.Path)
}

func (superpowersTransitionSource) ValidateTarget(string) error {
	return fmt.Errorf("Superpowers workspaces are read-only and do not support transitions")
}

func (superpowersTransitionSource) Preflight(WorkspaceConfig, string, string) error {
	return fmt.Errorf("Superpowers workspaces are read-only and do not support transitions")
}

func (superpowersTransitionSource) Trigger(context.Context, WorkspaceConfig, string, string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("Superpowers workspaces are read-only and do not support transitions")
}

func (trellisTransitionSource) ValidateTarget(target string) error {
	switch target {
	case trellis.StatusInProgress, trellis.StatusCompleted:
		return nil
	default:
		return fmt.Errorf("invalid targetPhase for Trellis: must be one of in_progress/completed")
	}
}

func (trellisTransitionSource) Preflight(workspace WorkspaceConfig, changeName, target string) error {
	if _, err := trellis.ScriptPath(workspace.Path); err != nil {
		return err
	}
	record, err := trellis.Find(workspace.Path, changeName)
	if err != nil {
		return err
	}
	switch target {
	case trellis.StatusInProgress:
		if record.Archived || record.Task.Status != trellis.StatusPlanning {
			return fmt.Errorf("Trellis task %q cannot start from status %q", changeName, record.Task.Status)
		}
	case trellis.StatusCompleted:
		if record.Archived {
			return fmt.Errorf("Trellis task %q is already archived", changeName)
		}
		if record.Task.Status != trellis.StatusInProgress && record.Task.Status != trellis.StatusCompleted {
			return fmt.Errorf("Trellis task %q cannot archive from status %q", changeName, record.Task.Status)
		}
		records, scanErr := trellis.Scan(workspace.Path)
		if scanErr != nil {
			return scanErr
		}
		completed, total := trellis.Progress(record, records)
		if total > 0 && completed < total {
			return fmt.Errorf("Trellis task %q has incomplete acceptance items (%d/%d)", changeName, completed, total)
		}
	}
	return nil
}

func (trellisTransitionSource) Trigger(ctx context.Context, workspace WorkspaceConfig, changeName, target string) (io.ReadCloser, error) {
	script, err := trellis.ScriptPath(workspace.Path)
	if err != nil {
		return nil, err
	}
	record, err := trellis.Find(workspace.Path, changeName)
	if err != nil {
		return nil, err
	}
	args := []string{script}
	if target == trellis.StatusInProgress {
		args = append(args, "start", record.DirName)
	} else {
		args = append(args, "archive", record.DirName, "--no-commit")
	}

	reader, writer := io.Pipe()
	cmd := exec.CommandContext(ctx, "python3", args...)
	cmd.Dir = workspace.Path
	cmd.Stdout = writer
	cmd.Stderr = writer
	go func() {
		runErr := cmd.Run()
		writer.CloseWithError(runErr)
	}()
	return reader, nil
}
