---
id: FS-002
name: Authoring
priority: Critical
dependencies: [FS-001]
updated: 2026-06-24
---

# Authoring

## Overview

The write surface over the core library: mint, read, edit, relate, and remove entities, with the schema and git as the only gates. Agent and human are symmetric writers of the same graph — no propose-then-approve step. A file written by hand, outside these verbs, is gated the same way: `khub validate <file>` re-checks it against the schema (FS-004) — the per-file counterpart to graph-wide `khub check`. Referential integrity hard-fails on write, while a missing required field saves the entity as a `draft` so capture is never blocked. Every command is a thin adapter over a core verb (`create`, `get`, `update`, `delete`, `link`).

**Primary Actor:** Agent

**Depends on:** FS-001: Workspace & Schema

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-ENT-001 | Create an Entity | Agent, Operator |
| STORY-ENT-002 | Read an Entity | Agent, Operator |
| STORY-ENT-003 | Edit an Entity | Agent, Operator |
| STORY-ENT-004 | Link and Unlink Relations | Agent, Operator |
| STORY-ENT-005 | Remove an Entity | Agent, Operator |

---

## Stories

---

### STORY-ENT-001: Create an Entity

**As an** Agent or Operator
**I want to** mint a new entity of a schema type with field values
**So that** typed context is captured as one Markdown file in git, validated against the schema

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves with a compiled schema
- [ ] PRE-002: The named type exists in the schema
- [ ] PRE-003: Any relation target named at create time already exists (referential integrity)

#### Acceptance Criteria

##### AC-001: Create a Well-Formed Active Entity

**Given** the agent supplies a known type with all required fields and resolvable relations
**When** they run `khub add opportunity --client initech --owner noor --stage discovery`
**Then** the system shall:
- [ ] Validate each declared field against its type, enum, and pattern
- [ ] Resolve `client` and `owner` to existing targets
- [ ] Mint a bare slug as the id, unique within the type
- [ ] Write one file at the type's layout path (`opportunities/{slug}/_index.md`)
- [ ] Set `created` and `updated` to today
- [ ] Set `draft: false` because all required fields and relations are present
- [ ] Print the new id and the file path
- [ ] Display: "Created opportunity '{slug}' (active)"

##### AC-002: Missing Required Field Saves as Draft

**Given** the agent omits a required field or relation
**When** they run `khub add opportunity --stage discovery`
**Then** the system shall:
- [ ] Save the entity with `draft: true`
- [ ] Preserve the supplied fields
- [ ] Not block capture
- [ ] Display: "Created opportunity '{slug}' (draft: missing required client, owner)"

##### AC-003: Relation to a Non-Existent Target Is Rejected

**Given** the agent names a relation target that does not resolve
**When** they run `khub add opportunity --client ghost-co --owner noor --stage discovery`
**Then** the system shall:
- [ ] Reject the write (referential integrity hard-fail)
- [ ] Display: "No client 'ghost-co' to satisfy relation 'client'"
- [ ] Write no file

##### AC-004: Unknown Field Under Strict

**Given** the agent passes a field the schema does not declare
**When** they run `khub add opportunity --client initech --owner noor --stage discovery --vibe high --strict`
**Then** the system shall:
- [ ] Reject the write under `--strict`
- [ ] Display: "Unknown field 'vibe' rejected under --strict"
- [ ] Without `--strict`, accept and preserve `vibe` as a free extension

##### AC-005: Create a Meeting With an Explicit Engagement Edge

**Given** the agent creates a meeting for a parent engagement
**When** they run `khub add meeting --engagement initech-pov --call-type client --source recording --date 2026-06-19`
**Then** the system shall:
- [ ] Write the file flat at `meetings/{slug}.md`
- [ ] Mint a bare slug as the id
- [ ] Store `engagement` as an explicit edge resolving to its union target (opportunity | project | build | partnership)

##### AC-006: Explicit Id and Collision Suffix

**Given** the agent supplies an explicit slug
**When** they run `khub add client --id acme --name "Acme Corp"`
**Then** the system shall:
- [ ] Use `acme` as the id when unique within the type
- [ ] Append a deterministic suffix on a within-type collision (e.g. `acme-2`)
- [ ] Mint a slug (from `--id`, else the name or type) when `--id` is omitted

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-ENT001-01 | EARS-E | When all required fields and relations are present, the system shall write the entity as `active` |
| REQ-ENT001-02 | EARS-W | If a required field or relation is missing, then the system shall save the entity as `draft` and preserve the supplied fields |
| REQ-ENT001-03 | EARS-W | If a relation names a non-existent target, then the system shall reject the write and write no file |
| REQ-ENT001-04 | EARS-O | Where `--strict` is set, the system shall reject any undeclared field |
| REQ-ENT001-05 | EARS-E | When a meeting is created, the system shall store `engagement` as an explicit edge and write the entity flat (nesting deferred post-MVP) |
| REQ-ENT001-06 | EARS-W | If an explicit `--id` collides within the type, then the system shall append a deterministic suffix |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| ENT-001 | The id is a bare slug, unique per `(type, slug)` | Constraint |
| ENT-002 | Referential integrity hard-fails on write; a relation must resolve | Validation |
| ENT-003 | A well-formed but incomplete entity is saved as `draft`, never rejected | Validation |
| ENT-004 | Undeclared fields are preserved unless `--strict` closes the schema | Validation |

#### State Machine

```
┌─────────────┐
│   (create)  │
└──────┬──────┘
       │ required fields + relations present?
       ├── yes ──► active
       └── no  ──► draft
                    │ edit fills the gap
                    └──────────► active
```

| From | Action | To | Conditions |
|------|--------|----|------------|
| (create) | add with all required present | active | every required field and relation resolves |
| (create) | add with a gap | draft | a required field or relation is missing |
| draft | edit fills the missing field/relation | active | completeness reached |

#### Technical Notes

- **Command:** `khub add <type>` — `--<field> <value>` (repeatable), `--id`, `--body-file <path>` (`-` for stdin), `--strict`
- **Body:** authored in the file, not argv. `add` writes frontmatter with an empty body and prints the path; `--body-file`/stdin covers the agent that pipes generated prose. Inline `--body <string>` is omitted — prose in argv is quoting-hostile.
- **Library verb:** `core.create(type, fields, parent)`
- **Entities:** all 12 firm-ops types
- **Invariant upheld:** relations are authoritative; Markdown is truth
- **Output:** the new id and file path; `--format json` emits the written record

#### Test Hints

- **Unit:** slug minting, draft-vs-active gating, enum/pattern validation
- **Integration:** referential-integrity hard-fail; explicit engagement edge on a flat meeting
- **E2E:** `add opportunity` then `get` round-trips frontmatter and body

---

### STORY-ENT-002: Read an Entity

**As an** Agent or Operator
**I want to** print an entity's frontmatter and body, optionally with derived edges
**So that** I can act on typed context, including the inverse edges the schema computes

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The id resolves to one entity

#### Acceptance Criteria

##### AC-001: Print an Entity

**Given** an entity exists at the given id
**When** the agent runs `khub get initech-pov`
**Then** the system shall:
- [ ] Print the frontmatter and the body
- [ ] Resolve the id by slug, qualified `type/slug` only on ambiguity
- [ ] Render a Rich view on a TTY or JSON when `--format json` is set
- [ ] Emit the raw file content unchanged under `--format raw`

##### AC-002: Include Derived Edges

**Given** an entity that has computed inverse edges
**When** the agent runs `khub get decision-0012 --edges`
**Then** the system shall:
- [ ] Include the stored forward edges (`supersedes`)
- [ ] Include the derived inverse (`superseded_by`)
- [ ] Mark which edges are stored versus derived

##### AC-003: Unknown Id

**Given** no entity resolves to the id
**When** the agent runs `khub get nope`
**Then** the system shall:
- [ ] Return a lookup error
- [ ] Display: "No entity 'nope' found"

##### AC-004: Ambiguous Slug

**Given** two types share a slug
**When** the agent runs `khub get acme` without a type qualifier
**Then** the system shall:
- [ ] Return an ambiguity error
- [ ] Display: "Slug 'acme' is ambiguous: client/acme, partnership/acme. Qualify as type/slug"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-ENT002-01 | EARS-E | When get runs on a resolvable id, the system shall print frontmatter and body |
| REQ-ENT002-02 | EARS-O | Where `--edges` is set, the system shall include derived inverse edges, marked as derived |
| REQ-ENT002-03 | EARS-W | If the id does not resolve, then the system shall return a lookup error |
| REQ-ENT002-04 | EARS-W | If a bare slug is ambiguous across types, then the system shall require a `type/slug` qualifier |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| ENT-005 | Derived edges are computed at read time, never stored | Constraint |
| ENT-006 | A bare slug resolves only when unique across types | Validation |

#### Technical Notes

- **Command:** `khub get <id>` — `--format json\|table\|raw`, `--edges`
- **Library verb:** `core.get(id, with_edges)`
- **Entities:** any type; inverse edges per Principle 4
- **Invariant upheld:** relations are authoritative from two sources
- **Output:** Rich view, JSON, or raw file content

#### Test Hints

- **Unit:** id resolution, ambiguity detection
- **Integration:** inverse-edge derivation (`supersedes` → `superseded_by`)
- **E2E:** `get --edges` on a decision shows the derived `superseded_by`

---

### STORY-ENT-003: Edit an Entity

**As an** Agent or Operator
**I want to** change an entity's fields and re-validate
**So that** edits produce a minimal git diff, bump `updated`, and can promote a draft to active

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The id resolves to one entity

#### Acceptance Criteria

##### AC-001: Edit a Field

**Given** an entity exists
**When** the agent runs `khub edit initech-pov stage prove`
**Then** the system shall:
- [ ] Validate the new value against the field's enum (project stages)
- [ ] Write the change, preserving key order and comments (minimal diff)
- [ ] Bump `updated` to today
- [ ] Display: "Updated project 'initech-pov'"

##### AC-002: Edit Promotes a Draft to Active

**Given** a draft entity missing one required relation
**When** the agent runs `khub edit some-opp --owner noor`
**Then** the system shall:
- [ ] Resolve the relation target
- [ ] Re-validate completeness
- [ ] Clear the `draft` flag (set `draft: false`) once all required are present

##### AC-003: Invalid Enum Value

**Given** an entity with an enum field
**When** the agent runs `khub edit initech-pov stage banana`
**Then** the system shall:
- [ ] Reject the edit
- [ ] Display: "'banana' is not a valid stage (diagnose, prove, scale, complete)"
- [ ] Leave the file unchanged

##### AC-004: Unknown Field Under Strict

**Given** the agent edits an undeclared field
**When** they run `khub edit initech-pov vibe high --strict`
**Then** the system shall:
- [ ] Reject the edit under `--strict`
- [ ] Without `--strict`, accept and preserve the extension

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-ENT003-01 | EARS-E | When a field is edited, the system shall re-validate, bump `updated`, and write a minimal diff |
| REQ-ENT003-02 | EARS-E | When an edit fills the last missing required field, the system shall promote `draft` to `active` |
| REQ-ENT003-03 | EARS-W | If the new value violates the field's type or enum, then the system shall reject the edit and leave the file unchanged |
| REQ-ENT003-04 | EARS-O | Where `--strict` is set, the system shall reject edits to undeclared fields |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| ENT-007 | Edits round-trip frontmatter, preserving key order and comments | Constraint |
| ENT-008 | `updated` is bumped on every successful edit | Automation |
| ENT-009 | Completeness is re-evaluated on edit, driving the draft↔active flip | Validation |

#### Technical Notes

- **Command:** `khub edit <id> <field> <value>` — or `--<field> <value>`, `--body-file <path>` (`-` for stdin), `--strict`
- **Library verb:** `core.update(id, changes)`
- **Entities:** any type
- **Invariant upheld:** Markdown is truth; minimal-diff writes via round-trip YAML
- **Output:** confirmation; `--format json` emits the updated record

#### Test Hints

- **Unit:** enum re-validation, `updated` bump
- **Integration:** round-trip write preserves key order and comments
- **E2E:** edit fills a draft's last required relation and flips it to active

---

### STORY-ENT-004: Link and Unlink Relations

**As an** Agent or Operator
**I want to** add and remove schema-checked relations between entities
**So that** the typed graph stays authoritative and every edge resolves to a legal target

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: Both the source id and the target id resolve
- [ ] PRE-003: The predicate is legal for the source type

#### Acceptance Criteria

##### AC-001: Add a Relation

**Given** two entities exist and the predicate is legal for the source type
**When** the agent runs `khub link initech-pov partner northwind`
**Then** the system shall:
- [ ] Check the predicate is declared for the source type
- [ ] Resolve the target by its schema-known type
- [ ] Enforce cardinality (single-valued versus many)
- [ ] Store the edge single-sided on the source entity
- [ ] Display: "Linked initech-pov --partner--> northwind"

##### AC-002: Illegal Predicate

**Given** a predicate not declared for the source type
**When** the agent runs `khub link initech-pov supersedes adr-0007`
**Then** the system shall:
- [ ] Reject the link
- [ ] Display: "Predicate 'supersedes' is not legal for type 'project'"

##### AC-003: Unresolvable Target

**Given** the target does not exist
**When** the agent runs `khub link initech-pov owner ghost`
**Then** the system shall:
- [ ] Reject the link (referential integrity)
- [ ] Display: "No person 'ghost' to satisfy predicate 'owner'"

##### AC-004: Unlink a Relation

**Given** an existing edge
**When** the agent runs `khub unlink initech-pov partner northwind`
**Then** the system shall:
- [ ] Remove the edge from the source entity
- [ ] Leave any derived inverse to recompute
- [ ] Display: "Unlinked initech-pov --partner--> northwind"

##### AC-005: Cardinality Violation

**Given** a single-valued predicate already set
**When** the agent runs `khub link initech-pov owner dana` while `owner` is set
**Then** the system shall:
- [ ] Reject or replace per the single-valued rule
- [ ] Display: "Predicate 'owner' is single-valued; use edit to replace"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-ENT004-01 | EARS-E | When link runs, the system shall verify the predicate is legal, the target resolves, and cardinality holds |
| REQ-ENT004-02 | EARS-U | The system shall store forward edges single-sided on the source entity |
| REQ-ENT004-03 | EARS-W | If the predicate is illegal for the source type, then the system shall reject the link |
| REQ-ENT004-04 | EARS-W | If the target does not resolve, then the system shall reject the link |
| REQ-ENT004-05 | EARS-E | When unlink runs, the system shall remove the edge and let derived inverses recompute |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| ENT-010 | A predicate must be schema-legal for the source type | Validation |
| ENT-011 | Forward edges are stored single-sided; inverses are derived, never stored | Constraint |
| ENT-012 | Cardinality is enforced from the schema (single versus many) | Validation |

#### Technical Notes

- **Command:** `khub link <id> <predicate> <target>` · `khub unlink <id> <predicate> <target>`
- **Library verb:** `core.link(id, predicate, target)` / `core.unlink(...)`
- **Entities:** any type; 17 firm-ops predicates
- **Invariant upheld:** relations are authoritative (Principle 4)
- **Output:** confirmation; legal-predicate and target checks on every call

#### Test Hints

- **Unit:** predicate legality, cardinality checks
- **Integration:** single-sided storage; inverse recomputation after unlink
- **E2E:** `link` then `neighbors` shows the new edge in both directions

---

### STORY-ENT-005: Remove an Entity

**As an** Agent or Operator
**I want to** delete an entity, guarded by inbound edges
**So that** removal never silently breaks referential integrity across the graph

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The id resolves to one entity

#### Acceptance Criteria

##### AC-001: Remove an Unreferenced Entity

**Given** no inbound edge resolves to the entity
**When** the agent runs `khub remove old-fragment`
**Then** the system shall:
- [ ] Delete the entity file (or folder for a folder-layout type)
- [ ] Display: "Removed fragment 'old-fragment'"

##### AC-002: Refuse When Inbound Edges Resolve

**Given** another entity links to the target
**When** the agent runs `khub remove initech`
**Then** the system shall:
- [ ] Refuse the delete
- [ ] List the inbound edges (which entities and predicates point at it)
- [ ] Display: "Refusing to remove client 'initech': 3 inbound edges resolve to it. Pass --force to override"

##### AC-003: Force Override

**Given** inbound edges resolve and the operator accepts the breakage
**When** the agent runs `khub remove initech --force`
**Then** the system shall:
- [ ] Delete the entity
- [ ] Leave the now-dangling inbound edges for `check` to surface

##### AC-004: Unknown Id

**Given** the id does not resolve
**When** the agent runs `khub remove ghost`
**Then** the system shall:
- [ ] Return a lookup error
- [ ] Display: "No entity 'ghost' found"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-ENT005-01 | EARS-E | When remove runs with no inbound edges, the system shall delete the entity |
| REQ-ENT005-02 | EARS-W | If an inbound edge resolves to the target, then the system shall refuse to delete unless `--force` is passed |
| REQ-ENT005-03 | EARS-E | When `--force` is passed, the system shall delete and leave dangling edges for `check` |
| REQ-ENT005-04 | EARS-W | If the id does not resolve, then the system shall return a lookup error |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| ENT-013 | Removal is refused while any inbound edge resolves, unless forced | Validation |
| ENT-014 | A forced removal surfaces resulting breakage through `check`, not a silent fix | Constraint |

#### Technical Notes

- **Command:** `khub remove <id>` — `--force`
- **Library verb:** `core.delete(id, force)`
- **Entities:** any type
- **Invariant upheld:** structural integrity is guarded, not silently repaired
- **Output:** confirmation, or an inbound-edge report on refusal

#### Test Hints

- **Unit:** inbound-edge detection
- **Integration:** folder-layout deletion; force path leaving dangling edges
- **E2E:** remove a referenced client is refused, then `--force` plus `check` reports the break

---

## Shared Context

### Entities

The firm-ops preset's 12 types are the authoring surface. Full capture lives in `docs/firm-ops-preset.md`; the representative attribute tables below cover the types the stories exercise.

| Entity | Description |
|--------|-------------|
| opportunity | Pipeline deal; converts to a project or build |
| project | Post-sale consulting engagement (folder layout, shares path with build) |
| build | Product/engineering engagement; `type: build` under the same `projects/` path |
| meeting | Engagement touchpoint; `engagement` is an explicit union edge (flat layout) |
| transcript | Raw Recorder capture; largest node population |
| fragment | A partner's atomic note; matures through stages |
| decision | Durable ADR-style record with a supersession chain |
| case-study | Proven client outcome from a delivered engagement |
| isms-doc | Compliance/ISMS artifact |
| partnership | BD relationship feeding opportunities and projects |
| person | ENDGAME team member; the `owner`/`team` target |
| client | Client organization behind the pipeline |

#### opportunity

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `opportunity` |
| Client | Reference | Yes | Edge → client |
| Stage | Status | Yes | Pipeline stage enum |
| Owner | Reference | Yes | Edge → person |
| Created | Date | Yes | Mint date |
| Updated | Date | Yes | Last edit date |
| Source | Type | No | Lead source enum |
| Partner | Reference | No | Edge → partnership |
| Confidence | Number | No | 0–1 |

#### person

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `person` |
| Name | Text | Yes | Full name |
| Role | Status | Yes | Role enum |
| Department | Type | No | Department enum |
| Created | Date | Yes | Mint date |

#### meeting

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `meeting` |
| Date | Date | Yes | Event date |
| Engagement | Reference | Yes | Edge → opportunity \| project \| build \| partnership (explicit union edge) |
| Call Type | Type | Yes | client, sales, partner, internal |
| Source | Type | Yes | recording, manual |
| Transcript | Reference | No | Edge → transcript |
| Owner | Reference | No | Edge → person |

### Lifecycle Flag *(`draft` boolean)*

| Value | Description |
|-------|-------------|
| `draft: true` | Well-formed but incomplete; does not satisfy another entity's required relation |
| `draft: false` | All required fields and relations present (default); counts toward `check` completeness |

### Opportunity Stage *(named enumeration)*

| Value | Description |
|-------|-------------|
| nurturing | Early, pre-discovery |
| discovery | Scoping the need |
| proposal | Proposal issued |
| negotiation | Terms in flight |
| closed-won | Converted to a project or build |
| closed-lost | Did not convert |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| Remove an entity with inbound edges | Prevent deletion (unless `--force`) | Referential integrity must not break silently |
| Force-remove a referenced entity | Delete and leave dangling edges | `check` surfaces the breakage; git revert is the backstop |
| Remove a folder-layout entity | Delete the folder and its `_index.md` | The folder is the entity's storage unit |
| Unlink a forward edge | Recompute any derived inverse | Inverses are never stored |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| ENT-SHARED-001 | Referential integrity hard-fails on write; every relation must resolve | STORY-ENT-001, STORY-ENT-004 |
| ENT-SHARED-002 | A well-formed but incomplete entity saves as `draft`; capture is never blocked | STORY-ENT-001, STORY-ENT-003 |
| ENT-SHARED-003 | Forward edges store single-sided; inverse edges are derived | STORY-ENT-002, STORY-ENT-004 |
| ENT-SHARED-004 | Structural integrity is guaranteed; semantic truth is not — a schema-legal but false write validates | STORY-ENT-001, STORY-ENT-003 |

### Cross-Feature Dependencies

| Entity | Source Feature | Usage |
|--------|---------------|-------|
| Schema | FS-001: Workspace & Schema | Every write introspects the compiled schema for fields, enums, and legal predicates |
| Validate / check | FS-004: Integrity Loop | A hand-authored file is gated by `khub validate <file>`; graph-wide consistency is `khub check` |

### Cross-Story Dependencies

```
STORY-ENT-001 (Create an Entity)
    ├── STORY-ENT-002 (Read an Entity)
    ├── STORY-ENT-003 (Edit an Entity)
    │       └── promotes draft → active
    ├── STORY-ENT-004 (Link and Unlink Relations)
    └── STORY-ENT-005 (Remove an Entity)
```

An entity must exist before it can be read, edited, linked, or removed. Edit and link both feed the draft→active promotion; remove is guarded by the edges that link establishes.

## Scope Changes

### Deferred Stories

| Story | Moved To | Reason | Date |
|-------|----------|--------|------|
| Rename a slug (`khub rename`) | Deferred | Renaming with inbound-reference rewrite is named but unscheduled | 2026-06-20 |
| Concurrency arbitration on simultaneous writes | Deferred | Out of v1 scope; git is the merge surface | 2026-06-20 |
| Nested inventories and placement-derived edges (`--parent`) | Deferred | MVP flattens meetings to a root folder; nesting and path-derived parent edges are post-MVP | 2026-06-21 |
