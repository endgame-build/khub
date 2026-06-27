---
id: TS-003
name: Query & Graph Test Spec
spec: plan/specs/FS-003-query-graph.md
updated: 2026-06-27
---

# Query & Graph — Test Spec

## Summary

Tests the read and traversal surface — `query` filtering entities by frontmatter and derived edges, and the three `networkx` walks (`neighbors`, `impact`, `history`) for one-hop adjacency, transitive blast radius, and supersession lineage. Coverage runs from filter composition and gap-finding (`--missing` / `--has`), through the walk mechanics that define the feature — inbound adjacency includes derived inverses computed not stored, impact is cycle-safe and visits each node once, history follows a self-referential chain in order — to the cross-cutting invariant that every read operates on a derived projection rebuilt from Markdown, so no result is persisted.

**Feature Spec:** FS-003: Query & Graph
**Stories Covered:** 4
**Test Scenarios:** 16 (plus 24 unit tests)

---

## Test Strategy

### Approach

**Philosophy:** Hybrid — each surface is a thin command over a `core` verb walking the in-memory index, so the discrete mechanics (filter ANDing, `--missing`/`--has` resolution, direction and predicate filtering, descendants vs ancestors selection, cycle-safe visitation, chain ordering and limit) are unit-testable against a built index in isolation, but the guarantees that matter — inbound walks surface derived inverses that were never written to disk, a cycle terminates, an empty filter is a success not an error, the graph reflects the live tree because it is rebuilt on demand — only hold when the index is built from a real entity tree and walked end to end. Unit tests pin the graph algorithms; integration tests prove the command surface, the JSON shapes, and the located error messages; E2E proves the reads that span the seeded firm-ops tree (filter to an entry node, neighbors off it, impact down a `depends_on` chain, history up a supersession chain).

**Test Pyramid:**

| Level | Share | Scope |
|-------|-------|-------|
| Unit | ~60% | Filter composition + AND semantics, `--missing`/`--has` edge resolution, `--format ids`, orphan/stale annotation + filters, draft scoping, direction filtering, predicate filtering, derived-inverse inclusion, bounded `--depth` vs unbounded closure, descendants/ancestors selection, default predicates, cycle-safe visit-once, tree depth marking, chain ordering + limit, history-vs-log distinction |
| Integration | ~30% | Command surface for all four verbs, empty-result success, unknown-field filter error, located lookup errors, `--in`/`--out`/`--predicate` filtering, `--reverse` ancestors, no-reachable-nodes message, cycle termination, `--limit` chain cap, `--format json` shape and parity |
| E2E | ~10% | Filter an entry node, then `neighbors` it both directions including a derived inverse; `impact` over a seeded `depends_on` chain renders the reachable tree; `history` over a seeded supersession chain returns the lineage with the derived `superseded_by` |

### Coverage Targets

| Target | Goal |
|--------|------|
| Acceptance criteria | 100% of ACs from FS-003 (16 ACs across 4 stories) |
| EARS requirements | 100% of REQ-QRY* requirements (16) |
| Business rules | 100% of rule enforcement (QRY-001..010, QRY-SHARED-001..003) |
| Edge cases | Empty result is success, unknown filter field, isolated node, unknown id, no downstream impact, cycle on the walked predicate, chain with no history, `--limit` cap, derived inverse never stored |

---

## Test Scenarios

---

### STORY-QRY-001: Filter Entities by Frontmatter

**Spec:** As an Agent or Operator, I want to filter entities by type, status, field values, and relation presence, So that I can pull a precise slice of typed context, including gaps via `--missing`

#### TS-QRY-001-01: Filter by Type and Field

**Validates:** AC-001
**Level:** E2E

**Given** a firm-ops workspace with opportunities across stages
**When** the agent runs `khub query --type opportunity --stage prospect --format json`
**Then:**
- [ ] only `opportunity` entities at stage `prospect` are returned
- [ ] each match is annotated with its `orphan` and `stale` flags by default
- [ ] each match is emitted as JSON when `--format json` is set
- [ ] `--limit` caps the returned set when given

**Test Data:** firm-ops workspace with several `opportunity` entities at `prospect`, `proposal-sent`, and `won` so the filter discriminates. (FS-003 wrote `--stage discovery`; the v1 preset uses the CRM sales stages `prospect, proposal-sent, won, signed, lost`, so `prospect` is the runnable equivalent — see Risks.)

#### TS-QRY-001-02: Surface Gaps With Missing

**Validates:** AC-002
**Level:** Integration

**Given** entities that lack a required relation
**When** the agent runs `khub query --type project --missing owner`
**Then:**
- [ ] only projects with no resolvable `owner` edge are returned
- [ ] `--has owner` returns the inverse set (projects whose `owner` resolves)
- [ ] the gap surfaces regardless of `draft` state (incompleteness is orthogonal to the manual flag)

**Test Data:** firm-ops workspace with one draft `project` saved missing its required `owner` and one active `project` whose `owner` resolves to `person/noor`

#### TS-QRY-001-03: Empty Result

**Validates:** AC-003
**Level:** Integration

**Given** no entity matches the filter
**When** the agent runs `khub query --type opportunity --stage lost`
**Then:**
- [ ] an empty set is returned, not an error (exit 0)
- [ ] `No entities match` is displayed on a TTY
- [ ] `[]` is emitted under `--format json`

**Test Data:** firm-ops workspace holding opportunities, none at stage `lost`. (FS-003 wrote `--type build --stage delivered`; `build` and that stage set are HQ-only, dropped from the v1 preset — the empty filter on a live type is the runnable equivalent. See Risks.)

#### TS-QRY-001-04: Unknown Filter Field

**Validates:** AC-004
**Level:** Integration

**Given** a filter naming an undeclared field
**When** the agent runs `khub query --type opportunity --vibe high`
**Then:**
- [ ] a filter error is returned
- [ ] `No field 'vibe' on type 'opportunity'` is displayed

**Test Data:** firm-ops workspace; the undeclared field `vibe` against the `opportunity` type

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-QRY-001-U01 | Filter composer | ANDs every filter, returning only entities matching all of type, stage, field, and tag | REQ-QRY001-01, QRY-001 |
| TS-QRY-001-U02 | Missing/has resolver | `--missing <pred>` returns entities with no resolvable edge for the predicate; `--has <pred>` returns the inverse | REQ-QRY001-02 |
| TS-QRY-001-U03 | Field guard | Raises a filter error for a field the type does not declare | REQ-QRY001-03, QRY-001 |
| TS-QRY-001-U04 | Ids formatter | `--format ids` emits bare ids, one per line, for piping | REQ-QRY001-04 |
| TS-QRY-001-U05 | Flag annotator | Carries each entity's `orphan` and `stale` flags by default; `--orphan`/`--stale` filter on them | QRY-009 |
| TS-QRY-001-U06 | Empty-set return | Returns an empty collection (success) when no entity matches, never raising | REQ-QRY001-01, QRY-002 |
| TS-QRY-001-U07 | Draft scoper | Includes `draft` entities by default; `--active` excludes them, `--draft` isolates them | QRY-SHARED-003 |

---

### STORY-QRY-002: Walk One-Hop Neighbors

**Spec:** As an Agent, I want to list an entity's adjacent nodes by predicate and direction, So that I can navigate the typed graph one hop at a time, inbound or outbound

#### TS-QRY-002-01: List Neighbors in Both Directions

**Validates:** AC-001
**Level:** E2E

**Given** an entity with inbound and outbound edges
**When** the agent runs `khub neighbors initech-pov`
**Then:**
- [ ] outbound edges are returned (the entity's own `client` and `owner` relations)
- [ ] inbound edges are returned (a meeting's `engagement` edge resolving to it, surfaced as a derived inverse)
- [ ] each neighbor is labeled with its predicate and direction

**Test Data:** firm-ops workspace with `project/initech-pov` (outbound `client → initech`, `owner → noor`) and a `meeting` whose `engagement → initech-pov` provides the inbound edge

#### TS-QRY-002-02: Filter by Predicate and Direction

**Validates:** AC-002
**Level:** Integration

**Given** an entity with edges across several predicates
**When** the agent runs `khub neighbors initech --predicate client --in`
**Then:**
- [ ] only inbound `client` edges are returned (the opportunities and projects naming `initech` as their client)
- [ ] `--depth 2` extends adjacency to the second hop over all predicates

**Test Data:** `client/initech` with two inbound `client` edges (`project/initech-pov` and one `opportunity`); a second hop reachable from one of those for the `--depth 2` assertion

#### TS-QRY-002-03: Isolated Entity

**Validates:** AC-003
**Level:** Integration

**Given** an entity with no resolvable edges
**When** the agent runs `khub neighbors lonely-client`
**Then:**
- [ ] an empty neighbor set is returned
- [ ] `No neighbors` is displayed

**Test Data:** a `client/lonely-client` with no outbound edges and nothing pointing at it. (FS-003 wrote `lone-fragment`; a firm-ops `fragment` always carries a required `owner` edge, so a relation-free `client` is the truly isolated equivalent. See Risks.)

#### TS-QRY-002-04: Unknown Id

**Validates:** AC-004
**Level:** Integration

**Given** the id does not resolve
**When** the agent runs `khub neighbors ghost`
**Then:**
- [ ] a lookup error is returned
- [ ] `No entity 'ghost' found` is displayed

**Test Data:** firm-ops workspace with no entity slugged `ghost`

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-QRY-002-U01 | Direction filter | `--in`/`--out` narrow adjacency; the default is both | REQ-QRY002-02, QRY-004 |
| TS-QRY-002-U02 | Predicate filter | `--predicate <p>` restricts adjacency to edges of that predicate | REQ-QRY002-02 |
| TS-QRY-002-U03 | Inverse includer | Inbound adjacency includes derived inverses, computed from the stored forward edge, never read from disk | REQ-QRY002-04, QRY-003, QRY-SHARED-002 |
| TS-QRY-002-U04 | Depth bounder | `--depth N` is bounded multi-hop over all predicates, distinct from `impact`'s unbounded single-predicate closure | REQ-QRY002-02, QRY-010 |
| TS-QRY-002-U05 | Lookup error | Raises a lookup error for an unresolvable id | REQ-QRY002-03 |
| TS-QRY-002-U06 | Adjacency labeler | Labels each returned neighbor with its predicate and direction | REQ-QRY002-01 |

---

### STORY-QRY-003: Compute Blast Radius

**Spec:** As an Agent, I want to walk the transitive closure over an edge from an entity, So that I can see everything a change touches before I make it

#### TS-QRY-003-01: Forward Blast Radius

**Validates:** AC-001
**Level:** E2E

**Given** an entity with a transitive `depends_on` chain
**When** the agent runs `khub impact node-a --format tree`
**Then:**
- [ ] the transitive forward closure over `depends_on` (the default predicate) is walked
- [ ] the reachable set is rendered as a tree
- [ ] depth from the source is marked

**Test Data:** a seeded `depends_on` chain `node-a → node-b → node-c` (the universal `depends_on` edge is `any → any`); fragments or clients carrying the edges

#### TS-QRY-003-02: Reverse Closure (Ancestors)

**Validates:** AC-002
**Level:** Integration

**Given** an entity reached by an upstream chain
**When** the agent runs `khub impact node-c --reverse`
**Then:**
- [ ] ancestors are walked (what reaches this node) instead of descendants
- [ ] the upstream set `{node-b, node-a}` is returned
- [ ] `--predicate <p>` retargets the closure to a non-default predicate

**Test Data:** the same `node-a → node-b → node-c` `depends_on` chain, walked in reverse. (FS-003 wrote `--predicate affects --reverse`; `affects` is HQ-only, dropped from the v1 preset, so reverse over the universal `depends_on` proves the ancestors mechanic. See Risks.)

#### TS-QRY-003-03: No Reachable Nodes

**Validates:** AC-003
**Level:** Integration

**Given** an entity with no outbound edge on the predicate
**When** the agent runs `khub impact leaf-node`
**Then:**
- [ ] only the source node is returned
- [ ] `No downstream impact` is displayed

**Test Data:** a `leaf-node` with no outbound `depends_on` edge

#### TS-QRY-003-04: Cycle Safety

**Validates:** AC-004
**Level:** Integration

**Given** a graph that contains a cycle on the predicate
**When** the agent runs `khub impact node-in-cycle`
**Then:**
- [ ] the walk terminates without looping
- [ ] each reachable node is returned exactly once

**Test Data:** a seeded `depends_on` cycle `node-x → node-y → node-z → node-x`

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-QRY-003-U01 | Descendants selector | Returns the forward transitive closure over the predicate via `networkx` descendants | REQ-QRY003-01, QRY-005 |
| TS-QRY-003-U02 | Ancestors selector | On `--reverse`, returns ancestors instead of descendants | REQ-QRY003-02 |
| TS-QRY-003-U03 | Cycle guard | Visits each node once and terminates even when the graph has a cycle | REQ-QRY003-03, QRY-006 |
| TS-QRY-003-U04 | Default predicate | Defaults the impact predicate to `depends_on` when `--predicate` is omitted | REQ-QRY003-01, QRY-005 |
| TS-QRY-003-U05 | Tree depth marker | `--format tree` marks each node's depth from the source | REQ-QRY003-04 |
| TS-QRY-003-U06 | Predicate override | `--predicate <p>` retargets the closure to any declared edge | REQ-QRY003-02 |

---

### STORY-QRY-004: Trace Supersession History

**Spec:** As an Agent, I want to follow the supersession chain from a decision, So that I can read decision lineage without scanning files

> **Preset note:** the v1 firm-ops preset drops the `decision` type and the `supersedes` / derived `superseded_by` predicates (HQ-only). These scenarios run against a generic schema fixture declaring a self-referential `supersedes` edge with a derived `superseded_by` inverse — the same engine pattern TS-002 uses for derived inverses, since firm-ops demonstrates none natively. See Risks.

#### TS-QRY-004-01: Walk the Supersession Chain

**Validates:** AC-001
**Level:** E2E

**Given** a record that supersedes an earlier one
**When** the agent runs `khub history decision-0012`
**Then:**
- [ ] the `supersedes` edge (the default predicate) is followed back through the chain
- [ ] each prior record is returned in order
- [ ] the derived `superseded_by` direction is included

**Test Data:** a generic fixture type with a self-referential `supersedes` (and derived `superseded_by`); a chain `decision-0012 supersedes decision-0007 supersedes decision-0001`

#### TS-QRY-004-02: Limit the Chain

**Validates:** AC-002
**Level:** Integration

**Given** a long chain
**When** the agent runs `khub history decision-0012 --limit 3`
**Then:**
- [ ] at most the three most recent links are returned

**Test Data:** a fixture supersession chain of four or more links rooted at `decision-0012`

#### TS-QRY-004-03: No History

**Validates:** AC-003
**Level:** Integration

**Given** a record that supersedes nothing
**When** the agent runs `khub history decision-0001`
**Then:**
- [ ] only the source record is returned
- [ ] `No supersession history` is displayed

**Test Data:** `decision-0001`, the chain root, with no outbound `supersedes` edge

#### TS-QRY-004-04: Unknown Id

**Validates:** AC-004
**Level:** Integration

**Given** the id does not resolve
**When** the agent runs `khub history ghost`
**Then:**
- [ ] a lookup error is returned
- [ ] `No entity 'ghost' found` is displayed

**Test Data:** workspace with no entity slugged `ghost`

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-QRY-004-U01 | Chain orderer | Follows `supersedes` back and returns prior records in order | REQ-QRY004-01, QRY-007 |
| TS-QRY-004-U02 | Limit capper | `--limit <n>` caps the returned chain length | REQ-QRY004-02 |
| TS-QRY-004-U03 | Inverse includer | Includes the derived `superseded_by` direction, computed not stored | REQ-QRY004-01, QRY-007, QRY-SHARED-002 |
| TS-QRY-004-U04 | Default predicate | Defaults the history predicate to `supersedes` when `--predicate` is omitted | REQ-QRY004-01, QRY-007 |
| TS-QRY-004-U05 | Lookup error | Raises a lookup error for an unresolvable id | REQ-QRY004-03 |
| TS-QRY-004-U06 | History-vs-log distinction | `history` reads the graph chain; `log` reads git — the two surfaces stay distinct | REQ-QRY004-04, QRY-008 |

---

## Coverage Matrix

### Acceptance Criteria → Test Scenarios

| Story | AC | Description | Test Scenarios |
|-------|----|-------------|----------------|
| STORY-QRY-001 | AC-001 | Filter by type and field | TS-QRY-001-01 |
| STORY-QRY-001 | AC-002 | Surface gaps with `--missing` | TS-QRY-001-02 |
| STORY-QRY-001 | AC-003 | Empty result is success | TS-QRY-001-03 |
| STORY-QRY-001 | AC-004 | Unknown filter field | TS-QRY-001-04 |
| STORY-QRY-002 | AC-001 | List neighbors in both directions | TS-QRY-002-01 |
| STORY-QRY-002 | AC-002 | Filter by predicate and direction | TS-QRY-002-02 |
| STORY-QRY-002 | AC-003 | Isolated entity | TS-QRY-002-03 |
| STORY-QRY-002 | AC-004 | Unknown id | TS-QRY-002-04 |
| STORY-QRY-003 | AC-001 | Forward blast radius | TS-QRY-003-01 |
| STORY-QRY-003 | AC-002 | Reverse closure (ancestors) | TS-QRY-003-02 |
| STORY-QRY-003 | AC-003 | No reachable nodes | TS-QRY-003-03 |
| STORY-QRY-003 | AC-004 | Cycle safety | TS-QRY-003-04 |
| STORY-QRY-004 | AC-001 | Walk the supersession chain | TS-QRY-004-01 |
| STORY-QRY-004 | AC-002 | Limit the chain | TS-QRY-004-02 |
| STORY-QRY-004 | AC-003 | No history | TS-QRY-004-03 |
| STORY-QRY-004 | AC-004 | Unknown id | TS-QRY-004-04 |

### EARS Requirements → Test Scenarios

| Requirement | Type | Description | Test Scenarios |
|-------------|------|-------------|----------------|
| REQ-QRY001-01 | EARS-E | Query with filters → return only entities matching every filter | TS-QRY-001-01, TS-QRY-001-U01, TS-QRY-001-U06 |
| REQ-QRY001-02 | EARS-O | `--missing <pred>` → return entities lacking a resolvable edge | TS-QRY-001-02, TS-QRY-001-U02 |
| REQ-QRY001-03 | EARS-W | Filter names an undeclared field → filter error | TS-QRY-001-04, TS-QRY-001-U03 |
| REQ-QRY001-04 | EARS-O | `--format ids` → emit bare ids for piping | TS-QRY-001-U04 |
| REQ-QRY002-01 | EARS-E | `neighbors` → return adjacent nodes labeled by predicate and direction | TS-QRY-002-01, TS-QRY-002-U06 |
| REQ-QRY002-02 | EARS-O | `--in`/`--out`/`--predicate` → filter adjacency accordingly | TS-QRY-002-02, TS-QRY-002-U01, TS-QRY-002-U02, TS-QRY-002-U04 |
| REQ-QRY002-03 | EARS-W | Id does not resolve → lookup error | TS-QRY-002-04, TS-QRY-002-U05 |
| REQ-QRY002-04 | EARS-S | Walking inbound edges → include derived inverses | TS-QRY-002-01, TS-QRY-002-U03 |
| REQ-QRY003-01 | EARS-E | `impact` → return the transitive forward closure over the predicate | TS-QRY-003-01, TS-QRY-003-U01, TS-QRY-003-U04 |
| REQ-QRY003-02 | EARS-O | `--reverse` → walk ancestors instead of descendants | TS-QRY-003-02, TS-QRY-003-U02, TS-QRY-003-U06 |
| REQ-QRY003-03 | EARS-S | Walking → visit each node once even with a cycle | TS-QRY-003-04, TS-QRY-003-U03 |
| REQ-QRY003-04 | EARS-O | `--format tree` → render depth from the source | TS-QRY-003-01, TS-QRY-003-U05 |
| REQ-QRY004-01 | EARS-E | `history` → follow the chain, return prior records in order | TS-QRY-004-01, TS-QRY-004-U01, TS-QRY-004-U03, TS-QRY-004-U04 |
| REQ-QRY004-02 | EARS-O | `--limit <n>` → cap the chain length | TS-QRY-004-02, TS-QRY-004-U02 |
| REQ-QRY004-03 | EARS-W | Id does not resolve → lookup error | TS-QRY-004-04, TS-QRY-004-U05 |
| REQ-QRY004-04 | EARS-U | Distinguish `history` (graph chain) from `log` (git history) | TS-QRY-004-U06 |

### Business Rules → Test Scenarios

| Rule | Description | Enforcement | Test Scenarios |
|------|-------------|-------------|----------------|
| QRY-001 | Filters match frontmatter and derived edges, never body prose | Constraint | TS-QRY-001-01, TS-QRY-001-U01, TS-QRY-001-U03 |
| QRY-002 | An empty result is a success, not an error | Constraint | TS-QRY-001-03, TS-QRY-001-U06 |
| QRY-003 | Inbound adjacency includes derived inverses, computed not stored | Constraint | TS-QRY-002-01, TS-QRY-002-U03 |
| QRY-004 | Default direction is both; `--in`/`--out` narrow it | Constraint | TS-QRY-002-02, TS-QRY-002-U01 |
| QRY-005 | The default impact predicate is `depends_on` | Constraint | TS-QRY-003-01, TS-QRY-003-U01, TS-QRY-003-U04 |
| QRY-006 | Traversal is cycle-safe; nodes are visited once | Constraint | TS-QRY-003-04, TS-QRY-003-U03 |
| QRY-007 | The default history predicate is `supersedes`; the inverse is derived | Constraint | TS-QRY-004-01, TS-QRY-004-U01, TS-QRY-004-U04 |
| QRY-008 | `history` reads the graph chain, not git; `log` reads git | Constraint | TS-QRY-004-U06 |
| QRY-009 | Query output carries each entity's `orphan` and `stale` flags by default; `--orphan`/`--stale` filter on them | Constraint | TS-QRY-001-01, TS-QRY-001-U05 |
| QRY-010 | `neighbors --depth N` is bounded multi-hop over all predicates; `impact` is the unbounded single-predicate closure | Constraint | TS-QRY-002-02, TS-QRY-002-U04 |
| QRY-SHARED-001 | All reads operate on the derived projection, rebuilt from Markdown; no result is stored | Constraint | TS-QRY-002-01, TS-QRY-003-01, TS-QRY-004-01 |
| QRY-SHARED-002 | Inbound walks include derived inverse edges | Constraint | TS-QRY-002-U03, TS-QRY-004-U03 |
| QRY-SHARED-003 | Reads include `draft` entities by default and carry the `stale`/`orphan` flag; `--active` excludes drafts, `--draft` isolates them; `--missing` surfaces incompleteness regardless of draft | Constraint | TS-QRY-001-02, TS-QRY-001-U07 |

---

## Test Data

### Entities

#### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | several at `stage` `prospect` / `proposal-sent` / `won` | Type-and-field filter discriminates (TS-QRY-001-01) |
| Boundary | no opportunity at `stage` `lost` | Empty-result success (TS-QRY-001-03) |
| Valid | one with `client → initech` | Inbound `client` adjacency on the client (TS-QRY-002-02) |

#### project

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Invalid | draft project saved missing required `owner` | `--missing owner` surfaces the gap regardless of draft (TS-QRY-001-02) |
| Valid | active project whose `owner → noor` resolves | `--has owner` inverse set (TS-QRY-001-02) |
| Valid | `initech-pov` with `client → initech`, `owner → noor` | Outbound + inbound neighbors (TS-QRY-002-01, -02) |

#### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech` with two inbound `client` edges | Inbound predicate/direction filter, `--depth 2` (TS-QRY-002-02) |
| Boundary | `lonely-client` with no inbound or outbound edges | Isolated entity, `No neighbors` (TS-QRY-002-03) |

#### meeting

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `engagement → initech-pov` | Provides the inbound (derived-inverse) edge on the project (TS-QRY-002-01) |

#### depends_on chain (generic nodes)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `node-a → node-b → node-c` via universal `depends_on` | Forward closure + tree depth; reverse ancestors (TS-QRY-003-01, -02) |
| Boundary | `leaf-node` with no outbound `depends_on` | `No downstream impact` (TS-QRY-003-03) |
| Boundary | `node-x → node-y → node-z → node-x` cycle | Cycle-safe visit-once (TS-QRY-003-04) |

#### supersession chain (generic fixture)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | fixture type with self-referential `supersedes` + derived `superseded_by`; `decision-0012 → decision-0007 → decision-0001` | Chain order + derived inverse (TS-QRY-004-01) |
| Boundary | chain of four or more links | `--limit 3` cap (TS-QRY-004-02) |
| Boundary | `decision-0001` chain root, no outbound `supersedes` | `No supersession history` (TS-QRY-004-03) |

#### Missing lookups

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Missing | id `ghost` resolves to nothing | Lookup error on neighbors/history (TS-QRY-002-04, TS-QRY-004-04) |

### Management

- **Setup:** firm-ops scenarios seed the workspace via the FS-001 `init firm-ops` scaffold into a per-test temp dir, then write the prerequisite entities (`client/initech`, `person/noor`, `project/initech-pov`, the opportunities, the referencing meeting) through `core.create` so referential integrity holds, then build the in-memory index before the walk under test runs; the Typer `CliRunner` drives the command. The `depends_on` chains/cycles seed generic nodes carrying the universal edge. The supersession scenarios (STORY-QRY-004) instead seed a generic schema fixture declaring a `supersedes`/`superseded_by` pair, since firm-ops v1 carries neither.
- **Cleanup:** each workspace is written into a `tmp_path` and torn down after the test; no shared `.khub/` or index between tests.
- **Isolation:** each scenario scaffolds and walks its own workspace; the derived-projection assertion (QRY-SHARED-001) rebuilds the index from the seeded Markdown and confirms no walk result is written back to disk.

---

## Test Environment

### Stack

| Tool | Purpose |
|------|---------|
| pytest | Unit and integration tests |
| pytest `tmp_path` fixtures | Per-test workspace and index isolation |
| Typer / Click `CliRunner` | Drive `query`, `neighbors`, `impact`, `history` and capture exit code + output |
| networkx | The in-memory index the walks traverse (descendants/ancestors, adjacency, chain) |
| FS-001 workspace scaffold + FS-000 compiled schema | Real schema the filters and edge resolution introspect for fields and predicate legality |
| Rich (console capture, `force_terminal` toggle) | Assert the TTY table branch and the non-TTY JSON branch |

### Mocks

| Service | Strategy | Rationale |
|---------|----------|-----------|
| Compiled schema | Real, not mocked | Field legality and predicate/target resolution only hold against the real compiled contract |
| In-memory index | Real, built from the seeded tree | The derived-projection, derived-inverse, and cycle guarantees need a real `networkx` build, not a stub |
| Filesystem `.khub/` + entity tree | Real temp dir (`tmp_path`) | The "rebuilt from Markdown on demand" invariant needs real file input |
| TTY detection | Forced via `CliRunner` / Rich `force_terminal` | Exercises both the table and JSON branches deterministically |
| git (`log`) | Not exercised here | `history` reads the graph only; the `history`-vs-`log` distinction (TS-QRY-004-U06) asserts `history` never touches git |

---

## Execution Plan

| Phase | Tests | Gate | Target |
|-------|-------|------|--------|
| 1. Unit | Filter composition + AND, missing/has, ids format, orphan/stale + draft scoping, direction/predicate filtering, derived-inverse inclusion, depth vs closure, descendants/ancestors, default predicates, cycle guard, tree depth, chain order + limit, history-vs-log | Block PR | < 30s |
| 2. Integration | Four-verb command surface, empty-result success, unknown-field error, located lookup errors, `--in`/`--predicate` filtering, `--reverse` ancestors, no-impact message, cycle termination, `--limit` cap, `--format json` parity | Block PR | < 2min |
| 3. E2E | Filter an entry node then `neighbors` it both directions incl. a derived inverse; `impact` over a `depends_on` chain renders the tree; `history` over a supersession chain returns the lineage | Block merge | < 5min |

### CI Triggers

- **Pull request:** Phases 1-2 (fast feedback)
- **Main branch:** Phases 1-3 (full coverage, including the cross-surface walks)
- **Pre-release:** Phases 1-3 (no separate performance phase; the v1 in-memory index is rebuilt per call and is not yet a runtime hot path — see Risks)

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| FS-003 quotes HQ-only types/stages/predicates the v1 firm-ops preset drops (`--stage discovery`, `--type build`, `--predicate affects`, the `decision`/`supersedes` history surface) | TS-QRY-001-01/03, TS-QRY-003-02, all of STORY-QRY-004 would assert against entities the preset cannot produce | Pin scenarios to preset-valid types/stages and the universal `depends_on`; run the history surface against a generic `supersedes` fixture; treat FS-003 as the known-stale spec (preset is source of truth) and reconcile its examples before pinning the AC text verbatim |
| Located error messages (`No field 'vibe' on type 'opportunity'`, `No entity 'ghost' found`, `No entities match`, `No neighbors`, `No downstream impact`, `No supersession history`) are asserted verbatim and brittle to wording | TS-QRY-001-04, TS-QRY-002-03/04, TS-QRY-003-03, TS-QRY-004-03/04 break on cosmetic edits | Assert on the variable fields (field, id, type) plus a message substring, keeping the spec's exact text as the canonical example |
| A derived inverse (`superseded_by`, the inbound `engagement`/`client`) could be accidentally persisted, passing a read-time assertion while corrupting the store | TS-QRY-002-01/U03, TS-QRY-004-01/U03 validate the walk view but not the on-disk absence | Assert the inverse appears in the walk output AND is absent from the target entity's stored frontmatter on disk |
| Cycle safety relies on the visited-set guard; a regression loops indefinitely rather than failing fast | TS-QRY-003-04 could hang CI instead of reporting a failure | Run the cycle scenario under a per-test timeout and assert the returned set equals the cycle's node set exactly once |
| `neighbors --depth N` and `impact` could converge on the same code path, hiding the QRY-010 distinction (bounded all-predicate adjacency vs unbounded single-predicate closure) | TS-QRY-002-U04 gives false confidence that the two surfaces stay distinct | Assert `--depth` is bounded (stops at N hops across predicates) while `impact` is exhaustive on one predicate, on a graph where the two would return different sets |
| The in-memory v1 index is rebuilt from Markdown on every call; a large tree makes walks slow and the empty-result/`--limit` paths costly | Execution-plan targets slip as the seeded tree grows toward the live HQ scale (140+ transcripts) | Keep seeded trees minimal per scenario; track the SQLite/FTS fast-follow (deferred `search`/`path`) as the projection that retires per-call rebuilds |
