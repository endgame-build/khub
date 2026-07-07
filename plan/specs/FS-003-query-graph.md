---
id: FS-003
name: Query & Graph
priority: High
dependencies: [FS-002]
updated: 2026-07-07
---

# Query & Graph

## Overview

The read and traversal surface — the agent's primary retrieval path into typed context. `query` filters entities by frontmatter; `neighbors`, `impact`, and `history` walk the in-memory `networkx` index for one-hop adjacency, blast radius, and supersession chains. The graph is a derived projection, rebuilt from the Markdown on demand, so every walk reflects the live tree. Full-text `search` follows the same grain: an in-memory SQLite FTS5 index over title + full body, built per invocation (shipped 2026-07-07) — no persisted `.db`, so no staleness.

**Primary Actor:** Agent

**Depends on:** FS-002: Authoring

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-QRY-001 | Filter Entities by Frontmatter | Agent, Operator |
| STORY-QRY-002 | Walk One-Hop Neighbors | Agent |
| STORY-QRY-003 | Compute Blast Radius | Agent |
| STORY-QRY-004 | Trace Supersession History | Agent |

---

## Stories

---

### STORY-QRY-001: Filter Entities by Frontmatter

**As an** Agent or Operator
**I want to** filter entities by type, status, field values, and relation presence
**So that** I can pull a precise slice of typed context, including gaps via `--missing`

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The in-memory index is built from the entity tree

#### Acceptance Criteria

##### AC-001: Filter by Type and Field

**Given** a firm-ops workspace with opportunities
**When** the agent runs `khub query --type opportunity --stage prospect --format json`
**Then** the system shall:
- [ ] Return only `opportunity` entities at stage `prospect`
- [ ] Annotate each match with its `orphan` and `stale` flags by default
- [ ] Emit each match as JSON when `--format json` is set
- [ ] Apply `--limit` when given

##### AC-002: Surface Gaps With Missing

**Given** entities that lack a required relation
**When** the agent runs `khub query --type project --missing owner`
**Then** the system shall:
- [ ] Return projects with no resolvable `owner` edge
- [ ] Support `--has <pred>` as the inverse filter

##### AC-003: Empty Result

**Given** no entity matches the filter
**When** the agent runs `khub query --type opportunity --stage lost`
**Then** the system shall:
- [ ] Return an empty set, not an error
- [ ] Display: "No entities match" on a TTY
- [ ] Emit `[]` under `--format json`

##### AC-004: Unknown Filter Field

**Given** a filter naming an undeclared field
**When** the agent runs `khub query --type opportunity --vibe high`
**Then** the system shall:
- [ ] Return a filter error
- [ ] Display: "No field 'vibe' on type 'opportunity'"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-QRY001-01 | EARS-E | When query runs with filters, the system shall return only entities matching every filter |
| REQ-QRY001-02 | EARS-O | Where `--missing <pred>` is set, the system shall return entities lacking a resolvable edge for that predicate |
| REQ-QRY001-03 | EARS-W | If a filter names an undeclared field, then the system shall return a filter error |
| REQ-QRY001-04 | EARS-O | Where `--format ids` is set, the system shall emit bare ids for piping |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| QRY-001 | Filters match against frontmatter and derived edges, never body prose | Constraint |
| QRY-002 | An empty result is a success, not an error | Constraint |
| QRY-009 | Query output carries each entity's `orphan` and `stale` flags by default; `--orphan`/`--stale` filter on them | Constraint |

#### Technical Notes

- **Command:** `khub query` — `--type`, `--draft` / `--active`, `--orphan`, `--stale`, `--<field>`, `--tag`, `--has`, `--missing`, `--limit`, `--format json\|table\|ids`
- **Library verb:** `core.query(filters)`
- **Entities:** any type
- **Invariant upheld:** the graph is a derived projection (Principle 2)
- **Output:** Rich table, JSON, or bare ids

#### Test Hints

- **Unit:** filter composition, `--missing` / `--has` logic
- **Integration:** query against a seeded firm-ops tree
- **E2E:** `query --missing owner` finds the seeded incomplete project

---

### STORY-QRY-002: Walk One-Hop Neighbors

**As an** Agent
**I want to** list an entity's adjacent nodes by predicate and direction
**So that** I can navigate the typed graph one hop at a time, inbound or outbound

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The source id resolves to one entity

#### Acceptance Criteria

##### AC-001: List Neighbors in Both Directions

**Given** an entity with inbound and outbound edges
**When** the agent runs `khub neighbors initech`
**Then** the system shall:
- [ ] Return outbound edges (the entity's own relations)
- [ ] Return inbound edges (entities that point at it, including derived inverses)
- [ ] Label each neighbor with its predicate and direction

##### AC-002: Filter by Predicate and Direction

**Given** an entity with edges across several predicates
**When** the agent runs `khub neighbors initech --predicate client --in`
**Then** the system shall:
- [ ] Return only inbound `client` edges (opportunities and projects for that client)
- [ ] Honor `--depth` for multi-hop adjacency

##### AC-003: Isolated Entity

**Given** an entity with no resolvable edges
**When** the agent runs `khub neighbors lonely-client`
**Then** the system shall:
- [ ] Return an empty neighbor set
- [ ] Display: "No neighbors"

##### AC-004: Unknown Id

**Given** the id does not resolve
**When** the agent runs `khub neighbors ghost`
**Then** the system shall:
- [ ] Return a lookup error
- [ ] Display: "No entity 'ghost' found"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-QRY002-01 | EARS-E | When neighbors runs, the system shall return adjacent nodes labeled by predicate and direction |
| REQ-QRY002-02 | EARS-O | Where `--in`, `--out`, or `--predicate` is set, the system shall filter adjacency accordingly |
| REQ-QRY002-03 | EARS-W | If the id does not resolve, then the system shall return a lookup error |
| REQ-QRY002-04 | EARS-S | While walking inbound edges, the system shall include derived inverses |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| QRY-003 | Inbound adjacency includes derived inverses, computed not stored | Constraint |
| QRY-004 | Default direction is both; `--in` / `--out` narrow it | Constraint |
| QRY-010 | `neighbors --depth N` is bounded multi-hop adjacency over all predicates; `impact` is the unbounded single-predicate closure — distinct surfaces | Constraint |

#### Technical Notes

- **Command:** `khub neighbors <id>` — `--predicate`, `--in` / `--out` / `--both`, `--depth <n=1>`, `--format`
- **Library verb:** `core.neighbors(id, predicate, direction, depth)` over `networkx`
- **Entities:** any type
- **Invariant upheld:** relations are authoritative from two sources
- **Output:** Rich table or JSON adjacency list

#### Test Hints

- **Unit:** direction filtering, predicate filtering
- **Integration:** inbound inverse inclusion; `--depth 2` adjacency
- **E2E:** `neighbors <client> --in` lists that client's pipeline

---

### STORY-QRY-003: Compute Blast Radius

**As an** Agent
**I want to** walk the transitive closure over an edge from an entity
**So that** I can see everything a change touches before I make it

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The source id resolves to one entity

#### Acceptance Criteria

##### AC-001: Forward Blast Radius

**Given** an entity with a transitive `depends_on` chain
**When** the agent runs `khub impact some-node --format tree`
**Then** the system shall:
- [ ] Walk the transitive forward closure over the predicate (default `depends_on`)
- [ ] Render the reachable set as a tree
- [ ] Mark depth from the source

##### AC-002: Reverse Closure (Ancestors)

**Given** an entity reached by a `references` chain
**When** the agent runs `khub impact some-component --predicate references --reverse`
**Then** the system shall:
- [ ] Walk ancestors (what reaches this node) instead of descendants
- [ ] Return the upstream set

##### AC-003: No Reachable Nodes

**Given** an entity with no outbound edge on the predicate
**When** the agent runs `khub impact leaf-node`
**Then** the system shall:
- [ ] Return only the source node
- [ ] Display: "No downstream impact"

##### AC-004: Cycle Safety

**Given** a graph that contains a cycle on the predicate
**When** the agent runs `khub impact node-in-cycle`
**Then** the system shall:
- [ ] Terminate without looping
- [ ] Return each reachable node once

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-QRY003-01 | EARS-E | When impact runs, the system shall return the transitive forward closure over the predicate |
| REQ-QRY003-02 | EARS-O | Where `--reverse` is set, the system shall walk ancestors instead of descendants |
| REQ-QRY003-03 | EARS-S | While walking, the system shall visit each node once even if the graph has a cycle |
| REQ-QRY003-04 | EARS-O | Where `--format tree` is set, the system shall render depth from the source |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| QRY-005 | The default impact predicate is `depends_on` | Constraint |
| QRY-006 | Traversal is cycle-safe; nodes are visited once | Constraint |

#### Technical Notes

- **Command:** `khub impact <id>` — `--predicate <p=depends_on>`, `--reverse`, `--format tree\|json`
- **Library verb:** `core.impact(id, predicate, reverse)` via `networkx` descendants/ancestors
- **Entities:** any type; `depends_on` (default), `references` (firm-ops v1 dropped the HQ-only `affects`)
- **Invariant upheld:** the graph is derived; blast radius is computed on demand
- **Output:** tree or JSON closure

#### Test Hints

- **Unit:** descendants/ancestors selection
- **Integration:** cycle termination; depth marking
- **E2E:** `impact` over a seeded `depends_on` chain returns the full reachable set

---

### STORY-QRY-004: Trace Supersession History

**As an** Agent
**I want to** follow the supersession chain from a decision
**So that** I can read decision lineage without scanning files

> **Preset note:** firm-ops v1 declares no `decision` type and no `supersedes` / derived `superseded_by` predicates — all dropped, 0 live entities. The `history` walk is an engine capability over any self-referential edge; firm-ops v1 demonstrates none natively, so the examples below describe a generic fixture chain (the engine follows supersession regardless of preset). See `docs/firm-ops-preset.md`.

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The source id resolves to one entity

#### Acceptance Criteria

##### AC-001: Walk the Supersession Chain

**Given** a decision that supersedes an earlier one
**When** the agent runs `khub history decision-0012`
**Then** the system shall:
- [ ] Follow the `supersedes` edge (default predicate) back through the chain
- [ ] Return each prior decision in order
- [ ] Include the derived `superseded_by` direction

##### AC-002: Limit the Chain

**Given** a long chain
**When** the agent runs `khub history decision-0012 --limit 3`
**Then** the system shall:
- [ ] Return at most the three most recent links

##### AC-003: No History

**Given** a decision that supersedes nothing
**When** the agent runs `khub history decision-0001`
**Then** the system shall:
- [ ] Return only the source decision
- [ ] Display: "No supersession history"

##### AC-004: Unknown Id

**Given** the id does not resolve
**When** the agent runs `khub history ghost`
**Then** the system shall:
- [ ] Return a lookup error
- [ ] Display: "No entity 'ghost' found"

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-QRY004-01 | EARS-E | When history runs, the system shall follow the supersession chain and return prior records in order |
| REQ-QRY004-02 | EARS-O | Where `--limit <n>` is set, the system shall cap the chain length |
| REQ-QRY004-03 | EARS-W | If the id does not resolve, then the system shall return a lookup error |
| REQ-QRY004-04 | EARS-U | The system shall distinguish `history` (graph chain) from `log` (git history) |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| QRY-007 | The default history predicate is `supersedes`; the inverse is derived | Constraint |
| QRY-008 | `history` reads the graph chain, not git; `log` reads git | Constraint |

#### Technical Notes

- **Command:** `khub history <id>` — `--predicate <p=supersedes>`, `--limit`, `--format`
- **Library verb:** `core.history(id, predicate)` over the supersession edge
- **Entities:** any self-referential edge (firm-ops v1 declares none; the engine supports it generically)
- **Invariant upheld:** relations are authoritative; inverse derived
- **Output:** ordered chain as table or JSON

#### Test Hints

- **Unit:** chain ordering, limit handling
- **Integration:** derived `superseded_by` inclusion
- **E2E:** `history` over a seeded supersession chain returns the lineage

---

## Shared Context

### Entities

Query and traversal read the same firm-ops nodes and edges that authoring writes. The graph model below is what the walks operate over; full type capture lives in `docs/firm-ops-preset.md`.

| Entity | Description |
|--------|-------------|
| Node | One entity in the in-memory index, keyed by `(type, slug)` |
| Edge | One relation: a predicate from a source node to a target node |
| Query Result | The filtered set returned by `query` |
| Walk Result | The adjacency, closure, or chain returned by a traversal |

#### Edge

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Predicate | Text | Yes | The relation name (one of 14 in firm-ops) |
| Source | Reference | Yes | The node the edge is stored on |
| Target | Reference | Yes | The resolved target node |
| Direction | Type | Yes | Outbound (stored) or inbound (including derived inverse) |
| Cardinality | Type | Yes | Single or many, per the schema |
| Is Derived | Yes/No | Yes | True for inverse edges |

#### Query Result

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Matches | Collection | Yes | The entities passing every filter |
| Count | Number | Yes | Size of the match set |
| Format | Type | Yes | table, json, or ids |

### Edge Direction *(named enumeration)*

| Value | Description |
|-------|-------------|
| out | The edge is stored on the source entity's frontmatter |
| in | The edge points at the entity, including derived inverses |
| both | The default; inbound and outbound combined |

### Traversal Kind *(named enumeration)*

| Value | Description |
|-------|-------------|
| neighbors | One-hop (or `--depth`) adjacency |
| impact | Transitive closure (blast radius) over a predicate |
| history | Supersession chain over a self-referential predicate |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| Source entity removed | Its edges drop from the next walk | The graph is rebuilt from Markdown on demand |
| Forward edge unlinked | Inbound walks lose the derived inverse | Inverses are computed, never stored |
| Cycle on the walked predicate | Each node visited once | Traversal is cycle-safe |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| QRY-SHARED-001 | All reads operate on the derived projection, rebuilt from Markdown; no result is stored | STORY-QRY-001, STORY-QRY-002, STORY-QRY-003, STORY-QRY-004 |
| QRY-SHARED-002 | Inbound walks include derived inverse edges | STORY-QRY-002, STORY-QRY-004 |
| QRY-SHARED-003 | All reads include `draft` entities in scope by default and carry each entity's `stale` (and `orphan`) flag; `--active` excludes drafts, `--draft` isolates them. `draft` is a manual publish flag (FS-002), orthogonal to completeness — `--missing` surfaces incompleteness regardless of draft state | STORY-QRY-001, STORY-QRY-002, STORY-QRY-003, STORY-QRY-004 |

### Cross-Feature Dependencies

| Entity | Source Feature | Usage |
|--------|---------------|-------|
| Node / Edge | FS-002: Authoring | Authoring writes the entities and forward edges that every walk reads |
| Schema | FS-001: Workspace & Schema | Predicate legality and target types come from the schema |
| `draft` flag | FS-002: Authoring | Manual publish flag behind `--draft`/`--active`; orthogonal to the `--missing` completeness filter |

### Cross-Story Dependencies

```
STORY-QRY-001 (Filter Entities)
    └── STORY-QRY-002 (Walk One-Hop Neighbors)
            ├── STORY-QRY-003 (Compute Blast Radius)
            └── STORY-QRY-004 (Trace Supersession History)
```

Filtering finds an entry node; neighbors walks one hop from it; impact and history extend that walk into a transitive closure and a typed chain.

## Scope Changes

### Deferred Stories

| Story | Moved To | Reason | Date |
|-------|----------|--------|------|
| Full-text search (`khub search`) | Fast-follow | Needs the SQLite/FTS5 projection; the in-memory v1 index has no full-text | 2026-06-20 |
| Full-text search (`khub search`) | **Shipped** (TS-SRCH-001) | Landed as an in-memory-per-invocation FTS5 projection — no persisted `.db` needed at current scale | 2026-07-07 |
| Shortest path (`khub path <from> <to>`) | Fast-follow | Pathfinding lands with the materialized projection | 2026-06-20 |
