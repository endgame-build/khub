---
id: FS-005
name: Projection
priority: Critical
dependencies: [FS-002]
updated: 2026-06-21
---

# Projection

## Overview

The output surface that renders the typed graph into navigable artifacts. `reindex` regenerates the OKF `index.md` navigation; `backfill` adds missing frontmatter and dates from git; `viz` writes a self-contained Cytoscape HTML over the typed graph. `reindex` and `backfill` are cutover requirements — they take over what `kb.py reindex` and `kb.py backfill` do today (functional, not byte-parity). The SQLite projection and OKF-bundle export wait on the fast-follow. `reindex` and `backfill` are cutover-critical, which sets this feature's Critical priority; `viz` is the lower-criticality member.

**Primary Actor:** Operator

**Depends on:** FS-002: Authoring

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-PRJ-001 | Regenerate the OKF Index | Operator, Agent |
| STORY-PRJ-002 | Backfill Frontmatter and Dates | Operator |
| STORY-PRJ-003 | Visualize the Typed Graph | Operator, Agent |

---

## Stories

---

### STORY-PRJ-001: Regenerate the OKF Index

**As an** Operator or Agent
**I want to** rebuild the OKF `index.md` navigation from the live graph
**So that** the workspace keeps a current, conformant entry point without hand-maintenance

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The in-memory index is built from the entity tree

#### Acceptance Criteria

##### AC-001: Regenerate the Index

**Given** a workspace whose entities have changed since the last index
**When** the operator runs `khub reindex`
**Then** the system shall:
- [ ] Walk the typed graph
- [ ] Write an OKF `index.md` grouping entities by type with cross-links
- [ ] Stamp the OKF version on the index
- [ ] Display: "Reindexed N entities into index.md"

##### AC-002: Dry Run

**Given** the operator wants to preview the change
**When** they run `khub reindex --dry-run`
**Then** the system shall:
- [ ] Compute the new index
- [ ] Print the diff against the current `index.md`
- [ ] Write nothing

##### AC-003: Empty Workspace

**Given** a workspace with no entities
**When** the operator runs `khub reindex`
**Then** the system shall:
- [ ] Write a valid, empty OKF `index.md`
- [ ] Display: "Reindexed 0 entities"

##### AC-004: HQ Cutover

**Given** an HQ snapshot
**When** the operator runs `khub reindex`
**Then** the system shall:
- [ ] Produce a valid, current OKF `index.md` for the same tree

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-PRJ001-01 | EARS-E | When reindex runs, the system shall regenerate the OKF `index.md` from the graph and stamp the OKF version |
| REQ-PRJ001-02 | EARS-O | Where `--dry-run` is set, the system shall print the diff and write nothing |
| REQ-PRJ001-03 | EARS-U | The system shall derive the index from the graph, never from a stored copy |
| REQ-PRJ001-04 | EARS-E | When run against an HQ snapshot, the system shall produce a valid, current OKF `index.md` |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| PRJ-001 | `index.md` is derived from the graph and regenerated, never hand-edited | Constraint |
| PRJ-002 | The index is stamped with the OKF version it conforms to | Automation |

#### Technical Notes

- **Command:** `khub reindex` — `--dry-run`
- **Library verb:** `core.reindex()` over the `networkx` index
- **Entities:** all types; OKF `index.md` output
- **Invariant upheld:** the projection is derived (Principle 2); OKF conformance
- **Output:** written `index.md`, or a diff under `--dry-run`

#### Test Hints

- **Unit:** grouping by type, cross-link generation
- **Integration:** OKF-version stamping; dry-run diff
- **E2E:** `reindex` on an HQ snapshot produces a valid OKF `index.md`

---

### STORY-PRJ-002: Backfill Frontmatter and Dates

**As an** Operator
**I want to** add missing frontmatter and dates from git history
**So that** an imported or legacy tree reaches a consistent, validatable state at cutover

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The workspace is a git repository with history

#### Acceptance Criteria

##### AC-001: Backfill Missing Dates

**Given** entities with missing `created` or `updated` fields
**When** the operator runs `khub backfill`
**Then** the system shall:
- [ ] Read first- and last-commit dates from `git log` per file
- [ ] Write `created` and `updated` where missing
- [ ] Preserve existing values, key order, and comments
- [ ] Display: "Backfilled dates on N entities"

##### AC-002: Backfill Missing Frontmatter

**Given** an entity inferable as a type but missing required scaffolding
**When** the operator runs `khub backfill --type opportunity`
**Then** the system shall:
- [ ] Add missing frontmatter scaffolding for that type
- [ ] Leave entities that already validate untouched

##### AC-003: Dry Run

**Given** the operator wants to preview the writes
**When** they run `khub backfill --dry-run`
**Then** the system shall:
- [ ] List the entities and fields that would change
- [ ] Write nothing

##### AC-004: No Git History

**Given** a workspace with no git history
**When** the operator runs `khub backfill`
**Then** the system shall:
- [ ] Skip date backfill
- [ ] Display: "No git history; dates not backfilled"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-PRJ002-01 | EARS-E | When backfill runs, the system shall write missing `created`/`updated` from `git log` |
| REQ-PRJ002-02 | EARS-U | The system shall preserve existing values, key order, and comments on backfill |
| REQ-PRJ002-03 | EARS-O | Where `--dry-run` is set, the system shall list changes and write nothing |
| REQ-PRJ002-04 | EARS-W | If the workspace has no git history, then the system shall skip date backfill |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| PRJ-003 | Backfill writes only where a value is missing; it never overwrites existing data | Constraint |
| PRJ-004 | Backfilled dates come from `git log`, the authoritative history | Automation |

#### Technical Notes

- **Command:** `khub backfill` — `--type <t>`, `--dry-run`
- **Library verb:** `core.backfill(type)` reading `git log` via subprocess (shares the `git log` date helper with `stale`, FS-004), writing via round-trip YAML
- **Entities:** any type
- **Invariant upheld:** history derived from git; minimal-diff round-trip writes
- **Output:** count of entities changed, or a dry-run list

#### Test Hints

- **Unit:** missing-field detection
- **Integration:** git first/last-commit date extraction; round-trip preservation
- **E2E:** `backfill` on an HQ snapshot writes missing dates and frontmatter

---

### STORY-PRJ-003: Visualize the Typed Graph

**As an** Operator or Agent
**I want to** render the typed graph as a self-contained interactive HTML
**So that** I can see the entity and relation structure without a server or extra tooling

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The in-memory index is built

#### Acceptance Criteria

##### AC-001: Write a Visualization

**Given** a workspace with entities and edges
**When** the operator runs `khub viz`
**Then** the system shall:
- [ ] Render nodes by type and edges by predicate
- [ ] Write a self-contained Cytoscape HTML file (default `viz.html`)
- [ ] Inline all assets so the file opens with no network
- [ ] Display: "Wrote viz.html (N nodes, M edges)"

##### AC-002: Custom Output and Open

**Given** a chosen output path
**When** the operator runs `khub viz --out graph.html --open`
**Then** the system shall:
- [ ] Write to `graph.html`
- [ ] Open it in the default browser

##### AC-003: Filter by Type

**Given** a large graph
**When** the operator runs `khub viz --type project`
**Then** the system shall:
- [ ] Render only `project` nodes and their incident edges

##### AC-004: Empty Graph

**Given** a workspace with no entities
**When** the operator runs `khub viz`
**Then** the system shall:
- [ ] Write a valid HTML with an empty canvas
- [ ] Display: "Wrote viz.html (0 nodes, 0 edges)"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-PRJ003-01 | EARS-E | When viz runs, the system shall write a self-contained Cytoscape HTML over the typed graph |
| REQ-PRJ003-02 | EARS-U | The system shall inline all assets so the output opens with no network access |
| REQ-PRJ003-03 | EARS-O | Where `--type <t>` is set, the system shall render only that type and its incident edges |
| REQ-PRJ003-04 | EARS-O | Where `--open` is set, the system shall open the file in the default browser |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| PRJ-005 | The visualization is self-contained; no external assets or server | Constraint |
| PRJ-006 | Nodes are colored by type and edges labeled by predicate | Constraint |

#### Technical Notes

- **Command:** `khub viz` — `--out <file=viz.html>`, `--open`, `--type <t>`
- **Library verb:** `core.viz(out, type)` rendering the `networkx` index to Cytoscape
- **Entities:** all types; nodes by type, edges by predicate
- **Invariant upheld:** the projection is derived and disposable
- **Output:** a standalone HTML file

#### Test Hints

- **Unit:** node/edge serialization to Cytoscape JSON
- **Integration:** asset inlining; type filtering
- **E2E:** `viz` over a seeded tree opens and renders the firm-ops graph

---

## Shared Context

### Entities

Projection reads the firm-ops graph and emits navigation and visualization artifacts. Source entity types live in `docs/firm-ops-preset.md`.

| Entity | Description |
|--------|-------------|
| OKF Index | The generated `index.md` navigation, grouped by type with cross-links |
| Visualization | The self-contained Cytoscape HTML over the typed graph |
| Backfill Change | A written frontmatter or date addition sourced from git |

#### OKF Index

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| OKF Version | Text | Yes | The OKF version stamped on the index |
| Sections | Collection | Yes | One group per entity type |
| Cross Links | Collection | Yes | Links between related entities |
| Entity Count | Number | Yes | Number of entities indexed |

#### Visualization

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Output Path | Text | Yes | Where the HTML is written (default `viz.html`) |
| Node Count | Number | Yes | Rendered nodes |
| Edge Count | Number | Yes | Rendered edges |
| Is Self Contained | Yes/No | Yes | All assets inlined; opens with no network |
| Type Filter | Reference | No | The type to restrict rendering to |

### Projection Artifact *(named enumeration)*

| Value | Description |
|-------|-------------|
| index | The OKF `index.md` navigation from `reindex` |
| viz | The Cytoscape HTML from `viz` |
| sqlite | The materialized SQLite projection (fast-follow, out of v1) |
| okf-bundle | A conformant OKF export (fast-follow, out of v1) |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| Entities change | Reindex and viz regenerate from the live graph | Projections are derived, never the source of truth |
| Backfill over existing values | Preserve them; write only what is missing | Backfill never overwrites authored data |
| Regenerating any projection | Overwrite the prior artifact | Projection output is disposable |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| PRJ-SHARED-001 | Every projection is derived from the graph and regenerated on demand | STORY-PRJ-001, STORY-PRJ-003 |
| PRJ-SHARED-002 | Writes preserve authored data; round-trip keeps key order and comments | STORY-PRJ-002 |
| PRJ-SHARED-003 | `reindex` and `backfill` are cutover requirements (functional, not byte-parity) | STORY-PRJ-001, STORY-PRJ-002 |

### Cross-Feature Dependencies

| Entity | Source Feature | Usage |
|--------|---------------|-------|
| Node / Edge | FS-002: Authoring | The graph that reindex and viz render |
| OKF fields | FS-001: Workspace & Schema | `title`, `description`, `resource` feed the index cross-links |

### Cross-Story Dependencies

```
STORY-PRJ-002 (Backfill Frontmatter and Dates)
    └── STORY-PRJ-001 (Regenerate the OKF Index)
            └── STORY-PRJ-003 (Visualize the Typed Graph)
```

Backfill brings an imported tree to a consistent state; reindex then renders a current navigation; viz draws the same graph. At cutover, backfill runs first, then reindex.

## Scope Changes

### Deferred Stories

| Story | Moved To | Reason | Date |
|-------|----------|--------|------|
| Materialize the SQLite projection (`khub build`) | Fast-follow | The in-memory index covers v1; SQLite lands past the performance budget | 2026-06-20 |
| Export an OKF bundle (`khub export --okf`) | Fast-follow | Consume-side OKF and bundle export ship with facet ingestion | 2026-06-20 |
| Preset drift / promote-back (`khub diff-preset`) | Deferred | Hub↔engagement sync is named but unscheduled | 2026-06-20 |
