package wiki

import (
	"strings"
	"testing"
)

func TestExtractSemanticTextPreservesSectionsAndIgnoresExampleChecklists(t *testing.T) {
	content := []byte(`---
example: "- [ ] not a task"
---
# Cache Design

Opening paragraph.

Second paragraph is intentionally omitted.

## Decision

Use content hashes for invalidation.

Another paragraph is omitted.

~~~text
- [ ] example checkbox
~~~

## Tasks

- [x] Cache schema implemented
- [ ] Run migration
`)
	first := ExtractSemanticText("Cache Design", content, 4096)
	second := ExtractSemanticText("Cache Design", content, 4096)
	if first.Text != second.Text {
		t.Fatal("semantic extraction is not deterministic")
	}
	for _, expected := range []string{"# Cache Design", "Opening paragraph.", "## Decision", "Use content hashes", "## Tasks"} {
		if !strings.Contains(first.Text, expected) {
			t.Fatalf("semantic text missing %q: %s", expected, first.Text)
		}
	}
	for _, omitted := range []string{"Second paragraph", "Another paragraph", "example checkbox", "example: "} {
		if strings.Contains(first.Text, omitted) {
			t.Fatalf("semantic text unexpectedly contains %q: %s", omitted, first.Text)
		}
	}
	if first.ChecklistDone != 1 || first.ChecklistTotal != 2 {
		t.Fatalf("checklist=%d/%d, want 1/2", first.ChecklistDone, first.ChecklistTotal)
	}
	if len(first.Headings) != 3 || len(first.KeyParagraphs) != 3 {
		t.Fatalf("headings=%+v paragraphs=%+v", first.Headings, first.KeyParagraphs)
	}
}
