# Changelog

Notable changes to khub. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); khub is pre-release.

## [0.5.0] — 2026-07-09

### Added — interactive CLI for humans (questionary)

Write verbs run a wizard on a terminal when an input is missing; agents and scripts are untouched.

- `khub init` picks a preset, prompts the directory, and confirms the wire/skill tails (with an agent multiselect). `khub add`/`edit` walk the type's schema, offering enums as menus and relations as picks from existing entities. `khub link`/`get`/`remove` and a bare `khub query` pick entities/types from lists.
- New global `--agent`: never prompt (agents cannot use interactivity). khub also stays silent under `--format json`, on a pipe/redirect (non-TTY), and in CI. Output format is unchanged by `--agent`.
- Every prompted value has a flag, so an interactive result reproduces headlessly; the wizard marks required fields but never blocks capture.
- Built on questionary (inline prompts on prompt_toolkit); it loads lazily, off the path of every agent invocation. Typer and Rich are unchanged. Full-screen TUI frameworks (Textual) were assessed and rejected for wizards; Textual is reserved for a future `khub tui` browser.

## [0.4.1] — 2026-07-08

### Fixed — `khub init` skill/wire tails (post-release review)

- `--format json` stays a single JSON document: the skill install captures npx output instead of letting it precede the JSON on stdout.
- `khub init` no longer exits with a traceback when a tail fails on a completed scaffold: the skill install catches `OSError` (Windows `npx.cmd`, broken PATH) and the wire tail catches `OSError` (read-only `CLAUDE.md`) in addition to `LocatedError`.
- `khub init` gitignores the per-machine skill directories (`.claude/skills/`, `.agents/skills/`) it drops into the workspace; `skills-lock.json` stays tracked.
- The skill-install failure line goes to stderr, and its recovery hint (and the docs) install both skills: `npx skills add … -s khub -s setup`.
- Plugin manifest version corrected to `1.1.0` (the `0.4.0` in v0.4.0 was a downgrade below the `1.0.0` shipped at v0.3.0, which would skip the plugin update).

## [0.4.0] — 2026-07-08

### Added — agnostic skill install, folded into `khub init`

`khub init` now sets a workspace up for agents in one step. After scaffolding it wires the schema into `CLAUDE.md` and installs the khub agent skill:

- The skill installs through Vercel's `npx skills` (the skills.sh CLI), which lands it in whichever coding agent is present (Claude Code, Cursor, Codex, and ~70 more) and writes a `skills-lock.json`. Same convention Neon's `neon init` uses.
- Both tails are best-effort: no `npx` on `PATH` prints `skill install skipped` and the scaffold still succeeds. `--no-wire` / `--no-skill` turn off either tail.
- `.claude-plugin/marketplace.json` declares the `khub` and `setup` skills, so `npx skills add git@github.com:endgame-build/khub.git` discovers them from the private repo over SSH.

### Distribution

- Install commands across the docs move to the pinned form `…@v0.4.0`.

## [0.3.0] — 2026-07-08

### Added — `khub wire`

Wire a workspace into an agent's context. `khub wire` injects an idempotent, marker-delimited block into `CLAUDE.md` (`--agents` mirrors it into `AGENTS.md`):

- A Claude Code `@.khub/schema.yaml` import, so an agent reasons in the workspace ontology even without running the CLI, plus the active preset and the declared types.
- The command surface, for when khub is installed.
- Schema-generic and minimal-diff: the block reflects the live schema and re-running replaces it in place. `--dry-run` prints without writing; outside a workspace it errors and suggests `khub init`.

### Distribution

- A khub Claude Code plugin ships from this repo as a single-plugin marketplace: `claude plugins add` the repo, install `khub@khub`, then `/khub:setup` installs the CLI (`uv tool install …@v0.3.0`) and runs `khub wire`.
- Install commands across the docs standardize on the pinned form `…@v0.3.0`. A `RELEASING.md` records the release checklist.

## [0.2.0] — 2026-07-08

### Docs — 2026-07-08: reference set and truth pass

- New user docs: `getting-started.md` (a worked firm-ops walkthrough, every command verified against a live run), `concepts.md` (the mental model), `schema.md` (the base block, layout × format, extending a schema).
- `firm-ops-preset.md` rebuilt from the shipped preset (9 types, 14 predicates); the drifted `build`/`decision`/`isms-doc` and the HQ cutover narrative are gone.
- `cli.md` reconciled against the code: lock path fix, `add`/`edit` `--format`, dropped the unshipped rows, corrected the search and orphans/strays wording.
- README gains a quickstart and a presets section; the planned Claude Code skill reads as planned.
- Design records de-polluted (spec IDs and release-tier framing removed) and brand-voiced (em-dash purge).

### Added — 2026-07-08: single-file collections (`layout: collection`)

One file holds every entity of a type as a row (`format: json|jsonl|yaml`; `path` names the file, format derivable from its suffix). The `project-repos.yaml` storage shape becomes first-class typed entities.

- Row identity: yaml/json collections are mappings keyed by slug; jsonl rows carry a reserved `slug` key. Duplicate slugs are malformed in all three formats (json via a duplicate-key-rejecting parse, never last-wins). `slug`/`type`/`body` are reserved row keys, rejected as schema field names on collection types.
- Every verb and gate works on rows. `add`/`get`/`edit`/`search` records add `locator: "path#slug"`; `get --format raw` prints only the row.
- Writes: one locked read-modify-write path — exclusive flock on `.khub/generated/locks/<type>.lock`, fresh in-lock uniqueness gate (the O_EXCL replacement), temp + fsync + atomic rename. No-op link/unlink never rewrite the file.
- Malformed contract (v1): any bad row makes the whole file malformed — no rows load, writes to the type refuse, derivative dangling reports are suppressed into a counted `suppressed_dangling` on `check`. Missing/empty file = zero entities.
- Git at row altitude: `log` attributes a commit to the rows whose values changed (blob diff per commit); `stale` judges a row only on its own `updated` (never the file's commit date); `backfill` skips collection types and reports it.
- Contract: `docs/collections-design.md`.

### Added — 2026-07-07: per-entity serialization formats (`format: json|yaml`)

A type may store entities as `.json`/`.yaml` documents instead of md — a single mapping, pure metadata.

- Prose rides in a reserved `body` field (string or null): `--body`/`--body-file` write it, `get` returns it as the body, the key never reaches frontmatter output or query filters. New `--body <text>` flag on `add`/`edit` alongside `--body-file`.
- `core/formats.py` is the one serialization strategy module (scan, verbs, gitlog, and search all dispatch through it); md keeps its fence semantics byte-for-byte.
- The meta-schema whitelists formats per layout; `jsonl` is collection-only, `gjson` stays named-but-undefined and rejected.

### Added — 2026-07-07: full-text search (`khub search`)

The `build-graph.py` replacement's missing half — BM25-ranked FTS5 over title + full body, built `:memory:` per invocation (stdlib sqlite3, zero new deps, derived and never stale).

- Raw MATCH syntax passes through (`"phrases"`, `OR`, `NEAR`, `prefix*`); malformed expressions are located errors. `--type` prunes at index-build time; non-md entities index their `body` field plus every scalar string field.
- Real-corpus recall: 75 hits for `modernization` on the HQ port vs 29 from the retired 500-char `.hq-graph.sqlite` index, ~250 ms end to end.

## [0.1.0] — 2026-07-07 (unreleased baseline)

The v1 engine, proven by the firm-hq cutover (241 entities, `kb.py`/`build-graph.py`/`hq.schema.yml` retired):

- Schema-generic core over Markdown-in-git: `init` (presets, measured non-destructive force-seed), `compile` (LinkML + Pydantic + JSON Schema, optional extra), schema introspection, `status`.
- Authoring verbs (`add`/`get`/`edit`/`link`/`unlink`/`remove`): referential-integrity hard-fail, capture never blocked, minimal-diff ruamel round-trip, O_EXCL slug minting, inbound-edge delete guard.
- Query and graph (`query`/`neighbors`/`impact`/`history`): in-memory networkx projection rebuilt per command.
- Integrity loop (`validate`/`check`/`stale`/`log`): declared-subset validation, graph-wide completeness/dangling/strays/cycles, orphans informational (`--strict` gates), git history at entity altitude.
- Projections (`reindex`/`viz`/`backfill`): OKF `index.md`, Cytoscape HTML, additive git-date/scaffolding backfill.
- CLI agent contract: JSON on a pipe, uniform `id`/`type`/`slug` records, located errors, reads never gate.
