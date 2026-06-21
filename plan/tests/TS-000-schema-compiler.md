---
id: TS-000
name: Schema & Compiler Test Spec
spec: plan/specs/FS-000-schema-compiler.md
updated: 2026-06-21
---

# Schema & Compiler — Test Spec

## Summary

Tests the schema foundation: operators declare types in khub vocabulary (`base`, `entities`, `attributes`, `relations`, storage config), the resolver merges the base block, and the compiler emits LinkML, Pydantic v2, and JSON Schema into `generated/`. Coverage runs from vocabulary parsing through a clean firm-ops compile that validates a live HQ snapshot.

**Feature Spec:** FS-000: Schema & Compiler
**Stories Covered:** 4
**Test Scenarios:** 16 (plus 23 unit tests)

---

## Test Strategy

### Approach

**Philosophy:** Hybrid — the compiler is a deterministic transform, so most logic is unit-testable (vocabulary parsing, base merge, predicate resolution), but the contract only holds end to end when a real schema compiles and the generated model rejects a bad write. Unit tests pin the rules; integration tests prove generation; E2E proves the firm-ops cutover.

**Test Pyramid:**

| Level | Share | Scope |
|-------|-------|-------|
| Unit | ~65% | Vocabulary parsing, base-merge precedence, predicate-from-field-name, target resolution, porting-note transforms |
| Integration | ~25% | Resolve → compile flow, generated Pydantic/JSON Schema, located-error rejection, determinism |
| E2E | ~10% | Declare a type and reject a bad write; compile firm-ops and validate an HQ snapshot |

### Coverage Targets

| Target | Goal |
|--------|------|
| Acceptance criteria | 100% of ACs from FS-000 (16 ACs) |
| EARS requirements | 100% of REQ-SCH* requirements (16) |
| Business rules | 100% of rule enforcement (SCH-001..012, SCH-SHARED-001..003) |
| Edge cases | Located errors, determinism, override precedence, atomic no-partial-write |

---

## Test Scenarios

---

### STORY-SCH-001: Declare a Type in khub Vocabulary

**Spec:** As an Operator, I want to declare an entity type with attributes, enums, relations, and storage config in khub vocabulary, So that I model a domain without writing raw LinkML and the engine compiles it into validation

#### TS-SCH-001-01: Declare a Well-Formed Type

**Validates:** AC-001
**Level:** E2E

**Given** a schema declaring `project: { layout: folder, attributes: { stage: {enum: [diagnose, prove, scale, complete], required: true} }, relations: { client: {to: client, required: true}, owner: {to: person, required: true} } }`
**When** the operator resolves and compiles the schema
**Then:**
- [ ] `attributes` are accepted as scalars and enums
- [ ] `relations` are accepted as typed edges with a `to:` target and cardinality
- [ ] the storage config (`layout: folder`) is accepted
- [ ] the type resolves to a validatable model
- [ ] each relation field name (`client`, `owner`) is treated as the predicate

**Test Data:** `project` type declaration with `client` and `person` target types present in the schema

#### TS-SCH-001-02: Declared Constraints Generate Validation

**Validates:** AC-002
**Level:** Integration

**Given** a type that declares an enum, a pattern, and a `many` cardinality
**When** the schema compiles and an entity is validated against the generated model
**Then:**
- [ ] an out-of-enum value is rejected
- [ ] a pattern mismatch is rejected
- [ ] a single-valued relation given many targets is rejected, and a `many` relation accepts a list

**Test Data:** entity with `stage: invalid-stage`, entity with malformed `airtable_id`, entity with two values on a `to: { card: 1 }` relation

#### TS-SCH-001-03: Malformed Declaration — Unknown Relation Target

**Validates:** AC-003
**Level:** Integration

**Given** a type that names a relation target type that does not exist
**When** the operator declares `relations: { owner: {to: persn} }` and the schema compiles
**Then:**
- [ ] the schema is rejected with a located error
- [ ] the message reads: `Type 'project' relation 'owner' targets unknown type 'persn'`
- [ ] no artifacts are written to `generated/`

**Test Data:** `project` declaration with a typo'd `to: persn` target

#### TS-SCH-001-04: A Type Declares Only Its Delta

**Validates:** AC-004
**Level:** Integration

**Given** a type that carries only domain-specific fields
**When** the operator declares `client: { layout: file, attributes: { name: {required: true}, industry: {} } }`
**Then:**
- [ ] `type`, `draft`, `created`, `updated`, `tags`, and the OKF fields are inherited from the base block
- [ ] the operator is not required to redeclare base attributes

**Test Data:** minimal `client` declaration; base block defining the standard fields

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-SCH-001-U01 | Schema resolver | Parses `attributes`, `relations`, and storage config from khub vocabulary | REQ-SCH001-01 |
| TS-SCH-001-U02 | Schema resolver | Treats a relation's field name as its predicate, its value as the target | REQ-SCH001-02, SCH-002 |
| TS-SCH-001-U03 | Schema resolver | Resolves a `to:` target that is a single type, a list of types, or `any` | SCH-003 |
| TS-SCH-001-U04 | Schema resolver | Rejects khub vocabulary that smuggles raw LinkML constructs | SCH-001 |
| TS-SCH-001-U05 | Schema resolver | Lets a type declare only its delta, inheriting the base block | REQ-SCH001-04 |
| TS-SCH-001-U06 | Schema resolver | Raises a located error when a relation targets an unknown type | REQ-SCH001-03 |

---

### STORY-SCH-002: Merge the Base Block and Override

**Spec:** As an Operator, I want to define the attributes and relations every entity carries once in `core.yaml`, So that every type inherits them at resolve time and any type can override a base attribute by redeclaring it

#### TS-SCH-002-01: Merge Base Into Every Entity

**Validates:** AC-001
**Level:** Integration

**Given** a `base` block defining `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, `tags`, and the universal edges
**When** the schema resolves
**Then:**
- [ ] the base attributes are merged into every entity type
- [ ] the universal relations (`related`, `sources`, `references`, `depends_on`, each `to: any`) are merged into every entity type
- [ ] every entity carries a boolean `draft` field defaulting to `false`
- [ ] the merged fields are not stored in each type's declaration (merge is at resolve time)

**Test Data:** `core.yaml` base block; two preset types that declare no base fields

#### TS-SCH-002-02: A Type Overrides a Base Attribute

**Validates:** AC-002
**Level:** Integration

**Given** a type that redeclares an attribute the base also defines
**When** the type sets `updated: {required: true}` while base leaves it optional
**Then:**
- [ ] the type's declaration is applied over the base for `updated`
- [ ] every other base attribute remains inherited unchanged

**Test Data:** one type overriding `updated`; a sibling type that does not

#### TS-SCH-002-03: Missing Import

**Validates:** AC-003
**Level:** Integration

**Given** a preset that omits `imports: [core]`
**When** the schema resolves
**Then:**
- [ ] the base block is reported as unavailable
- [ ] the message reads: `Preset does not import 'core'; base attributes are missing`
- [ ] the schema is rejected rather than producing types with no `draft` flag or `type`

**Test Data:** preset file with the `imports:` line removed

#### TS-SCH-002-04: Universal Edges Available Everywhere

**Validates:** AC-004
**Level:** Integration

**Given** the base defines `any → any` relations
**When** an operator links any two entities with `related`, `sources`, `references`, or `depends_on`
**Then:**
- [ ] the edge is accepted on any type without that type redeclaring the predicate

**Test Data:** two entities of types that never declare the universal predicates

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-SCH-002-U01 | Base merger | Merges base attributes and universal relations into every entity at resolve time | REQ-SCH002-01, SCH-004 |
| TS-SCH-002-U02 | Base merger | Applies a type-level redeclaration over the base for that attribute | REQ-SCH002-02, SCH-005 |
| TS-SCH-002-U03 | Base merger | Guarantees every entity carries `type` and the boolean `draft` flag | SCH-006 |
| TS-SCH-002-U04 | Base merger | Rejects a preset that does not import `core` | REQ-SCH002-03 |
| TS-SCH-002-U05 | Base merger | Exposes the universal `any → any` edges on every type without redeclaration | REQ-SCH002-04 |

---

### STORY-SCH-003: Compile the Schema to LinkML, Pydantic, and JSON Schema

**Spec:** As an Operator or Agent, I want to compile the resolved khub schema into LinkML and its generated validation artifacts, So that validation, typed return objects, and tool schemas all derive from one contract

#### TS-SCH-003-01: Compile to Generated Artifacts

**Validates:** AC-001
**Level:** E2E

**Given** a resolved schema
**When** the compiler runs
**Then:**
- [ ] a LinkML schema is emitted from the khub vocabulary
- [ ] Pydantic v2 models for validation are generated into `generated/`
- [ ] JSON Schema for tools and editors is generated into `generated/`
- [ ] `generated/` is marked derived and gitignored

**Test Data:** a resolved two-type schema; a clean `generated/` target directory

#### TS-SCH-003-02: Operators See Only khub Vocabulary

**Validates:** AC-002
**Level:** Integration

**Given** an operator who never writes LinkML
**When** they compile
**Then:**
- [ ] LinkML stays the generation backend, hidden from the authored files
- [ ] khub vocabulary goes in and generated artifacts come out (round-trip)

**Test Data:** authored `core.yaml` + preset containing no LinkML keywords

#### TS-SCH-003-03: Malformed Schema — Duplicate Type or Import Cycle

**Validates:** AC-003
**Level:** Integration

**Given** a schema with a duplicate type or an import cycle
**When** the compiler runs
**Then:**
- [ ] the compile fails with a located error
- [ ] the message reads `Duplicate type 'project'` or `Import cycle through 'core'`
- [ ] no artifacts are written to `generated/`

**Test Data:** schema declaring `project` twice; schema whose imports form a cycle through `core`

#### TS-SCH-003-04: Regeneration Is Deterministic

**Validates:** AC-004
**Level:** Integration

**Given** an unchanged schema
**When** the compiler runs twice
**Then:**
- [ ] the generated artifacts are byte-identical across both runs
- [ ] the second run overwrites the prior `generated/` output without drift

**Test Data:** a fixed schema; two sequential compile invocations against the same target

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-SCH-003-U01 | Compiler | Translates khub vocabulary into a LinkML schema | REQ-SCH003-02 |
| TS-SCH-003-U02 | Compiler | Generates Pydantic v2 and JSON Schema from the resolved contract | REQ-SCH003-01, SCH-008 |
| TS-SCH-003-U03 | Compiler | Fails atomically on a malformed schema, writing no partial artifacts | REQ-SCH003-03, SCH-009 |
| TS-SCH-003-U04 | Compiler | Produces identical output for identical input | REQ-SCH003-04 |
| TS-SCH-003-U05 | Compiler | Treats `generated/` as derived and gitignored, never hand-edited | SCH-007 |
| TS-SCH-003-U06 | Compiler | Emits Pydantic v2 models with `extra="allow"` for open-schema writes | SCH-008 |

---

### STORY-SCH-004: Author the Firm-Ops Preset

**Spec:** As an Operator, I want to express `hq.schema.yml` as the khub firm-ops preset — 12 types, 17 relation predicates, the six porting notes resolved, So that the cutover seeds from a preset that compiles clean and validates the live HQ corpus

#### TS-SCH-004-01: Declare All Twelve Types

**Validates:** AC-001
**Level:** Integration

**Given** the firm-ops capture in `docs/firm-ops-preset.md`
**When** the operator writes `firm-ops.yaml`
**Then:**
- [ ] opportunity, project, build, meeting, transcript, fragment, decision, case-study, isms-doc, partnership, person, and client are declared
- [ ] each type carries its attributes, enums (`stage`, `call_type`, `role`, `doc_kind`), and patterns
- [ ] each type's storage layout is set (file for client and meeting, folder for project)

**Test Data:** `firm-ops.yaml` with all 12 type declarations; `core.yaml` imported

#### TS-SCH-004-02: Declare All Edges (16 Stored, 17 Total)

**Validates:** AC-002
**Level:** Integration

**Given** the relation vocabulary
**When** the operator declares relations
**Then:**
- [ ] the 16 stored predicates are declared with `from`, `to`, and cardinality (12 in `firm-ops.yaml`; the 4 universal edges inherited from `core`)
- [ ] typed and union edges are declared (`owner → person`, `client → client`, `engagement → opportunity|project|build|partnership`), relying on `core` for the universal edges
- [ ] required relations are marked (`owner`, `client`, `engagement`)
- [ ] `superseded_by` is left derived (not stored), for 17 predicates total

**Test Data:** firm-ops relation declarations; the four universal predicates resolved from `core`

#### TS-SCH-004-03: Resolve the Six Porting Notes

**Validates:** AC-003
**Level:** Integration

**Given** the divergences between `hq.schema.yml` and khub invariants
**When** the operator ports the schema
**Then:**
- [ ] `superseded_by` is dropped and derived from `supersedes` (no stored inverse)
- [ ] a boolean `draft` flag is added over HQ's per-type `stage`/`status`
- [ ] `project` and `build` share the `projects/{slug}/_index.md` layout, discriminated by `type`
- [ ] the `partner` edge `from`-list is tightened to include `client`
- [ ] `meeting.engagement` stays an explicit stored edge and meetings flatten to `meetings/{slug}.md`
- [ ] the slug stays the id, with external ids riding as `source_id` aliases

**Test Data:** decision pair with `supersedes`; project + build sharing `projects/`; client carrying `partner`

#### TS-SCH-004-04: Compile Clean and Validate HQ

**Validates:** AC-004
**Level:** E2E

**Given** the authored preset
**When** the operator compiles and validates against an HQ snapshot read-only
**Then:**
- [ ] the preset compiles with no errors
- [ ] the snapshot validates, surfacing only the referential breaks the porting notes predict
- [ ] the preset stands as the input to the cutover seed (`khub init firm-ops`)

**Test Data:** read-only HQ snapshot fixture; compiled firm-ops generated model

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-SCH-004-U01 | Firm-ops preset | Declares all 12 types with their attributes, enums, and layouts | REQ-SCH004-01, SCH-010 |
| TS-SCH-004-U02 | Firm-ops preset | Declares the 16 stored predicates with `from`, `to`, and cardinality (17 total) | REQ-SCH004-02 |
| TS-SCH-004-U03 | Firm-ops preset | Derives `superseded_by` from `supersedes`, storing no inverse | SCH-011 |
| TS-SCH-004-U04 | Firm-ops preset | Declares `project` and `build` under one `projects/{slug}/` layout, discriminated by `type` | SCH-012 |
| TS-SCH-004-U05 | Firm-ops preset | Applies the six porting-note transforms | REQ-SCH004-03 |
| TS-SCH-004-U06 | Firm-ops preset | Compiles clean and validates an HQ snapshot | REQ-SCH004-04 |

---

## Coverage Matrix

### Acceptance Criteria → Test Scenarios

| Story | AC | Description | Test Scenarios |
|-------|----|-------------|----------------|
| STORY-SCH-001 | AC-001 | Declare a well-formed type | TS-SCH-001-01 |
| STORY-SCH-001 | AC-002 | Declared constraints generate validation | TS-SCH-001-02 |
| STORY-SCH-001 | AC-003 | Malformed declaration (unknown target) | TS-SCH-001-03 |
| STORY-SCH-001 | AC-004 | A type declares only its delta | TS-SCH-001-04 |
| STORY-SCH-002 | AC-001 | Merge base into every entity | TS-SCH-002-01 |
| STORY-SCH-002 | AC-002 | A type overrides a base attribute | TS-SCH-002-02 |
| STORY-SCH-002 | AC-003 | Missing import | TS-SCH-002-03 |
| STORY-SCH-002 | AC-004 | Universal edges available everywhere | TS-SCH-002-04 |
| STORY-SCH-003 | AC-001 | Compile to generated artifacts | TS-SCH-003-01 |
| STORY-SCH-003 | AC-002 | Operators see only khub vocabulary | TS-SCH-003-02 |
| STORY-SCH-003 | AC-003 | Malformed schema (duplicate/cycle) | TS-SCH-003-03 |
| STORY-SCH-003 | AC-004 | Regeneration is deterministic | TS-SCH-003-04 |
| STORY-SCH-004 | AC-001 | Declare all twelve types | TS-SCH-004-01 |
| STORY-SCH-004 | AC-002 | Declare all edges (16 stored, 17 total) | TS-SCH-004-02 |
| STORY-SCH-004 | AC-003 | Resolve the six porting notes | TS-SCH-004-03 |
| STORY-SCH-004 | AC-004 | Compile clean and validate HQ | TS-SCH-004-04 |

### EARS Requirements → Test Scenarios

| Requirement | Type | Description | Test Scenarios |
|-------------|------|-------------|----------------|
| REQ-SCH001-01 | EARS-E | Accept attributes, relations, storage config in khub vocabulary | TS-SCH-001-01, TS-SCH-001-U01 |
| REQ-SCH001-02 | EARS-U | Relation field name is the predicate, value is the target | TS-SCH-001-01, TS-SCH-001-U02 |
| REQ-SCH001-03 | EARS-W | Reject a relation targeting an unknown type with a located error | TS-SCH-001-03, TS-SCH-001-U06 |
| REQ-SCH001-04 | EARS-U | A type declares only its delta, inheriting the base | TS-SCH-001-04, TS-SCH-001-U05 |
| REQ-SCH002-01 | EARS-E | Merge the base block into every entity type | TS-SCH-002-01, TS-SCH-002-U01 |
| REQ-SCH002-02 | EARS-U | A type overrides a base attribute by redeclaring it | TS-SCH-002-02, TS-SCH-002-U02 |
| REQ-SCH002-03 | EARS-W | Reject a preset that does not import `core` | TS-SCH-002-03, TS-SCH-002-U04 |
| REQ-SCH002-04 | EARS-U | Universal `any → any` edges on every type without redeclaration | TS-SCH-002-04, TS-SCH-002-U05 |
| REQ-SCH003-01 | EARS-E | Generate Pydantic and JSON Schema into `generated/` | TS-SCH-003-01, TS-SCH-003-U02 |
| REQ-SCH003-02 | EARS-U | Keep LinkML a hidden backend; operators write only khub vocabulary | TS-SCH-003-02, TS-SCH-003-U01 |
| REQ-SCH003-03 | EARS-W | Fail the compile and write no artifacts when malformed | TS-SCH-003-03, TS-SCH-003-U03 |
| REQ-SCH003-04 | EARS-U | Regenerate `generated/` deterministically | TS-SCH-003-04, TS-SCH-003-U04 |
| REQ-SCH004-01 | EARS-U | Declare all 12 types with attributes, enums, layouts | TS-SCH-004-01, TS-SCH-004-U01 |
| REQ-SCH004-02 | EARS-U | Declare the 16 stored predicates (17 total with derived) | TS-SCH-004-02, TS-SCH-004-U02 |
| REQ-SCH004-03 | EARS-E | Resolve each of the six porting notes | TS-SCH-004-03, TS-SCH-004-U05 |
| REQ-SCH004-04 | EARS-E | Compile clean and validate an HQ snapshot | TS-SCH-004-04, TS-SCH-004-U06 |

### Business Rules → Test Scenarios

| Rule | Description | Enforcement | Test Scenarios |
|------|-------------|-------------|----------------|
| SCH-001 | Operators write khub vocabulary, never raw LinkML | Constraint | TS-SCH-001-U04 |
| SCH-002 | A relation's field name is the predicate; its value is the target | Constraint | TS-SCH-001-U02 |
| SCH-003 | A typed relation names a declared target type, a list, or `any` | Validation | TS-SCH-001-U03 |
| SCH-004 | Base block merged at resolve time, never copied into each type | Constraint | TS-SCH-002-01, TS-SCH-002-U01 |
| SCH-005 | A type-level declaration overrides the base for that attribute | Constraint | TS-SCH-002-02, TS-SCH-002-U02 |
| SCH-006 | Every entity carries `type` and the boolean `draft` flag | Validation | TS-SCH-002-U03 |
| SCH-007 | `generated/` is derived, gitignored, never hand-edited | Constraint | TS-SCH-003-01, TS-SCH-003-U05 |
| SCH-008 | Validation, typed objects, tool schemas derive from one compiled contract | Constraint | TS-SCH-003-U02, TS-SCH-003-U06 |
| SCH-009 | A malformed schema fails the compile atomically; no partial artifacts | Constraint | TS-SCH-003-03, TS-SCH-003-U03 |
| SCH-010 | The firm-ops preset is the LinkML port of `hq.schema.yml`, nothing omitted | Constraint | TS-SCH-004-01, TS-SCH-004-U01 |
| SCH-011 | No stored inverse edge; `superseded_by` derives from `supersedes` | Constraint | TS-SCH-004-03, TS-SCH-004-U03 |
| SCH-012 | `project` and `build` share `projects/{slug}/`, discriminated by `type` | Constraint | TS-SCH-004-03, TS-SCH-004-U04 |
| SCH-SHARED-001 | Operators write khub vocabulary; LinkML is a hidden backend | Constraint | TS-SCH-001-U04, TS-SCH-003-02 |
| SCH-SHARED-002 | The base block is the one source of standard fields and lifecycle | Constraint | TS-SCH-002-01, TS-SCH-004-U01 |
| SCH-SHARED-003 | `generated/` is derived and gitignored, regenerated from the schema | Constraint | TS-SCH-003-01, TS-SCH-004-U06 |

---

## Test Data

### Entities

#### Type Declaration

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `project: { layout: folder, attributes: { stage: {enum: [...], required: true} }, relations: { client: {to: client, required: true} } }` | Happy-path declare-and-compile (TS-SCH-001-01) |
| Invalid | `relations: { owner: {to: persn} }` — target type not declared | Located-error rejection (TS-SCH-001-03) |
| Boundary | type declaring only its delta, no base fields | Delta-only inheritance (TS-SCH-001-04) |

#### Base Block

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, `tags` + `related`/`sources`/`references`/`depends_on` (`to: any`) | Merge-into-every-entity (TS-SCH-002-01) |
| Override | type sets `updated: {required: true}` over optional base | Override precedence (TS-SCH-002-02) |
| Missing | preset without `imports: [core]` | Missing-import rejection (TS-SCH-002-03) |

#### Schema (compile input)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | resolved two-type schema, base merged | Clean compile + determinism (TS-SCH-003-01, TS-SCH-003-04) |
| Invalid | duplicate `project` type; imports cycling through `core` | Atomic-fail, no partial artifacts (TS-SCH-003-03) |
| Boundary | identical schema compiled twice | Byte-identical regeneration (TS-SCH-003-04) |

#### Firm-Ops Entity (e.g. decision, project, build, client)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | decision with `supersedes`; project + build under `projects/{slug}/` | Porting-note transforms (TS-SCH-004-03) |
| Invalid | `meeting` with a value outside the `call_type` enum; `partner` from an undeclared `from` type | Enum/`from`-list validation (TS-SCH-004-01, TS-SCH-004-03) |
| Boundary | HQ snapshot carrying the predicted referential breaks | Validate-HQ-cleanly (TS-SCH-004-04) |

### Management

- **Setup:** schema fixtures authored as small khub-vocabulary YAML files under a tests fixtures dir; the firm-ops case compiles `core.yaml` + `firm-ops.yaml`; the HQ snapshot is a read-only copy of a branch of `firm-hq`.
- **Cleanup:** `generated/` written to a per-test temp directory and torn down after each test; no shared `generated/` between tests.
- **Isolation:** each scenario resolves and compiles from its own fixture into its own temp output dir; the HQ snapshot is mounted read-only so validation never mutates source files.

---

## Test Environment

### Stack

| Tool | Purpose |
|------|---------|
| pytest | Unit and integration tests |
| pytest tmp_path fixtures | Compile-into-temp-dir isolation and byte-diff assertions |
| linkml / linkml-runtime | Real generation backend under test (LinkML → Pydantic v2, JSON Schema) |
| Pydantic v2 | Validating entities against generated models (`extra="allow"`) |
| HQ snapshot fixture | E2E firm-ops validation against the live corpus, read-only |

### Mocks

| Service | Strategy | Rationale |
|---------|----------|-----------|
| LinkML generation | Real, not mocked | LinkML is the contract-under-test; mocking it would test nothing |
| Filesystem `generated/` | Real temp dir (`tmp_path`) | Determinism and atomic-no-partial-write need real file output |
| HQ corpus | Read-only snapshot fixture | Validates real data without touching the live repo or its integrations |
| CRM / Recorder / Airtable | Not exercised | External ids ride as `source_id` aliases, not entities; outside schema-layer scope |

---

## Execution Plan

| Phase | Tests | Gate | Target |
|-------|-------|------|--------|
| 1. Unit | Vocabulary parsing, base merge, predicate/target resolution, porting-note transforms | Block PR | < 30s |
| 2. Integration | Resolve → compile flow, generated Pydantic/JSON Schema, located-error rejection, determinism | Block PR | < 2min |
| 3. E2E | Declare-and-reject-a-bad-write; compile firm-ops and validate the HQ snapshot | Block merge | < 5min |

### CI Triggers

- **Pull request:** Phases 1-2 (fast feedback)
- **Main branch:** Phases 1-3 (full coverage, including the firm-ops HQ-snapshot E2E)
- **Pre-release:** Phases 1-3 (no separate performance phase; the compiler is not a runtime hot path)

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| LinkML → Pydantic/JSON Schema generation is non-deterministic (ordering, timestamps) | TS-SCH-003-04 flaky; drift in `generated/` | Pin the LinkML version; normalize and sort generation output before byte-diff; assert on a canonical form |
| HQ snapshot drifts from the porting-note predictions as the live repo changes | TS-SCH-004-04 surfaces unexpected breaks | Freeze the snapshot to a tagged branch; treat new breaks as a spec-update signal, not a test failure |
| Located-error messages are asserted verbatim and brittle to wording changes | TS-SCH-001-03, TS-SCH-002-03, TS-SCH-003-03 break on cosmetic edits | Assert on the located fields (type, relation, target) plus a message substring, keeping the spec's exact text as the canonical example |
| `extra="allow"` open-schema writes mask constraint regressions | TS-SCH-001-02 passes while real validation runs loose | Test both that declared constraints reject and that undeclared fields are tolerated, as distinct cases |
