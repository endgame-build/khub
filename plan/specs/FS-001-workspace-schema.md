---
id: FS-001
name: Workspace & Schema
priority: Critical
dependencies: [FS-000]
updated: 2026-06-21
---

# Workspace & Schema

## Overview

Stands up a khub workspace and exposes the active schema to every other surface. `khub init` scaffolds a seeded fork from a preset, flattening `core` and the chosen preset into one editable `.khub/schema.yaml` and invoking the FS-000 compiler; `khub schema` and `khub status` read that workspace back. This is the workspace foundation, one layer above the schema: nothing authors, queries, or checks until a workspace exists and the schema can be introspected. The firm-ops preset (12 types, 17 relation predicates) is the v1 proving ground.

**Primary Actor:** Operator

**Depends on:** FS-000: Schema & Compiler

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-WS-001 | Initialize a Workspace from a Preset | Operator |
| STORY-WS-002 | Introspect the Active Schema | Agent, Operator |
| STORY-WS-003 | Report Workspace Status | Operator, Agent |

---

## Stories

---

### STORY-WS-001: Initialize a Workspace from a Preset

**As an** Operator
**I want to** scaffold a workspace from a named preset
**So that** the engagement carries a self-contained, editable schema and an entity tree with no runtime tie to the hub

#### Preconditions

- [ ] PRE-001: The named preset resolves in the hub repo or at `--preset-source`
- [ ] PRE-002: The target path is empty, or `--force` is passed
- [ ] PRE-003: The hub's `core.yaml` and `<preset>.yaml` are readable

#### Acceptance Criteria

##### AC-001: Scaffold a Workspace Successfully

**Given** the operator names a known preset and an empty target path
**When** they run `khub init firm-ops ./hq`
**Then** the system shall:
- [ ] Merge `core.yaml` and `firm-ops.yaml` into one flattened `.khub/schema.yaml`
- [ ] Write `.khub/config.yaml` with preset provenance + version, source, and command defaults (`format`, `stale_days`)
- [ ] Set the workspace name from `--name` (default: the target directory name) in `config.yaml`
- [ ] Stamp the schema header with provenance (`# khub-preset: firm-ops@<version>`, the preset's declared semver)
- [ ] Compile the resolved schema to LinkML, Pydantic, and JSON Schema under `.khub/generated/`
- [ ] Add `.khub/generated/` to the workspace `.gitignore`
- [ ] Lay down the entity tree per each type's storage layout
- [ ] Display: "Initialized firm-ops workspace at ./hq"

##### AC-002: Unknown Preset

**Given** the operator names a preset that does not resolve
**When** they run `khub init bogus ./hq`
**Then** the system shall:
- [ ] Return a configuration error
- [ ] Display: "Unknown preset 'bogus'. Known presets: firm-ops"
- [ ] Prevent any file from being written

##### AC-003: Non-Empty Target Without Force

**Given** the target path already holds files
**When** the operator runs `khub init firm-ops ./hq` without `--force`
**Then** the system shall:
- [ ] Return a guard error
- [ ] Display: "Target ./hq is not empty. Pass --force to scaffold anyway"
- [ ] Leave the existing files untouched

##### AC-004: Seed Over a Live Corpus With Force

**Given** the target is a branch of an existing entity corpus (the HQ cutover)
**When** the operator runs `khub init firm-ops . --force`
**Then** the system shall:
- [ ] Write `.khub/` without overwriting any existing entity `.md` file
- [ ] Treat existing entities as the seeded tree
- [ ] Display: "Initialized firm-ops workspace; 0 entity files modified"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-WS001-01 | EARS-E | When init runs against a known preset, the system shall flatten `core` and the preset into one `.khub/schema.yaml` |
| REQ-WS001-02 | EARS-E | When init completes, the system shall generate LinkML, Pydantic, and JSON Schema into `.khub/generated/` |
| REQ-WS001-03 | EARS-W | If the preset is unknown, then the system shall reject init and write nothing |
| REQ-WS001-04 | EARS-W | If the target is non-empty and `--force` is absent, then the system shall refuse to scaffold |
| REQ-WS001-05 | EARS-U | The system shall never overwrite an existing entity `.md` file during init |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| WS-001 | The schema is flattened (core + preset) at init; layering and sync are post-v1 | Constraint |
| WS-002 | `.khub/generated/` is derived and gitignored, never authored by hand | Constraint |
| WS-003 | Init is non-destructive to entity files; only `.khub/` is written | Constraint |
| WS-009 | The preset `version` is the preset file's declared semver, stamped into provenance; `config.yaml` `defaults` holds workspace command defaults (`format`, `stale_days`) | Constraint |

#### Technical Notes

- **Command:** `khub init <preset> [path=.]` — `--preset-source`, `--name`, `--force`
- **Library verb:** workspace scaffolder over the schema compiler
- **Writes:** `.khub/config.yaml`, `.khub/schema.yaml`, `.khub/generated/`
- **Invariant upheld:** Markdown is truth (init owns `.khub/`, not the entities)
- **Output:** confirmation line; `--format json` emits the resolved provenance

#### Test Hints

- **Unit:** preset resolution, `core`+preset merge, provenance stamping
- **Integration:** LinkML compile to Pydantic + JSON Schema; gitignore write
- **E2E:** `init firm-ops` then `schema types` returns the 12 firm-ops types

---

### STORY-WS-002: Introspect the Active Schema

**As an** Agent or Operator
**I want to** read the effective schema — types, fields, enums, relations, and storage layout
**So that** every surface introspects the contract at runtime and never hardcodes per-type knowledge

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves above the working directory
- [ ] PRE-002: `.khub/schema.yaml` is well-formed and compiles

#### Acceptance Criteria

##### AC-001: Show the Full Effective Schema

**Given** a valid firm-ops workspace
**When** the agent runs `khub schema --format json`
**Then** the system shall:
- [ ] Return every type with its fields, enums, required flags, and relations
- [ ] Return each type's storage layout, format, and nesting config
- [ ] Return provenance (source preset and version)
- [ ] Emit valid JSON when `--format json` is set

##### AC-002: View a Single Type

**Given** a valid workspace
**When** the agent runs `khub schema show opportunity`
**Then** the system shall:
- [ ] Return the `opportunity` fields, the `stage` enum values, required fields, and relations (`client`, `owner`, `partner`)
- [ ] Mark `client` and `owner` as required relations
- [ ] Render a Rich table on a TTY or JSON otherwise

##### AC-003: Unknown Type

**Given** a valid workspace
**When** the agent runs `khub schema show widget`
**Then** the system shall:
- [ ] Return a lookup error
- [ ] Display: "No type 'widget' in the firm-ops schema"
- [ ] List the known types

##### AC-004: List the Relation Vocabulary

**Given** a valid workspace
**When** the agent runs `khub schema edges`
**Then** the system shall:
- [ ] Return all 17 predicates (16 stored + the derived `superseded_by`) with their `from`, `to`, and cardinality
- [ ] Distinguish typed, union, and universal (`any → any`) edges, and mark which are derived
- [ ] Mark which predicates are required relations

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-WS002-01 | EARS-U | The system shall derive all schema output from `.khub/schema.yaml` at runtime |
| REQ-WS002-02 | EARS-E | When `schema show <type>` runs, the system shall return that type's fields, enums, required flags, relations, and layout |
| REQ-WS002-03 | EARS-W | If the named type is unknown, then the system shall return a lookup error and list known types |
| REQ-WS002-04 | EARS-O | Where `--format json` is set, the system shall emit machine-readable JSON for the agent |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| WS-004 | Schema introspection reads only the compiled schema; no per-type code path exists | Constraint |
| WS-005 | Required relations are reported from the schema, not inferred from data | Validation |

#### Technical Notes

- **Command:** `khub schema` · `schema types` · `schema show <type>` · `schema edges`
- **Library verb:** schema introspection over the compiled LinkML model
- **Reads:** `.khub/schema.yaml`, `.khub/generated/`
- **Invariant upheld:** the schema is the contract (Principle 3)
- **Output:** Rich table on TTY, JSON otherwise

#### Test Hints

- **Unit:** type lookup, enum extraction, required-relation flagging
- **Integration:** JSON shape parity between `schema` and `schema show`
- **E2E:** `schema edges` over firm-ops returns 17 predicates

---

### STORY-WS-003: Report Workspace Status

**As an** Operator or Agent
**I want to** see counts and health flags for the workspace at a glance
**So that** I can orient before authoring and confirm the cutover is sound

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The entity tree is readable

#### Acceptance Criteria

##### AC-001: Summarize a Healthy Workspace

**Given** a firm-ops workspace with entities present
**When** the operator runs `khub status`
**Then** the system shall:
- [ ] Return counts per type
- [ ] Return draft versus active counts
- [ ] Return orphan and stale counts
- [ ] Return an OKF-conformance flag
- [ ] Render a Rich table on a TTY

##### AC-002: Status on an Empty Workspace

**Given** a freshly initialized workspace with no entities
**When** the operator runs `khub status`
**Then** the system shall:
- [ ] Return zero counts for every type
- [ ] Display: "Workspace initialized; no entities yet"

##### AC-003: No Workspace Found

**Given** the working directory has no `.khub/` above it
**When** the operator runs `khub status`
**Then** the system shall:
- [ ] Return a workspace-resolution error
- [ ] Display: "No .khub workspace found. Run khub init <preset>"

##### AC-004: Machine-Readable Status

**Given** a valid workspace
**When** an agent runs `khub status --format json`
**Then** the system shall:
- [ ] Emit per-type counts, draft/active totals, orphan/stale counts, and the OKF flag as JSON

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-WS003-01 | EARS-E | When status runs, the system shall return per-type counts and draft/active totals |
| REQ-WS003-02 | EARS-E | When status runs, the system shall return orphan and stale counts |
| REQ-WS003-03 | EARS-W | If no workspace resolves, then the system shall return a resolution error |
| REQ-WS003-04 | EARS-O | Where `--format json` is set, the system shall emit machine-readable counts |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| WS-006 | Status counts are derived from the graph projection, never stored | Constraint |
| WS-007 | The OKF-conformance flag reports whether the workspace would project to a valid OKF bundle (every entity carries `type`, relations resolve, an `index.md` generates) — the conditions `export --okf` requires | Validation |
| WS-008 | Orphan and stale are core projection properties, computed identically for `status`, `query`, and `check` | Constraint |

#### Technical Notes

- **Command:** `khub status` — `--format`
- **Library verb:** graph projection summary
- **Reads:** the entity tree, the in-memory index
- **Invariant upheld:** the graph is a derived projection (Principle 2)
- **Output:** Rich table on TTY, JSON otherwise

#### Test Hints

- **Unit:** per-type counting, draft/active split
- **Integration:** orphan and stale derivation against a seeded tree
- **E2E:** `status` after `init` shows initialized-but-empty state

---

## Shared Context

### Entities

| Entity | Description |
|--------|-------------|
| Workspace | A seeded fork: `.khub/` config and flattened schema plus the entity tree |
| Preset | The canonical operational setup (firm-ops in v1); merged into the workspace at init |
| Schema | The effective contract: types, attributes, relations, and storage layout, compiled to LinkML |
| Type | One entity type declared by the schema (12 in firm-ops) |

#### Workspace

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Path | Text | Yes | Root holding `.khub/` and the entity tree |
| Preset Provenance | Text | Yes | Source preset and version (`firm-ops@<version>`) |
| Preset Source | Reference | No | Git URL or path the preset was pulled from |
| Schema | Reference | Yes | The flattened `.khub/schema.yaml` |
| Generated Artifacts | Collection | Yes | LinkML, Pydantic, and JSON Schema under `.khub/generated/` (gitignored) |

#### Schema

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

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| Init over an existing entity tree | Preserve all entity files | Markdown is truth; init owns only `.khub/` |
| Schema deletion of a type | Out of scope for v1 | Schema edits are manual file edits; no destructive type removal command |
| Regenerating `.khub/generated/` | Overwrite derived artifacts | Generated output is disposable and rebuilt from the schema |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| WS-SHARED-001 | Every surface introspects `.khub/schema.yaml` at runtime; no per-type knowledge is hardcoded | STORY-WS-002, STORY-WS-003 |
| WS-SHARED-002 | The workspace is self-contained after init: no runtime dependency on the hub | STORY-WS-001, STORY-WS-002 |

### Cross-Feature Dependencies

| Entity | Source Feature | Usage |
|--------|---------------|-------|
| Schema / Compiled Artifact | FS-000: Schema & Compiler | Init flattens `core` + preset and invokes the compiler; `schema` introspects the compiled contract |
| Firm-Ops Preset | FS-000: Schema & Compiler | The preset `init` seeds the workspace from |

### Cross-Story Dependencies

```
STORY-WS-001 (Initialize a Workspace)
    ├── STORY-WS-002 (Introspect the Active Schema)
    └── STORY-WS-003 (Report Workspace Status)
```

Init creates the workspace and the schema; introspection and status both read that workspace and never run before it exists.

## Scope Changes

### Deferred Stories

| Story | Moved To | Reason | Date |
|-------|----------|--------|------|
| Schema drift report (`khub schema --diff`) | Fast-follow | Hub↔engagement sync is post-v1; v1 owns the flattened schema outright | 2026-06-20 |
| Promote-back / `diff-preset` | Deferred | Layered subtree sync is named but unscheduled | 2026-06-20 |
