# Knowledge Hub (khub)

**Structured, schema-bound context management for analytical and operational work.**

khub gives an AI agent typed, validated, queryable context — structured memory it can navigate and write back to — instead of unstructured documents stuffed into a context window.

One generic engine: entities live in git as Markdown with YAML frontmatter (the default), as `.json`/`.yaml` documents, or as rows of a single-file collection (`repos.jsonl`) — per-type schema config. A [LinkML](https://linkml.io) ontology is the contract — types, attributes, and legal relations. A Python core library provides schema-validated CRUD and graph queries; a generic `khub` CLI and a Claude Code skill are thin, schema-driven surfaces over it. The graph is a projection rebuilt from the Markdown on demand — no database is ever the source of truth.

Built on top of the [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md). khub's Markdown entities are OKF concepts; on top, khub adds a typed schema, a graph, and the serialization formats and collections OKF lacks. Any workspace projects to a conformant OKF bundle.

**The schema is the operational setup.** It configures what a given hub is *for*. Canonical ontology presets — one per domain (building and maintaining a system, consulting, research, operations) — are the reusable IP. Each engagement gets its own repo seeded from a preset, where agents and humans co-author entities and extend the schema as the work demands.

## What it does today

- **Author** — `add` / `get` / `edit` / `link` / `unlink` / `remove`: schema-validated writes, referential integrity hard-fails, capture never blocked, minimal-diff round-trips.
- **Find** — `query` (frontmatter + edge filters), `search` (BM25 full-text over titles, bodies, and fields — FTS5, built in-memory per call, never stale), `neighbors` / `impact` / `history` (graph walks).
- **Gate** — `validate` (per-entity well-formedness) and `check` (graph-wide completeness, dangling edges, strays, cycles; orphans informational unless `--strict`); `stale` and `log` read git at entity altitude — row-accurate even inside collections.
- **Project** — `reindex` (OKF `index.md`), `viz` (Cytoscape HTML), `backfill` (git-derived dates and scaffolding).
- **Store** — per-type `layout` (file / folder / collection) × `format` (md / json / yaml; collections take json / jsonl / yaml). Non-md entities carry prose in a reserved `body` field; collection writes are lock-serialized and crash-atomic.

Full command surface and JSON contracts: [`docs/cli.md`](docs/cli.md). Feature history: [`CHANGELOG.md`](CHANGELOG.md).

**Status:** v1 engine shipped and proven on a live corpus (the firm-hq cutover: 250+ entities, the incumbent scripts retired). Design rationale in [`docs/design-memo.md`](docs/design-memo.md); the collections row model in [`docs/collections-design.md`](docs/collections-design.md).
