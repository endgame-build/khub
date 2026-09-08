# Changelog

Notable changes to khub. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); khub is pre-release.

## [Unreleased]

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

## [0.22.1] — 2026-08-30

Internal only: no command, schema, output or file-format change. Every entry
below is invisible from the outside, which is why none of the three PRs behind
it carried a changelog line of its own.

### Fixed

- **Errors are matched through `errors.Is`/`errors.As`, never through
  `os.IsNotExist` or a bare type assertion.** Both stop matching the moment
  anything wraps upstream, and khub's error text is fixture-pinned product, so
  the failure would have arrived as changed output rather than a compile error.
  One site was worse than the rest: `pathBugMessage` asserted unchecked and was
  safe only because its caller gated on a matching predicate first — converting
  one without the other would have turned a misprint into a panic. They now
  share a single classifier. Three byte-identical hand-rolled `Unwrap()` walkers
  went with it. No fixture byte moved; `internal/cli` gained its first test file
  to pin what the corpus cannot see.

### Changed

- **CI pins that floated.** `goreleaser-action` moves to the Node 24 runtime
  (v6 targeted Node 20, which GitHub had begun force-running on 24), and
  `golangci-lint` is pinned instead of tracking `latest`, so a linter release
  can no longer turn the tree red with no commit of ours.

## [0.22.0] — 2026-08-30

### BREAKING

- **The workspace schema is three layer files, and the base block is embedded.**
  `.khub/schema.yaml` is gone; a workspace now carries `.khub/ontology.yaml`
  (the domain: per-type `attributes`, `relations`, `when`), `.khub/policy.yaml`
  (`required`, `orphan`) and `.khub/storage.yaml` (`layout`, `path`, `format`,
  `id_prefix`, `template`), merged at load time into the same resolved contract
  as before — the layers merge on top-level key, so one file carrying all three
  blocks also resolves. The `base` block ships embedded in the binary
  (`khub schema base` prints it) and is never copied into a workspace; an
  authored `ontology.base` is rejected — a type overrides a base attribute by
  redeclaring it. Preset directories follow the same shape
  (`<preset>/{ontology,policy,storage}.yaml`). The pre-split single-file layout
  is not read; no live workspace was on it, so there is no migration. The split
  exists so the ontology is a clean projection surface: `ontology.yaml` is
  purely the domain, which is what an RDF/SHACL export or an external modeling
  tool wants to see.
- **`acyclic` moved with the split**: it rides on the relation in ontology (a
  property of the predicate), while `required`/`orphan` are policy and
  layout/path/format/id_prefix are storage. A policy or storage entry naming a
  type ontology never declared is a resolve error.

### Added

- **Declared templates** — `storage.<type>.template: <name>` names the body
  template (`.khub/templates/<name>.yaml`; a name, never a path), so two types
  can share one and a rename cannot silently disable scaffolding and the body
  contract: a declared name pointing at nothing is a `check` finding
  (`missing_templates`) — capture is never blocked, so the broken link never
  takes `add` down — and a template file no type claims is another
  (`stray_templates`); both fail the gate. Undeclared keeps the convention (a
  file named for the type); `template: false` opts out, and the opted-out type
  still claims its conventional stem so keeping the file is not a finding.
- **`khub schema base`** — print the effective base block (fields and
  relations), since no workspace file carries it any more.
- **Derived singleton cue links** — a `when` cue is pure domain language;
  `khub wire` appends a singleton's edit target
  (`— edit [knowledge/prd.md](knowledge/prd.md), never add a second`) derived
  from its storage path at render time, so a moved file can never strand a
  stale link in the ontology. `CLAUDE.md` imports the workspace's layer files
  (only the ones that exist), and a link destination carrying spaces is
  wrapped per CommonMark.
- **Storage defaults** — a type declared in ontology with no storage entry
  stores one file per entity under a directory named for the type. An entry
  that exists must state its `layout` — the default is for wholly absent
  entries, so a half-written entry cannot silently become `layout: file`.

### Fixed

- **Unknown schema keys are rejected again** — an unknown top-level key in a
  layer file (`version` stays legal), or an unknown key inside `ontology:`,
  is a resolve error instead of being silently dropped; a typo'd `entities:`
  can no longer resolve to a zero-type schema with every gate green. A
  workspace whose `.khub/` has `policy.yaml`/`storage.yaml` but no
  `ontology.yaml` is likewise an error naming the missing declaring layer.
  `khub init` holds preset files to the same vocabulary: a preset declaring
  `ontology.base` is rejected before anything is written (copying it used to
  scaffold a workspace no command could load), an unknown top-level key in
  any preset layer file is an error, and a policy/storage file authored
  without its `policy:`/`storage:` wrapper no longer reads as an empty layer
  that silently drops every declared gate. A supplied base document that
  yields no `ontology.base` is a loud error too.
- **`template` is a storage-matrix cell** — declaring `template:` (the
  `false` opt-out included) on a collection or non-md type is a schema
  error: nothing ever reads a body template there, so the old behavior left
  `check` permanently red over a file no verb would consult.
- **init scaffolds from the resolved schema** — the entity tree and singleton
  passes read the workspace's RESOLVED types (written and preserved layer
  files alike, over the embedded base) instead of a hand-kept mirror of the
  resolver's template and storage defaults. A re-init that would mix schema
  generations (a preserved ontology beside a freshly written storage layer
  annotating a type it never declared) now fails loudly, and the unwind
  removes exactly the files that run wrote, leaving the workspace as it was.
- **Schema errors name the failing file and layer** — a parse failure in
  `policy.yaml`/`storage.yaml` is no longer headlined against
  `ontology.yaml` (the wrap adds no path of its own; the underlying error
  already carries one), and the `required`-is-singleton-only violation is
  located at `policy.<type>.required`, the layer it is authored in, instead
  of `storage.<type>`.
- **`check` reports a template file no type can read** — the stray sweep let
  every declared type claim a template stem whatever its layout or format,
  while `add` and `validate` both skip non-md and collection types. So
  `.khub/templates/<collection-type>.yaml` read as claimed and was never
  reported, though no verb would ever consult it: the renamed-template hole
  seen from the claiming side, which is the case `stray_templates` exists to
  catch. One predicate now answers "does this type read a template?" for all
  three verbs.
- **A schema layer khub cannot examine reports the real cause** — an
  unsearchable `.khub`, a dangling symlink or an I/O error all read as "the
  file is absent", so khub blamed a missing `ontology.yaml`: the wrong cause,
  and the one a reader acts on. An unreadable *file* was never affected — the
  permission error already came from the read.

## [0.21.0] — 2026-08-29

### Changed

- **khub is Apache-2.0; MIT is retired.** Both are permissive and neither restricts what
  anyone may do with khub, so nothing about who may use it changes — what changes is how
  much the license says. Apache-2.0 grants a patent license in writing and terminates it
  for anyone who sues over the code (§3); MIT grants none and is silent, leaving the
  question to be argued rather than answered. It also writes down three things MIT leaves
  unwritten: contributions arrive under the same terms without a separate CLA (§5), the
  grant does not extend to the name (§6), and a redistributor is told exactly what to
  carry (§4) — so khub can travel inside someone else's deliverable without anyone
  guessing at the obligation. ENDGAME holds copyright on every commit and MIT permits
  sublicensing, so the change required no consent-gathering. Not retroactive: every
  release through 0.20.0 stays MIT under the terms it shipped with, and the
  `@endgame-build/khub` versions already published keep the `"license": "MIT"` they carry
  — an npm version is immutable and none is being re-published. The README badge and the
  npm package's `license` field move with the file; every dependency is MIT, MIT-0,
  BSD-3-Clause or Apache-2.0, and neither Apache-2.0 dependency ships a NOTICE, so khub
  has none to propagate.

## [0.20.0] — 2026-08-29

### Changed

- **khub distributes through npm.** One package, `@endgame-build/khub`,
  carrying a prebuilt binary per platform behind a dependency-free launcher,
  published to GitHub Packages on every release tag. The primary install is a
  per-repo `devDependencies` pin — one khub version per repo, upgraded by a
  reviewed one-line PR — which is what keeps khub's byte-stable output
  contract from drifting between teammates. npm is the only channel: the curl
  downloader, `install.sh`, its Cloudflare Pages hosting at `khub.end.game`
  and the `publish-install` workflow are all deleted — a script designed to
  locate a repo-scoped GitHub token was khub's largest attack surface, and it
  wrapped one command. A machine-global install is `npm install -g
  @endgame-build/khub`. The wired agent block and the `setup` skill now give
  the npm instructions (the wired block had still said `uv tool install`,
  wrong since the Go cutover). Spec: `docs/npm-distribution.md`.

### Added

- **Schema introspection lists derived inverse predicates** (#14). A declared inverse has been
  a first-class read surface since v0.11.0 — `get --edges` returns it, `query --has/--missing`
  accepts it — but `schema show` and `schema edges` omitted it, so the one discovery surface
  `skills/khub/SKILL.md` tells agents to build writes from hid predicates those agents may use.
  Every relation row now carries `derived`; stored relations keep their declaration order and
  come first.
- **Two collection types resolving to one file are rejected** (#15), at schema-resolve time
  rather than by `check`. Sharing an inventory file made each type's `query` claim the other's
  rows, and the write lock is keyed per type — so two writers took different locks on one file.
  New error code `collection_path_collision`.

### Changed

- **`reindex --dry-run` previews an incomplete graph instead of refusing** (#16). It writes
  nothing, and its purpose is to show what the index would become — which is exactly what is
  wanted while diagnosing the bad merge that caused the malformed file. A stderr line names the
  files it could not see; a real `reindex` still refuses.
- **A path that cannot exist as addressed reports `internal_path_error`, not `os_error`** (#18).
  The blanket catch exists so a read-only directory reads as one clean line, but the bug that
  motivated it was itself an OSError — `delete()` built `prd.md/prd.md` for a singleton and the
  loud failure is what exposed it. ENOTDIR, EISDIR and ENAMETOOLONG now get their own code. The
  errno cannot distinguish khub's mistake from a file placed where a directory belongs, so the
  message names both.
- **`--id` is documented as required for types you do not title** (#17). Without a `name` or
  `title` the slug is minted from the TYPE NAME (`repo`, `repo-2`), and those meaningless keys
  are what every later `link` and `get` must use. The fallback itself is unchanged and
  deliberate: it is what keeps capture from being blocked on naming something.

## [0.19.0] — 2026-08-16

### Changed

- **khub is a Go binary.** The Python implementation is retired; a single static binary
  replaces `uv tool install`, starting roughly 8x faster and needing no interpreter. The
  CLI surface, JSON contracts, exit codes and on-disk bytes are unchanged — the port was
  developed against a suite of recorded fixtures that pinned all four, and it passes them
  unaided along with a 50-seed x 40-command differential against the Python build.
- **All datetimes render ISO 8601 on every surface** (#42). `--format json` used to emit
  `2026-05-01 13:00:00+02:00` for a datetime while the file it had just read said
  `2026-05-01T13:00:00+02:00`, because the CLI encoder stringified where the disk encoder
  called `isoformat()`. The space form is not ISO 8601, so `Date.parse`, Go's
  `time.RFC3339` and older `datetime.fromisoformat` all rejected or mishandled it.
  Anything parsing the old form sees a changed string. Plain `date` values are unaffected,
  which is why this survived so long.
- **`khub edit` preserves more of a hand-written file.** The Go writer splices only the
  tokens whose values changed, so a deliberately quoted scalar, a bare `empty:`, and an
  author's sequence indentation all survive an edit that previously normalized them.

## [0.18.0] — 2026-07-26

### Changed

- **Bare `khub wire` now writes both context files, creating either that is missing.** It
  used to update only files that already existed and create none, so a repo carrying just
  `CLAUDE.md` stayed invisible to every agent that reads `AGENTS.md` — and a repo with
  neither got a hint instead of a wiring. `--target` still narrows to one file, an
  existing file still keeps everything outside the markers, and a re-run is still
  `unchanged`. The "No CLAUDE.md or AGENTS.md to wire" hint is gone: there is no such
  state any more. `init` is unaffected — it already wired both.

## [0.17.1] — 2026-07-25

Repository only — no behaviour change.

### Fixed

- **`__init__.py` said 0.14.1 while `pyproject.toml` said 0.17.0.** Three releases bumped
  one and not the other, so CI's version-consistency step was red and the 0.15.0–0.17.0
  tags carry the drift. Nothing at runtime was affected — `cli/main.py` prefers installed
  metadata, and `khub --version` from the v0.17.0 tag reports 0.17.0 — only the fallback
  constant was stale.

### Added

- **`smoke.sh`** — 73 assertions over the CLI itself: build-lite, build-hub and firm-ops,
  every command with its exit code, the wired block's content (capture cues, the singleton
  links, CLAUDE.md's import directive and its absence in AGENTS.md), and the
  misplaced-entity gate. The unit suite proves the core; this proves the binary. khub had
  no such harness — the previous one lived in `kb/smoke.sh` and left with kb. Runs in CI
  after pytest.

## [0.17.0] — 2026-07-25

### Changed

- **A singleton's capture cue now links its own file.** The wired block told an agent to
  "edit the existing document, never add a second" without saying *which* document, so it
  had to run `khub schema show prd` or guess a path. All seven singleton cues across
  build-lite and build-hub now carry a Markdown link to their declared `path`. Content
  only — `wire` already emitted `when` verbatim, so no code changed. The link is authored
  rather than derived to keep the wording the preset author's; a test holds every shipped
  singleton cue to containing its own `path`, so a moved file or a typo'd link fails the
  suite instead of misdirecting an agent. firm-ops declares no singletons and is
  unaffected.

## [0.16.0] — 2026-07-25

### Added

- **`check` reports a misplaced entity** — a file outside every declared layout whose
  frontmatter names a type the schema knows. This is the one breakage a schema-driven
  scan is blind to by construction: the globs follow the schema, so a file the schema
  no longer covers is not *absent*, it is *unscanned*, and every other gate passes over
  it in silence. Found by graduating a flat build-lite corpus onto build-hub's nested
  layout — `khub init` reported `entity_files_modified: 0` and `validate` returned
  clean while seeing none of the eight entities. It is the mirror of `stray` (a
  non-entity inside a layout) and fails the gate for the same reason; the two never
  double-report the same file. Deliberately narrow: a README carries no `type` and an
  unrelated doc carries an unknown one, so neither fires.

### Changed

- **kb moved to its own repository.** The standalone build-lite implementation and
  its two parity suites left this repo for `paved-kb`, extracted with history. khub
  goes back to being one thing — the general engine — and kb owns the build tooling.
  The `build-lite` preset **stays here**: kb's layout is flat (`knowledge/requirements`)
  where build-hub's is nested (`knowledge/product/requirements`), so build-lite is
  what a kb corpus graduates onto without moving a file. The parity tests now run in
  kb's CI against a pinned khub, which is where the burden belongs — but drift is
  detected one repo away, so a khub release that changes a payload will fail kb's CI
  rather than this one's.

## [0.15.0] — 2026-07-25

### Added

- **kb tracks main's 0.13.0/0.14.x contract.** Merging main in broke three parity
  guarantees, each fixed by matching khub rather than by relaxing the test: `search`
  now indexes scalar attribute values as well as the body (khub's 0.13.0 rule), with
  dates and bools excluded by their *declared* type — khub holds a date as a `date`
  object so it drops out of its "is a str" filter, while kb's reader keeps the string,
  and filtering on the schema is what keeps the two BM25 indexes identical; `check`
  emits main's new `draft_required_singletons` key; and `build.schema.yaml` was
  regenerated for the `when` cues main added to the preset. Reading those cues needed
  block scalars (`>-`) in kb's YAML subset — the last shape it refused.

- **kb reads a khub workspace's own body templates.** Dropped into a `.khub/`
  workspace, kb now holds entities to *that* workspace's `.khub/templates/*.yaml`
  contract rather than the four Markdown templates it ships, and seeds new bodies
  from them. Found by running kb over a build-hub corpus, where it had been checking
  build-lite's prd/arc42 sections and missing every template build-hub declares. Its
  YAML reader grew the two shapes those files need — a mapping that starts on a `-`
  item, and a plain scalar containing a comma — and is now checked against
  `ruamel.yaml` on every preset, template and schema file in the repo.
- **`kb/smoke.sh`** — 108 assertions across four cells: khub and kb, each over
  build-hub and build-lite, plus cross-tool parity and the graduation path. Every
  exit code asserted, payloads compared as data.

- **`id_prefix`, and enumerated ids for every minted slug** (breaking: minted ids change shape). `add` now mints
  `<prefix>-NNN-<slug>` where a type declares `id_prefix`, and `NNN-<slug>` where it
  does not — so a corpus reads in authoring order and an entity can be named in prose
  by a short stable handle (`ad-004`) instead of a whole title. The number is one past
  the highest in use, padded to three and counting past it (001, 045, 1000); each
  prefix keeps its own sequence, so `fr-001` and `cst-001` coexist. `id_prefix` takes a
  literal (`adr: ad`) or a by-value mapping (`{ by: kind, map: {...} }`) that puts the
  kind in the filename, where a mislabelled entity is visible; the map must cover its
  enum exactly or the schema is rejected at resolve time. An explicit `--id` is
  untouched. build-lite and build-hub declare the prefixes their docs already used in
  prose (`ad-`, `fr-`, `cst-`, `cmp-`, `fs-`, `wp-`, …); firm-ops declares none.
  `validate` holds entities to the scheme: a slug that does not follow it, or whose
  prefix disagrees with the attribute that chose it (a `fr-` file whose `kind` says
  `constraint`), is an error on field `id` — a disagreement no other gate can see,
  since the enum is legal and every relation resolves. Types declaring no prefix are
  unchecked, so `firm-ops` corpora are untouched — but **every non-singleton
  `build-lite` and `build-hub` type now declares one**, so entities in those
  workspaces that predate this release are reported until their files are renamed by
  hand. There is no `rename` verb yet.
  See [`docs/schema.md`](docs/schema.md#enumerated-ids).

- **`kb/` — build-lite standalone**, a separate, dependency-free
  implementation of the `build-lite` preset for one build project driven by
  opencode. A drop-in directory: `skills/kb/SKILL.md` for the agent, and
  `scripts/` holding `kb.py`, `build.schema.yaml` and the Markdown body templates.
  Eight commands — scaffold an entity, keep an edge honest, walk the derived graph,
  sweep the corpus — in ~850 lines of stdlib Python against khub's 7,000 and six
  dependencies. `build.schema.yaml` is khub's `core.yaml` base and the build-lite
  preset combined into one file in khub's own vocabulary, and it is the only file to
  edit to add a field.
  `build.schema.yaml` is a verbatim concatenation of `presets/core.yaml` and
  `presets/build-lite/schema.yaml`, so it is 1:1 by construction;
  `tests/test_build_lite_standalone.py` diffs it against both sources key by key and
  fails on any difference at all, generates the body templates from the preset's own
  renderer, checks that both tools mint the same id, and runs the compatibility claim
  in both directions. The rationale, the option comparison, and the four khub invariants
  it deliberately breaks are in
  [`docs/build-lite-standalone.md`](docs/build-lite-standalone.md); it ships nothing
  into the `khub` package and changes no khub behaviour. A corpus it authors
  validates and checks clean under `khub init build-lite`, unmodified.
## [0.14.1] — 2026-07-25

Repository only — the built wheel is byte-identical to 0.14.0. The eval harness that produced
0.14.0's findings was run from a scratch directory and existed nowhere in the repo, so nothing
in 0.14.0 was reproducible by anyone else.

### Added

- **`tests/eval/run_build_lite.py`** — the build-lite retarget of the wiring eval, with the
  three task sets the 0.14.0 work was measured on: `narrative` (27 ordered engineering asks,
  adherence end to end), `statements` (8 bare facts, isolating activation), and `cues` (one
  fact × four imperatives, isolating the cue word with the failing variant as an in-run
  control). It overrides three firm-ops-shaped things and edits nothing in `base.py`/`run.py`:
  `preflight` asserts the PATH khub rather than force-installing over the operator's global
  binary, `build_wired` takes the preset as a parameter and can lay a workspace inside a copy
  of a real repo, and the turn cap is a flag — `run.py`'s hardcoded 14 assumes an empty
  workspace, and a smoke run on a real codebase hit it mid-exploration.
- **`tests/eval/README.md`** documents both, and states the rule the design depends on: read
  results from in-run controls, never from cross-run baselines.

## [0.14.0] — 2026-07-25

An agentic eval on an unfamiliar codebase (27 tasks over a copy of `httpie/cli`, wired vs
unwired) put wired adherence at 78% against unwired's 0%. The wired losses were almost all
**inaction, not wrong action**: four of six were single-turn replies with no tool call at all.
Asked "Policy: credentials must never appear in output. Note it.", the agent answered
"Noted." and stopped. The schema said how to write and the agent did that well; nothing said
when.

### Added

- **`when:` — a per-type capture trigger in the schema.** One line of domain language naming
  the moment a type should be recorded ("a rule is stated that the system must satisfy or must
  never violate"), declared next to `layout` and `required`. `khub wire` renders every trigger
  into the agent context files under "Record as you go", and `schema show` exposes it, so an
  agent can recognise the moment rather than only execute a command once told. Per-domain
  knowledge belongs in the schema, so a new type ships its own trigger and no surface code
  learns a type name. All three presets declare one on every type, pinned by a test.

### Changed

- **The wired block states the CLI rule and the hand-edit recovery.** "Every write goes through
  the CLI — the only path that validates against the schema and resolves relations. If you edit
  an entity file by hand anyway, run `khub validate` on it immediately." An unvalidated
  hand-edit is how a workspace acquires a field no default gate will report (see 0.13.0's note
  on `validate --strict`).
- **The block now says reading the graph is a khub operation.** The eval's one wired
  read-bypass spent 13 `Read` calls on `knowledge/**` instead of `khub query`, and ran out of
  turns before writing anything. The block previously led with writes.
- **The khub skill gained a "Know when to write" section** — the same triggers, plus the rule
  that a stated fact about the system is a write while a question about it is a query.
- **The capture cues are mapped, and the overclaim is gone.** A 20-turn A/B held one fact
  byte-identical and moved only the closing imperative: `Record it.` recorded 5/5 with the
  right type and kind, `Note it.` recorded **0/5** — replying "Noted." every time — and a bare
  statement with no imperative recorded 0/5, answering "No task given yet." The capability was
  never in doubt; one verb worked and its synonyms did not, because "Note it." collides with
  "Noted." as a reply token. The block and the skill now say that "note it", "write it down",
  "log it", "FYI", "heads up", "for the record" and a bare statement all mean record it, that an
  acknowledgement is not a record, and that entities are written with `khub add` rather than by
  choosing a path (the second observed failure: `Write it down.` sent the agent hunting for a
  target file). Re-measured on the same four variants: **30% → 70%**, with `Note it.` at 5/5.
  The block's previous claim that capture happens "without being asked" measured 0/5 and has
  been removed rather than left overclaiming.

## [0.13.0] — 2026-07-25

A smoke test drove every command against a real `build-lite` workspace and found eleven
defects no test caught: khub worked for a human at a TTY and misled an agent at a pipe.
This release closes all eleven. No schema changed; every fix is in the CLI, the search
index, or the query layer.

### Fixed

- **Write verbs emit JSON on a pipe.** `add`, `edit`, `remove`, `init`, `link` and
  `unlink` gated on a literal `--format json` while every read command used the shared
  output gate, so a piped agent got prose when a command SUCCEEDED and JSON when it
  failed — the exact asymmetry the error renderer exists to prevent. All six now route
  through `emit`. `link`/`unlink` gained `--format` and a record carrying `changed`,
  since both verbs are idempotent and exit 0 either way: prose alone could not tell a
  write from a no-op.
- **`search` reaches frontmatter.** md entities indexed the body alone, so
  `khub search python` returned nothing while `stack: python` sat in the file — on the
  one format every preset uses by default. Every format now indexes body plus each
  scalar field, which is what the README and the khub skill always claimed.
- **`query --has` / `--missing` accept attributes.** Both answered from the graph, so an
  attribute raised `No field 'repo' on type 'component'` — a message that was simply
  false. Absent, null, or empty counts as missing. Together with the search gap this had
  left **no route** to find an entity by an attribute value, which pushes an agent back
  to grep: the bypass the wiring exists to prevent.
- **Ids resolve case-insensitively, and writes store the canonical spelling.** `add --id
  CMP-001-Api` slugifies to `cmp-001-api`, and every read of the string the caller passed
  failed. Both resolvers fold case, on the bare and qualified forms alike. Folding on read
  alone would have been worse than none: `link` stored the caller's raw string and deduped
  by exact match, so three spellings of one node became three parallel edges, each
  reporting `changed: true`, and `unlink` of another spelling was a silent no-op.
- **`--no-template` is refused on a templated type.** Its only outcome there was an
  entity `validate` rejected on the very next run.
- **`schema show` / `schema edges` expose what they enforce.** `acyclic`, `inverse`,
  `pattern` and `default` were resolved, enforced, and invisible — an agent told to
  discover the schema at runtime could not learn why a write was rejected.
- **`schema edges` emits one row per distinct declaration.** Keying rows by predicate name
  kept only the first-seen targets, so build-lite's `supersedes` (adr → adr, feature-spec →
  feature-spec) advertised `feature-spec --supersedes--> adr` — an edge `validate` rejects.
  Merging the targets does not fix it; `from` × `to` is a cross product, so a union just
  adds the reverse claim too. Only types declaring a predicate identically now share a row.
- **A drafted singleton is reported.** A drafted non-required singleton (`arc42`) left the
  active subgraph and appeared in no list, with `check` passing; a drafted required one
  (`prd`) was reported twice, once as "missing" for a file sitting on disk. The two
  conditions are now disjoint, with `draft_required_singletons` naming the subset that
  fails the gate — and the text view reports the optional case too, which by definition
  never reaches the failure path.
- **`--missing <name>` skips types that cannot carry the name.** Untyped, `--missing kind`
  returned every singleton alongside the entities that genuinely lack it, diluting a gap
  query with rows no author could ever close.
- **`search` no longer double-counts titles.** `title`/`name` already populate the
  dedicated FTS `title` column; folding them into the body sweep as well shifted BM25
  ranking and let a snippet excerpt frontmatter as if it were prose.
- **The eval's bypass detector sees singleton writes.** A singleton's storage path is a
  file, so the prefix test could never match it and an agent that hand-edited the PRD
  scored on-rails. firm-ops declares no singletons, which is why the published adherence
  numbers never exposed it.

### Changed

- **`target_not_empty` says what `--force` does.** Adding khub to a repo that already has
  code is the common case, and the remedy read like it would overwrite the repo; it
  scaffolds alongside and modifies nothing already there.
- **The cardinality error names the verb and its semantics.** "use edit to replace" was
  silent about the trap: on a many-valued relation `edit` replaces the whole list where
  `link` appends.
- **`query` records carry `title`.** Listing a type and reading one field per row was
  N+1 — one `query` plus one `get` per entity.
- **The wired CLAUDE.md block and the khub skill** no longer claim `--format json` is
  universal; it is false for the operator commands (`reindex`, `viz`, `backfill`, `wire`),
  which take no `--format` at all. The skill also now recommends `validate --strict` in
  CI, since capture is never blocked and a typo'd field name is otherwise never reported.

### Added

- **`tests/test_build_lite_e2e.py`** — build-lite shipped with schema-resolution tests
  only and was never passed to `khub init` anywhere in the suite, because `fresh_ws` is
  firm-ops-hardcoded. A new `ws_for` fixture scaffolds any preset; the file pins the
  fresh-workspace `--strict` gate, the orphan sweep agreeing across its three read sites,
  `--force` leaving a populated repo untouched, and each integrity gate.
- **CI asserts the version is consistent.** `__version__` is only a fallback, so it had
  drifted four minors behind `pyproject.toml` unnoticed.

## [0.12.0] — 2026-07-25

### Added

- **`build-lite` preset** — `build-hub` cut to necessity: six entity types (`prd`,
  `arc42`, `requirement`, `adr`, `component`, `feature-spec`), four predicates, four
  body templates. A type earns a slot only if it is referenced by id, walked as a
  graph, or gated by `check`; every one of the fourteen cuts names its replacement,
  and the schema header carries the order the rest comes back in. See
  [`docs/build-lite-preset.md`](docs/build-lite-preset.md).
- **`orphan: true` type-level schema key** — declares that edge-less is a type's
  expected state, so `check` stops sweeping it, `--strict` stops failing on it,
  `query --orphan` stops flagging it and the `status` count stops including it.
  Defaults to `false`. Unlike `required` it is not singleton-only: any type whose
  members are legitimately unwired may declare it.

### Changed

- **`build-hub` 0.2.0 → 0.3.0: `external-system` folded into `component`.** A vendor
  or neighbouring product is now a `component` with `kind: external`. `kind` becomes
  **required** — it is the sole carrier of the ownership boundary once
  `contract.provider` stops being a `component | external-system` union — and
  `component.repo` drops from required to optional, because an external has no
  codebase and khub schemas cannot express "required unless `kind: external`". The
  component↔codebase mapping is a review-time fact now, not a `check` gate; an
  all-internal engagement may re-tighten `repo` in its own `.khub/schema.yaml`.
  Twenty types, seventeen predicates in twenty-three declarations. No field was lost:
  `external-system` carried only `title` and `consumes`, both already on `component`.
- **A freshly scaffolded workspace can pass `check --strict`.** build-hub's five
  narrative singletons (and build-lite's two) now declare `orphan: true`. They are
  roots nothing points at by design — every stored edge runs up the durability ladder
  and the prd sits above its top — so reporting them was a finding no authoring could
  close, and it made `--strict` fail every correct workspace out of the box. The
  exemption removes no signal: a missing required edge is still reported by
  required-completeness, which names the field.

### Migration

Existing workspaces are unaffected — `.khub/schema.yaml` is flattened at init, so
these preset changes reach new inits only. A workspace re-initialised onto build-hub
0.3.0 must:

1. Convert each `external-system` entity into a `component` with `kind: external`
   and retarget its inbound edges (a `contract.provider` pointing at one now
   points at the component).
2. Backfill `kind` on **every** existing component — it went from optional to
   required, so until then `check` reports each one active-but-incomplete.

## [0.11.0] — 2026-07-25

A stress test of the `build-hub` preset — three agents plus a manual pass — found
thirteen defects; two independent code reviews of the fixes found nine more. All
reproduced before fixing and re-verified after.

### Fixed

- **`khub remove <singleton>` crashed** on every singleton (`NotADirectoryError`,
  raw traceback, file left on disk). `delete()` re-derived the path instead of using
  `entity_path()`, and a singleton's `path` IS the file. This was also the only
  recovery path for a corrupted singleton — every write verb rightly refuses one.
- **`init --force` destroyed workspace-owned files.** It rewrote `.khub/schema.yaml`,
  `config.yaml` and every template, while reporting `entity_files_modified: 0`
  (`_entity_hashes` skips `.khub/`). Re-init now preserves them and reports
  `preserved`; genuinely missing files are still restored. It also refuses a re-init
  whose preset differs from the workspace's, which would otherwise mint files for
  types the preserved schema does not declare.
- **Cycle detection covered only `depends_on`.** Three ADRs each superseding the next
  passed `check` clean, with `history` giving a different answer per entry point.
  Relations now carry `acyclic: true`; `depends_on` remains acyclic by contract with
  or without the flag, so no existing workspace loses the check. Self-cycles
  (a self-superseding record written by hand or by import) are caught too.
- **A `draft` required singleton satisfied `check`**, while a draft target already
  failed to satisfy another entity's required relation — the two `required` gates
  disagreed about the same flag. `check` now reports it, and distinguishes
  *unpublished* from *absent*.
- **`reindex` and `viz` wrote from a scan that had silently dropped files.** A
  malformed collection is not in the graph, so `reindex` rewrote `index.md` with an
  entire type erased and exited 0. Both refuse now, naming the files.
- **Derived inverse edges were unreachable.** Neither preset declared any `inverse:`,
  so `get --edges` never returned one and "is this ADR superseded?" read as
  "current". build-hub now declares the inverses its comments already promised
  (`superseded`, `consumed_by`), `query --has/--missing` accepts them (making
  `--missing superseded` the "still current?" query), a stored forward edge is never
  shadowed by an inverse, and an inverse is rejected on a type that cannot carry it.
- **Failures were not machine-readable.** They now use the same output gate as
  successes, so a piped agent no longer gets a JSON record on success and prose on
  failure. `remove` gained `--format`; `check --format json` reports `strict` and
  `draft_singletons`; `OSError` renders as a normal failure instead of a traceback
  (`BrokenPipeError` is re-raised, so `khub schema | head` still works).
- **`''` and `null` are now consistent.** `--field ""` clears to null on write;
  `validate` rejects a stored `''`, which `check` already counted as missing.
- **A scoped `validate <entity>` reported other types' template errors** and exited 1.
- **`sections: []`** declares an empty body contract instead of erroring — and stays a
  template, so `add` still seeds the title and `init` still creates the singleton.
- A yaml collection's **document header comment** survived only until the first write.
- **`backfill --dry-run`** omitted the collection-skip line the real run prints.

### Changed

- `build-hub`: `supersedes` is `acyclic: true` on adr/pdr/feature-spec and declares
  `inverse: superseded`; `consumes` declares `inverse: consumed_by`.

**Upgrading an existing workspace.** `.khub/schema.yaml` is workspace-owned and is
never rewritten, so a workspace scaffolded before this release keeps its current
schema. Cycle detection on `depends_on` and every engine fix apply immediately. To
pick up build-hub's new `acyclic`/`inverse` declarations, copy those keys into your
`.khub/schema.yaml` — `khub init --force` deliberately will not do it for you.

## [0.10.0] — 2026-07-25

khub's agent skills now ship inside the package, so installing them is a file
copy instead of a network clone. The Claude Code plugin marketplace — a second,
Claude-only channel for the same two files — is gone.

### Changed

- **`khub install-skills` copies from the package.** No Node, no network, no
  clone of a private repo. It shelled out to `npx skills add
  git@github.com:endgame-build/khub.git` because `plugin/skills/` sat outside the
  wheel and the files genuinely were not on disk at runtime — which made a
  two-file copy unusable for anyone without SSH access to the ENDGAME org, and
  broken offline.
- **New flags:** `--target claude|agents|opencode` and `--skill khub|setup` (both
  repeatable), `--global` for the home directories, `--dry-run`. Default writes
  both skills to `.claude/skills/`, `.agents/skills/`, and `.opencode/skills/`.
  Every file is compared first and reported `created` / `updated` / `unchanged`,
  so the command is safe to re-run — and worth re-running after a khub upgrade.
- **Skills moved to `skills/` at the repo root** and reach the wheel through
  hatch's `force-include`. That location is deliberate: it is the container
  skills.sh discovers without a manifest, so `npx skills add <repo> -s setup`
  still works as the bootstrap for a machine with no khub yet. Two channels, two
  moments — npx bootstraps, `install-skills` is the steady state.
- The `khub wire` block no longer advertises `/khub:setup`; it names
  `uv tool install` and `khub install-skills`.

### Removed

- **The Claude Code plugin marketplace** (`.claude-plugin/marketplace.json`,
  `plugin/.claude-plugin/plugin.json`). **Migration:** run `khub install-skills`,
  which writes `.claude/skills/` by default. An already-installed plugin keeps
  working but no longer updates.
- **`install-skills --agent`** — it was the npx passthrough.

### Fixed

- The gitignore written by a project-scope install covered the whole target
  directory, so a repo committing its own `.claude/skills/<name>/` had every file
  added to it afterwards silently ignored. It now ignores `<dir>/<skill>/` per
  installed skill.
- `--global` required a khub workspace, failing with "No .khub workspace found"
  in exactly the case a machine-wide install is for.
- The opencode `--global` target ignored `XDG_CONFIG_HOME`, writing skills where
  opencode never looks while reporting them installed.

## [0.9.1] — 2026-07-25

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
