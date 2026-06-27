---
id: WPK-003-3
name: Compute Blast Radius and Trace Supersession History
feature-spec: plan/specs/FS-003-query-graph.md
test-spec: plan/tests/TS-003-query-graph.md
stories: [STORY-QRY-003, STORY-QRY-004]
test-scenarios: [TS-QRY-003-01, TS-QRY-003-02, TS-QRY-003-03, TS-QRY-003-04, TS-QRY-003-U01, TS-QRY-003-U02, TS-QRY-003-U03, TS-QRY-003-U04, TS-QRY-003-U05, TS-QRY-003-U06, TS-QRY-004-01, TS-QRY-004-02, TS-QRY-004-03, TS-QRY-004-04, TS-QRY-004-U01, TS-QRY-004-U02, TS-QRY-004-U03, TS-QRY-004-U04, TS-QRY-004-U05, TS-QRY-004-U06]
depends-on: [WPK-003-2]
updated: 2026-06-27
---

# Compute Blast Radius and Trace Supersession History

## Objective

Delivers the two transitive walks that extend one-hop adjacency into a closure and a chain: `khub impact <id>` (`core.impact`) and `khub history <id>` (`core.history`). `impact` walks the transitive forward closure over a predicate (default `depends_on`) via `networkx` descendants — or ancestors under `--reverse` — and renders the reachable set as a depth-marked tree; the walk is cycle-safe and visits each node once. `history` follows a self-referential supersession chain (default `supersedes`) back through prior records in order, includes the derived `superseded_by` direction, and caps with `--limit`. Both are reads over the projection rebuilt from Markdown; `history` reads the graph chain, never git.

> **Preset note:** firm-ops v1 declares no `decision` type and no `supersedes` / derived `superseded_by` predicates (HQ-only, dropped). The `history` scenarios run against a generic schema fixture declaring a self-referential `supersedes` with a derived `superseded_by` — the same engine pattern WPK-002-1 uses for derived inverses. `impact` runs over the universal `depends_on` edge (`any → any`); FS-003 wrote the HQ-only `affects` for the reverse example, so reverse over `depends_on` is the runnable equivalent.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-QRY-003 | AC-001 | Forward blast radius: `impact node-a --format tree` walks the transitive forward closure over `depends_on` (the default predicate), renders the reachable set as a tree, and marks depth from the source | TS-QRY-003-01 |
| STORY-QRY-003 | AC-002 | Reverse closure (ancestors): `impact node-c --reverse` walks ancestors (what reaches this node) instead of descendants, returns the upstream set `{node-b, node-a}`; `--predicate <p>` retargets the closure to a non-default predicate | TS-QRY-003-02 |
| STORY-QRY-003 | AC-003 | No reachable nodes: `impact leaf-node` (no outbound edge on the predicate) returns only the source node and displays `No downstream impact` | TS-QRY-003-03 |
| STORY-QRY-003 | AC-004 | Cycle safety: `impact node-in-cycle` (graph contains a cycle on the predicate) terminates without looping and returns each reachable node exactly once | TS-QRY-003-04 |
| STORY-QRY-004 | AC-001 | Walk the supersession chain: `history decision-0012` follows the `supersedes` edge (the default predicate) back through the chain, returns each prior record in order, and includes the derived `superseded_by` direction | TS-QRY-004-01 |
| STORY-QRY-004 | AC-002 | Limit the chain: `history decision-0012 --limit 3` returns at most the three most recent links | TS-QRY-004-02 |
| STORY-QRY-004 | AC-003 | No history: `history decision-0001` (supersedes nothing) returns only the source record and displays `No supersession history` | TS-QRY-004-03 |
| STORY-QRY-004 | AC-004 | Unknown id: `history ghost` returns a lookup error and displays `No entity 'ghost' found` | TS-QRY-004-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-QRY-003 | REQ-QRY003-01 | EARS-E | When impact runs, the system shall return the transitive forward closure over the predicate | TS-QRY-003-U01 |
| STORY-QRY-003 | REQ-QRY003-02 | EARS-O | Where `--reverse` is set, the system shall walk ancestors instead of descendants | TS-QRY-003-U02 |
| STORY-QRY-003 | REQ-QRY003-03 | EARS-S | While walking, the system shall visit each node once even if the graph has a cycle | TS-QRY-003-U03 |
| STORY-QRY-003 | REQ-QRY003-04 | EARS-O | Where `--format tree` is set, the system shall render depth from the source | TS-QRY-003-U05 |
| STORY-QRY-004 | REQ-QRY004-01 | EARS-E | When history runs, the system shall follow the supersession chain and return prior records in order | TS-QRY-004-U01 |
| STORY-QRY-004 | REQ-QRY004-02 | EARS-O | Where `--limit <n>` is set, the system shall cap the chain length | TS-QRY-004-U02 |
| STORY-QRY-004 | REQ-QRY004-03 | EARS-W | If the id does not resolve, then the system shall return a lookup error | TS-QRY-004-U05 |
| STORY-QRY-004 | REQ-QRY004-04 | EARS-U | The system shall distinguish `history` (graph chain) from `log` (git history) | TS-QRY-004-U06 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-QRY-003 | QRY-005 | The default impact predicate is `depends_on` | Constraint | TS-QRY-003-U01, TS-QRY-003-U04 |
| STORY-QRY-003 | QRY-006 | Traversal is cycle-safe; nodes are visited once | Constraint | TS-QRY-003-U03 |
| STORY-QRY-004 | QRY-007 | The default history predicate is `supersedes`; the inverse is derived | Constraint | TS-QRY-004-U01, TS-QRY-004-U04 |
| STORY-QRY-004 | QRY-008 | `history` reads the graph chain, not git; `log` reads git | Constraint | TS-QRY-004-U06 |
| STORY-QRY-003 | QRY-SHARED-001 | All reads operate on the derived projection, rebuilt from Markdown; no result is stored | Constraint | TS-QRY-003-01 |
| STORY-QRY-004 | QRY-SHARED-002 | Inbound walks include derived inverse edges (the `superseded_by` direction) | Constraint | TS-QRY-004-U03 |
| STORY-QRY-004 | QRY-SHARED-003 | Reads include `draft` entities by default; `--active` excludes drafts, `--draft` isolates them | Constraint | TS-QRY-004-01 |

---

## Entities

> Both walks operate over the projection's Node/Edge model. `impact` walks any predicate (default the universal `depends_on`); `history` walks a self-referential predicate (default `supersedes`) and surfaces its derived inverse.

### Edge

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Predicate | Text | Yes | The relation name (`depends_on` for impact; `supersedes` for history) |
| Source | Reference | Yes | The node the edge is stored on |
| Target | Reference | Yes | The resolved target node |
| Direction | Type | Yes | Outbound (stored) or inbound (including derived inverse, e.g. `superseded_by`) |
| Is Derived | Yes/No | Yes | True for inverse edges |

### Traversal Kind *(named enumeration)*

| Value | Description |
|-------|-------------|
| neighbors | One-hop (or `--depth`) adjacency (WPK-003-2) |
| impact | Transitive closure (blast radius) over a predicate — descendants, or ancestors under `--reverse` |
| history | Supersession chain over a self-referential predicate, returned in order |

### depends_on chain *(generic nodes, `impact` source)*

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Any type (the `depends_on` edge is universal, `any → any`) |
| depends_on | Reference | No | Outbound edge → any node; the predicate `impact` walks by default |

### supersession fixture *(generic, `history` source)*

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | A generic fixture type (firm-ops v1 declares none) |
| supersedes | Reference | No | Self-referential outbound edge → same type; the predicate `history` walks by default |
| superseded_by | Reference | No | Derived inverse of `supersedes`, computed at read time, never stored |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

Not applicable — `khub impact` and `khub history` are read-only traversals. They walk the derived projection (a closure and a chain respectively) and transition no entity state. Cycle handling is a traversal-safety concern (visited-set guard, QRY-006), not an entity state machine.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). Each story maps to one `khub` subcommand.

### STORY-QRY-003: Compute Blast Radius (`khub impact`)

- **Library verb:** `core.impact(id, predicate, reverse)` via `networkx` descendants/ancestors
- **Command:** `khub impact <id>`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| id | string (positional) | Yes | Bare slug, or `type/slug` to disambiguate |
| --predicate | string (default `depends_on`) | No | The edge to walk the closure over |
| --reverse | flag | No | Walk ancestors (what reaches this node) instead of descendants |
| --format | enum(tree, json) | No | Depth-marked tree (renders depth from the source) or JSON closure |

- **Reads:** the in-memory `networkx` projection (rebuilt from Markdown on demand)
- **Output (success):** the transitive closure over the predicate — descendants by default, ancestors under `--reverse` — rendered as a depth-marked tree or a JSON closure. No reachable node returns only the source: `No downstream impact`.
- **Errors:** (none beyond an empty closure; an unresolvable id surfaces the shared lookup error)

### STORY-QRY-004: Trace Supersession History (`khub history`)

- **Library verb:** `core.history(id, predicate)` over the supersession edge
- **Command:** `khub history <id>`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| id | string (positional) | Yes | Bare slug, or `type/slug` to disambiguate |
| --predicate | string (default `supersedes`) | No | The self-referential edge to follow |
| --limit | int | No | Cap the returned chain length to the most recent links |
| --format | enum(table, json) | No | Rich table on a TTY (default); JSON ordered chain otherwise |

- **Reads:** the in-memory projection (the supersession chain) — never git
- **Output (success):** each prior record in order, including the derived `superseded_by` direction; `--limit n` caps to the n most recent links. A record that supersedes nothing returns only the source: `No supersession history`.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | lookup_error | Id resolves to nothing — `No entity 'ghost' found` |

---

## Test Data

### depends_on chain (generic nodes)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `node-a → node-b → node-c` via universal `depends_on` | Forward closure + tree depth; reverse ancestors `{node-b, node-a}` (TS-QRY-003-01, -02) |
| Boundary | `leaf-node` with no outbound `depends_on` | `No downstream impact` (TS-QRY-003-03) |
| Boundary | `node-x → node-y → node-z → node-x` cycle | Cycle-safe visit-once (TS-QRY-003-04) |

### supersession chain (generic fixture)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | fixture type with self-referential `supersedes` + derived `superseded_by`; `decision-0012 → decision-0007 → decision-0001` | Chain order + derived inverse (TS-QRY-004-01) |
| Boundary | chain of four or more links rooted at `decision-0012` | `--limit 3` cap (TS-QRY-004-02) |
| Boundary | `decision-0001`, the chain root, no outbound `supersedes` | `No supersession history` (TS-QRY-004-03) |

### Missing lookups

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Missing | id `ghost` resolves to nothing | Lookup error on `history` (TS-QRY-004-04) |

---

## Implementation Notes

- Both commands are thin adapters over a core verb walking the `networkx` projection. `impact` selects `networkx` descendants by default and ancestors under `--reverse` (QRY-005 defaults the predicate to `depends_on`). `history` follows the self-referential `supersedes` edge back in order (QRY-007 defaults the predicate to `supersedes`).
- **Cycle safety (QRY-006) is the load-bearing guard:** a visited-set keeps each node to one visit and terminates on a cycle. A regression loops indefinitely and hangs CI rather than failing — run the cycle scenario under a per-test timeout and assert the returned set equals the cycle's node set exactly once.
- `--format tree` marks each node's depth from the source (QRY-003-04 / -U05). No reachable node returns only the source with `No downstream impact` — not an error.
- `history` includes the derived `superseded_by` direction (QRY-SHARED-002), computed from the stored `supersedes` edge, never persisted — assert it appears in the walk output **and** is absent from the target's stored frontmatter. `--limit n` caps to the n most recent links.
- `history` reads the graph chain only; `log` reads git (QRY-008). The history-vs-log distinction (TS-QRY-004-U06) asserts `history` never touches git — git is not exercised here.
- **Preset reconciliation:** `history` runs against a generic schema fixture declaring `supersedes`/`superseded_by` (firm-ops v1 carries neither). `impact` reverse runs over the universal `depends_on` (FS-003's `affects` is HQ-only, dropped). Treat FS-003 as the known-stale spec — the preset is source of truth.
- Located errors are brittle to wording — assert on the id substring plus the variable, keeping the exact text (`No entity 'ghost' found`, `No downstream impact`, `No supersession history`) as the canonical example.

---

## Done Criteria

- [ ] All 8 acceptance criteria pass (QRY-003 AC-001 through AC-004; QRY-004 AC-001 through AC-004)
- [ ] All 8 EARS requirements tested (REQ-QRY003-01 through -04; REQ-QRY004-01 through -04)
- [ ] All 4 business rules enforced (QRY-005, QRY-006, QRY-007, QRY-008), plus shared QRY-SHARED-001/002/003 where they apply
- [ ] All 20 test scenarios pass (TS-QRY-003-01 through -04 + U01–U06; TS-QRY-004-01 through -04 + U01–U06)
- [ ] Cycle scenario terminates under a per-test timeout and returns each node exactly once
- [ ] `impact --reverse` returns the ancestor set `{node-b, node-a}`, distinct from the forward closure
- [ ] Derived `superseded_by` shows in `history` output yet is absent from the target's stored frontmatter on disk
- [ ] `impact` over a seeded `depends_on` chain renders the depth-marked tree; `history` over a seeded supersession chain returns the lineage (E2E)
- [ ] No regressions in existing tests
