package wiki

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed taxonomy.yaml
var taxonomyFS embed.FS

// Facet groups tags by what kind of thing they name, which decides what they
// are allowed to do. Product lines are great filters and terrible edges: a
// `rx101` tag covering 178 documents tells a reader what to click and tells a
// clustering algorithm nothing.
type Facet struct {
	Name  string
	Edges bool
}

// Taxonomy is the controlled vocabulary: which tags exist, what they are
// called, and which of them may build graph edges.
type Taxonomy struct {
	facetOf   map[string]Facet  // canonical tag -> facet
	canonical map[string]string // normalized alias or canonical -> canonical
	phrases   []taxonomyPhrase  // multi-token keys, longest first
	singles   map[string]string // single-token key -> canonical

	minDocs  int
	maxShare float64
}

type taxonomyPhrase struct {
	key       string
	canonical string
	tokens    []string
}

type taxonomyFile struct {
	EdgeMinDocs  int     `yaml:"edgeMinDocs"`
	EdgeMaxShare float64 `yaml:"edgeMaxShare"`
	Facets       map[string]struct {
		Edges bool                `yaml:"edges"`
		Tags  map[string][]string `yaml:"tags"`
	} `yaml:"facets"`
}

// ParseTaxonomy builds a Taxonomy from the YAML shape documented in
// taxonomy.yaml. Keys are case-insensitive and separator-normalized.
func ParseTaxonomy(data []byte) (*Taxonomy, error) {
	var file taxonomyFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse taxonomy: %w", err)
	}
	if len(file.Facets) == 0 {
		return nil, fmt.Errorf("parse taxonomy: no facets defined")
	}
	if file.EdgeMinDocs < 0 {
		return nil, fmt.Errorf("parse taxonomy: edgeMinDocs must not be negative")
	}
	if file.EdgeMaxShare < 0 {
		return nil, fmt.Errorf("parse taxonomy: edgeMaxShare must not be negative")
	}
	t := &Taxonomy{
		facetOf:   make(map[string]Facet),
		canonical: make(map[string]string),
		singles:   make(map[string]string),
		minDocs:   file.EdgeMinDocs,
		maxShare:  file.EdgeMaxShare,
	}
	if t.minDocs == 0 {
		t.minDocs = 3
	}
	if t.maxShare == 0 {
		t.maxShare = 0.035
	}
	if t.maxShare > 1 {
		return nil, fmt.Errorf("parse taxonomy: edgeMaxShare must not exceed 1")
	}

	facetNames := make([]string, 0, len(file.Facets))
	for name := range file.Facets {
		facetNames = append(facetNames, name)
	}
	sort.Strings(facetNames)
	for _, name := range facetNames {
		facet := file.Facets[name]
		f := Facet{Name: name, Edges: facet.Edges}
		tagNames := make([]string, 0, len(facet.Tags))
		for tag := range facet.Tags {
			tagNames = append(tagNames, tag)
		}
		sort.Strings(tagNames)
		for _, tag := range tagNames {
			aliases := facet.Tags[tag]
			canonical := normalizeTaxonomyKey(tag)
			if canonical == "" {
				return nil, fmt.Errorf("parse taxonomy: empty canonical tag in facet %q", name)
			}
			if existing, ok := t.facetOf[canonical]; ok {
				return nil, fmt.Errorf("parse taxonomy: tag %q declared in both %q and %q", canonical, existing.Name, name)
			}
			if prior, ok := t.canonical[canonical]; ok {
				return nil, fmt.Errorf("parse taxonomy: canonical tag %q conflicts with alias for %q", canonical, prior)
			}
			t.facetOf[canonical] = f
			t.register(canonical, canonical)

			seenAliases := make(map[string]struct{}, len(aliases))
			for _, alias := range aliases {
				key := normalizeTaxonomyKey(alias)
				if key == "" {
					return nil, fmt.Errorf("parse taxonomy: empty alias for %q", canonical)
				}
				// Separator/case variants of the canonical spelling are already
				// accepted by normalization and need not be registered twice.
				if key == canonical {
					continue
				}
				if _, duplicate := seenAliases[key]; duplicate {
					return nil, fmt.Errorf("parse taxonomy: duplicate alias %q for %q", key, canonical)
				}
				seenAliases[key] = struct{}{}
				if prior, ok := t.canonical[key]; ok {
					return nil, fmt.Errorf("parse taxonomy: alias %q maps to both %q and %q", key, prior, canonical)
				}
				t.register(key, canonical)
			}
		}
	}
	// Match the most specific phrase first. Token count is the semantic length;
	// character count breaks that tie, then key lexical order breaks an equal
	// token-count and character-count tie deterministically.
	sort.Slice(t.phrases, func(i, j int) bool {
		if len(t.phrases[i].tokens) != len(t.phrases[j].tokens) {
			return len(t.phrases[i].tokens) > len(t.phrases[j].tokens)
		}
		if len(t.phrases[i].key) != len(t.phrases[j].key) {
			return len(t.phrases[i].key) > len(t.phrases[j].key)
		}
		return t.phrases[i].key < t.phrases[j].key
	})
	return t, nil
}

func (t *Taxonomy) register(key, canonical string) {
	t.canonical[key] = canonical
	tokens := strings.Split(key, "-")
	if len(tokens) > 1 {
		t.phrases = append(t.phrases, taxonomyPhrase{key: key, canonical: canonical, tokens: tokens})
		return
	}
	t.singles[key] = canonical
}

var (
	taxonomyOnce   sync.Once
	loadedTaxonomy *Taxonomy
)

// TaxonomyOverridePath is where a user-maintained vocabulary may shadow the
// embedded default, so terms can be added without rebuilding the binary.
func TaxonomyOverridePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".comet-panel", "taxonomy.yaml")
}

// LoadTaxonomy returns the process-wide vocabulary: the user override when it
// parses, otherwise the embedded default. A broken override is logged and
// ignored rather than fatal — a typo in a vocabulary file must not take the
// whole index down.
func LoadTaxonomy() *Taxonomy {
	taxonomyOnce.Do(func() {
		defaultData, err := taxonomyFS.ReadFile("taxonomy.yaml")
		if err != nil {
			log.Printf("wiki taxonomy: embedded vocabulary unreadable: %v", err)
			loadedTaxonomy = emptyTaxonomy()
			return
		}
		loadedTaxonomy, err = loadTaxonomy(TaxonomyOverridePath(), defaultData)
		if err != nil {
			log.Printf("wiki taxonomy: embedded vocabulary invalid: %v", err)
			loadedTaxonomy = emptyTaxonomy()
		}
	})
	return loadedTaxonomy
}

// loadTaxonomy keeps override failure non-fatal while returning an error for a
// broken embedded default. Keeping this decision outside sync.Once makes the
// fallback behavior directly testable.
func loadTaxonomy(overridePath string, defaultData []byte) (*Taxonomy, error) {
	if overridePath != "" {
		if data, err := os.ReadFile(overridePath); err == nil {
			if parsed, parseErr := ParseTaxonomy(data); parseErr == nil {
				log.Printf("wiki taxonomy: loaded override %s", overridePath)
				return parsed, nil
			} else {
				log.Printf("wiki taxonomy: ignoring invalid override %s: %v", overridePath, parseErr)
			}
		} else if !os.IsNotExist(err) {
			log.Printf("wiki taxonomy: unable to read override %s: %v", overridePath, err)
		}
	}
	return ParseTaxonomy(defaultData)
}

func emptyTaxonomy() *Taxonomy {
	return &Taxonomy{
		facetOf:   map[string]Facet{},
		canonical: map[string]string{},
		singles:   map[string]string{},
		minDocs:   3,
		maxShare:  0.035,
	}
}

// Canonical folds an alias, case variant, or separator variant onto its
// canonical tag. The second return reports whether the term is controlled.
func (t *Taxonomy) Canonical(raw string) (string, bool) {
	key := normalizeTaxonomyKey(raw)
	if key == "" {
		return "", false
	}
	canonical, ok := t.canonical[key]
	return canonical, ok
}

// Facet returns the facet a canonical tag belongs to.
func (t *Taxonomy) Facet(tag string) (Facet, bool) {
	f, ok := t.facetOf[normalizeTaxonomyKey(tag)]
	return f, ok
}

// EdgeEligible reports whether a tag may build similarity edges: its facet must
// allow it, and its document count must sit inside the coverage band. Below the
// floor a tag connects too few documents to form anything; above the ceiling it
// connects unrelated ones (the live corpus has three tags covering 189
// documents each — an entire vendor manual sharing one label).
func (t *Taxonomy) EdgeEligible(tag string, docCount, corpusSize int) bool {
	facet, ok := t.Facet(tag)
	if !ok || !facet.Edges || corpusSize <= 0 {
		return false
	}
	if docCount < t.minDocs {
		return false
	}
	if float64(docCount) > t.maxShare*float64(corpusSize) {
		return false
	}
	return true
}

// Size reports how many canonical tags the vocabulary holds.
func (t *Taxonomy) Size() int { return len(t.facetOf) }

var (
	datePrefix         = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-?`)
	taxonomySeparators = regexp.MustCompile(`[-_.\s]+`)
)

func normalizeTaxonomyKey(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.Trim(taxonomySeparators.ReplaceAllString(raw, "-"), "-")
}

// DeriveFromSlug extracts vocabulary tags from a change slug or filename such
// as "2026-05-29-kmc-aws-kms-integration". Multi-token vocabulary terms are
// matched against the whole slug first (so `md-bus-stress` yields `md-bus`, not
// a meaningless `bus`), then remaining single tokens are looked up.
//
// This is what makes coverage grow without guessing: every artifact of an
// OpenSpec change shares one directory slug, so proposal/design/tasks/spec all
// derive the same tags from the name the author already chose.
func (t *Taxonomy) DeriveFromSlug(slug string) []string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil
	}
	slug = datePrefix.ReplaceAllString(slug, "")
	slug = strings.TrimSuffix(slug, ".md")
	normalized := normalizeTaxonomyKey(slug)
	if normalized == "" {
		return nil
	}
	tokens := strings.Split(normalized, "-")
	consumed := make([]bool, len(tokens))

	var tags []string
	seen := make(map[string]bool)
	add := func(canonical string) {
		if !seen[canonical] {
			seen[canonical] = true
			tags = append(tags, canonical)
		}
	}

	// Phrase pass: longest phrases claim token spans before shorter phrases or
	// single aliases can see them. Scanning left-to-right makes repeated and
	// equal-length phrase matches deterministic.
	for _, phrase := range t.phrases {
		for start := 0; start+len(phrase.tokens) <= len(tokens); start++ {
			match := true
			for offset, want := range phrase.tokens {
				if consumed[start+offset] || tokens[start+offset] != want {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			add(phrase.canonical)
			for offset := range phrase.tokens {
				consumed[start+offset] = true
			}
		}
	}
	for i, token := range tokens {
		if consumed[i] {
			continue
		}
		if canonical, ok := t.singles[token]; ok {
			add(canonical)
		}
	}
	sort.Strings(tags)
	return tags
}

const (
	derivedTagsKey   = "_derivedTags"
	inheritedTagsKey = "_inheritedTags"
)

// ExplicitComponentTags parses only author-supplied tags. It deliberately
// ignores synthetic provenance so callers cannot accidentally confuse stored
// frontmatter with index-time enrichment.
func ExplicitComponentTags(component Component) []string {
	return frontmatterTags(component.Frontmatter, "tags")
}

// EffectiveComponentTags returns the one tag view consumers should use.
// Explicit tags retain author order and preserve unknown terms. Derived and
// inherited tags are controlled canonical terms appended in stable lexical
// order.
func EffectiveComponentTags(component Component, taxonomy *Taxonomy) []string {
	explicit := ExplicitComponentTags(component)
	derived := frontmatterTags(component.Frontmatter, derivedTagsKey)
	inherited := frontmatterTags(component.Frontmatter, inheritedTagsKey)

	tags := make([]string, 0, len(explicit)+len(derived)+len(inherited))
	seen := make(map[string]struct{}, cap(tags))
	add := func(raw string) {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			return
		}
		if taxonomy != nil {
			if canonical, ok := taxonomy.Canonical(tag); ok {
				tag = canonical
			}
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
	}
	for _, tag := range explicit {
		add(tag)
	}
	appendSorted := func(source []string) {
		canonical := make([]string, 0, len(source))
		for _, raw := range source {
			if taxonomy == nil {
				continue
			}
			tag, controlled := taxonomy.Canonical(raw)
			if !controlled {
				continue
			}
			canonical = append(canonical, tag)
		}
		sort.Slice(canonical, func(i, j int) bool {
			left, right := strings.ToLower(canonical[i]), strings.ToLower(canonical[j])
			if left != right {
				return left < right
			}
			return canonical[i] < canonical[j]
		})
		for _, tag := range canonical {
			add(tag)
		}
	}
	appendSorted(derived)
	appendSorted(inherited)
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// DeriveComponentTags extracts controlled terms only from path conventions
// that carry semantic slugs. OpenSpec members use their change-directory slug;
// Superpowers documents use their filename (and artifact path segments).
func DeriveComponentTags(component Component, taxonomy *Taxonomy) []string {
	if taxonomy == nil || component.Path == "" {
		return nil
	}
	path := filepath.ToSlash(filepath.Clean(component.Path))
	if slug, ok := openSpecChangeSlug(path); ok {
		return taxonomy.DeriveFromSlug(slug)
	}

	lowerPath := strings.ToLower(path)
	const absoluteMarker = "/docs/superpowers/"
	const relativeMarker = "docs/superpowers/"
	var remainder string
	if index := strings.Index(lowerPath, absoluteMarker); index >= 0 {
		remainder = path[index+len(absoluteMarker):]
	} else if strings.HasPrefix(lowerPath, relativeMarker) {
		remainder = path[len(relativeMarker):]
	} else {
		return nil
	}
	parts := strings.Split(remainder, "/")
	if len(parts) < 2 {
		return nil
	}
	section := strings.ToLower(parts[0])
	allowedType := false
	switch section {
	case "specs":
		allowedType = component.Type == TypeDesign
	case "plans":
		allowedType = component.Type == TypePlan
	case "reports":
		allowedType = component.Type == TypeReport
	case "artifacts":
		allowedType = component.Type == TypeArtifact
	default:
		return nil
	}
	if !allowedType {
		return nil
	}
	slugs := parts[len(parts)-1:]
	if section == "artifacts" {
		slugs = parts[1:]
	}
	var tags []string
	seen := make(map[string]struct{})
	for _, slug := range slugs {
		for _, tag := range taxonomy.DeriveFromSlug(slug) {
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// EnrichComponentTags computes synthetic tag provenance entirely in memory.
// It mutates the caller-supplied component slice while preserving its order;
// callers that need the original component values must pass a copy. Components
// and edges are indexed once to avoid a component-by-component graph scan.
func EnrichComponentTags(components []Component, edges []Edge, taxonomy *Taxonomy) {
	byID := make(map[string]int, len(components))
	changeMembers := make(map[string][]int)
	changeIndexes := make([]int, 0)

	for index := range components {
		// Sessions are not documents: they own no authored tags, and deriving
		// tags from a transcript filename would invent vocabulary hits.
		if components[index].Type == TypeSession {
			continue
		}
		frontmatter := cloneFrontmatter(components[index].Frontmatter)
		delete(frontmatter, derivedTagsKey)
		delete(frontmatter, inheritedTagsKey)
		if len(frontmatter) == 0 {
			frontmatter = nil
		}
		components[index].Frontmatter = frontmatter

		derived := DeriveComponentTags(components[index], taxonomy)
		setSyntheticTags(&components[index], derivedTagsKey, derived)
		byID[components[index].ID] = index
		if key, ok := openSpecChangeKey(components[index]); ok {
			changeMembers[key] = append(changeMembers[key], index)
		}
		if components[index].Type == TypeChange {
			changeIndexes = append(changeIndexes, index)
		}
	}

	outgoing := make(map[string][]Edge)
	for _, edge := range edges {
		outgoing[edge.From] = append(outgoing[edge.From], edge)
	}
	inherited := make([][]string, len(components))
	for _, changeIndex := range changeIndexes {
		change := components[changeIndex]
		seed := controlledSeedTags(change, taxonomy)
		if len(seed) == 0 {
			continue
		}
		if key, ok := openSpecChangeKey(change); ok {
			for _, memberIndex := range changeMembers[key] {
				if memberIndex != changeIndex {
					inherited[memberIndex] = appendUniqueTags(inherited[memberIndex], seed)
				}
			}
		}

		depthOne := make([]int, 0)
		depthOneSeen := make(map[int]struct{})
		for _, edge := range outgoing[change.ID] {
			if edge.Source != "yaml" || (edge.Kind != "implements" && edge.Kind != "references") {
				continue
			}
			targetIndex, exists := byID[edge.To]
			if !exists || components[targetIndex].Workspace != change.Workspace {
				continue
			}
			inherited[targetIndex] = appendUniqueTags(inherited[targetIndex], seed)
			if components[targetIndex].Type == TypeTasks || components[targetIndex].Type == TypePlan {
				if _, duplicate := depthOneSeen[targetIndex]; !duplicate {
					depthOneSeen[targetIndex] = struct{}{}
					depthOne = append(depthOne, targetIndex)
				}
			}
		}
		for _, sourceIndex := range depthOne {
			source := components[sourceIndex]
			for _, edge := range outgoing[source.ID] {
				if (edge.Source != "convention-internal" && edge.Source != "superpowers-convention") ||
					(edge.Kind != "generates" && edge.Kind != "implements") {
					continue
				}
				targetIndex, exists := byID[edge.To]
				if !exists || components[targetIndex].Workspace != change.Workspace {
					continue
				}
				inherited[targetIndex] = appendUniqueTags(inherited[targetIndex], seed)
			}
		}
	}
	for index := range components {
		tags := inherited[index]
		sort.Slice(tags, func(i, j int) bool {
			left, right := strings.ToLower(tags[i]), strings.ToLower(tags[j])
			if left != right {
				return left < right
			}
			return tags[i] < tags[j]
		})
		setSyntheticTags(&components[index], inheritedTagsKey, tags)
	}
}

func controlledSeedTags(component Component, taxonomy *Taxonomy) []string {
	if taxonomy == nil {
		return nil
	}
	candidate := append(ExplicitComponentTags(component), frontmatterTags(component.Frontmatter, derivedTagsKey)...)
	tags := make([]string, 0, len(candidate))
	seen := make(map[string]struct{}, len(candidate))
	for _, raw := range candidate {
		tag, ok := taxonomy.Canonical(raw)
		if !ok {
			continue
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags
}

func sameTagInputs(left, right Component, taxonomy *Taxonomy) bool {
	if left.Type != right.Type || filepath.Clean(left.Path) != filepath.Clean(right.Path) {
		return false
	}
	return equalStrings(
		comparableTagInputs(left, taxonomy),
		comparableTagInputs(right, taxonomy),
	) && equalStrings(
		DeriveComponentTags(left, taxonomy),
		DeriveComponentTags(right, taxonomy),
	)
}

func comparableTagInputs(component Component, taxonomy *Taxonomy) []string {
	explicit := ExplicitComponentTags(component)
	tags := make([]string, 0, len(explicit))
	seen := make(map[string]struct{}, len(explicit))
	for _, raw := range explicit {
		tag := strings.TrimSpace(raw)
		if taxonomy != nil {
			if canonical, ok := taxonomy.Canonical(tag); ok {
				tag = canonical
			} else {
				tag = strings.ToLower(tag)
			}
		} else {
			tag = strings.ToLower(tag)
		}
		if tag == "" {
			continue
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func copySyntheticTags(destination *Component, source Component) {
	if destination == nil {
		return
	}
	destination.Frontmatter = cloneFrontmatter(destination.Frontmatter)
	delete(destination.Frontmatter, derivedTagsKey)
	delete(destination.Frontmatter, inheritedTagsKey)
	for _, key := range []string{derivedTagsKey, inheritedTagsKey} {
		tags := frontmatterTags(source.Frontmatter, key)
		setSyntheticTags(destination, key, tags)
	}
}

func stripSyntheticTags(component Component) Component {
	component.Frontmatter = cloneFrontmatter(component.Frontmatter)
	delete(component.Frontmatter, derivedTagsKey)
	delete(component.Frontmatter, inheritedTagsKey)
	return component
}

func frontmatterTags(frontmatter map[string]any, key string) []string {
	if frontmatter == nil {
		return nil
	}
	raw, exists := frontmatter[key]
	if !exists || raw == nil {
		return nil
	}
	var candidates []string
	switch value := raw.(type) {
	case []any:
		candidates = make([]string, 0, len(value))
		for _, item := range value {
			if item != nil {
				candidates = append(candidates, fmt.Sprint(item))
			}
		}
	case []string:
		candidates = value
	case string:
		candidates = []string{value}
	default:
		return nil
	}
	tags := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		tag := strings.TrimSpace(candidate)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func openSpecChangeSlug(path string) (string, bool) {
	remainder, ok := openSpecChangeRemainder(path)
	if !ok {
		return "", false
	}
	parts := strings.Split(remainder, "/")
	if strings.EqualFold(parts[0], "archive") {
		if len(parts) < 2 || parts[1] == "" {
			return "", false
		}
		return parts[1], true
	}
	return parts[0], parts[0] != ""
}

func openSpecChangeRemainder(path string) (string, bool) {
	path = filepath.ToSlash(filepath.Clean(path))
	lower := strings.ToLower(path)
	const absoluteMarker = "/openspec/changes/"
	if index := strings.Index(lower, absoluteMarker); index >= 0 {
		return path[index+len(absoluteMarker):], true
	}
	const relativeMarker = "openspec/changes/"
	if strings.HasPrefix(lower, relativeMarker) {
		return path[len(relativeMarker):], true
	}
	return "", false
}

func openSpecChangeKey(component Component) (string, bool) {
	remainder, ok := openSpecChangeRemainder(component.Path)
	if !ok {
		return "", false
	}
	parts := strings.Split(remainder, "/")
	count := 1
	if strings.EqualFold(parts[0], "archive") {
		if len(parts) < 2 {
			return "", false
		}
		count = 2
	}
	changeDir := strings.ToLower(strings.Join(parts[:count], "/"))
	return component.Workspace + "\x00" + changeDir, true
}

func appendUniqueTags(destination, source []string) []string {
	seen := make(map[string]struct{}, len(destination)+len(source))
	for _, tag := range destination {
		seen[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	for _, raw := range source {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		destination = append(destination, tag)
	}
	return destination
}

func setSyntheticTags(component *Component, key string, tags []string) {
	if len(tags) == 0 {
		return
	}
	if component.Frontmatter == nil {
		component.Frontmatter = make(map[string]any)
	}
	component.Frontmatter[key] = append([]string(nil), tags...)
}

func cloneFrontmatter(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
