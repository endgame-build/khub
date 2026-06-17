# Knowledge Hub (khub) — Design Memo

**Status:** engine and v1 seed schema locked, 2026-06-17. khub is structured, schema-bound context management for analytical and operational work — a domain-agnostic engine where the schema is the operational setup. v1 proves the engine on a small engineering seed schema. The richer engineering ontology is a documented target, not a v1 commitment.

## What it is

khub is **structured, schema-bound context management for analytical and operational work** — a semantic, ontology-aligned context hub for agents. It gives an AI agent typed, validated, queryable context — structured memory it navigates and writes back to — instead of unstructured documents stuffed into a context window.

One generic engine: every entity is one Markdown file with YAML frontmatter, held in git. A LinkML ontology defines the types, attributes, and legal relations. A Python core library provides schema-validated CRUD and graph queries. A generic CLI (`khub`) and a Claude Code skill are thin, schema-driven surfaces over that library.

**The schema is the operational setup.** It configures what a given hub is *for*. The engine knows nothing about engineering, consulting, or research — the schema does. Swap the schema and the same engine becomes a different operational hub.

The product is two things at once:

1. A library of **canonical ontology presets — one per domain** (the operational setup): engineering (building and maintaining a system), consulting and analytical engagements, research, firm operations (HQ-style). Each preset is the firm's encoded judgment about how to model a kind of work. This is the IP.
2. A **per-engagement workspace** — each engagement gets its own repo seeded from a preset, where agents and humans author entities and extend the schema as the work demands.

## Who it serves

The **agent is the primary consumer**. khub exists to be the structured context an agent operates from — it reads typed context to act, and writes its results back as typed entities. The **human** authors the schema (the operational setup) and the seed context, and reviews.

Both are first-class, symmetric writers of the same graph through the same library, with no propose-then-approve gate. The gate is the schema and git, not a human in the loop. Because the agent is a named user, the CLI and the Claude Code skill ship together in v1 — the skill is not optional.

## The engine

| # | Layer | Tech |
|---|-------|------|
| 1 | Source of truth | Markdown + YAML frontmatter, git |
| 2 | Ontology | LinkML (YAML) — the contract |
| 3 | Core library | Python — validate / create / get / update / delete / link / query |
| 4 | Graph projection | In-memory index (v1); SQLite (nodes/edges + FTS) as fast-follow; no graph engine in v1 |
| 5 | Access | Generic CLI (`khub`) + Claude Code skill (MCP later) |

The core library is the only place logic lives. The CLI, skill, and any future MCP server are thin, schema-introspecting adapters over it. Adding or changing a type is an edit to the ontology — no surface code changes.

### Principles (invariants)

1. **Markdown is truth.** One entity equals one file in git. Audit, diff, PR review, and portability come for free.
2. **The graph is a derived projection,** rebuilt from the Markdown on demand. No graph database is ever the source of truth. History (`khub log`) is derived from git the same way.
3. **The schema is the contract.** Surfaces introspect the schema at runtime and never hardcode per-type knowledge.
4. **Frontmatter relations are authoritative.** A relation is a role-named entity field the schema marks as an edge — the field name is the predicate, the value is the target. Stored single-sided on one entity; the inverse is derived, never stored. Inline body links are navigational only.
5. **Structural integrity is not semantic truth.** khub guarantees an entity is well-formed and every relation resolves; it does not guarantee an assertion is correct. A schema-legal but false write validates. The backstop is attributable git history and `git revert`, not a gate.

### Authoring and integrity

- **Identity.** The library mints each entity's id as an immutable, type-prefixed, human-readable slug (`comp-auth`, `adr-0012`); the filename equals the id; relations reference the id; a deterministic suffix resolves collisions. An external identifier rides along as a non-authoritative `source_id` alias (the ingestion path). Renaming a slug is deferred.
- **Write rules.** Referential integrity hard-fails on write — a relation to a non-existent target is rejected. An incomplete but well-formed entity is saved as a `draft`; capture is never blocked.
- **Lifecycle.** Every entity carries `status: draft|active`. `check` enforces required-relation completeness over the active subgraph only — a `draft` does not satisfy another entity's required relation.
- **The integrity loop** keeps the graph clean without manual policing — the v1 acceptance signals:
  - `khub validate` — per-entity well-formedness against the schema plus referential integrity.
  - `khub check` — graph-wide: relations resolve, required-relation completeness for `active` entities, no orphans, no stray files.
  - `khub stale` — entities whose `updated` is past a threshold; dates backfilled from `git log`.
  - `khub log` — git history rendered at ontology altitude (entities and relations, not files), for orientation without a gate.

## Deployment model — seeded fork

The hub repo (this repo) holds the engine, the canonical presets, the skill, and an installer.

`khub init <preset>` scaffolds a fresh workspace. It flattens the chosen preset (with `core` merged in) into one self-contained, fully editable schema, then lays down the entity tree.

```
client-repo/
  .khub/
    config.yaml
    schema.yaml        # seeded from the preset — edit/delete freely
    generated/         # json-schema + pydantic, regenerated, gitignored
  knowledge/
    <type-folders>/    # one .md per entity
```

The schema header stamps provenance (`# khub-preset: engineering@1.0.0`). The engagement schema is a fully editable fork; the canonical preset stays pristine in the hub. Drift detection and promote-back (`diff-preset`) and hub↔engagement sync reuse atelier's git-subtree pattern — both deferred past v1.

## v1 scope

Prove the **engine** for one operator, one workspace, hand-authored and kept clean — on a small engineering **seed schema**, not the full engineering ontology.

**In:** the engine (schema-introspecting core library, in-memory index, the integrity loop `validate`/`check`/`stale` + `log`), `khub init`, the Claude Code skill, and the six-type engineering seed schema below.

**Out** — deferred and named: the full engineering preset and any preset beyond it; the SQLite/graph engine; `diff-preset` drift/promotion; hub↔engagement sync; the MCP server; facet ingestion (fast-follow #1); graph visualization and semantic search; `rename`; concurrency arbitration.

### The v1 seed schema (six types)

The seed schema is the smallest real engineering subset that exercises every engine mechanism. Its job is to prove the engine, not to model engineering completely. It doubles as the seed the engineering preset grows from.

| Type | Engine mechanism it proves |
|------|----------------------------|
| `Component` (kind: service/library/frontend/worker) | enum attribute; self-edge `depends_on` (blast radius); required relation `lives_in` Repository; `external_url` scalar; `source_id` alias |
| `APISchema` (protocol: rest/graphql/grpc/event) | the multi-predicate type-pair — `exposes` vs `consumes` (a predicate cannot be inferred from the target type) |
| `Repository` | the required-relation target → check-time completeness, `draft`/`active` |
| `Capability` | keystone; reached-by rule (`realizes` / `satisfies`); gap query |
| `Requirement` | gap query — a Requirement with no satisfying Capability |
| `Decision` (kind: adr/pdr) | supersession-chain query (`supersedes`); decision reach (`decides_on`) |

Together these exercise all four query families (blast radius, decision history, decision reach, gaps), required-relation completeness, multi-predicate and self-referential edges, enums, scalars, the `source_id` alias, slug minting, and the `draft`/`active` lifecycle.

## The engineering preset — target (post-v1)

The engineering preset is the first real operational setup the seed schema grows into: an **operational hub for building and maintaining a system**. It is a documented target — built post-v1 by hand and via the promote-back flywheel — not a v1 commitment.

The model is four verbs over an intent/behavior spine:

- **Spine:** `Requirement`, `Capability` (keystone). *Capability satisfies Requirement.*
- **Inventory** (what exists): `Domain` (grouping), `Component`, `APISchema`, `DataEntity`, `Repository`. *Component realizes Capability, belongs_to Domain, lives_in Repository, exposes/consumes APISchema, persists DataEntity, depends_on Component.*
- **Deliver** (what we build): `Spec`, `Epic`, `Story`. *Spec specifies Capability; Story implements Spec, part_of Epic.*
- **Decide** (why): `Decision (adr|pdr)`. *supersedes, decides_on.*
- **Operate** (what happened in prod): `Event (type: incident|outage|deployment|…)`, `PostMortem`, `Environment`. *Event affects Component, in Environment; PostMortem analyzes Event, produces Story/Decision.*
- **Verify:** `TestScenario` — *verifies Capability/Requirement; a PostMortem produces a regression TestScenario.*
- **People:** `Team` — *owned_by.*

The operational loop is the payoff the seed schema cannot yet show:

> incident `Event` → affects `Component` → `PostMortem` → produces `Decision (ADR)` → remediated by `Story` → changes `Component`.

khub records the **durable nodes**; live execution lives in the specialist tool — Story status in beads/GitHub, incident response in incident.io, test runs in CI — linked by `external_url`. khub is the record-of, not a live tracker. Deferred to a candidate tier (monotonic to add): `Constraint`, `Risk`, `Technology`, `TechnicalRecord`, `Actor`.

## Boundary and relationships

- **facet** seeds structural entities and supplies the ingestion format — the overlap is the ingestion path, not duplication. Loose coupling; khub reads facet output with no runtime dependency.
- **forge / beads / GitHub** own live delivery execution (work packages, sprint state, tickets). khub records the durable spec/epic/story/decision/incident nodes and links out by `external_url`.
- **firm-hq** is the working precedent for the projection-and-validation pattern; khub generalizes it (LinkML contract, schema-introspected checks, no graph engine in the MVP) and is itself the kind of operational hub an HQ preset would produce.

## Open questions (non-blocking)

1. **Agent retrieval surface.** v1's query layer plus the skill already let an agent pull schema-bound context. Whether agent-optimized retrieval / context-assembly becomes its own feature is deferred.
2. **"Operational setup" depth.** v1 reads the schema as ontology-level setup (types, relations, integrity, queries). Whether a schema should also configure operational procedures (workflows, agent routines) is a later question.
3. **Projection engine.** In-memory for v1; SQLite (nodes/edges + FTS) is the fast-follow past the performance budget; a graph engine only much later, if ever.
4. **id ↔ facet alignment.** khub mints its own slugs and aliases facet ids via `source_id`; which facet layer ingestion seeds from is settled at ingestion-build time.

## Next step

Engine and seed schema locked. Implementation proceeds from the build phases, owned by Noor.

