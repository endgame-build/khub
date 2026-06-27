---
id: TS-002
name: Authoring Test Spec
spec: plan/specs/FS-002-authoring.md
updated: 2026-06-27
---

# Authoring — Test Spec

## Summary

Tests the write surface over the core library — `add`, `get`, `edit`, `link`/`unlink`, and `remove` as thin adapters over `core.create`, `get`, `update`, `link`/`unlink`, and `delete`, with the compiled schema and git as the only gates. Coverage runs from slug minting and field validation, through the two write invariants that define the feature — referential integrity hard-fails on write while a missing required field still saves active, since capture is never blocked and `draft` is a manual flag — to the read-time derivation of inverse edges and the inbound-edge guard on removal.

**Feature Spec:** FS-002: Authoring
**Stories Covered:** 5
**Test Scenarios:** 23 (plus 28 unit tests)

---

## Test Strategy

### Approach

**Philosophy:** Hybrid — every command is a thin adapter over a core verb, so the discrete rules (slug minting, enum/pattern validation, the manual `draft` flag, predicate legality, cardinality, inbound-edge detection) are unit-testable against the compiled schema in isolation, but the guarantees that matter — referential integrity hard-fails before any byte is written, an incomplete entity still saves active and a `draft` flag is set only by hand, an edit round-trips frontmatter to a minimal diff, an inverse edge is computed at read time and never stored — only hold when a real entity is written to and read back from a real filesystem inside a real workspace. Unit tests pin the rules; integration tests prove the command surface, the hard-fail/no-file contract, and JSON parity; E2E proves the round-trips that cross verbs (`add` → `get`, `edit <id> draft false` publishes a draft, `link` → derived inverse, `remove --force` → `check` surfaces the break).

**Test Pyramid:**

| Level | Share | Scope |
|-------|-------|-------|
| Unit | ~60% | Slug minting + collision suffix, field/enum/pattern validation, manual `draft` flag (default false, `--draft` sets true), referential-integrity check, strict-mode field filter, layout resolution, id + ambiguity resolution, inverse-edge derivation, round-trip write, `updated` bump, predicate legality, cardinality, inbound-edge detection |
| Integration | ~30% | Command surface for all five verbs, hard-fail-writes-no-file contract, incomplete `add` saves active, `--draft` flag, `--strict` rejection, illegal-predicate/unresolvable-target rejection, cardinality refusal, inbound-edge refusal, `--force` override, `--format json` shape and parity, located error messages |
| E2E | ~10% | `add opportunity` → `get` round-trips frontmatter and body; a field edit leaves `draft` untouched, then `edit <id> draft false` publishes; `link` then a derived inverse shows on the target; `remove` a referenced client is refused, then `--force` plus `check` reports the dangling edge |

### Coverage Targets

| Target | Goal |
|--------|------|
| Acceptance criteria | 100% of ACs from FS-002 (23 ACs across 5 stories) |
| EARS requirements | 100% of REQ-ENT* requirements (23) |
| Business rules | 100% of rule enforcement (ENT-001..014, ENT-SHARED-001..004) |
| Edge cases | Hard-fail writes no file, missing-required saves active (draft is manual), within-type slug collision, ambiguous bare slug, derived inverse never stored, single-valued cardinality, folder-layout deletion, forced removal leaving dangling edges |

---

## Test Scenarios

---

### STORY-ENT-001: Create an Entity

**Spec:** As an Agent or Operator, I want to mint a new entity of a schema type with field values, So that typed context is captured as one Markdown file in git, validated against the schema

#### TS-ENT-001-01: Create a Well-Formed Active Entity

**Validates:** AC-001
**Level:** E2E

**Given** the agent supplies a known type with all required fields and resolvable relations
**When** they run `khub add opportunity --client initech --owner noor --stage prospect`
**Then:**
- [ ] each declared field is validated against its type, enum, and pattern
- [ ] `client` and `owner` resolve to existing targets
- [ ] a bare slug is minted as the id, unique within the `opportunity` type
- [ ] one file is written at the type's layout path (`opportunities/{slug}/_index.md`)
- [ ] `created` and `updated` are set to today
- [ ] `draft` is `false` by default (the flag is not passed)
- [ ] the new id and the file path are printed
- [ ] the system displays `Created opportunity '{slug}' (active)`

**Test Data:** firm-ops workspace with existing `client/initech` and `person/noor`; a valid `opportunity` type declaring a `stage` enum and required `client`/`owner` relations

#### TS-ENT-001-02: Missing Required Field Saves Active; --draft Sets the Flag

**Validates:** AC-002
**Level:** Integration

**Given** the agent omits a required field or relation
**When** they run `khub add opportunity --stage prospect`
**Then:**
- [ ] the entity is saved with `draft: false` (active by default; a missing required field does not auto-set draft)
- [ ] the supplied fields (`stage`) are preserved
- [ ] capture is not blocked (the file is written)
- [ ] the system displays `Created opportunity '{slug}' (active)`
- [ ] re-running the same call with `--draft` instead saves `draft: true` and displays `Created opportunity '{slug}' (draft)`

**Test Data:** firm-ops workspace; `add` invocation omitting the required `client` and `owner` relations, run once without and once with `--draft`

#### TS-ENT-001-03: Relation to a Non-Existent Target Is Rejected

**Validates:** AC-003
**Level:** Integration

**Given** the agent names a relation target that does not resolve
**When** they run `khub add opportunity --client ghost-co --owner noor --stage prospect`
**Then:**
- [ ] the write is rejected (referential integrity hard-fail)
- [ ] the system displays `No client 'ghost-co' to satisfy relation 'client'`
- [ ] no file is written

**Test Data:** firm-ops workspace where `client/ghost-co` does not exist but `person/noor` does

#### TS-ENT-001-04: Unknown Field Under Strict

**Validates:** AC-004
**Level:** Integration

**Given** the agent passes a field the schema does not declare
**When** they run `khub add opportunity --client initech --owner noor --stage prospect --vibe high --strict`
**Then:**
- [ ] the write is rejected under `--strict`
- [ ] the system displays `Unknown field 'vibe' rejected under --strict`
- [ ] without `--strict`, the same call accepts and preserves `vibe` as a free extension

**Test Data:** firm-ops workspace with resolvable `initech`/`noor`; the undeclared field `vibe`; runs with and without `--strict`

#### TS-ENT-001-05: Create a Meeting With an Explicit Engagement Edge

**Validates:** AC-005
**Level:** Integration

**Given** the agent creates a meeting for a parent engagement
**When** they run `khub add meeting --engagement initech-pov --call-type client --source recording --date 2026-06-19`
**Then:**
- [ ] the file is written flat at `meetings/{slug}.md`
- [ ] a bare slug is minted as the id
- [ ] `engagement` is stored as an explicit edge resolving to its union target (opportunity | project | partnership)

**Test Data:** firm-ops workspace with an existing `project/initech-pov` (a legal `engagement` union target); a `meeting` type with the flat layout

#### TS-ENT-001-06: Explicit Id and Collision Suffix

**Validates:** AC-006
**Level:** Integration

**Given** the agent supplies an explicit slug
**When** they run `khub add client --id acme --name "Acme Corp"`
**Then:**
- [ ] `acme` is used as the id when unique within the `client` type
- [ ] a deterministic suffix is appended on a within-type collision (e.g. `acme-2`)
- [ ] when `--id` is omitted, a slug is minted from the name, else the type

**Test Data:** firm-ops workspace; one run into an empty `client` type, a second run with `client/acme` already present to force the `acme-2` suffix

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-ENT-001-U01 | Slug minter | Mints a bare slug from `--id`, else the name or type, unique within the type | REQ-ENT001-01, ENT-001 |
| TS-ENT-001-U02 | Field validator | Validates each declared field against its type, enum, and pattern | REQ-ENT001-01 |
| TS-ENT-001-U03 | Draft flag | Defaults `draft` to false even when a required field/relation is missing (saved active); `--draft` sets it true | REQ-ENT001-01, REQ-ENT001-02, ENT-003 |
| TS-ENT-001-U04 | Referential-integrity checker | Hard-fails the write and emits no file when a relation target does not resolve | REQ-ENT001-03, ENT-002 |
| TS-ENT-001-U05 | Strict-mode filter | Rejects an undeclared field under `--strict`; preserves it as a free extension otherwise | REQ-ENT001-04, ENT-004 |
| TS-ENT-001-U06 | Collision suffixer | Appends a deterministic suffix (`acme-2`) on a within-type `(type, slug)` collision | REQ-ENT001-06, ENT-001 |
| TS-ENT-001-U07 | Layout resolver | Resolves the write path from the type's layout (`opportunities/{slug}/_index.md` folder vs `meetings/{slug}.md` flat) and stores `engagement` as an explicit edge | REQ-ENT001-05 |

---

### STORY-ENT-002: Read an Entity

**Spec:** As an Agent or Operator, I want to print an entity's frontmatter and body, optionally with derived edges, So that I can act on typed context, including the inverse edges the schema computes

#### TS-ENT-002-01: Print an Entity

**Validates:** AC-001
**Level:** E2E

**Given** an entity exists at the given id
**When** the agent runs `khub get initech-pov`
**Then:**
- [ ] the frontmatter and the body are printed
- [ ] the id resolves by slug, qualified `type/slug` only on ambiguity
- [ ] a Rich view renders on a TTY, or JSON when `--format json` is set
- [ ] the raw file content is emitted unchanged under `--format raw`

**Test Data:** firm-ops workspace with `project/initech-pov` carrying frontmatter and a body; runs across the default, `--format json`, and `--format raw` branches

#### TS-ENT-002-02: Include Derived Edges

**Validates:** AC-002
**Level:** Integration

**Given** an entity whose schema declares a derived inverse of a stored predicate — firm-ops v1 declares none, so a generic fixture type with a stored `supersedes` (and its derived `superseded_by`) exercises the engine's inverse derivation
**When** the agent runs `khub get <node> --edges`
**Then:**
- [ ] the stored forward edge (`supersedes`) is included
- [ ] the derived inverse (`superseded_by`) is included
- [ ] each edge is marked as stored versus derived

**Test Data:** a generic fixture type declaring `supersedes` with a derived `superseded_by`; `node-a` stores `supersedes → node-b`, so the inverse derives on `node-b` (firm-ops carries no native stored→derived pair)

#### TS-ENT-002-03: Unknown Id

**Validates:** AC-003
**Level:** Integration

**Given** no entity resolves to the id
**When** the agent runs `khub get nope`
**Then:**
- [ ] a lookup error is returned
- [ ] the system displays `No entity 'nope' found`

**Test Data:** firm-ops workspace with no entity slugged `nope`

#### TS-ENT-002-04: Ambiguous Slug

**Validates:** AC-004
**Level:** Integration

**Given** two types share a slug
**When** the agent runs `khub get acme` without a type qualifier
**Then:**
- [ ] an ambiguity error is returned
- [ ] the system displays `Slug 'acme' is ambiguous: client/acme, partnership/acme. Qualify as type/slug`

**Test Data:** workspace holding both `client/acme` and `partnership/acme`

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-ENT-002-U01 | Id resolver | Resolves a bare slug to one entity, returning frontmatter and body | REQ-ENT002-01 |
| TS-ENT-002-U02 | Ambiguity detector | Requires a `type/slug` qualifier when a bare slug resolves across two types | REQ-ENT002-04, ENT-006 |
| TS-ENT-002-U03 | Inverse-edge deriver | Computes `superseded_by` from a stored `supersedes` at read time, never persisting it | REQ-ENT002-02, ENT-005 |
| TS-ENT-002-U04 | Edge marker | Tags each returned edge as stored versus derived | REQ-ENT002-02 |
| TS-ENT-002-U05 | Lookup error | Raises a lookup error for an unresolvable id | REQ-ENT002-03 |
| TS-ENT-002-U06 | Output formatter | Emits JSON for `--format json` and byte-unchanged file content for `--format raw` | REQ-ENT002-01 |

---

### STORY-ENT-003: Edit an Entity

**Spec:** As an Agent or Operator, I want to change an entity's fields and re-validate, So that edits produce a minimal git diff, bump `updated`, and let me publish a draft by hand with `edit <id> draft false`

#### TS-ENT-003-01: Edit a Field

**Validates:** AC-001
**Level:** Integration

**Given** an entity exists
**When** the agent runs `khub edit initech-deal stage proposal-sent`
**Then:**
- [ ] the new value is validated against the field's enum (opportunity stages)
- [ ] the change is written, preserving key order and comments (minimal diff)
- [ ] `updated` is bumped to today
- [ ] the system displays `Updated opportunity 'initech-deal'`

**Test Data:** `opportunity/initech-deal` with a `stage` enum (`prospect, proposal-sent, won, signed, lost`) and an `updated` date earlier than today

#### TS-ENT-003-02: A Field Edit Leaves Draft Untouched; Explicit `draft false` Publishes

**Validates:** AC-002
**Level:** E2E

**Given** a draft entity missing one required relation
**When** the agent runs `khub edit some-opp --owner noor`
**Then:**
- [ ] the relation target (`person/noor`) resolves and the edit is written
- [ ] the `draft` flag is left untouched (`draft: true` — a field edit never auto-promotes)

**And When** the agent then runs `khub edit some-opp draft false`
**Then:**
- [ ] the `draft` flag is set to `false` by hand, publishing the entity

**Test Data:** an `opportunity/some-opp` saved as `draft: true` missing only `owner`; an existing `person/noor`

#### TS-ENT-003-03: Invalid Enum Value

**Validates:** AC-003
**Level:** Integration

**Given** an entity with an enum field
**When** the agent runs `khub edit initech-deal stage banana`
**Then:**
- [ ] the edit is rejected
- [ ] the system displays `'banana' is not a valid stage (prospect, proposal-sent, won, signed, lost)`
- [ ] the file is left unchanged

**Test Data:** `opportunity/initech-deal` with the `stage` enum; the out-of-enum value `banana`

#### TS-ENT-003-04: Unknown Field Under Strict

**Validates:** AC-004
**Level:** Integration

**Given** the agent edits an undeclared field
**When** they run `khub edit initech-deal vibe high --strict`
**Then:**
- [ ] the edit is rejected under `--strict`
- [ ] without `--strict`, the same edit accepts and preserves the extension

**Test Data:** `opportunity/initech-deal`; the undeclared field `vibe`; runs with and without `--strict`

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-ENT-003-U01 | Enum re-validator | Rejects an out-of-enum value and leaves the file unchanged | REQ-ENT003-03 |
| TS-ENT-003-U02 | `updated` bumper | Bumps `updated` to today on every successful edit | REQ-ENT003-01, ENT-008 |
| TS-ENT-003-U03 | Round-trip writer | Preserves key order and comments, writing a minimal diff | REQ-ENT003-01, ENT-007 |
| TS-ENT-003-U04 | Manual draft setter | A field edit never flips `draft`; `edit <id> draft true|false` sets it by hand | REQ-ENT003-02, ENT-009 |
| TS-ENT-003-U05 | Strict-mode editor | Rejects an edit to an undeclared field under `--strict`; preserves the extension otherwise | REQ-ENT003-04, ENT-004 |

---

### STORY-ENT-004: Link and Unlink Relations

**Spec:** As an Agent or Operator, I want to add and remove schema-checked relations between entities, So that the typed graph stays authoritative and every edge resolves to a legal target

#### TS-ENT-004-01: Add a Relation

**Validates:** AC-001
**Level:** E2E

**Given** two entities exist and the predicate is legal for the source type
**When** the agent runs `khub link initech-pov partner northwind`
**Then:**
- [ ] the predicate is checked as declared for the source type
- [ ] the target resolves by its schema-known type
- [ ] cardinality is enforced (single-valued versus many)
- [ ] the edge is stored single-sided on the source entity
- [ ] the system displays `Linked initech-pov --partner--> northwind`

**Test Data:** `project/initech-pov` and a resolvable `partner` target `northwind`; the `partner` predicate declared legal for `project`

#### TS-ENT-004-02: Illegal Predicate

**Validates:** AC-002
**Level:** Integration

**Given** a predicate not declared for the source type
**When** the agent runs `khub link initech-pov engagement some-meeting`
**Then:**
- [ ] the link is rejected
- [ ] the system displays `Predicate 'engagement' is not legal for type 'project'`

**Test Data:** `project/initech-pov`; the `engagement` predicate (legal for `meeting`, not `project`)

#### TS-ENT-004-03: Unresolvable Target

**Validates:** AC-003
**Level:** Integration

**Given** the target does not exist
**When** the agent runs `khub link initech-pov owner ghost`
**Then:**
- [ ] the link is rejected (referential integrity)
- [ ] the system displays `No person 'ghost' to satisfy predicate 'owner'`

**Test Data:** `project/initech-pov`; no `person/ghost` in the workspace

#### TS-ENT-004-04: Unlink a Relation

**Validates:** AC-004
**Level:** Integration

**Given** an existing edge
**When** the agent runs `khub unlink initech-pov partner northwind`
**Then:**
- [ ] the edge is removed from the source entity
- [ ] any derived inverse is left to recompute
- [ ] the system displays `Unlinked initech-pov --partner--> northwind`

**Test Data:** `project/initech-pov` already storing `partner → northwind`

#### TS-ENT-004-05: Cardinality Violation

**Validates:** AC-005
**Level:** Integration

**Given** a single-valued predicate already set
**When** the agent runs `khub link initech-pov owner dana` while `owner` is set
**Then:**
- [ ] the link is rejected or replaced per the single-valued rule
- [ ] the system displays `Predicate 'owner' is single-valued; use edit to replace`

**Test Data:** `project/initech-pov` whose single-valued `owner` is already set to `noor`; a second resolvable `person/dana`

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-ENT-004-U01 | Predicate-legality checker | Confirms the predicate is declared for the source type, rejecting otherwise | REQ-ENT004-01, REQ-ENT004-03, ENT-010 |
| TS-ENT-004-U02 | Cardinality enforcer | Reads single-versus-many from the schema and refuses a second value on a single-valued predicate | REQ-ENT004-01, ENT-012 |
| TS-ENT-004-U03 | Single-sided storer | Stores the forward edge on the source entity only, never on the target | REQ-ENT004-02, ENT-011 |
| TS-ENT-004-U04 | Target resolver | Resolves the target by its schema-known type, rejecting an unresolvable target | REQ-ENT004-04 |
| TS-ENT-004-U05 | Unlink + inverse recompute | Removes the forward edge and lets the derived inverse recompute (never stored) | REQ-ENT004-05, ENT-011 |

---

### STORY-ENT-005: Remove an Entity

**Spec:** As an Agent or Operator, I want to delete an entity, guarded by inbound edges, So that removal never silently breaks referential integrity across the graph

#### TS-ENT-005-01: Remove an Unreferenced Entity

**Validates:** AC-001
**Level:** E2E

**Given** no inbound edge resolves to the entity
**When** the agent runs `khub remove old-fragment`
**Then:**
- [ ] the entity file (or folder for a folder-layout type) is deleted
- [ ] the system displays `Removed fragment 'old-fragment'`

**Test Data:** a `fragment/old-fragment` with no inbound edges pointing at it

#### TS-ENT-005-02: Refuse When Inbound Edges Resolve

**Validates:** AC-002
**Level:** Integration

**Given** another entity links to the target
**When** the agent runs `khub remove initech`
**Then:**
- [ ] the delete is refused
- [ ] the inbound edges are listed (which entities and predicates point at it)
- [ ] the system displays `Refusing to remove client 'initech': 3 inbound edges resolve to it. Pass --force to override`

**Test Data:** `client/initech` with three resolving inbound edges (e.g. opportunities/projects naming it as `client`)

#### TS-ENT-005-03: Force Override

**Validates:** AC-003
**Level:** Integration

**Given** inbound edges resolve and the operator accepts the breakage
**When** the agent runs `khub remove initech --force`
**Then:**
- [ ] the entity is deleted
- [ ] the now-dangling inbound edges are left for `check` to surface

**Test Data:** the same referenced `client/initech`; `--force` flag; a follow-up `check` run asserting the dangling edges

#### TS-ENT-005-04: Unknown Id

**Validates:** AC-004
**Level:** Integration

**Given** the id does not resolve
**When** the agent runs `khub remove ghost`
**Then:**
- [ ] a lookup error is returned
- [ ] the system displays `No entity 'ghost' found`

**Test Data:** workspace with no entity slugged `ghost`

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-ENT-005-U01 | Inbound-edge detector | Finds every entity and predicate whose edge resolves to the target | REQ-ENT005-02, ENT-013 |
| TS-ENT-005-U02 | Removal guard | Refuses the delete while any inbound edge resolves, unless `--force` is set | REQ-ENT005-02, ENT-013 |
| TS-ENT-005-U03 | Folder-layout deleter | Deletes the folder and its `_index.md` for a folder-layout type; the flat file otherwise | REQ-ENT005-01 |
| TS-ENT-005-U04 | Force path | Deletes under `--force` and leaves dangling inbound edges for `check` to surface | REQ-ENT005-03, ENT-014 |
| TS-ENT-005-U05 | Lookup error | Raises a lookup error for an unresolvable id | REQ-ENT005-04 |

---

## Coverage Matrix

### Acceptance Criteria → Test Scenarios

| Story | AC | Description | Test Scenarios |
|-------|----|-------------|----------------|
| STORY-ENT-001 | AC-001 | Create a well-formed active entity | TS-ENT-001-01 |
| STORY-ENT-001 | AC-002 | Missing required field saves active; `--draft` sets the flag | TS-ENT-001-02 |
| STORY-ENT-001 | AC-003 | Relation to a non-existent target is rejected | TS-ENT-001-03 |
| STORY-ENT-001 | AC-004 | Unknown field under strict | TS-ENT-001-04 |
| STORY-ENT-001 | AC-005 | Create a meeting with an explicit engagement edge | TS-ENT-001-05 |
| STORY-ENT-001 | AC-006 | Explicit id and collision suffix | TS-ENT-001-06 |
| STORY-ENT-002 | AC-001 | Print an entity | TS-ENT-002-01 |
| STORY-ENT-002 | AC-002 | Include derived edges | TS-ENT-002-02 |
| STORY-ENT-002 | AC-003 | Unknown id | TS-ENT-002-03 |
| STORY-ENT-002 | AC-004 | Ambiguous slug | TS-ENT-002-04 |
| STORY-ENT-003 | AC-001 | Edit a field | TS-ENT-003-01 |
| STORY-ENT-003 | AC-002 | A field edit leaves draft untouched; explicit `draft false` publishes | TS-ENT-003-02 |
| STORY-ENT-003 | AC-003 | Invalid enum value | TS-ENT-003-03 |
| STORY-ENT-003 | AC-004 | Unknown field under strict | TS-ENT-003-04 |
| STORY-ENT-004 | AC-001 | Add a relation | TS-ENT-004-01 |
| STORY-ENT-004 | AC-002 | Illegal predicate | TS-ENT-004-02 |
| STORY-ENT-004 | AC-003 | Unresolvable target | TS-ENT-004-03 |
| STORY-ENT-004 | AC-004 | Unlink a relation | TS-ENT-004-04 |
| STORY-ENT-004 | AC-005 | Cardinality violation | TS-ENT-004-05 |
| STORY-ENT-005 | AC-001 | Remove an unreferenced entity | TS-ENT-005-01 |
| STORY-ENT-005 | AC-002 | Refuse when inbound edges resolve | TS-ENT-005-02 |
| STORY-ENT-005 | AC-003 | Force override | TS-ENT-005-03 |
| STORY-ENT-005 | AC-004 | Unknown id | TS-ENT-005-04 |

### EARS Requirements → Test Scenarios

| Requirement | Type | Description | Test Scenarios |
|-------------|------|-------------|----------------|
| REQ-ENT001-01 | EARS-E | All required present → write the entity as `active` | TS-ENT-001-01, TS-ENT-001-U01, TS-ENT-001-U02, TS-ENT-001-U03 |
| REQ-ENT001-02 | EARS-W | Required field/relation missing → still save active (draft is manual), preserve fields; `--draft` sets the flag | TS-ENT-001-02, TS-ENT-001-U03 |
| REQ-ENT001-03 | EARS-W | Relation names a non-existent target → reject, write no file | TS-ENT-001-03, TS-ENT-001-U04 |
| REQ-ENT001-04 | EARS-O | `--strict` → reject any undeclared field | TS-ENT-001-04, TS-ENT-001-U05 |
| REQ-ENT001-05 | EARS-E | Meeting created → store `engagement` explicit edge, write flat | TS-ENT-001-05, TS-ENT-001-U07 |
| REQ-ENT001-06 | EARS-W | Explicit `--id` collides within type → append deterministic suffix | TS-ENT-001-06, TS-ENT-001-U06 |
| REQ-ENT002-01 | EARS-E | `get` on a resolvable id → print frontmatter and body | TS-ENT-002-01, TS-ENT-002-U01, TS-ENT-002-U06 |
| REQ-ENT002-02 | EARS-O | `--edges` → include derived inverse, marked as derived | TS-ENT-002-02, TS-ENT-002-U03, TS-ENT-002-U04 |
| REQ-ENT002-03 | EARS-W | Id does not resolve → lookup error | TS-ENT-002-03, TS-ENT-002-U05 |
| REQ-ENT002-04 | EARS-W | Bare slug ambiguous across types → require `type/slug` qualifier | TS-ENT-002-04, TS-ENT-002-U02 |
| REQ-ENT003-01 | EARS-E | Field edited → re-validate, bump `updated`, minimal diff | TS-ENT-003-01, TS-ENT-003-U02, TS-ENT-003-U03 |
| REQ-ENT003-02 | EARS-E | `edit <id> draft true|false` → set the flag by hand; a field edit never touches it | TS-ENT-003-02, TS-ENT-003-U04 |
| REQ-ENT003-03 | EARS-W | New value violates type/enum → reject, leave file unchanged | TS-ENT-003-03, TS-ENT-003-U01 |
| REQ-ENT003-04 | EARS-O | `--strict` → reject edits to undeclared fields | TS-ENT-003-04, TS-ENT-003-U05 |
| REQ-ENT004-01 | EARS-E | `link` → verify predicate legal, target resolves, cardinality holds | TS-ENT-004-01, TS-ENT-004-U01, TS-ENT-004-U02 |
| REQ-ENT004-02 | EARS-U | Store forward edges single-sided on the source entity | TS-ENT-004-01, TS-ENT-004-U03 |
| REQ-ENT004-03 | EARS-W | Predicate illegal for source type → reject the link | TS-ENT-004-02, TS-ENT-004-U01 |
| REQ-ENT004-04 | EARS-W | Target does not resolve → reject the link | TS-ENT-004-03, TS-ENT-004-U04 |
| REQ-ENT004-05 | EARS-E | `unlink` → remove edge, let derived inverses recompute | TS-ENT-004-04, TS-ENT-004-U05 |
| REQ-ENT005-01 | EARS-E | `remove` with no inbound edges → delete the entity | TS-ENT-005-01, TS-ENT-005-U03 |
| REQ-ENT005-02 | EARS-W | Inbound edge resolves → refuse unless `--force` | TS-ENT-005-02, TS-ENT-005-U01, TS-ENT-005-U02 |
| REQ-ENT005-03 | EARS-E | `--force` → delete and leave dangling edges for `check` | TS-ENT-005-03, TS-ENT-005-U04 |
| REQ-ENT005-04 | EARS-W | Id does not resolve → lookup error | TS-ENT-005-04, TS-ENT-005-U05 |

### Business Rules → Test Scenarios

| Rule | Description | Enforcement | Test Scenarios |
|------|-------------|-------------|----------------|
| ENT-001 | The id is a bare slug, unique per `(type, slug)` | Constraint | TS-ENT-001-06, TS-ENT-001-U01, TS-ENT-001-U06 |
| ENT-002 | Referential integrity hard-fails on write; a relation must resolve | Validation | TS-ENT-001-03, TS-ENT-001-U04 |
| ENT-003 | A well-formed but incomplete entity saves active, never rejected; `draft` is a manual flag | Validation | TS-ENT-001-02, TS-ENT-001-U03 |
| ENT-004 | Undeclared fields are preserved unless `--strict` closes the schema | Validation | TS-ENT-001-04, TS-ENT-001-U05, TS-ENT-003-U05 |
| ENT-005 | Derived edges are computed at read time, never stored | Constraint | TS-ENT-002-02, TS-ENT-002-U03 |
| ENT-006 | A bare slug resolves only when unique across types | Validation | TS-ENT-002-04, TS-ENT-002-U02 |
| ENT-007 | Edits round-trip frontmatter, preserving key order and comments | Constraint | TS-ENT-003-01, TS-ENT-003-U03 |
| ENT-008 | `updated` is bumped on every successful edit | Automation | TS-ENT-003-01, TS-ENT-003-U02 |
| ENT-009 | `draft` is set only by hand via `edit <id> draft true|false`; an edit never auto-flips it | Validation | TS-ENT-003-02, TS-ENT-003-U04 |
| ENT-010 | A predicate must be schema-legal for the source type | Validation | TS-ENT-004-02, TS-ENT-004-U01 |
| ENT-011 | Forward edges are stored single-sided; inverses are derived, never stored | Constraint | TS-ENT-004-01, TS-ENT-004-04, TS-ENT-004-U03, TS-ENT-004-U05 |
| ENT-012 | Cardinality is enforced from the schema (single versus many) | Validation | TS-ENT-004-05, TS-ENT-004-U02 |
| ENT-013 | Removal is refused while any inbound edge resolves, unless forced | Validation | TS-ENT-005-02, TS-ENT-005-U01, TS-ENT-005-U02 |
| ENT-014 | A forced removal surfaces resulting breakage through `check`, not a silent fix | Constraint | TS-ENT-005-03, TS-ENT-005-U04 |
| ENT-SHARED-001 | Referential integrity hard-fails on write; every relation must resolve | Validation | TS-ENT-001-03, TS-ENT-004-03 |
| ENT-SHARED-002 | A well-formed but incomplete entity saves active; capture is never blocked and `draft` is manual | Validation | TS-ENT-001-02, TS-ENT-003-02 |
| ENT-SHARED-003 | Forward edges store single-sided; inverse edges are derived | Constraint | TS-ENT-002-02, TS-ENT-004-01 |
| ENT-SHARED-004 | Structural integrity is guaranteed; semantic truth is not (a schema-legal but false write validates) | Constraint | TS-ENT-001-01, TS-ENT-003-01 |

---

## Test Data

### Entities

#### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | resolvable `client`/`owner`, `stage` in enum | Happy-path active create (TS-ENT-001-01) |
| Invalid | required `client`/`owner` omitted | Incomplete saves active; `--draft` sets the flag (TS-ENT-001-02) |
| Invalid | `client` names a non-existent `ghost-co` | Referential-integrity hard-fail, no file (TS-ENT-001-03) |
| Boundary | extra undeclared field `vibe`, with/without `--strict` | Strict-mode rejection vs free extension on create (TS-ENT-001-04) |
| Valid | `initech-deal` with `stage` in enum, `updated` earlier than today | Edit a field; strict-mode edit (TS-ENT-003-01, TS-ENT-003-04) |
| Invalid | `initech-deal` `stage` set to `banana` | Out-of-enum rejection, file unchanged (TS-ENT-003-03) |
| Boundary | `some-opp` saved `draft: true`, missing only `owner` | Field edit leaves draft set; explicit `draft false` publishes (TS-ENT-003-02) |

#### meeting

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `engagement → initech-pov`, `call-type client`, `source recording`, `date 2026-06-19` | Explicit union edge, flat layout (TS-ENT-001-05) |

#### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `--id acme`, `--name "Acme Corp"` into an empty type | Explicit-id use (TS-ENT-001-06) |
| Boundary | `acme` already present within the `client` type | Deterministic `acme-2` suffix (TS-ENT-001-06) |
| Boundary | three resolving inbound edges | Inbound-edge refusal and `--force` override (TS-ENT-005-02, -03) |

#### project

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-pov` with required `client`/`owner` (folder layout), carrying a body | get round-trip; link (TS-ENT-002-01, TS-ENT-004-01) |
| Boundary | single-valued `owner` already set | Cardinality refusal (TS-ENT-004-05) |

#### generic fixture (derived inverse)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | synthetic type declaring `supersedes` with a derived `superseded_by`; `node-a` stores `supersedes → node-b` | Derived inverse at read time — firm-ops declares none (TS-ENT-002-02) |

#### fragment

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `old-fragment` with no inbound edges | Clean removal (TS-ENT-005-01) |

#### Missing / ambiguous lookups

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Missing | id `nope` / `ghost` resolves to nothing | Lookup error on get/remove (TS-ENT-002-03, TS-ENT-005-04) |
| Ambiguous | both `client/acme` and `partnership/acme` exist | Bare-slug ambiguity error (TS-ENT-002-04) |

### Management

- **Setup:** most scenarios seed the workspace via the FS-001 `init firm-ops` scaffold into a per-test temp dir, then write the prerequisite entities (`client/initech`, `person/noor`, `opportunity/initech-deal`, the referenced `client/initech`) through `core.create` so referential integrity holds before the verb under test runs; the Typer `CliRunner` drives the command. The derived-inverse scenario (TS-ENT-002-02) instead seeds a generic schema fixture declaring a `supersedes`/`superseded_by` pair, since firm-ops declares no inverse.
- **Cleanup:** each workspace is written into a `tmp_path` and torn down after the test; no shared `.khub/` or entity tree between tests.
- **Isolation:** each scenario scaffolds and mutates its own workspace; hard-fail cases snapshot the directory listing before the call and assert it is unchanged after, so "writes no file" is verified against a disposable tree.

---

## Test Environment

### Stack

| Tool | Purpose |
|------|---------|
| pytest | Unit and integration tests |
| pytest `tmp_path` fixtures | Per-test workspace isolation and before/after file-listing diffs |
| Typer / Click `CliRunner` | Drive `add`, `get`, `edit`, `link`/`unlink`, `remove` and capture exit code + output |
| FS-001 workspace scaffold + FS-000 compiled schema | Real schema the write verbs introspect for fields, enums, and legal predicates |
| ruamel.yaml (round-trip) | Assert minimal-diff edits preserve key order and comments |
| Rich (console capture, `force_terminal` toggle) | Assert the TTY Rich-view branch and the non-TTY JSON branch on `get` |

### Mocks

| Service | Strategy | Rationale |
|---------|----------|-----------|
| Compiled schema | Real, not mocked | Field/enum/pattern validation and predicate legality only hold against the real compiled contract |
| Filesystem `.khub/` + entity tree | Real temp dir (`tmp_path`) | The hard-fail-writes-no-file and minimal-diff guarantees need real file output |
| `check` (FS-004) | Real run on the resulting tree | The `--force` dangling-edge assertion (TS-ENT-005-03) verifies `check` surfaces the break, not a stub |
| TTY detection | Forced via `CliRunner` / Rich `force_terminal` | Exercises both the Rich-view and JSON branches deterministically |
| CRM / Recorder / Airtable | Not exercised | External ids ride as `source_id` aliases; outside the authoring-verb scope |

---

## Execution Plan

| Phase | Tests | Gate | Target |
|-------|-------|------|--------|
| 1. Unit | Slug minting + collision, field/enum/pattern validation, manual draft flag, referential-integrity check, strict filter, layout resolution, id/ambiguity resolution, inverse derivation, round-trip write, `updated` bump, predicate legality, cardinality, inbound-edge detection | Block PR | < 30s |
| 2. Integration | Five-verb command surface, hard-fail-no-file, incomplete `add` saves active + `--draft` flag, `--strict` rejection, illegal-predicate/unresolvable-target/cardinality refusal, inbound-edge refusal, `--force`, `--format json` parity, located error messages | Block PR | < 2min |
| 3. E2E | `add` → `get` round-trip; a field edit leaves draft set then `edit <id> draft false` publishes; `link` → derived inverse on the target; `remove` referenced client refused then `--force` + `check` reports the break | Block merge | < 5min |

### CI Triggers

- **Pull request:** Phases 1-2 (fast feedback)
- **Main branch:** Phases 1-3 (full coverage, including the cross-verb round-trips)
- **Pre-release:** Phases 1-3 (no separate performance phase; the authoring verbs are not runtime hot paths)

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Located error messages (no client to satisfy relation, unknown field under strict, ambiguous slug, illegal predicate, single-valued, inbound-edge refusal) are asserted verbatim and brittle to wording | TS-ENT-001-03/04, TS-ENT-002-04, TS-ENT-004-02/05, TS-ENT-005-02 break on cosmetic edits | Assert on the variable fields (slug, field, predicate, type, count) plus a message substring, keeping the spec's exact text as the canonical example |
| The "writes no file" half of a hard-fail is silently satisfied if a partial file leaks before validation | TS-ENT-001-03 is the only guard against a half-written entity on a rejected create | Snapshot the full directory listing before the call and assert byte-for-byte identity after, independent of the exit code |
| Minimal-diff round-trips depend on the YAML library preserving key order and comments; a serializer swap can pass tests that only check values | TS-ENT-003-01, TS-ENT-003-U03 give false confidence in git-diff quality | Assert the raw file text diff (not just parsed values) is confined to the changed key and the `updated` line |
| Derived `superseded_by` could be accidentally persisted to disk, passing a read-time assertion while corrupting the store | TS-ENT-002-02 validates the read view but not the on-disk absence | Assert the inverse appears in `get --edges` output AND is absent from the target entity's stored frontmatter on disk |
| Within-type slug collision is deterministic only if the suffix algorithm is stable across runs and OS file ordering | TS-ENT-001-06 flakes if the suffix derives from directory scan order | Pin the suffix to a within-type `(type, slug)` count, asserted twice in one test to confirm `acme` then `acme-2` regardless of filesystem ordering |
| Single-valued cardinality (AC-005) leaves the reject-versus-replace choice open in the spec | TS-ENT-004-05 could assert a behavior the implementation does not pick | Assert the invariant (no second value silently lands) and the located message; treat reject-vs-replace as a spec decision to confirm before pinning one branch |
