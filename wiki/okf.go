package wiki

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"stele/internal/claims"
)

// okfVersion is the bundle version declared in the mirror's root index.md.
// The format is Google's Open Knowledge Format (OKF), v0.2: vendor-neutral
// markdown concepts with YAML frontmatter, portable to any OKF-aware
// consumer. Conformance essentials (spec: okf/SPEC.md): parseable
// frontmatter in every non-reserved .md file and a non-empty `type` in
// every concept. Everything else is recommended, not required.
const okfVersion = "0.2"

// okfGenerator names this wiki in the `generated.by` provenance field.
const okfGenerator = "stele-wiki"

// OKFProjector projects OKF v0.2 frontmatter onto mirrored documents. It is
// optional on the Mirror: a mirror without a projector keeps copying source
// files verbatim (the previous behavior).
type OKFProjector struct {
	claims *claims.Store // nil when the claims layer is not wired
}

// NewOKFProjector builds a projector. store may be nil: sources then stay
// empty and only the structural fields (type, generated) are projected.
func NewOKFProjector(store *claims.Store) *OKFProjector {
	return &OKFProjector{claims: store}
}

// okfSource is one entry of the frontmatter `sources` list: a provenance
// signal projected from a claim's versioned evidence.
type okfSource struct {
	ID       string `yaml:"id"`
	Resource string `yaml:"resource"`
	Title    string `yaml:"title"`
}

// Project transforms one raw markdown document into an OKF v0.2 concept:
// parseable frontmatter carrying a non-empty `type`, generation metadata,
// and — when claims attach to the document — its evidence as `sources` plus
// the lifecycle fields `status`/`verified`. The body is never modified; only
// the frontmatter is created or merged. ctype is the wiki component type
// (used when the document carries no `type` of its own); docID is the
// component ID claims attach to (the absolute source path).
func (p *OKFProjector) Project(raw, ctype, docID string) string {
	fmText, body := splitFrontmatter(raw)
	fm := map[string]any{}
	if fmText != "" {
		if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
			// Broken source frontmatter cannot be merged. Prepend a minimal
			// valid block so the concept still conforms; the broken text
			// stays visible in the body rather than being silently dropped.
			fm = map[string]any{}
			body = fmText + "\n" + body
		}
	}

	// `type` is the only required field: keep an author-supplied one, else
	// project the wiki's classification, else a safe generic.
	if t, _ := fm["type"].(string); t == "" {
		if ctype == "" {
			ctype = "document"
		}
		fm["type"] = ctype
	}
	fm["generated"] = map[string]any{
		"by": okfGenerator,
		"at": time.Now().UTC().Format(time.RFC3339),
	}

	if p.claims != nil && docID != "" {
		sources, stale, verified := p.projectClaims(docID)
		if len(sources) > 0 {
			fm["sources"] = sources
		}
		if stale {
			fm["status"] = "stale"
		} else if verified {
			fm["verified"] = true
		}
	}

	out, err := yaml.Marshal(fm)
	if err != nil {
		return raw // projection must never destroy the document
	}
	return "---\n" + string(out) + "---\n" + body
}

// projectClaims maps the claims attached to one document onto OKF v0.2
// provenance. Returns the sources list, whether any attached claim is
// stale, and whether the document is backed by at least one non-retracted,
// non-stale claim (the `verified` signal).
func (p *OKFProjector) projectClaims(docID string) (sources []okfSource, stale, verified bool) {
	for _, c := range p.claims.List(claims.Filter{}) {
		if c.DocID != docID || c.Status == claims.StatusRetracted {
			continue
		}
		if c.Status == claims.StatusStale {
			stale = true
		} else {
			verified = true
		}
		text := c.Text
		// Rune-safe truncation: a byte cut inside a multibyte character would
		// emit invalid UTF-8, which yaml.v3 serializes as !!binary.
		if r := []rune(text); len(r) > 80 {
			text = string(r[:80])
		}
		for _, ev := range c.Evidence {
			sources = append(sources, okfSource{ID: c.ID, Resource: ev.Resource, Title: text})
		}
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].ID != sources[j].ID {
			return sources[i].ID < sources[j].ID
		}
		return sources[i].Resource < sources[j].Resource
	})
	return sources, stale, verified
}

// OKFIndex renders the mirror's root index.md: the reserved bundle entry
// that declares the target OKF version and lists the concepts.
func OKFIndex(concepts []string) string {
	var sb strings.Builder
	sb.WriteString("---\nokf_version: \"" + okfVersion + "\"\n---\n\n# Index\n\n")
	for _, c := range concepts {
		fmt.Fprintf(&sb, "- %s\n", c)
	}
	return sb.String()
}

// OKFLogLine renders one update-history line for the reserved log.md.
func OKFLogLine(when time.Time, msg string) string {
	return fmt.Sprintf("- %s %s\n", when.UTC().Format(time.RFC3339), msg)
}

// writeOKFDocument mirrors one markdown source with OKF projection. A
// missing source (deleted between event and flush) is a no-op, matching
// copyFile. The document ID claims attach to is the absolute source path.
func writeOKFDocument(p *OKFProjector, src, dest, ctype string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	return os.WriteFile(dest, []byte(p.Project(string(raw), ctype, src)), 0644)
}

// writeOKFBundleMeta writes the reserved bundle entry files: index.md
// (declares the OKF version and lists every concept) and log.md (append-
// only update history). They are written before the git add so they ship
// in the same commit as the concepts.
func (m *Mirror) writeOKFBundleMeta(when time.Time) {
	var concepts []string
	filepath.Walk(m.repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasPrefix(path, filepath.Join(m.repoDir, ".git")) {
			return nil
		}
		base := filepath.Base(path)
		if base == "index.md" || base == "log.md" {
			return nil
		}
		if !strings.HasSuffix(base, ".md") {
			return nil
		}
		if rel, err := filepath.Rel(m.repoDir, path); err == nil {
			concepts = append(concepts, rel)
		}
		return nil
	})
	sort.Strings(concepts)
	if err := os.WriteFile(filepath.Join(m.repoDir, "index.md"), []byte(OKFIndex(concepts)), 0644); err != nil {
		log.Printf("mirror: write index.md failed: %v", err)
		return
	}
	logPath := filepath.Join(m.repoDir, "log.md")
	existing, _ := os.ReadFile(logPath)
	var sb strings.Builder
	if len(existing) == 0 {
		sb.WriteString("# Log\n\n")
	} else {
		sb.Write(existing)
	}
	sb.WriteString(OKFLogLine(when, "bundle updated"))
	if err := os.WriteFile(logPath, []byte(sb.String()), 0644); err != nil {
		log.Printf("mirror: write log.md failed: %v", err)
	}
}
