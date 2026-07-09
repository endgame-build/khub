# khub CLI

The CLI is a thin, schema-introspecting adapter over the core library's verbs (create, get, update, delete, link, query). It hardcodes no per-type knowledge; it reads the active schema at runtime. Every read command emits `--format json` for an agent or a Rich table for a human. The Claude Code skill maps agent intent onto these same commands, and the MCP server (post-v1) exposes the same verbs as tools.

## Global options

| Option | Meaning |
|---|---|
| `-C, --workspace <path>` | Operate on this workspace instead of the working directory. Default: the nearest `.khub/` above the working directory. Given a path, khub resolves the nearest `.khub/` at or above it. |
| `--agent` | Agent mode: never prompt. A missing input becomes the usual error. Output format is unchanged. |
| `--format <json\|table>` | Output shape for read commands. Default: table on a TTY, json otherwise. Some commands add `ids`, `raw`, or `tree`. |
| `--version` | Print the khub version and exit. |
| `--help` | Show help. |

Read commands include `draft` entities in scope and surface each entity's `orphan`/`stale` flag by default; `--active`/`--draft` and `--orphan`/`--stale` narrow the set.

### Interactive prompts

On a terminal, a write verb run with a missing input opens a wizard instead of erroring: `khub init` picks a preset and confirms the wire/skill tails; `khub add`/`edit` walk the type's fields, offering enums as menus and relations as picks from existing entities; `khub link`/`get`/`remove` pick the entity, predicate, and target from lists. A wizard fills only what a flag left empty, and it never blocks capture: required fields are marked but stay skippable.

khub never prompts an agent: with `--agent`, with `--format json`, when stdin/stdout is not a TTY (a pipe, a redirect), or under CI, a missing input is the same error it has always been. Every prompted value has a flag, so any interactive result reproduces headlessly.

### JSON record shape

Every JSON record that identifies an entity carries the qualified `id` = `"type/slug"` plus separate `type` and `slug` keys, uniform across `query`, `search`, `add`, `get`, `edit`, `neighbors`, `impact`, `history`, `stale`, `log`, and the `validate`/`check` error rows. For example, `khub get initech-pov --format json` emits `{"id": "opportunity/initech-pov", "type": "opportunity", "slug": "initech-pov", …}`. (`check`'s `orphans` are qualified `type/slug`; `strays` are file paths, `path` or `path#slug`.)

## Validation and open schema

khub validates the **schema-declared subset** of an entity and leaves everything else alone:

1. **Declared fields are enforced.** Any present field the schema knows is checked against its type, enum, pattern, and cardinality. A malformed value, or a relation to a non-existent target, is rejected on write.
2. **`required` is a completeness gate, not a capture block.** A missing required field or relation does not reject the write; the entity is still saved; active by default. Capture is never blocked. `check` enforces required-completeness over the `active` subgraph and reports an active-but-incomplete entity.
3. **Extensions are free.** Any key the schema does not declare is accepted with any value, validated against nothing, and preserved verbatim on round-trip.
4. **`--strict` closes the schema.** `validate --strict` (and `add`/`edit --strict`) rejects unknown keys, for when a closed contract is wanted.

### Write semantics

The write verbs (`add`, `edit`, `link`, `unlink`, `remove`) reject malformed input before touching a file:

- **A comma-list on a single-valued relation is rejected**: pass one target; a many-valued relation takes the list.
- **An ambiguous bare target is rejected**: when a slug names entities of two types, qualify it as `type/slug`.
- **Malformed dates and booleans are rejected** on write (a non-ISO date, a non-boolean for a `bool` field).
- **A self-link is rejected**: an entity cannot link to itself.
- **An explicit `--id` that collides** with an existing entity of the type is rejected (minting auto-suffixes; an explicit id does not).
- **An over-long slug is rejected.**

`link` and `unlink` are idempotent and report the no-op rather than pretend to act:

- `link <id> <pred> <target>` when the edge already exists prints `Edge already present` (exit 0).
- `unlink <id> <pred> <target>` when there is no such edge prints `No edge <pred> -> <target> on <slug>` (exit 0).

A single malformed entity file (a broken frontmatter fence, unparseable YAML) no longer crashes the read commands. `validate` reports it as an error and `check` reports it as a `malformed` entry (text and JSON), and the rest of the workspace still resolves.

### Entity formats

A type stores its entities as `md` (the default: YAML frontmatter + prose body), `json`, or `yaml`, per-type schema config (`format:` next to `layout:`/`path:`). A json/yaml entity is a single mapping: pure metadata, with prose carried in a reserved `body` field. `--body`/`--body-file` write it, `get` returns it as the body, an empty body writes no key, and the key never appears in `frontmatter` output or query filters. `search` indexes non-md entities over the body field plus every scalar string field. `gjson` is not supported; the schema rejects it.

### Collections

`layout: collection` stores every entity of a type as a row in ONE file (`format: json|jsonl|yaml`; `path` names the file, default `{type}.{format}`, and the format may be derived from the path's extension). Row identity: yaml/json collections are mappings keyed by slug; jsonl rows carry a reserved `slug` key. Rows may omit `type` (the schema binding supplies it; a disagreeing `type` makes the row a stray, reported as `path#slug`). Every verb and gate works on rows; `add`/`get`/`edit` JSON records additionally carry `locator: "path#slug"`, and `get --format raw` prints only the row. Writes are serialized by a per-type lock under `.khub/generated/locks/` (gitignored via `.khub/generated/`) and land via an atomic replace; a crash never leaves a torn file. A missing or empty collection file is zero entities; any bad row makes the whole file malformed: no rows load, writes to the type refuse, and derivative dangling reports are suppressed into the malformed finding (`check` emits `suppressed_dangling`). Git semantics: `log` is row-correct (a commit is attributed to the rows whose values changed); `stale` judges a row only on its own `updated` (undated rows are skipped, never given the file's commit date); `backfill` skips collection types and says so. See `docs/collections-design.md` for the full contract.

## Workspace

| Command | Args and options | Returns / does |
|---|---|---|
| `khub init <preset> [path=.]` | `--preset-source <path>`, `--name <name>`, `--force`, `--no-wire`, `--no-skill`, `--format <text\|json>` (json emits resolved provenance) | scaffold a workspace from a preset (the seeded fork), then wire it into `CLAUDE.md` and install the agent skill via `npx skills` (best-effort; needs `npx`). `--no-wire` / `--no-skill` skip either tail |
| `khub schema` | `--format` | the full effective schema: types, fields, enums, relations, layout/format/nesting per type, and provenance (source preset + version) |
| `khub schema types` | `--format` | type list (view of the above) |
| `khub schema show <type>` | `--format` | one type's fields, enums, required, relations, layout (view) |
| `khub schema edges` | `--format` | the relation vocabulary (view) |
| `khub status` | `--format` | counts per type, draft vs active, orphan and stale counts, OKF-conformance flag (projectable-to-OKF) |

## Author

| Command | Args and options | Does |
|---|---|---|
| `khub add <type>` | `--<field> <value>` (repeatable; schema or extension), `--id <slug>`, `--draft`, `--body <text>`, `--body-file <path>` (`-` for stdin; not both), `--strict`, `--format text\|json` (emits the written record) | mint a slug, write a well-formed entity (active by default; `--draft` marks it unpublished); print its id |
| `khub get <id>` | `--format json\|table\|raw`, `--edges` | print an entity; `--edges` includes derived inverse edges |
| `khub edit <id> <field> <value>` | or `--<field> <value>` (repeatable), `--body <text>` (`''` clears) / `--body-file` (not both), `--strict`, `--format text\|json` (emits the updated record) | edit fields, bump `updated`, re-validate |
| `khub remove <id>` | `--force` | delete an entity; refuses while an inbound edge resolves to it, unless `--force` |
| `khub link <id> <predicate> <target>` | | add a schema-checked relation |
| `khub unlink <id> <predicate> <target>` | | remove a relation |

## Lookup

| Command | Args and options | Does |
|---|---|---|
| `khub query` | `--type <t>`, `--draft` / `--active`, `--orphan`, `--stale`, `--<field> <value>`, `--tag <tag>`, `--has <pred>`, `--missing <pred>`, `--limit <n>`, `--format json\|table\|ids` | filter entities by frontmatter; includes drafts and carries `orphan`/`stale` flags by default; `--missing` surfaces gaps |
| `khub search <text>` | `--type <t>`, `--limit <n=20>`, `--format text\|json\|ids` | full-text over title and body (SQLite FTS5, BM25-ranked, in-memory projection built per call, never stale). Raw MATCH syntax passes through: terms, `"phrases"`, `OR`, `NEAR`, `prefix*`. Records add `title`, `score` (lower = better), `snippet`, `path` to the uniform id keys, no `draft`/`orphan`/`stale` flags on this command |

## Traversal

| Command | Args and options | Walk / family |
|---|---|---|
| `khub neighbors <id>` | `--predicate <p>`, `--in` / `--out` (default both), `--depth <n=1>`, `--format` | one-hop adjacency |
| `khub impact <id>` | `--predicate <p>` (default `depends_on`), `--reverse`, `--format tree\|json` | transitive forward (blast radius), `--reverse` for ancestors |
| `khub history <id>` | `--predicate <p>` (default `supersedes`), `--limit <n>`, `--format` | the supersession chain (decision history) |

## Integrity

| Command | Args and options | Does |
|---|---|---|
| `khub validate [target=all]` | `--strict`, `--fix` (v1: date backfill only), `--format` | per-entity well-formedness and referential integrity over the declared subset (default: whole workspace) |
| `khub check` | `--strict`, `--format` | graph-wide: relations resolve, required-completeness for `active`, no stray files (non-entities inside a type layout; reference docs outside type layouts are skipped), no edge cycles. Orphans (zero relations) are always reported but fail the gate only under `--strict`: a fully disconnected entity can be legitimate (a dormant client whose engagements were archived) |
| `khub stale` | `--days <n>` (default: the workspace `stale_days`, 90 in firm-ops), `--format` | entities past an `updated` threshold; dates backfilled from `git log` |
| `khub log [id]` | `--limit <n>`, `--since <date>`, `--format` | git history at ontology altitude (who changed what, when); distinct from `history`. `--format json` always emits one shape: `{"entries": [...], "git_available": true|false}` (a no-git workspace is `entries: []`, `git_available: false`, never a bare notice) |

## Projection and output

| Command | Args and options | Does |
|---|---|---|
| `khub reindex` | `--dry-run` | regenerate the OKF `index.md` navigation from the graph |
| `khub viz` | `--out <file.html=viz.html>`, `--open`, `--type <t>` | self-contained Cytoscape HTML over the typed graph |
| `khub backfill` | `--type <t>`, `--dry-run` | add missing frontmatter and dates from `git log` |
| `khub wire` | `--agents`, `--dry-run` | inject a managed khub block into `CLAUDE.md` (and `AGENTS.md` with `--agents`): a `@.khub/schema.yaml` import plus the command surface, so an agent reasons in the workspace ontology even without running khub. Idempotent, minimal-diff |

`khub init` runs `wire` as a tail. It also installs the agent skill by shelling out to `npx skills` (Vercel's skills.sh CLI), which drops the skill into whichever coding agent is present (Claude Code, Cursor, Codex, and others) and writes a `skills-lock.json`. To install the skill into an existing workspace without re-scaffolding: `npx skills add git@github.com:endgame-build/khub.git -s khub -s setup`.

*Planned verbs (not yet shipped): `path`, `build`, `export --okf`, `diff-preset`, `rename`. See the repo for status.*
