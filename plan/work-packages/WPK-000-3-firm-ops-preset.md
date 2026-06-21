---
id: WPK-000-3
name: Firm-Ops Preset
feature-spec: plan/specs/FS-000-schema-compiler.md
test-spec: plan/tests/TS-000-schema-compiler.md
stories: [STORY-SCH-004]
test-scenarios: [TS-SCH-004-01, TS-SCH-004-02, TS-SCH-004-03, TS-SCH-004-04]
depends-on: [WPK-000-2]
updated: 2026-06-21
---

# Firm-Ops Preset

## Objective

Delivers `firm-ops.yaml` — the khub port of `hq.schema.yml` and the first real schema to exercise the resolver and compiler end to end. It declares 12 types with their attributes, enums, and storage layouts; the firm-ops relation predicates (16 stored, 17 total including the derived `superseded_by`); and resolves the six porting notes that reconcile HQ with khub invariants. Authored against `core.yaml`, it must compile clean and validate a read-only HQ snapshot, standing as the seed for `khub init firm-ops`.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-SCH-004 | AC-001 | Declare all 12 types (opportunity, project, build, meeting, transcript, fragment, decision, case-study, isms-doc, partnership, person, client) with attributes, enums (`stage`, `call_type`, `role`, `doc_kind`), patterns, and storage layouts | TS-SCH-004-01 |
| STORY-SCH-004 | AC-002 | Declare the 16 stored predicates with `from`/`to`/cardinality (12 in firm-ops, 4 universal from `core`); typed and union edges; mark required (`owner`, `client`, `engagement`); leave `superseded_by` derived — 17 predicates total | TS-SCH-004-02 |
| STORY-SCH-004 | AC-003 | Resolve the six porting notes (derive `superseded_by`; add boolean `draft`; share `projects/{slug}/`; tighten `partner` from-list; keep `meeting.engagement` + flatten meetings; slug = id with `source_id` aliases) | TS-SCH-004-03 |
| STORY-SCH-004 | AC-004 | Compile with no errors and validate an HQ snapshot read-only, surfacing only the porting-note-predicted referential breaks; stand as the input to `khub init firm-ops` | TS-SCH-004-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-SCH-004 | REQ-SCH004-01 | EARS-U | The firm-ops preset shall declare all 12 types with their attributes, enums, and layouts | TS-SCH-004-U01 |
| STORY-SCH-004 | REQ-SCH004-02 | EARS-U | The firm-ops preset shall declare the 16 stored predicates with `from`, `to`, and cardinality (17 total, including the derived `superseded_by`) | TS-SCH-004-U02 |
| STORY-SCH-004 | REQ-SCH004-03 | EARS-E | When the preset is ported, the system shall resolve each of the six porting notes | TS-SCH-004-U05 |
| STORY-SCH-004 | REQ-SCH004-04 | EARS-E | When compiled, the firm-ops preset shall generate clean validation artifacts and validate an HQ snapshot | TS-SCH-004-U06 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-SCH-004 | SCH-010 | The firm-ops preset is the LinkML port of `hq.schema.yml`, nothing omitted | Constraint | TS-SCH-004-U01 |
| STORY-SCH-004 | SCH-011 | No stored inverse edge; `superseded_by` derives from `supersedes` | Constraint | TS-SCH-004-U03 |
| STORY-SCH-004 | SCH-012 | `project` and `build` share `projects/{slug}/`, discriminated by `type` | Constraint | TS-SCH-004-U04 |
| STORY-SCH-004 | SCH-SHARED-002 | The base block is the one source of standard fields and the lifecycle | Constraint | TS-SCH-004-U01 |
| STORY-SCH-004 | SCH-SHARED-003 | `generated/` is derived and gitignored, regenerated from the schema | Constraint | TS-SCH-004-U06 |

---

## Entities

### Preset

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Name | Text | Yes | `firm-ops` — the named, authored schema that `init` seeds from |
| Imports | Collection | Yes | `[core]` — pulls in the base block (v1 imports `core` only) |
| Types | Collection | Yes | The 12 firm-ops entity types |
| Relations | Collection | Yes | The 12 firm-ops stored predicates (plus 4 universal from `core`; `superseded_by` derived) |
| Source | Reference | Yes | `docs/firm-ops-preset.md` — the authoritative capture |

### Firm-Ops Types *(authored content; full detail in `docs/firm-ops-preset.md`)*

| Type | Layout | Notable attributes / enums |
|------|--------|----------------------------|
| opportunity | per capture | engagement union member |
| project | folder (`projects/{slug}/_index.md`) | `stage` enum; `owner`, `client` required |
| build | folder (shares `projects/{slug}/`) | discriminated from project by `type` |
| meeting | file (`meetings/{slug}.md`) | `call_type` enum; `engagement` explicit stored edge |
| transcript | per capture | sources/references to meeting |
| fragment | per capture | — |
| decision | per capture | `supersedes` (→ decision); `superseded_by` derived |
| case-study | per capture | — |
| isms-doc | per capture | `doc_kind` enum |
| partnership | per capture | engagement union member |
| person | per capture | `role` enum; target of `owner` |
| client | file | target of `client`; member of `partner` from-list |

### Relation Predicates *(16 stored + 1 derived = 17; full set in `docs/firm-ops-preset.md`)*

| Predicate | From | To | Cardinality | Notes |
|-----------|------|----|-----------|-------|
| owner | (types with ownership) | person | single, required | typed edge |
| client | (engagement types) | client | single, required | typed edge |
| engagement | meeting, … | opportunity \| project \| build \| partnership | single, required | union edge |
| partner | client, … | (partner targets) | many | from-list tightened to include `client` |
| supersedes | decision | decision | many | inverse `superseded_by` derived, not stored |
| related / sources / references / depends_on | any | any | many | the 4 universal edges, inherited from `core` |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection
>
> The schema constructs (Schema, Base Block, Type Declaration) are defined in WPK-000-1. This work package authors firm-ops *content*; the per-type attribute detail beyond the spec lives in `docs/firm-ops-preset.md`.

---

## State Transitions

Firm-ops entities carry the base `draft` flag (default `false`); there is no per-type workflow. The one porting-note-relevant relationship is `supersedes`: a decision's superseded status is *derived* from another decision's `supersedes` edge, never stored as an inverse.

```
┌───────────────────────┐
│  Published            │
│  (draft=false)        │
└──────────┬────────────┘
           │ set draft: true / false
           ▼
        Draft  ⇄  Published

decision A  --supersedes-->  decision B
   (B's "superseded_by" is derived from A, never stored)
```

| From | Action | To | Conditions |
|------|--------|----|------------|
| Published | Set `draft: true` | Draft | Every firm-ops entity inherits the flag from `core` |
| decision (current) | Another decision declares `supersedes` it | decision (superseded) | Status derived from `supersedes`; no stored `superseded_by` (SCH-011) |

---

## API Contracts

> This work package ships authored content compiled via WPK-000-2, not a code endpoint.

### STORY-SCH-004: Author and Compile the Firm-Ops Preset

- **Verb:** authored `firm-ops.yaml` (`imports: [core]`), compiled via `core.compile` (WPK-000-2)
- **CLI:** the compiled preset seeds `khub init firm-ops`
- **Input:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| docs/firm-ops-preset.md | Reference | Yes | The authoritative capture — 12 types, 17 relation predicates, six porting notes |
| core.yaml | Reference | Yes | The base block the preset imports |
| HQ snapshot | Reference | Yes | Read-only copy of a tagged `firm-hq` branch, for validation |

- **Output (success):**

| Field | Type | Description |
|-------|------|-------------|
| firm-ops.yaml | Reference | The authored preset — compiles clean, validates the HQ snapshot, seeds `khub init firm-ops` |

- **Errors / surfaced breaks:**

| Error | Condition | Message |
|-------|-----------|---------|
| enum_violation | A value falls outside a declared enum (e.g. `meeting.call_type`) | Located validation error naming the type and field |
| invalid_from | A `partner` edge originates from a type outside the tightened from-list | Located error naming the edge and source type |
| predicted_referential_break | HQ snapshot data references a missing target | Surfaced (expected per porting notes), not a compile failure |

---

## Test Data

### Firm-Ops Entity (decision, project, build, meeting, client)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | decision with `supersedes`; project + build under `projects/{slug}/` | Porting-note transforms (TS-SCH-004-03) |
| Invalid | `meeting` with a value outside the `call_type` enum; `partner` from an undeclared `from` type | Enum / `from`-list validation (TS-SCH-004-01, TS-SCH-004-03) |
| Boundary | HQ snapshot carrying the predicted referential breaks | Validate-HQ-cleanly (TS-SCH-004-04) |

---

## Implementation Notes

- **Source of truth:** `docs/firm-ops-preset.md` is the authoritative capture. Author `firm-ops.yaml` with `imports: [core]`, 12 types, 17 relation predicates (16 stored + derived `superseded_by`).
- **The six porting notes (AC-003):**
  1. Drop `superseded_by`; derive it from `supersedes` — no stored inverse (SCH-011).
  2. Add the boolean `draft` flag over HQ's per-type `stage`/`status`.
  3. Declare `project` and `build` under the same `projects/{slug}/_index.md` layout, discriminated by `type` (SCH-012).
  4. Tighten the `partner` edge `from`-list to include `client`.
  5. Keep `meeting.engagement` as an explicit stored edge; flatten meetings to `meetings/{slug}.md` (nesting deferred post-MVP).
  6. Keep the slug as the id; ride external ids as `source_id` aliases.
- **Compile then validate:** compile via WPK-000-2, then validate against a read-only HQ snapshot. The snapshot must surface *only* the referential breaks the porting notes predict.
- **Freeze the snapshot:** pin it to a tagged `firm-hq` branch. New breaks beyond the predicted set are a spec-update signal, not a test failure (TS-000 Risks). Mount read-only so validation never mutates source.
- **Functional cutover, not byte-parity:** the E2E proves a clean compile + clean validate, not byte-for-byte equivalence with `kb.py` (TS-000 test hints).
- **External systems are not entities:** CRM / Recorder / Airtable are not modeled; their ids ride as `source_id` aliases (TS-000 mocks).
- **Dependency:** requires the compiler (WPK-000-2) and, transitively, the resolver (WPK-000-1).

---

## Done Criteria

- [ ] All 4 acceptance criteria pass (see AC table above)
- [ ] All 4 EARS requirements tested (see Requirements table above)
- [ ] All 3 business rules enforced (SCH-010, SCH-011, SCH-012), plus the 2 shared rules (see Business Rules table above)
- [ ] All 4 integration/E2E test scenarios pass (TS-SCH-004-01..04)
- [ ] All 6 unit tests pass (TS-SCH-004-U01..U06)
- [ ] All 12 types declared with attributes, enums, and layouts
- [ ] 16 stored predicates declared (12 firm-ops + 4 universal from `core`); `superseded_by` derived — 17 total
- [ ] All six porting notes resolved
- [ ] firm-ops compiles clean and validates the frozen HQ snapshot, surfacing only predicted breaks
- [ ] Stands as the seed for `khub init firm-ops`
- [ ] No regressions in existing tests
