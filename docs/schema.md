# Schema

**How a khub workspace is configured: author it once, extend it as the work demands.**

A schema is authored in khub's own YAML vocabulary, split across three layer files under `.khub/`:

| File | Layer | Carries |
|---|---|---|
| `ontology.yaml` | the domain | per-type `attributes`, `relations`, `when` |
| `policy.yaml` | this workspace's gates | `required`, `orphan` |
| `storage.yaml` | where bytes land | `layout`, `path`, `format`, `id_prefix`, `template` |

`khub init` copies a preset's three files into `.khub/`; you edit them directly, and they carry no runtime tie to the hub. The three reunite at load time into one resolved contract every command reads — the layers merge on their **top-level keys** (`ontology:` / `policy:` / `storage:`), never on filenames, so one file carrying all three blocks resolves identically (useful for fixtures; three files is the canonical layout). Ontology declares which types exist; policy and storage only annotate them, and an entry for a type ontology never declared is a resolve error. See [`concepts.md`](concepts.md) for why the schema is the contract.

The split is what makes the ontology a clean projection surface: `ontology.yaml` is purely the domain — no paths, no gates, no khub plumbing — which is what an RDF/SHACL export or any external modeling tool wants to see.

## The base block is embedded, not copied

Every entity inherits nine base attributes: `type` (text, required: the discriminator each kind pins its value on), `draft` (bool, default false), `author` (text: the writer, person or agent), `created` (date, required), `updated` (date), the three OKF fields `title`, `description`, and `resource` (text; `resource` is the canonical URI, khub's link-out), and `tags` (list) — plus four universal relations (`related`, `sources`, `references`, `depends_on`), each `to: any` and `many: true`, with `depends_on` acyclic by contract.

The base is khub's own plumbing, not domain modelling, so it ships **embedded in the binary** (`presets/core/ontology.yaml`) and is supplied to every resolve — it is never written into a workspace, which keeps `ontology.yaml` purely the domain. Read the effective base with `khub schema base`. An authored `ontology.base` is **rejected** with a located error: the base is not an authoring surface (redefining it whole would silently change what `check`, `stale`, and the graph walks read), and the override mechanism is per-type, below.

A type overrides a base attribute by redeclaring it in its own `attributes`: make `updated` required, make `title` required, relax `created` to optional. An override facet-merges (an unstated facet is inherited; an explicit `required: false` beats an inherited true); a redeclared relation replaces the base's whole.

## Authoring a type

A type is declared under `ontology.entities`, keyed by its name, carrying only domain semantics:

`attributes:` is a map keyed by name → `{ type?, required?, default?, enum?, pattern? }`. `type` is one of `text | number | date | datetime | bool | list` (default `text`). `enum` is a list of allowed values. `pattern` is a regex the value must match.

`relations:` is a map keyed by predicate → `{ to, many?, required?, inverse?, acyclic? }`. The field name *is* the predicate. `to` names a single type, a list of type names (a union edge), or `any` (polymorphic). A relation is single-valued by default; `many: true` makes it multi-valued. `required: true` makes it a completeness requirement, enforced by `khub check` over the active graph. `acyclic` lives here, on the predicate, because it is a property of the domain — a hierarchy edge is acyclic by meaning, not by workspace preference.

A real snippet from the firm-ops preset shows the shape:

```yaml
# ontology.yaml
ontology:
  entities:
    client:
      attributes:
        name:     { type: text, required: true }
        industry: { type: text }
        updated:  { required: true }          # override base: make updated required
      relations:
        partner:  { to: partnership }
    project:
      attributes:
        active: { type: bool, default: true }
        external_repo: { type: text, pattern: '^endgame-build/[a-z0-9-]+$' }
      relations:
        client: { to: client,  required: true }
        owner:  { to: person,  required: true }
        team:   { to: person,  many: true }

# storage.yaml
storage:
  client:  { layout: file,   path: clients }
  project: { layout: folder, path: projects }   # projects/{slug}/_index.md
```

## Storage config (`storage.yaml`)

Per type, keyed by the type's name. khub reads these directly.

- `layout` — `file` (one entity per file, `{path}/{slug}.md`), `folder` (one entity per folder, `{path}/{slug}/_index.md`), `collection` (every entity of the type is a row in ONE file), or `singleton` (exactly one fixed file, 0..1 entity; the slug IS the type name, so `khub get prd` resolves it).
- `format` — `md` (the default: YAML frontmatter + prose body), `json`, or `yaml`. Collections take `json | jsonl | yaml`. A non-md entity is a single mapping; prose rides in a reserved `body` field.
- `path` — the inventory directory, relative to the workspace root (or the file, for a collection/singleton).
- `id_prefix` and `template` — below.

A type with no storage entry at all takes the default: one file per entity under a directory named for the type (`layout: file`, `path: <type>`), so an ontology-only schema resolves and runs. An entry that **does** exist must state its `layout` — a path with no layout is a half-statement, and silently defaulting it to `file` would turn a forgotten `layout: collection` into a directory of strays. A collection with no `path` stores at `{type}.{format}`; a singleton always needs its `path` — the exact file it lives at.

## Workspace gates (`policy.yaml`)

Per type, keyed by the type's name — what `check` demands of it in *this* workspace.

- `required` — singleton-only: `check` reports a missing required singleton (e.g. a workspace without its `prd.md`).
- `orphan` — `orphan: true` declares that edge-less is this type's *expected* state, so `check` stops reporting its entities as orphans, `--strict` stops failing on them, `query --orphan` stops flagging them and the `status` count stops including them. Default `false`. Use it for a narrative root nothing points at by design: build-hub declares it on its two narrative singletons (`prd`, `arc42`), because every stored edge in that preset points *up* the durability ladder and the documents sit above its top — so orphan-ness there is a finding no authoring could ever close, and without the flag `check --strict` could not go green on a correct workspace. Unlike `required` this is **not** singleton-only: any type whose members are legitimately unwired may declare it, and a singleton that *does* carry relations is still swept. It removes no signal — a missing required edge is still reported by required-completeness, which names the field.

## Capture cues (`when`)

A type may declare `when` in its ontology entry — one line of domain language naming the moment to capture it ("a choice is made, rejected, or revisited between real alternatives"). `khub wire` renders every one of them into the managed block in `CLAUDE.md` / `AGENTS.md`, so an agent knows *when* to record without running the CLI; `khub schema show <type>` prints it too.

A cue is **pure domain language — never a file path**. For a singleton (edited, never added to), `wire` derives the edit target from the type's storage path at render time and appends it to the rendered cue (`— edit [knowledge/prd.md](knowledge/prd.md), never add a second`), so a moved file can never strand a stale link in the ontology. A test holds every shipped cue to embedding no markdown link.

## Ids (`id_prefix`, `id_date`, in storage)

A minted id is `<prefix>-<YYYY-MM-DD>-<slug>`, with the prefix and the date each declared per type and each optional:

```
<prefix>-<slug>                when the type declares id_prefix
<prefix>-<YYYY-MM-DD>-<slug>   … and id_date: true
<slug>                         when it declares neither
```

The slug is the slugified `name`, else `title`. Minting is a **pure function of the schema, the type, the frontmatter and that title** — it reads no siblings. The ordinal it replaced (`ad-004-…`) was `max(existing) + 1` over a directory scan, a read-modify-write that two branches, worktrees or agents each won: the filenames differed, so git merged both cleanly, `add` never saw a collision and `check` reported nothing. Now the same title mints the same id, which `add` refuses (exit 2, naming `--id`), and which git surfaces as an add/add conflict on one filename rather than two files that quietly coexist.

`id_date` exists because only a decision legitimately recurs under one title — "use Postgres" is decided, superseded and revisited, which is what `supersedes` is for. A registry of what currently holds (requirements, components, repos) is the opposite: a repeated title *is* a duplicate. The date is the day the id was minted (an explicit `--created` on the `add` dates it, so an imported decision keeps its real day) and it stays that; nothing checks it against `created`, which `edit` can rewrite — such a check would fire on every legitimate edit and on every `--id`.

`id_prefix` takes either a literal token, or a mapping that picks one by the value of another attribute; `id_date` is a flat sibling, so the two compose:

```yaml
# storage.yaml
storage:
  adr:
    id_prefix: ad                  # ad-2026-01-15-use-postgres
    id_date: true
  requirement:
    id_prefix: req                 # req-pay-by-card
  component:
    id_prefix: { by: kind, map: { service: svc, library: lib, external: ext } }
# ontology.yaml declares the deciding attribute:
#   component: { attributes: { kind: { enum: [service, library, external], required: true } } }
```

The by-value form puts the kind in the filename, which is where a mislabelled entity becomes visible. Its `by` must name an attribute of that type declaring an `enum`, and its `map` must cover exactly that enum's members — otherwise a legal value would mint no prefix, and the schema is rejected at resolve time rather than failing later on one unlucky entity. The cost is that the deciding attribute is no longer freely editable (khub has no rename), and that `add` **refuses to mint** while it is unset (`needs --kind <…>`; pass it, or name the entity with `--id`). That is why the shipped presets collapsed `requirement` to a literal `req`: `kind` became an ordinary field. Either key on a singleton is a schema error — its id is its type name, so it mints nothing.

An explicit `--id` is slugified and written as given. It is *not* exempt from the gate below, so on a type that declares a scheme an `--id` outside it is written and then reported. That is khub's standing split: writes capture, gates report. A type with neither `name` nor `title` and no `--id` refuses: the type-name fallback went with the ordinal, because without one it mints a single id per type.

`validate` holds every non-singleton entity to its type's scheme, one finding per slug on field `id`, three arms in order:

1. **prefix** — the slug must start with a prefix the type mints (declared prefixes tried longest first), and for the by-value form with the one its deciding attribute chose: a `svc-` file whose `kind` says `external` is `slug says 'svc-' but the schema mints 'ext-' for kind 'external'`. That disagreement is invisible to every other gate — the enum is legal, the relations resolve, nothing dangles. An unset deciding attribute reports nothing here; `check` names it as incomplete.
2. **date** — when `id_date`, the rest must open with `YYYY-MM-DD-`. Presence only, never absence: a title like "2026 07 28 audit" legitimately mints a date-shaped opening on an undated type.
3. **retired ordinal** — a surviving `NNN-` from the old scheme is rejected with a message naming `git mv`, unless the title itself slugifies to start with those digits ("404 handling" → `cmp-404-handling` is fine; "Public API" does not produce the `001-` in `cmp-001-public-api`).

The shape every message quotes (`ad-YYYY-MM-DD-slug`, `req-slug`, `svc|lib|ext-slug`, `slug`) is the same one `khub schema show <type>` prints as `id_shape`, so a reader fixing a slug against one is never told something different by the other.

build-hub declares the prefixes its docs use in prose (`cap-`, `act-`, `uc-`, `req-`, `ad-`, `sys-`, `cmp-`, `api-`, `rp-`) and dates only `adr`; firm-ops declares neither, so its ids are bare slugs.

### Migrating an ordinal corpus

`khub validate` reports every surviving `NNN-` id as an `id` error, so a corpus that has not been migrated fails the gate rather than drifting. There is no `khub migrate-ids`: a renamer cannot fix prose references, and it cannot fix external spec files whose frontmatter points in — both of which the docs actively encourage. Three loops cover the two presets; run them from the workspace root.

**firm-ops** strips the ordinal from every id. Folder-layout types (opportunity, project, partnership) are directories, so the directory moves:

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

**Decisions** (build-hub `knowledge/decisions/ad-`) substitute the ordinal with the file's own `created` date. On a dated type an unmigrated `ad-001-x` reports `slug carries no date — this type mints ad-YYYY-MM-DD-slug`, not the `git mv` message, because the date arm runs before the ordinal arm; this loop fixes both in one move:

```bash
for f in knowledge/decisions/ad-[0-9][0-9][0-9]-*.md; do
  [ -e "$f" ] || continue
  d=$(sed -n 's/^created: //p' "$f" | head -1)
  git mv "$f" "$(dirname "$f")/$(basename "$f" | sed -E "s/^ad-[0-9]+-/ad-$d-/")"
done
```

**Every other prefixed type** strips the ordinal, and requirements also rewrite their three prefixes to one — the only move that changes the leading token:

```bash
for f in knowledge/components/cmp-*.md knowledge/capabilities/cap-*.md; do
  [ -e "$f" ] || continue
  git mv "$f" "$(echo "$f" | sed -E 's#/([a-z]+)-[0-9]+-#/\1-#')"
done
for f in knowledge/requirements/{fr,cst,br}-[0-9][0-9][0-9]-*.md; do
  [ -e "$f" ] || continue
  git mv "$f" "$(echo "$f" | sed -E 's#/(fr|cst|br)-[0-9]+-#/req-#')"
done
```

None of these fix references. After renaming, `khub check` names every edge that no longer resolves — fix those with `khub link`/`khub unlink` (or `khub edit`), run `khub reindex`, then grep your prose and any external spec frontmatter for the old ids yourself. Two ids that differed only by ordinal (`fr-001-x` and `cst-001-x`) collapse onto the same `req-x`; the second `git mv` refuses rather than clobber, and you decide which title to change.

## Body templates (`template`, in storage)

A template is small YAML in `.khub/templates/`. One file is both the scaffold `add` seeds from and the contract `validate` and `check` hold the body to, so the two cannot drift apart:

```yaml
title: PRD                      # optional; the init-created singleton's title
hint: one statement             # optional; leads the body as a comment — for a type with no headings
sections:
  - heading: Vision             # required unless `optional: true`
    hint: one paragraph         # rendered as an HTML comment placeholder
    word_count: {min: 25, max: 200}
  - heading: Alternatives
    optional: true              # may be absent; fails nothing
    hint: what else was on the table
  - heading: Decision
    text: |                     # optional literal pre-filled markdown
      Nothing here yet.
    required_text: ["we chose"]                 # a literal, matched case-insensitively
    forbidden_text: [{pattern: '\btbd\b'}]      # or a regex
  - heading: Building Block View
    code_blocks: [{lang: mermaid, min: 1}]      # omit `lang` to count every fenced block
lenses:
  - code: alternatives
    name: A real alternative, not a strawman
    instruction: |
      Name what else was genuinely considered and what it would have cost.
```

`khub init` flattens the preset's `templates/` dir into `.khub/templates/` (an editable workspace copy, like the schema files themselves; `khub upgrade` refreshes it, backing an edited copy up to `<name>.bak`), creates every missing md singleton from its template, and `khub add` seeds new bodies from it — every section, optional ones included, each heading followed by its hint as a comment, the top-level `hint` leading (`--no-template` opts out, and is refused on a type whose template has a required heading). A type with no template, or one declaring `sections: []`, has no heading contract and any body passes; the `hint` and the lenses are what such a file carries instead.

Which template a type reads is settled in storage:

```yaml
storage:
  rfc:  { layout: file, path: rfcs, template: decision }   # explicit name
  adr:  { layout: file, path: adrs, template: decision }   # two types, one template
  memo: { layout: file, path: memos, template: false }     # explicitly untemplated
```

- `template: <name>` names the file stem (`.khub/templates/<name>.yaml`) — a **name, never a path**, so templates stay in one directory and two types can share one. A declared name pointing at nothing is a `check` finding (`missing_templates`), naming the type and the file — capture is never blocked, so a broken template link never takes `add` down with it.
- The key belongs only where a body does: declaring `template` (or `template: false`) on a collection or non-md type is a schema error — nothing ever reads a body template there, so the config would be dead weight `check` could never act on.
- Undeclared keeps the standing convention: a file named for the type (`adr` → `adr.yaml`) templatizes it, and its absence just means "not templated".
- `template: false` opts out even if a conventionally named file exists — the opted-out type still claims its conventional stem, so keeping the file around is not a finding.
- A template file **no** type claims — usually a renamed one, which would otherwise silently disable both scaffolding and the body contract — is a `check` finding (`stray_templates`) and fails the gate.

### Shape is an error, content is a gap

**The heading contract is presence and order of the non-optional declared headings, and nothing else.** A gate that fires on ordinary writing gets routed around instead of fixed, so all of these pass: extra `##` sections anywhere; anything deeper than `##`; numbering (`## 1. Context` and `## 2.1) Context` both satisfy `Context` — the separator is what makes it numbering, so `## 2026 goals` keeps its digits and can be declared verbatim); a heading that says more than asked (a declared `Context` is satisfied by `## Context and scope`); any prose, code or fenced blocks between headings. A `##` inside a code fence or an HTML comment is content, not structure — it satisfies nothing and displaces nothing. Two things fail, both `body_shape`: a required heading that is **absent**, and declared headings **out of declared order**. Shape means the document is not what it claims to be, so it is an error in `validate` and fails `check`.

A section may also say what belongs under it: `word_count: {min, max}` (prose only), `required_text` / `forbidden_text` (literals matched case-insensitively, or `{pattern: <regex>}` — RE2, so no backreferences or lookaround) and `code_blocks: [{lang, min, max}]` (neither bound is implied by the other; a rule with neither is refused rather than accepted and left inert). Every violation is one `body_rule` **gap** — in `validate`'s `gaps`, in `check`'s `thin` — and none of them fails a gate, not even `check --strict`: content means it is that document, unfinished. Rules read what the author wrote: fenced code and HTML comments are excluded, so a scaffold hint never satisfies a `required_text` and a forbidden phrase inside a code sample is not a hit; `word_count` counts each CJK character as a word. **An empty section is skipped** (unless it holds a fenced block) — unwritten is not badly written, and every section of a freshly scaffolded document is empty, so a minimum that fired there would mean `add` handed back a document already in violation. The key names are [mdschema](https://github.com/jackchuka/mdschema)'s where they overlap. Reserved keys (`repeat`, `pattern`, `images`, `lists`, `tables`, `min_tokens`, `max_tokens`, `budget`) are refused with a clear error rather than ignored, and a template that does not parse is a `template_invalid` finding against the type, never a crash: it silences only that type's `body_shape` and `body_rule`, and `add` seeds an empty body, so capture continues while you fix it.

### Every section a type gains after it ships is `optional: true`

A required heading added to a shipped template turns every existing body of that type into a `body_shape` error the moment the workspace runs `khub upgrade` — a green corpus goes red with no edit of its own. So the standing rule for the shipped presets, and the sensible one for a workspace's own templates: the headings that carried the contract when the type shipped stay required; anything added later is `optional: true`, and anything a template starts asking of the prose is a rule (a gap), not a heading (an error). Optional sections are still scaffolded — declaring one optional says it may be absent from a finished body, not that an author should have to remember it exists — and a heading you have nothing to say under may be deleted. The one exception the shipped presets took: the arc42 and prd headings were renamed to the names a reader outside khub already knows (see the CHANGELOG map), and a rename is a break by construction.

### Lenses

A template may carry `lenses:` — review questions for whoever is reading the document. khub resolves which apply, orders them and prints them beneath the per-section word counts of `khub validate <type>/<slug>` (as `lenses` and `body` under `--format json`, computed only for a single named entity); it never answers one and never turns one into a finding. The judgement is the reader's, and the point of a lens is that the reader can disagree with it.

```yaml
lenses:
- code: measurable
  name: A number and its measurement
  when: {kind: non-functional}   # optional; frontmatter this lens applies to
  after: single                  # optional; another lens code, or a list, for ordering
  section: Decision              # optional; a heading this template declares
  instruction: |
    Look for the value, the unit, where it is measured, and what enforces it.
    An adjective is not a bound.
```

- `when` reads frontmatter with the schema's defaults filled in, so it can filter on a field nobody had to write; a value, or a list of them, is compared as the scalar would be written to disk, so `draft: false` in a lens meets `draft: false` in a body whether either arrived as a bool or as text. The field must be one the type declares (attribute or relation), and `section` must name a declared heading: a typo in either would apply to nobody, forever, with nothing to report it, so both are `template_invalid`.
- `after` orders: lenses come out in declaration order, adjusted so every `after` holds (a stable topological sort — two lenses with no ordering between them keep their written order). An unknown code, a repeated code or a cycle is `template_invalid`.
- `name` defaults to `code`; `instruction` is printed verbatim, indented, one lens per block.

The [CLI reference](cli.md) covers the on-disk contract for entity formats and collections in full, and its [finding table](cli.md#reading-check-output) lists every body finding beside the rest.

## Extend

Editing the `.khub/` layer files takes effect on the next command, with no build step, because every command resolves and introspects the schema at runtime. Adding a type (ontology), changing an enum (ontology), or moving a type's inventory (storage) is the entire override mechanism, and no surface code changes.

Inspect the effective schema with `khub schema` (full), `khub schema types`, `khub schema show <type>`, `khub schema edges`, and `khub schema base` (the embedded base block). For a complete worked schema, read the [firm-ops preset](firm-ops-preset.md).

## Open schema and `--strict`

khub enforces the schema-declared subset and leaves the rest alone. A declared field is checked against its type, enum, pattern, and cardinality; a relation to a non-existent target is rejected on write. Any key the schema does not declare is accepted, validated against nothing, and preserved verbatim on round-trip. `--strict` (on `validate`, `add`, and `edit`) closes the schema and rejects unknown keys, for when you want a closed contract.
