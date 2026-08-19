# KAG: reviewed, edges taken, runtime rejected

**Status:** comparative review, 2026-08. Verdict up front: **adopt no code, no
runtime, no dependency.** KAG is the strongest published validation of khub's
schema-first bet, arrived at from the opposite direction — it *reconstructs*
structure that khub *authors* — and the clearest exemplar of the architecture
khub deliberately rejects (stores as truth, an in-engine reasoner, models in the
loop). What transfers are decisions, not components, at the two edges where khub
will face KAG's problems: the ingestion path, where unstructured sources enter,
and the retrieval surface, where an agent composes the verbs. Actionable output:
candidates **#59–61** plus reinforcements in
[`feature-candidates.md`](feature-candidates.md).

Reviewed at [`OpenSPG/KAG`](https://github.com/OpenSPG/KAG) commit `fdab15b`
(2026-01-28), from the README, the 0.5→0.8.0 release history, and the
[`OpenSPG/openspg`](https://github.com/OpenSPG/openspg) engine README. The
framework paper is arXiv:2409.13731 (Ant Group, 2024). arxiv.org is
egress-blocked from the review environment, so paper claims below are the ones
the repo's own documentation restates; anything sourced from the paper alone is
attributed as such.

## What it is

KAG ("Knowledge Augmented Generation", Ant Group with OpenKG) is a
question-answering framework over professional-domain corpora. Its diagnosis:
vector-similarity RAG is insensitive to logic and ambiguity, and GraphRAG-style
open extraction floods the graph with noise. Its response is a pipeline that
builds a schema-constrained knowledge graph from documents, cross-indexed with
the source text, and a solver that answers questions by decomposing them into
logical forms executed as hybrid graph-plus-text retrieval. The paper claims
19.6% / 33.5% relative F1 improvement over RAG baselines on HotpotQA / 2wiki at
launch; the 0.7 release reports EM 0.603 HotpotQA, 0.684 2wiki, 0.385 MuSiQue,
and the paper reports production deployment in Ant Group's e-government and
e-health Q&A.

| | |
|---|---|
| Org / license | Ant Group + OpenKG, Apache-2.0 |
| Language | Python 3.10+ over a Java engine (OpenSPG server) |
| Runtime | OpenSPG server + graph store + vector store + LLM endpoints; Docker Compose ("product mode") or pip ("developer mode") |
| Releases | 0.5 2024-10 → 0.6 2025-01 → 0.7 2025-04 → 0.8.0 2025-06 |
| Cadence | slowed after 0.8.0: KAG-Thinker support 2025-07, single fixes 2025-08 and 2026-01 |
| Paper | arXiv:2409.13731, five enhancements: LLM-friendly representation, mutual indexing, logical-form-guided reasoning, knowledge alignment, model enhancement |

## Architecture

Three layers: an engine it stands on, a builder that constructs the knowledge
base, a solver that consumes it.

### The engine underneath: OpenSPG

SPG ("Semantic-enhanced Programmable Graph") is Ant's attempt to make RDF/OWL
semantics industrially practical on LPG property-graph storage: SPG-Schema
(subject and concept models over property graphs), SPG-Reasoner (KGDSL, a rule
language for logical inference), and storage adapters over a pluggable graph
store and vector store. KAG is the LLM layer over this server — every KAG
deployment carries the whole stack.

### kag-builder: the reconstruction pipeline

A staged pipeline: **Reader** (documents, structured records) → **Splitter**
(layout-aware chunking) → **Extractor** (LLM information extraction, in two
modes: *schema-free* openIE and *schema-constrained* against a domain schema) →
**Vectorizer** (embeddings) → **Alignment** → **Writer** (into the stores).

Three decisions inside it are the reviewable substance:

- **Mutual indexing.** Graph nodes and source chunks are cross-linked both
  ways, so retrieval can pivot from a text hit to its entities and from an
  entity to its supporting text. This is KAG's answer to "structure alone loses
  the prose; prose alone loses the structure."
- **Layered representation** (the paper's LLMFriSPG, a DIKW framing): raw
  chunks at the bottom, openIE-extracted information above, schema-constrained
  knowledge at the top, one node addressable across layers. 0.8.0 generalizes
  this into configurable index types — Chunk, Outline, Summary, KnowledgeUnit,
  AtomicQuery, Table — each an extractor/retriever pair.
- **Alignment as its own stage.** Extraction output is not written as-is:
  mentions are normalized and linked against existing nodes using concept-level
  semantic reasoning (synonym, hypernym, is-a), because unaligned extraction
  mints duplicate nodes and dangling references. The paper calls this
  "knowledge alignment to alleviate noise"; it is the stage that makes the rest
  of the pipeline usable.

Also instructive: 0.7's "lightweight construction" mode cut build token cost by
a reported 89% with minimal quality loss — evidence that a cheap single-pass
extraction mode is worth having alongside a thorough one.

### kag-solver: the compensation pipeline

A **planner** decomposes the question into subtasks expressed in a logical-form
DSL — static planning for closed questions, iterative planning with reflection
for open ones — and **executors** run them: `kag_hybrid_executor` (retrieval
over three knowledge layers: schema-constrained graph, schema-free graph, raw
chunks), `math_executor`, `cypher_executor`. Retrieval inside the hybrid
executor cascades: exact graph match first, then graph traversal, then
text/vector retrieval over the chunks reachable through the mutual index. A
**generator** composes the answer with chunk citations, and the reflection loop
re-plans when the evidence is judged insufficient. Since 0.8.0 a fine-tuned
model (KAG-Thinker, Qwen2.5-based, "broad decomposition and deep solving") can
drive the decomposition, and the whole solver is exposed over HttpAPI, MCP, and
an embeddable frontend.

A question like "which company acquired the startup founded by X?" becomes:
plan two retrievals (founded-by, acquired-by); execute the first as an exact
graph lookup; on partial miss, fall back to chunk retrieval around the
candidate nodes; deduce the join; generate with citations; reflect and re-plan
if a hop came back empty.

### Why the machinery exists

Every major KAG subsystem compensates for one of two absences. The corpus is
born unstructured, so structure must be reconstructed — that is the builder,
the alignment stage, the mutual index, and both stores. And the consuming model
is not trusted to plan, so planning is encoded — that is the logical-form DSL,
the solver, and a fine-tuned thinker model. khub has neither absence: its
corpus is born structured (entities are authored against the schema, and
referential integrity hard-fails on write), and its consumer is a frontier
agent that plans its own retrieval. That is why nothing in KAG's runtime
transfers, and why the transferable decisions cluster exactly where khub meets
KAG's conditions: ingestion (unstructured sources entering the workspace) and
retrieval ergonomics (how much an agent gets per call).

## Full comparison

| Dimension | KAG | khub |
|---|---|---|
| Problem | answer end-user questions over a corpus | give an agent typed context to act on and write back to |
| Consumer | humans asking; its own solver LLM executing | frontier agent + human, symmetric writers |
| Source of truth | graph store + vector store, pipeline output | Markdown/JSON/YAML in git |
| Structure | reconstructed by LLM extraction | authored, schema-validated at write |
| Schema | SPG classes, concept models, semantic relations | khub-vocabulary YAML, base merge, per-type storage |
| Identity | entity linking + normalization at build time | per-type slug, `source_id` alias, hard referential integrity |
| Text ↔ graph | mutual index, built and persisted | structural — an entity is chunk and node in one file |
| Retrieval | logical-form solver, hybrid cascade, reflection | agent composes `query` / `neighbors` / `impact` / `search` |
| Inference | KGDSL rules, concept reasoning | none by design; derived inverses and `acyclic` only |
| Integrity gates | alignment-stage noise reduction | `validate` (write gate) + `check` (graph gate) |
| Provenance | supporting-chunk citations | git attribution + `sources` edges |
| Writes back | no — read-side QA | first-class write verbs |
| Evals | multi-hop QA EM/F1, published per release | parity bytes + goldens; no retrieval tier (→ #60) |
| Runtime | Java server, Python, two stores, LLM endpoints | one static Go binary |
| Scale target | enterprise corpora | an engagement's corpus |

## What khub gets by construction

Half of KAG's build effort produces properties khub has structurally; worth
naming so they are recognized as assets rather than gaps:

- **The mutual index** is khub's file format: one entity is the chunk (body,
  FTS5-indexed) and the node (typed frontmatter edges) under one id. The pivot
  KAG builds two stores to support is a no-op — except that `khub search`
  output does not yet *behave* like a node (→ #61).
- **Summary/Outline index types** are `title` / `description` plus the
  `reindex`-generated `index.md`.
- **Supporting-chunk citations** are the `sources` relation plus git
  attribution — with the stronger property that provenance is per-write and
  attributable to an author, not per-extraction.
- **The DIKW split** is reference docs outside the type layouts (raw), typed
  entities (knowledge), and the `draft` flag as the deliberate boundary — with
  the difference that khub's top layer is authored, so nothing in it is an
  unreviewed model guess.
- **Concept normalization** is the type system, enums, and slug identity;
  noise cannot enter through the authored write path at all. The gap KAG's
  alignment covers remains open only on the ingestion path (→ #59).

## What to take

Three new candidates, specified in
[`feature-candidates.md`](feature-candidates.md) §K:

| # | Item | Effort |
|---|---|---|
| 59 | Ingestion alignment pass — link before write | M (design note now) |
| 60 | Multi-hop retrieval eval over a live corpus | S–M |
| 61 | Search hits are graph nodes | S |

**#59** is the load-bearing lesson of the builder: extraction and writing must
be separated by an explicit linking pass, or ingestion mints duplicates. khub's
version is an alignment ladder run per extracted reference — exact `(type,
slug)`, then alias (#15), then `source_id`, then FTS5 candidates *proposed but
never auto-merged* — with unresolved mentions minted as **draft** entities
carrying `source_id`, so referential integrity holds on write and `check`
surfaces the new drafts for review. Capture is never blocked; identity is never
guessed silently.

**#60** is KAG's benchmark discipline pointed at khub's actual product surface:
a question set over the HQ corpus whose answers require composing verbs across
hops, run agent-in-the-loop, scored fuzzily, tracked as a trend. The parity
suite pins bytes; nothing today measures whether the surface is *good to
retrieve with*.

**#61** cashes in the structural mutual index at the output layer: search hits
carry the flags and a one-hop edge digest so an agent picks which hit to walk
without a round of `get`s.

Reinforcements to existing candidates (recorded in
[`feature-candidates.md`](feature-candidates.md), no new numbers): **#41**
(skill guidance — the solver's cascade and static-vs-iterative split, ported
from code to prose), **#53** (`retrieve` — third independent convergence, after
IWE, on one-call context assembly), **#57** (MCP shape — KAG exposes answers;
khub must expose evidence), **#26** (schema-derived extraction scaffold,
validated at industrial scale, plus the lightweight-mode lesson), **#25**
(conflict policy extends from field values to identity), **#15** (aliases — the
entire KAG alignment stage collapsed into an authoring-time identity surface,
and #59's second rung), **#10** (competency questions — publish per-preset
scores once #60 exists), **#39** (machine-actionable misses are what make an
agent's reflection loop possible).

## Considered and declined

- **The runtime, in every form** — OpenSPG server, graph store, vector store,
  Docker Compose. Two invariants gone in one adoption: a store becomes the
  truth, and the single static binary becomes a deployment.
- **An in-engine solver and logical-form DSL.** khub's consumer is the
  planner. Encoding retrieval plans in a DSL the tool executes rebuilds the
  wizard problem one level up — machinery for an invocation khub's actual
  users never make (the 0.9.0 lesson). The cascade survives as skill guidance
  (#41), not as code.
- **Schema-free / openIE extraction.** Unauthored facts don't exist; khub
  ingestion is schema-constrained only (#26, #59).
- **The concept layer** (hypernym taxonomies as a semantic noise sink).
  khub's taxonomy is the type system, enums, and tags; noise is refused at the
  write gate and resolved in the alignment ladder, not absorbed semantically
  after the fact.
- **Fine-tuned pipeline models** (KAG-Model, KAG-Thinker). khub ships no
  model; the frontier agent it serves is the model.
- **AtomicQuery-style question indexing** (indexing content by the questions
  it answers). Wants embeddings and an LLM at index time; the schema-level
  cousin — declared question↔query pairs — is #10.
- **Richer predicate semantics** (transitive/symmetric properties, KGDSL
  rules). They exist to feed inference. khub's only derived assertions are
  declared inverses and `acyclic` cycle refusal — both already shipped
  (`presets/core.yaml`, `internal/integrity/check.go`) — and the standing
  rejection of reasoning engines holds.
- **Vector embeddings / persisted hybrid index.** Already on the standing
  rejected list; FTS5 plus typed traversal wins at khub's corpus size, and the
  swappable projection layer remains the escape hatch if that stops being
  true.

## Sources

- [OpenSPG/KAG](https://github.com/OpenSPG/KAG) — README and release notes
  0.5→0.8.0, at commit `fdab15b` (2026-01-28)
- [OpenSPG/openspg](https://github.com/OpenSPG/openspg) — the SPG engine README
- KAG paper — arXiv:2409.13731 (Ant Group, 2024); cited from its published
  abstract and the repo's restatement, arxiv.org being egress-blocked at
  review time
