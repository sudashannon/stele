package wiki

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func parseEmbeddedTaxonomy(t *testing.T) *Taxonomy {
	t.Helper()
	data, err := taxonomyFS.ReadFile("taxonomy.yaml")
	if err != nil {
		t.Fatalf("read embedded taxonomy: %v", err)
	}
	taxonomy, err := ParseTaxonomy(data)
	if err != nil {
		t.Fatalf("parse embedded taxonomy: %v", err)
	}
	return taxonomy
}

func TestParseTaxonomyRejectsDuplicateNormalizedAlias(t *testing.T) {
	data := []byte(`
facets:
  subsystem:
    edges: true
    tags:
      first: [shared-tag]
      second: [shared_tag]
`)
	if _, err := ParseTaxonomy(data); err == nil {
		t.Fatal("expected aliases differing only by separator to be rejected")
	}
}

func TestCanonicalCaseAndSeparatorFolding(t *testing.T) {
	taxonomy := parseEmbeddedTaxonomy(t)

	tests := map[string]string{
		"  JeTsOn_ORIN  ": "orin",
		"md_bus":          "md-bus",
		"MD-BUS":          "md-bus",
		"wan2_2":          "wan22",
		"WAN2-2":          "wan22",
	}
	for input, want := range tests {
		got, ok := taxonomy.Canonical(input)
		if !ok || got != want {
			t.Errorf("Canonical(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
}

func TestDeriveFromSlugUsesLongestPhrase(t *testing.T) {
	taxonomy, err := ParseTaxonomy([]byte(`
facets:
  subsystem:
    edges: true
    tags:
      bus: []
      md-bus: []
  activity:
    edges: false
    tags:
      stress: []
`))
	if err != nil {
		t.Fatalf("ParseTaxonomy: %v", err)
	}

	want := []string{"md-bus", "stress"}
	for _, slug := range []string{
		"2026-05-29-md-bus-stress",
		"2026-05-29-md_bus_stress.md",
		"MD.BUS STRESS",
	} {
		if got := taxonomy.DeriveFromSlug(slug); !reflect.DeepEqual(got, want) {
			t.Errorf("DeriveFromSlug(%q) = %v; want %v", slug, got, want)
		}
	}
}

func TestKMCAndKMSRemainSeparate(t *testing.T) {
	taxonomy := parseEmbeddedTaxonomy(t)

	for _, tag := range []string{"kmc", "kms"} {
		got, ok := taxonomy.Canonical(tag)
		if !ok || got != tag {
			t.Fatalf("Canonical(%q) = %q, %v; want the same canonical tag", tag, got, ok)
		}
	}
	want := []string{"kmc", "kms"}
	if got := taxonomy.DeriveFromSlug("2026-05-29-kmc-aws-kms-integration"); !reflect.DeepEqual(got, want) {
		t.Fatalf("DeriveFromSlug returned %v; want %v", got, want)
	}
}

func TestFacetLooksUpCanonicalTag(t *testing.T) {
	taxonomy := parseEmbeddedTaxonomy(t)

	facet, ok := taxonomy.Facet("MD_BUS")
	if !ok || facet.Name != "subsystem" || !facet.Edges {
		t.Fatalf("Facet(md_bus) = %+v, %v; want edge-enabled subsystem", facet, ok)
	}
	if _, ok := taxonomy.Facet("not-controlled"); ok {
		t.Fatal("unknown tag unexpectedly has a facet")
	}
}

func TestParseTaxonomyRejectsEdgeMaxShareAboveOne(t *testing.T) {
	data := []byte(`
edgeMaxShare: 1.01
facets:
  subsystem:
    edges: true
    tags:
      pcie: []
`)
	if _, err := ParseTaxonomy(data); err == nil {
		t.Fatal("expected edgeMaxShare above 1 to be rejected")
	}
}

func TestParseTaxonomyRejectsNegativeEdgeMinDocs(t *testing.T) {
	data := []byte(`
edgeMinDocs: -1
facets:
  subsystem:
    edges: true
    tags:
      pcie: []
`)
	if _, err := ParseTaxonomy(data); err == nil {
		t.Fatal("expected negative edgeMinDocs to be rejected")
	}
}

func TestParseTaxonomyRejectsNegativeEdgeMaxShare(t *testing.T) {
	data := []byte(`
edgeMaxShare: -0.01
facets:
  subsystem:
    edges: true
    tags:
      pcie: []
`)
	if _, err := ParseTaxonomy(data); err == nil {
		t.Fatal("expected negative edgeMaxShare to be rejected")
	}
}

func TestParseTaxonomyZeroThresholdsUseDefaults(t *testing.T) {
	data := []byte(`
edgeMinDocs: 0
edgeMaxShare: 0
facets:
  subsystem:
    edges: true
    tags:
      pcie: []
`)
	taxonomy, err := ParseTaxonomy(data)
	if err != nil {
		t.Fatalf("ParseTaxonomy: %v", err)
	}
	if taxonomy.minDocs != 3 || taxonomy.maxShare != 0.035 {
		t.Fatalf("thresholds = %d, %g; want 3, 0.035", taxonomy.minDocs, taxonomy.maxShare)
	}
}

func TestEdgeEligibleCoverageThresholds(t *testing.T) {
	taxonomy := parseEmbeddedTaxonomy(t)

	tests := []struct {
		name       string
		tag        string
		docCount   int
		corpusSize int
		want       bool
	}{
		{name: "below minimum", tag: "pcie", docCount: 2, corpusSize: 1000},
		{name: "at minimum", tag: "pcie", docCount: 3, corpusSize: 1000, want: true},
		{name: "at maximum share", tag: "pcie", docCount: 35, corpusSize: 1000, want: true},
		{name: "above maximum share", tag: "pcie", docCount: 36, corpusSize: 1000},
		{name: "fractional maximum floors naturally", tag: "pcie", docCount: 4, corpusSize: 100},
		{name: "filter-only facet", tag: "rx101", docCount: 3, corpusSize: 1000},
		{name: "unknown tag", tag: "not-controlled", docCount: 3, corpusSize: 1000},
		{name: "invalid empty corpus", tag: "pcie", docCount: 3, corpusSize: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := taxonomy.EdgeEligible(test.tag, test.docCount, test.corpusSize); got != test.want {
				t.Errorf("EdgeEligible(%q, %d, %d) = %v; want %v", test.tag, test.docCount, test.corpusSize, got, test.want)
			}
		})
	}
}

func TestTaxonomyOverridePathUsesUserHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".comet-panel", "taxonomy.yaml")
	if got := TaxonomyOverridePath(); got != want {
		t.Fatalf("TaxonomyOverridePath() = %q; want %q", got, want)
	}
}

func TestTaxonomyOverridePathReturnsEmptyWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	if got := TaxonomyOverridePath(); got != "" {
		t.Fatalf("TaxonomyOverridePath() = %q; want empty path", got)
	}
}

func TestLoadTaxonomySkipsEmptyOverridePath(t *testing.T) {
	defaultData := []byte(`
facets:
  subsystem:
    edges: true
    tags:
      pcie: []
`)
	taxonomy, err := loadTaxonomy("", defaultData)
	if err != nil {
		t.Fatalf("loadTaxonomy: %v", err)
	}
	if got, ok := taxonomy.Canonical("pcie"); !ok || got != "pcie" {
		t.Fatalf("default Canonical(pcie) = %q, %v; want pcie, true", got, ok)
	}
}

func TestLoadTaxonomyInvalidOverrideFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "taxonomy.yaml")
	if err := os.WriteFile(overridePath, []byte("facets: ["), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	defaultData := []byte(`
facets:
  subsystem:
    edges: true
    tags:
      pcie: []
`)

	taxonomy, err := loadTaxonomy(overridePath, defaultData)
	if err != nil {
		t.Fatalf("loadTaxonomy: %v", err)
	}
	if got, ok := taxonomy.Canonical("PCIE"); !ok || got != "pcie" {
		t.Fatalf("fallback Canonical(PCIE) = %q, %v; want pcie, true", got, ok)
	}
}

func TestLoadTaxonomyLogsUnreadableOverrideAndFallsBack(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	defaultData := []byte(`
facets:
  subsystem:
    edges: true
    tags:
      pcie: []
`)
	overridePath := t.TempDir()
	taxonomy, err := loadTaxonomy(overridePath, defaultData)
	if err != nil {
		t.Fatalf("loadTaxonomy: %v", err)
	}
	if got, ok := taxonomy.Canonical("pcie"); !ok || got != "pcie" {
		t.Fatalf("fallback Canonical(pcie) = %q, %v; want pcie, true", got, ok)
	}
	if got := logs.String(); !strings.Contains(got, "unable to read override "+overridePath) {
		t.Fatalf("log output %q does not report unreadable override", got)
	}
}

func TestLoadTaxonomyRejectsInvalidDefaultAfterInvalidOverride(t *testing.T) {
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "taxonomy.yaml")
	if err := os.WriteFile(overridePath, []byte("facets: ["), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}

	if _, err := loadTaxonomy(overridePath, []byte("facets: [")); err == nil {
		t.Fatal("expected invalid default taxonomy to be reported")
	}
}

func parseEffectiveTagTaxonomy(t *testing.T) *Taxonomy {
	t.Helper()
	taxonomy, err := ParseTaxonomy([]byte(`
facets:
  platform:
    edges: true
    tags:
      orin: [jetson-orin]
      pcie: [PCI-Express]
  subsystem:
    edges: true
    tags:
      cache: []
      kmc: []
      kms: []
`))
	if err != nil {
		t.Fatalf("ParseTaxonomy: %v", err)
	}
	return taxonomy
}

func TestEffectiveComponentTagsOrderingCanonicalizationAndUnknownRetention(t *testing.T) {
	taxonomy := parseEffectiveTagTaxonomy(t)
	component := Component{Frontmatter: map[string]any{
		"tags":           []any{" Jetson_ORIN ", "Owner Label", "owner label", "PCI-Express"},
		derivedTagsKey:   []string{"orin", "kmc", "unknown-derived"},
		inheritedTagsKey: []any{"unknown-inherited", "kms", "PCIE"},
	}}

	wantExplicit := []string{"Jetson_ORIN", "Owner Label", "PCI-Express"}
	if got := ExplicitComponentTags(component); !reflect.DeepEqual(got, wantExplicit) {
		t.Fatalf("ExplicitComponentTags = %v; want %v", got, wantExplicit)
	}
	wantEffective := []string{"orin", "Owner Label", "pcie", "kmc", "kms"}
	if got := EffectiveComponentTags(component, taxonomy); !reflect.DeepEqual(got, wantEffective) {
		t.Fatalf("EffectiveComponentTags = %v; want %v", got, wantEffective)
	}
}

func TestDeriveComponentTagsFromOpenSpecChangeSlug(t *testing.T) {
	taxonomy := parseEffectiveTagTaxonomy(t)
	for _, path := range []string{
		"/repo/openspec/changes/2026-07-20-orin-pcie/design.md",
		"/repo/openspec/changes/archive/2026-07-20-orin-pcie/specs/x/spec.md",
		"openspec/changes/2026-07-20-orin-pcie/.comet.yaml",
	} {
		component := Component{Path: path, Type: TypeDesign}
		want := []string{"orin", "pcie"}
		if got := DeriveComponentTags(component, taxonomy); !reflect.DeepEqual(got, want) {
			t.Errorf("DeriveComponentTags(%q) = %v; want %v", path, got, want)
		}
	}
}

func TestDeriveComponentTagsFromSuperpowersPathFamilies(t *testing.T) {
	taxonomy := parseEffectiveTagTaxonomy(t)
	tests := []struct {
		component Component
		want      []string
	}{
		{Component{Path: "/repo/docs/superpowers/specs/2026-07-20-orin-design.md", Type: TypeDesign}, []string{"orin"}},
		{Component{Path: "/repo/docs/superpowers/plans/2026-07-20-pcie.md", Type: TypePlan}, []string{"pcie"}},
		{Component{Path: "/repo/docs/superpowers/reports/2026-07-20-kms-verify.md", Type: TypeReport}, []string{"kms"}},
		{Component{Path: "/repo/docs/superpowers/artifacts/cache-rollout/review.md", Type: TypeArtifact}, []string{"cache"}},
	}
	for _, test := range tests {
		if got := DeriveComponentTags(test.component, taxonomy); !reflect.DeepEqual(got, test.want) {
			t.Errorf("DeriveComponentTags(%q) = %v; want %v", test.component.Path, got, test.want)
		}
	}
	wrongType := Component{Path: "/repo/docs/superpowers/plans/2026-07-20-pcie.md", Type: TypeKnowledge}
	if got := DeriveComponentTags(wrongType, taxonomy); got != nil {
		t.Fatalf("wrong-type Superpowers path derived %v; want nil", got)
	}
}

func TestEnrichComponentTagsPropagationBoundariesAndProvenance(t *testing.T) {
	taxonomy := parseEffectiveTagTaxonomy(t)
	changeID := "/repo/openspec/changes/2026-07-20-pcie/.comet.yaml"
	components := []Component{
		{ID: changeID, Path: changeID, Type: TypeChange, Workspace: "alpha",
			Frontmatter: map[string]any{"tags": []any{" Owner Label "}, inheritedTagsKey: []string{"stale"}}},
		{ID: "member", Path: "/repo/openspec/changes/2026-07-20-pcie/specs/cache/spec.md", Type: TypeSpec, Workspace: "alpha"},
		{ID: "tasks", Path: "/external/tasks.md", Type: TypeTasks, Workspace: "alpha"},
		{ID: "plan", Path: "/external/plan.md", Type: TypePlan, Workspace: "alpha"},
		{ID: "yaml-generates", Path: "/external/yaml-generates.md", Type: TypeTasks, Workspace: "alpha"},
		{ID: "wrong-first-source", Path: "/external/wrong-first-source.md", Type: TypeTasks, Workspace: "alpha"},
		{ID: "cross-workspace", Path: "/external/cross.md", Type: TypeTasks, Workspace: "beta"},
		{ID: "convention-generates", Path: "/external/a.md", Type: TypeArtifact, Workspace: "alpha"},
		{ID: "convention-implements", Path: "/external/b.md", Type: TypeArtifact, Workspace: "alpha"},
		{ID: "superpowers-generates", Path: "/external/c.md", Type: TypeArtifact, Workspace: "alpha"},
		{ID: "superpowers-implements", Path: "/external/d.md", Type: TypeTasks, Workspace: "alpha"},
		{ID: "plan-second-hop", Path: "/external/e.md", Type: TypeArtifact, Workspace: "alpha"},
		{ID: "blocked-kind", Path: "/external/f.md", Type: TypeArtifact, Workspace: "alpha"},
		{ID: "blocked-source", Path: "/external/g.md", Type: TypeArtifact, Workspace: "alpha"},
		{ID: "blocked-depth", Path: "/external/h.md", Type: TypeArtifact, Workspace: "alpha"},
		{ID: "same-change-tasks", Path: "/repo/openspec/changes/2026-07-20-pcie/tasks.md", Type: TypeTasks, Workspace: "alpha"},
		{ID: "blocked-from-directory-member", Path: "/external/i.md", Type: TypeArtifact, Workspace: "alpha"},
		{ID: "same-path-other-workspace", Path: "/repo/openspec/changes/2026-07-20-pcie/design.md", Type: TypeDesign, Workspace: "beta"},
		{ID: "stale-only", Path: "/unrelated.md", Type: TypeKnowledge, Workspace: "alpha",
			Frontmatter: map[string]any{derivedTagsKey: []string{"stale"}, inheritedTagsKey: []string{"stale"}}},
	}
	edges := []Edge{
		{From: changeID, To: "tasks", Source: "yaml", Kind: "implements"},
		{From: changeID, To: "plan", Source: "yaml", Kind: "references"},
		{From: changeID, To: "yaml-generates", Source: "yaml", Kind: "generates"},
		{From: changeID, To: "wrong-first-source", Source: "convention-internal", Kind: "implements"},
		{From: changeID, To: "cross-workspace", Source: "yaml", Kind: "implements"},
		{From: "tasks", To: "convention-generates", Source: "convention-internal", Kind: "generates"},
		{From: "tasks", To: "convention-implements", Source: "convention-internal", Kind: "implements"},
		{From: "tasks", To: "superpowers-generates", Source: "superpowers-convention", Kind: "generates"},
		{From: "tasks", To: "superpowers-implements", Source: "superpowers-convention", Kind: "implements"},
		{From: "plan", To: "plan-second-hop", Source: "superpowers-convention", Kind: "implements"},
		{From: "tasks", To: "blocked-kind", Source: "convention-internal", Kind: "references"},
		{From: "tasks", To: "blocked-source", Source: "yaml", Kind: "implements"},
		{From: "superpowers-implements", To: "blocked-depth", Source: "convention-internal", Kind: "generates"},
		{From: "same-change-tasks", To: "blocked-from-directory-member", Source: "convention-internal", Kind: "generates"},
	}
	originalOrder := make([]string, len(components))
	for index := range components {
		originalOrder[index] = components[index].ID
	}

	EnrichComponentTags(components, edges, taxonomy)

	for index, component := range components {
		if component.ID != originalOrder[index] {
			t.Fatalf("component order changed at %d: got %q, want %q", index, component.ID, originalOrder[index])
		}
	}
	if got := components[0].Frontmatter[derivedTagsKey]; !reflect.DeepEqual(got, []string{"pcie"}) {
		t.Fatalf("change derived provenance = %#v; want [pcie]", got)
	}
	if _, exists := components[0].Frontmatter[inheritedTagsKey]; exists {
		t.Fatalf("change retained stale inherited provenance: %#v", components[0].Frontmatter)
	}
	if got := EffectiveComponentTags(components[0], taxonomy); !reflect.DeepEqual(got, []string{"Owner Label", "pcie"}) {
		t.Fatalf("change effective tags = %v; unknown explicit must remain visible on source", got)
	}
	wantInherited := []string{"pcie"}
	for _, index := range []int{1, 2, 3, 7, 8, 9, 10, 11, 15} {
		if got := components[index].Frontmatter[inheritedTagsKey]; !reflect.DeepEqual(got, wantInherited) {
			t.Errorf("%s inherited provenance = %#v; want %#v", components[index].ID, got, wantInherited)
		}
		if got := EffectiveComponentTags(components[index], taxonomy); !reflect.DeepEqual(got, wantInherited) {
			t.Errorf("%s effective tags = %#v; unknown change tag must not propagate", components[index].ID, got)
		}
	}
	for _, index := range []int{4, 5, 6, 12, 13, 14, 16, 17, 18} {
		if got := components[index].Frontmatter[inheritedTagsKey]; got != nil {
			t.Errorf("%s unexpectedly inherited %#v", components[index].ID, got)
		}
	}
	if components[18].Frontmatter != nil {
		t.Fatalf("stale-only component provenance was not cleared: %#v", components[18].Frontmatter)
	}

	once := append([]Component(nil), components...)
	for index := range once {
		once[index].Frontmatter = cloneFrontmatter(components[index].Frontmatter)
	}
	EnrichComponentTags(components, edges, taxonomy)
	if !reflect.DeepEqual(components, once) {
		t.Fatalf("second enrichment changed components:\nfirst: %#v\nsecond: %#v", once, components)
	}
}

func TestEnrichComponentTagsDoesNotMutateSharedSourceFrontmatter(t *testing.T) {
	taxonomy := parseEffectiveTagTaxonomy(t)
	sourceFrontmatter := map[string]any{"tags": []string{"pcie"}, inheritedTagsKey: []string{"stale"}}
	components := []Component{{
		ID:   "/repo/openspec/changes/pcie/.comet.yaml",
		Path: "/repo/openspec/changes/pcie/.comet.yaml", Type: TypeChange, Workspace: "alpha",
		Frontmatter: sourceFrontmatter,
	}}
	EnrichComponentTags(components, nil, taxonomy)
	if got := sourceFrontmatter[inheritedTagsKey]; !reflect.DeepEqual(got, []string{"stale"}) {
		t.Fatalf("source frontmatter was mutated: %#v", sourceFrontmatter)
	}
}

func TestSyntheticTagHelpersAndSameTagInputs(t *testing.T) {
	taxonomy := parseEffectiveTagTaxonomy(t)
	left := Component{
		Path: "/repo/docs/superpowers/plans/2026-07-20-pcie.md", Type: TypePlan,
		Frontmatter: map[string]any{"tags": []string{"PCI-Express"}, derivedTagsKey: []string{"stale"}},
	}
	right := Component{
		Path: "/repo/docs/superpowers/plans/2026-07-20-pcie.md", Type: TypePlan,
		Frontmatter: map[string]any{"tags": []string{"pcie"}, inheritedTagsKey: []string{"other"}},
	}
	if !sameTagInputs(left, right, taxonomy) {
		t.Fatal("canonical-equivalent explicit tags with different provenance should have the same tag inputs")
	}
	right.Path = "/repo/docs/superpowers/plans/2026-07-20-kms.md"
	if sameTagInputs(left, right, taxonomy) {
		t.Fatal("path-derived tag change should not have the same tag inputs")
	}

	equivalentLeft := Component{
		Path: "/same.md", Type: TypeKnowledge,
		Frontmatter: map[string]any{"tags": []string{" Local Label ", "PCI-Express", "local label"}},
	}
	equivalentRight := Component{
		Path: "/same.md", Type: TypeKnowledge,
		Frontmatter: map[string]any{"tags": []string{"pcie", "LOCAL LABEL"}},
	}
	if !sameTagInputs(equivalentLeft, equivalentRight, taxonomy) {
		t.Fatal("explicit reordering, case changes, deduplication, and canonical aliases should compare as one tag set")
	}
	equivalentRight.Frontmatter["tags"] = []string{"pcie", "LOCAL LABEL", "new label"}
	if sameTagInputs(equivalentLeft, equivalentRight, taxonomy) {
		t.Fatal("a real explicit tag-set change should not have the same tag inputs")
	}

	destination := Component{Frontmatter: map[string]any{"title": "kept", derivedTagsKey: []string{"old"}}}
	copySyntheticTags(&destination, left)
	if got := destination.Frontmatter[derivedTagsKey]; !reflect.DeepEqual(got, []string{"stale"}) {
		t.Fatalf("copied derived tags = %#v; want [stale]", got)
	}
	if _, exists := destination.Frontmatter[inheritedTagsKey]; exists {
		t.Fatalf("copy retained absent inherited tags: %#v", destination.Frontmatter)
	}
	stripped := stripSyntheticTags(destination)
	if _, exists := stripped.Frontmatter[derivedTagsKey]; exists {
		t.Fatalf("strip retained derived tags: %#v", stripped.Frontmatter)
	}
	if destination.Frontmatter[derivedTagsKey] == nil {
		t.Fatal("stripSyntheticTags mutated its source component")
	}
}
