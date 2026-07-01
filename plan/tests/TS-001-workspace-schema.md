---
id: TS-001
name: Workspace & Schema Test Spec
spec: plan/specs/FS-001-workspace-schema.md
updated: 2026-06-23
---

# Workspace & Schema — Test Spec

## Summary

Tests the workspace layer one level above the schema: `khub init` flattens `core` and a named preset into one `.khub/schema.yaml`, invokes the FS-000 compiler, stamps provenance, and lays down the entity tree without touching existing entity files; `khub schema` and `khub status` then read that workspace back, deriving everything from the compiled contract at runtime. Coverage runs from preset resolution and provenance stamping through a clean firm-ops scaffold that introspects to 9 types and 14 predicates.

**Feature Spec:** FS-001: Workspace & Schema
**Stories Covered:** 3
**Test Scenarios:** 12 (plus 21 unit tests)

---

## Test Strategy

### Approach

**Philosophy:** Hybrid — the workspace layer orchestrates the FS-000 compiler and reads its output, so the discrete rules (preset resolution, provenance stamping, count derivation, type lookup) are unit-testable, but the guarantees that matter — init never overwrites an entity file, schema and status derive solely from the compiled contract, the firm-ops scaffold introspects to its declared shape — only hold when a real preset compiles into a real workspace on a real filesystem. Unit tests pin the rules; integration tests prove the command surface and JSON parity; E2E proves the init → introspect path and the HQ cutover.

**Test Pyramid:**

| Level | Share | Scope |
|-------|-------|-------|
| Unit | ~60% | Preset resolution, core+preset flatten, provenance stamping, config defaults, type/enum lookup, required-relation flagging, per-type counting, draft/active split, orphan/stale derivation, OKF flag |
| Integration | ~30% | Init command flow (compile + gitignore write), schema/status command surface, `--format json` shape and parity, error rejection (unknown preset/type, non-empty target, no workspace) |
| E2E | ~10% | `init firm-ops` then `schema types` returns the 9 types; `schema edges` returns 14 predicates; force-seed over a live corpus modifies 0 entity files; `status` after `init` shows initialized-but-empty |

### Coverage Targets

| Target | Goal |
|--------|------|
| Acceptance criteria | 100% of ACs from FS-001 (12 ACs) |
| EARS requirements | 100% of REQ-WS* requirements (13) |
| Business rules | 100% of rule enforcement (WS-001..009, WS-SHARED-001..002) |
| Edge cases | Non-destructive force-seed, empty workspace, no workspace resolved, unknown preset/type, the `derived` edge marker (firm-ops declares none), self-containment after init |

---

## Test Scenarios

---

### STORY-WS-001: Initialize a Workspace from a Preset

**Spec:** As an Operator, I want to scaffold a workspace from a named preset, So that the engagement carries a self-contained, editable schema and an entity tree with no runtime tie to the hub

#### TS-WS-001-01: Scaffold a Workspace Successfully

**Validates:** AC-001
**Level:** E2E

**Given** the operator names a known preset and an empty target path
**When** they run `khub init firm-ops ./hq`
**Then:**
- [ ] `core.yaml` and `firm-ops.yaml` are merged into one flattened `.khub/schema.yaml`
- [ ] `.khub/config.yaml` is written with preset provenance + version, source, and command defaults (`format`, `stale_days`)
- [ ] the workspace name is set from `--name` (default: the target directory name `hq`) in `config.yaml`
- [ ] the schema header is stamped `# khub-preset: firm-ops@<version>` with the preset's declared semver
- [ ] the resolved schema is compiled to LinkML, Pydantic, and JSON Schema under `.khub/generated/`
- [ ] `.khub/generated/` is added to the workspace `.gitignore`
- [ ] the entity tree is laid down per each type's storage layout
- [ ] the system displays `Initialized firm-ops workspace at ./hq`
- [ ] a follow-up `khub schema types` returns the 9 firm-ops types

**Test Data:** firm-ops preset fixture (`core.yaml` + `firm-ops.yaml` with a declared semver); empty target directory `./hq`

#### TS-WS-001-02: Unknown Preset

**Validates:** AC-002
**Level:** Integration

**Given** the operator names a preset that does not resolve
**When** they run `khub init bogus ./hq`
**Then:**
- [ ] a configuration error is returned
- [ ] the message reads `Unknown preset 'bogus'. Known presets: firm-ops`
- [ ] no file is written (no `.khub/`, no entity tree, no `.gitignore` change)

**Test Data:** target directory that stays empty; preset registry resolving only `firm-ops`

#### TS-WS-001-03: Non-Empty Target Without Force

**Validates:** AC-003
**Level:** Integration

**Given** the target path already holds files
**When** the operator runs `khub init firm-ops ./hq` without `--force`
**Then:**
- [ ] a guard error is returned
- [ ] the message reads `Target ./hq is not empty. Pass --force to scaffold anyway`
- [ ] the existing files are left untouched (byte-identical before and after)

**Test Data:** `./hq` pre-populated with one unrelated file; no `--force` flag

#### TS-WS-001-04: Seed Over a Live Corpus With Force

**Validates:** AC-004
**Level:** E2E

**Given** the target is a branch of an existing entity corpus (the HQ cutover)
**When** the operator runs `khub init firm-ops . --force`
**Then:**
- [ ] `.khub/` is written without overwriting any existing entity `.md` file
- [ ] the existing entities are treated as the seeded tree
- [ ] the system displays `Initialized firm-ops workspace; 0 entity files modified`
- [ ] every pre-existing entity `.md` is byte-identical after init

**Test Data:** a temp copy of an HQ corpus branch (entity `.md` files present, no `.khub/`); `--force` flag

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-WS-001-U01 | Preset resolver | Resolves a known preset from the hub repo or from `--preset-source` | REQ-WS001-01 |
| TS-WS-001-U02 | Workspace scaffolder | Flattens `core` and the preset into one `.khub/schema.yaml` | REQ-WS001-01, WS-001 |
| TS-WS-001-U03 | Provenance stamper | Stamps `# khub-preset: firm-ops@<version>` using the preset file's declared semver | WS-009 |
| TS-WS-001-U04 | Config writer | Writes preset provenance, source, name (from `--name` or target dir), and `defaults` (`format`, `stale_days`) | WS-009 |
| TS-WS-001-U05 | Gitignore writer | Adds `.khub/generated/` to the workspace `.gitignore` and marks it derived | WS-002 |
| TS-WS-001-U06 | Preset resolver | Rejects an unknown preset and writes nothing | REQ-WS001-03 |
| TS-WS-001-U07 | Target guard | Refuses a non-empty target when `--force` is absent | REQ-WS001-04 |
| TS-WS-001-U08 | Workspace scaffolder | Never overwrites an existing entity `.md`; writes only under `.khub/` | REQ-WS001-05, WS-003 |

---

### STORY-WS-002: Introspect the Active Schema

**Spec:** As an Agent or Operator, I want to read the effective schema — types, fields, enums, relations, and storage layout, So that every surface introspects the contract at runtime and never hardcodes per-type knowledge

> **Preset note:** firm-ops v1 declares no derived inverse (`decision` / `supersedes` / `superseded_by` dropped), so `schema edges` returns 14 predicates, all stored. The `derived` output marker is exercised against a generic self-referential fixture in TS-003; here every firm-ops edge is non-derived. See `docs/firm-ops-preset.md`.

#### TS-WS-002-01: Show the Full Effective Schema

**Validates:** AC-001
**Level:** Integration

**Given** a valid firm-ops workspace
**When** the agent runs `khub schema --format json`
**Then:**
- [ ] every type is returned with its fields, enums, required flags, and relations
- [ ] each type's storage layout, format, and nesting config is returned
- [ ] provenance (source preset and version) is returned
- [ ] the output is valid JSON

**Test Data:** scaffolded firm-ops workspace; `--format json`

#### TS-WS-002-02: View a Single Type

**Validates:** AC-002
**Level:** Integration

**Given** a valid workspace
**When** the agent runs `khub schema show opportunity`
**Then:**
- [ ] the `opportunity` fields, the `stage` enum values, required fields, and relations (`client`, `owner`, `partner`) are returned
- [ ] `client` and `owner` are marked as required relations
- [ ] a Rich table renders on a TTY; JSON renders otherwise

**Test Data:** firm-ops workspace whose `opportunity` declares a `stage` enum and `client`/`owner`/`partner` relations

#### TS-WS-002-03: Unknown Type

**Validates:** AC-003
**Level:** Integration

**Given** a valid workspace
**When** the agent runs `khub schema show widget`
**Then:**
- [ ] a lookup error is returned
- [ ] the message reads `No type 'widget' in the firm-ops schema`
- [ ] the known types are listed

**Test Data:** firm-ops workspace; the unknown type name `widget`

#### TS-WS-002-04: List the Relation Vocabulary

**Validates:** AC-004
**Level:** E2E

**Given** a valid workspace
**When** the agent runs `khub schema edges`
**Then:**
- [ ] all 14 predicates (all stored; firm-ops declares no derived inverse) are returned with `from`, `to`, and cardinality
- [ ] typed, union, and universal (`any → any`) edges are distinguished; every firm-ops edge is marked non-derived
- [ ] the required relations are marked

**Test Data:** scaffolded firm-ops workspace whose compiled schema carries 14 stored predicates and no derived inverse

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-WS-002-U01 | Schema introspector | Looks up a type from the compiled schema and returns its fields | REQ-WS002-02 |
| TS-WS-002-U02 | Schema introspector | Extracts enum values and required flags for a type's fields | REQ-WS002-02 |
| TS-WS-002-U03 | Schema introspector | Flags required relations from the schema, not from data | REQ-WS002-02, WS-005 |
| TS-WS-002-U04 | Schema introspector | Raises a lookup error listing known types for an unknown type | REQ-WS002-03 |
| TS-WS-002-U05 | Schema introspector | Derives all output from `.khub/schema.yaml` at runtime with no per-type code path | REQ-WS002-01, WS-004 |
| TS-WS-002-U06 | Edge introspector | Returns 14 predicates (all stored), distinguishing typed/union/universal and emitting the `derived` marker (false for every firm-ops edge) | AC-004 |
| TS-WS-002-U07 | Output formatter | Emits machine-readable JSON when `--format json` is set, parity with the Rich-table fields | REQ-WS002-04 |

---

### STORY-WS-003: Report Workspace Status

**Spec:** As an Operator or Agent, I want to see counts and health flags for the workspace at a glance, So that I can orient before authoring and confirm the cutover is sound

#### TS-WS-003-01: Summarize a Healthy Workspace

**Validates:** AC-001
**Level:** Integration

**Given** a firm-ops workspace with entities present
**When** the operator runs `khub status`
**Then:**
- [ ] per-type counts are returned
- [ ] draft versus active counts are returned
- [ ] orphan and stale counts are returned
- [ ] an OKF-conformance flag is returned
- [ ] a Rich table renders on a TTY

**Test Data:** firm-ops workspace seeded with a mix of draft and active entities, at least one orphan and one stale entity

#### TS-WS-003-02: Status on an Empty Workspace

**Validates:** AC-002
**Level:** E2E

**Given** a freshly initialized workspace with no entities
**When** the operator runs `khub status`
**Then:**
- [ ] zero counts are returned for every type
- [ ] the system displays `Workspace initialized; no entities yet`

**Test Data:** workspace produced by `khub init firm-ops` with no entity files added

#### TS-WS-003-03: No Workspace Found

**Validates:** AC-003
**Level:** Integration

**Given** the working directory has no `.khub/` above it
**When** the operator runs `khub status`
**Then:**
- [ ] a workspace-resolution error is returned
- [ ] the message reads `No .khub workspace found. Run khub init <preset>`

**Test Data:** a bare temp directory with no `.khub/` in any parent

#### TS-WS-003-04: Machine-Readable Status

**Validates:** AC-004
**Level:** Integration

**Given** a valid workspace
**When** an agent runs `khub status --format json`
**Then:**
- [ ] per-type counts, draft/active totals, orphan/stale counts, and the OKF flag are emitted as JSON

**Test Data:** seeded firm-ops workspace; `--format json`

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-WS-003-U01 | Status reporter | Counts entities per type from the graph projection | REQ-WS003-01 |
| TS-WS-003-U02 | Status reporter | Splits counts into draft versus active | REQ-WS003-01 |
| TS-WS-003-U03 | Projection | Derives orphan and stale counts from the graph projection, never from stored values | REQ-WS003-02, WS-006, WS-008 |
| TS-WS-003-U04 | OKF checker | Computes the OKF-conformance flag (every entity carries `type`, relations resolve, an `index.md` generates) | WS-007 |
| TS-WS-003-U05 | Workspace resolver | Raises a resolution error when no `.khub/` resolves above the working directory | REQ-WS003-03 |
| TS-WS-003-U06 | Output formatter | Emits per-type counts, draft/active, orphan/stale, and the OKF flag as JSON | REQ-WS003-04 |

---

## Coverage Matrix

### Acceptance Criteria → Test Scenarios

| Story | AC | Description | Test Scenarios |
|-------|----|-------------|----------------|
| STORY-WS-001 | AC-001 | Scaffold a workspace successfully | TS-WS-001-01 |
| STORY-WS-001 | AC-002 | Unknown preset | TS-WS-001-02 |
| STORY-WS-001 | AC-003 | Non-empty target without force | TS-WS-001-03 |
| STORY-WS-001 | AC-004 | Seed over a live corpus with force | TS-WS-001-04 |
| STORY-WS-002 | AC-001 | Show the full effective schema | TS-WS-002-01 |
| STORY-WS-002 | AC-002 | View a single type | TS-WS-002-02 |
| STORY-WS-002 | AC-003 | Unknown type | TS-WS-002-03 |
| STORY-WS-002 | AC-004 | List the relation vocabulary (14 predicates) | TS-WS-002-04 |
| STORY-WS-003 | AC-001 | Summarize a healthy workspace | TS-WS-003-01 |
| STORY-WS-003 | AC-002 | Status on an empty workspace | TS-WS-003-02 |
| STORY-WS-003 | AC-003 | No workspace found | TS-WS-003-03 |
| STORY-WS-003 | AC-004 | Machine-readable status | TS-WS-003-04 |

### EARS Requirements → Test Scenarios

| Requirement | Type | Description | Test Scenarios |
|-------------|------|-------------|----------------|
| REQ-WS001-01 | EARS-E | Flatten `core` and the preset into one `.khub/schema.yaml` | TS-WS-001-01, TS-WS-001-U01, TS-WS-001-U02 |
| REQ-WS001-02 | EARS-E | Generate LinkML, Pydantic, and JSON Schema into `.khub/generated/` | TS-WS-001-01 |
| REQ-WS001-03 | EARS-W | Reject an unknown preset and write nothing | TS-WS-001-02, TS-WS-001-U06 |
| REQ-WS001-04 | EARS-W | Refuse a non-empty target when `--force` is absent | TS-WS-001-03, TS-WS-001-U07 |
| REQ-WS001-05 | EARS-U | Never overwrite an existing entity `.md` during init | TS-WS-001-04, TS-WS-001-U08 |
| REQ-WS002-01 | EARS-U | Derive all schema output from `.khub/schema.yaml` at runtime | TS-WS-002-01, TS-WS-002-U05 |
| REQ-WS002-02 | EARS-E | `schema show <type>` returns fields, enums, required flags, relations, layout | TS-WS-002-02, TS-WS-002-U01, TS-WS-002-U02, TS-WS-002-U03 |
| REQ-WS002-03 | EARS-W | Unknown type returns a lookup error and lists known types | TS-WS-002-03, TS-WS-002-U04 |
| REQ-WS002-04 | EARS-O | Emit machine-readable JSON when `--format json` is set | TS-WS-002-01, TS-WS-002-U07 |
| REQ-WS003-01 | EARS-E | Return per-type counts and draft/active totals | TS-WS-003-01, TS-WS-003-U01, TS-WS-003-U02 |
| REQ-WS003-02 | EARS-E | Return orphan and stale counts | TS-WS-003-01, TS-WS-003-U03 |
| REQ-WS003-03 | EARS-W | Return a resolution error when no workspace resolves | TS-WS-003-03, TS-WS-003-U05 |
| REQ-WS003-04 | EARS-O | Emit machine-readable counts when `--format json` is set | TS-WS-003-04, TS-WS-003-U06 |

### Business Rules → Test Scenarios

| Rule | Description | Enforcement | Test Scenarios |
|------|-------------|-------------|----------------|
| WS-001 | Schema is flattened (core + preset) at init; layering and sync are post-v1 | Constraint | TS-WS-001-01, TS-WS-001-U02 |
| WS-002 | `.khub/generated/` is derived and gitignored, never authored by hand | Constraint | TS-WS-001-01, TS-WS-001-U05 |
| WS-003 | Init is non-destructive to entity files; only `.khub/` is written | Constraint | TS-WS-001-04, TS-WS-001-U08 |
| WS-004 | Schema introspection reads only the compiled schema; no per-type code path | Constraint | TS-WS-002-01, TS-WS-002-U05 |
| WS-005 | Required relations are reported from the schema, not inferred from data | Validation | TS-WS-002-02, TS-WS-002-U03 |
| WS-006 | Status counts are derived from the graph projection, never stored | Constraint | TS-WS-003-01, TS-WS-003-U03 |
| WS-007 | The OKF-conformance flag reports whether the workspace would project to a valid OKF bundle | Validation | TS-WS-003-01, TS-WS-003-U04 |
| WS-008 | Orphan and stale are core projection properties, computed identically for `status`, `query`, and `check` | Constraint | TS-WS-003-01, TS-WS-003-U03 |
| WS-009 | Preset `version` is the file's declared semver, stamped into provenance; `config.yaml` `defaults` holds `format`, `stale_days` | Constraint | TS-WS-001-01, TS-WS-001-U03, TS-WS-001-U04 |
| WS-SHARED-001 | Every surface introspects `.khub/schema.yaml` at runtime; no per-type knowledge hardcoded | Constraint | TS-WS-002-U05, TS-WS-003-U03 |
| WS-SHARED-002 | The workspace is self-contained after init: no runtime dependency on the hub | Constraint | TS-WS-001-01, TS-WS-002-01 |

---

## Test Data

### Entities

#### Workspace

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | empty target dir; `.khub/config.yaml`, `.khub/schema.yaml`, `.khub/generated/` written; name from `--name` or dir | Happy-path scaffold (TS-WS-001-01) |
| Invalid | target dir already holding a file, no `--force` | Non-empty-target guard (TS-WS-001-03) |
| Boundary | existing entity `.md` corpus, `--force` set | Non-destructive force-seed, 0 files modified (TS-WS-001-04) |

#### Preset

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `firm-ops` resolving in the hub repo / `--preset-source`, with a declared semver | Resolution + provenance stamping (TS-WS-001-01, TS-WS-001-U03) |
| Invalid | `bogus` — resolves nowhere | Unknown-preset rejection, write nothing (TS-WS-001-02) |

#### Schema (compiled, read back)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | flattened firm-ops schema: 9 types, 14 stored predicates (no derived inverse) | Full schema, single type, edges (TS-WS-002-01, -02, -04) |
| Invalid | lookup for a type (`widget`) the schema never declares | Unknown-type lookup error (TS-WS-002-03) |
| Boundary | `opportunity` with a `stage` enum and required `client`/`owner` relations | Required-relation flagging (TS-WS-002-02) |

#### Entity Tree (status input)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | mixed draft/active entities, at least one orphan and one stale | Healthy-workspace summary (TS-WS-003-01) |
| Empty | freshly initialized workspace, no entities | Initialized-but-empty status (TS-WS-003-02) |
| Missing | bare directory with no `.khub/` in any parent | No-workspace resolution error (TS-WS-003-03) |

### Management

- **Setup:** preset fixtures (`core.yaml` + `firm-ops.yaml` with a declared semver) under a tests fixtures dir; workspaces scaffolded into a per-test temp dir via the Typer CLI runner; the force-seed case copies an HQ corpus branch into the temp dir before init; status fixtures seed the entity tree with draft/active/orphan/stale entities.
- **Cleanup:** every workspace is written into a `tmp_path` and torn down after each test; no shared `.khub/` between tests.
- **Isolation:** each scenario scaffolds and reads its own workspace from its own fixture into its own temp dir; the HQ corpus branch is copied (not mounted from the live repo) so the non-destructive assertion runs against a disposable tree.

---

## Test Environment

### Stack

| Tool | Purpose |
|------|---------|
| pytest | Unit and integration tests |
| pytest `tmp_path` fixtures | Scaffold-into-temp-dir isolation and before/after byte-diff assertions |
| Typer / Click `CliRunner` | Drive `khub init`, `schema`, and `status` and capture exit code + output |
| FS-000 compiler (resolver + LinkML → Pydantic v2, JSON Schema) | Real generation backend invoked by `init` |
| Rich (console capture, `force_terminal` toggle) | Assert the TTY Rich-table branch and the non-TTY JSON branch |

### Mocks

| Service | Strategy | Rationale |
|---------|----------|-----------|
| FS-000 compiler | Real, not mocked | `init` only holds end to end if a real preset compiles; mocking it would test nothing (mirrors TS-000) |
| Filesystem `.khub/` + entity tree | Real temp dir (`tmp_path`) | The non-destructive guarantee and the gitignore write need real file output |
| TTY detection | Forced via `CliRunner` / Rich `force_terminal` | Exercises both the Rich-table and JSON branches deterministically |
| Hub repo / preset source | Local fixture preset dir, no network | Self-containment (WS-SHARED-002): schema/status run with the hub fixture removed after init |
| HQ corpus | Disposable copy of a branch | Non-destructive force-seed validated without touching the live repo |
| CRM / Recorder / Airtable | Not exercised | External ids ride as `source_id` aliases; outside the workspace-layer scope |

---

## Execution Plan

| Phase | Tests | Gate | Target |
|-------|-------|------|--------|
| 1. Unit | Preset resolution, flatten, provenance stamping, config defaults, type/enum lookup, counting, orphan/stale, OKF flag | Block PR | < 30s |
| 2. Integration | Init compile + gitignore write, schema/status command surface, `--format json` shape and parity, error rejection | Block PR | < 2min |
| 3. E2E | `init firm-ops` → `schema types` (9 types); `schema edges` (14 predicates); force-seed 0 files modified; `status` after `init` empty | Block merge | < 5min |

### CI Triggers

- **Pull request:** Phases 1-2 (fast feedback)
- **Main branch:** Phases 1-3 (full coverage, including the firm-ops init → introspect E2E)
- **Pre-release:** Phases 1-3 (no separate performance phase; init and introspection are not runtime hot paths)

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Type and predicate counts (9 types, 14 predicates, all stored) are asserted verbatim against the firm-ops preset (now aligned with FS-000) | TS-WS-001-01, TS-WS-002-04 drift if the preset evolves | Read the counts from the compiled schema as the source of truth and assert the count plus the named set; treat a mismatch as a spec-reconciliation signal, not a silent test edit |
| Located error messages (unknown preset, non-empty target, unknown type, no workspace) are asserted verbatim and brittle to wording | TS-WS-001-02/03, TS-WS-002-03, TS-WS-003-03 break on cosmetic edits | Assert on the variable fields (preset name, path, type name) plus a message substring, keeping the spec's exact text as the canonical example |
| Force-seed over a live corpus could overwrite an entity file under a regression | TS-WS-001-04 is the only guard on data loss during the cutover | Snapshot every entity `.md` hash before init and assert byte-identical after; assert the `0 entity files modified` count independently of the hashes |
| Rich-table rendering varies by terminal width and version, making TTY-branch assertions flaky | TS-WS-002-02, TS-WS-003-01 flake on the table branch | Assert structured fields via the JSON branch for content; assert only that the TTY branch renders a table (presence, not layout) using a fixed-width captured console |
| Self-containment after init is assumed, not exercised, if every test keeps the hub fixture on disk | WS-SHARED-002 passes vacuously | Remove the hub/preset fixture after init in the self-containment case and assert `schema` and `status` still resolve from `.khub/` alone |
