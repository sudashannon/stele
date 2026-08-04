package source

import (
	"os"
	"path/filepath"
	"testing"
)

// docsTree builds the shapes measured on a real model-deployment tree: a
// project's own documentation directory, a vendored dependency with its own
// docs/, a build output that fetched another dependency, markdown sitting beside
// source code, and per-module READMEs.
func docsTree(t *testing.T) string {
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
	// Kept: the project's own documents.
	write("deploy-plan.md", "# plan")
	write("README.md", "# project")
	write("docs/benchmark-verdict.md", "# verdict")
	write("docs/results/trt-integration.md", "# trt")
	write("deployment_design/control-design.md", "# design")

	// Dropped: vendored dependency documentation.
	write("3rdparty/llama.cpp/docs/build.md", "# upstream")
	write("mllm/vendors/kleidiai/docs/README.md", "# vendor")
	// Dropped: a build output that fetched a dependency.
	write("build-x86-native/_deps/highway-src/g3doc/faq.md", "# generated")
	// Dropped: markdown documenting the code beside it.
	write("engine/kernel.cpp", "int main() {}")
	write("engine/NOTES.md", "# module notes")
	// Dropped: per-module READMEs below the top level.
	write("tools/pmu/README.md", "# module")

	return root
}

func TestDocsLayoutAcceptsATreeWithItsOwnDocuments(t *testing.T) {
	if !HasDocsLayout(docsTree(t)) {
		t.Fatal("HasDocsLayout = false, want true: the tree holds documents the scanner keeps")
	}
}

func TestDocsLayoutRejectsATreeWhoseOnlyMarkdownIsExcluded(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Every candidate is excluded: vendored, build output, source-adjacent and a
	// nested module README. Accepting this tree would register a workspace that
	// then indexes nothing.
	mk("3rdparty/upstream/docs/guide.md", "# upstream")
	mk("build/_deps/dep/g3doc/faq.md", "# generated")
	mk("engine/kernel.cpp", "int main() {}")
	mk("engine/NOTES.md", "# beside code")
	mk("tools/pmu/README.md", "# module")
	if HasDocsLayout(root) {
		t.Fatal("HasDocsLayout = true, want false: every markdown file here is excluded")
	}
}

func TestDocsKindIsNeverAutoDetected(t *testing.T) {
	root := docsTree(t)
	// "Holds markdown" describes almost every directory, so auto-detection must
	// not claim it; the error names docs as the explicit way in.
	if _, err := ResolveKind(root, ""); err == nil {
		t.Fatal("ResolveKind with no configured type succeeded, want a failure that points at the docs type")
	}
	kind, err := ResolveKind(root, KindDocs)
	if err != nil || kind != KindDocs {
		t.Fatalf("ResolveKind(docs) = %q, %v; want docs and no error", kind, err)
	}
}

func TestDocsRootsAreTheRegisteredTree(t *testing.T) {
	root := docsTree(t)
	ws := Workspace{Alias: "md", Path: root, Type: KindDocs}
	// A docs workspace has no fixed content subdirectory, so both the scan and
	// the watch surface are the tree itself.
	if got := ScanRoots(ws); len(got) != 1 || got[0] != filepath.Clean(root) {
		t.Fatalf("ScanRoots = %v, want [%s]", got, filepath.Clean(root))
	}
	if got := WatchRoots(ws); len(got) != 1 || got[0] != filepath.Clean(root) {
		t.Fatalf("WatchRoots = %v, want [%s]", got, filepath.Clean(root))
	}
	if got := ProjectRoot(ws); got != filepath.Clean(root) {
		t.Fatalf("ProjectRoot = %s, want %s", got, filepath.Clean(root))
	}
	if got := MirrorRoot(ws); got != filepath.Clean(root) {
		t.Fatalf("MirrorRoot = %s, want %s", got, filepath.Clean(root))
	}
}

func TestExcludedDocsDirCoversVendoredAndBuildTrees(t *testing.T) {
	for _, name := range []string{
		"3rdparty", "third_party", "vendor", "vendors", "_deps", "external",
		"build", "build-x86-native", "cmake-build-release", "dist", "out",
	} {
		if !IsExcludedDocsDir(name) {
			t.Errorf("IsExcludedDocsDir(%q) = false, want true", name)
		}
	}
	// Directories that hold real documents must survive.
	for _, name := range []string{"docs", "results", "knowledge", "deployment_design", "reports", "buildsystem"} {
		if IsExcludedDocsDir(name) {
			t.Errorf("IsExcludedDocsDir(%q) = true, want false", name)
		}
	}
}

func TestDirHoldsSourceCodeSeparatesModuleDocsFromDocDirs(t *testing.T) {
	root := t.TempDir()
	codeDir := filepath.Join(root, "engine")
	docDir := filepath.Join(root, "docs")
	for _, d := range []string{codeDir, docDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(codeDir, "kernel.cu"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "verdict.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	codeEntries, err := os.ReadDir(codeDir)
	if err != nil {
		t.Fatal(err)
	}
	if !DirHoldsSourceCode(codeEntries) {
		t.Error("DirHoldsSourceCode = false for a directory holding kernel.cu, want true")
	}
	docEntries, err := os.ReadDir(docDir)
	if err != nil {
		t.Fatal(err)
	}
	if DirHoldsSourceCode(docEntries) {
		t.Error("DirHoldsSourceCode = true for a markdown-only directory, want false")
	}
}

func TestModuleReadmeKeepsTheProjectsOwnOverview(t *testing.T) {
	root := "/repo"
	// The project's README and a top-level directory's README are overviews.
	for _, p := range []string{"/repo/README.md", "/repo/docs/README.md"} {
		if IsModuleReadme(root, p) {
			t.Errorf("IsModuleReadme(%q) = true, want false: this is an overview", p)
		}
	}
	// Deeper down it documents a code module.
	for _, p := range []string{"/repo/tools/pmu/README.md", "/repo/mllm/compile/ir/perf/README.md"} {
		if !IsModuleReadme(root, p) {
			t.Errorf("IsModuleReadme(%q) = false, want true: this is module documentation", p)
		}
	}
}
