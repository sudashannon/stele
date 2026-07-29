package superpowers

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	PhaseDesign    = "design"
	PhasePlan      = "plan"
	PhaseBuild     = "build"
	PhaseVerify    = "verify"
	PhaseCompleted = "completed"

	VerifyPending = "pending"
	VerifyPass    = "pass"
	VerifyFail    = "fail"
)

type DocumentKind string

const (
	DocumentSpec     DocumentKind = "spec"
	DocumentPlan     DocumentKind = "plan"
	DocumentArtifact DocumentKind = "artifact"
	DocumentReport   DocumentKind = "report"
)

// Document is one durable Superpowers project artifact.
type Document struct {
	Kind        DocumentKind
	File        string
	Label       string
	Path        string
	Title       string
	Frontmatter map[string]any
	UpdatedAt   time.Time
	content     string
}

// Record groups the exact design, plan, execution evidence, and verification
// documents that belong to one standalone Superpowers work item.
type Record struct {
	Name           string
	Title          string
	CreatedAt      string
	UpdatedAt      time.Time
	Phase          string
	Archived       bool
	VerifyResult   string
	TasksCompleted int
	TasksTotal     int
	AnchorPath     string
	Specs          []Document
	Plans          []Document
	Artifacts      []Document
	Reports        []Document
}

var (
	leadingDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)
	fileDatePattern    = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-`)
	checkboxPattern    = regexp.MustCompile(`(?i)^\s*[-*+]\s+\[([ x])\]`)
)

type documentRoot struct {
	kind DocumentKind
	path string
}

// Scan reads only docs/superpowers/{specs,plans,artifacts,reports} under a
// project root. Per-file parse errors are logged and skipped so one malformed
// artifact cannot hide the remaining work items.
func Scan(projectRoot string) ([]Record, error) {
	root := filepath.Join(filepath.Clean(projectRoot), "docs", "superpowers")
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("scan Superpowers workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan Superpowers workspace: %s is not a directory", root)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve Superpowers root: %w", err)
	}
	resolvedProjectRoot, err := filepath.EvalSymlinks(filepath.Clean(projectRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	relative, err := filepath.Rel(resolvedProjectRoot, resolvedRoot)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("Superpowers root %q resolves outside project root %q", root, projectRoot)
	}

	roots := []documentRoot{
		{kind: DocumentSpec, path: filepath.Join(root, "specs")},
		{kind: DocumentPlan, path: filepath.Join(root, "plans")},
		{kind: DocumentArtifact, path: filepath.Join(root, "artifacts")},
		{kind: DocumentReport, path: filepath.Join(root, "reports")},
	}
	grouped := make(map[string]*Record)
	for _, documentRoot := range roots {
		if err := scanDocumentRoot(documentRoot, grouped); err != nil {
			return nil, err
		}
	}

	records := make([]Record, 0, len(grouped))
	for _, record := range grouped {
		finalizeRecord(record)
		records = append(records, *record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return records, nil
}

// Find resolves a work item by its stable exact name.
func Find(projectRoot, name string) (Record, error) {
	records, err := Scan(projectRoot)
	if err != nil {
		return Record{}, err
	}
	for _, record := range records {
		if record.Name == name {
			return record, nil
		}
	}
	return Record{}, fmt.Errorf("Superpowers work item %q not found", name)
}

func scanDocumentRoot(root documentRoot, grouped map[string]*Record) error {
	info, err := os.Stat(root.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scan %s: %w", root.path, err)
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.Walk(root.path, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsPermission(walkErr) {
				log.Printf("superpowers scan: permission denied, skipping %s: %v", path, walkErr)
				if info != nil && info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return walkErr
		}
		if info.IsDir() {
			if path != root.path && strings.HasPrefix(filepath.Base(path), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			log.Printf("superpowers scan: skipping non-regular file %s", path)
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}

		document, parseErr := readDocument(path, root.kind, info.ModTime())
		if parseErr != nil {
			log.Printf("superpowers scan: skipping %s: %v", path, parseErr)
			return nil
		}
		relative, relErr := filepath.Rel(root.path, path)
		if relErr != nil {
			return relErr
		}
		name := documentIdentity(document, relative)
		if name == "" {
			log.Printf("superpowers scan: skipping %s: no stable work item identity", path)
			return nil
		}
		record := grouped[name]
		if record == nil {
			record = &Record{Name: name}
			grouped[name] = record
		}
		switch root.kind {
		case DocumentSpec:
			record.Specs = append(record.Specs, document)
		case DocumentPlan:
			record.Plans = append(record.Plans, document)
		case DocumentArtifact:
			record.Artifacts = append(record.Artifacts, document)
		case DocumentReport:
			record.Reports = append(record.Reports, document)
		}
		return nil
	})
}

func readDocument(path string, kind DocumentKind, updatedAt time.Time) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	content := string(data)
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return Document{}, err
	}
	title := stringValue(frontmatter, "title")
	if title == "" {
		title = firstHeading(body)
	}
	return Document{
		Kind:        kind,
		File:        filepath.Base(path),
		Label:       filepath.Base(path),
		Path:        filepath.Clean(path),
		Title:       title,
		Frontmatter: frontmatter,
		UpdatedAt:   updatedAt,
		content:     body,
	}, nil
}

func splitFrontmatter(content string) (map[string]any, string, error) {
	frontmatter := make(map[string]any)
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return frontmatter, content, nil
	}
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closing = i
			break
		}
	}
	if closing == -1 {
		return nil, "", fmt.Errorf("unterminated YAML frontmatter")
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &frontmatter); err != nil {
		return nil, "", fmt.Errorf("parse YAML frontmatter: %w", err)
	}
	return frontmatter, strings.Join(lines[closing+1:], "\n"), nil
}

func firstHeading(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func documentIdentity(document Document, relative string) string {
	for _, key := range []string{"superpowers_id", "superpowers-id", "change", "comet_change", "archived-with"} {
		if value := stringValue(document.Frontmatter, key); value != "" {
			return explicitIdentity(value)
		}
	}
	if document.Kind == DocumentPlan {
		for _, key := range []string{"design-doc", "design_doc"} {
			if value := stringValue(document.Frontmatter, key); value != "" {
				return canonicalSlug(filepath.Base(filepath.FromSlash(value)), DocumentSpec)
			}
		}
	}
	if document.Kind == DocumentReport {
		if value := reportChange(document.content); value != "" {
			return explicitIdentity(value)
		}
	}
	if document.Kind == DocumentArtifact {
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) > 1 && parts[0] != "" && parts[0] != "." {
			return explicitIdentity(parts[0])
		}
	}
	return canonicalSlug(document.File, document.Kind)
}

func explicitIdentity(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "\"'"))
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return ""
	}
	return value
}

func canonicalSlug(value string, kind DocumentKind) string {
	value = strings.TrimSpace(strings.Trim(value, "\"'"))
	value = filepath.Base(filepath.FromSlash(value))
	value = strings.TrimSuffix(value, filepath.Ext(value))
	value = leadingDatePattern.ReplaceAllString(value, "")
	switch kind {
	case DocumentSpec:
		value = strings.TrimSuffix(value, "-design")
	case DocumentReport:
		value = strings.TrimSuffix(value, "-verify")
	}
	return strings.Trim(value, "- ")
}

func reportChange(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimLeft(scanner.Text(), "-* "))
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "change") {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func finalizeRecord(record *Record) {
	for _, documents := range [][]Document{record.Specs, record.Plans, record.Artifacts, record.Reports} {
		sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	}

	record.Title = firstDocumentTitle(record.Specs, record.Plans, record.Reports, record.Artifacts)
	if record.Title == "" {
		record.Title = record.Name
	}
	record.AnchorPath = firstDocumentPath(record.Specs, record.Plans, record.Reports, record.Artifacts)
	record.CreatedAt, record.UpdatedAt = recordTimes(record)
	for _, plan := range record.Plans {
		completed, total := countCheckboxes(plan.content)
		record.TasksCompleted += completed
		record.TasksTotal += total
	}

	record.VerifyResult = latestVerifyResult(record.Reports)
	record.Phase = PhaseDesign
	if hasStatus(record, "approved") {
		record.Phase = PhasePlan
	}
	if len(record.Plans) > 0 || len(record.Artifacts) > 0 {
		record.Phase = PhaseBuild
	}
	if len(record.Reports) > 0 || (record.TasksTotal > 0 && record.TasksCompleted == record.TasksTotal) {
		record.Phase = PhaseVerify
	}
	if record.VerifyResult == VerifyPass || hasStatus(record, "completed", "archived") {
		record.Phase = PhaseCompleted
		record.Archived = true
	}
}

func firstDocumentTitle(groups ...[]Document) string {
	for _, documents := range groups {
		for _, document := range documents {
			if document.Title != "" {
				return document.Title
			}
		}
	}
	return ""
}

func firstDocumentPath(groups ...[]Document) string {
	for _, documents := range groups {
		if len(documents) > 0 {
			return documents[0].Path
		}
	}
	return ""
}

func recordTimes(record *Record) (string, time.Time) {
	var created string
	var updated time.Time
	for _, documents := range [][]Document{record.Specs, record.Plans, record.Artifacts, record.Reports} {
		for _, document := range documents {
			if match := fileDatePattern.FindStringSubmatch(document.File); len(match) == 2 && (created == "" || match[1] < created) {
				created = match[1]
			}
			if document.UpdatedAt.After(updated) {
				updated = document.UpdatedAt
			}
		}
	}
	if created == "" && !updated.IsZero() {
		created = updated.Format("2006-01-02")
	}
	return created, updated
}

func countCheckboxes(content string) (completed, total int) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inFence := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		match := checkboxPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		total++
		if strings.EqualFold(match[1], "x") {
			completed++
		}
	}
	return completed, total
}

func latestVerifyResult(reports []Document) string {
	for i := len(reports) - 1; i >= 0; i-- {
		report := reports[i]
		for _, key := range []string{"verify_result", "verify-result", "result"} {
			if result := normalizeVerifyResult(stringValue(report.Frontmatter, key)); result != VerifyPending {
				return result
			}
		}
		if result := verifyResultFromBody(report.content); result != VerifyPending {
			return result
		}
	}
	return VerifyPending
}

func verifyResultFromBody(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.ToUpper(strings.TrimSpace(strings.TrimLeft(scanner.Text(), "#-* ")))
		if !(strings.Contains(line, "结论") || strings.HasPrefix(line, "RESULT") || strings.HasPrefix(line, "STATUS")) {
			continue
		}
		if strings.Contains(line, "FAIL") {
			return VerifyFail
		}
		if strings.Contains(line, "PASS") {
			return VerifyPass
		}
	}
	return VerifyPending
}

func normalizeVerifyResult(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VerifyPass, "passed", "success":
		return VerifyPass
	case VerifyFail, "failed", "failure":
		return VerifyFail
	default:
		return VerifyPending
	}
}

func hasStatus(record *Record, statuses ...string) bool {
	allowed := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[strings.ToLower(status)] = struct{}{}
	}
	for _, documents := range [][]Document{record.Specs, record.Plans, record.Artifacts, record.Reports} {
		for _, document := range documents {
			status := strings.ToLower(stringValue(document.Frontmatter, "status"))
			if _, ok := allowed[status]; ok {
				return true
			}
		}
	}
	return false
}

func stringValue(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
