package wiki

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"log"
	"os"
	"sort"
	"strings"

	"stele/internal/appdir"
	"stele/internal/claims"
)

// Claim vectors live in their own cache, separate from graph embeddings:
// mixing claim vectors into the graph cache would leak them into document
// search, community detection and similarity edges, none of which should see
// them (the same isolation the claims design doc requires).
const (
	claimVectorCacheMagic         = "CLMV"
	claimVectorCacheSchemaVersion = uint16(1)
	// claimEmbedMaxLen bounds the embedded text: claim text is capped at 4096
	// bytes, and 512 keeps batch embedding cheap without losing the gist.
	claimEmbedMaxLen = 512
)

// claimVectorEntry binds a vector to the exact text that produced it.
type claimVectorEntry struct {
	Hash   [sha256.Size]byte
	Vector []float32
}

// ClaimVectorsPath is the on-disk vector cache.
func ClaimVectorsPath() string {
	return appdir.Path("claims-vectors.bin")
}

// claimEmbedText is the text both the vector and its hash are computed from,
// so cache validity checks and recomputation always agree.
func claimEmbedText(c claims.Claim) string {
	t := c.Text
	if len(t) > claimEmbedMaxLen {
		t = t[:claimEmbedMaxLen]
	}
	return t
}

// loadClaimVectors restores cached claim vectors at startup. Entries whose
// text hash no longer matches the current claim are dropped; absent entries
// stay absent (search falls back to substring matching, and the next upsert
// recomputes the vector).
func (a *API) loadClaimVectors(claimTexts map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.claimVectors == nil {
		a.claimVectors = map[string]claimVectorEntry{}
	}
	data, err := os.ReadFile(ClaimVectorsPath())
	if err != nil {
		return // first run: nothing cached
	}
	buf := bytes.NewReader(data)
	var header struct {
		Magic   [4]byte
		Version uint16
		Count   uint32
	}
	if err := binary.Read(buf, binary.LittleEndian, &header); err != nil {
		return
	}
	if string(header.Magic[:]) != claimVectorCacheMagic || header.Version != claimVectorCacheSchemaVersion {
		return
	}
	for i := uint32(0); i < header.Count && i < 100000; i++ {
		var idLen uint32
		if err := binary.Read(buf, binary.LittleEndian, &idLen); err != nil {
			return
		}
		if idLen > 4096 {
			return
		}
		id := make([]byte, idLen)
		if _, err := buf.Read(id); err != nil {
			return
		}
		var hash [sha256.Size]byte
		if err := binary.Read(buf, binary.LittleEndian, &hash); err != nil {
			return
		}
		var n uint32
		if err := binary.Read(buf, binary.LittleEndian, &n); err != nil {
			return
		}
		if n == 0 || n > 10000 {
			return
		}
		vec := make([]float32, n)
		if err := binary.Read(buf, binary.LittleEndian, &vec); err != nil {
			return
		}
		text, ok := claimTexts[string(id)]
		if !ok || textHash(text) != hash {
			continue // unknown claim or text changed since caching
		}
		a.claimVectors[string(id)] = claimVectorEntry{Hash: hash, Vector: vec}
	}
}

// refreshClaimVectors computes and caches vectors for the given claim ids.
// Embedding failures are tolerated: claims remain searchable through the
// substring fallback and the next upsert retries.
func (a *API) refreshClaimVectors(ids []string) {
	if len(ids) == 0 {
		return
	}
	store := a.ClaimsStoreSnapshot()
	if store == nil {
		return
	}
	var comps []Component
	byID := map[string]claims.Claim{}
	for _, id := range ids {
		for _, ws := range a.workspacesSnapshot() {
			if c, ok := store.ByKey(ws.Alias, id); ok {
				byID[id] = c
				comps = append(comps, Component{ID: id, Title: claimEmbedText(c), Path: ""})
				break
			}
		}
	}
	if len(comps) == 0 {
		return
	}
	vectors, err := ComputeEmbeddings(comps, findEmbedScript())
	if err != nil {
		log.Printf("claims: embedding failed (non-fatal): %v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.claimVectors == nil {
		a.claimVectors = map[string]claimVectorEntry{}
	}
	changed := false
	for _, c := range comps {
		vec, ok := vectors[c.ID]
		if !ok || len(vec) == 0 {
			continue
		}
		text := claimEmbedText(byID[c.ID])
		h := textHash(text)
		if entry, ok := a.claimVectors[c.ID]; ok && entry.Hash == h {
			continue // unchanged text: cache hit
		}
		a.claimVectors[c.ID] = claimVectorEntry{Hash: h, Vector: vec}
		changed = true
	}
	if changed {
		if err := a.persistClaimVectorsLocked(); err != nil {
			log.Printf("claims: vector cache write failed (non-fatal): %v", err)
		}
	}
}

// persistClaimVectorsLocked writes the whole vector cache. Caller holds a.mu.
func (a *API) persistClaimVectorsLocked() error {
	if a.claimVectors == nil {
		return nil
	}
	ids := make([]string, 0, len(a.claimVectors))
	for id := range a.claimVectors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var buf bytes.Buffer
	var b [4]byte
	copy(b[:], claimVectorCacheMagic)
	buf.Write(b[:])
	binary.Write(&buf, binary.LittleEndian, claimVectorCacheSchemaVersion)
	binary.Write(&buf, binary.LittleEndian, uint32(len(ids)))
	for _, id := range ids {
		entry := a.claimVectors[id]
		binary.Write(&buf, binary.LittleEndian, uint32(len(id)))
		buf.WriteString(id)
		binary.Write(&buf, binary.LittleEndian, entry.Hash)
		binary.Write(&buf, binary.LittleEndian, uint32(len(entry.Vector)))
		binary.Write(&buf, binary.LittleEndian, entry.Vector)
	}
	dir := appdir.Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".claims-vectors-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, ClaimVectorsPath())
}

// textHash is the cache-key input: exactly the text the vector was made from.
func textHash(text string) [sha256.Size]byte {
	return sha256.Sum256([]byte(text))
}

// claimSearchHit is one scored claim.
type claimSearchHit struct {
	Claim      claims.Claim
	Similarity float64
}

// claimSearch ranks non-retracted claims against a query: cosine over the
// cached vectors with a substring fallback (the same two-stage strategy as
// document search). Workspace and kind filters apply before ranking.
func (a *API) claimSearch(query, workspace, kind string, topK int) []claimSearchHit {
	store := a.ClaimsStoreSnapshot()
	if store == nil {
		return nil
	}
	if topK <= 0 || topK > 20 {
		topK = 5
	}
	var candidates []claims.Claim
	for _, c := range store.List(claims.Filter{Workspace: workspace, Kind: kind}) {
		if c.Status == claims.StatusRetracted {
			continue
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return nil
	}

	var hits []claimSearchHit
	if queryVec, qNorm := a.claimQueryVector(query); queryVec != nil {
		a.mu.RLock()
		for _, c := range candidates {
			entry, ok := a.claimVectors[c.ID]
			if !ok {
				continue
			}
			sim := cosineSim(queryVec, entry.Vector, qNorm, vecNorm(entry.Vector))
			if sim > 0.15 {
				hits = append(hits, claimSearchHit{Claim: c, Similarity: sim})
			}
		}
		a.mu.RUnlock()
	}
	// Lexical fallback: substring match on text and tags.
	if len(hits) == 0 {
		q := strings.ToLower(query)
		for _, c := range candidates {
			if strings.Contains(strings.ToLower(c.Text), q) {
				hits = append(hits, claimSearchHit{Claim: c, Similarity: 0.5})
				continue
			}
			for _, tag := range c.Tags {
				if strings.Contains(strings.ToLower(tag), q) {
					hits = append(hits, claimSearchHit{Claim: c, Similarity: 0.4})
					break
				}
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Similarity != hits[j].Similarity {
			return hits[i].Similarity > hits[j].Similarity
		}
		return hits[i].Claim.ID < hits[j].Claim.ID
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits
}

// claimQueryVector embeds the query with the shared Bun script; a failure
// degrades to nil (lexical path only), never an error.
func (a *API) claimQueryVector(query string) ([]float32, float64) {
	vectors, err := ComputeEmbeddings([]Component{{ID: "__claim_query__", Title: query}}, findEmbedScript())
	if err != nil {
		return nil, 0
	}
	vec, ok := vectors["__claim_query__"]
	if !ok || len(vec) == 0 {
		return nil, 0
	}
	return vec, vecNorm(vec)
}
