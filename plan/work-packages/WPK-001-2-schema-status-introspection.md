---
id: WPK-001-2
name: Schema and Status Introspection
feature-spec: plan/specs/FS-001-workspace-schema.md
test-spec: plan/tests/TS-001-workspace-schema.md
stories: [STORY-WS-002, STORY-WS-003]
test-scenarios: [TS-WS-002-01, TS-WS-002-02, TS-WS-002-03, TS-WS-002-04, TS-WS-002-U01, TS-WS-002-U02, TS-WS-002-U03, TS-WS-002-U04, TS-WS-002-U05, TS-WS-002-U06, TS-WS-002-U07, TS-WS-003-01, TS-WS-003-02, TS-WS-003-03, TS-WS-003-04, TS-WS-003-U01, TS-WS-003-U02, TS-WS-003-U03, TS-WS-003-U04, TS-WS-003-U05, TS-WS-003-U06]
depends-on: [WPK-001-1]
updated: 2026-06-23
---

# Schema and Status Introspection

## Objective

Delivers the two read-side surfaces that orient every other tool: `khub schema` (with `schema types`, `schema show <type>`, `schema edges`) and `khub status`. Both derive everything from the compiled `.khub/schema.yaml` and the graph projection at runtime — no per-type code path, no stored counts. Schema introspection returns each type's fields, enums, required flags, relations, and storage layout, plus the full relation vocabulary (16 stored predicates and the derived `superseded_by`); status returns per-type counts, the draft/active split, orphan and stale counts, and an OKF-conformance flag. Rich table on a TTY, JSON otherwise.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-WS-002 | AC-001 | Show the full effective schema: every type with fields, enums, required flags, relations; each type's storage layout/format/nesting; provenance (source preset and version); valid JSON under `--format json` | TS-WS-002-01 |
| STORY-WS-002 | AC-002 | View a single type: `opportunity` fields, `stage` enum values, required fields, relations (`client`, `owner`, `partner`); `client` and `owner` marked required; Rich table on TTY, JSON otherwise | TS-WS-002-02 |
| STORY-WS-002 | AC-003 | Unknown type returns a lookup error, displays `No type 'widget' in the firm-ops schema`, and lists the known types | TS-WS-002-03 |
| STORY-WS-002 | AC-004 | List the relation vocabulary: all 17 predicates (16 stored + derived `superseded_by`) with `from`/`to`/cardinality; distinguish typed, union, and universal edges and mark the derived one; mark required relations | TS-WS-002-04 |
| STORY-WS-003 | AC-001 | Summarize a healthy workspace: per-type counts, draft vs active counts, orphan and stale counts, OKF-conformance flag; Rich table on TTY | TS-WS-003-01 |
| STORY-WS-003 | AC-002 | Status on an empty workspace returns zero counts for every type and displays `Workspace initialized; no entities yet` | TS-WS-003-02 |
| STORY-WS-003 | AC-003 | No workspace found returns a resolution error and displays `No .khub workspace found. Run khub init <preset>` | TS-WS-003-03 |
| STORY-WS-003 | AC-004 | Machine-readable status emits per-type counts, draft/active totals, orphan/stale counts, and the OKF flag as JSON under `--format json` | TS-WS-003-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-WS-002 | REQ-WS002-01 | EARS-U | The system shall derive all schema output from `.khub/schema.yaml` at runtime | TS-WS-002-U05 |
| STORY-WS-002 | REQ-WS002-02 | EARS-E | When `schema show <type>` runs, the system shall return that type's fields, enums, required flags, relations, and layout | TS-WS-002-U01 |
| STORY-WS-002 | REQ-WS002-03 | EARS-W | If the named type is unknown, then the system shall return a lookup error and list known types | TS-WS-002-U04 |
| STORY-WS-002 | REQ-WS002-04 | EARS-O | Where `--format json` is set, the system shall emit machine-readable JSON for the agent | TS-WS-002-U07 |
| STORY-WS-003 | REQ-WS003-01 | EARS-E | When status runs, the system shall return per-type counts and draft/active totals | TS-WS-003-U01 |
| STORY-WS-003 | REQ-WS003-02 | EARS-E | When status runs, the system shall return orphan and stale counts | TS-WS-003-U03 |
| STORY-WS-003 | REQ-WS003-03 | EARS-W | If no workspace resolves, then the system shall return a resolution error | TS-WS-003-U05 |
| STORY-WS-003 | REQ-WS003-04 | EARS-O | Where `--format json` is set, the system shall emit machine-readable counts | TS-WS-003-U06 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-WS-002 | WS-004 | Schema introspection reads only the compiled schema; no per-type code path exists | Constraint | TS-WS-002-U05 |
| STORY-WS-002 | WS-005 | Required relations are reported from the schema, not inferred from data | Validation | TS-WS-002-U03 |
| STORY-WS-003 | WS-006 | Status counts are derived from the graph projection, never stored | Constraint | TS-WS-003-U03 |
| STORY-WS-003 | WS-007 | The OKF-conformance flag reports whether the workspace would project to a valid OKF bundle (every entity carries `type`, relations resolve, an `index.md` generates) | Validation | TS-WS-003-U04 |
| STORY-WS-003 | WS-008 | Orphan and stale are core projection properties, computed identically for `status`, `query`, and `check` | Constraint | TS-WS-003-U03 |
| STORY-WS-002 | WS-SHARED-001 | Every surface introspects `.khub/schema.yaml` at runtime; no per-type knowledge is hardcoded | Constraint | TS-WS-002-U05 |

---

## Entities

### Schema

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Types | Collection | Yes | The entity types and their attributes |
| Relations | Collection | Yes | The relation vocabulary (predicates, `from`, `to`, cardinality) |
| Base Block | Collection | Yes | Attributes and relations merged into every entity at resolve time |
| Provenance | Text | Yes | Preset name and version stamped in the header |

### Type

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Name | Text | Yes | Type identifier (e.g. `opportunity`; 12 in firm-ops) |
| Fields | Collection | Yes | Declared attributes with required flags |
| Enums | Collection | No | Named enumerations on fields (e.g. `stage`) |
| Relations | Collection | No | Outgoing relations, with required relations flagged |
| Storage Layout | Type | Yes | `file` or `folder` |
| Serialization Format | Type | Yes | `md` (default), `json`, `jsonl`, `gjson`, or `yaml` |

### Relation (Predicate)

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Predicate | Text | Yes | Relation name (e.g. `client`, `owner`, `partner`, `superseded_by`) |
| From | Reference | Yes | Source type(s) — typed, union, or universal (`any`) |
| To | Reference | Yes | Target type(s) |
| Cardinality | Text | Yes | Cardinality (one/many) |
| Required | Yes/No | No | Whether the relation is required |
| Derived | Yes/No | No | Whether the predicate is derived (e.g. `superseded_by`) vs stored |

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

Not applicable — `khub schema` and `khub status` are read-only surfaces. `status` reports the draft/active/superseded counts derived from the projection; it does not transition entity state. The entity lifecycle is owned by authoring features outside this work package.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). Each story maps to one `khub` subcommand family.

### STORY-WS-002: Introspect the Schema (`khub schema`)

- **Library verb:** schema introspection over the compiled LinkML model
- **Commands:** `khub schema` · `khub schema types` · `khub schema show <type>` · `khub schema edges`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| subcommand | enum(— , types, show, edges) | No | Bare `schema` shows the full effective schema; `types` lists type names; `show <type>` details one type; `edges` lists the relation vocabulary |
| type | string (positional) | Yes (for `show`) | Type name to detail (e.g. `opportunity`) |
| --format | enum(text, json) | No | Rich table on a TTY (default); `json` emits machine-readable output |

- **Reads:** `.khub/schema.yaml`, `.khub/generated/`
- **Output (success):** Rich table on a TTY, JSON otherwise. `schema show opportunity` returns fields, the `stage` enum, required fields, and relations with `client`/`owner` marked required. `schema edges` returns all 17 predicates (16 stored + derived `superseded_by`) with `from`/`to`/cardinality, distinguishing typed/union/universal and marking the derived edge and required relations.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | lookup_error | Unknown type — `No type 'widget' in the firm-ops schema`; lists the known types |

### STORY-WS-003: Report Status (`khub status`)

- **Library verb:** graph projection summary
- **Command:** `khub status`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| --format | enum(text, json) | No | Rich table on a TTY (default); `json` emits machine-readable counts |

- **Reads:** the entity tree, the in-memory index
- **Output (success):** per-type counts, draft/active totals, orphan/stale counts, and the OKF-conformance flag — Rich table on a TTY, JSON under `--format json`. An empty workspace displays `Workspace initialized; no entities yet`.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | resolution_error | No `.khub/` resolves above the working directory — `No .khub workspace found. Run khub init <preset>` |

---

## Test Data

### Schema (compiled, read back)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | flattened firm-ops schema: 12 types, 16 stored + 1 derived (`superseded_by`) predicates | Full schema, single type, edges (TS-WS-002-01, -02, -04) |
| Invalid | lookup for a type (`widget`) the schema never declares | Unknown-type lookup error (TS-WS-002-03) |
| Boundary | `opportunity` with a `stage` enum and required `client`/`owner` relations | Required-relation flagging (TS-WS-002-02) |

### Entity Tree (status input)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | mixed draft/active entities, at least one orphan and one stale | Healthy-workspace summary (TS-WS-003-01) |
| Empty | freshly initialized workspace, no entities | Initialized-but-empty status (TS-WS-003-02) |
| Missing | bare directory with no `.khub/` in any parent | No-workspace resolution error (TS-WS-003-03) |

---

## Implementation Notes

- Both surfaces are pure reads. Schema introspection derives everything from the compiled `.khub/schema.yaml` at runtime (WS-004 / WS-SHARED-001) — no per-type code path. Status counts come from the graph projection, never stored values (WS-006).
- `schema edges` returns 17 predicates (16 stored + the derived `superseded_by`), distinguishing typed, union, and universal (`any → any`) edges and marking the derived one. Required relations are flagged from the schema, not inferred from data (WS-005).
- Counts as source of truth: read type and predicate counts from the compiled schema, not literals (FS-000 still describes firm-ops as 9 types / 14 predicates). Assert the count plus the named set; treat a mismatch as a spec-reconciliation signal, not a silent test edit.
- Orphan and stale are core projection properties, computed identically for `status`, `query`, and `check` (WS-008). The OKF-conformance flag reports whether the workspace would project to a valid OKF bundle — every entity carries `type`, relations resolve, an `index.md` generates (WS-007) — the conditions `export --okf` requires.
- Output branches: Rich table on a TTY, JSON otherwise; `--format json` forces machine-readable output with field parity to the table. Assert structured content via the JSON branch; assert only table presence (not layout) on the TTY branch.
- Self-containment (WS-SHARED-002): both commands resolve from `.khub/` alone with the hub/preset fixture removed.

---

## Done Criteria

- [ ] All 8 acceptance criteria pass (WS-002 AC-001 through AC-004, WS-003 AC-001 through AC-004)
- [ ] All 8 EARS requirements tested (REQ-WS002-01 through -04, REQ-WS003-01 through -04)
- [ ] All 6 business rules enforced (WS-004, WS-005, WS-006, WS-007, WS-008, WS-SHARED-001)
- [ ] All 21 test scenarios pass (TS-WS-002-01 through -04 + U01–U07; TS-WS-003-01 through -04 + U01–U06)
- [ ] `schema edges` returns the full predicate set (count + named set) read from the compiled schema
- [ ] JSON branch (`--format json`) has field parity with the Rich-table branch for both `schema` and `status`
- [ ] No regressions in existing tests
