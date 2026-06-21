---
id: WPK-000-2
name: Schema Compiler
feature-spec: plan/specs/FS-000-schema-compiler.md
test-spec: plan/tests/TS-000-schema-compiler.md
stories: [STORY-SCH-003]
test-scenarios: [TS-SCH-003-01, TS-SCH-003-02, TS-SCH-003-03, TS-SCH-003-04]
depends-on: [WPK-000-1]
updated: 2026-06-21
---

# Schema Compiler

## Objective

Delivers the compiler — the verb that turns a resolved khub schema into its generated validation artifacts. It emits a LinkML schema from khub vocabulary, then generates Pydantic v2 models (validation, typed return objects) and JSON Schema (tools, editors) into `generated/`. LinkML stays a hidden backend so operators write only khub vocabulary; regeneration is deterministic and overwrites prior output; a malformed schema fails the compile atomically, writing no partial artifacts.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-SCH-003 | AC-001 | Compile a resolved schema: emit LinkML, generate Pydantic v2 and JSON Schema into `generated/`, marked derived and gitignored | TS-SCH-003-01 |
| STORY-SCH-003 | AC-002 | LinkML stays the hidden backend; khub vocabulary goes in and generated artifacts come out (round-trip), operators never write LinkML | TS-SCH-003-02 |
| STORY-SCH-003 | AC-003 | A duplicate type or import cycle fails the compile with a located error and writes no artifacts | TS-SCH-003-03 |
| STORY-SCH-003 | AC-004 | Two runs on an unchanged schema produce byte-identical artifacts; the second overwrites the first without drift | TS-SCH-003-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-SCH-003 | REQ-SCH003-01 | EARS-E | When the compiler runs on a resolved schema, the system shall generate Pydantic and JSON Schema into `generated/` | TS-SCH-003-U02 |
| STORY-SCH-003 | REQ-SCH003-02 | EARS-U | The system shall keep LinkML as a hidden backend; operators write only khub vocabulary | TS-SCH-003-U01 |
| STORY-SCH-003 | REQ-SCH003-03 | EARS-W | If the schema is malformed, then the system shall fail the compile and write no artifacts | TS-SCH-003-U03 |
| STORY-SCH-003 | REQ-SCH003-04 | EARS-U | The system shall regenerate `generated/` deterministically from the schema | TS-SCH-003-U04 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-SCH-003 | SCH-007 | `generated/` is derived, gitignored, and never hand-edited | Constraint | TS-SCH-003-U05 |
| STORY-SCH-003 | SCH-008 | Validation, typed objects, and tool schemas all derive from the one compiled contract | Constraint | TS-SCH-003-U02 |
| STORY-SCH-003 | SCH-008 | Pydantic v2 models use `extra="allow"` for open-schema writes | Constraint | TS-SCH-003-U06 |
| STORY-SCH-003 | SCH-009 | A malformed schema fails the compile atomically; no partial artifacts | Constraint | TS-SCH-003-U03 |
| STORY-SCH-003 | SCH-SHARED-001 | Operators write khub vocabulary; LinkML is a hidden generation backend | Constraint | TS-SCH-003-02 |
| STORY-SCH-003 | SCH-SHARED-003 | `generated/` is derived and gitignored, regenerated from the schema | Constraint | TS-SCH-003-01 |

---

## Entities

### Compiled Artifact

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Location | Text | Yes | `.khub/generated/` — derived, gitignored, never hand-edited |
| LinkML Schema | Reference | Yes | The intermediate LinkML emitted from khub vocabulary |
| Pydantic Models | Reference | Yes | Pydantic v2 models for validation and typed return objects (`extra="allow"`) |
| JSON Schema | Reference | Yes | JSON Schema for tools (MCP, post-v1) and editors |

### Compile Target *(named enumeration)*

| Value | Description |
|-------|-------------|
| linkml | The intermediate LinkML schema emitted from khub vocabulary |
| pydantic | Pydantic v2 models for validation and typed return objects |
| json-schema | JSON Schema for tools and editors |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection
>
> The compiler's input is the `ResolvedSchema` produced by WPK-000-1 (Schema, Base Block, Type Declaration). Those entity definitions live in that work package and are not repeated here.

---

## State Transitions

The compiler is a transform with two outcomes, not an entity workflow. The state of interest is the compile result, which encodes the atomic-no-partial-write invariant (SCH-009).

```
┌──────────────────┐
│ Resolved Schema  │
└────────┬─────────┘
         │ core.compile(schema)
         ▼
   ┌───────────┐
   │ Compiling │
   └─────┬─────┘
         │
         ├── valid ──────────► Generated (generated/ overwritten)
         └── malformed ─────► Rejected (located error, no artifacts)
```

| From | Action | To | Conditions |
|------|--------|----|------------|
| Resolved Schema | `core.compile(schema)` | Generated | Schema is well-formed; `generated/` overwritten deterministically |
| Resolved Schema | `core.compile(schema)` | Rejected | Duplicate type or import cycle; no artifacts written |

---

## API Contracts

> This work package ships a library verb plus its CLI trigger, not an HTTP endpoint.

### STORY-SCH-003: Compile Schema

- **Verb:** `core.compile(schema) -> generated/` over `linkml` / `linkml-runtime`
- **CLI:** `khub compile` (operator-facing trigger)
- **Input:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| schema | Reference | Yes | The resolved schema from WPK-000-1 (or schema files the compiler resolves first) |

- **Output (success):**

| Field | Type | Description |
|-------|------|-------------|
| generated/ | Reference | Regenerated `.khub/generated/` — LinkML, Pydantic v2, JSON Schema; overwrites prior output |

- **Errors:**

| Error | Condition | Message |
|-------|-----------|---------|
| duplicate_type | The schema declares the same type twice | `Duplicate type 'project'` |
| import_cycle | The imports form a cycle | `Import cycle through 'core'` |

---

## Test Data

### Schema (compile input)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | resolved two-type schema, base merged | Clean compile + determinism (TS-SCH-003-01, TS-SCH-003-04) |
| Invalid | duplicate `project` type; imports cycling through `core` | Atomic-fail, no partial artifacts (TS-SCH-003-03) |
| Boundary | identical schema compiled twice into the same target | Byte-identical regeneration (TS-SCH-003-04) |

---

## Implementation Notes

- **LinkML hidden backend:** emit a LinkML schema from khub vocabulary, then generate Pydantic v2 and JSON Schema from it. Operators never see LinkML — khub vocabulary in, artifacts out (REQ-SCH003-02, SCH-SHARED-001). The round-trip test feeds authored `core.yaml` + preset with no LinkML keywords.
- **Open-schema writes:** Pydantic v2 models are generated with `extra="allow"` so writes can carry undeclared fields (SCH-008). Test both directions as distinct cases — declared constraints still reject, and undeclared fields are tolerated (TS-000 Risks; TS-SCH-003-U06).
- **Determinism:** pin the LinkML version; normalize and sort generation output before byte-diff; assert on a canonical form (TS-000 Risks). Two runs on an unchanged schema produce byte-identical `generated/` (REQ-SCH003-04). LinkML ordering/timestamps are the known flakiness source.
- **`generated/` is derived:** gitignored, never hand-edited (SCH-007); each compile overwrites the prior output without drift.
- **Atomic fail:** a duplicate type or import cycle fails the whole compile and leaves `generated/` untouched — no partial artifacts (SCH-009). Use a per-test temp output dir; assert nothing is written on failure.
- **Located errors:** assert on the located fields plus a message substring rather than the verbatim string (TS-000 Risks).
- **Dependency:** consumes the `ResolvedSchema` from WPK-000-1. Tests resolve small khub-vocabulary fixtures, then compile into a `tmp_path` directory.

---

## Done Criteria

- [ ] All 4 acceptance criteria pass (see AC table above)
- [ ] All 4 EARS requirements tested (see Requirements table above)
- [ ] All 3 business rules enforced (SCH-007, SCH-008, SCH-009), plus the 2 shared rules (see Business Rules table above)
- [ ] All 4 integration/E2E test scenarios pass (TS-SCH-003-01..04)
- [ ] All 6 unit tests pass (TS-SCH-003-U01..U06)
- [ ] Determinism asserted on a normalized/canonical form with the LinkML version pinned
- [ ] `extra="allow"` tested both ways: declared constraints reject, undeclared fields tolerated
- [ ] Malformed-schema compile writes no artifacts to the temp `generated/`
- [ ] No regressions in existing tests
