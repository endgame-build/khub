# Knowledge Hub CLI (`khub`)

The CLI is a thin, schema-introspecting adapter over the core library's verbs (create, get, update, delete, link, query). It hardcodes no per-type knowledge; it reads the active schema at runtime. Every read command emits `--format json` for an agent or a Rich table for a human. The Claude Code skill maps agent intent onto these same commands, and the MCP server (post-v1) exposes the same verbs as tools.

Tiers: **v1** ships in the first release; **fast-follow** lands shortly after (mostly the SQLite/FTS projection and ingestion); **deferred** is named but unscheduled.

## Global options

| Option | Meaning |
|---|---|
| `-C, --workspace <path>` | Operate on this workspace. Default: the nearest `.khub/` above the working directory. |
| `--format <json\|table>` | Output shape for read commands. Default: table on a TTY, json otherwise. Some commands add `ids`, `raw`, or `tree`. |
| `-q, --quiet` / `-v, --verbose` | Quieter or louder logging. |
| `--version`, `--help` | Version and help. |

Read commands include `draft` entities in scope and surface each entity's `orphan`/`stale` flag by default; `--active`/`--draft` and `--orphan`/`--stale` narrow the set.

## Validation and open schema

khub validates the **schema-declared subset** of an entity and leaves everything else alone:

1. **Declared fields are enforced.** Any present field the schema knows is checked against its type, enum, pattern, and cardinality. A malformed value, or a relation to a non-existent target, is rejected on write.
2. **`required` is a completeness gate, not a capture block.** A missing required field or relation does not reject the write; it saves the entity as a draft (`draft: true`). Capture is never blocked. `check` enforces required-completeness over the `active` subgraph.
3. **Extensions are free.** Any key the schema does not declare is accepted with any value, validated against nothing, and preserved on round-trip (Pydantic `extra="allow"` over the generated model).
4. **`--strict` closes the schema.** `validate --strict` (and `add`/`edit --strict`) rejects unknown keys, for when a closed contract is wanted.

## Workspace

| Command | Args and options | Returns / does | Tier |
|---|---|---|---|
| `khub init <preset> [path=.]` | `--preset-source <git\|path>`, `--name <name>`, `--force` | scaffold a workspace from a preset (the seeded fork) | v1 |
| `khub schema` | `--format` | the full effective schema: types, fields, enums, relations, layout/format/nesting per type, and provenance (source preset + version) | v1 |
| `khub schema types` | `--format` | type list (view of the above) | v1 |
| `khub schema show <type>` | `--format` | one type's fields, enums, required, relations, layout (view) | v1 |
| `khub schema edges` | `--format` | the relation vocabulary (view) | v1 |
| `khub schema --diff` | `--format` | engagement overrides versus the canonical preset | fast-follow |
| `khub status` | `--format` | counts per type, draft vs active, orphan and stale counts, OKF-conformance flag (projectable-to-OKF) | v1 |

## Author

| Command | Args and options | Does | Tier |
|---|---|---|---|
| `khub add <type>` | `--<field> <value>` (repeatable; schema or extension), `--id <slug>`, `--body <text>` / `--body-file <path>`, `--strict` | mint a slug, write a well-formed (possibly `draft`) entity; print its id | v1 |
| `khub get <id>` | `--format json\|table\|raw`, `--edges` | print an entity; `--edges` includes derived inverse edges | v1 |
| `khub edit <id> <field> <value>` | or `--<field> <value>` (repeatable), `--body` / `--body-file`, `--strict` | edit fields, bump `updated`, re-validate | v1 |
| `khub remove <id>` | `--force` | delete an entity; refuses while an inbound edge resolves to it, unless `--force` | v1 |
| `khub link <id> <predicate> <target>` | | add a schema-checked relation | v1 |
| `khub unlink <id> <predicate> <target>` | | remove a relation | v1 |

## Lookup

| Command | Args and options | Does | Tier |
|---|---|---|---|
| `khub query` | `--type <t>`, `--draft` / `--active`, `--orphan`, `--stale`, `--<field> <value>`, `--tag <tag>`, `--has <pred>`, `--missing <pred>`, `--limit <n>`, `--format json\|table\|ids` | filter entities by frontmatter; includes drafts and carries `orphan`/`stale` flags by default; `--missing` surfaces gaps | v1 |
| `khub search <text>` | `--type <t>`, `--limit <n>`, `--format` | full-text over body and prose | fast-follow (FTS) |

## Traversal

| Command | Args and options | Walk / family | Tier |
|---|---|---|---|
| `khub neighbors <id>` | `--predicate <p>`, `--in` / `--out` / `--both` (default both), `--depth <n=1>`, `--format` | one-hop adjacency | v1 |
| `khub impact <id>` | `--predicate <p>` (default `depends_on`), `--reverse`, `--format tree\|json` | transitive forward (blast radius), `--reverse` for ancestors | v1 |
| `khub history <id>` | `--predicate <p>` (default `supersedes`), `--limit <n>`, `--format` | the supersession chain (decision history) | v1 |
| `khub path <from> <to>` | `--predicate <p>`, `--format` | shortest path between two entities | fast-follow |

## Integrity

| Command | Args and options | Does | Tier |
|---|---|---|---|
| `khub validate [target=all]` | `--strict`, `--fix` (v1: date backfill only), `--format` | per-entity well-formedness and referential integrity over the declared subset (default: whole workspace) | v1 |
| `khub check` | `--format` | graph-wide: relations resolve, required-completeness for `active`, no orphans (zero relations), no stray files (non-entities inside a type layout; reference docs outside type layouts are skipped), no edge cycles | v1 |
| `khub stale` | `--days <n=30>`, `--format` | entities past an `updated` threshold; dates backfilled from `git log` | v1 |
| `khub log [id]` | `--limit <n>`, `--since <date>`, `--format` | git history at ontology altitude (who changed what, when); distinct from `history` | v1 |

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
