---
id: WPK-002-2
name: Edit and Relate Entities
feature-spec: plan/specs/FS-002-authoring.md
test-spec: plan/tests/TS-002-authoring.md
stories: [STORY-ENT-003, STORY-ENT-004]
test-scenarios: [TS-ENT-003-01, TS-ENT-003-02, TS-ENT-003-03, TS-ENT-003-04, TS-ENT-003-U01, TS-ENT-003-U02, TS-ENT-003-U03, TS-ENT-003-U04, TS-ENT-003-U05, TS-ENT-004-01, TS-ENT-004-02, TS-ENT-004-03, TS-ENT-004-04, TS-ENT-004-05, TS-ENT-004-U01, TS-ENT-004-U02, TS-ENT-004-U03, TS-ENT-004-U04, TS-ENT-004-U05]
depends-on: [WPK-002-1]
updated: 2026-06-24
---

# Edit and Relate Entities

## Objective

Delivers the mutation verbs over an existing entity — `khub edit <id>` (`core.update`) and `khub link`/`khub unlink` (`core.link`/`core.unlink`) — with the compiled schema and git as the only gates. Edits round-trip frontmatter to a minimal git diff and bump `updated`; the `draft` flag is set by hand via `edit <id> draft true|false` and a field edit never touches it. Link and unlink add and remove schema-checked relations — predicate legality, target resolution, and cardinality enforced — storing forward edges single-sided on the source while leaving inverse edges to derive, never touching `draft`.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-ENT-003 | AC-001 | Edit a field: validate the new value against the field's enum (opportunity stages), write preserving key order and comments (minimal diff), bump `updated` to today, display `Updated opportunity 'initech-deal'` | TS-ENT-003-01 |
| STORY-ENT-003 | AC-002 | Manually toggle the draft flag: `edit <id> draft false` sets `draft: false` and `edit <id> draft true` sets `draft: true`; a field edit never changes `draft`, and there is no auto-promote or auto-degrade | TS-ENT-003-02 |
| STORY-ENT-003 | AC-003 | Invalid enum value: reject the edit, display `'banana' is not a valid stage (prospect, proposal-sent, won, signed, lost)`, leave the file unchanged | TS-ENT-003-03 |
| STORY-ENT-003 | AC-004 | Unknown field under `--strict`: reject the edit under `--strict`; without `--strict`, accept and preserve the extension | TS-ENT-003-04 |
| STORY-ENT-004 | AC-001 | Add a relation: check the predicate is declared for the source type, resolve the target by its schema-known type, enforce cardinality (single vs many), store the edge single-sided on the source, display `Linked initech-pov --partner--> northwind` | TS-ENT-004-01 |
| STORY-ENT-004 | AC-002 | Illegal predicate: reject the link, display `Predicate 'engagement' is not legal for type 'project'` | TS-ENT-004-02 |
| STORY-ENT-004 | AC-003 | Unresolvable target: reject the link (referential integrity), display `No person 'ghost' to satisfy predicate 'owner'` | TS-ENT-004-03 |
| STORY-ENT-004 | AC-004 | Unlink a relation: remove the edge from the source, leave any derived inverse to recompute, display `Unlinked initech-pov --partner--> northwind` | TS-ENT-004-04 |
| STORY-ENT-004 | AC-005 | Cardinality violation: reject or replace per the single-valued rule, display `Predicate 'owner' is single-valued; use edit to replace` | TS-ENT-004-05 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-ENT-003 | REQ-ENT003-01 | EARS-E | When a field is edited, the system shall re-validate, bump `updated`, and write a minimal diff | TS-ENT-003-U03 |
| STORY-ENT-003 | REQ-ENT003-02 | EARS-E | When `edit <id> draft true|false` runs, the system shall set the `draft` flag to that value and never derive it from completeness | TS-ENT-003-U04 |
| STORY-ENT-003 | REQ-ENT003-03 | EARS-W | If the new value violates the field's type or enum, then the system shall reject the edit and leave the file unchanged | TS-ENT-003-U01 |
| STORY-ENT-003 | REQ-ENT003-04 | EARS-O | Where `--strict` is set, the system shall reject edits to undeclared fields | TS-ENT-003-U05 |
| STORY-ENT-004 | REQ-ENT004-01 | EARS-E | When link runs, the system shall verify the predicate is legal, the target resolves, and cardinality holds | TS-ENT-004-U01 |
| STORY-ENT-004 | REQ-ENT004-02 | EARS-U | The system shall store forward edges single-sided on the source entity | TS-ENT-004-U03 |
| STORY-ENT-004 | REQ-ENT004-03 | EARS-W | If the predicate is illegal for the source type, then the system shall reject the link | TS-ENT-004-U01 |
| STORY-ENT-004 | REQ-ENT004-04 | EARS-W | If the target does not resolve, then the system shall reject the link | TS-ENT-004-U04 |
| STORY-ENT-004 | REQ-ENT004-05 | EARS-E | When unlink runs, the system shall remove the edge and let derived inverses recompute | TS-ENT-004-U05 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-ENT-003 | ENT-007 | Edits round-trip frontmatter, preserving key order and comments | Constraint | TS-ENT-003-U03 |
| STORY-ENT-003 | ENT-008 | `updated` is bumped on every successful edit | Automation | TS-ENT-003-U02 |
| STORY-ENT-003 | ENT-009 | `draft` is toggled only by hand (`edit <id> draft true|false`); a field edit never touches it, and there is no auto-promote or auto-degrade | Validation | TS-ENT-003-U04 |
| STORY-ENT-004 | ENT-010 | A predicate must be schema-legal for the source type | Validation | TS-ENT-004-U01 |
| STORY-ENT-004 | ENT-011 | Forward edges are stored single-sided; inverses are derived, never stored | Constraint | TS-ENT-004-U03 |
| STORY-ENT-004 | ENT-012 | Cardinality is enforced from the schema (single versus many) | Validation | TS-ENT-004-U02 |
| STORY-ENT-003 | ENT-004 | Undeclared fields are preserved unless `--strict` closes the schema | Validation | TS-ENT-003-U05 |
| STORY-ENT-004 | ENT-SHARED-001 | Referential integrity hard-fails on write; every relation must resolve | Validation | TS-ENT-004-03 |
| STORY-ENT-003 | ENT-SHARED-002 | An incomplete entity saves active by default (`--draft` to mark unpublished); capture is never blocked | Validation | TS-ENT-003-U04 |
| STORY-ENT-004 | ENT-SHARED-003 | Forward edges store single-sided; inverse edges are derived | Constraint | TS-ENT-004-U03 |
| STORY-ENT-003 | ENT-SHARED-004 | Structural integrity is guaranteed; semantic truth is not (a schema-legal but false write validates) | Constraint | TS-ENT-003-01 |

---

## Entities

### opportunity

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `opportunity` |
| Stage | Status | Yes | Pipeline stage enum — the edited field on `initech-deal` |
| Client | Reference | Yes | Edge → client |
| Owner | Reference | Yes | Edge → person |
| Source | Type | No | Lead source enum |
| Partner | Reference | No | Edge → partnership |
| Created | Date | Yes | Mint date |
| Updated | Date | Yes | Last edit date |

> Folder layout: `opportunities/{slug}/_index.md`. The enum-edit worked example (`initech-deal`) and the manual draft toggle (`some-opp`) are both opportunities.

### project

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `project` |
| Active | Yes/No | No | Engagement ongoing? (default true) — replaces a per-stage lifecycle; project carries no stage enum |
| Source | Type | No | Lead source enum |
| Client | Reference | Yes | Edge → client |
| Owner | Reference | Yes | Edge → person (single-valued) |
| Team | Reference | No | Edge → person (many) |
| Origin Opportunity | Reference | No | Edge → opportunity |
| Partner | Reference | No | Edge → partnership |
| Created | Date | Yes | Mint date |
| Updated | Date | Yes | Last edit date |

> Folder layout: `projects/{slug}/_index.md`. The link and cardinality examples use `initech-pov`.

### person

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `person` |
| Name | Text | Yes | Full name |
| Role | Status | Yes | Role enum |
| Department | Type | No | Department enum |
| Created | Date | Yes | Mint date |

### partnership

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `partnership` |
| Partner | Text | Yes | Partner name = slug source (the `partner` edge target, e.g. `northwind`) |
| Created | Date | Yes | Mint date |

### Predicates exercised

| Predicate | From | To | Cardinality | Description |
|-----------|------|----|-------------|-------------|
| partner | opportunity \| project | partnership | single | Legal for `project`; the link happy path (`northwind`) |
| owner | (most types) | person | single | Single-valued; cardinality refusal directs to `edit` to replace |
| engagement | meeting | opportunity \| project \| partnership | single | Meeting-only — **not** legal for `project`: the illegal-predicate case |

### Opportunity Stage *(named enumeration)*

> The CRM "Sales" pipeline stages — the edited enum in STORY-ENT-003.

| Value | Description |
|-------|-------------|
| prospect | Early-stage lead in the pipeline |
| proposal-sent | Proposal issued |
| won | Deal won |
| signed | Contract signed |
| lost | Did not convert |

### Lifecycle Flag *(`draft` boolean)*

| Value | Description |
|-------|-------------|
| `draft: true` | Manually marked unpublished via `edit <id> draft true`; a draft never satisfies another entity's required relation |
| `draft: false` | Active (default); completeness is decoupled from draft and enforced by `check` (FS-004) |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

```
┌─────────┐
│  draft  │  (manually marked unpublished)
└────┬────┘
     │ edit <id> draft false
     ▼
┌─────────┐
│ active  │  (draft: false)
└─────────┘
   │ edit <id> draft true
   └──────► draft
```

| From | Action | To | Conditions |
|------|--------|----|------------|
| draft | `edit <id> draft false` | active | manual toggle; `draft` set to `false` |
| active | `edit <id> draft true` | draft | manual toggle; `draft` set to `true` |
| active | `edit` a field to a valid value | active | re-validates, bumps `updated`, minimal diff; `draft` untouched |

> The create-time `--draft` flag lives in WPK-002-1; this work package owns manual draft toggling via edit. A field edit never touches `draft`, and link/unlink never touch it.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). Each story maps to one `khub` subcommand family.

### STORY-ENT-003: Edit an Entity (`khub edit`)

- **Library verb:** `core.update(id, changes)`
- **Command:** `khub edit <id> <field> <value>` (or `--<field> <value>`)
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| id | string (positional) | Yes | Entity to edit (bare slug or `type/slug`) |
| field value | positional pair | No | `<field> <value>` (e.g. `stage proposal-sent`); or `--<field> <value>` |
| --body-file | path (`-` for stdin) | No | Replace the body from a file or stdin |
| --strict | flag | No | Reject edits to undeclared fields |
| --format | enum(text, json) | No | `text` confirmation (default); `json` emits the updated record |

- **Writes:** the changed key(s) plus the `updated` line, via round-trip YAML preserving key order and comments (minimal diff).
- **Output (success):** `Updated <type> '<slug>'`; `--format json` emits the updated record.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | enum_violation | Out-of-enum value — `'banana' is not a valid stage (prospect, proposal-sent, won, signed, lost)`; file left unchanged |
| non-zero | strict_unknown_field | Undeclared field under `--strict` |

### STORY-ENT-004: Link and Unlink Relations (`khub link` / `khub unlink`)

- **Library verb:** `core.link(id, predicate, target)` / `core.unlink(id, predicate, target)`
- **Command:** `khub link <id> <predicate> <target>` · `khub unlink <id> <predicate> <target>`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| id | string (positional) | Yes | Source entity |
| predicate | string (positional) | Yes | Relation predicate (must be schema-legal for the source type) |
| target | string (positional) | Yes | Target entity (resolved by its schema-known type) |

- **Writes:** the forward edge stored single-sided on the source entity (`link`); the edge removed from the source (`unlink`). Inverses are never stored.
- **Output (success):** `Linked <id> --<predicate>--> <target>` / `Unlinked <id> --<predicate>--> <target>`.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | illegal_predicate | Predicate not declared for the source type — `Predicate 'engagement' is not legal for type 'project'` |
| non-zero | referential_integrity | Target does not resolve — `No person 'ghost' to satisfy predicate 'owner'` |
| non-zero | cardinality_violation | Single-valued predicate already set — `Predicate 'owner' is single-valued; use edit to replace` |

---

## Test Data

### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-deal` with a `stage` enum (`prospect, proposal-sent, won, signed, lost`) and an `updated` date earlier than today | Edit a field (TS-ENT-003-01) |
| Invalid | `initech-deal` `stage` set to `banana` | Out-of-enum rejection, file unchanged (TS-ENT-003-03) |
| Boundary | `initech-deal` with undeclared field `vibe`, with/without `--strict` | Strict-mode edit rejection vs free extension (TS-ENT-003-04) |
| Boundary | `some-opp` saved `draft: true` | Manual draft toggle via `edit <id> draft false` (TS-ENT-003-02) |

### project

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-pov` (folder layout) with required `client`/`owner` | Link a relation (TS-ENT-004-01) |
| Boundary | single-valued `owner` already set to `noor` | Cardinality refusal on a second `owner` (TS-ENT-004-05) |

### person

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `noor` resolvable | Resolvable `owner` target for the link cases |
| Valid | `dana` resolvable | Second `owner` candidate for the cardinality case (TS-ENT-004-05) |
| Missing | `ghost` resolves to nothing | Unresolvable-target rejection on link (TS-ENT-004-03) |

### partnership

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `northwind` resolvable; `partner` legal for `project` | Link happy path and unlink (TS-ENT-004-01, TS-ENT-004-04) |

---

## Implementation Notes

- Both commands are thin adapters over a core verb (`core.update`, `core.link`, `core.unlink`); the compiled schema and git are the only gates. Predicate legality, cardinality, and enums are read from the schema, never hardcoded.
- Minimal-diff writes go through round-trip YAML (ruamel.yaml): preserve key order and comments. Assert the **raw file-text diff** is confined to the changed key and the `updated` line — a serializer swap can pass a values-only check while wrecking git-diff quality.
- `updated` is bumped on every successful edit; a rejected edit (invalid enum, strict violation) leaves the file byte-unchanged.
- `draft` is a manual flag: only `edit <id> draft true|false` changes it. A field edit never touches `draft`, and link/unlink never touch it — there is no auto-promote or auto-degrade. Completeness is decoupled from draft and enforced by `check` (FS-004).
- `link` order: predicate legality (declared for the source type) → target resolves by its schema-known type (referential integrity) → cardinality (single vs many). Forward edges are stored single-sided on the source; inverses are never stored. `unlink` removes the forward edge and lets the derived inverse recompute.
- Single-valued cardinality (AC-005) leaves the reject-vs-replace choice open in the spec. Assert the invariant — no second value silently lands — plus the located message; confirm the chosen branch before pinning a single behavior.
- Located error messages are brittle to wording: assert on the variable fields (slug, field, predicate, type) plus a message substring, keeping the spec's exact text as the canonical example.

---

## Done Criteria

- [ ] All 9 acceptance criteria pass (ENT-003 AC-001 through AC-004; ENT-004 AC-001 through AC-005)
- [ ] All 9 EARS requirements tested (REQ-ENT003-01 through -04; REQ-ENT004-01 through -05)
- [ ] All 6 business rules enforced (ENT-007 through ENT-012), plus ENT-004 and shared ENT-SHARED-001..004 where they apply
- [ ] All 19 test scenarios pass (TS-ENT-003-01 through -04 + U01–U05; TS-ENT-004-01 through -05 + U01–U05)
- [ ] Minimal-diff edit confines the raw file-text diff to the changed key and the `updated` line
- [ ] `edit <id> draft false` flips a draft to active and `edit <id> draft true` flips it back; a field edit leaves `draft` untouched (E2E)
- [ ] `link` stores the edge single-sided; the derived inverse shows on the target yet is absent from its stored frontmatter (E2E)
- [ ] No regressions in existing tests
