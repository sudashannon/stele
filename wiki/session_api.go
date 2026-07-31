package wiki

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HandleSessions returns every indexed agent session, newest activity first.
// Touched-path lists are narrowed to indexed documents so a client can link
// every entry it renders.
func (a *API) HandleSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": a.sessionSummaries()})
}

// HandleSession returns one session by transcript path. It never serves the
// transcript itself: those files reach hundreds of megabytes and contain raw
// tool output.
func (a *API) HandleSession(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	index, workspaces := a.sessionsSnapshot()
	if index == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session layer is not enabled"})
		return
	}
	digest, ok := index.Digest(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	component, ok := SessionComponent(digest, workspaces)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session is outside configured workspaces"})
		return
	}
	a.mu.RLock()
	summary := SessionSummaryOf(digest, component, a.graph.Components())
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, summary)
}

// HandleSessionsRefresh re-reads transcripts on demand and re-grafts them.
func (a *API) HandleSessionsRefresh(w http.ResponseWriter, r *http.Request) {
	index := a.SessionsIndexSnapshot()
	if index == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session layer is not enabled"})
		return
	}
	changed, err := index.Refresh()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	a.ApplySessions()
	writeJSON(w, http.StatusOK, map[string]any{"changed": changed, "sessions": len(index.Digests())})
}

func (a *API) sessionSummaries() []SessionSummary {
	index, workspaces := a.sessionsSnapshot()
	if index == nil {
		return []SessionSummary{}
	}
	digests := index.Digests()
	summaries := make([]SessionSummary, 0, len(digests))
	a.mu.RLock()
	documents := a.graph.Components()
	for _, digest := range digests {
		component, ok := SessionComponent(digest, workspaces)
		if !ok {
			continue
		}
		summaries = append(summaries, SessionSummaryOf(digest, component, documents))
	}
	a.mu.RUnlock()
	return summaries
}

// contextPacket is the recall payload: the documents that answer a query, the
// sessions that worked on those documents, and the agent-memory artifacts the
// runtime consolidated for the same project.
type contextPacket struct {
	Query     string                  `json:"query"`
	Documents []contextDocument       `json:"documents"`
	Sessions  []contextSessionHit     `json:"sessions"`
	Memory    []contextMemoryArtifact `json:"memory,omitempty"`
}

type contextDocument struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Type       string   `json:"type"`
	Workspace  string   `json:"workspace"`
	Similarity float64  `json:"similarity"`
	Tags       []string `json:"tags,omitempty"`
}

type contextSessionHit struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Workspace string   `json:"workspace"`
	UpdatedAt string   `json:"updatedAt"`
	Documents []string `json:"documents"`
	Intents   []string `json:"intents,omitempty"`
}

type contextMemoryArtifact struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Excerpt string `json:"excerpt"`
}

// contextIntentSample caps how much of a session's intent list travels in a
// packet: enough to recognize what the session was doing, not a transcript.
const contextIntentSample = 6

// memoryExcerptBytes bounds how much of an agent-memory artifact is read.
const memoryExcerptBytes = 4096

// BuildContextPacket assembles recall context for a query: ranked documents
// (reusing document semantic search), the sessions whose session→document
// edges point at those documents, and any agent-memory artifact mentioning the
// query. Sessions are ordered by how many matched documents they touched, so
// the session that did the most relevant work comes first.
func (a *API) BuildContextPacket(query string, queryVec []float32, limit int) contextPacket {
	if limit <= 0 {
		limit = 5
	}
	taxonomy := LoadTaxonomy()
	packet := contextPacket{Query: query, Documents: []contextDocument{}, Sessions: []contextSessionHit{}}

	a.mu.RLock()
	components := a.graph.Components()
	ranked := rankSemanticSearch(query, nil, queryVec, components, a.graph.Embeddings(), taxonomy)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	touched := make(map[string][]string)
	sessionComponents := make(map[string]Component)
	for _, hit := range ranked {
		component, ok := components[hit.id]
		if !ok {
			continue
		}
		packet.Documents = append(packet.Documents, contextDocument{
			ID:         component.ID,
			Title:      component.Title,
			Type:       string(component.Type),
			Workspace:  component.Workspace,
			Similarity: hit.score,
			Tags:       EffectiveComponentTags(component, taxonomy),
		})
		// Sessions reach documents through backlinks, which are already
		// indexed, so expansion costs nothing beyond the matched set.
		for _, edge := range a.graph.Backlinks(component.ID) {
			if edge.Source != SourceSession {
				continue
			}
			if _, known := sessionComponents[edge.From]; !known {
				sessionComponent, found := components[edge.From]
				if !found {
					continue
				}
				sessionComponents[edge.From] = sessionComponent
			}
			touched[edge.From] = append(touched[edge.From], component.ID)
		}
	}
	a.mu.RUnlock()

	index := a.SessionsIndexSnapshot()
	for sessionID, documents := range touched {
		component := sessionComponents[sessionID]
		sort.Strings(documents)
		hit := contextSessionHit{
			ID:        sessionID,
			Path:      component.Path,
			Title:     component.Title,
			Workspace: component.Workspace,
			UpdatedAt: component.UpdatedAt.Format("2006-01-02"),
			Documents: documents,
		}
		if index != nil {
			if digest, found := index.Digest(sessionID); found {
				if digest.ID != "" {
					hit.ID = digest.ID
				}
				hit.Intents = digest.Intents
				if len(hit.Intents) > contextIntentSample {
					hit.Intents = hit.Intents[:contextIntentSample]
				}
			}
		}
		packet.Sessions = append(packet.Sessions, hit)
	}
	sort.Slice(packet.Sessions, func(i, j int) bool {
		left, right := packet.Sessions[i], packet.Sessions[j]
		if len(left.Documents) != len(right.Documents) {
			return len(left.Documents) > len(right.Documents)
		}
		if left.UpdatedAt != right.UpdatedAt {
			return left.UpdatedAt > right.UpdatedAt
		}
		return left.Path < right.Path
	})
	if len(packet.Sessions) > limit {
		packet.Sessions = packet.Sessions[:limit]
	}
	packet.Memory = a.memoryArtifacts(query)
	return packet
}

// contextQueryMaxBytes bounds a recall query. The query is embedded through the
// Bun script and then ranked against the whole corpus, so an unbounded string
// is both the most expensive input this API takes and useless as a query.
const contextQueryMaxBytes = 1024

// normalizeContextQuery trims a caller-supplied query and rejects an empty or
// oversized one.
func normalizeContextQuery(raw string) (string, bool) {
	query := strings.TrimSpace(raw)
	if query == "" || len(query) > contextQueryMaxBytes {
		return "", false
	}
	return query, true
}

// HandleContext serves the recall packet over REST.
func (a *API) HandleContext(w http.ResponseWriter, r *http.Request) {
	query, ok := normalizeContextQuery(r.URL.Query().Get("q"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("q is required and must be at most %d bytes", contextQueryMaxBytes),
		})
		return
	}
	limit := 5
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := parsePositiveInt(raw); err == nil {
			limit = parsed
		}
	}
	writeJSON(w, http.StatusOK, a.BuildContextPacket(query, a.embedQuery(query), limit))
}

// embedQuery embeds a query with the same script the offline corpus build uses.
// A failure degrades to lexical-only ranking rather than an error: the packet
// is still useful without vectors.
func (a *API) embedQuery(query string) []float32 {
	vectors, err := ComputeEmbeddings([]Component{{ID: "__query__", Title: query}}, findEmbedScript())
	if err != nil {
		return nil
	}
	return vectors["__query__"]
}

func parsePositiveInt(raw string) (int, error) {
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("value %q is not positive", raw)
	}
	return value, nil
}

// MarkdownContextPacket renders a packet as the compact Markdown an agent can
// act on directly, which is what the MCP tool returns.
func MarkdownContextPacket(packet contextPacket) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Context: %s\n\n", packet.Query)
	if len(packet.Documents) == 0 && len(packet.Sessions) == 0 && len(packet.Memory) == 0 {
		sb.WriteString("No indexed document, session or memory artifact matched.\n")
		return sb.String()
	}
	if len(packet.Documents) > 0 {
		sb.WriteString("## Documents\n\n")
		for _, doc := range packet.Documents {
			fmt.Fprintf(&sb, "- [%s] %s (%s, %.0f%%)\n  %s\n", doc.Type, doc.Title, doc.Workspace, doc.Similarity*100, doc.ID)
			if len(doc.Tags) > 0 {
				fmt.Fprintf(&sb, "  tags: %s\n", strings.Join(doc.Tags, ", "))
			}
		}
		sb.WriteString("\n")
	}
	if len(packet.Sessions) > 0 {
		sb.WriteString("## Sessions that worked on these documents\n\n")
		for _, hit := range packet.Sessions {
			fmt.Fprintf(&sb, "- %s (%s, %s) — %d matched document(s)\n", hit.Title, hit.Workspace, hit.UpdatedAt, len(hit.Documents))
			for _, intent := range hit.Intents {
				fmt.Fprintf(&sb, "  · %s\n", intent)
			}
			fmt.Fprintf(&sb, "  session: %s\n", hit.Path)
		}
		sb.WriteString("\n")
	}
	if len(packet.Memory) > 0 {
		sb.WriteString("## Agent memory\n\n")
		for _, artifact := range packet.Memory {
			fmt.Fprintf(&sb, "- %s (%s)\n  %s\n", artifact.Kind, artifact.Path, artifact.Excerpt)
		}
	}
	return sb.String()
}

// memoryArtifacts returns agent-memory files whose text mentions the query.
// The runtime owns these files and regenerates them wholesale, so they are
// read on demand and never indexed, embedded or mirrored. Reads are bounded to
// the documented artifact names and to memoryExcerptBytes each.
func (a *API) memoryArtifacts(query string) []contextMemoryArtifact {
	a.mu.RLock()
	root := a.memoryDir
	a.mu.RUnlock()
	if root == "" || query == "" {
		return nil
	}
	needle := strings.ToLower(query)
	var out []contextMemoryArtifact
	for _, candidate := range memoryArtifactPaths(root) {
		excerpt, ok := memoryExcerpt(candidate.path, needle)
		if !ok {
			continue
		}
		out = append(out, contextMemoryArtifact{Path: candidate.path, Kind: candidate.kind, Excerpt: excerpt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

type memoryCandidate struct {
	path string
	kind string
}

// memoryArtifactPaths enumerates the documented artifact set: memory_summary.md
// and MEMORY.md per project directory, plus generated skill playbooks. Unknown
// files are ignored so a runtime change cannot make the panel read something
// unbounded.
func memoryArtifactPaths(root string) []memoryCandidate {
	projects, err := readDirNames(root)
	if err != nil {
		return nil
	}
	var candidates []memoryCandidate
	for _, project := range projects {
		base := filepath.Join(root, project)
		candidates = append(candidates,
			memoryCandidate{path: filepath.Join(base, "memory_summary.md"), kind: "summary"},
			memoryCandidate{path: filepath.Join(base, "MEMORY.md"), kind: "memory"},
		)
		skills, err := readDirNames(filepath.Join(base, "skills"))
		if err != nil {
			continue
		}
		for _, skill := range skills {
			candidates = append(candidates, memoryCandidate{
				path: filepath.Join(base, "skills", skill, "SKILL.md"),
				kind: "skill:" + skill,
			})
		}
	}
	return candidates
}

// memoryExcerpt reads at most memoryExcerptBytes from path and reports whether
// the needle appears in them, returning a single-line excerpt around the hit.
func memoryExcerpt(path, needle string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	buffer := make([]byte, memoryExcerptBytes)
	n, err := file.Read(buffer)
	if n <= 0 {
		return "", false
	}
	_ = err
	text := string(buffer[:n])
	lower := strings.ToLower(text)
	index := strings.Index(lower, needle)
	if index < 0 {
		return "", false
	}
	lineStart := strings.LastIndexByte(text[:index], '\n') + 1
	lineEnd := index + len(needle)
	if next := strings.IndexByte(text[lineEnd:], '\n'); next >= 0 {
		lineEnd += next
	} else {
		lineEnd = len(text)
	}
	return strings.TrimSpace(text[lineStart:lineEnd]), true
}

func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
