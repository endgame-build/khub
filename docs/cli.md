# Knowledge Hub CLI (`khub`)

The CLI is a thin, schema-introspecting adapter over the core library's verbs (create, get, update, delete, link, query). It hardcodes no per-type knowledge; it reads the active schema at runtime. Every read command emits `--format json` for an agent or a Rich table for a human. The Claude Code skill maps agent intent onto these same commands, and the MCP server (post-v1) exposes the same verbs as tools.

Tiers: **v1** ships in the first release; **fast-follow** lands shortly after (mostly ingestion and the persisted SQLite projection); **deferred** is named but unscheduled.

## Global options

| Option | Meaning |
|---|---|
| `-C, --workspace <path>` | Operate on this workspace instead of the working directory. Default: the nearest `.khub/` above the working directory. Given a path, khub resolves the nearest `.khub/` at or above it. |
| `--format <json\|table>` | Output shape for read commands. Default: table on a TTY, json otherwise. Some commands add `ids`, `raw`, or `tree`. |
| `--version` | Print the khub version and exit. |
| `--help` | Show help. |

Read commands include `draft` entities in scope and surface each entity's `orphan`/`stale` flag by default; `--active`/`--draft` and `--orphan`/`--stale` narrow the set.

### JSON record shape

Every JSON record that identifies an entity carries the qualified `id` = `"type/slug"` plus separate `type` and `slug` keys — uniform across `query`, `search`, `add`, `get`, `edit`, `neighbors`, `impact`, `history`, `stale`, `log`, and the `validate`/`check` error rows. For example, `khub get initech-pov --format json` emits `{"id": "opportunity/initech-pov", "type": "opportunity", "slug": "initech-pov", …}`. (`check`'s `orphans` and `strays` are lists of already-qualified strings.)

## Validation and open schema

khub validates the **schema-declared subset** of an entity and leaves everything else alone:

1. **Declared fields are enforced.** Any present field the schema knows is checked against its type, enum, pattern, and cardinality. A malformed value, or a relation to a non-existent target, is rejected on write.
2. **`required` is a completeness gate, not a capture block.** A missing required field or relation does not reject the write; the entity is still saved (active by default — `draft` is a manual publish flag, FS-002). Capture is never blocked. `check` enforces required-completeness over the `active` subgraph and reports an active-but-incomplete entity.
3. **Extensions are free.** Any key the schema does not declare is accepted with any value, validated against nothing, and preserved on round-trip (Pydantic `extra="allow"` over the generated model).
4. **`--strict` closes the schema.** `validate --strict` (and `add`/`edit --strict`) rejects unknown keys, for when a closed contract is wanted.

### Write semantics

The write verbs (`add`, `edit`, `link`, `unlink`, `remove`) reject malformed input before touching a file:

- **A comma-list on a single-valued relation is rejected** — pass one target; a many-valued relation takes the list.
- **An ambiguous bare target is rejected** — when a slug names entities of two types, qualify it as `type/slug`.
- **Malformed dates and booleans are rejected** on write (a non-ISO date, a non-boolean for a `bool` field).
- **A self-link is rejected** — an entity cannot link to itself.
- **An explicit `--id` that collides** with an existing entity of the type is rejected (minting auto-suffixes; an explicit id does not).
- **An over-long slug is rejected.**

`link` and `unlink` are idempotent and report the no-op rather than pretend to act:

- `link <id> <pred> <target>` when the edge already exists prints `Edge already present` (exit 0).
- `unlink <id> <pred> <target>` when there is no such edge prints `No edge <pred> -> <target> on <slug>` (exit 0).

A single malformed entity file (a broken frontmatter fence, unparseable YAML) no longer crashes the read commands. `validate` reports it as an error and `check` reports it as a `malformed` entry (text and JSON), and the rest of the workspace still resolves.

### Entity formats

A type stores its entities as `md` (the default: YAML frontmatter + prose body), `json`, or `yaml` — per-type schema config (`format:` next to `layout:`/`path:`). A json/yaml entity is a single mapping: pure metadata, with prose carried in a reserved `body` field — `--body`/`--body-file` write it, `get` returns it as the body, an empty body writes no key, and the key never appears in `frontmatter` output or query filters. `search` indexes non-md entities over the body field plus every scalar string field. `gjson` is deferred; the schema rejects it.

### Collections

`layout: collection` stores every entity of a type as a row in ONE file (`format: json|jsonl|yaml`; `path` names the file, default `{type}.{format}`, and the format may be derived from the path's extension). Row identity: yaml/json collections are mappings keyed by slug; jsonl rows carry a reserved `slug` key. Rows may omit `type` (the schema binding supplies it; a disagreeing `type` makes the row a stray, reported as `path#slug`). Every verb and gate works on rows; `add`/`get`/`edit` JSON records additionally carry `locator: "path#slug"`, and `get --format raw` prints only the row. Writes are serialized by a per-type lock under `.khub/locks/` (gitignored) and land via an atomic replace — a crash never leaves a torn file. A missing or empty collection file is zero entities; any bad row makes the whole file malformed: no rows load, writes to the type refuse, and derivative dangling reports are suppressed into the malformed finding (`check` emits `suppressed_dangling`). Git semantics: `log` is row-correct (a commit is attributed to the rows whose values changed); `stale` judges a row only on its own `updated` (undated rows are skipped, never given the file's commit date); `backfill` skips collection types and says so. See `docs/collections-design.md` for the full contract.

## Workspace

| Command | Args and options | Returns / does | Tier |
|---|---|---|---|
| `khub init <preset> [path=.]` | `--preset-source <path>`, `--name <name>`, `--force`, `--format <text\|json>` (json emits resolved provenance) | scaffold a workspace from a preset (the seeded fork) | v1 |
| `khub compile` | `--schema <path=.khub/schema.yaml>`, `--out <dir=.khub/generated>` | compile the schema into LinkML + Pydantic v2 + JSON Schema under `.khub/generated/` | v1 |
| `khub schema` | `--format` | the full effective schema: types, fields, enums, relations, layout/format/nesting per type, and provenance (source preset + version) | v1 |
| `khub schema types` | `--format` | type list (view of the above) | v1 |
| `khub schema show <type>` | `--format` | one type's fields, enums, required, relations, layout (view) | v1 |
| `khub schema edges` | `--format` | the relation vocabulary (view) | v1 |
| `khub schema --diff` | `--format` | engagement overrides versus the canonical preset | fast-follow |
| `khub status` | `--format` | counts per type, draft vs active, orphan and stale counts, OKF-conformance flag (projectable-to-OKF) | v1 |

## Author

| Command | Args and options | Does | Tier |
|---|---|---|---|
| `khub add <type>` | `--<field> <value>` (repeatable; schema or extension), `--id <slug>`, `--draft`, `--body <text>`, `--body-file <path>` (`-` for stdin; not both), `--strict` | mint a slug, write a well-formed entity (active by default; `--draft` marks it unpublished); print its id | v1 |
| `khub get <id>` | `--format json\|table\|raw`, `--edges` | print an entity; `--edges` includes derived inverse edges | v1 |
| `khub edit <id> <field> <value>` | or `--<field> <value>` (repeatable), `--body <text>` (`''` clears) / `--body-file` (not both), `--strict` | edit fields, bump `updated`, re-validate | v1 |
| `khub remove <id>` | `--force` | delete an entity; refuses while an inbound edge resolves to it, unless `--force` | v1 |
| `khub link <id> <predicate> <target>` | | add a schema-checked relation | v1 |
| `khub unlink <id> <predicate> <target>` | | remove a relation | v1 |

## Lookup

| Command | Args and options | Does | Tier |
|---|---|---|---|
| `khub query` | `--type <t>`, `--draft` / `--active`, `--orphan`, `--stale`, `--<field> <value>`, `--tag <tag>`, `--has <pred>`, `--missing <pred>`, `--limit <n>`, `--format json\|table\|ids` | filter entities by frontmatter; includes drafts and carries `orphan`/`stale` flags by default; `--missing` surfaces gaps | v1 |
| `khub search <text>` | `--type <t>`, `--limit <n=20>`, `--format text\|json\|ids` | full-text over title and body (SQLite FTS5, BM25-ranked, in-memory projection built per call — never stale). Raw MATCH syntax passes through: terms, `"phrases"`, `OR`, `NEAR`, `prefix*`. Records add `title`, `score` (lower = better), `snippet`, `path` to the uniform id keys — no `draft`/`orphan`/`stale` flags and no narrowing options on this command | fast-follow — shipped 2026-07-07 |

## Traversal

| Command | Args and options | Walk / family | Tier |
|---|---|---|---|
| `khub neighbors <id>` | `--predicate <p>`, `--in` / `--out` (default both), `--depth <n=1>`, `--format` | one-hop adjacency | v1 |
| `khub impact <id>` | `--predicate <p>` (default `depends_on`), `--reverse`, `--format tree\|json` | transitive forward (blast radius), `--reverse` for ancestors | v1 |
| `khub history <id>` | `--predicate <p>` (default `supersedes`), `--limit <n>`, `--format` | the supersession chain (decision history) | v1 |
| `khub path <from> <to>` | `--predicate <p>`, `--format` | shortest path between two entities | fast-follow |

## Integrity

| Command | Args and options | Does | Tier |
|---|---|---|---|
| `khub validate [target=all]` | `--strict`, `--fix` (v1: date backfill only), `--format` | per-entity well-formedness and referential integrity over the declared subset (default: whole workspace) | v1 |
| `khub check` | `--strict`, `--format` | graph-wide: relations resolve, required-completeness for `active`, no stray files (non-entities inside a type layout; reference docs outside type layouts are skipped), no edge cycles. Orphans (zero relations) are always reported but fail the gate only under `--strict` — a fully disconnected entity can be legitimate (a dormant client whose engagements were archived) | v1 |
| `khub stale` | `--days <n>` (default: the workspace `stale_days`, 90 in firm-ops), `--format` | entities past an `updated` threshold; dates backfilled from `git log` | v1 |
| `khub log [id]` | `--limit <n>`, `--since <date>`, `--format` | git history at ontology altitude (who changed what, when); distinct from `history`. `--format json` always emits one shape: `{"entries": [...], "git_available": true|false}` (a no-git workspace is `entries: []`, `git_available: false` — never a bare notice) | v1 |

## Projection and output

| Command | Args and options | Does | Tier |
|---|---|---|---|
| `khub reindex` | `--dry-run` | regenerate the OKF `index.md` navigation from the graph | v1 |
| `khub viz` | `--out <file.html=viz.html>`, `--open`, `--type <t>` | self-contained Cytoscape HTML over the typed graph | v1 |
| `khub backfill` | `--type <t>`, `--dry-run` | add missing frontmatter and dates from `git log` | v1 |
| `khub build` | | materialize the SQLite projection | fast-follow |
| `khub export --okf [path]` | | render the workspace to a conformant OKF bundle (non-md entities become md concepts) | fast-follow |
| `khub diff-preset` | | drift against the canonical preset, and promote-back | deferred |
| `khub rename <id> <new-slug>` | | rename a slug and rewrite inbound references | deferred |
