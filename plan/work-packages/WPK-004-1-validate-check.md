---
id: WPK-004-1
name: Validate Entities and Check the Graph
feature-spec: plan/specs/FS-004-integrity.md
test-spec: plan/tests/TS-004-integrity.md
stories: [STORY-INT-001, STORY-INT-002]
test-scenarios: [TS-INT-001-01, TS-INT-001-02, TS-INT-001-03, TS-INT-001-04, TS-INT-001-U01, TS-INT-001-U02, TS-INT-001-U03, TS-INT-001-U04, TS-INT-001-U05, TS-INT-001-U06, TS-INT-001-U07, TS-INT-002-01, TS-INT-002-02, TS-INT-002-03, TS-INT-002-04, TS-INT-002-05, TS-INT-002-U01, TS-INT-002-U02, TS-INT-002-U03, TS-INT-002-U04, TS-INT-002-U05, TS-INT-002-U06, TS-INT-002-U07, TS-INT-002-U08]
depends-on: [WPK-002-1, WPK-002-3, WPK-003-1]
updated: 2026-06-27
---

# Validate Entities and Check the Graph

## Objective

Delivers the v1 integrity gate: `khub validate` (`core.validate`) checks per-entity well-formedness and referential integrity over a declared subset, and `khub check` (`core.check`) runs a graph-wide pass over the active (`draft: false`) subgraph. `validate` is per-entity and runs first — it checks each present declared field against the generated Pydantic model's type, enum, pattern, and cardinality, confirms every relation resolves, leaves undeclared extensions alone unless `--strict`, and collects every error rather than stopping at the first. `check` assumes entities individually validate and walks the `networkx` projection for the structural gaps that define it: required-completeness computed from the schema over active entities (never inferred from the `draft` flag), orphans, dangling edges, stray files, and edge cycles. Both uphold one invariant — khub guarantees structural integrity, never semantic truth: a false-but-legal write passes both gates, with git history as the only backstop.

> **Preset note:** the cycle scenario (INT-002 AC-005) runs on the universal acyclic `depends_on` edge present in v1 firm-ops; FS-004's original `supersedes` cycle example was HQ-only and dropped. The preset is source of truth — see `docs/firm-ops-preset.md` and the spec's reconciliation notes.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-INT-001 | AC-001 | Validate a clean tree: `khub validate` checks each present declared field against type/enum/pattern/cardinality, confirms every relation resolves, leaves undeclared extensions unchecked, displays `Validated N entities; 0 errors`, exits 0 | TS-INT-001-01 |
| STORY-INT-001 | AC-002 | Report per-entity errors: a malformed value and an unresolved relation are each reported with entity id, field, and reason; validation continues past the first error and reports both; exits non-zero | TS-INT-001-02 |
| STORY-INT-001 | AC-003 | Strict closes the schema: `khub validate --strict` rejects undeclared keys as errors; the same tree passes without `--strict` (extensions allowed) | TS-INT-001-03 |
| STORY-INT-001 | AC-004 | Skip reference files cleanly: files with no recognized frontmatter are skipped not errored; only typed entities are counted in `N` | TS-INT-001-04 |
| STORY-INT-002 | AC-001 | Check a sound graph: `khub check` confirms every relation resolves, required-completeness for every active entity (from the schema), no orphans, no stray files, no edge cycles; displays `Graph check passed`, exits 0 | TS-INT-002-01 |
| STORY-INT-002 | AC-002 | Report active-but-incomplete entities: a published entity missing a required field/relation is reported active-but-incomplete, naming each gap; completeness computed from the schema, never the `draft` flag; exits non-zero | TS-INT-002-02 |
| STORY-INT-002 | AC-003 | Drafts do not satisfy required relations: an active entity whose required relation points at a `draft` is reported incomplete, naming the active entity and the unsatisfied predicate | TS-INT-002-03 |
| STORY-INT-002 | AC-004 | Report orphans, dangling edges, stray files: lists orphan entities, dangling edges (target removed via `remove --force`), and stray files inside a type layout; exits non-zero | TS-INT-002-04 |
| STORY-INT-002 | AC-005 | Detect an edge cycle: a cycle on an acyclic predicate (`depends_on`) is reported with the participating ids; exits non-zero | TS-INT-002-05 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-INT-001 | REQ-INT001-01 | EARS-E | When validate runs, the system shall check each present declared field against the schema | TS-INT-001-U01, TS-INT-001-U07 |
| STORY-INT-001 | REQ-INT001-02 | EARS-W | If a relation does not resolve, then the system shall report a referential-integrity error | TS-INT-001-U02, TS-INT-001-U06 |
| STORY-INT-001 | REQ-INT001-03 | EARS-O | Where `--strict` is set, the system shall reject undeclared keys | TS-INT-001-U04 |
| STORY-INT-001 | REQ-INT001-04 | EARS-U | The system shall validate only the schema-declared subset, leaving extensions alone unless strict | TS-INT-001-U03 |
| STORY-INT-002 | REQ-INT002-01 | EARS-E | When check runs, the system shall verify relations resolve, required-completeness holds for active entities, and no cycles exist | TS-INT-002-U02, TS-INT-002-U08 |
| STORY-INT-002 | REQ-INT002-02 | EARS-W | If an active entity is missing a required field or relation, then the system shall report it as active-but-incomplete and exit non-zero | TS-INT-002-U03 |
| STORY-INT-002 | REQ-INT002-03 | EARS-S | While checking completeness, the system shall treat a `draft` target as not satisfying a required relation | TS-INT-002-U05 |
| STORY-INT-002 | REQ-INT002-04 | EARS-W | If an orphan, dangling edge, stray file, or edge cycle is found, then the system shall report it and exit non-zero | TS-INT-002-U01, TS-INT-002-U06, TS-INT-002-U07, TS-INT-002-U08 |
| STORY-INT-002 | REQ-INT002-05 | EARS-U | The system shall compute completeness from the schema over the active subgraph only, never inferring it from the `draft` flag | TS-INT-002-U02, TS-INT-002-U04 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-INT-001 | INT-001 | Validation covers the declared subset; undeclared keys pass unless `--strict` | Validation | TS-INT-001-U01, TS-INT-001-U03 |
| STORY-INT-001 | INT-002 | Referential integrity is part of validate: every relation must resolve | Validation | TS-INT-001-U02 |
| STORY-INT-001 | INT-003 | Files with no recognized frontmatter are skipped, not failed | Constraint | TS-INT-001-U05 |
| STORY-INT-002 | INT-004 | Required-completeness is enforced over active entities only, computed from the schema (never inferred from the `draft` flag) | Validation | TS-INT-002-U02, TS-INT-002-U04 |
| STORY-INT-002 | INT-005 | A `draft` does not satisfy another entity's required relation | Validation | TS-INT-002-U05 |
| STORY-INT-002 | INT-006 | `check` is the structural gap query: orphans (zero relations, in or out) and missing required relations | Validation | TS-INT-002-U01 |
| STORY-INT-002 | INT-011 | A stray file sits inside a type's layout path but does not parse as that type; markdown outside every type layout is a reference doc, skipped not flagged | Validation | TS-INT-002-U07 |
| STORY-INT-002 | INT-012 | An active entity missing a required field/relation is reported active-but-incomplete; completeness derived from the schema, not `draft` | Validation | TS-INT-002-U03 |
| STORY-INT-001, STORY-INT-002 | INT-SHARED-001 | Completeness is evaluated over the active subgraph only, from the schema; drafts excluded and a draft never satisfies a required relation | Constraint | TS-INT-002-U04, TS-INT-002-U05 |
| STORY-INT-001, STORY-INT-002 | INT-SHARED-003 | khub guarantees structural integrity, not semantic correctness | Constraint | TS-INT-001-01, TS-INT-002-01 |
| STORY-INT-002 | INT-SHARED-004 | Orphan (zero relations) is a core projection property; `check` gates on the same computation `status`/`query` surface | Constraint | TS-INT-002-04 |

---

## Entities

The loop reads the firm-ops graph and emits report records. The two report shapes below are what `validate` and `check` return; the typed entities they check (person, client, opportunity, project, meeting, fragment) come from the firm-ops preset and are seeded via `core.create`.

### Validation Result

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Entity | Reference | Yes | The entity validated |
| Is Well Formed | Yes/No | Yes | Declared fields legal (type, enum, pattern, cardinality) |
| Has Referential Integrity | Yes/No | Yes | Every relation resolves to an existing target |
| Errors | Collection | No | Field, reason pairs when failing (one per gap, all collected) |
| Strict | Yes/No | Yes | Whether undeclared keys were rejected |

### Check Report

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Relations Resolve | Yes/No | Yes | No dangling edge |
| Required Complete | Yes/No | Yes | Active entities meet required fields and relations, computed from the schema |
| Incomplete | Collection | No | Active-but-incomplete entities, each with its missing required fields and relations |
| Orphans | Collection | No | Entities with neither an inbound nor an outbound relation |
| Stray Files | Collection | No | Files inside a type's layout that are not valid entities of that type |
| Cycles | Collection | No | Edge cycles with participating ids |

### Check Outcome *(named enumeration)*

| Value | Description |
|-------|-------------|
| pass | Every relation resolves, required-completeness holds, no orphans, strays, or cycles; exit 0 |
| fail | One or more structural integrity rules broke; exit non-zero |

### Required Relation *(firm-ops examples — drives completeness)*

| Relation | On Type | Drives Completeness |
|----------|---------|---------------------|
| owner | most types | An active entity needs a resolvable, active owner |
| client | opportunity, project | An active engagement needs a client |
| engagement | meeting | A meeting needs its engagement (explicit edge) |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

`validate` and `check` are read-only gates — they transition no entity state. The completeness model below is the `check` verdict computed from the schema over the `draft` selector (the flag itself is set by `edit`/`add` in FS-002, not here):

```
active (draft: false) ─▶ check computes required-completeness from the schema
                         ├─ all required present, targets active   ─▶ pass
                         └─ a required field/relation missing, or a
                            target is a draft or removed            ─▶ FAIL

draft  (draft: true)  ─▶ exempt from required-completeness;
                         never satisfies another entity's required relation
```

| Subject | Condition | `check` outcome |
|---------|-----------|-----------------|
| active | every required field present; every required relation resolves to an active target | pass |
| active | a required field or relation is missing | fail — active-but-incomplete |
| active | a required relation resolves to a draft | fail — incomplete (draft does not satisfy) |
| active | a required relation target was removed | fail — dangling edge |
| draft | any | exempt; cannot satisfy another entity's required relation |

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). Each story maps to one `khub` subcommand; exit code reflects pass/fail.

### STORY-INT-001: Validate Entities (`khub validate`)

- **Library verb:** `core.validate(target, strict)` over the generated Pydantic model
- **Command:** `khub validate [target=all]`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| target | string (positional, default all) | No | Path/subset to validate; defaults to the whole workspace |
| --strict | flag | No | Close the schema: reject undeclared keys as errors |
| --fix | flag | No | v1 scope: date backfill only (the same `git log` logic as `backfill`, FS-005) |
| --format | enum(table, json) | No | Per-entity error list as table or JSON |

- **Reads:** real Markdown entities + the compiled schema / generated Pydantic model
- **Output (success):** `Validated N entities; 0 errors` — `N` counts only typed entities (reference markdown skipped). Exit 0.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | validation_error | Any malformed field, unresolved relation, or (under `--strict`) undeclared key — each reported with entity id, field, and reason; all collected, not just the first |

### STORY-INT-002: Check the Graph (`khub check`)

- **Library verb:** `core.check()` over the `networkx` projection (cycle detection, completeness)
- **Command:** `khub check`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| --format | enum(table, json) | No | Pass/fail report as table or JSON |

- **Reads:** the in-memory `networkx` projection (rebuilt from Markdown on demand), the active (`draft: false`) subgraph
- **Output (success):** `Graph check passed`. Exit 0.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | check_failed | One or more of: active-but-incomplete entity, required relation pointing at a draft, orphan, dangling edge, stray file, edge cycle — each reported with the offending ids/fields |

---

## Test Data

### person

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `noor`, `role partner`, reached by inbound `owner` edges | Clean tree, sound graph (TS-INT-001-01, TS-INT-002-01) |
| Draft | `newhire`, `draft: true` | Draft target that does not satisfy a required `owner` relation (TS-INT-002-03) |
| Boundary | `orphan-person` nothing owns, no outbound edge | Orphan (zero in/out) detection (TS-INT-002-04) |

### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech`, reached by inbound `client` edges | Clean tree, sound graph (TS-INT-001-01, TS-INT-002-01) |
| Invalid | `initech` carrying undeclared `vibe: high` | Strict rejects, non-strict passes (TS-INT-001-03) |
| Removed | `initech` dropped via `remove --force`, leaving `initech-pov`'s `client` dangling | Dangling-edge detection (TS-INT-002-04) |

### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-deal`, `client → initech`, `owner → noor`, `stage prospect` | Clean tree, sound graph (TS-INT-001-01, TS-INT-002-01) |
| Invalid | `bad-stage` with `stage: vibing` (off-enum) | Per-entity error reporting (TS-INT-001-02) |
| Incomplete | `initech-deal` whose required `owner → person/newhire` (draft) | Draft does not satisfy a required relation (TS-INT-002-03) |

### project

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-pov`, `client → initech`, `owner → noor`, `active: true` | Clean tree, sound graph (TS-INT-001-01, TS-INT-002-01) |
| Invalid | `ghost-owner` whose `owner → person/nobody` does not resolve | Unresolved-relation error (TS-INT-001-02) |
| Incomplete | `orphaned-pov` active (`add` default) but missing required `owner` | Active-but-incomplete from the schema (TS-INT-002-02) |

### meeting

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `kickoff`, `engagement → initech-pov`, `call_type client`, `source recording` | Clean tree, sound graph (explicit engagement edge) (TS-INT-001-01, TS-INT-002-01) |

### fragment

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Boundary | `a → b → c → a` via the universal `depends_on` edge | Edge cycle on an acyclic predicate (TS-INT-002-05) |

### reference doc (untyped)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Skipped | `identity/mission.md`, no frontmatter, outside every type layout | Skipped not errored; not counted in `N` (TS-INT-001-04) |
| Stray | `clients/notes.md`, malformed, inside the `client` layout | Stray file flagged by `check` — distinct from the skipped reference doc (TS-INT-002-04) |

---

## Implementation Notes

- **Two verbs, two engines.** `validate` runs per-entity against the generated Pydantic model (field type/enum/pattern/cardinality + referential resolution); `check` runs graph-wide against the `networkx` projection (orphans, completeness, dangling edges, cycles). `validate` runs first and `check` assumes entities individually validate — but they ship together because they are the single v1 gate.
- **Collect every error (INT-001 AC-002 / REQ-INT001-02).** `validate` must continue past the first failure and accumulate all errors before returning. A regression that stops at the first error passes a single-error fixture but fails the two-entity scenario — test with two distinct broken entities so the all-errors behavior is observable.
- **Completeness comes from the schema, never the flag (INT-004 / INT-012 / REQ-INT002-05).** The load-bearing invariant: a regression could silently read the `draft` flag and pass the assertion for the wrong reason. Assert both directions — an active entity missing a required field FAILS even when published, and a `draft` entity with the same gap is EXEMPT. The flag and the verdict must move independently.
- **A draft never satisfies a required relation (INT-005 / REQ-INT002-03).** An active entity whose required relation points at a `draft` target is incomplete — the draft is unpublished, so it cannot satisfy. Name the active entity and the unsatisfied predicate, not the draft.
- **Stray vs skipped (INT-011).** A stray file sits inside a type's layout path but does not parse as that type → flagged. Markdown outside every type layout is a reference doc → skipped, not flagged, not counted. The two fixtures (`clients/notes.md` inside the layout vs `identity/mission.md` outside) must give opposite verdicts.
- **Dangling edge via `remove --force` (INT-002 AC-004).** WPK-002-3 leaves now-dangling edges for `check` to surface — never silently repaired. The dangling-edge fixture drops `client/initech` via `remove --force`, leaving `project/initech-pov`'s `client` edge pointing at nothing.
- **Cycle safety (INT-002 AC-005 / REQ-INT002-04).** Cycle detection reuses the same visited-set discipline as `impact`/`history` (WPK-003-3). A regression loops instead of failing fast and hangs CI — run the cycle scenario under a per-test timeout and assert the reported cycle equals the participating id set exactly.
- **Structural, not semantic (INT-SHARED-003).** A false-but-legal write (structurally valid, semantically wrong) passes both `validate` and `check` by design — git revert, not a gate, is the backstop. Keep one explicit scenario proving the legal-but-false entity passes; do not "tighten" it into a failure.
- **YAML I/O uses `ruamel.yaml`** per the repo rule — never PyYAML in `src/` or `tests/`.
- **Located messages are brittle.** `Validated N entities; 0 errors`, `Graph check passed`, and the per-entity error strings (id/field/reason) are asserted verbatim in the spec. Assert on the variable fields (entity id, field name, count) plus a message substring, keeping the spec's exact text as the canonical example.

---

## Done Criteria

- [ ] All 9 acceptance criteria pass (INT-001 AC-001 through AC-004; INT-002 AC-001 through AC-005)
- [ ] All 9 EARS requirements tested (REQ-INT001-01 through -04; REQ-INT002-01 through -05)
- [ ] All 12 business rules enforced (INT-001 through INT-006, INT-011, INT-012), plus INT-SHARED-001/003/004 where they apply
- [ ] All 24 test scenarios pass (TS-INT-001-01 through -04 + U01–U07; TS-INT-002-01 through -05 + U01–U08)
- [ ] `validate` collects and reports every error (two-entity fixture), not just the first; exits non-zero
- [ ] Completeness moves with the schema, not the `draft` flag — active-incomplete FAILS while a draft with the same gap is EXEMPT
- [ ] A `draft` target leaves an active entity's required relation unsatisfied, naming the active entity and predicate
- [ ] Stray file (inside a layout) is flagged while a reference doc (outside every layout) is skipped and not counted
- [ ] Dangling edge after `remove --force` is surfaced, not silently repaired
- [ ] Cycle scenario terminates under a per-test timeout and reports the participating ids exactly
- [ ] A false-but-legal write passes both `validate` and `check` (structural integrity, not semantic truth)
- [ ] `validate` and `check` run clean on a seeded firm-ops snapshot (E2E, functional cutover)
- [ ] No regressions in existing tests
