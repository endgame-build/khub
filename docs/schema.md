# Schema

**How a khub workspace is configured: author it once, extend it as the work demands.**

A schema is authored in khub's own YAML vocabulary. It has two parts: a `base` block every entity inherits, and `entities`, the domain types. `khub init` merges the hub's `core.yaml` (the base) with a preset's `<preset>.yaml` into one self-contained, editable `.khub/schema.yaml`. You edit that one file; it carries no runtime tie to the hub. The schema is the contract every command reads at runtime. See [`concepts.md`](concepts.md) for why.

## The base block

Every entity inherits the base from `core.yaml`, merged at resolve time; a type never copies it in. The base carries nine attributes: `type` (text, required: the discriminator each kind pins its value on), `draft` (bool, default false), `author` (text: the writer, person or agent), `created` (date, required), `updated` (date), the three OKF fields `title`, `description`, and `resource` (text; `resource` is the canonical URI, khub's link-out), and `tags` (list). It also carries four relations (`related`, `sources`, `references`, `depends_on`), each `to: any` and `many: true`.

A type declares only its domain delta, and overrides a base attribute by redeclaring it: make `updated` required, make `title` required, relax `created` to optional.

## Authoring a type

A type is an entry under `entities`, keyed by its name. It carries `attributes`, `relations`, and storage config.

`attributes:` is a map keyed by name → `{ type?, required?, default?, enum?, pattern? }`. `type` is one of `text | number | date | datetime | bool | list` (default `text`). `enum` is a list of allowed values. `pattern` is a regex the value must match.

`relations:` is a map keyed by predicate → `{ to, many?, required? }`. The field name *is* the predicate. `to` names a single type, a list of type names (a union edge), or `any` (polymorphic). A relation is single-valued by default; `many: true` makes it multi-valued. `required: true` makes it a completeness requirement, enforced by `khub check`, not at write time.

A real snippet from the firm-ops preset shows the shape:

```yaml
entities:
  client:
    layout: file
    path: clients
    attributes:
      name:     { type: text, required: true }
      industry: { type: text }
      updated:  { required: true }          # override base: make updated required
    relations:
      partner:  { to: partnership }
  project:
    layout: folder
    path: projects                          # projects/{slug}/_index.md
    attributes:
      active: { type: bool, default: true }
      external_repo: { type: text, pattern: '^endgame-build/[a-z0-9-]+$' }
    relations:
      client: { to: client,  required: true }
      owner:  { to: person,  required: true }
      team:   { to: person,  many: true }
```

## Storage config

Three keys set where and how a type's entities live on disk. khub reads them directly.

- `layout` — `file` (one entity per file, `{path}/{slug}.md`), `folder` (one entity per folder, `{path}/{slug}/_index.md`), or `collection` (every entity of the type is a row in ONE file).
- `format` — `md` (the default: YAML frontmatter + prose body), `json`, or `yaml`. Collections take `json | jsonl | yaml`. A non-md entity is a single mapping; prose rides in a reserved `body` field.
- `path` — the inventory directory, relative to the workspace root (or the file, for a collection).

The [CLI reference](cli.md) covers the on-disk contract for entity formats and collections in full.

## Extend

Editing `.khub/schema.yaml` takes effect on the next command, with no build step, because every command resolves and introspects the schema at runtime. Adding a type, changing an enum, or overriding a type's layout is the entire override mechanism, and no surface code changes.

Inspect the effective schema with `khub schema` (full), `khub schema types`, `khub schema show <type>`, and `khub schema edges`. For a complete worked schema, read the [firm-ops preset](firm-ops-preset.md).

## Open schema and `--strict`

khub enforces the schema-declared subset and leaves the rest alone. A declared field is checked against its type, enum, pattern, and cardinality; a relation to a non-existent target is rejected on write. Any key the schema does not declare is accepted, validated against nothing, and preserved verbatim on round-trip. `--strict` (on `validate`, `add`, and `edit`) closes the schema and rejects unknown keys, for when you want a closed contract.
