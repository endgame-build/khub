---
id: WPK-002-3
name: Remove Entities
feature-spec: plan/specs/FS-002-authoring.md
test-spec: plan/tests/TS-002-authoring.md
stories: [STORY-ENT-005]
test-scenarios: [TS-ENT-005-01, TS-ENT-005-02, TS-ENT-005-03, TS-ENT-005-04, TS-ENT-005-U01, TS-ENT-005-U02, TS-ENT-005-U03, TS-ENT-005-U04, TS-ENT-005-U05]
depends-on: [WPK-002-1]
updated: 2026-06-24
---

# Remove Entities

## Objective

Delivers `khub remove <id>` (`core.delete`): delete an entity guarded by inbound edges so removal never silently breaks referential integrity across the graph. An unreferenced entity (file, or folder for a folder-layout type) is deleted outright; a referenced one is refused with an inbound-edge report unless `--force` is passed, in which case the entity is deleted and the now-dangling edges are left for `check` (FS-004) to surface — never silently repaired.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-ENT-005 | AC-001 | Remove an unreferenced entity: delete the file (or folder for a folder-layout type), display `Removed fragment 'old-fragment'` | TS-ENT-005-01 |
| STORY-ENT-005 | AC-002 | Refuse when inbound edges resolve: refuse the delete, list the inbound edges (which entities and predicates point at it), display `Refusing to remove client 'initech': 3 inbound edges resolve to it. Pass --force to override` | TS-ENT-005-02 |
| STORY-ENT-005 | AC-003 | Force override: delete the entity, leave the now-dangling inbound edges for `check` to surface | TS-ENT-005-03 |
| STORY-ENT-005 | AC-004 | Unknown id: return a lookup error, display `No entity 'ghost' found` | TS-ENT-005-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-ENT-005 | REQ-ENT005-01 | EARS-E | When remove runs with no inbound edges, the system shall delete the entity | TS-ENT-005-U03 |
| STORY-ENT-005 | REQ-ENT005-02 | EARS-W | If an inbound edge resolves to the target, then the system shall refuse to delete unless `--force` is passed | TS-ENT-005-U02 |
| STORY-ENT-005 | REQ-ENT005-03 | EARS-E | When `--force` is passed, the system shall delete and leave dangling edges for `check` | TS-ENT-005-U04 |
| STORY-ENT-005 | REQ-ENT005-04 | EARS-W | If the id does not resolve, then the system shall return a lookup error | TS-ENT-005-U05 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-ENT-005 | ENT-013 | Removal is refused while any inbound edge resolves, unless forced | Validation | TS-ENT-005-U02 |
| STORY-ENT-005 | ENT-014 | A forced removal surfaces resulting breakage through `check`, not a silent fix | Constraint | TS-ENT-005-U04 |

---

## Entities

### client

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `client` |
| Name | Text | Yes | Organization name |
| Created | Date | Yes | Mint date |
| Updated | Date | Yes | Last edit date |

> The referenced removal target — `client/initech` with three resolving inbound edges (opportunities/projects naming it as `client`).

### fragment

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `fragment` |
| Created | Date | Yes | Mint date |
| Updated | Date | Yes | Last edit date |

> A partner's atomic note. The unreferenced removal target — `fragment/old-fragment` with no inbound edges.

### Storage Layout *(named enumeration)*

| Value | Description |
|-------|-------------|
| file | One entity per file (e.g. `fragments/{slug}.md`) — remove deletes the single file |
| folder | A folder per entity with `_index.md` (e.g. `projects/{slug}/_index.md`) — remove deletes the folder and its `_index.md` |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| Remove an entity with inbound edges | Prevent deletion (unless `--force`) | Referential integrity must not break silently |
| Force-remove a referenced entity | Delete and leave dangling edges | `check` surfaces the breakage; git revert is the backstop |
| Remove a folder-layout entity | Delete the folder and its `_index.md` | The folder is the entity's storage unit |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

Not applicable — `khub remove` is a one-shot deletion guarded by inbound edges. STORY-ENT-005 has no entity state machine: the entity is either present, refused-while-referenced, or deleted. The draft/active lifecycle is owned by WPK-002-1 and WPK-002-2.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). The story maps to one `khub` subcommand.

### STORY-ENT-005: Remove an Entity (`khub remove`)

- **Library verb:** `core.delete(id, force)`
- **Command:** `khub remove <id>`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| id | string (positional) | Yes | Entity to remove (bare slug or `type/slug`) |
| --force | flag | No | Delete despite resolving inbound edges, leaving them to dangle |

- **Writes:** deletes the entity file (or the folder and its `_index.md` for a folder-layout type). No write on a refusal or lookup error.
- **Output (success):** `Removed <type> '<slug>'`; or an inbound-edge report on refusal.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | inbound_edge_refusal | Inbound edges resolve, no `--force` — `Refusing to remove client 'initech': 3 inbound edges resolve to it. Pass --force to override`; entity left in place |
| non-zero | lookup_error | Id resolves to nothing — `No entity 'ghost' found` |

---

## Test Data

### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Boundary | `initech` with three resolving inbound edges | Inbound-edge refusal and `--force` override (TS-ENT-005-02, TS-ENT-005-03) |

### fragment

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `old-fragment` with no inbound edges | Clean removal (TS-ENT-005-01) |

### Missing lookups

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Missing | id `ghost` resolves to nothing | Lookup error on `remove` (TS-ENT-005-04) |

---

## Implementation Notes

- Thin adapter over `core.delete(id, force)`; structural integrity is guarded, not silently repaired.
- Inbound-edge detection scans every entity whose stored forward edge resolves to the target (which entity, which predicate). Refuse while any inbound edge resolves, unless `--force` is passed.
- Folder-layout types delete the folder and its `_index.md`; flat types delete the single file. Resolve the layout from the type's schema, not a path heuristic.
- `--force` deletes and leaves the now-dangling inbound edges for `check` to surface — never a silent fix; git revert is the backstop.
- **Cross-feature dependency (FS-004 Integrity Loop):** the force-path E2E (TS-ENT-005-03) asserts a real `khub check` run reports the dangling edges. The removal itself only deletes and leaves the edges; the assertion needs FS-004's `check` available. FS-004 is not yet decomposed into work packages — sequence this E2E after `check` lands, or stub the assertion to the post-delete on-disk edge state until then.

---

## Done Criteria

- [ ] All 4 acceptance criteria pass (AC-001 through AC-004)
- [ ] All 4 EARS requirements tested (REQ-ENT005-01 through -04)
- [ ] All 2 business rules enforced (ENT-013, ENT-014)
- [ ] All 9 test scenarios pass (TS-ENT-005-01 through -04 + U01–U05)
- [ ] Folder-layout removal deletes the folder and its `_index.md`; flat removal deletes the single file
- [ ] Force-removal leaves dangling inbound edges that `check` (FS-004) surfaces — verified by a real `check` run on the resulting tree
- [ ] No regressions in existing tests
