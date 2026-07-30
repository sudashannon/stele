package wiki

type Graph struct {
	components       map[string]Component
	forward          map[string][]Edge
	backward         map[string][]Edge
	communities      map[string]int
	communityLabels  map[int]string
	embeddings       map[string][]float32
	embeddingEntries map[string]EmbeddingEntry
	failedWorkspaces []string
}

func BuildGraph(components []Component, edges []Edge) *Graph {
	g := &Graph{
		components:       make(map[string]Component, len(components)),
		forward:          make(map[string][]Edge),
		backward:         make(map[string][]Edge),
		embeddingEntries: make(map[string]EmbeddingEntry),
	}
	for _, c := range components {
		g.components[c.ID] = c
	}
	for _, e := range edges {
		g.forward[e.From] = append(g.forward[e.From], e)
		g.backward[e.To] = append(g.backward[e.To], e)
	}
	return g
}

// AddComponent inserts or replaces a component by its stable ID.
func (g *Graph) AddComponent(c Component) {
	if g.components == nil {
		g.components = make(map[string]Component)
	}
	g.components[c.ID] = c
}

// RemoveComponent removes a component and all graph state keyed by its ID.
func (g *Graph) RemoveComponent(id string) {
	g.RemoveEdgesFrom(id)
	g.RemoveEdgesTo(id)
	delete(g.components, id)
	delete(g.communities, id)
	g.RemoveEmbedding(id)
}

// AddEdges adds edges to both the forward and backlink indexes.
func (g *Graph) AddEdges(edges []Edge) {
	if g.forward == nil {
		g.forward = make(map[string][]Edge)
	}
	if g.backward == nil {
		g.backward = make(map[string][]Edge)
	}
	for _, e := range edges {
		g.forward[e.From] = append(g.forward[e.From], e)
		g.backward[e.To] = append(g.backward[e.To], e)
	}
}

// RemoveEdgesFrom removes every outgoing edge from id and its backlink entry.
func (g *Graph) RemoveEdgesFrom(id string) {
	edges := g.forward[id]
	delete(g.forward, id)
	for _, removed := range edges {
		backlinks := g.backward[removed.To]
		filtered := backlinks[:0]
		for _, edge := range backlinks {
			if !sameEdge(edge, removed) {
				filtered = append(filtered, edge)
			}
		}
		if len(filtered) == 0 {
			delete(g.backward, removed.To)
		} else {
			g.backward[removed.To] = filtered
		}
	}
}

// RemoveEdgesTo removes every incoming edge to id and its forward entry.
func (g *Graph) RemoveEdgesTo(id string) {
	edges := g.backward[id]
	delete(g.backward, id)
	for _, removed := range edges {
		forward := g.forward[removed.From]
		filtered := forward[:0]
		for _, edge := range forward {
			if !sameEdge(edge, removed) {
				filtered = append(filtered, edge)
			}
		}
		if len(filtered) == 0 {
			delete(g.forward, removed.From)
		} else {
			g.forward[removed.From] = filtered
		}
	}
}

type edgeIdentity struct {
	from   string
	to     string
	kind   string
	source string
}

func identityOfEdge(edge Edge) edgeIdentity {
	return edgeIdentity{from: edge.From, to: edge.To, kind: edge.Kind, source: edge.Source}
}

func sameEdge(a, b Edge) bool {
	return identityOfEdge(a) == identityOfEdge(b)
}

// UpdateEmbedding inserts a vector without cache-validity metadata. Durable
// index paths should use UpdateEmbeddingEntry instead.
func (g *Graph) UpdateEmbedding(id string, vec []float32) {
	if g.embeddings == nil {
		g.embeddings = make(map[string][]float32)
	}
	g.embeddings[id] = vec
	delete(g.embeddingEntries, id)
}

func (g *Graph) UpdateEmbeddingEntry(entry EmbeddingEntry) {
	if g.embeddings == nil {
		g.embeddings = make(map[string][]float32)
	}
	if g.embeddingEntries == nil {
		g.embeddingEntries = make(map[string]EmbeddingEntry)
	}
	g.embeddings[entry.ID] = entry.Vector
	g.embeddingEntries[entry.ID] = entry
}

// RemoveEmbedding removes the cached vector and its validity metadata.
func (g *Graph) RemoveEmbedding(id string) {
	delete(g.embeddings, id)
	delete(g.embeddingEntries, id)
}

func (g *Graph) Component(id string) (Component, bool) {
	c, ok := g.components[id]
	return c, ok
}

func (g *Graph) Components() map[string]Component {
	return g.components
}

func (g *Graph) Communities() map[string]int {
	return g.communities
}

func (g *Graph) SetCommunities(c map[string]int) {
	g.communities = c
}

func (g *Graph) CommunityLabels() map[int]string {
	return g.communityLabels
}

func (g *Graph) SetCommunityLabels(l map[int]string) {
	g.communityLabels = l
}

func (g *Graph) Embeddings() map[string][]float32 {
	return g.embeddings
}

func (g *Graph) SetEmbeddings(e map[string][]float32) {
	g.embeddings = e
	g.embeddingEntries = make(map[string]EmbeddingEntry)
}

func (g *Graph) EmbeddingEntries() map[string]EmbeddingEntry {
	return g.embeddingEntries
}

func (g *Graph) SetEmbeddingEntries(entries map[string]EmbeddingEntry) {
	g.embeddingEntries = entries
	g.embeddings = EmbeddingVectors(entries)
}

func (g *Graph) FailedWorkspaces() []string {
	return g.failedWorkspaces
}

func (g *Graph) SetFailedWorkspaces(aliases []string) {
	g.failedWorkspaces = aliases
}

func (g *Graph) Forward(id string) []Edge {
	return g.forward[id]
}

func (g *Graph) Backlinks(id string) []Edge {
	return g.backward[id]
}
