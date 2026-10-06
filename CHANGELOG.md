# Changelog

Notable changes to khub. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); khub is pre-release.

## [Unreleased]

### Changed

- The Claude Code plugin (0.1.1) draws a command made only of khub calls joined
  by `;` or `&&` as one compact row per call. A command that pipes or redirects
  khub's output, or runs another program, keeps Claude Code's raw drawing.
- The block `khub wire` writes into `CLAUDE.md` ends with the install of the
  khub plugin for Claude Code, `/plugin marketplace add endgame-build/khub` and
  `/plugin install khub@khub`, where it named `npx skills add`. The block in
  `AGENTS.md` keeps `npx skills add endgame-build/khub`. Run `khub wire` or
  `khub upgrade` to refresh an existing workspace.
- The README, `docs/plugin.md` and the `setup` skill (plugin 0.1.2) say how to
  update the plugin: `claude plugin marketplace update khub`, then
  `claude plugin update khub@khub`.

### Fixed

- In the ctrl+o detailed transcript the plugin's compact row was followed by
  the raw result. The row now stands for the whole call there, and its `json`
  button shows the raw drawing.

## [0.28.0] — 2026-10-06

The agent skills leave the binary. khub becomes a Claude Code plugin
marketplace, and `install-skills` is gone. **Breaking:** `khub install-skills`,
`khub upgrade --no-skill` and the `skills` / `skills_error` keys of upgrade's
JSON are removed. **Adopting hubs:** `khub upgrade` leaves the skill copies
earlier releases wrote under `.claude/skills/`, `.agents/skills/` and
`.opencode/skills/`. Delete those folders and their `.gitignore` lines by hand,
then install the plugin or run `npx skills add endgame-build/khub`. Until the
old copy is gone, Claude Code lists the skill twice beside the plugin's.

### Added

- **The `khub` plugin for Claude Code**, under `plugin/`, installed with
  `/plugin marketplace add endgame-build/khub` then `/plugin install khub@khub`.
  The repo is its marketplace, through `.claude-plugin/marketplace.json`. The
  plugin carries the `khub` and `setup` skills and a small session mod. The mod
  draws a one-line band above the prompt: preset and version, entity count,
  what the session added, removed and edited, draft count, and the `check`
  verdict. In a verbose session it also draws each khub call Claude makes as a
  compact row. See `docs/plugin.md`.

### Changed

- `khub init`'s `skill_hint` is `npx skills add endgame-build/khub`, behind
  `cd <path> &&` when the workspace is not the working directory, and its
  prose names that command and the Claude Code plugin install. The path is
  quoted for the shell when it holds a space or another character the shell
  would split or expand.
- The block `khub wire` writes into `CLAUDE.md` and `AGENTS.md` ends with
  `npx skills add endgame-build/khub`. Run `khub wire` or `khub upgrade` to
  refresh an existing workspace.
- The skills moved from `skills/` to `plugin/skills/`. `npx skills add
  endgame-build/khub` finds them through the marketplace manifest.

### Removed

- `khub install-skills`, with its `--target`, `--skill`, `--global` and
  `--dry-run` options. Other agents install the skills with
  `npx skills add endgame-build/khub`.
- `khub upgrade --no-skill`, and the `skills` and `skills_error` keys of
  upgrade's JSON.
- The `missing_root`, `unknown_skill` and `unknown_target` error codes.

## [0.27.0] — 2026-09-29

Entities get aliases, the schema gets a recorded baseline to diff against,
and khub has one regex engine. **One breaking change:** an attribute
`pattern:` using lookaround or a backreference now refuses when the schema
loads (see Changed). The `check` JSON payload gains an `alias_conflicts`
key, every schema view gains the `aliases` base field, and `filter_error`
messages gain a list of declared names.

### Added

- `khub schema snapshot` records the current resolved schema at
  `.khub/schema.applied.yaml`, a tracked file, and `khub schema diff` lists
  what changed since: `{pending, changes: [{op, path, from, to}]}` at exit 0.
  With no snapshot, diff refuses (`no_schema_snapshot`) and names the command
  that records one. `init` and `upgrade` never write the snapshot, so a preset
  upgrade shows up as pending. This is the baseline later migration work
  builds on. (#74)
- Entity aliases. `aliases` joins the base block (a list, on every type), and
  every command taking an ID (`get`, `edit`, `link`, `unlink`, `remove`,
  `neighbors`, `impact`, `history`) resolves it once the slug lookups miss,
  case-insensitively; a real slug always wins. An alias two entities claim,
  in any spelling, refuses as `ambiguity_error`. A relation value or link target
  named by alias is stored as the real slug, so no edge on disk depends on
  another file's aliases. `add` refuses a slug that is already another
  entity's alias (`alias_taken`), which closes the path where an agent
  searching for "Initech" minted a duplicate of `initech-corp`. `check`
  fails on `alias_conflicts`: an alias two entities declare, or one that is
  another entity's slug. The `check` payload gains the `alias_conflicts`
  key, and `schema` views list the new base field. (#165)

### Changed

- A query filter naming an undeclared field (`--<field>`, `--has`,
  `--missing`) already refused with `filter_error`; the message now lists the
  declared names, as `unknown_type` and `unknown_predicate` do, so an agent
  corrects the call without a second lookup. The code is unchanged. (#161)

- **Breaking:** an attribute `pattern:` compiles as Go RE2, the engine template
  `{pattern:}` rules already used, so khub has one regex engine. A pattern
  using lookaround or a backreference now refuses when the schema loads
  (`invalid_schema`, naming the attribute) instead of compiling. Every
  shipped preset pattern is unaffected. Matching runs in linear time, so the
  one-second match timeout is gone, along with the write path where a timed-out
  `add`/`edit` exited 1 with no message. The `regexp2` dependency is dropped.
  (#160)

## [0.26.0] — 2026-09-28

Search gets faster, more forgiving and more informative, and every read
command gets faster with it. No flag is removed and no JSON key is renamed:
search records gain keys, their scores move, and search may now print one
`note:` line on stderr.

### Added

- `khub search --plain` takes plain words instead of FTS5 MATCH syntax. Each
  distinct word (case-insensitive) is quoted, prefix-matched from three
  characters and ORed with the rest, up to 64 words. Punctuation never
  becomes syntax, and text with no words returns no hits. `encrypt customer
  sensitive payload` now finds the requirement raw MATCH missed, and
  `nothing-matches-this` no longer fails on its hyphen. (#133)
- Search records carry `draft`, `orphan` and `stale`, judged exactly as
  `query` judges them, and `edges`: per-predicate counts, out-edges keyed by
  predicate and in-edges by `<source type>.<predicate>`. An agent can pick a
  hit to walk without a `get` per hit. (#131)
- Under `--plain`, records also carry `match` and `title_match`: the share of
  the query's distinct words the hit holds anywhere and in its title, each
  word weighted by inverse document frequency. A `title_match` of 0 marks a
  body-only collision. (#163)
- Search prints one `note:` line to stderr, before its output and in every
  format, when a result should not be taken at face value: rows `--limit`
  dropped (with the total), no hits at all (with the entities searched and
  the verbs to switch to), files the scan could not parse, or a `--plain`
  query cut at 64 words. Otherwise stderr stays empty. (#163)

### Changed

- Every read command is faster. The entity scan reads and parses files
  concurrently, and search indexes the bodies the scan already parsed
  instead of reading each file a second time. On a 10,000-entity workspace,
  search goes from 830 to 168 ms, `query` from 395 to 134 ms and `check` from
  705 to 443 ms. Scan order, malformed-file reports and the error a broken
  workspace reports are unchanged. (#40)
- Search ranks a title match five times a body match, and `score` reports
  that weighted value, so scores differ from 0.25.0. The weight was chosen
  against a new relevance regression suite
  (`internal/search/relevance_test.go`). (#162)
- A malformed `stale_days` in `.khub/config.yaml` now fails `search` (exit
  2), as it already failed `query`.
- The `khub` skill tells agents to use `--plain` for natural-language
  lookups, to search before `add`, and to act on a search note. Re-run
  `install-skills` to pick it up.
- Every documented command runs khub as `npx @endgame-build/khub` instead of
  by the unscoped name. The unscoped npm name `khub` is an unrelated package:
  without a local install, npx downloads and runs it — in CI with only a
  warning. The change reaches the README, `docs/getting-started.md`, the
  `setup` skill, and the install hint `wire` writes into `CLAUDE.md` and
  `AGENTS.md`. The README's `npx @endgame-build/khub@latest` alternative to
  installing is gone, because the commands after it assumed an install. The
  program is still `khub` (`node_modules/.bin/khub`). **Adopting hubs:**
  re-run `npx @endgame-build/khub install-skills` and `wire` (or `upgrade`) to
  pick up the new text, and change any agent allowlist entry that permits
  the unscoped command to `Bash(npx @endgame-build/khub *)`.

### Fixed

- Search read every entity file a second time after the scan, through a
  path outside the workspace-confined file handle the scan uses. It now
  reads nothing after the scan.
- The release workflow waits for a published version to become readable
  before installing it. Both post-publish checks retried for 30 seconds, less
  than npmjs.org takes to fan a new version out to its read path, so 0.25.0
  failed its own verification minutes after publishing correctly.

## [0.25.0] — 2026-09-09

A housekeeping release. No command, flag or JSON field changes; the one
contract that moves is the `firm-ops` preset's vocabulary, below. What the
binary does is unchanged — what shipped alongside it is smaller and cleaner,
and the npm package is now public.

### BREAKING

- **firm-ops 0.3.0 drops its vendor-specific fields.** `crm_id` → `crm_id`,
  `budget_id` → `budget_id`, `notes_folder` → `notes_folder`,
  `notes_folder_id` → `notes_folder_id`; `opportunity.source` and
  `project.source` are `referral | outbound | inbound | partner | event |
  existing-client`; `meeting.source` is `recording | manual` and
  `transcript.source` is `recording`; `project.external_repo` accepts any
  `owner/name`. An existing firm-ops workspace renames the keys in its
  frontmatter and maps its old `source` values (`recording` → `recording`, a
  partner name → `partner`) before `khub upgrade`.

### Added

- Four fuzz targets in `internal/canon` seeded from `parity/corpus`:
  `FuzzJSONValid`, `FuzzReconcatIdentity`, `FuzzSpliceIdempotent` and
  `FuzzEmitRoundTrip`. `go test` runs the seeds; `-fuzz` explores from them.
  They found both bugs listed under Fixed.
- `internal/omap` has a test file, pinning insertion order, the stable
  position of a re-`Set` key, `Delete`, and the documented `Keys()` aliasing.
- A doc comment on every exported identifier that lacked one, `internal/errs`
  first: each factory now names the code it builds and when it is raised.

### Removed

- The Python-era `tests/eval/` harness, the parity runner's `-differential`
  mode (`-bin-b`, `-seed-ws`, `-seeds`, `-len`, `-v`, `-workflows`) and three
  Go test binaries that had been tracked since the cutover.
- Internal planning and decision-history documents: `docs/history/`,
  `docs/npm-distribution.md`, `parity/DECISIONS.md`, `parity/yamlgate/REPORT.md`
  and nine related-work reviews. What remains in `docs/` describes what khub
  is and how to use it; the reasoning behind past decisions lives in git
  history. `CHANGELOG.md` keeps 0.23.0 onward in full and summarises earlier
  releases in one paragraph.

### Changed

- CLI help examples and unit-test fixtures use fictional names (`initech`,
  `northwind`, `noor`) in place of real ones.
- golangci-lint runs staticcheck (minus ST1005), errorlint, unused and errcheck
  alongside the choke-point rules; the tree is clean under them.
- GitHub Actions are pinned to commit SHAs, and goreleaser archives carry the
  commit timestamp so a rebuild of the same commit is byte-identical.
- The npm package is public on npmjs.org, published through npm trusted
  publishing (no stored token). Consumers drop the GitHub Packages registry
  mapping from `.npmrc`; `npm install -D @endgame-build/khub` works bare.
  npm stays the only documented install channel while the repository is
  private: `go install` and the release archives both need a public repo, and
  `--provenance` is off for the same reason.
- New at the root: SECURITY.md, CODE_OF_CONDUCT.md, NOTICE, .editorconfig,
  .github/CODEOWNERS.

### Fixed

- A document whose key is written in explicit form (`? key` on its own line —
  what the emitter itself writes for a key past 128 characters) now loads;
  it failed with "unsupported YAML node". `edit` still re-emits such a document
  whole rather than splicing it.
- A plain scalar that matches the YAML 1.2 integer pattern without holding a
  digit (`_`, `0x_`) stays a string. It resolved to an empty integer literal,
  which emitted as nothing and loaded back as null. Both found by the new
  fuzz targets in `internal/canon`.

## [0.24.0] — 2026-09-08

### BREAKING

- **The two build presets are one: `build-hub` 0.6.0.** The seventeen-type
  `build-hub` is deleted and the six-type `build-lite` takes its name — one flat
  `knowledge/`, two narrative singletons (`prd`, `arc42`), no collections.
  `build-lite` survives as an alias: `khub init build-lite` and `khub upgrade` on
  a `build-lite` workspace both resolve it and record `preset: build-hub`; the
  known-preset list `init` prints on a typo names `build-hub` and `firm-ops`
  only. Five types join the six, eleven in all: `capability` (`cap-`), `actor`
  (`act-`), `use-case` (`uc-`; `trigger: human | scheduled | event | external`,
  required), `system` (`sys-`; `owner`) and `api` (`api-`; `kind` and `status`
  required, and a required `provider` edge to a component so `check` names an
  interface nobody owns — `add` still captures without it). `component` gains
  `owner` (optional text in slug form), `lifecycle` (`experimental | production
  | deprecated`) and `tier` (`tier-1 | tier-2 | tier-3`); `requirement` gains
  `enforcement` (`ui | backend | database | external | review`). The stored
  edges, each named by its predicate and read as a verb: a use case is
  *performed by* an `actor`, *belongs to* a `capability` and is *served by*
  components (`served_by`, many, inverse `serves`); a requirement is *placed in* `capabilities`,
  *governs* `use_cases` and is *realized in* `realized_in`; a component is *part
  of* a `system`, *lives in* a `repo` and *consumes* apis (`consumes`, inverse
  `consumed_by`); an api is *provided by* its `provider`. Every inverse is
  computed at read time (`impact <cmp> --reverse --predicate served_by` is what
  breaks for users when a component dies; `--predicate consumes` on an api is
  its consumers). `component.kind` keeps `external`: a system is a group of
  components with one owner, and a vendor has APIs, not components we can name,
  so it stays an external component and its API's `provider` points at it.

  **Migration.** `khub upgrade` on a `build-lite` workspace resolves the alias,
  replaces `.khub/*` from the new preset (an edited layer file is kept as
  `<name>.bak`), restamps `preset: build-hub` in `config.yaml` and reports the
  resolved name as `preset` in `--format json`, scaffolds the five new type
  directories and templates and no new singleton (there is none), and reports
  no new findings: `owner` is optional, and the one required edge, `provider`,
  sits on a type that has no entities yet. A workspace on the old seventeen-type
  `build-hub` has no automatic path. Its nested `knowledge/{product,architecture}`
  files move to the flat layout by hand (`prd.md` and `arc42.md` up to
  `knowledge/`; decisions, components, capabilities and requirements into their
  flat directories), and the types the new preset does not declare — `domain`,
  `entity`, `boundary`, `quality-attribute`, `contract`, `baseline`, `pdr`,
  `roadmap`, `glossary`, `erd` — become invisible: the scanner never visits
  their directories and an edge from a surviving entity to one of them dangles.
  Two findings do fire until the move is finished: their retired templates
  under `.khub/templates/` are `stray_templates`, and a file of a surviving
  type still sitting in the nested tree is `misplaced`; delete the former,
  move the latter. Move what they held into the homes
  `docs/build-hub-preset.md` names (roadmap and glossary into `prd`, erd into
  `arc42`, pdr into `adr`, boundary and quality-attribute into a `requirement`
  with `kind: constraint` or `non-functional`) or carry their type blocks in
  your workspace's own `ontology.yaml` and `storage.yaml`. Every fixture that
  initialises a build preset was re-recorded: `case.yaml` files say `build-hub`
  where they said `build-lite`, the tree manifests gain the five templates
  (a manifest lists files, and an empty type directory is not one), and
  `schema`, `status`, `init`'s known-preset list
  and error output name the eleven types.

  For kb: `khub init build-lite . --force` on a corpus kb 0.14.0 wrote still
  graduates it — the alias resolves, the six types kb ships keep their ids,
  paths and predicates, none of the five new types has entities, and `check` is
  clean on the result.

- **The delivery layer is gone from both build presets.** build-lite 0.3.0 drops
  `feature-spec`, and with it `specs/` and the `requirements` predicate; build-hub
  0.5.0 drops `feature-spec`, `test-spec` and `work-package`. What is in flight
  belongs to the framework that runs the build (SDD or its like), which already
  owns `specs/`; khub records what must hold and why, and two tools writing one
  directory is one too many. kb 0.14.0 made the same cut, so build-lite is back
  at parity with it. An external spec points *in* through its own frontmatter
  (`requirements: [req-…]`, `decisions: [ad-…]`, `components: [cmp-…]`); khub
  neither scans it nor resolves those ids. `requirement.realized_in` is the one
  stored implementation record.

  **Migration.** `khub upgrade` replaces `.khub/` from the new preset (an edited
  layer file is kept in `<name>.bak`). With the types gone the scanner never
  visits `specs/`, so the files stay on disk and no finding mentions them — not
  even `stray`, which only speaks for a layout the schema still declares. Move
  them to the spec framework's directory, or keep the old type blocks in your
  workspace's own `ontology.yaml` and `storage.yaml`. `upgrade` also leaves the
  retired templates in place, and a template no type claims is a
  `stray_templates` finding that fails `check`: delete
  `.khub/templates/feature-spec.yaml` (build-hub: also `test-spec.yaml` and
  `work-package.yaml`) after upgrading. Every fixture that
  initialises a build preset was re-recorded: the tree manifests lose the
  delivery templates, and `schema`, `status` and error output list one type
  fewer in build-lite and three fewer in build-hub; three cases that exercised
  `feature-spec` now exercise `requirement.realized_in` instead.

### Added

- **`khub serve` — a read-only HTTP view of the workspace graph on `127.0.0.1`.**
  The server rebuilds from the live tree on every request, so it can never answer
  from a stale graph; the page fetches once on load, and a reload shows the
  current tree. Three views over one workspace: **Schema** (the ontology — one
  node per declared type, one edge per declared relation, derived inverses and
  the four base predicates hidden by default, declared-but-unused relations
  dimmed), **Graph** (entities, three zoom levels) and **List** (sortable rows,
  with ids as selectable text). Clicking a type in Schema inspects it rather than
  jumping to instances. The page never writes: it composes `khub link` /
  `khub unlink` commands as text to copy, so every mutation stays on khub's
  validated path with its refusals and exit codes intact.

  Read-only is structural, not policy — the guard rejects every method but GET
  and HEAD before routing, so a write endpoint cannot appear by accident.
  **Host-header validation is the load-bearing guard**, not the loopback bind:
  DNS rebinding re-resolves an attacker domain to `127.0.0.1`, defeating both the
  bind and same-origin, but the request still announces the attacker hostname in
  `Host`. No CORS header is ever set, and `frame-ancestors` is declared
  explicitly because it does not fall back to `default-src`.

  Refuses to start without a TTY (`TTY_COMPATIBLE=1` is the documented escape),
  so an accidental invocation by an agent is an immediate refusal rather than a
  hang. `--port` defaults to 7777. `viz` is unchanged and stays the static
  artifact you share or print; both now call one shared read path and colour a
  type identically.

- `/api/schema` carries the ontology as a graph (`introspect.EdgesView`), and
  `/api/type/{name}` returns what `khub schema show <type>` prints, so the
  browser view and the CLI cannot disagree about the schema.

### Security

- **Fixed a stored XSS in `viz`.** Its HTML inlines the graph JSON inside a
  script element, and an HTML parser ends that element at the first `</script`
  in the text — before any JavaScript runs. Once node records began carrying the
  entity `title`, a title of `</script><img src=x onerror=alert(1)>` broke out
  and executed for anyone opening the shared file. Now escaped as a JSON unicode
  escape, so the parsed value is byte-identical, with a regression test pinning
  both containment and round-trip.

### Changed

- Serialize existing-workspace writes, preserve file modes during atomic publication,
  and reject unsafe storage paths, symlinks, overlapping ownership and incomplete scans.
  `init` and `upgrade` run their wire, skill and index tails under the same lock, and
  concurrent first writers retry the lock-file open on a transient `ENOENT`.
- Share bounded schema-pattern validation and resolved relation identity across
  mutation, integrity, queries and browser navigation.
- Add transactional upgrade preview (`--dry-run`), rollback journals and additive
  `dry_run` / `removed_types` JSON fields. The candidate copy prunes vendored
  directories and skips non-regular files.
- Add ordered multi-ID `get`; keep single-ID output unchanged and refuse raw batches.
- Emit plain help on pipes (the Rich terminal help is pinned on a pty); bound cycle
  reports to one witness per component.
- Batch search indexing and git-date lookup; cache inverse edges and collection reads
  within each operation; read each type through one `os.Root` scoped to its directory
  (`query` at 2000 entities from 157 to 96 ms). No persisted index.
- `check` reads `.json`/`.yaml` only inside declared storage trees when hunting
  `misplaced` entities; a root-level data file whose `type` names a declared type is
  no longer reported.

See [audit release notes](docs/architecture-audit-release-notes.md) for contract
changes, recovery limits and validation commands.

## [0.23.0] — 2026-09-02

The `-NNN-` ordinal is gone from minted ids and every refused call exits 2. Both
change the corpus and the contract; every existing corpus needs the rename in
Migration below, and every fixture that pinned an ordinal id or an exit 1 on a
refusal was re-recorded.

### BREAKING

- **`khub add` no longer numbers ids.** An id is `<prefix>-<YYYY-MM-DD>-<slug>`,
  with the prefix and the date each declared per type in `storage.yaml`
  (`id_prefix`, and the new `id_date: true|false`, default false) and each
  optional; the slug is the slugified `name`, else `title`. Shipped shapes:
  build-lite dates `adr` (`ad-2026-01-15-use-postgres`) and mints `req-`, `cmp-`,
  `rp-`, `fs-` plus the title; build-hub dates `adr` and `pdr` and mints the rest
  undated; firm-ops mints bare slugs.

  The ordinal was `max(existing) + 1` over a directory scan — a read-modify-write,
  and it raced. Two branches, two worktrees or two agents each saw `ad-003` as the
  highest and each minted `ad-004-<a different slug>`; the *filenames* differed,
  so git merged both cleanly, `add` never saw a collision and `check` reported
  nothing, while the skill was telling agents to write `ad-004` in prose as the
  short handle. Deleting the highest-numbered entity also freed its number for
  reuse. Minting is now a pure function of the schema, the type, the frontmatter
  and the title: it reads no siblings, so there is nothing to race on. The same
  title mints the same id twice, which `add` refuses (exit 2, `slug_taken`, naming
  `--id`), and which git surfaces as an add/add conflict on one filename rather
  than two files that quietly coexist.

  Only decisions carry a date, because only a decision legitimately recurs under
  one title — "use Postgres" is decided, superseded and revisited, which is what
  `supersedes` is for. The other types are registries of what currently holds,
  where a repeated title *is* a duplicate. The date is the day the id was minted
  (an explicit `--created` on the `add` dates it) and stays that; nothing checks
  it against `created`, which `edit` can rewrite.

  Three fallbacks went with the ordinal, each now a refusal that names the way
  out: a type with neither `name` nor `title` (`no_slug_source` — the type-name
  fallback would mint one id per type without a number to tell them apart), a
  by-value `id_prefix` whose deciding attribute is unset (`id_prefix_undecided`,
  `needs --kind <…>`), and a minted collision (nothing is ever suffixed —
  `acme-2` is gone, minted or explicit). `--id` is unchanged: slugified, written
  as given, and the documented answer to every one of the three.

- **`validate` holds every non-singleton entity to its type's id scheme.** Three
  arms, one finding per slug on field `id`: the prefix the type mints (and, for a
  by-value prefix, the one its deciding attribute chose), the date when `id_date`,
  and the retired `NNN-` ordinal — rejected with a message naming `git mv`, unless
  the title itself starts with those digits (`cmp-404-handling`). Types declaring
  no scheme used to be exempt; they are not any more, so an unmigrated corpus
  fails the gate rather than drifting. The bare `NNN-slug` leniency for "minted
  before `kind` was set" is gone with the fallback that produced it. `khub schema
  show` gains `id_prefix`, `id_date` and the rendered `id_shape`
  (`ad-YYYY-MM-DD-slug`; `null` on a singleton) — the same string every `id`
  finding quotes — and titles a minting type `adr (file · ids ad-YYYY-MM-DD-slug)`.
  `id_prefix` or `id_date` on a singleton is a schema error: its id is its type
  name, so it mints nothing.

- **`requirement` mints one literal `req-`** in build-lite and build-hub, replacing
  the three derived from `kind` (`fr-`, `cst-`, `br-`). `kind` keeps its values and
  stays required; build-lite's gains `non-functional`. The cost is real: the id
  gate can no longer catch a requirement labelled with the wrong `kind`. What it
  buys is that `kind` became an ordinary field — `khub edit <id> kind constraint`
  now works, where before it made the id wrong and the only fix was
  remove-and-re-add — and `add requirement` no longer needs `--kind` to mint.

- **build-lite gains `repo` as a type** (`knowledge/repos/`, `rp-slug`; `repo`
  matching `^[a-z0-9._-]+(/[a-z0-9._-]+)+$` and `status: active | archived`, both
  required) and `component.repo` becomes an edge to it instead of a text
  attribute, so a component cannot claim a codebase nobody registered.
  build-hub's `repo.repo` pattern widens to the same nested-group form.

- **firm-ops requires the slug source on every type**: `title: { required: true }`
  on `project`, `meeting`, `fragment`, `case-study` and `partnership` (the other
  four already required `name` or `title`). With no ordinal to fall back on, an
  untitled entity cannot be minted, so the schema says so up front.

- **Every refused call exits 2.** A `Located` failure — the call was malformed, or
  would have written something the schema forbids — renders as before (one line
  on stderr, or the `{"error": {"code", "message"}}` envelope under the JSON gate)
  and exits 2 instead of 1, including the text-mode `remove` refusal. Exit 1 is
  now reserved for a gate that ran and failed (`validate`, `check`), where the
  workspace is what is wrong rather than the call; usage errors stay 2. An agent
  reads the code as what to do next: on 2 correct the call and retry, nothing
  was written; on 1 fix the workspace.

- **The build-lite and build-hub `prd` and `arc42` templates take the section
  names a reader outside khub already knows** (ported from kb 0.14.0). Heading
  matching is a case-sensitive prefix and order is part of the contract, so a
  document written against the earlier template fails `body_shape` after
  `khub upgrade` until its headings are renamed — nothing else about the corpus
  moves. `arc42` follows the arc42 spine (docs.arc42.org), with four optional
  appendices after `Glossary`:

  | before | now |
  |---|---|
  | `Context and scope` | `Context and Scope` |
  | `Solution strategy` | `Solution Strategy` |
  | `Building blocks` / `Building block view` | `Building Block View` — asks for one mermaid block (a gap, not an error) |
  | `Data model` (build-lite) | *gone* — fold into `Crosscutting Concepts` (arc42 tip 8-7 puts the domain data model there) |
  | `Crosscutting concepts` | `Crosscutting Concepts` |
  | `Decisions` / `Architecture decisions` | `Architecture Decisions` |
  | `Risks and technical debt` | `Risks and Technical Debt` |
  | `Introduction and goals`, `Constraints`, `Runtime view`, `Deployment view`, `Quality requirements`, `Glossary` (build-hub) | `Introduction and Goals`, `Architecture Constraints`, `Runtime View`, `Deployment View`, `Quality Requirements`, `Glossary` — still required in build-hub; optional in build-lite, where they are new |

  `prd` orders for the reader and stops colliding with the ontology: `Glossary`
  sits before `Features`, and the capability section is `Features` rather than
  `Functional requirements` — `requirement` is already a type with
  `kind: functional`.

  | before | now |
  |---|---|
  | `Target user` | `Target Users` |
  | `Functional requirements` | `Features` |
  | `Non-goals` | `Non-Goals` |
  | `Success metrics` | `Success Metrics` |
  | `Glossary` (build-lite) | still required, moved before `Features`; in build-hub an optional pointer at the glossary singleton |
  | `Roadmap` (build-lite) | now `optional: true`; in build-hub an optional pointer at the roadmap singleton |

  Added, all `optional: true`: `Document Purpose`, `Deferred`, `Alternatives &
  Recommendation`, `Open Questions`, `Assumptions Index`, `Grounding Evidence`
  on `prd`; `Deferred Decisions`, `Open Questions`, `Assumptions Index`,
  `Grounding Evidence` on `arc42`. `adr` gains optional `Alternatives` and
  `Prevents`, word minimums on its three original headings and a
  `forbidden_text` on hedging inside `Decision` (`tbd`, `we should consider`).
  build-lite's `requirement`, `component` and `repo` and build-hub's `component`
  ship a template for the first time — `requirement` declares `sections: []`,
  the other three only optional headings, so no existing body owes anything;
  each carries a top-level `hint` and review lenses, and every shipped
  template's own scaffold satisfies its own rules. Every `NNN` in a hint is
  gone. Migration: `khub upgrade`, then rename the headings per the maps above;
  `khub validate arc42/arc42` and `khub validate prd/prd` name the first one
  still wrong, one at a time.

- Preset versions: build-lite 0.1.0 → 0.2.0, build-hub 0.3.0 → 0.4.0,
  firm-ops 0.1.0 → 0.2.0.

### Migration

`khub validate` reports every surviving `-NNN-` id as an `id` error, so a
corpus that has not been migrated fails the gate rather than drifting. There is
no `khub migrate-ids`: a renamer cannot fix prose references, and it cannot fix
external files whose frontmatter points in — both of which the docs actively
encourage. Three loops cover the three presets; run them from the workspace
root.

**firm-ops** strips the ordinal from every id. Folder-layout types (opportunity,
project, partnership) are directories, so the directory moves:

```bash
for p in opportunities projects partnerships; do
  for d in "$p"/[0-9][0-9][0-9]-*/; do
    [ -d "$d" ] || continue
    git mv "${d%/}" "$p/$(basename "$d" | sed 's/^[0-9]*-//')"
  done
done
for p in meetings transcripts fragments case-studies identity/team clients; do
  for f in "$p"/[0-9][0-9][0-9]-*.md; do
    [ -e "$f" ] || continue
    git mv "$f" "$p/$(basename "$f" | sed 's/^[0-9]*-//')"
  done
done
```

**Decisions** (build-lite `knowledge/decisions/ad-`, build-hub
`knowledge/architecture/decisions/ad-` and `knowledge/product/decisions/pd-`)
substitute the ordinal with the file's own `created` date. On a dated type an
unmigrated `ad-001-x` reports `slug carries no date — this type mints
ad-YYYY-MM-DD-slug`, not the `git mv` message, because the date arm runs before
the ordinal arm (kb's order); this loop fixes both in one move:

```bash
for f in knowledge/decisions/ad-[0-9][0-9][0-9]-*.md \
         knowledge/architecture/decisions/ad-[0-9][0-9][0-9]-*.md \
         knowledge/product/decisions/pd-[0-9][0-9][0-9]-*.md; do
  [ -e "$f" ] || continue
  d=$(sed -n 's/^created: //p' "$f" | head -1)
  git mv "$f" "$(dirname "$f")/$(basename "$f" | sed -E "s/^(ad|pd)-[0-9]+-/\1-$d-/")"
done
```

**Every other prefixed type** strips the ordinal, and requirements also rewrite
their three prefixes to one — the only move that changes the leading token:

```bash
for f in knowledge/components/cmp-*.md specs/fs-*.md \
         knowledge/product/capabilities/cap-*.md \
         knowledge/architecture/boundaries/bound-*.md \
         knowledge/architecture/quality-attributes/qa-*.md \
         knowledge/architecture/components/cmp-*.md \
         specs/feature-specs/fs-*.md specs/test-specs/ts-*.md specs/work-packages/wp-*.md; do
  [ -e "$f" ] || continue
  git mv "$f" "$(echo "$f" | sed -E 's#/([a-z]+)-[0-9]+-#/\1-#')"
done
for f in knowledge/requirements/{fr,cst,br}-[0-9][0-9][0-9]-*.md \
         knowledge/product/requirements/{fr,cst,br}-[0-9][0-9][0-9]-*.md; do
  [ -e "$f" ] || continue
  git mv "$f" "$(echo "$f" | sed -E 's#/(fr|cst|br)-[0-9]+-#/req-#')"
done
```

None of these fix references. After renaming, `khub check` names every edge that
no longer resolves — fix those with `khub link`/`khub unlink` (or `khub edit`),
run `khub reindex`, then grep your prose and any external frontmatter for the
old ids yourself. Two ids that differed only by ordinal (`fr-001-x` and
`cst-001-x`) collapse onto the same `req-x`; the second `git mv` refuses rather
than clobber, and you decide which title to change. build-lite workspaces also
need a `repo` entity per codebase and `component.repo` re-pointed at it
(`khub add repo --title … --repo org/name --status active`, then
`khub edit <component> repo rp-<slug>`); `khub upgrade` brings the schema in.

### Added

- **`khub upgrade`** — bring an existing workspace up to the khub on PATH.
  Deliberately not a re-init: `init` never writes over a `.khub/` file the
  workspace owns, and the tree and singleton passes read the workspace's own
  layer files, so a type or a section shipped in a new preset could never reach
  an existing workspace. `upgrade` replaces `.khub/{ontology,policy,storage}.yaml`
  and `.khub/templates/*.yaml` from the preset recorded in `.khub/config.yaml`
  (an edited file is copied to `<name>.bak` first; a file the workspace never
  had is created with no backup; an unchanged file is not reported), restamps
  the provenance `version`, re-reads the schema, scaffolds what the ontology
  gained, then re-installs the skills, re-wires the agent files and regenerates
  `index.md` — each tail non-fatal, reported as `*_error`. `--no-schema` keeps
  `.khub/` as it is and reports `schema_drift`; `--no-skill` and `--no-wire`
  skip their tails. Refuses outside a workspace, and in one whose config
  records no preset (`no_preset`). Ported from kb 0.14.0's `cmd_upgrade`.
- **`khub init` writes `index.md`.** The index is the cheapest read of the
  whole corpus, and a workspace that had never run `reindex` simply had none —
  an agent's first look found nothing. Written after the singletons, so it
  lists them; reported as `index` (`created`/`updated`/`unchanged`) in the JSON
  payload, or `index_error` with an `index skipped:` stderr note when the scan
  holds malformed files. A force re-init no longer reads it as a corpus seeded
  over.
- **Body-content vocabulary in templates.** A section may declare `optional`,
  `word_count: {min, max}`, `required_text` and `forbidden_text` (literals,
  case-insensitive, or `{pattern}`), and `code_blocks: [{lang, min, max}]`; a
  template may declare a top-level `hint` (leads the scaffold as a comment)
  and `lenses`. Bounds are non-negative whole numbers, `max` below `min` and a
  `code_blocks` rule with no bound are refused, and the reserved keys
  (`repeat`, `pattern`, `images`, `lists`, `tables`, `min_tokens`,
  `max_tokens`, `budget`) are refused rather than ignored — a template that
  half-works is worse than one that says no. Rules read prose only (comments
  and fences blanked), skip an empty section unless it holds a fenced block,
  and count each CJK character as a word. Ported from kb 0.14.0.
- **Lenses** — `{code, name, instruction, when, after, section}` review
  prompts a template declares and `validate <type>/<slug>` hands to whoever is
  reviewing the body; khub never answers one. `when` compares values as the
  scalars khub writes to disk, against the frontmatter with schema defaults
  resolved in (a `draft: false` nobody wrote still matches); `after` orders
  the list with a stable one-per-round Kahn sort so an edit moves only what it
  meant to move; a `when` field the type does not declare, a `section` the
  template does not declare, an unknown `after` code and a cycle are each
  `template_invalid`.
- **`validate` reports `gaps`, `body` and `lenses`.** The payload is
  `{count, errors, gaps, body, lenses}`: `errors` alone gate, body-rule
  findings land in `gaps`, and a single `type/slug` target of a templated md
  type also receives `body` (`{words, sections: [{heading, words}]}`) and the
  applicable `lenses` in template order (`null` / `[]` otherwise).
- **`check` reads bodies.** Each templated type's template is loaded once; one
  that does not parse is `template_invalid` against `<type>/*`, a body whose
  required headings are missing or out of order is `body_shape` — both fail
  the gate — and a body whose prose does not satisfy a section rule is `thin`,
  informational under `--strict` too: every rule a template gained would
  otherwise turn a green corpus red on upgrade, which is the one thing that
  would stop anyone from declaring a rule at all.

### Changed

- **`validate`'s summary line** is `Validated N entities; E errors, G gaps`
  (the `; 0 errors` short form is gone), and a gap prints with a `-` marker:
  `<id>: - body: '## Context' 2 words of prose, at least 50 asked for`.
- **`check`'s JSON** gains `template_invalid` and `body_shape` after
  `missing_templates`, and `thin` last, after `strict` — the one bucket that
  never affects `passed`. Text prints the two errors after the
  declared-template lines and `thin <id>: <reason>` last (`(informational)` on
  the passing path).
- **`search` drops HTML comments before indexing; fenced code stays.** A
  scaffold's hint comments are the template's words, not the author's, so
  every freshly added entity was a strong hit for whatever its own hints said
  (`search forecloses` returned every new ADR). A mermaid block names the
  components and a bash block names the command, which is what search is for.
  Comments are removed, not blanked, so snippets stay readable.
- **`khub install-skills` replaces each skill directory** rather than
  overlaying it: a file under `.<host>/skills/<skill>/` that khub no longer
  ships is reported `removed` and deleted (a `--dry-run` reports it and deletes
  nothing), and a directory that empties is pruned. Previously a release that
  renamed or dropped a file left the old copy beside the new one for the agent
  to read both.

### Fixed

- **A `##` inside `<!-- -->` is not a heading.** Heading discovery read
  comment-masked prose as structure, so a required heading present only inside
  a comment satisfied the contract and a commented-out one was never reported
  missing. An unterminated `<!--` masks to the end of the document.
- **Fence pairing follows CommonMark.** A closing fence repeats the opening
  character at least as many times and carries no info string, may be indented
  up to three spaces, a backtick fence's info string may not contain a
  backtick, and an unterminated fence runs to the end. A heading inside a
  four-backtick fence holding a three-backtick sample no longer counts as
  structure.
- **Heading numbering is stripped only when a `.` or `)` follows it.**
  `## 2026 goals` keeps its digits, so a template declaring that heading can
  be satisfied by a body containing it verbatim.
- **`##` may be indented up to three spaces**, as CommonMark allows.

## Earlier releases

Releases 0.1.0 through 0.22.1 (2026-07-07 to 2026-08-30) built the engine,
the presets, the agent skills, the parity fixtures and the Go rewrite that
replaced the original Python implementation at 0.19.0. Their individual
entries live in this repository's git history.
