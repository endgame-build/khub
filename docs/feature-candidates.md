# Feature Candidates from Related-Work Review

**Status:** index. Every candidate became a GitHub issue on 2026-09-07 (`→ #n`
below); the issue body carries the candidate's full text, the reinforcements from
later reviews, and the triage notes. Priority, size and order live on the
[khub project board](https://github.com/endgame-build/khub/issues). Numbering is stable; a candidate that was
shipped, dropped or folded says so in place of a link.

Compiled from comparative reviews (2026-08 and 2026-09) of the external
projects below against khub's design memo and code:

| Source tag | Project | One-line characterization |
|---|---|---|
| **[bwrb]** | [3mdistal/bwrb](https://github.com/3mdistal/bwrb) (Bowerbird) | "The type system for your notes" — schema-validated Markdown vault CLI built to sit under AI agents; deterministic, no LLM, no database. khub's closest independent sibling (PKM domain). Strongest on schema migration, audit/fix gating, and agent output contracts. |
| **[oo]** | [fabio-rovai/open-ontologies](https://github.com/fabio-rovai/open-ontologies) | Rust MCP server + CLI for OWL/RDF ontology engineering; "server provides validation and scaffolding, the connected LLM does the intelligence." Strongest on schema-*evolution* governance (Terraform-style plan/lock/apply/drift) and ontology QA (lint, align, competency questions). |
| **[sem]** | [semantica-agi/semantica](https://github.com/semantica-agi/semantica) | Extraction-first knowledge-graph pipeline ("open-source Palantir for agents"): ingest → NER/relation extraction → conflict detection → KG → provenance/decision intelligence. khub's thesis inverted (bottom-up extraction into a DB vs. top-down authoring in git); useful mainly as ingestion-path and temporal-query inspiration. |
| **[mds]** | [jackchuka/mdschema](https://github.com/jackchuka/mdschema) | Go CLI validating Markdown *document structure* against a YAML schema (headings, code blocks, tables, link integrity, basic frontmatter typing). Rejected as an engine — no relations, no graph, second schema dialect — but its rule vocabulary informs body content assertions (#49). |
| **[og]** | [NinePts/OntoGraph](https://github.com/NinePts/OntoGraph) | Java/Spring Boot service graphing OWL ontologies to GraphML in four notations (custom, Graffoo, VOWL, UML), via Stardog and hand layout in yEd. **Dead since Jan 2019 and unbuildable** — rejected as a tool, a port, and a dependency. Contributes one visualization idea (#58), a peer-reviewed citation for khub's "domain experts don't speak OWL" premise, and a caveated OWL test corpus. Full review: [`ontograph-review.md`](ontograph-review.md). |
| **[iwe]** | [iwe-org/iwe](https://github.com/iwe-org/iwe) | Rust markdown knowledge graph with CLI + LSP + MCP over one core library. khub's closest independent sibling on architecture: markdown-in-git as truth, derived in-memory graph, schema as machine-checked policy, agent as first-class writer. Validates documents in isolation (no referential integrity, untyped edges), so it is no threat to the graph layer — but it is ahead on agent write-safety, body-shape validation, and context assembly. Full review: [`iwe-comparison.md`](iwe-comparison.md). |
| **[bm]** | [basicmachines-co/basic-memory](https://github.com/basicmachines-co/basic-memory) | Python MCP-first personal AI memory on khub's exact substrate (markdown + frontmatter as truth, derived graph, agent + human as symmetric writers) with the opposite position on every axis above it: schema inferred and advisory, relations as freeform body wikilinks, persisted synced index, commercial cloud/teams layer. No new candidates; reinforces #13, #53, #57, #59. Full review: [`basicmemory-comparison.md`](basicmemory-comparison.md). |
| **[kag]** | [OpenSPG/KAG](https://github.com/OpenSPG/KAG) | LLM+KG question-answering framework from Ant Group (paper arXiv:2409.13731): builds a mutual-indexed, schema-constrained knowledge graph from documents, answers via a logical-form solver over graph + text. khub's thesis met from the opposite direction — it *reconstructs* the structure khub *authors* — over the stack khub rejects (server, graph store, vector store, models in the loop). Rejected as runtime and dependency; contributes the ingestion alignment pass (#59), the retrieval eval tier (#60), and graph-shaped search hits (#61). Full review: [`kag-review.md`](kag-review.md). |
| **[okfm]** | [okf-memory/okf-agent-memory](https://github.com/okf-memory/okf-agent-memory) | Go, stdlib-only implementation of Google's OKF v0.2 (Markdown + YAML frontmatter, body links as the graph, `index.md`/`log.md`, `generated`/`verified`/`sources`/`status`/`stale_after`) with a stdio MCP server and a bootstrap that writes `AGENTS.md` plus a skill. Two days old at review, 431 stars. Untyped where khub is typed; a hand-rolled YAML parser and re-serialize writes where khub splices; a prefix TF-IDF labelled BM25 where khub calls FTS5. Ahead on the OKF v0.2 provenance vocabulary, first-run cohesion, and forgiving keyword search. Contributes #62–#69. Full review: [`okf-comparison.md`](okf-comparison.md). |

Full comparative analysis lives in the review session, except for **[iwe]**,
**[og]**, **[kag]**, **[bm]**, and **[okfm]**, which have written reviews at
[`iwe-comparison.md`](iwe-comparison.md),
[`ontograph-review.md`](ontograph-review.md),
[`kag-review.md`](kag-review.md),
[`basicmemory-comparison.md`](basicmemory-comparison.md), and
[`okf-comparison.md`](okf-comparison.md); this file records
only the actionable candidates. Effort and priority are fields on the project board.

---

## A. Schema evolution & migration

The largest named gap: today, editing a schema layer (or drifting from a preset)
has zero guardrails. bwrb ships a working same-substrate migration engine;
open-ontologies contributes the blast-radius/lock framing.

1. **Schema snapshot + `khub schema diff`** [bwrb, M] → [#74](https://github.com/endgame-build/khub/issues/74)
2. **Change classification** [bwrb, M] → [#75](https://github.com/endgame-build/khub/issues/75)
3. **`khub schema migrate`** [bwrb, L] → [#76](https://github.com/endgame-build/khub/issues/76)
4. **Blast radius + risk on schema changes** [oo, M] — folded into [#76](https://github.com/endgame-build/khub/issues/76)
5. **Locked types/predicates** [oo, S] → [#77](https://github.com/endgame-build/khub/issues/77)
6. **`diff-preset` mechanics** [oo+bwrb, M] → [#78](https://github.com/endgame-build/khub/issues/78)
7. **`khub rename`, data-preserving** [bwrb+oo, M] → [#79](https://github.com/endgame-build/khub/issues/79)
8. **`khub layout migrate`** [bwrb-inspired, M] → [#80](https://github.com/endgame-build/khub/issues/80)

## B. Schema authoring & vocabulary

9. **Field bundles (traits)** [bwrb, M] → [#81](https://github.com/endgame-build/khub/issues/81)
10. **Competency questions as preset acceptance tests** [sem+oo, S–M] → [#82](https://github.com/endgame-build/khub/issues/82)
11. **`khub schema docs`** [sem+oo, S] → [#83](https://github.com/endgame-build/khub/issues/83)
12. **JSON Schema for the `.khub/` layer files** [bwrb, S] → [#84](https://github.com/endgame-build/khub/issues/84)
13. **`khub schema discover`** [bwrb, M] → [#85](https://github.com/endgame-build/khub/issues/85)
14. **Owned/colocated child entities** [bwrb, L] → [#86](https://github.com/endgame-build/khub/issues/86)
15. **Aliases as identity surface** [bwrb, S–M] → [#87](https://github.com/endgame-build/khub/issues/87)
16. **Schema-declared body sections** [bwrb] — dropped — shipped as body templates
17. **Type-level retention policy** [bwrb, S] → [#88](https://github.com/endgame-build/khub/issues/88)
18. **`--under <node>` scope filter** [bwrb, S] → [#89](https://github.com/endgame-build/khub/issues/89)

## C. Authoring & write path

19. **Richer templates** [bwrb, M] → [#90](https://github.com/endgame-build/khub/issues/90)
20. **Fork/lineage provenance in core** [bwrb, M] → [#91](https://github.com/endgame-build/khub/issues/91)
21. **`khub bulk`** [bwrb, M] → [#92](https://github.com/endgame-build/khub/issues/92)
22. **Transition guards + `khub explain`** [bwrb, L] → [#93](https://github.com/endgame-build/khub/issues/93)
23. **Transition effects** [bwrb, M; needs #22] → [#94](https://github.com/endgame-build/khub/issues/94)
24. **Spawn-on-transition (recurrence)** [bwrb, M; needs #22] → [#95](https://github.com/endgame-build/khub/issues/95)
25. **Ingestion conflict policy** [sem, S design note now] → [#96](https://github.com/endgame-build/khub/issues/96)
26. **Extraction scaffold + conformance validation** [oo, M] → [#97](https://github.com/endgame-build/khub/issues/97)

## D. Query & retrieval

27. **Saved queries** [bwrb, S] → [#98](https://github.com/endgame-build/khub/issues/98)
28. **`--where` expression filters** [bwrb, M] → [#99](https://github.com/endgame-build/khub/issues/99)
29. **Derived fields** [bwrb, M] → [#100](https://github.com/endgame-build/khub/issues/100)
30. **Point-in-time reads: `--at <ref|date>`** [sem, S–M] → [#101](https://github.com/endgame-build/khub/issues/101)
31. **Type-scoped search** [sem, S] — shipped — `search --type`
32. **`khub recent`** [bwrb, S] → [#102](https://github.com/endgame-build/khub/issues/102)
33. **Valid-time on edges** [sem, deferred] → [#103](https://github.com/endgame-build/khub/issues/103)

## E. Integrity, audit & fix

34. **`check --fix` with gated autofix** [bwrb, M] → [#104](https://github.com/endgame-build/khub/issues/104)
35. **Check-finding suppression allowlist** [oo, S] → [#105](https://github.com/endgame-build/khub/issues/105)
36. **Body-mention link suggestions** [bwrb, M] → [#106](https://github.com/endgame-build/khub/issues/106)
37. **Custom check rules per preset** [oo, M; needs #28] → [#107](https://github.com/endgame-build/khub/issues/107)
38. **`khub doctor`** [sem, S] → [#108](https://github.com/endgame-build/khub/issues/108)

## F. Agent surface & contracts

39. **Formal JSON output + exit-code contract** [bwrb, S] → [#109](https://github.com/endgame-build/khub/issues/109)
40. **`--receipt` mode** [bwrb, S] → [#110](https://github.com/endgame-build/khub/issues/110)
41. **Skill rewrite as decision guidance** [oo+bwrb, S] → [#111](https://github.com/endgame-build/khub/issues/111)
42. **MCP tool-exposure filter** [oo, S; with the planned MCP server] → [#112](https://github.com/endgame-build/khub/issues/112)
43. **Shell completion** [bwrb, S] → [#113](https://github.com/endgame-build/khub/issues/113)

## G. Interop & projections — status: **agreed** (this review)

44. **`docs/rdf-mapping.md`** [oo] → [#114](https://github.com/endgame-build/khub/issues/114)
45. **RDF projection into `.khub/generated/schema.ttl`** [oo] → [#115](https://github.com/endgame-build/khub/issues/115)
46. **Profile-checked RDF import** [oo, phase 2] → [#116](https://github.com/endgame-build/khub/issues/116)

## H. Engineering hygiene (internal)

47. **Determinism audit** [oo, S] → [#117](https://github.com/endgame-build/khub/issues/117)
48. **Docs canonicality policy** [bwrb, S] → [#118](https://github.com/endgame-build/khub/issues/118)

## Addenda (post-initial-list)

49. **Body content assertions + body link integrity** [mds+bwrb, S–M] → [#119](https://github.com/endgame-build/khub/issues/119)

## I. Agent write-safety and retrieval [iwe]

From the IWE review ([`iwe-comparison.md`](iwe-comparison.md)). The theme: khub
leads on the graph contract and trails on what happens *around* a write — how an
agent proves it knows what it is about to change, what it learns when the write
lands, and how it assembles context in one call instead of five.

50. **`expect` guards + surface-level strictness** [iwe, M] → [#120](https://github.com/endgame-build/khub/issues/120)
51. **Validate-all-then-write atomicity** [iwe, M; pairs with #21, #34] → [#121](https://github.com/endgame-build/khub/issues/121)
52. **Session-scoped integrity warnings on write results** [iwe, S] → [#122](https://github.com/endgame-build/khub/issues/122)
53. **`khub retrieve` — one-call context assembly** [iwe, M] → [#123](https://github.com/endgame-build/khub/issues/123)
54. **Near-duplicate detection** [iwe, M] → [#124](https://github.com/endgame-build/khub/issues/124)
55. **Cardinality predicates over relations** [iwe, S–M; feeds #37] → [#125](https://github.com/endgame-build/khub/issues/125)
56. **Universal `--dry-run`** [iwe, S] → [#126](https://github.com/endgame-build/khub/issues/126)
57. **MCP surface shape** [iwe, S; with the planned MCP server] → [#127](https://github.com/endgame-build/khub/issues/127)

## J. Schema visualization [og]

From the OntoGraph review ([`ontograph-review.md`](ontograph-review.md)). One
candidate; the rest of that repo is rejected.

58. **`khub viz --aspect schema|instances|both`** [og, S–M; completes #11] → [#128](https://github.com/endgame-build/khub/issues/128)

## K. Ingestion alignment & retrieval evaluation [kag]

From the KAG review ([`kag-review.md`](kag-review.md)). The theme: KAG's
runtime is rejected whole; what transfers are decisions at khub's two open
edges — what happens between extraction and write when unstructured sources
enter, and how retrieval quality is measured and surfaced.

59. **Ingestion alignment pass — link before write** [kag, M; design note now, with #25/#26] → [#129](https://github.com/endgame-build/khub/issues/129)
60. **Multi-hop retrieval eval over a live corpus** [kag, S–M; extends #10] → [#130](https://github.com/endgame-build/khub/issues/130)
61. **Search hits are graph nodes** [kag, S] → [#131](https://github.com/endgame-build/khub/issues/131)

## L. OKF interop, provenance & retrieval ergonomics [okfm]

From the OKF Agent Memory review ([`okf-comparison.md`](okf-comparison.md)),
which built and probed both tools. The theme: khub wins the engine — schema,
typed edges, write discipline, FTS5, integrity gates, formats — and okfm wins
the OKF v0.2 vocabulary and the first five minutes. What transfers is the
provenance layer khub's "OKF implementation" claim is missing, the export
boundary that makes the claim testable, and two ergonomics gaps the probes
exposed (standing context, forgiving search). Items name the review's feature
request they record (FR1–FR8) so the two documents agree.

62. **Standing-context budget** [okfm; FR1; feeds #41] → [#132](https://github.com/endgame-build/khub/issues/132)
63. **`search --plain`** [okfm; FR2] → [#133](https://github.com/endgame-build/khub/issues/133)
64. **Provenance stamping: `generated: {by, at}`** [okfm; this session] → [#25](https://github.com/endgame-build/khub/issues/25)
65. **Review validity + trust tier** [okfm; FR5 + this session; needs #64] → [#26](https://github.com/endgame-build/khub/issues/26) + [#27](https://github.com/endgame-build/khub/issues/27)
66. **`stale_after` authored expiry** [okfm; FR5 lifecycle row] → [#29](https://github.com/endgame-build/khub/issues/29)
67. **OKF v0.2 export/consume** [okfm; FR7 + this session; the design memo's planned `export --okf`, mechanics pinned here] → [#134](https://github.com/endgame-build/khub/issues/134) (export) · [#28](https://github.com/endgame-build/khub/issues/28) (`sources`) · [#32](https://github.com/endgame-build/khub/issues/32) (consume)
68. **Ordinary-file write safety** [okfm; FR8; relates to #50/#51] → [#135](https://github.com/endgame-build/khub/issues/135)
69. **Navigation drift gate** [okfm; review "lower priority" + this session] → [#136](https://github.com/endgame-build/khub/issues/136)

## Explicitly rejected (no issue)

Recorded so the picking is complete; each conflicts with an invariant or
solves a problem khub's corpus does not have:

- Reasoning/inference engines (unauthored facts don't exist).
- Embeddings, GraphRAG, vector stores (FTS5 + traversal wins at this corpus
  size; a projection-layer swap remains possible later).
- Graph analytics: centrality, communities, link prediction.
- Persisted graph databases as store; W3C stack as *native* schema format
  (see #44–46 for the boundary version).
- Type inheritance trees (#9 bundles are the borrowable piece).
- Agent-memory stores; plugin/method registries (the schema is the extension
  point); learned suppression (#35 is the explicit version); version-storage
  subsystems (git is this).

From the IWE review, with reasons in [`iwe-comparison.md`](iwe-comparison.md):

- Path-glob schema binding (the ontology would become implicit in the
  filesystem; type-binding is the invariant).
- A block-level mutation surface for entity bodies (frontmatter-first entities;
  body editing is the editor's job — only the *validation* vocabulary transfers,
  as #49).
- `extract`/`inline` body refactoring (wants owned children, #14, first; no
  candidate number until then).
- `normalize` as a corpus verb (the ruamel round-trip is the write discipline,
  and frontmatter-slug relations have no link titles to resync).
- An LSP surface (a real gap, a large project, not now).
- Untyped inclusion/reference edges (khub's typed predicates dominate).

From the KAG review, with reasons in [`kag-review.md`](kag-review.md):

- The runtime in every form — OpenSPG server, graph store, vector store,
  Docker Compose (a store becomes truth; the static binary becomes a
  deployment).
- An in-engine solver and logical-form DSL (khub's consumer is the planner;
  the cascade survives as skill guidance, #41).
- Schema-free/openIE extraction (unauthored facts don't exist; ingestion is
  schema-constrained only).
- The concept-normalization layer (khub's taxonomy is types + enums + tags;
  noise is refused at the write gate, not absorbed semantically after the
  fact).
- Fine-tuned pipeline models (khub ships no model; the frontier agent it
  serves is the model).
- AtomicQuery-style question indexing (wants embeddings and an LLM at index
  time; #10 is the schema-level cousin).
- Predicate semantics beyond `acyclic` + declared inverses (they exist to feed
  inference, which stays rejected; khub's two are already shipped).

From the OntoGraph review, with reasons in [`ontograph-review.md`](ontograph-review.md):

- OntoGraph itself, in every form — as a dependency (Java/Spring Boot 1.5.6, both
  its artifact repositories sunset, unbuildable since ~2021), as a service (requires
  Stardog, a commercial triple store, against the no-database invariant), and as a
  port (GraphML needing hand layout in yEd is strictly worse than the self-contained
  Cytoscape HTML `viz` already emits).
- Full-OWL construct coverage as a conformance target for #45 — its testcases reach
  `owl:oneOf`, `complementOf`, `withRestrictions`, `onDatatype`, transitive and
  asymmetric properties, nested unions. Use them to check that what khub *emits*
  round-trips; never as a bar to clear (#44 rejects full OWL deliberately).

From the OKF Agent Memory review, with reasons in [`okf-comparison.md`](okf-comparison.md):

- The body-link graph (typed frontmatter predicates are the invariant; body
  links stay navigational).
- A hand-rolled YAML parser and emitter (the review reproduced it losing a
  folded description; `internal/canon` is the choke point).
- Full re-serialize writes (the token splice is the contract).
- The prefix TF-IDF scorer labelled BM25 (loses MATCH syntax and Unicode; FTS5
  stays).
- A `curl | sh` or Homebrew channel (ci-release.md; deleted deliberately).
- An authored per-write `log.md` — and a generated one until a consumer needs
  it (git is the log; #67 omits it).
- MCP by default (verdict unchanged; re-measure the footprint against a
  six-tool implementation before arguing it again — #57 holds the shape).
- Replacing `draft` with OKF `status` (the manual publish flag is sharper;
  `status` is an export projection in #67).
- Backfilling `generated` onto existing entities (fabricates an actor).
- Human-review labels as a security boundary (#65 is a "say you knew" gate;
  authorization is repository policy).
- Attested computations as engine work (a preset can declare the type with
  `runtime`/`parameters`/`executor`/`attester` attributes).
- Cloud federation or automatic fact extraction as parity work (neither is
  delivered by the reviewed implementation).
- A new generic memory preset (try build-lite plus #62's compact onboarding
  first).
