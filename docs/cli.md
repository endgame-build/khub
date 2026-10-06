# khub CLI

The CLI is a thin, schema-introspecting adapter over the core library's verbs (create, get, update, delete, link, query). It hardcodes no per-type knowledge; it reads the active schema at runtime. Commands emit JSON on a pipe and Rich output on a TTY; help follows the same gate, preserving section order without box drawing when piped. The agent skill maps intent onto these same commands. MCP was evaluated and rejected for this local, single-user product shape.

> **Two CLI docs.** This page is the hand-written contract — JSON output shapes, exit codes, and the schema-driven `--<field>` options. The mechanical flag surface (every command with its declared flags and defaults) is generated from the Cobra app into [`cli-reference.md`](./cli-reference.md) and checked for drift in CI. The generated page cannot see the dynamic `--<field>` options on `add`/`edit`/`query` — those live here.

## Global options

| Option | Meaning |
|---|---|
| `-C, --workspace <path>` | Operate on this workspace instead of the working directory. Default: the nearest `.khub/` above the working directory. Given a path, khub resolves the nearest `.khub/` at or above it. |
| `--format <json\|table>` | Output shape. Default: table on a TTY, json otherwise — for **reads and writes alike**. Some commands add `ids`, `raw`, or `tree`. The operator commands (`reindex`, `viz`, `serve`, `backfill`, `wire`) print prose and take no `--format`. |
| `--version` | Print the khub version and exit. |
| `--help` | Show help. |

`-C/--workspace` and `--version` are parsed before the command name — put them first (`khub -C <path> <command>`), the git convention. `--format` and the other per-command options follow the command as usual.

Read commands include `draft` entities in scope and surface each entity's `orphan`/`stale` flag by default; `--active`/`--draft` and `--orphan`/`--stale` narrow the set. An entity of a type declaring `orphan: true` never carries the flag.

### khub never prompts

Every input is a flag or an argument; a missing one is a usage error (exit 2), never a question. khub shipped an interactive wizard through 0.8.0 — `init` picked a preset, `add`/`edit` walked the schema — behind a gate that switched it off for agents, pipes, and CI. The gate meant the wizard was dead weight on every agent invocation, which is the invocation khub is built for, so 0.9.0 removed both it and the `--agent` flag that disabled it. Output still adapts to the reader: a Rich table on a TTY, JSON on a pipe or under `--format json`.

### Failures

Every refused call renders one line on stderr and exits 2: the call was
malformed, or would have written something the schema forbids, and nothing was
written — correct it and retry. Under `--format json` the failure is a JSON
document instead — `{"error": {"code", "message"}}` — so an agent that asked for
machine output never has to parse prose. A usage error (an unknown flag, a
missing argument) is also exit 2 and stays plain text. Exit 1 is reserved for a
gate that ran and failed (`validate`, `check`): there the workspace is what is
wrong, not the call, and the report names what to fix.

### JSON record shape

Every JSON record that identifies an entity carries the qualified `id` = `"type/slug"` plus separate `type` and `slug` keys, uniform across `query`, `search`, `add`, `get`, `edit`, `link`, `unlink`, `remove`, `neighbors`, `impact`, `history`, `stale`, and the `validate`/`check` error rows. Writes obey the same output gate as reads: JSON on a pipe or under `--format json`, prose only on a TTY. For example, `khub get initech-pov --format json` emits `{"id": "opportunity/initech-pov", "type": "opportunity", "slug": "initech-pov", …}`. (`check`'s `orphans` are qualified `type/slug`; `strays` are file paths, `path` or `path#slug`.)

## Validation and open schema

khub validates the **schema-declared subset** of an entity and leaves everything else alone:

1. **Declared fields are enforced.** Any present field the schema knows is checked against its type, enum, pattern, and cardinality. A malformed value, or a relation to a non-existent target, is rejected on write.
2. **`required` is a completeness gate.** A missing required field or relation does not reject the write; the entity is still saved, active by default. Capture is never blocked. `check` enforces required-completeness over the `active` subgraph and reports an active-but-incomplete entity.
3. **Extensions are free.** Any key the schema does not declare is accepted with any value, validated against nothing, and preserved verbatim on round-trip.
4. **`--strict` closes the schema.** `validate --strict` (and `add`/`edit --strict`) rejects unknown keys, for when a closed contract is wanted. Run `validate --strict` in CI: by rule 3 a typo'd field name is captured silently and no default gate reports it.
5. **Templated types hold their body shape; their body content is reported, never gated.** When a type reads a template (`.khub/templates/<stem>.yaml` — the storage `template:` name, or the type's own name), `validate` requires the template's non-optional section headings in every instance body as an ordered subsequence (extras allowed) — a `body` **error** (`body_shape`), never blocking a write — and evaluates each section's rules (`word_count`, `required_text`, `forbidden_text`, `code_blocks`): every violation is a `body` **gap** (`body_rule`), reported but never failing the gate, `--strict` included. A template that does not parse is one `template` error against the type (`<type>/*`), and that type's bodies go unjudged. `check` reports the same three: `template_invalid` and `body_shape` fail it, `body_rule` lands in the informational `thin` list. `check` additionally reports a `required: true` singleton whose file is absent. See "Body templates" below.

### Write semantics

The write verbs (`add`, `edit`, `link`, `unlink`, `remove`) reject malformed input before touching a file:

All existing-workspace mutation paths acquire `.khub/generated/locks/workspace.lock` before the scan and hold it through validation and publication. The file is a stable lock identity: do not delete lock files while a process may hold them. Reads and dry runs create no lock. Writes use unique exclusive sibling temporaries, sync the file and parent directory, preserve an existing file's permissions, honor umask for a new file, and publish a new entity exclusively so a concurrent winner is never overwritten. An error after publication reports that durability is uncertain and tells the caller to inspect before retrying.

Schema storage paths must be workspace-relative, normalized, outside `.khub`, and non-overlapping: folder layouts own their subtree. Storage roots, entity paths, and control paths refuse symlinks. A missing storage path means an empty type; permission errors, wrong-shaped files/directories, and other scan failures propagate instead of producing a partial clean result.

- **A comma-list on a single-valued relation is rejected**: pass one target; a many-valued relation takes the list.
- **An ambiguous bare target is rejected**: when a slug names entities of two types, qualify it as `type/slug`.
- **A target named by alias is stored as the real slug.** `add`/`edit` relation values and `link`/`unlink` targets resolve through `aliases` once the slug lookups miss, and the write stores the owner's slug (qualified when the bare slug would be ambiguous), so no edge on disk depends on another file's aliases. A value on disk still resolves by slug only: a hand-written alias there dangles, and `check` reports it.
- **A slug that is already another entity's alias is rejected** (`alias_taken`): creating it would record the same thing twice. The refusal names the owner; pass `--id` for a different slug.
- **Malformed dates and booleans are rejected** on write (a non-ISO date, a non-boolean for a `bool` field).
- **A self-link is rejected**: an entity cannot link to itself.
- **Without `--id`**, the id is minted from `name`, then `title`, in the type's scheme (`<prefix>-<YYYY-MM-DD>-<slug>`, each part optional per type — `khub schema show <type>` prints it as `id_shape`; see [Ids](schema.md#ids-id_prefix-id_date-in-storage)). Minting reads no siblings, so it never races across branches. A type with neither `name` nor `title` **refuses** (`no_slug_source`): there is no type-name fallback any more, so **pass `--id` for a type you do not title.** A by-value `id_prefix` whose deciding attribute is unset refuses too (`id_prefix_undecided`, naming the flag).
- **A slug already taken is rejected, minted or explicit** — nothing is ever auto-suffixed. The same title mints the same id, so the refusal names the way out: `pass --id <slug> to name this one differently`.
- **An over-long slug is rejected.**

`link` and `unlink` are idempotent and report the no-op rather than pretend to act:

- `link <id> <pred> <target>` when the edge already exists prints `Edge already present` (exit 0).
- `unlink <id> <pred> <target>` when there is no such edge prints `No edge <pred> -> <target> on <slug>` (exit 0).

A single malformed entity file (a broken frontmatter fence, unparseable YAML) no longer crashes the read commands. `validate` reports it as an error and `check` reports it as a `malformed` entry (text and JSON), and the rest of the workspace still resolves.

### Entity formats

A type stores its entities as `md` (the default: YAML frontmatter + prose body), `json`, or `yaml`, per-type schema config (`format:` next to `layout:`/`path:`). A json/yaml entity is a single mapping: pure metadata, with prose carried in a reserved `body` field. `--body`/`--body-file` write it, `get` returns it as the body, an empty body writes no key, and the key never appears in `frontmatter` output or query filters. `search` indexes every entity over its body plus every scalar string field, in all formats. `gjson` is not supported; the schema rejects it.

### Collections

`layout: collection` stores every entity of a type as a row in ONE file (`format: json|jsonl|yaml`; `path` names the file, default `{type}.{format}`, and the format may be derived from the path's extension). Row identity: yaml/json collections are mappings keyed by slug; jsonl rows carry a reserved `slug` key. Rows may omit `type` (the schema binding supplies it; a disagreeing `type` makes the row a stray, reported as `path#slug`). Every verb and gate works on rows; `add`/`get`/`edit` JSON records additionally carry `locator: "path#slug"`, and a single `get --format raw` prints only the row. Collections use the workspace-wide mutation lock and the same atomic publication contract as item files. A missing or empty collection file is zero entities; any bad row makes the whole file malformed: no rows load, writes to the type refuse, and derivative dangling reports are suppressed into the malformed finding (`check` emits `suppressed_dangling`). Git semantics: `stale` judges a row only on its own `updated` (undated rows are skipped, never given the file's commit date); `backfill` skips collection types and says so. See `docs/collections-design.md` for the full contract.

### Body templates

`.khub/templates/<stem>.yaml` is a per-type body contract, read only by per-item and singleton `md` types (a `template:` name in storage picks the stem; `template: false` opts out). Top-level keys: `title`, `hint` (leads the scaffolded body as a `<!-- -->` comment), `sections`, `lenses`. `sections: []` declares no heading contract; a missing key is an error. Each section takes `heading` (required), `hint` (scaffolded as a comment), `text` (scaffolded literally), `optional` (scaffolded like any other, never required — and when absent, none of its rules are evaluated), `word_count: {min, max}`, `required_text` and `forbidden_text` (lists of literals, matched case-insensitively, or `{pattern: <regex>}`), and `code_blocks: [{lang, min, max}]` (a rule with neither bound is refused; `lang` absent counts every fenced block). `repeat`, `pattern`, `images`, `lists`, `tables`, `min_tokens`, `max_tokens`, `budget` are reserved and refused rather than ignored. Rules read prose only — HTML comments and fenced code are blanked — an empty section is skipped unless it holds a fenced block, and each CJK character counts as one word. Headings match as an ordered subsequence: a declared heading is a prefix of some body H2 (leading `1.`/`2)` numbering stripped, up to three spaces of indent allowed), a `##` inside a comment or a fence is not a heading, and an unmatched heading does not advance the cursor. A template's own scaffold satisfies every rule it declares.

`lenses` is a list of review prompts khub never answers — `{code, instruction, name?, section?, when?, after?}`. `when: {<field>: <value> | [<values>]}` limits a lens to entities whose frontmatter, with schema defaults resolved in, carries one of the values (compared as the scalars would be written to disk, so a bool meets a bool and a date meets its ISO string); the field must be one the type declares. `section` must name a declared heading. `after` names the lens codes this one follows; lenses are emitted in declaration order adjusted so every `after` holds (a cycle, an unknown code, a repeated code and an unknown key are all `template_invalid`). Findings: `body_shape` (a non-optional heading missing or out of order) is an error in `validate` and fails `check`; `body_rule` (`'## <heading>' <complaint>`) is a gap in `validate` and `thin` in `check` — reported, never gating.

## Workspace

| Command | Args and options | Returns / does |
|---|---|---|
| `khub init <preset> [path=.]` | `--preset-source <path>`, `--name <name>`, `--force`, `--no-wire`, `--format <text\|json>` (json emits resolved provenance) | scaffold a workspace from a preset directory (`{ontology,policy,storage}.yaml` + `templates/`), copy templates to `.khub/templates/`, create missing md singletons from their templates (creations only), wire the selected agent files, then write `index.md` (reported as `index`: `created`/`updated`/`unchanged`, or `index_error` with a stderr `index skipped:` note when the scan holds malformed files). `--no-wire` skips the wire tail. Prints how to install the agent skills (`skill_hint` in the JSON payload: `npx skills add endgame-build/khub`, behind `cd <path> &&` when the workspace is not the working directory, with the path quoted for the shell when it needs it); installs nothing. Re-running over an existing workspace preserves anything workspace-owned (`.khub/{ontology,policy,storage}.yaml`, `.khub/config.yaml`, `.khub/templates/*.yaml`) and reports it as `preserved`; only genuinely missing files are recreated. Refreshing from a newer preset is `khub upgrade`, not a scaffold |
| `khub upgrade` | `--dry-run`, `--no-schema`, `--no-wire`, `--format <text\|json>` | build and validate a candidate workspace before publishing. The candidate copies the workspace except `.git/`, `.khub/generated/`, vendored directories (`node_modules`, `.venv`, `venv`, `__pycache__`, `.kb`) and non-regular files, applies the selected schema/template/scaffold changes, and previews enabled tails. `--dry-run` creates no workspace files or lock. A real run recomputes under the workspace lock, stages originals and replacements in `.khub/generated/upgrade-*`, preserves file modes and backups, publishes `config.yaml` last, and runs the wire and index tails under that same lock (as `init` runs its wire and index tails), so no other writer can slip in between the core commit and the index that lists it. Failure restores prior files and removes only files it created; concurrent edits or incomplete rollback return `upgrade_recovery_failed` and retain the named journal for manual recovery. Crash material may remain for inspection; khub makes no automatic crash-recovery guarantee. The wire and index tails remain non-fatal after the core commit. `--no-schema` keeps `.khub/` and reports `schema_drift`. JSON appends `dry_run` and `removed_types`; when the current schema cannot be loaded for comparison, `removed_types` is empty. |
| `khub schema` | `--format` | the full effective schema: types, fields, enums, relations, layout/format/nesting per type, and provenance (source preset + version) |
| `khub schema types` | `--format` | type list (view of the above) |
| `khub schema show <type>` | `--format` | one type's fields, enums, required, relations, layout, id scheme (`id_prefix`, `id_date`, and the rendered `id_shape` — `ad-YYYY-MM-DD-slug`; `null` on a singleton), and `when` — the moment to capture it (view) |
| `khub schema edges` | `--format` | the relation vocabulary (view) |
| `khub schema base` | `--format` | the effective base block every type inherits — embedded in the binary since the layer split, so no workspace file carries it (view) |
| `khub schema snapshot` | `--format` | record the current resolved schema at `.khub/schema.applied.yaml` as the baseline `schema diff` compares against, replacing any earlier one. The file is tracked in git and records the resolved form, not the layer files: per type its storage, id scheme, `required`/`orphan`, template, and every attribute and relation with its facets. Derived inverses, provenance and `when` are left out, since none of them decides validity. `init` and `upgrade` never write it, so a preset upgrade shows up as pending. Returns `{path, types}` |
| `khub schema diff` | `--format` | the schema changes since the snapshot: `{pending, changes: [{op, path, from, to}]}`, `op` one of `added`/`removed`/`changed`, `path` dotted (`types.component.attributes.lifecycle.enum`). Key order is not a change. Exits 0 whether or not changes are pending; `pending` says which. With no snapshot it refuses (`no_schema_snapshot`, exit 2) and names `khub schema snapshot`; an unreadable one refuses with `invalid_snapshot` |
| `khub status` | `--format` | counts per type, draft vs active, orphan and stale counts, OKF-conformance flag (projectable-to-OKF) |

## Author

| Command | Args and options | Does |
|---|---|---|
| `khub add <type>` | `--<field> <value>` (repeatable; schema or extension), `--id <slug>`, `--draft`, `--body <text>`, `--body-file <path>` (`-` for stdin; not both), `--no-template` (start with an empty body; refused on a type whose template has a required heading, since the result would fail `validate` — use `--body` to supply your own sections), `--strict`, `--format text\|json` (emits the written record) | mint an id from `name`/`title` — `<prefix>-<YYYY-MM-DD>-<slug>`, prefix and date each per the type's `id_prefix`/`id_date`; an explicit `--id` is slugified and used as given; a taken slug refuses — write a well-formed entity (active by default; `--draft` marks it unpublished); a templated md type seeds its body from `.khub/templates/<type>.yaml`; a singleton's slug is its type name; print its id |
| `khub get <id>...` | `--format json\|table\|raw`, `--edges` | resolve all IDs against one scan. One ID preserves the single-object output; multiple IDs produce an array in argument order, including duplicates. Any missing or ambiguous ID refuses the whole batch before output. `--format raw` requires exactly one ID. `--edges` includes stored and derived edges on every record |
| `khub edit <id> <field> <value>` | or `--<field> <value>` (repeatable), `--body <text>` (`''` clears) / `--body-file` (not both), `--strict`, `--format text\|json` (emits the updated record) | edit fields, bump `updated`, re-validate |
| `khub remove <id>` | `--force`, `--format text\|json` (emits the removed record) | delete an entity; refuses while an inbound edge resolves to it, unless `--force` |
| `khub link <id> <predicate> <target>` | `--format text\|json` (emits the edge record) | add a schema-checked relation; idempotent, so read `changed` to tell a write from a no-op |
| `khub unlink <id> <predicate> <target>` | `--format text\|json` (emits the edge record) | remove a relation; idempotent, same `changed` key |

## Lookup

Every command that takes an entity ID (`get`, `edit`, `link`, `unlink`, `remove`, `neighbors`, `impact`, `history`) resolves it the same way: the exact slug, then the slug case-insensitively, then an entity's `aliases` (a base attribute, a list), case-insensitively. A real slug always wins, so an alias never shadows one. Aliases are free text, so case never picks between claimants: an alias two entities claim, in any spelling, refuses as `ambiguity_error` and must be qualified, `type/alias`. Matching folds case but does not normalize Unicode, the same as slugs.

| Command | Args and options | Does |
|---|---|---|
| `khub query` | `--type <t>`, `--draft` / `--active`, `--orphan`, `--stale`, `--<field> <value>`, `--tag <tag>`, `--has <name>`, `--missing <name>`, `--limit <n>`, `--format json\|table\|ids` | filter entities by frontmatter; includes drafts and carries `title` plus `orphan`/`stale` flags by default. `--has`/`--missing` take a relation, a declared inverse, OR an attribute — for an attribute, absent/null/empty counts as missing, so `--missing repo` surfaces the gap. A name the schema does not declare refuses (exit 2): `--type` with `unknown_type`, a `--<field>` or `--has`/`--missing` name with `filter_error`, both listing the declared names. An unmatched value, or an unused `--tag`, is a legitimate empty result at exit 0 |
| `khub search <text>` | `--type <t>`, `--limit <n=20>`, `--plain`, `--format text\|json\|ids` | full-text over title, body, and every scalar frontmatter value (SQLite FTS5, BM25-ranked, in-memory projection built per call, never stale). Raw MATCH syntax passes through: terms, `"phrases"`, `OR`, `NEAR`, `prefix*`. `--plain` takes plain words instead: each distinct word (case-insensitive) is quoted, prefix-matched from three characters, and ORed with the rest, up to 64 words; punctuation never becomes syntax, and text with no words returns no hits. Ranking weights a title match five times a body match. Records add `title`, the `draft`/`orphan`/`stale` flags `query` reports, `score` (lower = better; the same weighted value that orders the hits), `snippet`, `path`, and `edges`: `{"out": {<predicate>: n}, "in": {"<source type>.<predicate>": n}}`, non-zero counts only, out-keys in relation declaration order and in-keys sorted. Under `--plain`, each record also carries `match` and `title_match` after `score`: the share of the query's distinct words the hit contains anywhere and in its title, each word weighted by inverse document frequency, 0..1 to three decimals; a `title_match` of 0 means only the body matched. When a result should not be taken at face value, one `note:` line goes to stderr before the output, in every format: rows `--limit` dropped (with the total), no match at all (with the entities searched and the verbs to try instead), files that could not be parsed, or a `--plain` query cut at 64 words |

## Traversal

| Command | Args and options | Walk / family |
|---|---|---|
| `khub neighbors <id>` | `--predicate <p>`, `--in` / `--out` (default both), `--depth <n=1>`, `--format` | one-hop adjacency |
| `khub impact <id>` | `--predicate <p>` (default `depends_on`), `--reverse`, `--format tree\|json` | transitive forward (blast radius), `--reverse` for ancestors |
| `khub history <id>` | `--predicate <p>` (default `supersedes`), `--limit <n>`, `--format` | the supersession chain (decision history); `supersedes` is a schema-general default that the firm-ops preset does not declare |

## Integrity

| Command | Args and options | Does |
|---|---|---|
| `khub validate [target=all]` | `--strict`, `--format` | per-entity well-formedness and referential integrity over the declared subset (default: whole workspace). JSON `{count, errors, gaps, body, lenses}`: `errors` alone gate (exit 1); `gaps` are body-rule findings (`'## <heading>' <complaint>`), reported and never gating; `body` (`{words, sections: [{heading, words}]}`, prose word counts with comments and fenced code excluded) and `lenses` (`[{code, name, section, instruction}]`, the review prompts that apply, in template order) are filled only for a single `type/slug` target of a templated md type — `null` and `[]` otherwise. Text: `<id>: <field>: <reason>` per error, `<id>: - <field>: <reason>` per gap, then `Validated N entities; E errors, G gaps`, then `Body: N words (Heading n, …)` and one `Lens <code> [## <section>]: <name>` per lens with its instruction indented two spaces beneath. Never writes — repairing a missing date is `khub backfill` |
| `khub check` | `--strict`, `--format` | graph-wide: relations resolve, required-completeness for `active`, no stray files (non-entities inside a type layout), no stray templates (`.khub/templates/*.yaml` no type claims — the renamed-template case, which would otherwise silently disable scaffolding and the body contract; a `template: false` type claims its conventional stem, so the opted-out file is not a finding), no missing declared templates (a storage `template:` name whose file does not exist — `missing_templates`; the same hole from the claiming side, kept a `check` finding so a broken link never blocks capture), no misplaced entities (a file outside every layout whose frontmatter names a declared type — reference docs carrying no `type`, or an unknown one, are still skipped), no edge cycles. Orphans (zero relations) are always reported but fail the gate only under `--strict`: a fully disconnected entity can be legitimate (a dormant client whose engagements were archived). A type declaring `orphan: true` in the schema is exempt from the sweep entirely — edge-less is its expected state, so it is never reported and never fails `--strict` (build-hub's two narrative singletons, `prd` and `arc42`, declare it; without it a freshly scaffolded workspace could not pass `--strict` at all). The same rule governs `query --orphan` and the `status` orphan count. `--format json` reports `strict` so a consumer can tell an informational orphan list from the reason the gate failed. Singletons are reported in two disjoint lists: `missing_singletons` (a `required: true` type with no file) and `draft_singletons` (any singleton present but unpublished — including a non-required one, which otherwise leaves the active subgraph with no signal at all). Only `draft_required_singletons`, the subset of the latter, fails the gate. Bodies are read too, over every templated md type: `template_invalid` (a template that exists but does not parse — one row per type, `{id: "<type>/*", type, slug: "*", reason}`) and `body_shape` (a non-optional heading missing or out of order, `{id, type, slug, reason}`) fail the gate; `thin` (a section rule the prose does not satisfy, same shape) is informational — never consulted by `passed`, `--strict` included — and is the last JSON key, after `strict`. Text prints `template_invalid <type>/*: <reason>` and `body_shape <id>: <reason>` after the declared-template lines, and `thin <id>: <reason>` last (`… (informational)` before `Graph check passed` on the passing path) |
| `khub stale` | `--days <n>` (default: the workspace `stale_days`, 90 in firm-ops), `--format` | entities past an `updated` threshold; dates backfilled from `git log` |

### Reading check output

Every finding carries a stable location — the `field` on a `validate` row, the bucket on a `check` payload. Other tools key on these, so they do not change without a release note. The body findings are defined under [Body templates](#body-templates).

| finding | severity | in `validate` | in `check` | what it means, and what to do |
|---|---|---|---|---|
| `frontmatter` | error | yes | `malformed` | the file's frontmatter could not be parsed (a broken fence, unparseable YAML); the rest of the workspace still resolves — fix the file |
| `id` | error | yes | no | the slug disagrees with the id scheme its type declares: no prefix this type mints, a prefix that disagrees with the deciding attribute, no date on an `id_date` type, or the retired `NNN-` ordinal — the message names the shape; `git mv` the file, then fix references to it |
| `<attribute>` | error | yes | no | an enum, `pattern`, scalar-type or empty-string violation (`'x' is not a valid kind (a, b)`, `'x' does not match the pattern for repo (…)`, `'x' is not a valid date for as_of`, `kind is empty; omit the field or write null, not ''`) — `khub schema show <type>` shows what is allowed |
| `<predicate>` cardinality | error | yes | no | `single-valued relation has multiple values` — `unlink` the extra |
| `<predicate>` dangling | error | yes | `dangling` | `no <type> '<x>' to satisfy relation '<predicate>'` — create the target, or `unlink` |
| `<predicate>` ambiguous | error | yes | no | `'<x>' is ambiguous — qualify as type/slug (candidates: …)` — a hand-written bare slug naming entities of two types; the write path already refuses it |
| `<key>` undeclared | error under `--strict` | yes | no | `undeclared key rejected under --strict` — a typo, or drop it; without `--strict` it is a free extension nothing reports |
| `template` (`template_invalid`) | error | yes | `template_invalid` | the type's template does not parse; the message names the key. One row per type (`<type>/*`) — that type's bodies go unjudged until it is fixed, and `add` seeds an empty body |
| `body` (`body_shape`) | error | yes | `body_shape` | a non-optional `##` heading is missing or out of declared order — `missing or out-of-order section '## X' (template x.yaml requires its headings in order)` |
| `body` (`body_rule`) | gap | yes, in `gaps` | `thin` | a section rule is unmet — `'## X' 4 words of prose, at least 15 asked for`, `'## X' says nothing matching 'we chose'`, `'## X' uses /\btbd\b/`, `'## X' 0 mermaid block(s), at least 1 asked for`. Never fails a gate, `--strict` included |
| self-link | refused on write | no | no | `add`/`link` refuse an entity linking to itself (exit 2); a hand-authored one resolves and passes, which is one reason writes go through the CLI |
| `strays` | error | no | yes | a file inside a type layout whose `type:` disagrees (a collection row reports as `path#slug`); it is not in the graph at all |
| `stray_templates` | error | no | yes | a `.khub/templates/*.yaml` no type claims — usually a renamed template; rename it back or point a storage `template:` at it |
| `missing_templates` | error | no | yes | a storage `template:` name whose file does not exist |
| `misplaced` | error | no | yes | a file outside every layout whose frontmatter names a declared type — Markdown anywhere in the workspace, `.json`/`.yaml` only inside a declared storage tree (a root-level `data.json` is a data file, never a claim); no command can see it; move it under the layout the row names as `expected` |
| `cycles` | error | no | yes | one deterministic real cycle witness per cyclic strongly connected component for each predicate declared `acyclic`, including self-loops — `unlink` one edge |
| `missing_singletons` | error | no | yes | a `required: true` singleton has no file — `khub init` or `khub upgrade` scaffolds it |
| `draft_singletons` | gap; error for `draft_required_singletons` | no | yes | a singleton present but `draft: true`; only a required one fails the gate — `edit <type> draft false` publishes |
| `incomplete` | error | no | yes | an active entity with a required field or relation empty (`missing_fields`, `missing_relations`) — legal to write, wrong to publish; fill it, or `edit <id> draft true` |
| `orphans` | gap; error under `--strict` | no | yes | no relation in or out; a type declaring `orphan: true` is exempt and never listed |
| `alias_conflicts` | error | no | yes | an alias naming more than one entity, `{alias, claimants}`: two entities declare it (matched case-insensitively, as lookup matches), or it is another entity's slug. An ID lookup through it is ambiguous or lands on the slug's owner; drop or change the alias on one claimant. Drafts count, since lookup resolves their aliases too |
| `thin` | informational | no | yes | the corpus-wide `body_rule` gaps, `{id, type, slug, reason}`; never affects `passed` |

**Exit codes.** `0` clean · `1` the gate failed · `2` a refusal or a usage error. `check` returns 0 when only gaps remain, which is what makes it safe to run on every commit; `--strict` makes orphans fail too — and only orphans. `body_rule` stays informational under `--strict` on purpose: otherwise every rule a template gained would turn a green corpus red on upgrade, and nobody would declare one.

### Payload shapes

**`khub validate --format json`** — `{count, errors, gaps, body, lenses}`. `field` is the frontmatter key at fault, which is what a caller has to act on. The exit code follows `errors` alone; `body` and `lenses` are populated only when `validate` was given a single `<type>/<slug>` of a templated md type, and are `null` / `[]` otherwise — a whole-corpus run would put hundreds of section counts and a repeated checklist in front of someone who asked about one file.

```json
{
  "count": 1,
  "errors": [
    {
      "id": "repo/rp-001-api",
      "type": "repo",
      "slug": "rp-001-api",
      "field": "id",
      "reason": "slug uses the retired '001-' ordinal scheme; this type mints rp-slug — `git mv` the file to drop the ordinal, then fix references to it"
    }
  ],
  "gaps": [
    {
      "id": "adr/ad-2026-01-15-use-postgres",
      "type": "adr",
      "slug": "ad-2026-01-15-use-postgres",
      "field": "body",
      "reason": "'## Decision' 4 words of prose, at least 15 asked for"
    }
  ],
  "body": {
    "words": 8,
    "sections": [{"heading": "Context", "words": 4}, {"heading": "Decision", "words": 4}]
  },
  "lenses": [
    {
      "code": "alternatives",
      "name": "A real alternative, not a strawman",
      "section": null,
      "instruction": "A decision with one option on the table is a description, not a decision. …"
    }
  ]
}
```

**`khub check --format json`** — gate on `passed`, or on the exit code. `incomplete` rows carry `{id, type, slug, missing_fields, missing_relations}`; `dangling` `{id, type, slug, predicate, target}`; `misplaced` `{path, type, expected}`; `template_invalid`, `body_shape` and `thin` `{id, type, slug, reason}` (a `template_invalid` row's id is `<type>/*`). `thin` comes last, after `strict`, because it is the one bucket that never affects `passed`.

```json
{
  "passed": false,
  "incomplete": [],
  "orphans": ["requirement/req-settle-within-300ms", "repo/rp-api"],
  "dangling": [
    {
      "id": "requirement/req-settle-within-300ms",
      "type": "requirement",
      "slug": "req-settle-within-300ms",
      "predicate": "realized_in",
      "target": "cmp-public-api"
    }
  ],
  "strays": [],
  "stray_templates": [],
  "missing_templates": [],
  "template_invalid": [],
  "body_shape": [],
  "misplaced": [],
  "malformed": [],
  "cycles": [],
  "suppressed_dangling": 0,
  "missing_singletons": [],
  "draft_singletons": [],
  "draft_required_singletons": [],
  "alias_conflicts": [],
  "strict": false,
  "thin": [
    {
      "id": "adr/ad-2026-01-15-use-postgres",
      "type": "adr",
      "slug": "ad-2026-01-15-use-postgres",
      "reason": "'## Decision' 4 words of prose, at least 15 asked for"
    }
  ]
}
```

## Projection and output

| Command | Args and options | Does |
|---|---|---|
| `khub reindex` | `--dry-run` | regenerate the OKF `index.md` navigation from the graph |
| `khub viz` | `--out <file.html=viz.html>`, `--open`, `--type <t>` | self-contained Cytoscape HTML over the typed graph |
| `khub serve` | `--port <n=7777>` | read-only graph view over HTTP on `127.0.0.1`. Every request rebuilds from the live files, so a reload always shows the current tree and no cache can go stale; the page itself fetches once per load rather than polling. The live counterpart to `viz`, which stays the static, shareable, printable artifact. Blocks until interrupted, so it refuses on a non-terminal stdout (`TTY_COMPATIBLE=1` overrides); a port already in use is a refusal naming `--port`, never a silent hop. No write endpoints: the page composes `khub link` / `khub unlink` commands to run, so every mutation stays on the validated CLI path |
| `khub backfill` | `--type <t>`, `--dry-run` | add missing frontmatter and dates from `git log` |
| `khub wire` | `--target <claude\|agents\|both>`, `--dry-run` | inject a managed khub block into the workspace's agent files. Bare `wire` writes both `CLAUDE.md` and `AGENTS.md`, creating either that is missing; `--target` creates a specific file. `CLAUDE.md` gets `@.khub/{ontology,policy,storage}.yaml` imports; `AGENTS.md` (no import directive) gets schema pointers. Both carry the command surface, so an agent reasons in the workspace ontology even without running khub. Idempotent, minimal-diff |

`khub init` and `khub upgrade` both run `wire` and the index write as tails. khub installs no skills. They ship in the Claude Code plugin and through `npx skills add endgame-build/khub` (see [Agent skills](../README.md#agent-skills)). khub also leaves alone the skill copies that khub 0.27.0 and earlier wrote under `.claude/skills/`, `.agents/skills/` and `.opencode/skills/`; delete those folders and their `.gitignore` lines by hand.

*Planned verbs (not yet shipped): `path`, `build`, `export --okf`, `diff-preset`, `rename`. See the repo for status.*
