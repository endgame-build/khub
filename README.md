# Knowledge Hub (khub)

**Structured, schema-bound context management for analytical and operational work.**

khub gives an AI agent typed, validated, queryable context (structured memory it can navigate and write back to) instead of unstructured documents stuffed into a context window.

One generic engine: entities live in git as Markdown with YAML frontmatter (the default), as `.json`/`.yaml` documents, or as rows of a single-file collection (`repos.jsonl`), per-type schema config. A [LinkML](https://linkml.io) ontology is the contract: types, attributes, and legal relations. A Python core library provides schema-validated CRUD and graph queries; a generic `khub` CLI (and a planned Claude Code skill) are thin, schema-driven surfaces over it. The graph is a projection rebuilt from the Markdown on demand; no database is ever the source of truth.

Built on top of the [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md). khub's Markdown entities are OKF concepts; on top, khub adds a typed schema, a graph, and the serialization formats and collections OKF lacks. Any workspace projects to a conformant OKF bundle.

**The schema is the operational setup.** It configures what a given hub is *for*. Canonical ontology presets, one per domain (building and maintaining a system, consulting, research, operations), are the reusable IP. Each engagement gets its own repo seeded from a preset, where agents and humans co-author entities and extend the schema as the work demands.

## What it does today

- **Author** — `add` / `get` / `edit` / `link` / `unlink` / `remove`: schema-validated writes, referential integrity hard-fails, capture never blocked, minimal-diff round-trips.
- **Find** — `query` (frontmatter + edge filters), `search` (BM25 full-text over titles, bodies, and fields: FTS5, built in-memory per call, never stale), `neighbors` / `impact` / `history` (graph walks).
- **Gate** — `validate` (per-entity well-formedness) and `check` (graph-wide completeness, dangling edges, strays, cycles; orphans informational unless `--strict`); `stale` and `log` read git at entity altitude, row-accurate even inside collections.
- **Project** — `reindex` (OKF `index.md`), `viz` (Cytoscape HTML), `backfill` (git-derived dates and scaffolding).
- **Store** — per-type `layout` (file / folder / collection) × `format` (md / json / yaml; collections take json / jsonl / yaml). Non-md entities carry prose in a reserved `body` field; collection writes are lock-serialized and crash-atomic.

Full command surface and JSON contracts: [`docs/cli.md`](docs/cli.md). Feature history: [`CHANGELOG.md`](CHANGELOG.md).

## Quickstart

khub is a `khub` console script (Python 3.11+). Install it, then seed a workspace from a preset:

```bash
uv tool install git+https://github.com/endgame-build/knowledge-hub   # or: clone + uv sync
khub init firm-ops ./my-hub          # scaffold .khub/ and the entity tree
cd my-hub
```

Author entities. Referential integrity hard-fails on write (a relation to a missing target is rejected), but a missing field never blocks capture:

```bash
khub add client  --name "Acme Corp" --industry manufacturing
khub add person  --name "Dana Lee"  --role partner
khub add project --title "Acme Diagnostic" --client acme-corp --owner dana-lee
```

Then walk the graph and gate it:

```bash
khub query --type project        # → project/acme-diagnostic, with orphan/stale flags
khub neighbors acme-diagnostic   # → its client and owner edges
khub check                       # graph-wide: relations resolve, nothing dangling → passed
```

Every read command takes `--format json` for an agent and prints a Rich table for a human.

## Presets

A preset is a canonical ontology for one domain: its entity types, attributes, and legal relations. `khub init` merges `core.yaml` (the `base` block every entity carries: `type`, `created`/`updated`, `tags`, the OKF fields, the `any → any` edges) with the named preset into an engagement's `.khub/schema.yaml`, which agents and humans then extend as the work demands.

**`firm-ops` ships today**: the HQ operations ontology (client, project, person, opportunity, meeting, and more); see [`docs/firm-ops-preset.md`](docs/firm-ops-preset.md). Engineering, consulting, and research presets are planned. New to khub? Start with [`docs/getting-started.md`](docs/getting-started.md).

**Status:** v1 engine shipped and proven on a live corpus (the firm-hq cutover: 250+ entities, the incumbent scripts retired). Design rationale in [`docs/design-memo.md`](docs/design-memo.md); the collections row model in [`docs/collections-design.md`](docs/collections-design.md).
