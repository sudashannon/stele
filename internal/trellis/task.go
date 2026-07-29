package trellis

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	StatusPlanning   = "planning"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusRejected   = "rejected"
)

// Task mirrors the durable fields Trellis writes to task.json. Unknown fields
// are intentionally ignored so newer Trellis versions remain readable.
type Task struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Status       string         `json:"status"`
	DevType      string         `json:"dev_type"`
	Scope        string         `json:"scope"`
	Package      string         `json:"package"`
	Priority     string         `json:"priority"`
	Creator      string         `json:"creator"`
	Assignee     string         `json:"assignee"`
	CreatedAt    string         `json:"createdAt"`
	CompletedAt  string         `json:"completedAt"`
	Branch       string         `json:"branch"`
	BaseBranch   string         `json:"base_branch"`
	WorktreePath string         `json:"worktree_path"`
	Commit       string         `json:"commit"`
	PRURL        string         `json:"pr_url"`
	Children     []string       `json:"children"`
	Subtasks     []string       `json:"subtasks"`
	Parent       string         `json:"parent"`
	RelatedFiles []string       `json:"relatedFiles"`
	Notes        string         `json:"notes"`
	Meta         map[string]any `json:"meta"`
}

// Record couples parsed metadata to its physical task directory. DirName is
// stable when Trellis moves a completed task under archive/YYYY-MM/.
type Record struct {
	Task      Task
	Dir       string
	DirName   string
	TaskJSON  string
	Archived  bool
	UpdatedAt time.Time
}

// ProjectRoot returns the registered repository root for an active or
// archived task record.
func ProjectRoot(record Record) string {
	dir := record.Dir
	for {
		if filepath.Base(dir) == ".trellis" {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Artifact is a user-readable task file grouped by the lifecycle phase where
// Trellis creates or consumes it.
type Artifact struct {
	File   string
	Label  string
	Path   string
	Phase  string
	Exists bool
}

// Scan returns active tasks plus tasks archived under archive/YYYY-MM/.
// A Trellis workspace with no tasks directory yet is an empty task set;
// malformed individual tasks are logged and skipped.
func Scan(projectRoot string) ([]Record, error) {
	tasksRoot := filepath.Join(filepath.Clean(projectRoot), ".trellis", "tasks")
	entries, err := os.ReadDir(tasksRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}

	var records []Record
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == "archive" {
			archived, scanErr := scanArchive(filepath.Join(tasksRoot, entry.Name()))
			if scanErr != nil {
				log.Printf("trellis scan: archive unreadable, skipping: %v", scanErr)
			}
			records = append(records, archived...)
			continue
		}
		if record, readErr := readRecord(filepath.Join(tasksRoot, entry.Name()), false); readErr == nil {
			records = append(records, record)
		} else {
			log.Printf("trellis scan: skipping %s: %v", entry.Name(), readErr)
		}
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Archived != records[j].Archived {
			return !records[i].Archived
		}
		if records[i].Task.CreatedAt != records[j].Task.CreatedAt {
			return records[i].Task.CreatedAt > records[j].Task.CreatedAt
		}
		return records[i].DirName < records[j].DirName
	})
	return records, nil
}

func scanArchive(archiveRoot string) ([]Record, error) {
	months, err := os.ReadDir(archiveRoot)
	if err != nil {
		return nil, err
	}
	var records []Record
	for _, month := range months {
		if !month.IsDir() {
			continue
		}
		taskEntries, readErr := os.ReadDir(filepath.Join(archiveRoot, month.Name()))
		if readErr != nil {
			log.Printf("trellis scan: archive month %s unreadable, skipping: %v", month.Name(), readErr)
			continue
		}
		for _, taskEntry := range taskEntries {
			if !taskEntry.IsDir() {
				continue
			}
			dir := filepath.Join(archiveRoot, month.Name(), taskEntry.Name())
			record, recordErr := readRecord(dir, true)
			if recordErr != nil {
				log.Printf("trellis scan: skipping archived task %s: %v", taskEntry.Name(), recordErr)
				continue
			}
			records = append(records, record)
		}
	}
	return records, nil
}

func readRecord(dir string, archived bool) (Record, error) {
	taskJSON := filepath.Join(dir, "task.json")
	task, info, err := ReadTask(taskJSON)
	if err != nil {
		return Record{}, err
	}
	return Record{
		Task:      task,
		Dir:       filepath.Clean(dir),
		DirName:   filepath.Base(dir),
		TaskJSON:  filepath.Clean(taskJSON),
		Archived:  archived,
		UpdatedAt: info.ModTime(),
	}, nil
}

// ReadTask parses one task.json and returns its file metadata.
func ReadTask(path string) (Task, os.FileInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Task{}, nil, err
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return Task{}, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return Task{}, nil, err
	}
	if task.ID == "" {
		task.ID = task.Name
	}
	if task.Name == "" {
		task.Name = task.ID
	}
	if task.Title == "" {
		task.Title = task.Name
	}
	if task.Status == "" {
		task.Status = StatusPlanning
	}
	if !IsStatus(task.Status) {
		return Task{}, nil, fmt.Errorf("parse %s: unsupported status %q", path, task.Status)
	}
	if len(task.Children) == 0 && len(task.Subtasks) > 0 {
		task.Children = append([]string(nil), task.Subtasks...)
	}
	return task, info, nil
}

// Find resolves the stable task directory name first, then the task id/name.
// Ambiguous ids are rejected instead of silently choosing the wrong task.
func Find(projectRoot, name string) (Record, error) {
	records, err := Scan(projectRoot)
	if err != nil {
		return Record{}, err
	}
	for _, record := range records {
		if record.DirName == name {
			return record, nil
		}
	}
	var matches []Record
	for _, record := range records {
		if record.Task.ID == name || record.Task.Name == name {
			matches = append(matches, record)
		}
	}
	switch len(matches) {
	case 0:
		return Record{}, os.ErrNotExist
	case 1:
		return matches[0], nil
	default:
		return Record{}, fmt.Errorf("trellis task name %q is ambiguous; use its MM-DD directory name", name)
	}
}

// Progress uses child completion for parent tasks and PRD checkboxes for leaf
// tasks. Archived children count as complete even if an older task.json lacks
// status=completed.
func Progress(record Record, all []Record) (completed, total int) {
	if len(record.Task.Children) > 0 {
		byName := make(map[string]Record, len(all))
		for _, candidate := range all {
			byName[candidate.DirName] = candidate
		}
		for _, childName := range record.Task.Children {
			total++
			child, ok := byName[childName]
			if ok && (child.Archived || child.Task.Status == StatusCompleted) {
				completed++
			}
		}
		return completed, total
	}
	return CountCheckboxes(filepath.Join(record.Dir, "prd.md"))
}

var checkboxRE = regexp.MustCompile(`^\s*-\s*\[([ xX])\]`)

// CountCheckboxes counts markdown checklist items without interpreting prose.
func CountCheckboxes(path string) (completed, total int) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := checkboxRE.FindStringSubmatch(scanner.Text())
		if len(match) != 2 {
			continue
		}
		total++
		if match[1] == "x" || match[1] == "X" {
			completed++
		}
	}
	return completed, total
}

// Artifacts describes known Trellis task files and durable research notes.
func Artifacts(record Record) []Artifact {
	artifacts := []Artifact{
		artifact(record.Dir, "task.json", "task.json", StatusPlanning),
		artifact(record.Dir, "prd.md", "PRD", StatusPlanning),
		artifact(record.Dir, "design.md", "Design", StatusPlanning),
		artifact(record.Dir, "implement.md", "Implementation Plan", StatusPlanning),
		artifact(record.Dir, "implement.jsonl", "Implement Context", StatusInProgress),
		artifact(record.Dir, "check.jsonl", "Check Context", StatusInProgress),
		artifact(record.Dir, "debug.jsonl", "Debug Context", StatusInProgress),
	}
	researchDir := filepath.Join(record.Dir, "research")
	entries, err := os.ReadDir(researchDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				continue
			}
			artifacts = append(artifacts, artifact(researchDir, entry.Name(), "Research: "+entry.Name(), StatusPlanning))
		}
	}
	return artifacts
}

func artifact(dir, name, label, phase string) Artifact {
	path := filepath.Join(dir, name)
	_, err := os.Stat(path)
	return Artifact{File: name, Label: label, Path: path, Phase: phase, Exists: err == nil}
}

// ContextReferences returns repo-relative file references from Trellis JSONL
// context manifests. Seed/example rows without a file field are ignored.
func ContextReferences(record Record) ([]string, error) {
	var refs []string
	for _, name := range []string{"implement.jsonl", "check.jsonl", "debug.jsonl"} {
		path := filepath.Join(record.Dir, name)
		file, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var row struct {
				File string `json:"file"`
			}
			if json.Unmarshal(scanner.Bytes(), &row) == nil && row.File != "" {
				refs = append(refs, row.File)
			}
		}
		scanErr := scanner.Err()
		file.Close()
		if scanErr != nil {
			return nil, scanErr
		}
	}
	return refs, nil
}

// ResolveReference resolves a Trellis context-manifest path against the repo
// root and rejects traversal outside it.
func ResolveReference(projectRoot, reference string) (string, bool) {
	if reference == "" {
		return "", false
	}
	var candidate string
	if filepath.IsAbs(reference) {
		candidate = filepath.Clean(reference)
	} else {
		candidate = filepath.Join(filepath.Clean(projectRoot), filepath.FromSlash(reference))
	}
	rootAbs, rootErr := filepath.Abs(projectRoot)
	pathAbs, pathErr := filepath.Abs(candidate)
	if rootErr != nil || pathErr != nil {
		return "", false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Clean(pathAbs), true
}

// ScriptPath returns Trellis' official task CLI. State-changing callers must
// invoke this script instead of editing task.json directly.
func ScriptPath(projectRoot string) (string, error) {
	path := filepath.Join(filepath.Clean(projectRoot), ".trellis", "scripts", "task.py")
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("Trellis task CLI not found at %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("Trellis task CLI path %s is a directory", path)
	}
	return path, nil
}

// IsStatus reports whether status is part of Trellis' durable task schema.
func IsStatus(status string) bool {
	switch status {
	case StatusPlanning, StatusInProgress, StatusCompleted, StatusRejected:
		return true
	default:
		return false
	}
}
