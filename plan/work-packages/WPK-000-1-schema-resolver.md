---
id: WPK-000-1
name: Schema Resolver
feature-spec: plan/specs/FS-000-schema-compiler.md
test-spec: plan/tests/TS-000-schema-compiler.md
stories: [STORY-SCH-001, STORY-SCH-002]
test-scenarios: [TS-SCH-001-01, TS-SCH-001-02, TS-SCH-001-03, TS-SCH-001-04, TS-SCH-002-01, TS-SCH-002-02, TS-SCH-002-03, TS-SCH-002-04]
depends-on: []
updated: 2026-06-21
---

# Schema Resolver

## Objective

Delivers the resolver — the half of the schema layer that turns authored khub vocabulary into a resolved model ready for compile. An operator declares entity types (attributes, enums, typed and `any` relations, storage config) and defines the base block once in `core.yaml`; the resolver merges that base into every type at resolve time, applies per-type overrides, treats each relation's field name as its predicate, and rejects malformed declarations with located errors before any artifact is written.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-SCH-001 | AC-001 | Declare a well-formed type — accept scalar/enum attributes, typed relations (`to:` single/list/`any` + cardinality), and storage layout; resolve to a validatable model; treat each relation field name as the predicate | TS-SCH-001-01 |
| STORY-SCH-001 | AC-002 | Declared enum, pattern, and cardinality constraints flow through to generated validation | TS-SCH-001-02 |
| STORY-SCH-001 | AC-003 | A relation targeting an unknown type is rejected with a located error; no artifacts written | TS-SCH-001-03 |
| STORY-SCH-001 | AC-004 | A type declares only its domain delta, inheriting `type`, `draft`, `created`, `updated`, `tags`, and OKF fields from the base | TS-SCH-001-04 |
| STORY-SCH-002 | AC-001 | Merge base attributes and the universal `any → any` edges into every entity at resolve time; every entity carries boolean `draft` (default `false`) | TS-SCH-002-01 |
| STORY-SCH-002 | AC-002 | A type overrides a base attribute by redeclaring it; all other base attributes stay inherited unchanged | TS-SCH-002-02 |
| STORY-SCH-002 | AC-003 | A preset omitting `imports: [core]` is rejected with a located error for the missing base | TS-SCH-002-03 |
| STORY-SCH-002 | AC-004 | The universal edges (`related`, `sources`, `references`, `depends_on`) are accepted on any type without redeclaration | TS-SCH-002-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-SCH-001 | REQ-SCH001-01 | EARS-E | When a type is declared, the system shall accept attributes, relations, and storage config in khub vocabulary | TS-SCH-001-U01 |
| STORY-SCH-001 | REQ-SCH001-02 | EARS-U | The system shall treat a relation's field name as its predicate and its value as the target | TS-SCH-001-U02 |
| STORY-SCH-001 | REQ-SCH001-03 | EARS-W | If a relation targets an unknown type, then the system shall reject the schema with a located error | TS-SCH-001-U06 |
| STORY-SCH-001 | REQ-SCH001-04 | EARS-U | The system shall let a type declare only its domain delta, inheriting the base block | TS-SCH-001-U05 |
| STORY-SCH-002 | REQ-SCH002-01 | EARS-E | When the schema resolves, the system shall merge the base block into every entity type | TS-SCH-002-U01 |
| STORY-SCH-002 | REQ-SCH002-02 | EARS-U | The system shall let a type override a base attribute by redeclaring it | TS-SCH-002-U02 |
| STORY-SCH-002 | REQ-SCH002-03 | EARS-W | If a preset does not import `core`, then the system shall reject the schema for a missing base | TS-SCH-002-U04 |
| STORY-SCH-002 | REQ-SCH002-04 | EARS-U | The system shall make the universal `any → any` edges available on every type without redeclaration | TS-SCH-002-U05 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-SCH-001 | SCH-001 | Operators write khub vocabulary, never raw LinkML | Constraint | TS-SCH-001-U04 |
| STORY-SCH-001 | SCH-002 | A relation's field name is the predicate; its value is the target | Constraint | TS-SCH-001-U02 |
| STORY-SCH-001 | SCH-003 | A typed relation names a declared target type, a list of such types, or `any` | Validation | TS-SCH-001-U03 |
| STORY-SCH-002 | SCH-004 | The base block is merged at resolve time, never copied into each type | Constraint | TS-SCH-002-U01 |
| STORY-SCH-002 | SCH-005 | A type-level declaration overrides the base for that attribute | Constraint | TS-SCH-002-U02 |
| STORY-SCH-002 | SCH-006 | Every entity carries `type` and the boolean `draft` flag from the base | Validation | TS-SCH-002-U03 |
| STORY-SCH-001 | SCH-SHARED-001 | Operators write khub vocabulary; LinkML is a hidden generation backend | Constraint | TS-SCH-001-U04 |
| STORY-SCH-002 | SCH-SHARED-002 | The base block is the one source of standard fields and the lifecycle | Constraint | TS-SCH-002-U01 |

---

## Entities

### Schema

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Imports | Collection | Yes | The presets pulled in; a preset must import `core` for the base |
| Base Block | Reference | Yes | The attributes and relations every entity inherits (from `core.yaml`) |
| Type Declarations | Collection | No | The entity types this schema declares beyond the base |

### Type Declaration

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Name | Text | Yes | The type name (also the predicate target for typed edges) |
| Attributes | Collection | No | Scalars and enums, beyond the inherited base |
| Relations | Collection | No | Typed or `any` edges, with `to:` and cardinality |
| Layout | Type | Yes | file or folder |
| Format | Type | No | Serialization format (default md) |

### Base Block

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Attributes | Collection | Yes | `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, `tags` |
| Relations | Collection | Yes | `related`, `sources`, `references`, `depends_on` (all `to: any`, many) |
| Lifecycle Flag | Yes/No | Yes | `draft: true\|false` (default `false`), on every entity |

### Relation Target Kind *(named enumeration)*

| Value | Description |
|-------|-------------|
| typed | The edge targets a single named type; resolves by the schema-known target |
| union | The edge targets a list of named types (e.g. `engagement → opportunity\|project\|build\|partnership`) |
| any | A universal `any → any` edge; resolves by slug, qualified `type/slug` on ambiguity |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

The only lifecycle the resolver introduces is the base block's `draft` flag, carried by every entity. It is a boolean, not a workflow: entities default to published (`draft: false`) and an operator may flag a type as a draft.

```
┌───────────────────────┐
│  Published (draft=     │
│  false, default)       │
└──────────┬────────────┘
           │ set draft: true
           ▼
┌───────────────────────┐
│  Draft (draft=true)    │
└──────────┬────────────┘
           │ set draft: false
           ▼
       Published
```

| From | Action | To | Conditions |
|------|--------|----|------------|
| Published | Set `draft: true` | Draft | Every entity carries the flag from the base |
| Draft | Set `draft: false` | Published | Default state when the flag is absent |

---

## API Contracts

> This work package ships library verbs, not HTTP endpoints. "Contracts" below describe the resolver surface other layers call.

### STORY-SCH-001 + STORY-SCH-002: Resolve Schema

- **Verb:** `core.resolve(schema_files) -> ResolvedSchema`
- **Surface:** library (internal); invoked by the compiler (WPK-000-2)
- **Input:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| schema_files | Collection | Yes | Authored khub-vocabulary YAML — `core.yaml` plus any imported preset |

- **Output (success):**

| Field | Type | Description |
|-------|------|-------------|
| ResolvedSchema | Reference | All declared types with the base merged in, overrides applied, predicates resolved — ready for compile |

- **Errors:**

| Error | Condition | Message |
|-------|-----------|---------|
| unknown_relation_target | A relation `to:` names a type the schema does not declare | `Type 'project' relation 'owner' targets unknown type 'persn'` |
| missing_base_import | A preset omits `imports: [core]` | `Preset does not import 'core'; base attributes are missing` |
| raw_linkml_smuggled | A declaration uses raw LinkML constructs instead of khub vocabulary | Located error naming the offending construct |

---

## Test Data

### Type Declaration

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `project: { layout: folder, attributes: { stage: {enum: [diagnose, prove, scale, complete], required: true} }, relations: { client: {to: client, required: true}, owner: {to: person, required: true} } }` | Happy-path declare-and-resolve (TS-SCH-001-01) |
| Invalid | `relations: { owner: {to: persn} }` — target type not declared | Located-error rejection (TS-SCH-001-03) |
| Boundary | `client: { layout: file, attributes: { name: {required: true}, industry: {} } }` — only its delta, no base fields | Delta-only inheritance (TS-SCH-001-04) |

### Base Block

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, `tags` + `related`/`sources`/`references`/`depends_on` (`to: any`, many) | Merge-into-every-entity (TS-SCH-002-01) |
| Override | a type sets `updated: {required: true}` over optional base | Override precedence (TS-SCH-002-02) |
| Missing | preset without `imports: [core]` | Missing-import rejection (TS-SCH-002-03) |

---

## Implementation Notes

- **Resolve, don't copy:** the base block is merged into each type at resolve time. The merged fields are never written back into the type's declaration (SCH-004). A change to the base propagates to every type on the next resolve.
- **Override precedence:** a type-level attribute declaration replaces the base's declaration for that one attribute; every other base attribute stays inherited unchanged (SCH-005). Confirm by overriding `updated` to required on one type and checking a sibling stays optional.
- **Predicate from field name:** a relation's field name is its predicate; its `to:` value is the target — a single type, a list of types, or `any` (SCH-002, SCH-003). Resolve the target against the set of declared types.
- **Universal edges:** `related`, `sources`, `references`, `depends_on` (all `to: any`, many) come from the base and are available on every type without redeclaration (STORY-SCH-002 AC-004).
- **Located errors carry their location:** each rejection names the type, relation, and target. Tests assert the located fields plus a message substring rather than the exact string verbatim, keeping the spec's wording as the canonical example (see TS-000 Risks).
- **No artifacts on rejection:** the resolver fails before the compiler runs; a malformed schema produces no `generated/` output (atomicity is completed by WPK-000-2).
- **Hidden backend:** operators see only khub vocabulary; raw LinkML smuggled into a declaration is rejected (SCH-001, SCH-SHARED-001).
- **Out of scope here:** LinkML/Pydantic/JSON Schema generation (WPK-000-2); the real `core.yaml` base content and the firm-ops preset (WPK-000-3). This WPK uses small khub-vocabulary fixtures.

---

## Done Criteria

- [ ] All 8 acceptance criteria pass (see AC table above)
- [ ] All 8 EARS requirements tested (see Requirements table above)
- [ ] All 6 business rules enforced, plus the 2 shared rules (see Business Rules table above)
- [ ] All 8 integration/E2E test scenarios pass (TS-SCH-001-01..04, TS-SCH-002-01..04)
- [ ] All 11 unit tests pass (TS-SCH-001-U01..U06, TS-SCH-002-U01..U05)
- [ ] Located errors assert on type/relation/target fields plus a message substring
- [ ] No regressions in existing tests
