---
id: FS-004
name: Integrity Loop
priority: Critical
dependencies: [FS-002]
updated: 2026-06-27
---

# Integrity Loop

## Overview

The integrity loop keeps the graph clean without manual policing, and it is the v1 acceptance signal for the HQ cutover. `validate` checks per-entity well-formedness and referential integrity; `check` runs graph-wide over the active (published, `draft: false`) subgraph, computing required-completeness from the schema; `stale` flags entities past an `updated` threshold with dates backfilled from git; `log` renders git history at ontology altitude. `draft` is a manual publish flag (FS-002), not a completeness verdict — so a published entity can be incomplete and `check` reports it as active-but-incomplete; the published subgraph's completeness is *enforced by `check`*, not guaranteed by the flag. khub guarantees structural integrity, never semantic truth — the backstop for a false-but-legal write is attributable git history, not a gate.

**Primary Actor:** Agent

**Depends on:** FS-002: Authoring

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-INT-001 | Validate Entities | Agent, Operator |
| STORY-INT-002 | Check the Graph | Agent, Operator |
| STORY-INT-003 | Find Stale Entities | Operator, Agent |
| STORY-INT-004 | Render Git History at Ontology Altitude | Agent, Operator |

---

## Stories

---

### STORY-INT-001: Validate Entities

**As an** Agent or Operator
**I want to** check entities for well-formedness and referential integrity over the declared subset
**So that** I know every present field is legal and every relation resolves, before trusting the data

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves with a compiled schema
- [ ] PRE-002: The target path (default: the whole workspace) selects entities to validate

#### Acceptance Criteria

##### AC-001: Validate a Clean Tree

**Given** a workspace where every declared field is legal and every relation resolves
**When** the agent runs `khub validate`
**Then** the system shall:
- [ ] Check each present declared field against its type, enum, pattern, and cardinality
- [ ] Confirm every relation resolves to an existing target
- [ ] Leave undeclared extension fields unchecked
- [ ] Display: "Validated N entities; 0 errors"
- [ ] Exit 0

##### AC-002: Report Per-Entity Errors

**Given** an entity with a malformed value and an entity with an unresolved relation
**When** the agent runs `khub validate`
**Then** the system shall:
- [ ] Report each error with its entity id, field, and reason
- [ ] Continue past the first error and report all of them
- [ ] Exit non-zero

##### AC-003: Strict Closes the Schema

**Given** an entity carrying undeclared fields
**When** the agent runs `khub validate --strict`
**Then** the system shall:
- [ ] Reject undeclared keys as errors under `--strict`
- [ ] Pass the same tree without `--strict` (extensions allowed)

##### AC-004: Skip Reference Files Cleanly

**Given** the tree holds reference markdown with no frontmatter
**When** the agent runs `khub validate`
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

- **Command:** `khub validate [target=all]` — `--strict`, `--fix`, `--format`
- **`--fix` scope (v1):** date backfill only (the same `git log` logic as `backfill`); broader auto-repair is the deferred story below
- **Library verb:** `core.validate(target, strict)` over the generated Pydantic model
- **Entities:** any type; reference docs skipped
- **Invariant upheld:** structural integrity is guaranteed, semantic truth is not (Principle 5)
- **Output:** per-entity error list; exit code reflects pass/fail

#### Test Hints

- **Unit:** field/enum/pattern checks; extension passthrough
- **Integration:** referential-integrity failure; `--strict` rejection
- **E2E:** validate an HQ snapshot cleanly (functional cutover, not byte-parity with `kb.py`)

---

### STORY-INT-002: Check the Graph

**As an** Agent or Operator
**I want to** run a graph-wide integrity pass over the active subgraph
**So that** published entities are complete, no relation dangles, no entity is orphaned, no file is stray, and no edge cycles

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The in-memory index is built

#### Acceptance Criteria

##### AC-001: Check a Sound Graph

**Given** a published subgraph where every required relation resolves to a published target
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] Confirm every relation resolves
- [ ] Confirm required-completeness for every `active` (`draft: false`) entity, computed from the schema
- [ ] Confirm no orphans (entities with neither an inbound nor an outbound relation)
- [ ] Confirm no stray files — files inside a type's layout path that do not parse as that type (reference markdown outside every type layout is skipped, not flagged)
- [ ] Confirm no edge cycles
- [ ] Display: "Graph check passed"
- [ ] Exit 0

##### AC-002: Report Active-but-Incomplete Entities

**Given** a published entity (`draft: false`) missing a required field or relation — created with `add` (active by default) or published with `edit <id> draft false` while incomplete
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] Compute completeness from the schema, never from the `draft` flag
- [ ] Report the entity as active-but-incomplete, naming each missing required field and unresolved required relation
- [ ] Exit non-zero

##### AC-003: Drafts Do Not Satisfy Required Relations

**Given** an active entity whose required relation points at a `draft`
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] Report the required relation as incomplete (a draft does not satisfy it)
- [ ] Name the active entity and the unsatisfied predicate

##### AC-004: Report Orphans, Dangling Edges, and Stray Files

**Given** an orphan entity, a relation whose target was removed (e.g. by `remove --force`), and a file outside any type layout
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] List orphan entities
- [ ] List dangling edges (a relation whose target no longer resolves)
- [ ] List stray files
- [ ] Exit non-zero

##### AC-005: Detect an Edge Cycle

**Given** a cycle on a predicate that should be acyclic (e.g. `depends_on`)
**When** the agent runs `khub check`
**Then** the system shall:
- [ ] Report the cycle with the participating ids
- [ ] Exit non-zero

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-INT002-01 | EARS-E | When check runs, the system shall verify relations resolve, required-completeness holds for active entities, and no cycles exist |
| REQ-INT002-02 | EARS-W | If an active entity is missing a required field or relation, then the system shall report it as active-but-incomplete and exit non-zero |
| REQ-INT002-03 | EARS-S | While checking completeness, the system shall treat a `draft` target as not satisfying a required relation |
| REQ-INT002-04 | EARS-W | If an orphan, dangling edge, stray file, or edge cycle is found, then the system shall report it and exit non-zero |
| REQ-INT002-05 | EARS-U | The system shall compute completeness from the schema over the active subgraph only, never inferring it from the `draft` flag |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| INT-004 | Required-completeness is enforced over `active` entities only, computed from the schema (never inferred from the `draft` flag) | Validation |
| INT-005 | A `draft` does not satisfy another entity's required relation | Validation |
| INT-006 | `check` is the structural gap query: orphans (entities with zero relations, in or out) and missing required relations | Validation |
| INT-011 | A stray file sits inside a type's layout path but does not parse as that type; markdown outside every type layout is a reference doc, skipped not flagged | Validation |
| INT-012 | An `active` entity missing a required field or relation is reported as active-but-incomplete; completeness is derived from the schema, not the `draft` flag | Validation |

#### Completeness Model

`draft` is a manual publish flag (FS-002); `check` derives required-completeness
from the schema, not from the flag. The lifecycle transitions themselves live in
FS-002 — here the flag only selects what `check` evaluates.

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

#### Technical Notes

- **Command:** `khub check` — `--format`
- **Library verb:** `core.check()` over the `networkx` index (cycle detection, completeness)
- **Entities:** the whole active subgraph
- **Invariant upheld:** the schema is the contract; structural integrity is checked, not semantic truth
- **Output:** pass/fail report; exit code reflects result

#### Test Hints

- **Unit:** orphan detection; completeness from the schema; active-but-incomplete detection
- **Integration:** active-but-incomplete reporting; draft-does-not-satisfy; dangling edge after `remove --force`; cycle detection on `depends_on`
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
- **Library verb:** `core.stale(days)` reading `git log` via subprocess (shares the `git log` date helper with `backfill`, FS-005)
- **Entities:** any type with an `updated` field
- **Invariant upheld:** history is derived from git, the same way the graph is
- **Output:** Rich table or JSON, sorted by age

#### Test Hints

- **Unit:** threshold comparison, sort order
- **Integration:** git-date backfill for a missing `updated`
- **E2E:** `stale --days 30` surfaces the stale set on an HQ snapshot

---

### STORY-INT-004: Render Git History at Ontology Altitude

**As an** Agent or Operator
**I want to** read git history described in entities and relations, not files
**So that** I can orient on what changed without a gate, distinct from the supersession chain

> **Preset note:** firm-ops v1 declares no `decision` type and no `supersedes` predicate (both HQ-only, dropped). `log` runs against any firm-ops entity — AC-001 and AC-002 use `project/initech-pov`. AC-003's contrast with a populated supersession chain uses a generic fixture, since firm-ops demonstrates no `supersedes` natively; the `log` (git history) vs `history` (graph chain) distinction holds at the command level regardless of preset. See `docs/firm-ops-preset.md`.

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
| Required Complete | Yes/No | Yes | Active entities meet required fields and relations, computed from the schema |
| Incomplete | Collection | No | Active-but-incomplete entities, each with its missing required fields and relations |
| Orphans | Collection | No | Entities missing an expected edge |
| Stray Files | Collection | No | Files inside a type's layout that are not valid entities of that type |
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
| client | opportunity, project, case-study | An active engagement needs a client |
| engagement | meeting | A meeting needs its engagement (explicit edge) |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| A published entity is missing a required field/relation | `check` reports active-but-incomplete | Completeness is enforced by `check`, derived from the schema, not the `draft` flag |
| A required-relation target becomes `draft` | `check` reports incomplete | A draft does not satisfy a required relation |
| A required-relation target is removed | `check` reports a dangling edge | Referential integrity broke |
| A false-but-legal write | `validate` and `check` both pass | Structural integrity is guaranteed, not semantic truth; git revert is the backstop |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| INT-SHARED-001 | Completeness is evaluated over the active subgraph only, computed from the schema; drafts are excluded and a draft never satisfies a required relation | STORY-INT-001, STORY-INT-002 |
| INT-SHARED-002 | History and staleness are derived from git, never hand-maintained | STORY-INT-003, STORY-INT-004 |
| INT-SHARED-003 | khub guarantees structural integrity, not semantic correctness | STORY-INT-001, STORY-INT-002 |
| INT-SHARED-004 | Orphan (zero relations) and stale are core projection properties, surfaced by default in `status` and `query`; `check`/`stale` gate on the same computation | STORY-INT-002, STORY-INT-003 |

### Cross-Feature Dependencies

| Entity | Source Feature | Usage |
|--------|---------------|-------|
| Node / Edge | FS-002: Authoring | The entities and edges the loop validates and checks |
| Schema | FS-001: Workspace & Schema | Required relations and field rules come from the schema |
| `draft` flag | FS-002: Authoring | Manual publish flag; drafts are unpublished — exempt from completeness, never satisfy a required relation |

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
