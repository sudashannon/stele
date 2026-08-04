package wiki

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// docsScanTree mirrors the shapes measured on a real model-deployment tree.
func docsScanTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy-plan.md", "# X5 deploy plan")
	write("README.md", "# project overview")
	write("docs/benchmark-verdict.md", "# verdict")
	write("docs/results/trt-integration.md", "# TRT integration")
	write("deployment_design/control-design.md", "# control design")

	write("3rdparty/llama.cpp/docs/build.md", "# upstream build")
	write("mllm/vendors/kleidiai/docs/README.md", "# vendor readme")
	write("build-x86-native/_deps/highway-src/g3doc/faq.md", "# generated")
	write("engine/kernel.cpp", "int main() {}")
	write("engine/NOTES.md", "# notes beside code")
	write("tools/pmu/README.md", "# module readme")
	return root
}

func scannedNames(t *testing.T, components []Component, root string) []string {
	t.Helper()
	names := make([]string, 0, len(components))
	for _, c := range components {
		rel, err := filepath.Rel(root, c.Path)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, rel)
	}
	sort.Strings(names)
	return names
}

// The defect this whole kind exists for: a documentation repository with no
// workflow layout indexed almost nothing, because unclassified markdown needs a
// `wiki: true` opt-in that nobody writes. Measured on one real tree: 48 of 480
// files classified, 0 opted in.
func TestScanDocsComponentsIndexesUnclassifiedMarkdownAsKnowledge(t *testing.T) {
	root := docsScanTree(t)

	plain, err := ScanComponents(root, "md")
	if err != nil {
		t.Fatal(err)
	}
	docs, err := ScanDocsComponents(root, "md")
	if err != nil {
		t.Fatal(err)
	}

	// The workflow scanner sees nothing here: no directory name in this tree is
	// one it classifies, and no file carries the `wiki: true` opt-in. That is the
	// defect this kind exists for - a whole documentation repository indexing to
	// zero components.
	if got := scannedNames(t, plain, root); len(got) != 0 {
		t.Fatalf("ScanComponents = %v, want nothing: no path here classifies and none opts in", got)
	}

	want := []string{
		"README.md",
		"deploy-plan.md",
		filepath.Join("deployment_design", "control-design.md"),
		filepath.Join("docs", "benchmark-verdict.md"),
		filepath.Join("docs", "results", "trt-integration.md"),
	}
	got := scannedNames(t, docs, root)
	if len(got) != len(want) {
		t.Fatalf("ScanDocsComponents = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ScanDocsComponents = %v, want %v", got, want)
		}
	}
}

func TestScanDocsComponentsExcludesVendoredBuildAndModuleDocs(t *testing.T) {
	root := docsScanTree(t)
	docs, err := ScanDocsComponents(root, "md")
	if err != nil {
		t.Fatal(err)
	}
	// Each of these was reachable and would have been indexed without its rule.
	forbidden := map[string]string{
		filepath.Join("3rdparty", "llama.cpp", "docs", "build.md"):                   "vendored dependency documentation",
		filepath.Join("mllm", "vendors", "kleidiai", "docs", "README.md"):            "vendored dependency documentation",
		filepath.Join("build-x86-native", "_deps", "highway-src", "g3doc", "faq.md"): "build output",
		filepath.Join("engine", "NOTES.md"):                                          "markdown beside source code",
		filepath.Join("tools", "pmu", "README.md"):                                   "per-module README",
	}
	for _, name := range scannedNames(t, docs, root) {
		if reason, bad := forbidden[name]; bad {
			t.Errorf("indexed %s, want it excluded as %s", name, reason)
		}
	}
}

func TestScanDocsComponentsTypesUnclassifiedFilesAsKnowledge(t *testing.T) {
	root := docsScanTree(t)
	docs, err := ScanDocsComponents(root, "md")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range docs {
		if filepath.Base(c.Path) != "deploy-plan.md" {
			continue
		}
		if c.Type != TypeKnowledge {
			t.Fatalf("deploy-plan.md type = %q, want %q", c.Type, TypeKnowledge)
		}
		return
	}
	t.Fatal("deploy-plan.md was not indexed")
}
