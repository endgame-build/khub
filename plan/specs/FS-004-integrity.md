---
id: FS-004
name: Integrity Loop
priority: Critical
dependencies: [FS-002]
updated: 2026-06-20
---

# Integrity Loop

## Overview

The integrity loop keeps the graph clean without manual policing, and it is the v1 acceptance signal for the HQ cutover. `validate` checks per-entity well-formedness and referential integrity; `check` runs graph-wide over the active subgraph; `stale` flags entities past an `updated` threshold with dates backfilled from git; `log` renders git history at ontology altitude. khub guarantees structural integrity, never semantic truth — the backstop for a false-but-legal write is attributable git history, not a gate.

**Primary Actor:** Agent

**Depends on:** FS-002: Authoring

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-INT-001 | Validate Entities | Agent, Author |
| STORY-INT-002 | Check the Graph | Agent, Author |
| STORY-INT-003 | Find Stale Entities | Operator, Agent |
| STORY-INT-004 | Render Git History at Ontology Altitude | Agent, Author |

---

## Stories

---

### STORY-INT-001: Validate Entities

**As an** Agent or Author
**I want to** check entities for well-formedness and referential integrity over the declared subset
**So that** I know every present field is legal and every relation resolves, before trusting the data

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves with a compiled schema
- [ ] PRE-002: The target path or `--all` selects entities to validate

#### Acceptance Criteria

##### AC-001: Validate a Clean Tree

**Given** a workspace where every declared field is legal and every relation resolves
**When** the agent runs `khub validate --all`
**Then** the system shall:
- [ ] Check each present declared field against its type, enum, pattern, and cardinality
- [ ] Confirm every relation resolves to an existing target
- [ ] Leave undeclared extension fields unchecked
- [ ] Display: "Validated N entities; 0 errors"
- [ ] Exit 0

##### AC-002: Report Per-Entity Errors

**Given** an entity with a malformed value and an entity with an unresolved relation
**When** the agent runs `khub validate --all`
**Then** the system shall:
- [ ] Report each error with its entity id, field, and reason
- [ ] Continue past the first error and report all of them
- [ ] Exit non-zero

##### AC-003: Strict Closes the Schema

**Given** an entity carrying undeclared fields
**When** the agent runs `khub validate --all --strict`
**Then** the system shall:
- [ ] Reject undeclared keys as errors under `--strict`
- [ ] Pass the same tree without `--strict` (extensions allowed)

##### AC-004: Skip Reference Files Cleanly

**Given** the tree holds reference markdown with no frontmatter
**When** the agent runs `khub validate --all`
**Then** the system shall:
- [ ] Skip files with no recognized frontmatter, not error on them
- [ ] Count only typed entities

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-INT001-01 | EARS-E | When validate runs, the system shall check each present declared field against the schema |
| REQ-INT001-02 | EARS-W | If a relation does not resolve, then the system shall report a referential-integrity error |
| REQ-INT001-03 | EARS-O | Where `--strict` is set, the system shall reject undeclared keys |
| REQ-INT001-04 | EARS-U | The system shall validate only the schema-declared subset, leaving extensions alone unless strict |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| INT-001 | Validation covers the declared subset; undeclared keys pass unless `--strict` | Validation |
| INT-002 | Referential integrity is part of validate: every relation must resolve | Validation |
| INT-003 | Files with no recognized frontmatter are skipped, not failed | Constraint |

#### Technical Notes

- **Command:** `khub validate [target=all]` — `--all`, `--strict`, `--fix`, `--format`
- **Library verb:** `core.validate(target, strict)` over the generated Pydantic model
- **Entities:** any type; reference docs skipped
- **Invariant upheld:** structural integrity is guaranteed, semantic truth is not (Principle 5)
- **Output:** per-entity error list; exit code reflects pass/fail

#### Test Hints

- **Unit:** field/enum/pattern checks; extension passthrough
- **Integration:** referential-integrity failure; `--strict` rejection
- **E2E:** validate an HQ snapshot reaches parity with `kb.py validate`

---

### STORY-INT-002: Check the Graph

**As an** Agent or Author
**I want to** run a graph-wide integrity pass over the active subgraph
**So that** required relations are complete, no entity is orphaned, no file is stray, and no edge cycles

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The in-memory index is built

#### Acceptance Criteria

##### AC-001: Check a Sound Graph

**Given** an active subgraph where every required relation resolves
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] Confirm every relation resolves
- [ ] Confirm required relations are complete for `active` entities only
- [ ] Confirm no orphans (entities with no inbound or outbound edge where the schema expects one)
- [ ] Confirm no stray files outside the type layouts
- [ ] Confirm no edge cycles
- [ ] Display: "Graph check passed"
- [ ] Exit 0

##### AC-002: Drafts Do Not Satisfy Required Relations

**Given** an active entity whose required relation points at a `draft`
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] Report the required relation as incomplete (a draft does not satisfy it)
- [ ] Name the active entity and the missing-completeness predicate

##### AC-003: Report Orphans and Stray Files

**Given** an entity with no expected edge and a file outside any type layout
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] List orphan entities
- [ ] List stray files
- [ ] Exit non-zero

##### AC-004: Detect an Edge Cycle

**Given** a cycle on a predicate that should be acyclic (e.g. `supersedes`)
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] Report the cycle with the participating ids
- [ ] Exit non-zero

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-INT002-01 | EARS-E | When check runs, the system shall verify relations resolve, required-completeness holds for active entities, and no cycles exist |
| REQ-INT002-02 | EARS-S | While checking completeness, the system shall treat a `draft` target as not satisfying a required relation |
| REQ-INT002-03 | EARS-W | If an orphan, stray file, or edge cycle is found, then the system shall report it and exit non-zero |
| REQ-INT002-04 | EARS-U | The system shall evaluate completeness over the active subgraph only |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| INT-004 | Required-completeness is enforced over `active` entities only | Validation |
| INT-005 | A `draft` does not satisfy another entity's required relation | Validation |
| INT-006 | `check` is the structural gap query: orphans and missing required relations | Validation |

#### State Machine

```
┌─────────────┐
│    draft    │  excluded from required-completeness
└──────┬──────┘
       │ edit fills required fields/relations
       ▼
┌─────────────┐
│   active    │  counts toward check completeness;
└─────────────┘  may satisfy another entity's required relation
```

| From | Action | To | Conditions |
|------|--------|----|------------|
| draft | edit reaches completeness | active | all required fields and relations present |
| active | a required relation target is removed | (check fails) | the relation no longer resolves |

#### Technical Notes

- **Command:** `khub check` — `--format`
- **Library verb:** `core.check()` over the `networkx` index (cycle detection, completeness)
- **Entities:** the whole active subgraph
- **Invariant upheld:** the schema is the contract; structural integrity is checked, not semantic truth
- **Output:** pass/fail report; exit code reflects result

#### Test Hints

- **Unit:** orphan detection, completeness over active-only
- **Integration:** draft-does-not-satisfy; cycle detection on `supersedes`
- **E2E:** `check` on an HQ snapshot surfaces the firm-ops structural gaps

---

### STORY-INT-003: Find Stale Entities

**As an** Operator or Agent
**I want to** list entities whose `updated` is past a threshold, with dates backfilled from git
**So that** I can find context that has drifted out of date without trusting hand-kept timestamps

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The workspace is a git repository with history

#### Acceptance Criteria

##### AC-001: List Stale Entities

**Given** entities older than the default threshold
**When** the operator runs `khub stale`
**Then** the system shall:
- [ ] Return entities whose `updated` is more than 30 days old
- [ ] Sort by age, oldest first
- [ ] Render a Rich table on a TTY

##### AC-002: Custom Threshold

**Given** a different staleness window
**When** the operator runs `khub stale --days 90`
**Then** the system shall:
- [ ] Apply the 90-day threshold

##### AC-003: Backfill Missing Dates From Git

**Given** an entity with no `updated` field
**When** the operator runs `khub stale`
**Then** the system shall:
- [ ] Read the last-commit date from `git log` for that file
- [ ] Use the git date in the staleness comparison
- [ ] Not write the backfilled date (that is `backfill`'s job)

##### AC-004: No Git History

**Given** a workspace that is not a git repository
**When** the operator runs `khub stale`
**Then** the system shall:
- [ ] Fall back to the `updated` field only
- [ ] Display: "No git history; using updated field only"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-INT003-01 | EARS-E | When stale runs, the system shall return entities past the `updated` threshold, oldest first |
| REQ-INT003-02 | EARS-O | Where `--days <n>` is set, the system shall apply that threshold instead of 30 |
| REQ-INT003-03 | EARS-W | If `updated` is missing, then the system shall read the date from `git log` for the comparison |
| REQ-INT003-04 | EARS-W | If the workspace has no git history, then the system shall fall back to the `updated` field |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| INT-007 | The default staleness threshold is 30 days | Constraint |
| INT-008 | `stale` reads git dates but does not write them | Constraint |

#### Technical Notes

- **Command:** `khub stale` — `--days <n=30>`, `--format`
- **Library verb:** `core.stale(days)` reading `git log` via subprocess
- **Entities:** any type with an `updated` field
- **Invariant upheld:** history is derived from git, the same way the graph is
- **Output:** Rich table or JSON, sorted by age

#### Test Hints

- **Unit:** threshold comparison, sort order
- **Integration:** git-date backfill for a missing `updated`
- **E2E:** `stale --days 30` on an HQ snapshot matches `kb.py report --stale`

---

### STORY-INT-004: Render Git History at Ontology Altitude

**As an** Agent or Author
**I want to** read git history described in entities and relations, not files
**So that** I can orient on what changed without a gate, distinct from the supersession chain

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The workspace is a git repository with history

#### Acceptance Criteria

##### AC-001: Render Recent History

**Given** a git history of entity edits
**When** the agent runs `khub log`
**Then** the system shall:
- [ ] Read `git log` and map each commit's files to entity ids and relations
- [ ] Describe changes at ontology altitude (which entity, which relation), not raw file paths
- [ ] Honor `--limit` and `--since`

##### AC-002: History for One Entity

**Given** an entity with an edit history
**When** the agent runs `khub log initech-pov`
**Then** the system shall:
- [ ] Return only commits touching that entity
- [ ] Render the edit history in order

##### AC-003: Distinct From Supersession History

**Given** a decision with both git edits and a supersession chain
**When** the agent runs `khub log decision-0012`
**Then** the system shall:
- [ ] Return git commit history (who changed what, when)
- [ ] Not return the `supersedes` chain (that is `history`'s job)

##### AC-004: No Git History

**Given** a workspace with no git history
**When** the agent runs `khub log`
**Then** the system shall:
- [ ] Display: "No git history available"
- [ ] Exit 0

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-INT004-01 | EARS-E | When log runs, the system shall render git history mapped to entities and relations |
| REQ-INT004-02 | EARS-O | Where an id is given, the system shall return only commits touching that entity |
| REQ-INT004-03 | EARS-U | The system shall present log as git history, distinct from `history`'s supersession chain |
| REQ-INT004-04 | EARS-W | If no git history exists, then the system shall report it and exit 0 |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| INT-009 | `log` is derived from git, rendered at ontology altitude | Constraint |
| INT-010 | `log` (git history) is distinct from `history` (graph supersession chain) | Constraint |

#### Technical Notes

- **Command:** `khub log [id]` — `--limit`, `--since <date>`, `--format`
- **Library verb:** `core.log(id)` reading `git log` via subprocess
- **Entities:** any type
- **Invariant upheld:** history is a derived projection of git; no gate
- **Output:** ontology-altitude change list as table or JSON

#### Test Hints

- **Unit:** commit-to-entity mapping
- **Integration:** per-entity filtering; `--since` windowing
- **E2E:** `log` describes a recent edit by entity and relation, not file path

---

## Shared Context

### Entities

The integrity loop reads the firm-ops graph and emits report records. The report shapes below are what each command returns; the entity types they check live in `docs/firm-ops-preset.md`.

| Entity | Description |
|--------|-------------|
| Validation Result | Per-entity well-formedness and referential-integrity outcome |
| Check Report | Graph-wide outcome: completeness, orphans, stray files, cycles |
| Stale Entry | An entity past the `updated` threshold, with its effective date |
| Log Entry | A git change mapped to an entity and its relations |

#### Validation Result

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Entity | Reference | Yes | The entity validated |
| Is Well Formed | Yes/No | Yes | Declared fields legal |
| Has Referential Integrity | Yes/No | Yes | Every relation resolves |
| Errors | Collection | No | Field, reason pairs when failing |
| Strict | Yes/No | Yes | Whether undeclared keys were rejected |

#### Check Report

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Relations Resolve | Yes/No | Yes | No dangling edge |
| Required Complete | Yes/No | Yes | Active entities meet required relations |
| Orphans | Collection | No | Entities missing an expected edge |
| Stray Files | Collection | No | Files outside the type layouts |
| Cycles | Collection | No | Edge cycles with participating ids |

### Check Outcome *(named enumeration)*

| Value | Description |
|-------|-------------|
| pass | Every relation resolves, required-completeness holds, no orphans, strays, or cycles |
| fail | One or more structural integrity rules broke; exit non-zero |

### Required Relation *(firm-ops examples)*

| Relation | On Type | Drives Completeness |
|----------|---------|---------------------|
| owner | most types | An active entity needs a resolvable owner |
| client | opportunity, project, build, case-study | An active engagement needs a client |
| engagement | meeting | A meeting needs its parent (path-derived) |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| A required-relation target becomes `draft` | `check` reports incomplete | A draft does not satisfy a required relation |
| A required-relation target is removed | `check` reports a dangling edge | Referential integrity broke |
| A false-but-legal write | `validate` and `check` both pass | Structural integrity is guaranteed, not semantic truth; git revert is the backstop |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| INT-SHARED-001 | Completeness is evaluated over the active subgraph only; drafts are excluded | STORY-INT-001, STORY-INT-002 |
| INT-SHARED-002 | History and staleness are derived from git, never hand-maintained | STORY-INT-003, STORY-INT-004 |
| INT-SHARED-003 | khub guarantees structural integrity, not semantic correctness | STORY-INT-001, STORY-INT-002 |

### Cross-Feature Dependencies

| Entity | Source Feature | Usage |
|--------|---------------|-------|
| Node / Edge | FS-002: Authoring | The entities and edges the loop validates and checks |
| Schema | FS-001: Workspace & Schema | Required relations and field rules come from the schema |

### Cross-Story Dependencies

```
STORY-INT-001 (Validate Entities)
    └── STORY-INT-002 (Check the Graph)
STORY-INT-003 (Find Stale Entities)
STORY-INT-004 (Render Git History)
```

Validate is per-entity and runs first; check is graph-wide and assumes entities individually validate. Stale and log are independent git-derived reads that round out the loop.

## Scope Changes

### Deferred Stories

| Story | Moved To | Reason | Date |
|-------|----------|--------|------|
| Auto-fix on validate (`--fix` beyond date backfill) | Fast-follow | v1 `--fix` is limited; broad auto-repair is unscheduled | 2026-06-20 |
| Goldens-eval scoring of ingestion (precision/recall) | Fast-follow | Fuzzy eval lands with facet/OKF ingestion | 2026-06-20 |
