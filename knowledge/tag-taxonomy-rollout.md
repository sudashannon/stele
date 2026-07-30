---
title: Controlled tag taxonomy rollout
tags: [comet-panel, taxonomy, wiki]
---

## Goal

Turn free-form document tags into an auditable controlled vocabulary, inherit change-level tags across OpenSpec artifacts, and decide tag-edge eligibility by facet and corpus coverage before considering LLM tagging.

## Confirmed corpus facts

- Live graph: 1453 components. 260 documents currently carry explicit frontmatter tags.
- Coverage is structural: knowledge 252/322 (78.3%); proposal/design/tasks/change 0%; spec/report/plan/artifact about 0-2%.
- 114 OpenSpec change directories contain 570 indexed artifacts. All artifacts in one change share the directory slug; linked Superpowers design/plan/report files use the same slug convention.
- `.comet.yaml` is already the authoritative TypeChange metadata component and already links to declared design/plan/verification artifacts. See `wiki/workspace_source.go` and `wiki/links.go`.
- Normalized explicit vocabulary: 216 tags; 138 appear once, 29 twice, 46 in 3-50 documents, 3 above 50. Fourteen case-only collisions exist.
- User decision: `kmc` and `kms` remain separate canonical tags. They are related security hierarchy concepts and may correctly occur in one community.
- No prior controlled-taxonomy design was found in Comet Wiki search. Existing community design is `docs/superpowers/specs/2026-07-11-wiki-upgrade-design.md`.
- Full rebuild assembles every component before `BuildGraph`; OpenSpec incremental update replaces only changed components. Incremental inheritance must therefore recompute every member of an affected change (and its linked artifacts), or force a full rebuild when `.comet.yaml` changes. See `wiki/index.go` and `wiki/incremental.go`.

## Decisions

1. Facets: product, platform, subsystem, activity, state.
2. Product/activity/state tags are filter-only. Platform/subsystem tags may create edges only when corpus count is at least 3 and no more than 3.5% of documents.
3. Unknown explicit frontmatter tags stay visible/searchable but cannot create tag edges.
4. Canonicalization is case-insensitive and alias-driven. `orin` aliases include Jetson Orin spellings; `kmc` and `kms` do not alias each other.
5. Inheritance is index-time, not file mutation. Seed = TypeChange explicit tags + tags derived from its change-directory slug.
6. Propagation allowlist is origin-sensitive and depth-bounded:
   - All components physically inside the same OpenSpec change directory inherit directly.
   - From TypeChange, follow one outbound `yaml` edge whose kind is `implements` or `references`.
   - From a reached tasks/plan component, follow one additional outbound `convention-internal` or `superpowers-convention` edge whose kind is `generates` or `implements`.
   - Never traverse backlinks, `traces-back`, markdown/vector/slug/task-context/task-json/tag edges, or depth >2.
7. Deterministic derivation only matches controlled vocabulary terms in change slugs and Superpowers artifact filenames. It never invents a term.
8. LLM tagging is deferred until post-rollout coverage and edge-quality measurements.

## Deterministic contracts after plan review

### Effective tags

One accessor owns every consumer (API response, `tag:` filter, ranking, df and edge generation). It emits:

1. Explicit tags in document order: known aliases become lowercase canonical values; unknown explicit values are trimmed and retained.
2. Derived then inherited canonical tags, appended in canonical sort order.
3. Case-insensitive deduplication, with explicit values winning.

Raw parsing is named `ExplicitComponentTags`; it is never used by search/df/edges directly.

### Full/incremental parity

- Any `.comet.yaml` change forces full `Rebuild`.
- Component create/delete/rename forces full rebuild because corpus size, membership and df may change.
- An in-place markdown update may stay incremental only when component type, explicit tags and path-derived tags are unchanged; otherwise it rebuilds.
- Taxonomy override changes take effect on service restart/rebuild; they are not an incremental watcher input.

### Sparse tag-edge contract

- Never materialize a tag clique.
- For each eligible tag, sort member IDs and connect them as one cycle (`m` edges for `m>=3`).
- Deduplicate pairs by strongest weight, then global greedy selection by weight descending and `(from,to)` ascending while both endpoints have tag-degree `<6`.
- `Edge.Weight` is optional; tag edges use `Source=\"tag\"` and `Kind=\"shares-tag:<canonical>\"`.
- `w = 0.20 + 0.20*x`, where `x` is normalized IDF over the eligible `[minDocs,maxDocs]` interval. Therefore `w∈[0.20,0.40]`: never stronger than the existing vector maximum and always below authored links.

## Implementation phases

### Phase A — vocabulary core

- Add embedded `wiki/taxonomy.yaml` with optional `~/.comet-panel/taxonomy.yaml` override.
- Implement parse, alias canonicalization, facet lookup, deterministic slug matching, and edge eligibility.
- Unit-test duplicate alias rejection, case folding, phrase precedence, `kmc`/`kms` separation, and coverage thresholds.

Status: implemented in `wiki/taxonomy.go`, `wiki/taxonomy.yaml`, and `wiki/taxonomy_test.go`. Focused Phase A tests passed after `gofmt`.

### Phase B — inheritance and API integration

- Separate explicit tag parsing from effective tags.
- Enrich components during full and incremental indexing using change membership and graph links.
- Return canonical effective tags in semantic search and UI; preserve unknown explicit tags.
- Add provenance in frontmatter (`_derivedTags` / `_inheritedTags`) rather than overwriting user tags.

### Phase C — coverage pruning and tag edges

- Compute effective tag document frequencies after enrichment.
- Only platform/subsystem vocabulary tags in the configured coverage band may create weighted tag edges.
- IDF-weight qualified tag edges; do not persist broad/rare tag edges.
- Re-run community metrics and spot-check clusters before accepting any tag edge.

### Validation

- Focused Go tests per contract.
- Live corpus before/after coverage distribution and qualified edge counts.
- `go test ./...`, `go vet ./...`, frontend suite/build.
- Deploy and verify effective tags and `tag:` filtering in the browser.

## Current implementation

Phase A is implemented and focused tests pass. Phase B/C are not yet integrated. No git writes performed.

## Live-corpus dry-run (no build)

- Vocabulary: 107 canonical tags across five facets.
- Explicitly tagged: 260/1453 documents.
- Deterministic derived/inherited effective tags: 1158/1453 (79.7%).
- Edge-qualified tags: 57; `maxDocs=floor(1453*0.035)=50`.
- Sparse cycle candidates after pair deduplication: 576.
- Retained after endpoint degree cap 6: 575 edges over 516 nodes; max tag degree 6.
- Added edge count is 10.5% of the existing 5493 edges, rather than up to 1225 edges for one df=50 clique.
- Weight range observed: 0.218 for df=39 through 0.400 for df=3.
- Non-canonical slug-alias hits were manually enumerated. All observed hits matched their intended concepts; the future-ambiguous `wan -> wan22` alias was removed.
