# khub: Design Memo

## What It Is

khub is **structured, schema-bound context management for analytical and operational work**, a semantic, ontology-aligned context hub for agents. It gives an AI agent typed, validated, queryable context (structured memory it navigates and writes back to) instead of unstructured documents stuffed into a context window.

One generic engine: every entity is a Markdown file with YAML frontmatter, a `.json`/`.yaml` document, or a row in a single-file collection, held in git. A khub schema defines the entity types, their attributes, and legal relations, and is resolved in memory for validation. A Go core provides schema-validated CRUD and graph queries. A generic CLI (`khub`) and an agent skill are thin, schema-driven surfaces over that core. khub is an Open Knowledge Format (OKF) implementation and extension: its Markdown entities are OKF concepts, and khub adds a typed schema, a graph, and the serialization formats and collections OKF lacks on top of its Markdown-only model. Any workspace projects to a conformant OKF bundle.

**The schema is the operational setup.** It configures what a given hub is *for*. The engine knows nothing about engineering, consulting, or research; the schema does. Swap the schema, and the same engine becomes a different operational hub.

The product is two things at once:

1. A library of **canonical ontology presets** (the operational setup): `firm-ops`, `build-hub`, and the smaller `build-lite`. Each preset encodes a judgment about how to model one kind of work.
2. A **per-engagement workspace**: each engagement gets its own repo seeded from a preset, where agents and humans author entities and extend the schema as the work demands.

## Who It Serves

The **agent is the primary consumer**. khub exists to be the structured context an agent operates from: it reads typed context to act, and writes its results back as typed entities. The **human** authors the schema (the operational setup) and the seed context, and reviews.

Both are first-class, symmetric writers of the same graph through the same library, with no propose-then-approve gate: the schema and git are the gate. Because the agent is a named user, the agent skill is a required surface: it ships from the same repo as the CLI, in the Claude Code plugin and through `npx skills add`.

## The Engine

| # | Layer | Tech |
|---|-------|------|
| 1 | Source of truth | Markdown + YAML frontmatter, git |
| 2 | Ontology | khub schema (entities/attributes/relations), resolved in memory |
| 3 | Core | Go (`internal/`): validate / create / get / update / delete / link / query |
| 4 | Graph projection | In-memory ordered index; in-memory per-invocation FTS5 search; no persisted index or graph engine |
| 5 | Access | Generic CLI (`khub`) + agent skill; read-only loopback graph UI for humans |

The core is the only place logic lives. The CLI and skill are thin, schema-introspecting adapters over it. Adding or changing a type is an edit to the ontology, with no surface code changes.

### Schema

The schema is authored in khub's own vocabulary, not in a description logic. This
is the deliberate inversion of the usual ontology workflow, and the reason is well
documented: domain experts "are rarely versed in model or ontology development, and
do not know the formal languages or logic that express ontological concepts," and
asking one to work in OWL "may result in errors or omissions, or in the expert
becoming frustrated and losing interest entirely" (Westerinen & Tauber, *Ontology
Development by Domain Experts (Without Using the "O" Word)*, Applied Ontology, IOS
Press). khub's schema is the
rendering that fits how the expert works; any RDF/OWL projection is derived from it
and never the authoring surface. The schema is split into three layer files — `ontology.yaml` (the domain: per-type `attributes`, `relations`, `when`), `policy.yaml` (this workspace's gates: `required`, `orphan`) and `storage.yaml` (`layout`/`format`/`path`/`id_prefix`/`template`) — merging at load time into one resolved contract. The `base` block (`type`, `draft`, `author`, `created`/`updated`, `tags`, the OKF fields, the `any → any` edges) is khub's own plumbing and ships EMBEDDED in the binary, supplied to every resolve and never copied into a workspace; an authored `ontology.base` is rejected outright — the base is not an authoring surface. A type declares only its domain delta and overrides a base attribute by redeclaring it. The split is what makes the ontology projectable: `ontology.yaml` carries purely the domain, which is what an RDF/SHACL export reads.

```yaml
# core/ontology.yaml  (embedded in the binary — supplied to every resolve, never copied)
ontology:
  base:
    attributes: { type: {required: true}, draft: {type: bool, default: false},
                  author: {}, created: {type: date, required: true}, updated: {type: date},
                  title: {}, description: {}, resource: {}, tags: {type: list} }
    relations:  { related: {to: any, many: true}, sources: {to: any, many: true},
                  references: {to: any, many: true}, depends_on: {to: any, many: true, acyclic: true} }

# firm-ops/ontology.yaml — purely the domain
ontology:
  entities:
    client:  { attributes: { name: {required: true}, industry: {} } }
    project: { attributes: { stage: {enum: [diagnose, prove, scale, complete], required: true} },
               relations: { client: {to: client, required: true}, owner: {to: person, required: true} } }

# firm-ops/storage.yaml — where bytes land
storage:
  client:  { layout: file,   path: clients }
  project: { layout: folder, path: projects }
```

khub validates natively from the resolved schema: `internal/schema/vocab.go` parses and checks the schema vocabulary, then shared runtime field and relation checks enforce it. There is no Pydantic runtime or separate compilation backend.

### Technology Choices

The concrete stack under the five layers. Each pick stays dependency-light and earns its place; the projection layer is swappable because it is derived.

| Concern | Choice | Why |
|---------|--------|-----|
| Schema | khub YAML → `ResolvedSchema` (the native resolver) | authors write entities/attributes/relations; the resolver merges the base and produces the in-memory contract every surface reads |
| Validation | native (`internal/schema/vocab.go` meta-schema + runtime field/relation checks) | precise errors that feed `validate`; no generated code |
| Frontmatter | **`internal/canon`** — **`goccy/go-yaml`** parses, khub's own emitter writes | an edit rewrites only the tokens whose values moved, so key order, comments and the author's quoting all survive and `khub edit` produces a minimal git diff. The emitter reproduces ruamel.yaml byte-for-byte because the on-disk shape is a contract, not a preference |
| In-memory graph | khub's own ordered adjacency; **`gonum`** for strongly connected components | `impact`/`history` give blast radius and supersession chains; `check` reports one deterministic real cycle witness per cyclic component. Adjacency is insertion-ordered because walk determinism is contract |
| Projection | **FTS5** via **`ncruces/go-sqlite3`** | full-text search (`khub search`) builds one in-memory index per invocation and never goes stale. A pure-Go translated build keeps CGO off and all release targets cross-compile without a C toolchain |
| CLI | **`cobra`** + **`pflag`** | declarative commands, `--format json` for the agent, trees and tables for a human; the panel and table rendering is khub's own |
| Git history | `git` subprocess | `stale` and `backfill` batch file history reads where possible; git is present, so no library dependency |
| Tooling | **Go**, **gofmt**, **go vet**, **golangci-lint**, `go test` | a golden-corpus test runs khub against a firm-ops corpus snapshot and asserts it validates and checks cleanly (a functional cutover, judged on its own output) |

Go, shipped as a single static binary. It was a Python console script through 0.18.0; the rewrite removed the interpreter from every install. Agent skills, thin `SKILL.md` files over the same commands, ship apart from the binary: in the Claude Code plugin under `plugin/`, and through `npx skills add` for other agents.
### Principles (Invariants)

1. **Markdown is truth.** One entity equals one file in git. Audit, diff, PR review, and portability come for free.
2. **The graph is a derived projection,** rebuilt from the Markdown on demand. No graph database is ever the source of truth. Git-derived dates (`khub stale`) are read the same way.
3. **The schema is the contract.** Surfaces introspect the schema at runtime and never hardcode per-type knowledge.
4. **Relations are authoritative, from two sources.** A relation feeds the graph from an explicit role-named field the schema marks as an edge (the field name is the predicate, the value is the target), or from the derived inverse of such a field. Forward fields are stored single-sided on one entity; inverse edges are computed, never stored. (Deriving a parent edge from nested placement is not yet supported; a parent relation is an explicit edge.) Inline body links are navigational only.
5. **Structural integrity is not semantic truth.** khub guarantees an entity is well-formed and every relation resolves; it does not guarantee an assertion is correct. A schema-legal but false write validates. The backstop is attributable git history and `git revert`.

### Authoring and Integrity

- **Identity.** Each entity's **id is its slug**: one bare, human-readable token (`auth`, `initech-pov`) that names the file or folder on disk and identifies the node in the graph. No type prefix. Uniqueness is per type, `(type, slug)`, with the file path as the globally-unique key; a within-type collision is refused rather than suffixed, because minting reads no siblings — an id is a function of the schema and the entity's own fields, nothing else on disk. Typed relations resolve by their schema-known target type (`lives_in: api`); polymorphic (`any`-typed) relations take a bare slug too, qualified as `type/slug` only when a slug is ambiguous across types. An entity may declare other names in `aliases`: every ID lookup falls back to them once the slug misses, a write target named by one is stored as the real slug, and `check` fails an alias that names two entities. An external identifier rides along as a non-authoritative `source_id` alias (the ingestion path). A preset may also declare integration routing keys (e.g. a notes-tool folder id) as first-class attributes; the `source_id` alias is specifically for an external system's record id. Renaming a slug is not yet supported.
- **Storage layout is per-type config.** A type stores its entities as individual files or as a single-file collection. A preset sets the layout per type; an engagement can override it. Where bytes land is storage, not ontology, so moving an inventory is a `storage.yaml` edit plus a file move, which leaves the domain — and anything projected from it — untouched. How to declare layouts and path templates is in [`schema.md`](schema.md#storage-config-storageyaml); the on-disk contract for entity formats and collections is in [`cli.md`](cli.md#entity-formats), with the collection row model in [`collections-design.md`](collections-design.md). Placement never derives an edge: a parent relation is an explicit frontmatter edge, even where a path template nests a child under its parent's directory.
- **Write rules.** Referential integrity hard-fails on write: a relation to a non-existent target is rejected. A missing required field or relation does not block capture: the entity is still written; completeness is enforced later by `check`.
- **Lifecycle.** Every entity carries a boolean `draft` flag, default `false`, a manual publish switch the author sets — `add --draft` on capture, `edit <id> draft true|false` after ([`cli.md`](cli.md#author)); no verb auto-promotes or auto-demotes. Completeness is computed from the schema and enforced by `check`, never inferred from this flag, so a published entity can be incomplete (`check` reports it active-but-incomplete) and a draft can be complete. A draft is unpublished: excluded from required-completeness, and it never satisfies another entity's required relation.
- **Standard fields.** Beyond `type` and the `draft` flag, an entity may carry OKF's optional `title`, `description`, and `resource` (the canonical URI of the underlying asset, khub's link-out), plus `author` (the writer, person or agent), `tags`, and `created`/`updated`. Per-type fields and relations come from the schema. The firm-ops preset adds `owner` (→ person) as its own accountability edge, distinct from the core `author`.
- **The integrity loop** keeps the graph clean without manual policing. Draft status, orphan, and stale are core projection properties: every read includes drafts in scope and carries each entity's `stale`/`orphan` flag by default; `check`/`stale` gate on the same computation, so what a read shows is what the gate sees. The acceptance signals are three gates at three altitudes:
  - `khub validate`: one entity at a time — is it well-formed, and do its relations resolve.
  - `khub check`: the whole active (`draft: false`) subgraph — completeness, connectivity and placement. Its findings, and which of them fail the gate, are listed in [`cli.md`](cli.md#reading-check-output).
  - `khub stale`: time — entities whose `updated` is past a threshold, dates read from `git log`.

### Command Surface

The CLI is a thin, schema-introspecting adapter over the core library's verbs: create, get, update, delete, link, query. Every read command emits `--format json` for the agent or a Rich table for a human. The Claude Code skill maps agent intent onto these same commands as the agent's retrieval surface: `query`, `get`, the graph walks (`neighbors`/`impact`/`history`), and `search`.

**One input path, no prompts.** Every value arrives as a flag or an argument; a missing one is a usage error (exit 2). There is no wizard and no interactive confirmation: an agent cannot answer a prompt.

The shipped commands, their flags, output shapes and exit codes are specified in [`cli.md`](cli.md); this memo does not restate them. Five verbs are planned and not yet shipped:

- `khub path <from> <to>` — the shortest path between two entities.
- `khub build` — materialize the SQLite projection. Derived like every projection, so deleting it loses nothing.
- `khub export --okf [path]` — render the workspace to a conformant OKF bundle (see [OKF](#okf)).
- `khub diff-preset` — drift against the canonical preset, and promote-back (see [Deployment Model](#deployment-model-seeded-fork)).
- `khub rename <id> <new-slug>` — rename a slug and rewrite inbound references.

## Deployment Model: Seeded Fork

The hub repo (this repo) holds the engine, the canonical presets and the skills.

`khub init <preset>` scaffolds a fresh workspace. It copies the preset's three layer files into editable `.khub/{ontology,policy,storage}.yaml` (the base block stays embedded in the binary), then lays down the entity tree. After init the engagement carries no runtime dependency on the hub.

```
engagement-repo/
  .khub/
    config.yaml        # workspace config: preset provenance, command defaults
    ontology.yaml      # the domain: types, attributes, relations, capture cues
    policy.yaml        # workspace gates: required singletons, orphan exemptions
    storage.yaml       # layouts, inventory paths, id prefixes, template links
    generated/         # stable workspace lock and upgrade recovery journals, gitignored
  …                    # the entity tree, wherever storage.yaml puts each type
```

The tree is illustrative: where each type's entities land is that type's storage config, specified in [`schema.md`](schema.md#storage-config-storageyaml).

Each schema file stamps provenance (`# khub-preset: engineering@1.0.0`). The engagement owns the three layer files outright: editing them (add a type in ontology, change an enum, move a type's inventory in storage) is the entire override mechanism, with no runtime tie to the hub — only the base block arrives from the binary, and it is not authorable: a type overrides a base attribute by redeclaring it. Pulling a preset improvement down, or promoting an override back up, is a planned sync mechanism: a layered git-subtree merge (the preset as a subtree plus an overrides patch), with `diff-preset` reporting the delta. Flattening is the current model; layering is the planned path.

## Build Presets

The shipped engineering setup is `build-hub`: eleven types spanning product knowledge and architecture in one flat `knowledge/` tree. `build-lite` is an alias that resolves to it. The preset files are authoritative; see [`build-hub-preset.md`](build-hub-preset.md).

khub records durable requirements, decisions, architecture, and specs. Live delivery status and test execution stay in specialist tools and link back through `resource`.

## OKF

**OKF (Open Knowledge Format)** is the vendor-neutral substrate khub speaks: a git tree of `.md` concepts with a required `type`, cross-links, `index.md`, `log.md`. khub's Markdown entities are conformant OKF concepts, so khub is an OKF implementation and extension: it adds a typed schema, typed relations, a `draft` flag, and the graph. Extra serialization formats and collections are khub's own; `khub reindex` writes the OKF `index.md`.
