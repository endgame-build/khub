---
id: WPK-003-1
name: Filter Entities by Frontmatter
feature-spec: plan/specs/FS-003-query-graph.md
test-spec: plan/tests/TS-003-query-graph.md
stories: [STORY-QRY-001]
test-scenarios: [TS-QRY-001-01, TS-QRY-001-02, TS-QRY-001-03, TS-QRY-001-04, TS-QRY-001-U01, TS-QRY-001-U02, TS-QRY-001-U03, TS-QRY-001-U04, TS-QRY-001-U05, TS-QRY-001-U06, TS-QRY-001-U07]
depends-on: [WPK-001-2, WPK-002-1]
updated: 2026-06-27
---

# Filter Entities by Frontmatter

## Objective

Delivers `khub query` (`core.query`) — the read surface that pulls a precise slice of typed context by ANDing filters over frontmatter and derived edges. One command composes `--type`, `--<field>`, `--tag`, `--has <pred>`, and `--missing <pred>` (the gap finder) against the in-memory projection, carries each match's `orphan` and `stale` flags by default, and emits Rich table, JSON, or bare ids. An empty result is a success, not an error; an undeclared filter field is a located error. Filters read the projection rebuilt from Markdown on demand — never body prose, never a stored result.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-QRY-001 | AC-001 | Filter by type and field: `query --type opportunity --stage prospect --format json` returns only `opportunity` entities at stage `prospect`, annotates each match with its `orphan` and `stale` flags by default, emits each match as JSON under `--format json`, and applies `--limit` when given | TS-QRY-001-01 |
| STORY-QRY-001 | AC-002 | Surface gaps with `--missing`: `query --type project --missing owner` returns only projects with no resolvable `owner` edge; `--has owner` returns the inverse set (projects whose `owner` resolves); the gap surfaces regardless of `draft` state | TS-QRY-001-02 |
| STORY-QRY-001 | AC-003 | Empty result is success: `query --type opportunity --stage lost` (no match) returns an empty set with exit 0, displays `No entities match` on a TTY, and emits `[]` under `--format json` | TS-QRY-001-03 |
| STORY-QRY-001 | AC-004 | Unknown filter field: `query --type opportunity --vibe high` returns a filter error and displays `No field 'vibe' on type 'opportunity'` | TS-QRY-001-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-QRY-001 | REQ-QRY001-01 | EARS-E | When query runs with filters, the system shall return only entities matching every filter | TS-QRY-001-U01 |
| STORY-QRY-001 | REQ-QRY001-02 | EARS-O | Where `--missing <pred>` is set, the system shall return entities lacking a resolvable edge for that predicate | TS-QRY-001-U02 |
| STORY-QRY-001 | REQ-QRY001-03 | EARS-W | If a filter names an undeclared field, then the system shall return a filter error | TS-QRY-001-U03 |
| STORY-QRY-001 | REQ-QRY001-04 | EARS-O | Where `--format ids` is set, the system shall emit bare ids for piping | TS-QRY-001-U04 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-QRY-001 | QRY-001 | Filters match against frontmatter and derived edges, never body prose | Constraint | TS-QRY-001-U01, TS-QRY-001-U03 |
| STORY-QRY-001 | QRY-002 | An empty result is a success, not an error | Constraint | TS-QRY-001-U06 |
| STORY-QRY-001 | QRY-009 | Query output carries each entity's `orphan` and `stale` flags by default; `--orphan`/`--stale` filter on them | Constraint | TS-QRY-001-U05 |
| STORY-QRY-001 | QRY-SHARED-001 | All reads operate on the derived projection, rebuilt from Markdown; no result is stored | Constraint | TS-QRY-001-01 |
| STORY-QRY-001 | QRY-SHARED-003 | Reads include `draft` entities by default and carry the `stale`/`orphan` flag; `--active` excludes drafts, `--draft` isolates them; `--missing` surfaces incompleteness regardless of draft | Constraint | TS-QRY-001-U07 |

---

## Entities

> Query operates over the projection's Node/Edge model; the concrete `opportunity`/`project` field sets below are what `--stage` and `--missing owner` introspect. Field legality and predicate targets come from the compiled schema (FS-001), not hardcoded.

### Query Result

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Matches | Collection | Yes | The entities passing every filter |
| Count | Number | Yes | Size of the match set |
| Format | Type | Yes | `table`, `json`, or `ids` |

### Edge *(resolved for `--missing` / `--has`)*

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Predicate | Text | Yes | The relation name (e.g. `owner`) |
| Source | Reference | Yes | The node the edge is stored on |
| Target | Reference | Yes | The resolved target node — absent when the edge is unresolvable (`--missing` hit) |
| Is Derived | Yes/No | Yes | True for inverse edges |

### opportunity *(field filter target)*

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `opportunity` |
| Stage | Status | Yes | Pipeline stage enum (`--stage` filter target) |
| Client | Reference | Yes | Edge → client |
| Owner | Reference | Yes | Edge → person |

### project *(`--missing owner` target)*

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `project` |
| Client | Reference | Yes | Edge → client |
| Owner | Reference | Yes | Edge → person (the predicate `--missing owner` / `--has owner` resolves) |

### Opportunity Stage *(named enumeration)*

> The CRM "Sales" pipeline stages — the firm-ops v1 preset (FS-003 wrote the HQ-only `discovery`/`build`; `prospect` and `lost` are the runnable equivalents).

| Value | Description |
|-------|-------------|
| prospect | Early-stage lead (happy-path filter) |
| proposal-sent | Proposal issued |
| won | Deal won |
| signed | Contract signed |
| lost | Did not convert (empty-result boundary) |

### Lifecycle Flag *(`draft` boolean, FS-002)*

| Value | Description |
|-------|-------------|
| `draft: false` | Active (default); included in query scope |
| `draft: true` | Manually unpublished; included by default, isolated by `--draft`, excluded by `--active`; orthogonal to `--missing` completeness |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

Not applicable — `khub query` is a read-only surface. It filters the derived projection and transitions no entity state.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). The story maps to one `khub` subcommand.

### STORY-QRY-001: Filter Entities (`khub query`)

- **Library verb:** `core.query(filters)`
- **Command:** `khub query`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| --type | string | No | Restrict to one entity type (e.g. `opportunity`, `project`) |
| --\<field\> \<value\> | repeatable | No | Frontmatter field filter (e.g. `--stage prospect`); ANDs with every other filter |
| --tag | string | No | Filter on a tag value |
| --has | predicate | No | Keep entities with a resolvable edge for the predicate |
| --missing | predicate | No | Keep entities with no resolvable edge for the predicate (gap finder) |
| --orphan | flag | No | Filter on the `orphan` projection flag |
| --stale | flag | No | Filter on the `stale` projection flag |
| --draft / --active | flag | No | `--draft` isolates drafts; `--active` excludes them; default includes both |
| --limit | int | No | Cap the returned set |
| --format | enum(table, json, ids) | No | Rich table on a TTY (default); `json` machine-readable; `ids` bare ids one per line for piping |

- **Reads:** the entity tree and the in-memory projection (orphan/stale computed identically to `status`, WPK-001-2)
- **Output (success):** matches passing every filter, each annotated with `orphan`/`stale` — Rich table on a TTY, JSON under `--format json`, bare ids under `--format ids`. No match returns an empty set (exit 0): `No entities match` on a TTY, `[]` under `--format json`.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | filter_error | Filter names a field the type does not declare — `No field 'vibe' on type 'opportunity'` |

---

## Test Data

### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | several at `stage` `prospect` / `proposal-sent` / `won` | Type-and-field filter discriminates (TS-QRY-001-01) |
| Boundary | no opportunity at `stage` `lost` | Empty-result success (TS-QRY-001-03) |
| Invalid | undeclared field `vibe` against `opportunity` | Unknown-field filter error (TS-QRY-001-04) |

### project

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Invalid | draft `project` saved missing required `owner` | `--missing owner` surfaces the gap regardless of draft (TS-QRY-001-02) |
| Valid | active `project` whose `owner → noor` resolves | `--has owner` inverse set (TS-QRY-001-02) |

---

## Implementation Notes

- `query` is a thin adapter over `core.query(filters)`; the compiled schema gates field legality (the `--vibe` error introspects the type's declared fields, WPK-001-2 layer — never hardcoded). Filters AND together: a match passes type, field, tag, `--has`/`--missing`, and flag filters simultaneously.
- `--missing <pred>` / `--has <pred>` resolve the edge against the projection: `--missing owner` keeps entities whose `owner` edge does not resolve (gap), `--has owner` keeps those whose `owner` resolves. The gap is orthogonal to `draft` (QRY-SHARED-003) — a draft project missing `owner` still surfaces under `--missing owner`; `draft` is a manual publish flag (FS-002), not a completeness signal.
- `orphan` and `stale` are core projection properties computed identically for `status`, `query`, and `check` (reuse the WPK-001-2 projection, do not recompute). Every match carries both flags by default; `--orphan`/`--stale` filter on them.
- Filters match frontmatter and derived edges only, never body prose (QRY-001). Assert a body-only string match returns nothing.
- An empty result is a success (QRY-002): return an empty collection with exit 0, never raise. `No entities match` on a TTY, `[]` under `--format json`.
- Output branches: Rich table on a TTY, JSON under `--format json` (field parity with the table), bare ids one per line under `--format ids`. Drive both branches deterministically via `CliRunner` / Rich `force_terminal`.
- Located errors are brittle to wording — assert on the variable fields (field name, type name) plus a message substring, keeping the exact text above as the canonical example.

---

## Done Criteria

- [ ] All 4 acceptance criteria pass (QRY-001 AC-001 through AC-004)
- [ ] All 4 EARS requirements tested (REQ-QRY001-01 through -04)
- [ ] All 3 business rules enforced (QRY-001, QRY-002, QRY-009), plus shared QRY-SHARED-001 and QRY-SHARED-003 where they apply
- [ ] All 11 test scenarios pass (TS-QRY-001-01 through -04 + U01–U07)
- [ ] `--missing owner` surfaces the draft project's gap; `--has owner` returns the active project (gap orthogonal to draft)
- [ ] Empty filter (`--stage lost`) exits 0 with `No entities match` / `[]`, never an error
- [ ] JSON branch (`--format json`) has field parity with the Rich-table branch, including `orphan`/`stale`
- [ ] No regressions in existing tests
