# Knowledge Hub — Design Memo (Draft)

**Status:** draft, 2026-06-16. Captures decisions from the initial brainstorm. Items marked **OPEN** are resolved before any code.

## What it is

A git-backed, ontology-typed knowledge base. Every entity is one Markdown file with YAML frontmatter, held in git. A LinkML ontology defines the types, attributes, and legal relations. A Python library provides validated CRUD and graph queries. A generic CLI (`khub`) and a Claude Code skill are thin, schema-driven surfaces over that library.

The product is two things at once:

1. A library of **canonical ontology presets, one per engagement type** — the firm's encoded judgment about how to model a kind of problem. This is the IP.
2. A **per-engagement workspace** — each engagement gets its own repo seeded from a preset, where the team authors entities and extends the schema as the work demands.

## Principles (invariants)

1. **Markdown is truth.** One entity equals one file in git. Audit, diff, PR review, and portability come for free.
2. **The graph is a derived projection,** rebuilt from the Markdown on demand. No graph database is ever the source of truth.
3. **The schema is the contract.** The CLI and skill are generic and reflect the schema. Adding a type touches only the ontology.
4. **Introspect, don't hardcode.** Per-type knowledge is read from the schema at runtime, never baked into the tooling.
5. **Frontmatter relations are authoritative.** Inline body links are navigational only and stay out of the graph.

## Deployment model — seeded fork

The hub repo (this repo) holds the tool, the canonical presets, the skills, and an installer.

`khub init <engagement-type>` scaffolds a fresh engagement repo. It flattens the chosen preset (with `core` merged in) into one self-contained, fully editable schema, then lays down the entity tree.

```
client-repo/
  .khub/
    config.yaml
    schema.yaml        # seeded from the preset — edit/delete freely
    generated/         # json-schema + pydantic, regenerated, gitignored
  knowledge/
    <type-folders>/    # one .md per entity
```

The schema header stamps provenance:

```
# khub-preset: engineering@1.0.0 (sha abc1234)
# Edit freely. `khub diff-preset` shows drift.
```

The engagement schema is a fully editable fork — add, change, or delete anything. The canonical preset stays pristine in the hub. `khub diff-preset` shows drift in both directions: pull firm updates in, or surface a locally proven type for promotion back into the hub.

Sync between hub and engagement repos reuses atelier's `hub-subtree` / spoke pattern (copied in when sync matters), not a parallel installer.

## First preset — engineering / building systems

Scope: **full system model** — the complete typed map, not just a decision layer. facet seeds the structural entities (Component, APISchema, DataEntity) from its extraction output; humans author the motivation and delivery layers and the cross-links. The overlap with facet is the ingestion path, not duplication.

### Core types, by layer

| Layer | Types |
|-------|-------|
| Motivation | `Requirement`, `Constraint`, `Risk`, `Decision` (kind: architecture[=ADR]/product/process) |
| Capability | `Capability` — keystone (**OPEN**: confirm inclusion) |
| Delivery | `Epic`, `Feature` |
| Structure | `Component` (service/library/frontend/worker), `APISchema` (rest/graphql/grpc/event), `DataEntity` |
| Code org | `Repository` |
| Technology | `Technology` (framework/library/runtime + version + lifecycle) |
| Runtime | `Environment` (dev/staging/prod) |
| People | `Team`, `Actor` |

### Traceability spine (relations)

- `Capability` **satisfies** `Requirement`
- `Feature` **delivers** `Capability`
- `Component` **realizes** `Capability`
- `Actor` **uses** `Capability`
- `Feature` **part_of** `Epic`

Structural edges, all from `Component`: **depends_on** `Component`, **exposes** / **consumes** `APISchema`, **persists** `DataEntity`, **lives_in** `Repository`, **built_with** `Technology`, **deployed_to** `Environment`.

Governance edges: `Decision` **supersedes** `Decision`; `Decision` **decides_on** `Component` / `Capability`; `Constraint` **constrains** `Component` / `Feature` / `Capability`; `Risk` **threatens** `Component` / `Capability` / `DataEntity`; **owned_by** → `Team` on `Repository` / `Component` / `Feature` / `Epic`; `Repository` **depends_on** `Repository`.

### Principle: the hub is not an issue tracker

Delivery stops at `Epic` / `Feature`. `WorkPackage` / `Story` / `Task` stay in beads or GitHub and link in by URL. The backlog is not mirrored into the graph.

### Optional (local extension, not firm core)

`DataStore`, `Deployment`, `Stakeholder`, `Goal`, `Assumption`, `Learning` / `OpenQuestion`, `Infrastructure`, `TechDebt` — added per engagement when needed.

## Architecture (5 layers)

| # | Layer | Tech |
|---|-------|------|
| 1 | Source of truth | Markdown + YAML frontmatter, git |
| 2 | Ontology | LinkML (YAML) |
| 3 | Core library | Python — validate / create / get / update / delete / link / query |
| 4 | Graph projection | NetworkX (default), swappable |
| 5 | Access | Generic CLI (`khub`) + Claude Code skill (MCP optional) |

The core library is the only place logic lives. The CLI, skill, and any future MCP server are thin adapters over it.

## Relationship to existing ENDGAME tooling

- **facet** seeds structural entities and supplies the ingestion format. Loose coupling — Knowledge Hub reads facet output, with no runtime dependency.
- **forge** owns finer delivery artifacts (work packages, specs). The hub links to them, it does not replace them.
- **hub-subtree / facet spoke** supplies the sync pattern reused for hub-to-engagement sync.

## Open questions

1. **Capability spine** — confirm `Capability` as the keystone of the engineering preset (recommended), or omit it.
2. **Core trim** — push any of `Actor` / `Environment` / `Technology` / `Requirement` to optional to keep the core leaner?
3. **Naming** — `khub` vs `kh` for the CLI and the dot-directory.
4. **Id scheme** — human-readable type-prefixed slugs (`comp-auth`, `adr-0012`) vs UUIDs; alignment with facet IDs (`FUNC` / `UCAP` / `COMP`) for the ingestion path.
5. **Engine swap** — NetworkX is the default projection engine; decide when a move to an embedded Cypher engine or Neo4j earns its cost.

## Next step

Resolve the open questions, then move to a written implementation plan covering build phases P0–P6. No code until the design is approved.
