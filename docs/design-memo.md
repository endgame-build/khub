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
and never the authoring surface. The schema is split into three layer files — `ontology.yaml` (the domain: per-type `attributes`, `relations`, `when`), `policy.yaml` (this workspace's gates: `required`, `orphan`) and `storage.yaml` (`layout`/`format`/`path`/`id_prefix`/`template`) — merging at load time into one resolved contract. The `base` block (`type`, `status`, `author`, `created`/`updated`, `tags`, the OKF fields and families, the `any → any` edges) is khub's own plumbing and ships EMBEDDED in the binary, supplied to every resolve and never copied into a workspace; an authored `ontology.base` is rejected outright — the base is not an authoring surface. A type declares only its domain delta and overrides a base attribute by redeclaring it. The split is what makes the ontology projectable: `ontology.yaml` carries purely the domain, which is what an RDF/SHACL export reads.

```yaml
# core/ontology.yaml  (embedded in the binary — supplied to every resolve, never copied)
ontology:
  base:
    attributes: { type: {required: true}, status: {enum: [draft, stable, deprecated], default: stable},
                  author: {}, created: {type: date, required: true}, updated: {type: date},
                  title: {}, description: {}, resource: {}, tags: {type: list}, aliases: {type: list},
                  generated: {type: object, fields: {by: {required: true}, at: {type: datetime}}},
                  verified: {type: list, items: {type: object, fields: {by: {required: true}, at: {type: datetime}, rev: {}}}},
                  stale_after: {type: datetime}, sources: {type: list, items: {type: object, fields: {resource: {required: true}, …}}},
                  usage_window: {type: object, fields: {from: {type: datetime}, to: {type: datetime}}} }
    relations:  { related: {to: any, many: true}, references: {to: any, many: true},
                  depends_on: {to: any, many: true, acyclic: true} }

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

- **Identity.** Each entity's **id is its slug**: one bare, human-readable token (`auth`, `initech-pov`) that names the file or folder on disk and identifies the node in the graph. No type prefix. Uniqueness is per type, `(type, slug)`, with the file path as the globally-unique key; a within-type collision is refused (`slug_taken`) rather than suffixed, because minting reads no siblings — an id is a function of the schema and the entity's own fields, nothing else on disk. Typed relations resolve by their schema-known target type (`lives_in: api`); polymorphic (`any`-typed) relations take a bare slug too, qualified as `type/slug` only when a slug is ambiguous across types. An entity may declare other names in `aliases`: every ID lookup falls back to them once the slug misses, a write target named by one is stored as the real slug, and `check` fails an alias that names two entities. An external identifier rides along as a non-authoritative `source_id` alias (the ingestion path). A preset may also declare integration routing keys (e.g. a notes-tool folder id) as first-class attributes; the `source_id` alias is specifically for an external system's record id. Renaming a slug is not yet supported.
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
- **Write rules.** Referential integrity hard-fails on write: a relation to a non-existent target is rejected. A missing required field or relation does not block capture: the entity is still written; completeness is enforced later by `check`.
- **Lifecycle.** Every entity carries the OKF `status` (`draft | stable | deprecated`, default `stable`). `status: draft` is a manual publish switch the author sets. `add` writes no status by default, `--draft` writes `status: draft`, and `edit <id> status draft|stable` moves it; an absent status reads as `stable` and is never written, and no verb auto-promotes or auto-demotes. A truthy legacy `draft` reads as draft whatever `status` says until `khub upgrade` migrates it, so a 0.30.0 draft is never published by accident. `deprecated` carries no gate meaning. Read rows keep a derived `draft` flag. Completeness is computed from the schema and enforced by `check`, never inferred from this flag, so a published entity can be incomplete (`check` reports it active-but-incomplete) and a draft can be complete. A draft is unpublished: excluded from required-completeness, and it never satisfies another entity's required relation.
- **Standard fields.** Beyond `type` and `status`, an entity may carry OKF's optional `title`, `description`, and `resource` (the canonical URI of the underlying asset, khub's link-out), plus `author` (the creator, set once), `tags`, `aliases` and `created`/`updated`, and the OKF provenance and trust families: `generated` (`{by, at}`, the last writer), `verified` (review events), `stale_after`, `sources` (what the concept was derived from, as metadata, never an edge) and `usage_window`. Per-type fields and relations come from the schema. The firm-ops preset adds `owner` (→ person) as its own accountability edge, distinct from the core `author`.
- **The integrity loop** keeps the graph clean without manual policing. Draft status, orphan, and stale are core projection properties: every read includes drafts in scope and carries each entity's `stale`/`orphan` flag by default; `check`/`stale` gate on the same computation. The acceptance signals:
  - `khub validate`: per-entity well-formedness against the schema, plus referential integrity.
  - `khub check`: graph-wide over the active (not `status: draft`) subgraph. Relations resolve, required-completeness holds for `active` entities (computed from the schema; an active-but-incomplete entity is reported), a `draft` does not satisfy a required relation, no orphans (entities with no inbound or outbound relation), no dangling edges, and no stray files (a file inside a type's layout that is not a valid entity of that type; reference docs outside the type layouts are skipped).
  - `khub stale`: entities whose `updated` is past a threshold; dates backfilled from `git log`.
- **Two stalenesses, kept apart.** Age staleness is inferred: the `updated` date (or git's last commit) is older than `stale_days`. Expiry is authored: `stale_after` says "this holds until then", and from that instant the entity is stale whatever its age. Reads carry one `stale` flag that either sets, `khub stale` names which in each row's `reason` (`age` or `expired`), `status` counts `stale` and `expired` separately and never sums them, and `check --stale` makes active expired entities fail the gate on request. No verb computes `stale_after` from an interval, for the same reason none auto-promotes a draft.
- **Provenance and trust.** Every write that changes an entity's content (body, declared fields, `status`, relations) stamps `generated: {by, at}`, the last writer. `by` comes from `--by`, then `KHUB_ACTOR`, then `agent/claude-code` under Claude Code, then `khub`, and never from git identity: an agent in someone's shell would otherwise be stamped as them. Repair verbs never stamp. A review is a `verified` event `{by, at, rev}` that `khub verify` appends; `rev` hashes the content the reviewer saw, so a later content or status edit supersedes the review and a bookkeeping edit (tags, aliases, `stale_after`) does not. The tier (`unverified`, `machine`, `human`) is derived on every read from the live events and never stored. An event counts only with a conventional actor and an OKF datetime `at`; one without a `rev` always holds (OKF §5.3). A `verified` entry is a claim; `verify --commit` adds a signed git commit as the proof.

### Command Surface

The CLI is a thin, schema-introspecting adapter over the core library's verbs: create, get, update, delete, link, query. Every read command emits `--format json` for the agent or a Rich table for a human. The Claude Code skill maps agent intent onto these same commands as the agent's retrieval surface: `query`, `get`, the graph walks (`neighbors`/`impact`/`history`), and `search`.

**One input path, no prompts.** Every value arrives as a flag or an argument; a missing one is a usage error (exit 2). There is no wizard and no interactive confirmation: an agent cannot answer a prompt.

| Group | Command | Does |
|-------|---------|------|
| Workspace | `khub init <preset>` | scaffold a workspace from a preset |
| | `khub schema [types \| show <type> \| edges]` | introspect the active schema: types, fields, enums, edges, required relations |
| | `khub status` | counts per type, draft vs active, orphan, stale and expired counts, review tiers, OKF 0.2 conformance and the files that break it |
| Author | `khub add <type> [--field v …]` | mint a slug, write a well-formed entity (active by default; `--draft` to mark unpublished) |
| | `khub get <id>` | print an entity: frontmatter and body |
| | `khub edit <id> <field> <value>` | edit a field, bump `updated`, re-validate |
| | `khub link <id> <predicate> <target>` | add a schema-checked relation: legal predicate, target resolves, cardinality holds |
| | `khub unlink <id> <predicate> <target>` | remove a relation |
| | `khub remove <id>` | delete an entity; refuse when an inbound edge still resolves to it |
| | `khub verify <id> --by <actor>` | record a review as an OKF `verified` event; `--commit` adds a signed commit |
| Query | `khub query [--type --<field> …]` | filter entities by frontmatter |
| | `khub neighbors <id> [--predicate p] [--in\|--out]` | one-hop edges in either direction |
| | `khub impact <id> [--predicate p]` | blast radius: transitive closure over an edge |
| | `khub history <id>` | supersession chain and edit history |
| | `khub path <from> <to>` | shortest path between two entities *(planned)* |
| | `khub search <text>` | full-text search (FTS5, in-memory per invocation) |
| Integrity | `khub validate [path]` | per-entity well-formedness and referential integrity (default: whole workspace) |
| | `khub check` | graph-wide: relations resolve, required relations complete, no orphans, no stray files, no edge cycles |
| | `khub stale [--days N]` | entities past an `updated` threshold or an authored `stale_after`, each with its `reason`; dates backfilled from `git log` |
| Projection | `khub reindex` | regenerate the root OKF 0.2 `index.md` from the graph |
| | `khub backfill [--type T]` | add missing frontmatter and dates from `git log` |
| | `khub viz` | self-contained Cytoscape HTML over the typed graph |
| | `khub serve` | the same graph live over loopback HTTP, read-only, rebuilt per request |
| | `khub build` | materialize the SQLite projection *(planned)* |
| | `khub export --okf <dir>` | render the workspace to a conformant OKF 0.2 bundle: one md concept per entity, relations as a `# Related` section, an `index.md` per directory |
| | `khub import --okf <dir>` | read an external OKF bundle in as drafts, permissively: unknown keys kept, unknown types mapped or skipped, links reported |
| | `khub diff-preset` | drift against the canonical preset, and promote-back *(planned)* |
| | `khub rename <id> <new-slug>` | rename a slug and rewrite inbound references *(planned)* |

*Verbs marked *(planned)* are not yet shipped; see [`cli.md`](cli.md) for the live surface.*

## Deployment Model: Seeded Fork

The hub repo (this repo) holds the engine, the canonical presets and the skills.

`khub init <preset>` scaffolds a fresh workspace. It copies the preset's three layer files into editable `.khub/{ontology,policy,storage}.yaml` (the base block stays embedded in the binary), then lays down the entity tree. After init the engagement carries no runtime dependency on the hub.

```
engagement-repo/
  .khub/
    config.yaml        # workspace config: preset provenance + version, source, command defaults (format, stale_days)
    ontology.yaml      # the domain: types, attributes, relations, capture cues
    policy.yaml        # workspace gates: required singletons, orphan exemptions
    storage.yaml       # layouts, inventory paths, id prefixes, template links
    generated/         # stable workspace lock and upgrade recovery journals, gitignored
  clients/             # entity type folders live at the workspace root, one per type,
  projects/            #   each laid out per the type's storage config (see Authoring and
    <slug>/_index.md   #   Integrity): flat `clients/{slug}.md` or folder `projects/{slug}/_index.md`
  meetings/            # flat-layout type: one file per slug
  identity/team/       # person nodes; identity/ also holds untyped reference markdown
```

Each schema file stamps provenance (`# khub-preset: engineering@1.0.0`). The engagement owns the three layer files outright: editing them (add a type in ontology, change an enum, move a type's inventory in storage) is the entire override mechanism, with no runtime tie to the hub — only the base block arrives from the binary, and it is not authorable: a type overrides a base attribute by redeclaring it. Pulling a preset improvement down, or promoting an override back up, is a planned sync mechanism: a layered git-subtree merge (the preset as a subtree plus an overrides patch), with `diff-preset` reporting the delta. Flattening is the current model; layering is the planned path.

## Build Presets

The shipped engineering setup is `build-hub`: eleven types spanning product knowledge and architecture in one flat `knowledge/` tree. `build-lite` is an alias that resolves to it. The preset files are authoritative; see [`build-hub-preset.md`](build-hub-preset.md).

khub records durable requirements, decisions, architecture, and specs. Live delivery status and test execution stay in specialist tools and link back through `resource`.

## OKF

**OKF (Open Knowledge Format)** is the vendor-neutral substrate khub speaks: a git tree of `.md` concepts with a required `type`, cross-links, `index.md`, `log.md`. khub targets OKF 0.2. A workspace is conformant (§11) when every `.md` file in its tree, meaning everything below the root outside hidden directories, vendored trees and `.khub/ignore`, has parseable frontmatter with a non-empty `type`, and every `index.md` and `log.md` keeps its reserved shape. The root agent files `khub wire` writes are khub's own wiring and sit outside the tree like `.khub/`, so a freshly wired workspace is conformant. Broken links never count. `khub status` reports the flag and names every file that breaks it, so the flag is never a bare no. khub's Markdown entities are conformant OKF concepts, so khub is an OKF implementation and extension: it adds a typed schema, typed relations and the graph, and it reads the OKF families (`status`, `generated`, `verified`, `stale_after`, `sources`, `usage_window`) as declared base attributes. It writes `generated` on every content change, appends `verified` through `khub verify` with one extension (`rev`, the content revision a review saw), and derives a trust tier from them on every read. `index.md` and `log.md` are navigation at any depth, never entities. Extra serialization formats and collections are khub's own; `khub reindex` writes the OKF `index.md`.

khub reads OKF in two modes. **Its own workspace is strict**: the schema is the contract, an unknown type is an error, and `validate` and `check` behave as documented. **An external bundle is read permissively** (`khub import --okf`, OKF §11's consumer side): a concept lands as a draft in the type its OKF `type` maps to, its frontmatter kept as written, unknown keys and key order included; an unknown type with no mapping, a document with no frontmatter and a broken link are reported, never fatal. What the bundle asserts is quarantined, not trusted: its `verified` moves to `okf_verified`, kept verbatim and never counted, so the concept reads `unverified` until someone here reviews it, and a relation value no entity answers to moves to `okf_unresolved`, so the write path's referential-integrity rule holds for import too. `generated` stays, as the producer's provenance claim. Identity follows the workspace: the id is minted by the type's scheme around the concept's file stem, as `add` mints one, and the stem is kept in `aliases`, so a lookup or a link by the old name resolves; an id is never guessed, suffixed or merged with an existing entity, and nothing is written back to the bundle. `khub export --okf` is the other direction, and the two round-trip: export's `# Related` section reads back into relations.
