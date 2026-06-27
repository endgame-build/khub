---
id: WPK-003-2
name: Walk One-Hop Neighbors
feature-spec: plan/specs/FS-003-query-graph.md
test-spec: plan/tests/TS-003-query-graph.md
stories: [STORY-QRY-002]
test-scenarios: [TS-QRY-002-01, TS-QRY-002-02, TS-QRY-002-03, TS-QRY-002-04, TS-QRY-002-U01, TS-QRY-002-U02, TS-QRY-002-U03, TS-QRY-002-U04, TS-QRY-002-U05, TS-QRY-002-U06]
depends-on: [WPK-003-1]
updated: 2026-06-27
---

# Walk One-Hop Neighbors

## Objective

Delivers `khub neighbors <id>` (`core.neighbors`) — the one-hop adjacency walk over the `networkx` projection. From a resolved id it returns outbound edges (the entity's own relations) and inbound edges (entities that point at it, including derived inverses computed from the stored forward edge, never read from disk), each labeled with its predicate and direction. `--predicate`, `--in`/`--out`/`--both`, and `--depth` narrow and extend the walk; the default direction is both. An isolated entity returns an empty set, an unresolvable id a located lookup error.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-QRY-002 | AC-001 | List neighbors in both directions: `neighbors initech-pov` returns outbound edges (its own `client`/`owner` relations) and inbound edges (a meeting's `engagement` resolving to it, surfaced as a derived inverse), labeling each neighbor with its predicate and direction | TS-QRY-002-01 |
| STORY-QRY-002 | AC-002 | Filter by predicate and direction: `neighbors initech --predicate client --in` returns only inbound `client` edges (the opportunities and projects naming `initech` as client); `--depth 2` extends adjacency to the second hop over all predicates | TS-QRY-002-02 |
| STORY-QRY-002 | AC-003 | Isolated entity: `neighbors lonely-client` (no resolvable edges) returns an empty neighbor set and displays `No neighbors` | TS-QRY-002-03 |
| STORY-QRY-002 | AC-004 | Unknown id: `neighbors ghost` returns a lookup error and displays `No entity 'ghost' found` | TS-QRY-002-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-QRY-002 | REQ-QRY002-01 | EARS-E | When neighbors runs, the system shall return adjacent nodes labeled by predicate and direction | TS-QRY-002-U06 |
| STORY-QRY-002 | REQ-QRY002-02 | EARS-O | Where `--in`, `--out`, or `--predicate` is set, the system shall filter adjacency accordingly | TS-QRY-002-U01 |
| STORY-QRY-002 | REQ-QRY002-03 | EARS-W | If the id does not resolve, then the system shall return a lookup error | TS-QRY-002-U05 |
| STORY-QRY-002 | REQ-QRY002-04 | EARS-S | While walking inbound edges, the system shall include derived inverses | TS-QRY-002-U03 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-QRY-002 | QRY-003 | Inbound adjacency includes derived inverses, computed not stored | Constraint | TS-QRY-002-U03 |
| STORY-QRY-002 | QRY-004 | Default direction is both; `--in` / `--out` narrow it | Constraint | TS-QRY-002-U01 |
| STORY-QRY-002 | QRY-010 | `neighbors --depth N` is bounded multi-hop adjacency over all predicates; `impact` is the unbounded single-predicate closure — distinct surfaces | Constraint | TS-QRY-002-U04 |
| STORY-QRY-002 | QRY-SHARED-001 | All reads operate on the derived projection, rebuilt from Markdown; no result is stored | Constraint | TS-QRY-002-01 |
| STORY-QRY-002 | QRY-SHARED-002 | Inbound walks include derived inverse edges | Constraint | TS-QRY-002-U03 |
| STORY-QRY-002 | QRY-SHARED-003 | Reads include `draft` entities by default; `--active` excludes drafts, `--draft` isolates them | Constraint | TS-QRY-002-01 |

---

## Entities

> The walk operates over the projection's Node/Edge model. Inbound edges include derived inverses — computed from the stored forward edge, never persisted.

### Edge

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Predicate | Text | Yes | The relation name (one of the firm-ops predicates, e.g. `client`, `owner`, `engagement`) |
| Source | Reference | Yes | The node the edge is stored on |
| Target | Reference | Yes | The resolved target node |
| Direction | Type | Yes | Outbound (stored) or inbound (including derived inverse) |
| Cardinality | Type | Yes | Single or many, per the schema |
| Is Derived | Yes/No | Yes | True for inverse edges |

### Edge Direction *(named enumeration)*

| Value | Description |
|-------|-------------|
| out | The edge is stored on the source entity's frontmatter |
| in | The edge points at the entity, including derived inverses |
| both | The default; inbound and outbound combined |

### project *(walk source)*

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `project` |
| Client | Reference | Yes | Outbound edge → client (`initech-pov → initech`) |
| Owner | Reference | Yes | Outbound edge → person (`initech-pov → noor`) |

### meeting *(inbound-edge source)*

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `meeting` |
| Engagement | Reference | Yes | Outbound edge → opportunity \| project \| partnership; resolving to `initech-pov` it is the project's inbound (derived-inverse) edge |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

Not applicable — `khub neighbors` is a read-only traversal. It walks the derived projection and transitions no entity state.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). The story maps to one `khub` subcommand.

### STORY-QRY-002: Walk Neighbors (`khub neighbors`)

- **Library verb:** `core.neighbors(id, predicate, direction, depth)` over `networkx`
- **Command:** `khub neighbors <id>`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| id | string (positional) | Yes | Bare slug, or `type/slug` to disambiguate |
| --predicate | string | No | Restrict adjacency to edges of that predicate (e.g. `client`) |
| --in / --out / --both | flag | No | Narrow direction; default is `both` |
| --depth | int (default 1) | No | Bounded multi-hop adjacency over all predicates |
| --format | enum(table, json) | No | Rich table on a TTY (default); JSON adjacency list otherwise |

- **Reads:** the in-memory `networkx` projection (rebuilt from Markdown on demand)
- **Output (success):** adjacent nodes labeled by predicate and direction — outbound (stored) plus inbound (including derived inverses) — as a Rich table on a TTY or a JSON adjacency list. An isolated entity returns an empty set: `No neighbors`.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | lookup_error | Id resolves to nothing — `No entity 'ghost' found` |

---

## Test Data

### project (walk source)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-pov` with outbound `client → initech`, `owner → noor` | Outbound neighbors (TS-QRY-002-01) |

### meeting (inbound edge)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `engagement → initech-pov` | Provides the inbound (derived-inverse) edge on the project (TS-QRY-002-01) |

### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech` with two inbound `client` edges (`project/initech-pov` and one `opportunity`); a second hop reachable from one | Inbound predicate/direction filter, `--depth 2` (TS-QRY-002-02) |
| Boundary | `lonely-client` with no inbound or outbound edges | Isolated entity, `No neighbors` (TS-QRY-002-03) |

> FS-003 wrote `lone-fragment`; a firm-ops `fragment` always carries a required `owner` edge, so a relation-free `client` is the truly isolated equivalent.

### Missing lookups

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Missing | id `ghost` resolves to nothing | Lookup error (TS-QRY-002-04) |

---

## Implementation Notes

- `neighbors` is a thin adapter over `core.neighbors(...)` walking the `networkx` projection. Outbound edges are the entity's stored relations; inbound edges are computed — for each stored forward edge pointing at the source, the walk surfaces the derived inverse (QRY-003 / QRY-SHARED-002). The derived inverse is never written to disk: assert it appears in the walk output **and** is absent from the target entity's stored frontmatter.
- Default direction is `both` (QRY-004); `--in`/`--out` narrow it; `--predicate` restricts to one relation. Label every returned neighbor with predicate and direction.
- `--depth N` is bounded multi-hop adjacency over **all** predicates, distinct from `impact`'s unbounded single-predicate closure (QRY-010, WPK-003-3). Keep the two on separate code paths; test on a graph where bounded-all-predicate and unbounded-one-predicate would return different sets, so the distinction cannot collapse.
- An isolated entity returns an empty set with `No neighbors` — not an error. An unresolvable id returns a located lookup error (`No entity 'ghost' found`); assert on the id substring plus the variable, keeping the exact text as the canonical example.
- Output branches: Rich table on a TTY, JSON adjacency list otherwise; drive both via `CliRunner` / Rich `force_terminal`.

---

## Done Criteria

- [ ] All 4 acceptance criteria pass (QRY-002 AC-001 through AC-004)
- [ ] All 4 EARS requirements tested (REQ-QRY002-01 through -04)
- [ ] All 3 business rules enforced (QRY-003, QRY-004, QRY-010), plus shared QRY-SHARED-001/002/003 where they apply
- [ ] All 10 test scenarios pass (TS-QRY-002-01 through -04 + U01–U06)
- [ ] Inbound derived inverse (the meeting's `engagement`) shows in `neighbors` output yet is absent from `initech-pov`'s stored frontmatter on disk
- [ ] `--depth 2` returns a bounded all-predicate set distinct from a single-predicate `impact` closure on the same graph
- [ ] `neighbors <client> --in` lists that client's inbound pipeline (E2E)
- [ ] No regressions in existing tests
