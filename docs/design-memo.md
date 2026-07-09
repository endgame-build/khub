# khub: Design Memo

**Status:** khub is structured, schema-bound context management for analytical and operational work, a domain-agnostic engine where the schema is the operational setup. The engine is proven on firm-hq's live firm-ops corpus.

## What It Is

khub is **structured, schema-bound context management for analytical and operational work**, a semantic, ontology-aligned context hub for agents. It gives an AI agent typed, validated, queryable context (structured memory it navigates and writes back to) instead of unstructured documents stuffed into a context window.

One generic engine: every entity is one Markdown file with YAML frontmatter, held in git. A khub schema defines the entity types, their attributes, and legal relations, and compiles to LinkML for validation. A Python core library provides schema-validated CRUD and graph queries. A generic CLI (`khub`) and a Claude Code skill are thin, schema-driven surfaces over that library. khub is an Open Knowledge Format (OKF) implementation and extension: its Markdown entities are OKF concepts, and khub adds a typed schema, a graph, and the serialization formats and collections OKF lacks on top of its Markdown-only model. Any workspace projects to a conformant OKF bundle.

**The schema is the operational setup.** It configures what a given hub is *for*. The engine knows nothing about engineering, consulting, or research; the schema does. Swap the schema, and the same engine becomes a different operational hub.

The product is two things at once:

1. A library of **canonical ontology presets, one per domain** (the operational setup): engineering (building and maintaining a system), consulting and analytical engagements, research, firm operations (HQ-style). Each preset is the firm's encoded judgment about how to model a kind of work. This is the IP.
2. A **per-engagement workspace**: each engagement gets its own repo seeded from a preset, where agents and humans author entities and extend the schema as the work demands.

## Who It Serves

The **agent is the primary consumer**. khub exists to be the structured context an agent operates from: it reads typed context to act, and writes its results back as typed entities. The **human** authors the schema (the operational setup) and the seed context, and reviews.

Both are first-class, symmetric writers of the same graph through the same library, with no propose-then-approve gate. The gate is the schema and git, not a human in the loop. Because the agent is a named user, the CLI and the Claude Code skill ship together; the skill is a required surface, not an afterthought.

## The Engine

| # | Layer | Tech |
|---|-------|------|
| 1 | Source of truth | Markdown + YAML frontmatter, git |
| 2 | Ontology | khub schema (entities/attributes/relations), compiled to LinkML |
| 3 | Core library | Python: validate / create / get / update / delete / link / query |
| 4 | Graph projection | In-memory index; in-memory per-invocation FTS5 search; persisted SQLite (nodes/edges) planned; no graph engine |
| 5 | Access | Generic CLI (`khub`) + Claude Code skill (MCP later) |

The core library is the only place logic lives. The CLI, skill, and any future MCP server are thin, schema-introspecting adapters over it. Adding or changing a type is an edit to the ontology, with no surface code changes.

### Schema

The schema is authored in khub's own vocabulary, not raw LinkML. A `base` block holds the attributes and relations every entity carries (`type`, `draft`, `author`, `created`/`updated`, `tags`, the OKF fields, the `any → any` edges); khub merges it into every entity at resolve time, so a type declares only its domain delta and may override a base attribute by redeclaring it. `entities` hold the types, each with `attributes` (scalars and enums), `relations` (typed edges, `to:` a single type, a list of types, or `any`), and storage config (`layout`/`format`; nesting is not yet supported). The base is not a declared dependency: `khub init` writes the `base` block as a header into the engagement's `schema.yaml`, alongside the preset's entities.

```yaml
# core.yaml
base:
  attributes: { type: {required: true}, draft: {type: bool, default: false},
                author: {}, created: {type: date, required: true}, updated: {type: date},
                title: {}, description: {}, resource: {}, tags: {type: list} }
  relations:  { related: {to: any, many: true}, sources: {to: any, many: true},
                references: {to: any, many: true}, depends_on: {to: any, many: true} }

# firm-ops.yaml  (the base is written in by `khub init`, not imported)
entities:
  client:  { layout: file,   attributes: { name: {required: true}, industry: {} } }
  project: { layout: folder, attributes: { stage: {enum: [diagnose, prove, scale, complete], required: true} },
             relations: { client: {to: client, required: true}, owner: {to: person, required: true} } }
```

khub compiles the resolved schema to LinkML, which generates the Pydantic models (validation) and JSON Schema (MCP tools, editors) into `.khub/generated/`. Operators and agents see only the khub vocabulary; LinkML is the generation backend.

### Technology Choices

The concrete stack under the five layers. Each pick stays dependency-light and earns its place; the projection layer is swappable because it is derived.

| Concern | Choice | Why |
|---------|--------|-----|
| Schema | **khub schema** → **LinkML** (`linkml`, `linkml-runtime`) | authors write entities/attributes/relations; khub compiles to LinkML, which generates Pydantic v2 and JSON Schema into `.khub/generated/` |
| Validation | **Pydantic v2**, generated from LinkML | a fast core, precise errors that feed `validate`, typed objects as the library's return type |
| Frontmatter | **`python-frontmatter`** to read, **`ruamel.yaml`** to write | round-trip writes preserve key order and comments, so `khub set` produces a minimal git diff |
| In-memory graph | **`networkx`** adjacency index | `descendants`/`ancestors` give blast radius and supersession chains; cycle detection backs `check` |
| Projection | **SQLite** + **FTS5** (stdlib `sqlite3`) | full-text search (`khub search`, in-memory per invocation, zero new dependency); persisted `nodes`/`edges` tables planned; HQ already proved the shape |
| Graph engine (if ever) | embedded graph engine (oxigraph or a kuzu fork) | considered and not adopted; kuzu was archived Oct 2025 (Apple acqui-hire), so a fork or oxigraph would be the path, and a server stays unjustified while the corpus is small |
| CLI | **Typer** + **Rich** | type-driven commands, `--format json` for the agent, trees and tables for a human |
| Git history | `git` over `subprocess` | `log` and `stale` read history; git is present, so no library dependency |
| Tooling | **uv**, **Ruff**, **mypy**, **pytest** | a golden-corpus test runs khub against an HQ snapshot and asserts it validates and checks cleanly (functional cutover, not byte-parity with `kb.py`) |

Python 3.11+, shipped as a `khub` console script (`uv tool install`). A Claude Code skill, a thin `SKILL.md` over the same commands, ships from this repo as a plugin; an MCP server exposing the same verbs is planned, its tools generated from the LinkML/Pydantic models so they validate for free.

Two eval tiers: deterministic golden-file tests cover the engine (the HQ functional-cutover test above), and an OKF-style fuzzy goldens-eval scores the LLM ingestion layer: precision and recall over extracted types and edges, gated on `khub check`.

### Principles (Invariants)

1. **Markdown is truth.** One entity equals one file in git. Audit, diff, PR review, and portability come for free.
2. **The graph is a derived projection,** rebuilt from the Markdown on demand. No graph database is ever the source of truth. History (`khub log`) is derived from git the same way.
3. **The schema is the contract.** Surfaces introspect the schema at runtime and never hardcode per-type knowledge.
4. **Relations are authoritative, from two sources.** A relation feeds the graph from an explicit role-named field the schema marks as an edge (the field name is the predicate, the value is the target), or from the derived inverse of such a field. Forward fields are stored single-sided on one entity; inverse edges are computed, never stored. (Deriving a parent edge from nested placement is not yet supported; a parent relation is an explicit edge.) Inline body links are navigational only.
5. **Structural integrity is not semantic truth.** khub guarantees an entity is well-formed and every relation resolves; it does not guarantee an assertion is correct. A schema-legal but false write validates. The backstop is attributable git history and `git revert`, not a gate.

### Authoring and Integrity

- **Identity.** Each entity's **id is its slug**: one bare, human-readable token (`auth`, `initech-pov`, `adr-0012`) that names the file or folder on disk and identifies the node in the graph. No type prefix. Uniqueness is per type, `(type, slug)`, with the file path as the globally-unique key; a deterministic suffix resolves within-type slug collisions. Typed relations resolve by their schema-known target type (`lives_in: api`); polymorphic (`any`-typed) relations take a bare slug too, qualified as `type/slug` only when a slug is ambiguous across types. An external identifier rides along as a non-authoritative `source_id` alias (the ingestion path). A preset may also declare integration routing keys (e.g. Recorder folder ids) as first-class attributes; the `source_id` alias is specifically for an external system's record id. Renaming a slug is not yet supported.
- **Storage layout is per-type config.** A type stores its entities as individual files or as a single-file collection. A preset sets the layout per type; an engagement can override it.

  Inventory as files (one entity per file):
  - `[inventory_name]/[item_name]/[slug].[md|json|jsonl|gjson|yaml]`
  - `[inventory_name]/[item_name]/_index.[md|json|jsonl|gjson|yaml]`
  - `[inventory_name]/[item_name].[md|json|jsonl|gjson|yaml]`

  Inventory as a single file (collection):
  - `[inventory_name].[json|jsonl|gjson|yaml]`
  - `[inventory_name]/[inventory_name].[json|jsonl|gjson|yaml]`
  - `[inventory_name]/_index.[json|jsonl|gjson|yaml]`

  An inventory sits at the workspace root. (Nesting an inventory under a parent item's folder, and deriving the parent edge from that placement, is not yet supported; the parent relation is an explicit frontmatter edge.)
- **Write rules.** Referential integrity hard-fails on write: a relation to a non-existent target is rejected. A missing required field or relation does not block capture: the entity is still written; completeness is enforced by `check`, not at write time.
- **Lifecycle.** Every entity carries a boolean `draft` flag (`draft: true|false`, default `false`), a manual publish switch, not a completeness verdict. `add` writes `draft: false` by default, `--draft` marks an entity unpublished, and `edit <id> draft true|false` toggles it; no verb auto-promotes or auto-demotes. Completeness is computed from the schema and enforced by `check`, never inferred from this flag, so a published entity can be incomplete (`check` reports it active-but-incomplete) and a draft can be complete. A draft is unpublished: excluded from required-completeness, and it never satisfies another entity's required relation.
- **Standard fields.** Beyond `type` and the `draft` flag, an entity may carry OKF's optional `title`, `description`, and `resource` (the canonical URI of the underlying asset, khub's link-out), plus `author` (the writer, person or agent), `tags`, and `created`/`updated`. Per-type fields and relations come from the schema. The firm-ops preset adds `owner` (→ person) as its own accountability edge, distinct from the core `author`.
- **The integrity loop** keeps the graph clean without manual policing. Draft status, orphan, and stale are core projection properties: every read includes drafts in scope and carries each entity's `stale`/`orphan` flag by default; `check`/`stale` gate on the same computation. The acceptance signals:
  - `khub validate`: per-entity well-formedness against the schema, plus referential integrity.
  - `khub check`: graph-wide over the active (`draft: false`) subgraph. Relations resolve, required-completeness holds for `active` entities (computed from the schema; an active-but-incomplete entity is reported), a `draft` does not satisfy a required relation, no orphans (entities with no inbound or outbound relation), no dangling edges, and no stray files (a file inside a type's layout that is not a valid entity of that type; reference docs outside the type layouts are skipped).
  - `khub stale`: entities whose `updated` is past a threshold; dates backfilled from `git log`.
  - `khub log`: git history rendered at ontology altitude (entities and relations, not files), for orientation without a gate.

### Command Surface

The CLI is a thin, schema-introspecting adapter over the core library's verbs: create, get, update, delete, link, query. Every read command emits `--format json` for the agent or a Rich table for a human. The Claude Code skill maps agent intent onto these same commands as the agent's retrieval surface: `query`, `get`, the graph walks (`neighbors`/`impact`/`history`), and `search`.

| Group | Command | Does |
|-------|---------|------|
| Workspace | `khub init <preset>` | scaffold a workspace from a preset |
| | `khub schema [types \| show <type> \| edges]` | introspect the active schema: types, fields, enums, edges, required relations |
| | `khub status` | counts per type, draft vs active, orphan and stale counts, OKF-conformance flag |
| Author | `khub add <type> [--field v …]` | mint a slug, write a well-formed entity (active by default; `--draft` to mark unpublished) |
| | `khub get <id>` | print an entity: frontmatter and body |
| | `khub edit <id> <field> <value>` | edit a field, bump `updated`, re-validate |
| | `khub link <id> <predicate> <target>` | add a schema-checked relation: legal predicate, target resolves, cardinality holds |
| | `khub unlink <id> <predicate> <target>` | remove a relation |
| | `khub remove <id>` | delete an entity; refuse when an inbound edge still resolves to it |
| Query | `khub query [--type --<field> …]` | filter entities by frontmatter |
| | `khub neighbors <id> [--predicate p] [--in\|--out]` | one-hop edges in either direction |
| | `khub impact <id> [--predicate p]` | blast radius: transitive closure over an edge |
| | `khub history <id>` | supersession chain and edit history |
| | `khub path <from> <to>` | shortest path between two entities |
| | `khub search <text>` | full-text search (FTS5, in-memory per invocation) |
| Integrity | `khub validate [path]` | per-entity well-formedness and referential integrity (default: whole workspace) |
| | `khub check` | graph-wide: relations resolve, required relations complete, no orphans, no stray files, no edge cycles |
| | `khub stale [--days N]` | entities past an `updated` threshold; dates backfilled from `git log` |
| | `khub log [<id>]` | git history at ontology altitude |
| Projection | `khub reindex` | regenerate the OKF `index.md` navigation from the graph |
| | `khub backfill [--type T]` | add missing frontmatter and dates from `git log` |
| | `khub viz` | self-contained Cytoscape HTML over the typed graph |
| | `khub build` | materialize the SQLite projection |
| | `khub export --okf [path]` | render the workspace to a conformant OKF bundle |
| | `khub diff-preset` | drift against the canonical preset, and promote-back |
| | `khub rename <id> <new-slug>` | rename a slug and rewrite inbound references |

## Deployment Model: Seeded Fork

The hub repo (this repo) holds the engine, the canonical presets, the skill, and an installer.

`khub init <preset>` scaffolds a fresh workspace. It merges the hub's `core.yaml` and `<preset>.yaml` into one self-contained, editable `.khub/schema.yaml`, then lays down the entity tree. After init the engagement carries no runtime dependency on the hub.

```
engagement-repo/
  .khub/
    config.yaml        # workspace config: preset provenance + version, source, command defaults (format, stale_days)
    schema.yaml        # core + preset flattened; the one file you edit
    generated/         # linkml + pydantic + json-schema, regenerated, gitignored
  clients/             # entity type folders live at the workspace root, one per type,
  projects/            #   each laid out per the type's storage config (see Authoring and
    <slug>/_index.md   #   Integrity): flat `clients/{slug}.md` or folder `projects/{slug}/_index.md`
  meetings/            # flat-layout type: one file per slug
  identity/team/       # person nodes; identity/ also holds untyped reference markdown
```

The schema header stamps provenance (`# khub-preset: engineering@1.0.0`). The engagement owns `schema.yaml` outright: editing that one file (add a type, change an enum, override a type's layout) is the entire override mechanism, with no runtime tie to the hub. Pulling a preset improvement down, or promoting an override back up, is a planned sync mechanism: a layered git-subtree merge (the preset as a subtree plus an overrides patch), with `diff-preset` reporting the delta. Flattening is the current model; layering is the planned path.

## Distribution

khub ships as a Python package and runs through `uv`. The zero-install path mirrors `npx`, straight from the private repo over git:

```
uvx --from git+ssh://git@github.com/endgame-build/khub@v0.4.1 khub init firm-ops ./my-hub
uv tool install git+ssh://git@github.com/endgame-build/khub@v0.4.1   # install once, then reuse
khub init engineering ./acme-hub
```

Everything stays private for now. `uvx` and `uv tool install` run from the private repo over git, authenticating with an SSH key or an HTTPS token (uv delegates auth to git). A public PyPI release and the bare `uvx khub` shorthand wait until there is a reason to open the engine.

**Engine and presets.** For now the engine and presets are collocated in this single private repo. The engine is generic plumbing; the presets are the IP, so they will most likely split into their own private repo later, pulled into `init` through `khub init --preset-source <private>`. Open-core (a public engine with private presets) stays a later option.

**Skill.** khub ships its agent skill from this repo two ways. For Claude Code, the repo is its own single-plugin marketplace: `claude plugins add` the repo, install `khub@khub`, then `/khub:setup` installs the CLI and sets up the project. For every other agent (Cursor, Codex, Gemini CLI, and ~70 more), the skill installs through Vercel's `npx skills`, the same convention Neon's `neon init` uses and the same call `khub init` makes under the hood. The skill is the agent's surface over the CLI verbs; `khub wire` links the schema into a project's `CLAUDE.md` so an agent reasons in the ontology even without the CLI.

## The Proving Ground

The **engine** is proven on the real thing: **firm-hq** cut over to khub. The proving ground is HQ's live firm-operations corpus, roughly 380 entities across 9 types, already running the projection-and-validation pattern under `kb.py`. khub runs read-only against the same files, then takes over, a functional cutover, not byte-parity with `kb.py`. Markdown is truth, so the risk stays low: khub never owns the data, the `.md` files go untouched, and the incumbent keeps working until cutover.

The cutover exercises the whole engine: the schema-introspecting core library, the in-memory `networkx` index, the integrity loop (`validate`/`check`/`stale` + `log`), plus `reindex` and `backfill` for the HQ migration; the full author and query command surface; `khub init`; and the **firm-ops preset**, the LinkML port of `hq.schema.yml` (9 types, 14 relation predicates), captured in full in `firm-ops-preset.md`.

Not yet built, and named: the engineering preset and any preset beyond firm-ops; the persisted SQLite/graph projection; `diff-preset` drift/promotion; hub↔engagement sync; the MCP server; facet and OKF-bundle ingestion; `rename`; concurrency arbitration.

### HQ Firm-Ops: Engine Coverage

The firm-ops schema is the real engine test. It exercises most of the engine's mechanisms on real data and at real scale, plus several a synthetic seed never would. The full entity, property, and relation capture lives in `firm-ops-preset.md`; the coverage map:

| Engine mechanism | Where HQ exercises it |
|------------------|------------------------|
| enums, scalars, `source_id` alias | `stage`/`call_type`/`role`/`phase` enums; `external_repo`/`website`; `crm_id`/`note_id` aliases |
| required relations → `check` completeness | `owner` (most types), `client` (engagements), `engagement` (meetings) |
| blast radius (transitive) | `depends_on` |
| multi-predicate over one type-pair | `owner` vs `team` (both → person, a predicate cannot be inferred from the target type) |
| union- and `any`-typed edges + `type/slug` | `engagement` (union → opportunity\|project\|partnership); `related`/`sources`/`depends_on` (`any`) |
| mixed storage layout | flat `clients/{slug}.md` vs folder `projects/{slug}/_index.md` |
| explicit union edge (nesting not yet supported) | `meeting` flat at `meetings/{slug}.md`; `engagement` an explicit union edge |
| the `draft` flag (`draft: true\|false`) | added by khub over HQ's per-type `stage`/`status` |
| real scale and mess | ~380 entities, plus reference docs with no frontmatter to skip cleanly |

The one family HQ leaves uncovered is the intent/behavior **satisfies-gap**: a Requirement with no Capability (engineering-specific, arriving with the engineering preset). Self-referential and derived-inverse edges (`supersedes`/`superseded_by`) also moved there when `decision` was folded out of firm-ops; the engine still supports them, exercised by the generic resolver/compiler tests. HQ's gap query is structural instead: orphans and missing required relations, both surfaced by `check`.

## The Engineering Preset

The engineering preset is the second operational setup, built after firm-ops: an **operational hub for building and maintaining a system**. It is a documented target, built by hand and via the promote-back flywheel.

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

khub records the **durable nodes**; live execution lives in the specialist tool (Story status in beads/GitHub, incident response in incident.io, test runs in CI), linked by `resource`. khub records durable state; the specialist tool tracks live execution. Candidate additions (monotonic to add): `Constraint`, `Risk`, `Technology`, `TechnicalRecord`, `Actor`.

## Boundary and Relationships

- **facet** seeds structural entities and supplies the ingestion format; the overlap is only the ingestion path. Coupling stays loose: khub reads facet output with no runtime dependency.
- **forge / beads / GitHub** own live delivery execution (work packages, sprint state, tickets). khub records the durable spec/epic/story/decision/incident nodes and links out by `resource`.
- **firm-hq** is the working precedent for the projection-and-validation pattern; khub generalizes it (LinkML contract, schema-introspected checks, no graph engine) and is itself the kind of operational hub an HQ preset would produce.
- **OKF (Open Knowledge Format)** is the vendor-neutral substrate khub speaks (Google, v0.1: a git tree of `.md` concepts with a required `type`, cross-links, `index.md`, `log.md`). khub's Markdown entities are conformant OKF concepts, so khub is an OKF implementation and extension: it adds a typed schema, typed relations, a `draft` flag, and the graph. Extra serialization formats and collections (which OKF lacks) go further: per-entity `json`/`yaml` (a single mapping; prose rides in a reserved `body` field) and single-file collections (`layout: collection`, `format: json|jsonl|yaml`, row-level entities; `docs/collections-design.md` holds the row-model contract). `gjson` remains named-but-undefined; the schema rejects it. Markdown entities carry the extras as OKF-tolerated frontmatter; any workspace projects to a conformant OKF bundle. khub adopts OKF's optional `title`, `description`, and `resource` fields and emits OKF `index.md` (stamped `okf_version`) from `reindex`. Reading external OKF bundles permissively as drafts is a planned consume-side path alongside facet ingestion (schema-validated and one-directional; khub is record-of and writes nothing back to the source). It does not adopt OKF's conventional body sections; relations stay typed in frontmatter. The `status` OKF-conformance flag reports whether the workspace would project to a valid OKF bundle, the conditions `export --okf` requires (every entity carries `type`, relations resolve, an `index.md` generates), rather than whether the on-disk tree is already all-Markdown.

## Open Questions (Non-Blocking)

1. **"Operational setup" depth.** khub reads the schema as ontology-level setup (types, relations, integrity, queries). Whether a schema should also configure operational procedures (workflows, agent routines) is a later question.
2. **Projection engine.** In-memory today, including `khub search`'s per-invocation FTS5 index; a persisted SQLite projection (nodes/edges, cached FTS) is planned past the performance budget; a graph engine only much later, if ever.
3. **id ↔ facet alignment.** khub mints its own slugs and aliases facet ids via `source_id`; which facet layer ingestion seeds from is settled at ingestion-build time.

## Next Step

Engine locked, firm-ops preset captured. Implementation proceeds from the build phases, owned by Noor.
