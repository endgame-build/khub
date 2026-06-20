# Knowledge Hub (khub): Design Memo

**Status:** engine locked, 2026-06-17; identity and storage-layout model refined, 2026-06-18; v1 proving ground moved to the live HQ firm-ops corpus, 2026-06-18. khub is structured, schema-bound context management for analytical and operational work, a domain-agnostic engine where the schema is the operational setup. v1 proves the engine by cutting firm-hq over to khub on the firm-ops schema; the engineering ontology stays a post-v1 target.

## What It Is

khub is **structured, schema-bound context management for analytical and operational work**, a semantic, ontology-aligned context hub for agents. It gives an AI agent typed, validated, queryable context (structured memory it navigates and writes back to) instead of unstructured documents stuffed into a context window.

One generic engine: every entity is one Markdown file with YAML frontmatter, held in git. A LinkML ontology defines the types, attributes, and legal relations. A Python core library provides schema-validated CRUD and graph queries. A generic CLI (`khub`) and a Claude Code skill are thin, schema-driven surfaces over that library. khub is an Open Knowledge Format (OKF) implementation and extension: its Markdown entities are OKF concepts, and khub adds a typed schema, a graph, and extra serialization formats on top. Any workspace projects to a conformant OKF bundle.

**The schema is the operational setup.** It configures what a given hub is *for*. The engine knows nothing about engineering, consulting, or research; the schema does. Swap the schema, and the same engine becomes a different operational hub.

The product is two things at once:

1. A library of **canonical ontology presets, one per domain** (the operational setup): engineering (building and maintaining a system), consulting and analytical engagements, research, firm operations (HQ-style). Each preset is the firm's encoded judgment about how to model a kind of work. This is the IP.
2. A **per-engagement workspace**: each engagement gets its own repo seeded from a preset, where agents and humans author entities and extend the schema as the work demands.

## Who It Serves

The **agent is the primary consumer**. khub exists to be the structured context an agent operates from: it reads typed context to act, and writes its results back as typed entities. The **human** authors the schema (the operational setup) and the seed context, and reviews.

Both are first-class, symmetric writers of the same graph through the same library, with no propose-then-approve gate. The gate is the schema and git, not a human in the loop. Because the agent is a named user, the CLI and the Claude Code skill ship together in v1; the skill is a v1 requirement.

## The Engine

| # | Layer | Tech |
|---|-------|------|
| 1 | Source of truth | Markdown + YAML frontmatter, git |
| 2 | Ontology | LinkML (YAML), the contract |
| 3 | Core library | Python: validate / create / get / update / delete / link / query |
| 4 | Graph projection | In-memory index (v1); SQLite (nodes/edges + FTS) as fast-follow; no graph engine in v1 |
| 5 | Access | Generic CLI (`khub`) + Claude Code skill (MCP later) |

The core library is the only place logic lives. The CLI, skill, and any future MCP server are thin, schema-introspecting adapters over it. Adding or changing a type is an edit to the ontology, with no surface code changes.

### Technology Choices

The concrete stack under the five layers. Each pick stays dependency-light and earns its place; the projection layer is swappable because it is derived.

| Concern | Choice | Why |
|---------|--------|-----|
| Schema | **LinkML** (`linkml`, `linkml-runtime`) | one contract generates Pydantic v2 models and JSON Schema into `generated/`; validation comes from the schema |
| Validation | **Pydantic v2**, generated from LinkML | a fast core, precise errors that feed `validate`, typed objects as the library's return type |
| Frontmatter | **`python-frontmatter`** to read, **`ruamel.yaml`** to write | round-trip writes preserve key order and comments, so `khub set` produces a minimal git diff |
| In-memory graph (v1) | **`networkx`** adjacency index | `descendants`/`ancestors` give blast radius and supersession chains; cycle detection backs `check` |
| Projection (fast-follow) | **SQLite** + **FTS5** (stdlib `sqlite3`) | `nodes`/`edges` tables and full-text search, zero new dependency; HQ already proves the shape |
| Graph engine (if ever) | kuzu / oxigraph, embedded | considered and deferred; a server stays unjustified while the corpus is small |
| CLI | **Typer** + **Rich** | type-driven commands, `--format json` for the agent, trees and tables for a human |
| Git history | `git` over `subprocess` | `log` and `stale` read history; git is present, so no library dependency |
| Tooling | **uv**, **Ruff**, **mypy**, **pytest** | a golden-corpus test runs khub against an HQ snapshot and asserts parity with `kb.py` |

Python 3.11+, shipped as a `khub` console script (`uv tool install`). The Claude Code skill is a thin `SKILL.md` over the same commands; an MCP server exposing the same verbs as tools comes later.

### Principles (Invariants)

1. **Markdown is truth.** One entity equals one file in git. Audit, diff, PR review, and portability come for free.
2. **The graph is a derived projection,** rebuilt from the Markdown on demand. No graph database is ever the source of truth. History (`khub log`) is derived from git the same way.
3. **The schema is the contract.** Surfaces introspect the schema at runtime and never hardcode per-type knowledge.
4. **Relations are authoritative, from three sources.** A relation feeds the graph from an explicit role-named field the schema marks as an edge (the field name is the predicate, the value is the target), from the derived inverse of such a field, or from a nested entity's placement, where living under a parent item's folder derives the parent edge from the path. Forward fields are stored single-sided on one entity; inverse and placement-derived edges are computed, never stored. Inline body links are navigational only.
5. **Structural integrity is not semantic truth.** khub guarantees an entity is well-formed and every relation resolves; it does not guarantee an assertion is correct. A schema-legal but false write validates. The backstop is attributable git history and `git revert`, not a gate.

### Authoring and Integrity

- **Identity.** Each entity's **id is its slug**: one bare, human-readable token (`auth`, `initech-pov`, `adr-0012`) that names the file or folder on disk and identifies the node in the graph. No type prefix. Uniqueness is per type, `(type, slug)`, with the file path as the globally-unique key; a deterministic suffix resolves within-type slug collisions. A nested entity's id is hierarchical, `{parent-slug}/{slug}`, unique within its parent, so its path and id stay isomorphic. Typed relations resolve by their schema-known target type (`lives_in: api`); polymorphic (`any`-typed) relations take a bare slug too, qualified as `type/slug` only when a slug is ambiguous across types. An external identifier rides along as a non-authoritative `source_id` alias (the ingestion path). Renaming a slug is deferred.
- **Storage layout is per-type config.** A type stores its entities as individual files or as a single-file collection. A preset sets the layout per type; an engagement can override it.

  Inventory as files (one entity per file):
  - `[inventory_name]/[item_name]/[slug].[md|json|jsonl|gjson|yaml]`
  - `[inventory_name]/[item_name]/_index.[md|json|jsonl|gjson|yaml]`
  - `[inventory_name]/[item_name].[md|json|jsonl|gjson|yaml]`

  Inventory as a single file (collection):
  - `[inventory_name].[json|jsonl|gjson|yaml]`
  - `[inventory_name]/[inventory_name].[json|jsonl|gjson|yaml]`
  - `[inventory_name]/_index.[json|jsonl|gjson|yaml]`

  An inventory sits at the knowledge root or **nests under a parent item's folder**, where the same patterns apply re-rooted:
  - `[parent_inventory]/[parent_item]/[inventory_name]/[item_name].[md|json|jsonl|gjson|yaml]` (plus the `_index` and collection variants)

  A nested inventory declares its parent type and the edge its placement encodes; the engine derives that edge from the path, so the parent relation needs no frontmatter field.
- **Write rules.** Referential integrity hard-fails on write: a relation to a non-existent target is rejected. An incomplete but well-formed entity is saved as a `draft`, so capture is never blocked.
- **Lifecycle.** Every entity carries `status: draft|active`. `check` enforces required-relation completeness over the active subgraph only: a `draft` does not satisfy another entity's required relation.
- **Standard fields.** Beyond `type` and `status`, an entity may carry OKF's optional `title`, `description`, and `resource` (the canonical URI of the underlying asset, khub's link-out), plus `tags` and `created`/`updated`. Per-type fields and relations come from the schema.
- **The integrity loop** keeps the graph clean without manual policing. The v1 acceptance signals:
  - `khub validate`: per-entity well-formedness against the schema, plus referential integrity.
  - `khub check`: graph-wide. Relations resolve, required relations complete for `active` entities, no orphans, and no stray files.
  - `khub stale`: entities whose `updated` is past a threshold; dates backfilled from `git log`.
  - `khub log`: git history rendered at ontology altitude (entities and relations, not files), for orientation without a gate.

### Command Surface

The CLI is a thin, schema-introspecting adapter over the core library's verbs: create, get, update, delete, link, query. Every read command emits `--format json` for the agent or a Rich table for a human. The Claude Code skill maps agent intent onto these same commands.

| Group | Command | Does | Tier |
|-------|---------|------|------|
| Workspace | `khub init <preset>` | scaffold a workspace from a preset | v1 |
| | `khub schema [types \| show <type> \| edges]` | introspect the active schema: types, fields, enums, edges, required relations | v1 |
| Author | `khub new <type> [--field v …]` | mint a slug, write a well-formed (possibly `draft`) entity | v1 |
| | `khub get <id>` | print an entity: frontmatter and body | v1 |
| | `khub set <id> <field> <value>` | edit a field, bump `updated`, re-validate | v1 |
| | `khub link <id> <predicate> <target>` | add a schema-checked relation: legal predicate, target resolves, cardinality holds | v1 |
| | `khub unlink <id> <predicate> <target>` | remove a relation | v1 |
| | `khub rm <id>` | delete an entity; refuse when an inbound edge still resolves to it | v1 |
| Query | `khub query [--type --<field> …]` | filter entities by frontmatter | v1 |
| | `khub neighbors <id> [--predicate p] [--in\|--out]` | one-hop edges in either direction | v1 |
| | `khub impact <id> [--predicate p]` | blast radius: transitive closure over an edge | v1 |
| | `khub history <id>` | supersession chain and edit history | v1 |
| | `khub search <text>` | full-text search | fast-follow |
| Integrity | `khub validate [path \| --all]` | per-entity well-formedness and referential integrity | v1 |
| | `khub check` | graph-wide: relations resolve, required relations complete, no orphans, no stray files, no edge cycles | v1 |
| | `khub stale [--days N]` | entities past an `updated` threshold; dates backfilled from `git log` | v1 |
| | `khub log [<id>]` | git history at ontology altitude | v1 |
| Projection | `khub reindex` | regenerate the OKF `index.md` navigation from the graph | v1 (HQ parity) |
| | `khub backfill [--type T]` | add missing frontmatter and dates from `git log` | v1 (HQ parity) |
| | `khub build` | materialize the SQLite projection | fast-follow |
| | `khub diff-preset` | drift against the canonical preset, and promote-back | deferred |
| | `khub rename <id> <new-slug>` | rename a slug and rewrite inbound references | deferred |

## Deployment Model: Seeded Fork

The hub repo (this repo) holds the engine, the canonical presets, the skill, and an installer.

`khub init <preset>` scaffolds a fresh workspace. It flattens the chosen preset (with `core` merged in) into one self-contained, editable schema, then lays down the entity tree.

```
client-repo/
  .khub/
    config.yaml
    schema.yaml        # seeded from the preset, edit freely
    generated/         # json-schema + pydantic, regenerated, gitignored
  knowledge/
    <type-folders>/    # entities per the type's storage layout (see Authoring and Integrity)
```

The schema header stamps provenance (`# khub-preset: engineering@1.0.0`). The engagement schema is an editable fork; the canonical preset stays pristine in the hub. Drift detection and promote-back (`diff-preset`) and hub↔engagement sync reuse atelier's git-subtree pattern; both stay deferred past v1.

## v1 Scope

Prove the **engine** on the real thing: cut **firm-hq** over to khub. The proving ground is HQ's live firm-operations corpus, roughly 380 entities across 12 types, already running the projection-and-validation pattern under `kb.py`. khub reaches parity with `kb.py` running read-only against the same files, then takes over. Markdown is truth, so the risk stays low: khub never owns the data, the `.md` files go untouched, and the incumbent keeps working until cutover.

**In:** the engine (schema-introspecting core library, in-memory `networkx` index, the integrity loop `validate`/`check`/`stale` + `log`, plus `reindex` and `backfill` for HQ parity); the full author and query command surface; `khub init`; the Claude Code skill; and the **firm-ops preset**, the LinkML port of `hq.schema.yml` (12 types, 19 edges), captured in full in `firm-ops-preset.md`.

**Out** (deferred and named): the engineering preset and any preset beyond firm-ops; the SQLite/graph projection and FTS search; `diff-preset` drift/promotion; hub↔engagement sync; the MCP server; facet and OKF-bundle ingestion (fast-follow #1); graph visualization; `rename`; concurrency arbitration.

### The v1 Proving Ground: HQ Firm-Ops

The firm-ops schema is the real engine test. It exercises every mechanism the engine has, on real data and at real scale, plus several mechanisms a synthetic seed never would. The full entity, property, and relation capture lives in `firm-ops-preset.md`; the coverage map:

| Engine mechanism | Where HQ exercises it |
|------------------|------------------------|
| enums, scalars, `source_id` alias | `stage`/`status`/`call_type`/`role`/`doc_kind` enums; `external_repo`/`website`; `crm_id`/`airtable_id`/`note_id` aliases |
| required relations → `check` completeness | `owner` (most types), `client` (engagements), `engagement` (meetings) |
| self-referential edge | `supersedes` (decision → decision) |
| blast radius (transitive) | `depends_on`, `affects` |
| decision history and reach | `supersedes` chain (inverse derived); `affects` |
| multi-predicate over one type-pair | `owner` vs `team` (both → person, a predicate cannot be inferred from the target type) |
| polymorphic (`any`-typed) edges + `type/slug` | `engagement` (→ opportunity\|project\|build\|partnership); `affects`/`related`/`sources` |
| mixed storage layout | flat `clients/{slug}.md` vs folder `projects/{slug}/_index.md` |
| path-shared types | `project` and `build` share `projects/{slug}/`, discriminated by `type` |
| nested inventory, edge from placement | `meeting` under `projects/{slug}/meetings/`; `engagement` derived from the path |
| `draft`/`active` lifecycle | added by khub over HQ's per-type `stage`/`status` |
| real scale and mess | ~380 entities, plus reference docs with no frontmatter to skip cleanly |

The one family HQ leaves uncovered is the intent/behavior **satisfies-gap**: a Requirement with no Capability. That spine is engineering-specific and arrives with the engineering preset. HQ's gap query is structural instead: orphans and missing required relations, both surfaced by `check`.

## The Engineering Preset: Target (Post-v1)

The engineering preset is the second operational setup, built after firm-ops: an **operational hub for building and maintaining a system**. It is a documented target, built post-v1 by hand and via the promote-back flywheel, not a v1 commitment.

The model is four verbs over an intent/behavior spine:

- **Spine:** `Requirement`, `Capability` (keystone). *Capability satisfies Requirement.*
- **Inventory** (what exists): `Domain` (grouping), `Component`, `APISchema`, `DataEntity`, `Repository`. *Component realizes Capability, belongs_to Domain, lives_in Repository, exposes/consumes APISchema, persists DataEntity, depends_on Component.*
- **Deliver** (what we build): `Spec`, `Epic`, `Story`. *Spec specifies Capability; Story implements Spec, part_of Epic.*
- **Decide** (why): `Decision (adr|pdr)`. *supersedes, decides_on.*
- **Operate** (what happened in prod): `Event (type: incident|outage|deployment|…)`, `PostMortem`, `Environment`. *Event affects Component, in Environment; PostMortem analyzes Event, produces Story/Decision.*
- **Verify:** `TestScenario`. *Verifies Capability/Requirement; a PostMortem produces a regression TestScenario.*
- **People:** `Team`. *owned_by.*

The operational loop is the payoff the firm-ops schema does not carry:

> incident `Event` → affects `Component` → `PostMortem` → produces `Decision (ADR)` → remediated by `Story` → changes `Component`.

khub records the **durable nodes**; live execution lives in the specialist tool (Story status in beads/GitHub, incident response in incident.io, test runs in CI), linked by `resource`. khub records durable state; the specialist tool tracks live execution. Deferred to a candidate tier (monotonic to add): `Constraint`, `Risk`, `Technology`, `TechnicalRecord`, `Actor`.

## Boundary and Relationships

- **facet** seeds structural entities and supplies the ingestion format; the overlap is only the ingestion path. Coupling stays loose: khub reads facet output with no runtime dependency.
- **forge / beads / GitHub** own live delivery execution (work packages, sprint state, tickets). khub records the durable spec/epic/story/decision/incident nodes and links out by `resource`.
- **firm-hq** is the working precedent for the projection-and-validation pattern; khub generalizes it (LinkML contract, schema-introspected checks, no graph engine in v1) and is itself the kind of operational hub an HQ preset would produce.
- **OKF (Open Knowledge Format)** is the vendor-neutral substrate khub speaks (Google, v0.1: a git tree of `.md` concepts with a required `type`, cross-links, `index.md`, `log.md`). khub's Markdown entities are conformant OKF concepts, so khub is an OKF implementation and extension: it adds a typed schema, typed relations, a `draft|active` lifecycle, the graph engine, and extra serialization formats (YAML/JSON/JSONL/gjson and collections, which OKF lacks). Markdown entities carry the extras as OKF-tolerated frontmatter; any workspace projects to a conformant OKF bundle. khub adopts OKF's optional `title`, `description`, and `resource` fields, emits OKF `index.md` (stamped `okf_version`) from `reindex`, and reads external OKF bundles permissively as drafts, a consume-side fast-follow with facet ingestion. It does not adopt OKF's conventional body sections; relations stay typed in frontmatter.

## Open Questions (Non-Blocking)

1. **Agent retrieval surface.** v1's query layer plus the skill already let an agent pull schema-bound context. Whether agent-optimized retrieval / context-assembly becomes its own feature is deferred.
2. **"Operational setup" depth.** v1 reads the schema as ontology-level setup (types, relations, integrity, queries). Whether a schema should also configure operational procedures (workflows, agent routines) is a later question.
3. **Projection engine.** In-memory for v1; SQLite (nodes/edges + FTS) is the fast-follow past the performance budget; a graph engine only much later, if ever.
4. **id ↔ facet alignment.** khub mints its own slugs and aliases facet ids via `source_id`; which facet layer ingestion seeds from is settled at ingestion-build time.

## Next Step

Engine locked, firm-ops preset captured. Implementation proceeds from the build phases, owned by Noor.
