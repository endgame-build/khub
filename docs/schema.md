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
- `orphan` — `orphan: true` declares that edge-less is this type's *expected* state, so `check` stops reporting its entities as orphans, `--strict` stops failing on them, `query --orphan` stops flagging them and the `status` count stops including them. Default `false`. Use it for a narrative root nothing points at by design: build-hub declares it on all five narrative singletons (`prd`, `roadmap`, `glossary`, `arc42`, `erd`) and build-lite on its two, because every stored edge in those presets points *up* the durability ladder and the documents sit above its top — so orphan-ness there is a finding no authoring could ever close, and without the flag `check --strict` could not go green on a correct workspace. Unlike `required` this is **not** singleton-only: any type whose members are legitimately unwired may declare it, and a singleton that *does* carry relations is still swept. It removes no signal — a missing required edge is still reported by required-completeness, which names the field.

## Capture cues (`when`)

A type may declare `when` in its ontology entry — one line of domain language naming the moment to capture it ("a choice is made, rejected, or revisited between real alternatives"). `khub wire` renders every one of them into the managed block in `CLAUDE.md` / `AGENTS.md`, so an agent knows *when* to record without running the CLI; `khub schema show <type>` prints it too.

A cue is **pure domain language — never a file path**. For a singleton (edited, never added to), `wire` derives the edit target from the type's storage path at render time and appends it to the rendered cue (`— edit [knowledge/prd.md](knowledge/prd.md), never add a second`), so a moved file can never strand a stale link in the ontology. A test holds every shipped cue to embedding no markdown link.

## Enumerated ids (`id_prefix`, in storage)

Every minted slug carries an ordinal, so a corpus reads in authoring order and an entity can be named in prose by a short stable handle (`ad-004`) rather than a whole title. The shape is:

```
<prefix>-<number>-<slug>     when the type declares id_prefix
<number>-<slug>              when it does not
```

The number is one past the highest already in use, zero-padded to three, and it keeps counting past that (`001`, `045`, `1000`). An explicit `--id` is written exactly as given and the next minted id still counts from it — but it is *not* exempt from the gate below, so on a type that declares a prefix an `--id` outside the scheme is written and then reported. That is khub's standing split: writes capture, gates report.

`id_prefix` takes either a literal token, or a mapping that picks one by the value of another attribute:

```yaml
# storage.yaml
storage:
  adr:
    id_prefix: ad                  # ad-001-use-postgres
  requirement:
    id_prefix: { by: kind, map: { functional: fr, constraint: cst, business-rule: br } }
# ontology.yaml declares the deciding attribute:
#   requirement: { attributes: { kind: { enum: [functional, constraint, business-rule], required: true } } }
```

The by-value form puts the kind in the filename, which is where a mislabelled entity becomes visible. Its `by` must name an attribute of that type declaring an `enum`, and its `map` must cover exactly that enum's members — otherwise a legal value would mint no prefix, and the schema is rejected at resolve time rather than failing later on one unlucky entity. Each prefix keeps its own sequence, so `fr-001` and `cst-001` coexist and the number reads as "the first constraint". A missing `by` value (capture is never blocked) falls back to a bare `NNN-slug`.

`validate` holds an entity to its type's scheme: a slug that does not follow it, or whose prefix disagrees with the attribute that chose it (a `fr-` file whose `kind` says `constraint`), is an error on field `id`. That disagreement is invisible to every other gate — the enum is legal, the relations resolve, nothing dangles. A bare `NNN-slug` passes: that is what `add` mints while the deciding attribute is still unset, and since khub has no `rename`, rejecting it later would strand the entity behind a gate no verb can clear. Types declaring no prefix are not checked at all, so corpora predating the scheme keep their bare slugs.

build-lite and build-hub declare the prefixes their docs already used in prose (`ad-`, `fr-`, `cst-`, `cmp-`, `fs-`, `wp-`, …); firm-ops declares none, so its entities are numbered without one.

## Body templates (`template`, in storage)

A template is small YAML in `.khub/templates/`:

```yaml
title: Product requirements     # optional; the init-created singleton's title
sections:
  - heading: Vision
    hint: one paragraph         # rendered as an HTML comment placeholder
  - heading: Non-goals
    text: |                     # optional literal pre-filled markdown
      Nothing here yet.
```

`khub init` flattens the preset's `templates/` dir into `.khub/templates/` (an editable workspace copy, like the schema files themselves), creates every missing md singleton from its template, and `khub add` seeds new bodies from it (`--no-template` opts out). `validate` then holds every instance body to the template: its `sections[].heading` list must appear in the body's H2 sequence as an ordered subsequence — extras allowed, capture never blocked. `optional`, `repeat`, and `pattern` on entries are reserved for a later version and rejected today.

Which template a type reads is settled in storage:

```yaml
storage:
  pdr:  { layout: file, path: pdrs, template: decision }   # explicit name
  adr:  { layout: file, path: adrs, template: decision }   # two types, one template
  memo: { layout: file, path: memos, template: false }     # explicitly untemplated
```

- `template: <name>` names the file stem (`.khub/templates/<name>.yaml`) — a **name, never a path**, so templates stay in one directory and two types can share one. A declared name pointing at nothing is a `check` finding (`missing_templates`), naming the type and the file — capture is never blocked, so a broken template link never takes `add` down with it.
- The key belongs only where a body does: declaring `template` (or `template: false`) on a collection or non-md type is a schema error — nothing ever reads a body template there, so the config would be dead weight `check` could never act on.
- Undeclared keeps the standing convention: a file named for the type (`adr` → `adr.yaml`) templatizes it, and its absence just means "not templated".
- `template: false` opts out even if a conventionally named file exists — the opted-out type still claims its conventional stem, so keeping the file around is not a finding.
- A template file **no** type claims — usually a renamed one, which would otherwise silently disable both scaffolding and the body contract — is a `check` finding (`stray_templates`) and fails the gate.

The [CLI reference](cli.md) covers the on-disk contract for entity formats and collections in full.

## Extend

Editing the `.khub/` layer files takes effect on the next command, with no build step, because every command resolves and introspects the schema at runtime. Adding a type (ontology), changing an enum (ontology), or moving a type's inventory (storage) is the entire override mechanism, and no surface code changes.

Inspect the effective schema with `khub schema` (full), `khub schema types`, `khub schema show <type>`, `khub schema edges`, and `khub schema base` (the embedded base block). For a complete worked schema, read the [firm-ops preset](firm-ops-preset.md).

## Open schema and `--strict`

khub enforces the schema-declared subset and leaves the rest alone. A declared field is checked against its type, enum, pattern, and cardinality; a relation to a non-existent target is rejected on write. Any key the schema does not declare is accepted, validated against nothing, and preserved verbatim on round-trip. `--strict` (on `validate`, `add`, and `edit`) closes the schema and rejects unknown keys, for when you want a closed contract.
