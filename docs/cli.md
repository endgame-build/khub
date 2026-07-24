# khub CLI

The CLI is a thin, schema-introspecting adapter over the core library's verbs (create, get, update, delete, link, query). It hardcodes no per-type knowledge; it reads the active schema at runtime. Every read command emits `--format json` for an agent or a Rich table for a human. The Claude Code skill maps agent intent onto these same commands, and the MCP server (post-v1) exposes the same verbs as tools.

> **Two CLI docs.** This page is the hand-written contract — JSON output shapes, exit codes, and the schema-driven `--<field>` options. The mechanical flag surface (every command with its declared flags and defaults) is generated from the Typer app into [`cli-reference.md`](./cli-reference.md) and checked for drift in CI. The generated page cannot see the dynamic `--<field>` options on `add`/`edit`/`query` — those live here.

## Global options

| Option | Meaning |
|---|---|
| `-C, --workspace <path>` | Operate on this workspace instead of the working directory. Default: the nearest `.khub/` above the working directory. Given a path, khub resolves the nearest `.khub/` at or above it. |
| `--format <json\|table>` | Output shape for read commands. Default: table on a TTY, json otherwise. Some commands add `ids`, `raw`, or `tree`. |
| `--version` | Print the khub version and exit. |
| `--help` | Show help. |

`-C/--workspace` and `--version` are parsed before the command name — put them first (`khub -C <path> <command>`), the git convention. `--format` and the other per-command options follow the command as usual.

Read commands include `draft` entities in scope and surface each entity's `orphan`/`stale` flag by default; `--active`/`--draft` and `--orphan`/`--stale` narrow the set.

### khub never prompts

Every input is a flag or an argument; a missing one is a usage error (exit 2), never a question. khub shipped an interactive wizard through 0.8.0 — `init` picked a preset, `add`/`edit` walked the schema — behind a gate that switched it off for agents, pipes, and CI. The gate meant the wizard was dead weight on every agent invocation, which is the invocation khub is built for, so 0.9.0 removed both it and the `--agent` flag that disabled it. Output still adapts to the reader: a Rich table on a TTY, JSON on a pipe or under `--format json`.

### JSON record shape

Every JSON record that identifies an entity carries the qualified `id` = `"type/slug"` plus separate `type` and `slug` keys, uniform across `query`, `search`, `add`, `get`, `edit`, `neighbors`, `impact`, `history`, `stale`, and the `validate`/`check` error rows. For example, `khub get initech-pov --format json` emits `{"id": "opportunity/initech-pov", "type": "opportunity", "slug": "initech-pov", …}`. (`check`'s `orphans` are qualified `type/slug`; `strays` are file paths, `path` or `path#slug`.)

## Validation and open schema

khub validates the **schema-declared subset** of an entity and leaves everything else alone:

1. **Declared fields are enforced.** Any present field the schema knows is checked against its type, enum, pattern, and cardinality. A malformed value, or a relation to a non-existent target, is rejected on write.
2. **`required` is a completeness gate.** A missing required field or relation does not reject the write; the entity is still saved, active by default. Capture is never blocked. `check` enforces required-completeness over the `active` subgraph and reports an active-but-incomplete entity.
3. **Extensions are free.** Any key the schema does not declare is accepted with any value, validated against nothing, and preserved verbatim on round-trip.
4. **`--strict` closes the schema.** `validate --strict` (and `add`/`edit --strict`) rejects unknown keys, for when a closed contract is wanted.
5. **Templated types hold their body shape.** When `.khub/templates/<type>.yaml` exists, `validate` requires the template's section headings in every instance body as an ordered subsequence (extras allowed) — reported as a `body` finding, never blocking a write. `check` additionally reports a `required: true` singleton whose file is absent.

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

`layout: collection` stores every entity of a type as a row in ONE file (`format: json|jsonl|yaml`; `path` names the file, default `{type}.{format}`, and the format may be derived from the path's extension). Row identity: yaml/json collections are mappings keyed by slug; jsonl rows carry a reserved `slug` key. Rows may omit `type` (the schema binding supplies it; a disagreeing `type` makes the row a stray, reported as `path#slug`). Every verb and gate works on rows; `add`/`get`/`edit` JSON records additionally carry `locator: "path#slug"`, and `get --format raw` prints only the row. Writes are serialized by a per-type lock under `.khub/generated/locks/` (gitignored via `.khub/generated/`) and land via an atomic replace; a crash never leaves a torn file. A missing or empty collection file is zero entities; any bad row makes the whole file malformed: no rows load, writes to the type refuse, and derivative dangling reports are suppressed into the malformed finding (`check` emits `suppressed_dangling`). Git semantics: `stale` judges a row only on its own `updated` (undated rows are skipped, never given the file's commit date); `backfill` skips collection types and says so. See `docs/collections-design.md` for the full contract.

## Workspace

| Command | Args and options | Returns / does |
|---|---|---|
| `khub init <preset> [path=.]` | `--preset-source <path>`, `--name <name>`, `--force`, `--no-wire`, `--format <text\|json>` (json emits resolved provenance) | scaffold a workspace from a preset directory (`schema.yaml` + `templates/`), flatten templates to `.khub/templates/`, create missing md singletons from their templates (creations only), then wire the selected agent files. `--no-wire` skips the tail. Prints the `khub install-skills` hint (`skill_hint` in the JSON payload); installs nothing |
| `khub schema` | `--format` | the full effective schema: types, fields, enums, relations, layout/format/nesting per type, and provenance (source preset + version) |
| `khub schema types` | `--format` | type list (view of the above) |
| `khub schema show <type>` | `--format` | one type's fields, enums, required, relations, layout (view) |
| `khub schema edges` | `--format` | the relation vocabulary (view) |
| `khub status` | `--format` | counts per type, draft vs active, orphan and stale counts, OKF-conformance flag (projectable-to-OKF) |

## Author

| Command | Args and options | Does |
|---|---|---|
| `khub add <type>` | `--<field> <value>` (repeatable; schema or extension), `--id <slug>`, `--draft`, `--body <text>`, `--body-file <path>` (`-` for stdin; not both), `--no-template`, `--strict`, `--format text\|json` (emits the written record) | mint a slug, write a well-formed entity (active by default; `--draft` marks it unpublished); a templated md type seeds its body from `.khub/templates/<type>.yaml`; a singleton's slug is its type name; print its id |
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
| `khub history <id>` | `--predicate <p>` (default `supersedes`), `--limit <n>`, `--format` | the supersession chain (decision history); `supersedes` is a schema-general default that the firm-ops preset does not declare |

## Integrity

| Command | Args and options | Does |
|---|---|---|
| `khub validate [target=all]` | `--strict`, `--format` | per-entity well-formedness and referential integrity over the declared subset (default: whole workspace). Never writes — repairing a missing date is `khub backfill` |
| `khub check` | `--strict`, `--format` | graph-wide: relations resolve, required-completeness for `active`, no stray files (non-entities inside a type layout; reference docs outside type layouts are skipped), no edge cycles. Orphans (zero relations) are always reported but fail the gate only under `--strict`: a fully disconnected entity can be legitimate (a dormant client whose engagements were archived) |
| `khub stale` | `--days <n>` (default: the workspace `stale_days`, 90 in firm-ops), `--format` | entities past an `updated` threshold; dates backfilled from `git log` |

## Projection and output

| Command | Args and options | Does |
|---|---|---|
| `khub reindex` | `--dry-run` | regenerate the OKF `index.md` navigation from the graph |
| `khub viz` | `--out <file.html=viz.html>`, `--open`, `--type <t>` | self-contained Cytoscape HTML over the typed graph |
| `khub backfill` | `--type <t>`, `--dry-run` | add missing frontmatter and dates from `git log` |
| `khub wire` | `--target <claude\|agents\|both>`, `--dry-run` | inject a managed khub block into the workspace's agent files. Bare `wire` updates whichever of `CLAUDE.md` / `AGENTS.md` already exist (creates none; hints if neither); `--target` creates a specific file. `CLAUDE.md` gets a `@.khub/schema.yaml` import; `AGENTS.md` (no import directive) gets a schema pointer. Both carry the command surface, so an agent reasons in the workspace ontology even without running khub. Idempotent, minimal-diff |
| `khub install-skills` | `--agent <name>` (repeatable), `--dry-run`, `--format` | install the khub agent skill into the coding agents under the workspace, by shelling out to `npx skills` (Vercel's skills.sh CLI). Omitted `--agent` keeps npx's auto-detect; `--dry-run` prints the exact command and runs nothing. Writes a `skills-lock.json` (commit it) and gitignores the per-machine `.claude/skills/` and `.agents/skills/`. A failed install exits 1 |

`khub init` runs `wire` as a tail. Installing the agent skill is a separate command, `khub install-skills` — until 0.9.0 it was a second init tail, which made scaffolding depend on Node and on SSH access to the skill repo.

*Planned verbs (not yet shipped): `path`, `build`, `export --okf`, `diff-preset`, `rename`. See the repo for status.*
