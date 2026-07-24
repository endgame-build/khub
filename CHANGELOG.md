# Changelog

Notable changes to khub. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); khub is pre-release.

## [Unreleased]

### Fixed

- **`khub install-skills` corrupted stdout on a pipe.** The gate deciding whether to capture
  npx's own output keyed on `--format json`, but the gate deciding whether to *emit* JSON is
  `want_json()` — which is also true for any non-TTY. Piping the command at the default
  `--format text` printed npx's progress and then the JSON document, leaving stdout
  unparseable. Both now use `want_json()`.
- **A missing `npx` no longer exits 0.** `skipped-no-npx` was a correct best-effort outcome
  while this was an `init` tail; as a command you run on purpose it is a failure, so a CI step
  on a Node-less image cannot report success with no skill installed.
- **A failed install now carries npx's diagnostics.** Under capture, the ssh/auth error npx
  printed was swallowed, leaving `{"action": "failed"}` and nothing to debug. `SkillOutcome`
  gained `detail`, echoed to stderr.
- **`khub init`'s hint is path-aware.** After `khub init firm-ops ./my-hub` it now prints
  `khub -C ./my-hub install-skills`; `install-skills` resolves its root from the working
  directory, so the bare hint found no workspace — or an unrelated one above cwd.
- **The eval harness ran `khub init --no-skill`**, a flag 0.9.0 deleted, so `tests/eval/`
  died with `CalledProcessError` before any agent ran.

### Removed

- Dead `_REC`/`_FLD` git-format separators in `core/gitlog.py`, orphaned with `khub log`.

## [0.9.0] — 2026-07-25

A surface cut. Four features earned less than they cost: three are gone, one moved out of
the command it was bolted onto. No core module changed — every removal was a CLI-layer or
projection-layer surface. Net: ~700 fewer lines of source, ~500 fewer of tests, one fewer
runtime dependency.

### Added

- **`khub install-skills`** — installs the agent skill via `npx skills`, split out of
  `khub init`. `--agent <name>` (repeatable) targets specific coding agents instead of npx's
  auto-detect; `--dry-run` prints the exact command and runs nothing; `--format json` emits
  the outcome. A failed install exits 1 — inside `init` the same failure was best-effort so
  the scaffold could survive it, but a command whose only job is the install does not.

### Removed

- **`khub log`** — git history at ontology altitude, including per-row commit attribution for
  collection types, was over half of `core/gitlog.py`. `khub history <id>` answers the
  graph-side question (the supersession chain) and `git log -- <path>` answers the rest.
  `khub stale` and the shared commit-date helpers are untouched.
- **`validate --fix`** — `khub backfill` already writes a missing `updated` from git via the
  same helper, plus `created` and per-type required scaffolding. `--fix` also made `validate`
  (a read gate) a writer, blurring the validate/check separation. **Scope note:** scoped date
  repair does not survive the removal. `--fix` accepted a `type/slug` target; `backfill`'s
  `--type` gates only its scaffolding pass, so date backfill is tree-wide. Accepted rather
  than widening `backfill`'s behaviour inside a cut.
- **The interactive wizard and `--agent`** — 180 lines of source, 301 of tests, and a
  questionary/prompt_toolkit dependency to prompt humans in an agent-first tool, behind a gate
  that switched it off for agents, pipes, and CI. Every input is now a flag or an argument;
  a missing one is the usage error it already was headlessly (exit 2). `--agent` is removed
  outright rather than kept as a silent no-op, so a script still passing it fails loudly.
  Output still adapts to the reader: Rich on a TTY, JSON on a pipe or under `--format json`.
- **The `npx skills` install inside `khub init`**, and its `--no-skill` flag — scaffolding a
  workspace hard-depended on Node and on SSH access to the skill repo. `init` now prints the
  follow-up command instead of running it.

### Changed

- `validate --format json` no longer carries `fixed`; nothing writes, so there is nothing to
  report.
- `init --format json` carries `skill_hint` (`"khub install-skills"`) instead of a `skill`
  outcome object.
- `khub init` seeds both `CLAUDE.md` and `AGENTS.md`; the per-agent file selection came from
  the wizard's multiselect, which no longer exists. `khub wire --target` still narrows it.

## [0.8.0] — 2026-07-24

### Added

- **`layout: singleton`** — a type living at one fixed file (0..1 entity; the slug is the type
  name, so `khub get prd` resolves). `add` refuses a second instance; a type-level
  `required: true` makes `check` report the singleton's absence (`missing_singletons`).
- **Body templates** — per-type YAML at `.khub/templates/<type>.yaml` (flattened from the
  preset's `templates/` dir at init; workspace-owned, editable). `add` seeds new md bodies from
  the template (`--no-template` opts out); `khub init` CREATES every missing md singleton from
  its template (creations only — WS-003 amended: init may create singletons, never modifies an
  existing entity file; `InitResult.singletons_created` reports them); `validate` holds every
  templated body to the template's section headings as an ordered subsequence (prefix-match,
  numbering stripped, extras allowed) — a `body` finding, capture never blocked. Entry keys
  `heading`/`hint`/`text` now; `optional`/`repeat`/`pattern` reserved and rejected.
- **build-hub narrative singletons** — prd (required), roadmap, glossary, arc42, erd become
  singleton types with shipped templates (21 types total); templates also ship for the ten
  ID-enumerated record types.

### Changed

- **Presets are directories** — `src/khub/presets/<name>/schema.yaml` (+ `templates/*.yaml`);
  `--preset-source` uses the same convention. BREAKING for flat `<name>.yaml` preset sources.

- **`build-hub` preset rewritten to the converged ontology (preset 0.2.0)** — the
  paved-road-hub convergence (2026-07-24). Layout moves to two knowledge roots
  (`knowledge/{product,architecture}` durable truth · root `specs/` delivery state). New
  governance types: `domain` (body = blueprint narration; `depends_on` narrowed to
  `domain→domain`), `entity` (`owner` single + required — single-writer schema-enforced),
  `boundary`, `quality-attribute`, `component` (required `repo` edge; carries the churny
  `consumes` side), `baseline` (yaml registry). `contract` becomes a hub-authored yaml-format
  entity (`provider → component | external-system`; machine spec under `contracts/specs/`,
  outside the scan). `repo` collection moves to `knowledge/architecture/repos.yaml`.
  `feature-spec` absorbs `feature` (the FS is the feature record); `solution-spec` dropped.

### Removed

- **`build-spoke` preset, its parity test, and its doc** — spokes carry no khub workspace:
  code plus a plain `entities.yaml` the hub `resource`-links; every decision, repo-local
  included, is a hub adr/pdr. The corpus is layout-invariant: monorepo and multi-repo use the
  same `build-hub` preset.

## [0.7.1] — 2026-07-10

### Fixed

- CI: `test_neighbors_rejects_removed_both_flag` asserted a Rich-rendered flag string that wraps differently across terminals, so it passed locally but failed under GitHub Actions — it now checks Click's exit code (the actual contract). v0.7.0's release workflow failed on this test; 0.7.1 is the first release that builds green. Install pins bumped to `@v0.7.1`.

## [0.7.0] — 2026-07-10

### Added

- MIT `LICENSE`; `pyproject.toml` gains `license`, `authors`, `keywords`, `classifiers`, and `[project.urls]`; a `py.typed` marker ships the package's types to downstream consumers.
- CI workflow (ruff + mypy + pytest across Python 3.11–3.13) and a tag-triggered release workflow (test, `uv build`, GitHub Release; no PyPI while the repo is private).
- `CONTRIBUTING.md`, issue and pull-request templates, and a dependabot config.
- `docs/README.md`, a Diátaxis index over the existing docs. The README gains license/Python badges and a "why not a folder / database / RAG store" comparison.

### Changed

- `khub wire` is agent-agnostic. Bare `wire` updates whichever agent files already exist (`CLAUDE.md` gets the `@.khub/schema.yaml` import; `AGENTS.md` gets a plain schema pointer, since non-Claude agents can't resolve the import); `khub wire --target claude|agents|both` creates a specific file (replaces the old `--agents` bool). `khub init` seeds the agent files for the selected agents — both `CLAUDE.md` + `AGENTS.md` when nothing is selected (including non-interactive/`--agent` runs). Fixes `AGENTS.md` previously receiving a Claude-only `@import` it couldn't read.

### Removed

- Stale internal notes (`docs/internal/omnigraph-comparison.md`, `audit-2026-07-05.md`) and the superseded planning specs (`plan/specs/FS-006`, `FS-007`): they described the removed LinkML compiler and called the shipped engine "design stage."

### Fixed

- Lingering compiler references in `core/errors.py`; the design-memo command table now marks never-shipped verbs (`path`, `build`, `export --okf`, `diff-preset`, `rename`) as planned; the `khub:setup` skill pins `@v0.7.0`.

## [0.6.0] — 2026-07-09

### Removed — LinkML / `khub compile`

Dropped the entire LinkML compile path: the `khub compile` command, `core/compile.py`, `core/linkml_emit.py`, `core/determinism.py`, the `khub[compile]` optional extra (linkml, ~77 packages), and the generated artifacts (`.khub/generated/{schema.linkml.yaml,models.py,schema.json}`).

- **Why:** those artifacts had zero runtime consumers. Validation, CRUD, query, and `check` all run natively on the resolved schema (`core/resolve.py` + `core/schema_model.py` + runtime field/relation checks), never on the generated Pydantic. LinkML was an unused compile target and the heaviest dependency.
- **No behavior change:** every command works exactly as before; `khub init` no longer prints "Generated artifacts skipped" and no longer writes `.khub/generated/` (that path now holds only collection lock files, created on demand). The test suite runs ~3× faster without the linkml import.
- **The contract is the khub schema** (authored YAML, resolved in memory). If JSON Schema or typed models are ever needed (an MCP server, editor integration), khub will emit them natively from the resolved schema.
- Docs reframed to match (README, design-memo, concepts, schema, cli, getting-started).

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
- Writes: one locked read-modify-write path: exclusive flock on `.khub/generated/locks/<type>.lock`, fresh in-lock uniqueness gate (the O_EXCL replacement), temp + fsync + atomic rename. No-op link/unlink never rewrite the file.
- Malformed contract (v1): any bad row makes the whole file malformed: no rows load, writes to the type refuse, derivative dangling reports are suppressed into a counted `suppressed_dangling` on `check`. Missing/empty file = zero entities.
- Git at row altitude: `log` attributes a commit to the rows whose values changed (blob diff per commit); `stale` judges a row only on its own `updated` (never the file's commit date); `backfill` skips collection types and reports it.
- Contract: `docs/collections-design.md`.

### Added — 2026-07-07: per-entity serialization formats (`format: json|yaml`)

A type may store entities as `.json`/`.yaml` documents instead of md: a single mapping, pure metadata.

- Prose rides in a reserved `body` field (string or null): `--body`/`--body-file` write it, `get` returns it as the body, the key never reaches frontmatter output or query filters. New `--body <text>` flag on `add`/`edit` alongside `--body-file`.
- `core/formats.py` is the one serialization strategy module (scan, verbs, gitlog, and search all dispatch through it); md keeps its fence semantics byte-for-byte.
- The meta-schema whitelists formats per layout; `jsonl` is collection-only, `gjson` stays named-but-undefined and rejected.

### Added — 2026-07-07: full-text search (`khub search`)

The `build-graph.py` replacement's missing half: BM25-ranked FTS5 over title + full body, built `:memory:` per invocation (stdlib sqlite3, zero new deps, derived and never stale).

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
