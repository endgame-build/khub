---
id: FS-000
name: Schema & Compiler
priority: Critical
dependencies: []
updated: 2026-06-21
---

# Schema & Compiler

## Overview

The schema layer is the foundation every other surface stands on: operators declare types in khub's own vocabulary — `base`, `entities`, `attributes`, `relations`, and storage config — and the compiler resolves that into LinkML, which generates the Pydantic models (validation) and JSON Schema (tools, editors) into `.khub/generated/`. The schema is the contract; surfaces introspect it at runtime and hardcode no per-type knowledge. The v1 deliverable is two authored files — `core.yaml` (the base block) and `firm-ops.yaml` (9 types, 14 relation predicates, the LinkML port of `hq.schema.yml`) — plus the compile pipeline that turns them into validation artifacts.

**Primary Actor:** Operator

**Depends on:** None (foundation)

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-SCH-001 | Declare a Type in khub Vocabulary | Operator |
| STORY-SCH-002 | Merge the Base Block and Override | Operator |
| STORY-SCH-003 | Compile the Schema to LinkML, Pydantic, and JSON Schema | Operator, Agent |
| STORY-SCH-004 | Author the Firm-Ops Preset | Operator |

---

## Stories

---

### STORY-SCH-001: Declare a Type in khub Vocabulary

**As an** Operator
**I want to** declare an entity type with attributes, enums, relations, and storage config in khub vocabulary
**So that** I model a domain without writing raw LinkML, and the engine compiles my declaration into validation

#### Preconditions

- [ ] PRE-001: A schema file (`core.yaml` or a preset) is being authored
- [ ] PRE-002: The khub vocabulary keywords are recognized (`attributes`, `relations`, `layout`, `format`)

#### Acceptance Criteria

##### AC-001: Declare a Well-Formed Type

**Given** an operator declares a type with scalar attributes, an enum, and a typed relation
**When** they add `project: { layout: folder, attributes: { stage: {enum: [diagnose, prove, scale, complete], required: true} }, relations: { client: {to: client, required: true}, owner: {to: person, required: true} } }`
**Then** the system shall:
- [ ] Accept `attributes` as scalars and enums
- [ ] Accept `relations` as typed edges with a `to:` target (a single type, a list of types, or `any`) and a cardinality
- [ ] Accept the storage config (`layout: folder`)
- [ ] Resolve the type so it compiles to a validatable model
- [ ] Treat the relation field name as the predicate

> `project` here is illustrative vocabulary, shown in flow-style YAML to demonstrate the type-declaration grammar — not a placement directive. The real `project` type is authored in `firm-ops.yaml` (STORY-SCH-004). In v1, `core.yaml` holds only the `base` block; entity types live in the preset.

##### AC-002: Declared Constraints Generate Validation

**Given** a type declares an enum, a pattern, and a cardinality
**When** the schema compiles
**Then** the system shall:
- [ ] Generate validation that rejects an out-of-enum value
- [ ] Generate validation that rejects a pattern mismatch
- [ ] Generate validation that enforces single versus many on relations

##### AC-003: Malformed Declaration

**Given** a type names a relation target type that does not exist
**When** the operator declares `relations: { owner: {to: persn} }` and the schema compiles
**Then** the system shall:
- [ ] Reject the schema with a located error
- [ ] Display: "Type 'project' relation 'owner' targets unknown type 'persn'"
- [ ] Write no generated artifacts

##### AC-004: A Type Declares Only Its Delta

**Given** a type that carries only domain-specific fields
**When** the operator declares `client: { layout: file, attributes: { name: {required: true}, industry: {} } }`
**Then** the system shall:
- [ ] Inherit `type`, `draft`, `created`, `updated`, `tags`, and the OKF fields from the base block
- [ ] Not require the operator to redeclare base attributes

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-SCH001-01 | EARS-E | When a type is declared, the system shall accept attributes, relations, and storage config in khub vocabulary |
| REQ-SCH001-02 | EARS-U | The system shall treat a relation's field name as its predicate and its value as the target |
| REQ-SCH001-03 | EARS-W | If a relation targets an unknown type, then the system shall reject the schema with a located error |
| REQ-SCH001-04 | EARS-U | The system shall let a type declare only its domain delta, inheriting the base block |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| SCH-001 | Operators write khub vocabulary, never raw LinkML | Constraint |
| SCH-002 | A relation's field name is the predicate; its value is the target | Constraint |
| SCH-003 | A typed relation names a target type the schema declares, a list of such types, or `any` | Validation |

#### Technical Notes

- **Artifact:** an `entities:` block in `core.yaml` or a preset file
- **Library verb:** schema resolver (khub vocabulary → resolved model)
- **Vocabulary:** `attributes`, `relations` (`to:` type, list of types, or `any`; `many`), `layout`/`format` (nesting deferred post-MVP)
- **Invariant upheld:** the schema is the contract (Principle 3)
- **Output:** a resolved type ready for compile

#### Test Hints

- **Unit:** vocabulary parsing, predicate-from-field-name, target-type resolution
- **Integration:** enum/pattern/cardinality flow through to generated validation
- **E2E:** declare a type, compile, and reject an out-of-enum write against it

---

### STORY-SCH-002: Merge the Base Block and Override

**As an** Operator
**I want to** define the attributes and relations every entity carries once, in `core.yaml`
**So that** every type inherits them at resolve time and any type can override a base attribute by redeclaring it

#### Preconditions

- [ ] PRE-001: `core.yaml` defines a `base` block with attributes and relations
- [ ] PRE-002: `khub init` writes the `base` block as a header into the engagement's `schema.yaml`, alongside the preset's entities (no import declaration)

#### Acceptance Criteria

##### AC-001: Merge Base Into Every Entity

**Given** a `base` block defining `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, `tags`, and the universal edges
**When** the schema resolves
**Then** the system shall:
- [ ] Merge the base attributes into every entity type
- [ ] Merge the universal relations (`related`, `sources`, `references`, `depends_on`, each `to: any`) into every entity type
- [ ] Provide a boolean `draft` field (`draft: true|false`, default `false`) on every entity
- [ ] Not store the merged fields in each type's declaration (merge is at resolve time)

##### AC-002: A Type Overrides a Base Attribute

**Given** a type redeclares an attribute the base also defines
**When** the type sets `updated: {required: true}` while base leaves it optional
**Then** the system shall:
- [ ] Apply the type's declaration over the base for that attribute
- [ ] Leave every other base attribute inherited unchanged

##### AC-003: Missing Base Block

**Given** a schema that declares entities with no `base` block present
**When** the schema resolves
**Then** the system shall:
- [ ] Report that the base block is unavailable
- [ ] Display: "Schema declares entities but no base block; base attributes are missing"
- [ ] Reject the schema rather than produce types with no `draft` flag or `type`

##### AC-004: Universal Edges Available Everywhere

**Given** the base defines `any → any` relations
**When** an operator links any two entities with `related`, `sources`, `references`, or `depends_on`
**Then** the system shall:
- [ ] Accept the edge on any type without that type redeclaring the predicate

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-SCH002-01 | EARS-E | When the schema resolves, the system shall merge the base block into every entity type |
| REQ-SCH002-02 | EARS-U | The system shall let a type override a base attribute by redeclaring it |
| REQ-SCH002-03 | EARS-W | If a schema declares entities with no base block present, then the system shall reject the schema for a missing base |
| REQ-SCH002-04 | EARS-U | The system shall make the universal `any → any` edges available on every type without redeclaration |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| SCH-004 | The base block is merged at resolve time, never copied into each type | Constraint |
| SCH-005 | A type-level declaration overrides the base for that attribute | Constraint |
| SCH-006 | Every entity carries `type` and the boolean `draft` flag from the base | Validation |

#### Technical Notes

- **Artifact:** the `base` block in `core.yaml`; `khub init` writes it as a `base:` header into the engagement `schema.yaml` (the base is not a declared dependency — no `imports`)
- **Library verb:** the resolve-time base merger
- **Base attributes:** `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, `tags`
- **Base relations:** `related`, `sources`, `references`, `depends_on` (all `to: any`, many)
- **Invariant upheld:** standard fields and the lifecycle come from one place

#### Test Hints

- **Unit:** base-merge precedence, override resolution
- **Integration:** missing-base rejection; universal-edge availability
- **E2E:** override `updated` to required on one type, confirm others stay optional

---

### STORY-SCH-003: Compile the Schema to LinkML, Pydantic, and JSON Schema

**As an** Operator or Agent
**I want to** compile the resolved khub schema into LinkML and its generated validation artifacts
**So that** validation, typed return objects, and tool schemas all derive from one contract

#### Preconditions

- [ ] PRE-001: The schema resolves (types declared, base merged)
- [ ] PRE-002: `linkml` and `linkml-runtime` are available

#### Acceptance Criteria

##### AC-001: Compile to Generated Artifacts

**Given** a resolved schema
**When** the compiler runs
**Then** the system shall:
- [ ] Emit a LinkML schema from the khub vocabulary
- [ ] Generate Pydantic v2 models for validation into `.khub/generated/`
- [ ] Generate JSON Schema for tools and editors into `.khub/generated/`
- [ ] Mark `.khub/generated/` as derived and gitignored

##### AC-002: Operators See Only khub Vocabulary

**Given** an operator who never writes LinkML
**When** they compile
**Then** the system shall:
- [ ] Keep LinkML as the generation backend, hidden from the authored files
- [ ] Round-trip: khub vocabulary in, generated artifacts out

##### AC-003: Malformed Schema

**Given** a schema with a duplicate type or an import cycle
**When** the compiler runs
**Then** the system shall:
- [ ] Fail the compile with a located error
- [ ] Display: "Duplicate type 'project'" or "Import cycle through 'core'"
- [ ] Write no generated artifacts

##### AC-004: Regeneration Is Deterministic

**Given** an unchanged schema
**When** the compiler runs twice
**Then** the system shall:
- [ ] Produce identical generated artifacts each run
- [ ] Overwrite the prior `.khub/generated/` output without drift

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-SCH003-01 | EARS-E | When the compiler runs on a resolved schema, the system shall generate Pydantic and JSON Schema into `.khub/generated/` |
| REQ-SCH003-02 | EARS-U | The system shall keep LinkML as a hidden backend; operators write only khub vocabulary |
| REQ-SCH003-03 | EARS-W | If the schema is malformed, then the system shall fail the compile and write no artifacts |
| REQ-SCH003-04 | EARS-U | The system shall regenerate `.khub/generated/` deterministically from the schema |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| SCH-007 | `.khub/generated/` is derived, gitignored, and never hand-edited | Constraint |
| SCH-008 | Validation, typed objects, and tool schemas all derive from the one compiled contract | Constraint |
| SCH-009 | A malformed schema fails the compile atomically; no partial artifacts | Constraint |

#### Technical Notes

- **Artifact:** `.khub/generated/` (LinkML, Pydantic v2, JSON Schema)
- **Library verb:** `core.compile(schema)` over `linkml` / `linkml-runtime`
- **Tooling:** LinkML generation backend; Pydantic v2 `extra="allow"` for open-schema writes
- **Invariant upheld:** the schema is the contract; surfaces introspect, never hardcode
- **Output:** regenerated `.khub/generated/`, or a located compile error

#### Test Hints

- **Unit:** khub-vocabulary → LinkML translation
- **Integration:** Pydantic + JSON Schema generation; duplicate/cycle rejection
- **E2E:** compile firm-ops, then validate an entity against the generated model

---

### STORY-SCH-004: Author the Firm-Ops Preset

**As an** Operator
**I want to** express `hq.schema.yml` as the khub firm-ops preset — 9 types, 14 relation predicates, the four porting notes resolved
**So that** the cutover seeds from a preset that compiles clean and validates the live HQ corpus

#### Preconditions

- [ ] PRE-001: The capture in `docs/firm-ops-preset.md` is the authoritative input
- [ ] PRE-002: `core.yaml` defines the base block
- [ ] PRE-003: The compile pipeline (STORY-SCH-003) is in place

#### Acceptance Criteria

##### AC-001: Declare All Nine Types

**Given** the firm-ops capture
**When** the operator writes `firm-ops.yaml`
**Then** the system shall:
- [ ] Declare opportunity, project, meeting, transcript, fragment, case-study, partnership, person, and client
- [ ] Carry each type's attributes, enums (`stage`, `call_type`, `role`), and patterns
- [ ] Set each type's storage layout (file for client and meeting, folder for project)

##### AC-002: Declare All Edges (14 Stored, 14 Total)

**Given** the relation vocabulary
**When** the operator declares relations
**Then** the system shall:
- [ ] Declare the 14 stored predicates with their `from`, `to`, and cardinality (10 in `firm-ops.yaml`; the 4 universal edges inherited from `core`)
- [ ] Declare typed and union edges, written predicate → target (`owner → person`; `client → client`, i.e. the `client` predicate targets the `client` type; `engagement → opportunity|project|partnership`); rely on `core` for the universal edges (`related`, `sources`, `references`, `depends_on`)
- [ ] Mark required relations (`owner`, `client`, `engagement`)
- [ ] Declare no derived inverse edge, for 14 predicates total

##### AC-003: Resolve the Four Porting Notes

**Given** the divergences between `hq.schema.yml` and khub invariants
**When** the operator ports the schema
**Then** the system shall:
- [ ] Add the boolean `draft` flag over HQ's per-type `stage`/`status`
- [ ] Tighten the `partner` edge `from`-list to include `client`
- [ ] Keep `meeting.engagement` as an explicit stored edge and flatten meetings to `meetings/{slug}.md` (nesting deferred post-MVP)
- [ ] Keep the slug as the id and ride external ids as `source_id` aliases

##### AC-004: Compile Clean and Validate HQ

**Given** the authored preset
**When** the operator compiles and validates against an HQ snapshot read-only
**Then** the system shall:
- [ ] Compile with no errors
- [ ] Validate the snapshot, surfacing only the referential breaks the porting notes predict
- [ ] Stand as the input to the cutover seed (`khub init firm-ops`)

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-SCH004-01 | EARS-U | The firm-ops preset shall declare all 9 types with their attributes, enums, and layouts |
| REQ-SCH004-02 | EARS-U | The firm-ops preset shall declare the 14 stored predicates (10 firm-ops + 4 universal) with `from`, `to`, and cardinality (14 total, no derived inverse) |
| REQ-SCH004-03 | EARS-E | When the preset is ported, the system shall resolve each of the four porting notes |
| REQ-SCH004-04 | EARS-E | When compiled, the firm-ops preset shall generate clean validation artifacts and validate an HQ snapshot |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| SCH-010 | The firm-ops preset is the LinkML port of `hq.schema.yml`, nothing omitted | Constraint |

#### Technical Notes

- **Artifact:** `firm-ops.yaml` (9 types, 14 relation predicates); the base comes from `core.yaml`'s `base` block, written into the engagement by `khub init` (no `imports`)
- **Library verb:** authored content, compiled via STORY-SCH-003
- **Source:** `docs/firm-ops-preset.md` (the capture)
- **Invariant upheld:** relations authoritative; slug = id; the `draft` flag
- **Output:** a preset that seeds `khub init firm-ops`

#### Test Hints

- **Unit:** each type's enum and pattern declarations
- **Integration:** the four porting-note transformations
- **E2E:** compile firm-ops and validate an HQ snapshot cleanly (functional cutover, not byte-parity with `kb.py`)

---

## Shared Context

### Entities

The schema layer's "entities" are the schema constructs themselves, not firm-ops nodes. The firm-ops types they produce are captured in `docs/firm-ops-preset.md`.

| Entity | Description |
|--------|-------------|
| Schema | The authored contract: a base block plus entity types, in khub vocabulary |
| Base Block | The attributes and relations every entity inherits at resolve time |
| Type Declaration | One entity type's attributes, relations, and storage config |
| Compiled Artifact | The generated LinkML, Pydantic, and JSON Schema under `.khub/generated/` |
| Preset | A named, authored schema (firm-ops in v1) that `init` seeds from |

#### Type Declaration

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Name | Text | Yes | The type name (also the predicate target for typed edges) |
| Attributes | Collection | No | Scalars and enums, beyond the inherited base |
| Relations | Collection | No | Typed or `any` edges, with `to:` and cardinality |
| Layout | Type | Yes | file or folder |
| Format | Type | No | Serialization format (default md) |

#### Base Block

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Attributes | Collection | Yes | `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, `tags` |
| Relations | Collection | Yes | `related`, `sources`, `references`, `depends_on` (all `to: any`, many) |
| Lifecycle Flag | Bool | Yes | `draft: true\|false` (default `false`), on every entity |

### Compile Target *(named enumeration)*

| Value | Description |
|-------|-------------|
| linkml | The intermediate LinkML schema emitted from khub vocabulary |
| pydantic | Pydantic v2 models for validation and typed return objects |
| json-schema | JSON Schema for tools (MCP, post-v1) and editors |

### Relation Target Kind *(named enumeration)*

| Value | Description |
|-------|-------------|
| typed | The edge targets a single named type; resolves by the schema-known target |
| union | The edge targets a list of named types (e.g. `engagement → opportunity\|project\|partnership`); resolves by the schema-known targets |
| any | A universal `any → any` edge; resolves by slug, qualified `type/slug` on ambiguity |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| Base block changes | Propagate to every type on the next resolve | The base is merged, not copied |
| A type overrides a base attribute | The type's declaration wins for that attribute | Per-type delta over inherited default |
| Schema changes | Regenerate `.khub/generated/`, overwriting the prior output | Generated artifacts are derived and disposable |
| Schema declares entities with no base block | Reject the schema | No entity may lack `type` and the `draft` flag |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| SCH-SHARED-001 | Operators write khub vocabulary; LinkML is a hidden generation backend | STORY-SCH-001, STORY-SCH-003 |
| SCH-SHARED-002 | The base block is the one source of standard fields and the lifecycle | STORY-SCH-002, STORY-SCH-004 |
| SCH-SHARED-003 | `.khub/generated/` is derived and gitignored, regenerated from the schema | STORY-SCH-003, STORY-SCH-004 |

### Cross-Story Dependencies

```
STORY-SCH-001 (Declare a Type)
STORY-SCH-002 (Merge the Base Block)
    └── STORY-SCH-003 (Compile the Schema)
            └── STORY-SCH-004 (Author the Firm-Ops Preset)
```

Type declaration and base merge are the two halves of a resolved schema; compile turns that into validation artifacts; the firm-ops preset is the first real schema to exercise all three and the seed for `khub init`.

## Scope Changes

### Deferred Stories

| Story | Moved To | Reason | Date |
|-------|----------|--------|------|
| Layered preset sync (subtree + overrides patch) | Deferred | v1 flattens core + preset at init; hub↔engagement sync is post-v1 | 2026-06-20 |
| The engineering preset (intent/behavior spine) | Post-v1 | Built after firm-ops, by hand and via the promote-back flywheel | 2026-06-20 |
| Preset imports / multi-preset composition | Deferred | v1 has no imports; the base is a written `base:` header, not an imported dependency; preset-to-preset composition is unscheduled | 2026-06-21 |
| Nested storage layout (`nests_under`) and placement-derived edges | Deferred | v1 supports file and folder layouts only; nesting is post-MVP | 2026-06-21 |
| Typed list items (an `items:` key) — list-of-number/date and object item shapes | Post-MVP | v1 `list` is untyped (compiles to `list[str]`); no firm-ops field needs a typed list, and object items (e.g. meeting `attendees`) are simplified for v1 | 2026-06-22 |
