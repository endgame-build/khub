---
id: WPK-001-1
name: Workspace Initialization
feature-spec: plan/specs/FS-001-workspace-schema.md
test-spec: plan/tests/TS-001-workspace-schema.md
stories: [STORY-WS-001]
test-scenarios: [TS-WS-001-01, TS-WS-001-02, TS-WS-001-03, TS-WS-001-04, TS-WS-001-U01, TS-WS-001-U02, TS-WS-001-U03, TS-WS-001-U04, TS-WS-001-U05, TS-WS-001-U06, TS-WS-001-U07, TS-WS-001-U08]
depends-on: []
updated: 2026-06-23
---

# Workspace Initialization

## Objective

Delivers `khub init <preset> [path]`: the workspace scaffolder that flattens `core` and a named preset into one editable `.khub/schema.yaml`, invokes the FS-000 compiler to generate LinkML, Pydantic, and JSON Schema, stamps preset provenance, and lays down the entity tree per each type's storage layout. Init owns only `.khub/` — it never overwrites an existing entity `.md`, so the same command both green-fields a fresh workspace and seeds over a live corpus during the HQ cutover. After init the workspace is self-contained, with no runtime tie to the hub.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-WS-001 | AC-001 | Scaffold a workspace successfully: merge `core`+preset into one `.khub/schema.yaml`, write `config.yaml` (provenance, source, name, defaults), stamp the schema header, compile to `.khub/generated/`, gitignore `generated/`, lay down the entity tree, display `Initialized firm-ops workspace at ./hq` | TS-WS-001-01 |
| STORY-WS-001 | AC-002 | Unknown preset returns a configuration error, displays `Unknown preset 'bogus'. Known presets: firm-ops`, and writes nothing | TS-WS-001-02 |
| STORY-WS-001 | AC-003 | Non-empty target without `--force` returns a guard error, displays `Target ./hq is not empty. Pass --force to scaffold anyway`, and leaves existing files untouched | TS-WS-001-03 |
| STORY-WS-001 | AC-004 | Force-seed over a live corpus writes `.khub/` without overwriting any entity `.md`, treats existing entities as the seeded tree, and displays `Initialized firm-ops workspace; 0 entity files modified` | TS-WS-001-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-WS-001 | REQ-WS001-01 | EARS-E | When init runs against a known preset, the system shall flatten `core` and the preset into one `.khub/schema.yaml` | TS-WS-001-U02 |
| STORY-WS-001 | REQ-WS001-02 | EARS-E | When init completes, the system shall generate LinkML, Pydantic, and JSON Schema into `.khub/generated/` | TS-WS-001-01 |
| STORY-WS-001 | REQ-WS001-03 | EARS-W | If the preset is unknown, then the system shall reject init and write nothing | TS-WS-001-U06 |
| STORY-WS-001 | REQ-WS001-04 | EARS-W | If the target is non-empty and `--force` is absent, then the system shall refuse to scaffold | TS-WS-001-U07 |
| STORY-WS-001 | REQ-WS001-05 | EARS-U | The system shall never overwrite an existing entity `.md` file during init | TS-WS-001-U08 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-WS-001 | WS-001 | The schema is flattened (core + preset) at init; layering and sync are post-v1 | Constraint | TS-WS-001-U02 |
| STORY-WS-001 | WS-002 | `.khub/generated/` is derived and gitignored, never authored by hand | Constraint | TS-WS-001-U05 |
| STORY-WS-001 | WS-003 | Init is non-destructive to entity files; only `.khub/` is written | Constraint | TS-WS-001-U08 |
| STORY-WS-001 | WS-009 | The preset `version` is the file's declared semver, stamped into provenance; `config.yaml` `defaults` holds workspace command defaults (`format`, `stale_days`) | Constraint | TS-WS-001-U03 |
| STORY-WS-001 | WS-SHARED-002 | The workspace is self-contained after init: no runtime dependency on the hub | Constraint | TS-WS-001-01 |

---

## Entities

### Workspace

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Path | Text | Yes | Root holding `.khub/` and the entity tree |
| Preset Provenance | Text | Yes | Source preset and version (`firm-ops@<version>`) |
| Preset Source | Reference | No | Git URL or path the preset was pulled from |
| Schema | Reference | Yes | The flattened `.khub/schema.yaml` |
| Generated Artifacts | Collection | Yes | LinkML, Pydantic, and JSON Schema under `.khub/generated/` (gitignored) |

### Preset

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Name | Text | Yes | Preset identifier (e.g. `firm-ops`) |
| Version | Text | Yes | Declared semver from the preset file, stamped into provenance |
| Source | Reference | No | Git URL or path resolved from the hub repo or `--preset-source` |

### Schema

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Types | Collection | Yes | The entity types and their attributes |
| Relations | Collection | Yes | The relation vocabulary (predicates, `from`, `to`, cardinality) |
| Base Block | Collection | Yes | Attributes and relations merged into every entity at resolve time |
| Provenance | Text | Yes | Preset name and version stamped in the header |

### Storage Layout *(named enumeration)*

| Value | Description |
|-------|-------------|
| file | One entity per file (e.g. `clients/{slug}.md`) |
| folder | A folder per entity with `_index.md` as the entry (e.g. `projects/{slug}/_index.md`) |

### Serialization Format *(named enumeration)*

| Value | Description |
|-------|-------------|
| md | Markdown with YAML frontmatter — the default, OKF-conformant |
| json | Single JSON document |
| jsonl | One JSON object per line |
| gjson | Graph-JSON collection |
| yaml | YAML document or collection |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

Not applicable — `khub init` is a one-shot scaffold. STORY-WS-001 has no entity state machine; the workspace is either absent or initialized.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). Each story maps to one `khub` subcommand.

### STORY-WS-001: Initialize a Workspace (`khub init`)

- **Library verb:** workspace scaffolder over the schema compiler
- **Command:** `khub init <preset> [path=.]`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| preset | string (positional) | Yes | Named preset to seed from (e.g. `firm-ops`) |
| path | string (positional) | No | Target directory (default: `.`) |
| --preset-source | path/URL | No | Where to resolve the preset if not in the hub repo |
| --name | string | No | Workspace name (default: the target directory name) |
| --force | flag | No | Scaffold into a non-empty target |
| --format | enum(text, json) | No | `text` confirmation (default); `json` emits the resolved provenance |

- **Writes:**

| Path | Description |
|------|-------------|
| `.khub/config.yaml` | Preset provenance + version, source, workspace name, `defaults` (`format`, `stale_days`) |
| `.khub/schema.yaml` | Flattened `core` + preset; header stamped `# khub-preset: firm-ops@<version>` |
| `.khub/generated/` | LinkML, Pydantic, JSON Schema (gitignored) |
| `.gitignore` | Appends `.khub/generated/` |
| entity tree | One path per type's storage layout (`file` or `folder`) |

- **Output (success):** `Initialized firm-ops workspace at ./hq` on a clean target; `Initialized firm-ops workspace; 0 entity files modified` on a force-seed over a corpus. With `--format json`, emits the resolved provenance object instead.

- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | configuration_error | Unknown preset — `Unknown preset 'bogus'. Known presets: firm-ops`; nothing written |
| non-zero | guard_error | Non-empty target without `--force` — `Target ./hq is not empty. Pass --force to scaffold anyway`; existing files untouched |

---

## Test Data

### Workspace

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | empty target dir; `.khub/config.yaml`, `.khub/schema.yaml`, `.khub/generated/` written; name from `--name` or dir | Happy-path scaffold (TS-WS-001-01) |
| Invalid | target dir already holding a file, no `--force` | Non-empty-target guard (TS-WS-001-03) |
| Boundary | existing entity `.md` corpus, `--force` set | Non-destructive force-seed, 0 files modified (TS-WS-001-04) |

### Preset

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `firm-ops` resolving in the hub repo / `--preset-source`, with a declared semver | Resolution + provenance stamping (TS-WS-001-01, TS-WS-001-U03) |
| Invalid | `bogus` — resolves nowhere | Unknown-preset rejection, write nothing (TS-WS-001-02) |

---

## Implementation Notes

- Init flattens `core.yaml` + `<preset>.yaml` into one `.khub/schema.yaml`, then invokes the FS-000 compiler (resolver → LinkML → Pydantic v2 + JSON Schema). The compiler is real, never mocked — init only holds end to end if a real preset compiles.
- Provenance (WS-009): stamp the schema header `# khub-preset: <preset>@<version>` using the preset file's declared semver; `config.yaml` carries provenance + version, source, name (from `--name` or the target dir), and a `defaults` block (`format`, `stale_days`).
- Order of operations and failure modes: resolve preset (reject unknown, write nothing) → guard target (refuse non-empty without `--force`) → flatten → compile → write `.khub/` and append `.khub/generated/` to `.gitignore` → lay down the entity tree per each type's storage layout (`file` vs `folder`).
- Non-destructive (REQ-WS001-05 / WS-003): writes only under `.khub/`. Never overwrites an existing entity `.md`. Force-seed treats the existing corpus as the seeded tree and reports `0 entity files modified`.
- Self-containment (WS-SHARED-002): after init the workspace has no runtime tie to the hub; `schema` and `status` resolve from `.khub/` alone even with the hub/preset fixture removed.

---

## Done Criteria

- [ ] All 4 acceptance criteria pass (AC-001 through AC-004)
- [ ] All 5 EARS requirements tested (REQ-WS001-01 through REQ-WS001-05)
- [ ] All 5 business rules enforced (WS-001, WS-002, WS-003, WS-009, WS-SHARED-002)
- [ ] All 12 test scenarios pass (TS-WS-001-01 through -04 and TS-WS-001-U01 through -U08)
- [ ] Compiler invoked for real — LinkML, Pydantic, and JSON Schema generated under `.khub/generated/`
- [ ] Force-seed leaves every pre-existing entity `.md` byte-identical (hash before == hash after)
- [ ] No regressions in existing tests
