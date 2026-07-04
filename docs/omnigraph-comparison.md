# Omnigraph and khub: A Comparison

**Status:** analysis, 2026-07-04. Source material: the [ModernRelay/omnigraph](https://github.com/ModernRelay/omnigraph) repository (README, `docs/user/*`, `crates/*`) and khub's own `docs/design-memo.md`.

## Summary

Omnigraph and khub target the same role: a typed graph that holds the memory and context for fleets of AI agents. Both declare the schema as code. Both use git-style branch-and-merge as the multi-agent write model. Both reach agents through a Claude Code skill and an MCP server. The systems diverge on architecture. khub keeps every entity as a Markdown file in git and rebuilds the graph as a projection on demand. Omnigraph stores the graph as a columnar database on object storage and runs as a server. khub sits at design stage; omnigraph publishes release binaries through v0.8.0. The two share a purpose and split on the core implementation choices.

## What Each System Is

**Omnigraph** (ModernRelay; Rust; MIT license; release series through v0.8.0). Omnigraph describes itself as a lakehouse graph database for context assembly and multi-agent coordination. The store is [Lance](https://github.com/lance-format/lance) columnar datasets, one per node type and one per edge type, on any S3-compatible object store, coordinated by an append-only `__manifest` table. It ships a property-graph schema language (`.pg`) and a query and mutation language (`.gq`), typechecked, with a cost-based traversal planner. Writes go through three-way row-level branch merge, with Cedar policy enforced server-side on every mutation. One query runtime fuses graph traversal, vector ANN, BM25 full-text, and Reciprocal Rank Fusion for multimodal retrieval. An Axum HTTP server boots from a Terraform-style `cluster.yaml` control plane and serves N graphs under `/graphs/{id}/`. Clients include a TypeScript SDK, an MCP server, and an OpenAPI contract.

**khub** (ENDGAME; Python; private; design stage). khub describes itself as structured, schema-bound context management. Every entity is one Markdown file with YAML frontmatter in git, and the files are the source of truth. The graph is a projection rebuilt on demand with networkx, held in memory. A khub schema of entities, attributes, and relations compiles to LinkML, which generates Pydantic models and JSON Schema for validation. A Typer CLI and a Claude Code skill are thin adapters that introspect the schema at runtime. khub builds on Google's Open Knowledge Format (OKF). The reusable asset is the per-domain preset (firm-ops, consulting, research, engineering). v1 proves the engine by cutting the firm-hq corpus, roughly 380 entities across 9 types, over to khub.

## Where They Overlap

| Axis | Omnigraph | khub | Overlap |
|---|---|---|---|
| Positioning | operational state and coordination layer for fleets of agents | structured memory an agent navigates and writes back to | Near-identical. Omnigraph's build list (company brain, agentic memory, context graph, dev graph, R&D layer) maps onto khub's presets (firm-ops, engineering, consulting, research). |
| Typed schema as contract | `.pg`: node, edge, interface, `@key`, `@card`, `@unique`, enums, typecheck | khub schema compiled to LinkML; required relations, cardinality, enums | Both compile a schema and typecheck. Both treat the schema as the operational setup. |
| Declared as code | `cluster.yaml`, then `plan` and `apply` | `.khub/schema.yaml` with preset provenance, then `khub init` | The schema and config are the versioned, reusable asset that configures the hub. |
| Git-style branch and merge | branch per agent, three-way row-level merge, review then merge | git on Markdown; symmetric writers, gate is schema and git, `git revert` as backstop | Both adopt a branch per agent or task, merged on review, as the coordination model. |
| Agent-first access | ships `skills/omnigraph` and `@modernrelay/omnigraph-mcp` | ships a Claude Code skill; MCP after v1 | Same distribution surface; both name the agent as the primary consumer. |
| Graph traversal | `.gq` variable-length traversal, aggregates | `neighbors`, `impact`, `path`: transitive closure over an edge | Both answer "what depends on or reaches this entity." |
| Self-hosted and private | your object store, data stays in your store | local git repo, everything stays private | Both keep data in the operator's own custody. |

The strongest overlap is the positioning. Omnigraph's own description of what it is for restates khub's purpose in the same terms. The list matches: a typed graph for agent memory, git-style branches, schema as code, and access through a skill and MCP. That shared pitch is the part worth taking seriously.

## Where They Diverge

| Axis | Omnigraph | khub |
|---|---|---|
| Source of truth | Lance columnar datasets on object storage are the truth | Markdown in git is the truth; the graph is derived and disposable |
| Retrieval | vector ANN, BM25, RRF, and an embeddings pipeline with provider config, built in | frontmatter filtering and networkx traversal; FTS is a fast-follow; vectors are absent from the design |
| Scale and deployment | Axum server, S3, hundreds of agents, columnar analytics, cluster control plane | in-memory networkx, one repo, roughly 380 entities, no server |
| Multimodal | `Blob` and `Vector` scalars store documents, images, and video as data | text Markdown only |
| Security | Cedar policy engine, server-side on every mutation, hashed bearer tokens, server-resolved actor | no policy engine; repo access, git attribution, and `git revert` |
| Schema evolution | migration planner: add, rename, drop; soft-drop reversible via time travel; hard-drop behind `--allow-data-loss` | editing `schema.yaml` is the override mechanism; migrations stay informal |
| Maturity and language | Rust, crates.io, released binaries, TypeScript SDK, OpenAPI, v0.8.0 | Python, design stage, engine locked, v1 not yet cut over |

The divergence that drives the others is the source of truth. khub exists to hold a human-legible, PR-reviewable, portable, zero-infrastructure corpus that a firm co-authors: Markdown you read, diff, and revert. Omnigraph is a columnar database built for query throughput and vector retrieval at fleet scale, with no Markdown, no file-per-entity, and no reading the store by eye. Retrieval, security, and scale all follow from that one choice.

## Strategic Read for khub

### Competition Is Real at the Level of Positioning

Competition holds on the pitch and fades on the workload. Omnigraph tells the same story with more volume and with shipped code, so khub's differentiators have to stay sharp: Markdown as truth (legibility, git-native audit, PR review, portability, no infrastructure), presets as encoded firm judgment (the reusable asset), and OKF conformance (a vendor-neutral substrate). Omnigraph carries none of those. It competes on scale, multimodal retrieval, and policy as code, which khub defers by design.

### Omnigraph Fits a Slot khub Already Left Open

khub's design memo marks the projection layer as swappable and names a graph engine (kuzu or oxigraph, embedded) as a deferred option, held back while the corpus stays small. Omnigraph is a heavier version of that deferred slot. The clean framing keeps Markdown as truth and projects into an omnigraph cluster only when a workspace outgrows networkx and needs vector retrieval or fleet coordination. That preserves the invariant, files are truth and the graph is derived, and treats omnigraph as one possible derived backend.

### Patterns Worth Borrowing

Three omnigraph designs solve problems khub will hit, and none of them touch the truth model:

- **Merge-conflict taxonomy.** `DivergentUpdate`, `DeleteVsUpdate`, `OrphanEdge`, and the unique, cardinality, and value-constraint violations form a model of what `khub check` should catch when two agents edit concurrently. khub's memo defers concurrency arbitration; this is the vocabulary for it.
- **Schema migration planner.** Add, rename, and drop with reversible soft-drops is the mature shape of schema evolution. khub currently treats editing one file as the whole mechanism, which the first live preset change under an engagement will strain.
- **RRF multimodal retrieval.** Graph plus vector plus full-text fused by Reciprocal Rank Fusion is the shape khub's search fast-follow would want if it moves past FTS5.

### Where They Do Not Compete

khub's regime is a local-first, no-infrastructure, human-authored corpus of a few hundred entities that a firm reviews in PRs. Standing up S3, Cedar, and Lance for 380 Markdown files is overkill. Omnigraph's regime is fleet-scale vector retrieval over blobs across hundreds of agents, which networkx over Markdown does not reach. Neither system operates in the other's scale band.

## Bottom Line

Omnigraph bets on the database and gains scale, retrieval, and policy enforcement. khub bets on the file and gains legibility, portability, and zero infrastructure. Treat omnigraph as a candidate projection backend and a source of solved-problem patterns. Its workload sits in a different scale band from the one khub v1 targets.
