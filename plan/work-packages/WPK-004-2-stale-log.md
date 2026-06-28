---
id: WPK-004-2
name: Find Stale Entities and Render Git History
feature-spec: plan/specs/FS-004-integrity.md
test-spec: plan/tests/TS-004-integrity.md
stories: [STORY-INT-003, STORY-INT-004]
test-scenarios: [TS-INT-003-01, TS-INT-003-02, TS-INT-003-03, TS-INT-003-04, TS-INT-003-U01, TS-INT-003-U02, TS-INT-003-U03, TS-INT-003-U04, TS-INT-003-U05, TS-INT-003-U06, TS-INT-004-01, TS-INT-004-02, TS-INT-004-03, TS-INT-004-04, TS-INT-004-U01, TS-INT-004-U02, TS-INT-004-U03, TS-INT-004-U04, TS-INT-004-U05, TS-INT-004-U06]
depends-on: [WPK-002-1, WPK-003-1]
updated: 2026-06-27
---

# Find Stale Entities and Render Git History

## Objective

Delivers the two git-derived reads that round out the integrity loop: `khub stale` (`core.stale`) lists entities whose `updated` is past a threshold with missing dates backfilled from `git log`, and `khub log` (`core.log`) renders git history at ontology altitude. `stale` returns entities older than the default 30-day window (overridable with `--days`), sorted oldest first, reading the last-commit date from git for any entity without an `updated` field — without writing it back (that is `backfill`'s job, FS-005). `log` reads `git log`, maps each commit's files to entity ids and relations, and describes changes by entity and relation rather than raw file path, honoring `--limit` and `--since`; `log <id>` filters to one entity. Both share the `git log` date helper and uphold the loop's invariant: history and staleness are derived from git, never hand-maintained. `log` is distinct from `history` (FS-003) — git history, not the graph supersession chain.

> **Preset note:** firm-ops v1 declares no `decision` type and no `supersedes` predicate (both HQ-only, dropped). The `log`-vs-`history` distinction scenario (INT-004 AC-003) runs against a generic schema fixture declaring a self-referential `supersedes` — the same fixture pattern WPK-003-3 uses for the `history` surface. The git-log scenarios run against valid firm-ops entities (`project/initech-pov`, `opportunity/initech-deal`). The preset is source of truth — see `docs/firm-ops-preset.md`.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-INT-003 | AC-001 | List stale entities: `khub stale` returns entities whose effective date is more than 30 days old, sorted by age oldest first, rendered as a Rich table on a TTY | TS-INT-003-01 |
| STORY-INT-003 | AC-002 | Custom threshold: `khub stale --days 90` applies the 90-day threshold instead of 30 | TS-INT-003-02 |
| STORY-INT-003 | AC-003 | Backfill missing dates from git: for an entity with no `updated`, the last-commit date is read from `git log` and used in the comparison; the backfilled date is not written to disk | TS-INT-003-03 |
| STORY-INT-003 | AC-004 | No git history: in a non-git workspace, `stale` falls back to the `updated` field only and displays `No git history; using updated field only` | TS-INT-003-04 |
| STORY-INT-004 | AC-001 | Render recent history: `khub log` reads `git log`, maps each commit's files to entity ids and relations, describes changes at ontology altitude (not raw file paths), honoring `--limit` and `--since` | TS-INT-004-01 |
| STORY-INT-004 | AC-002 | History for one entity: `khub log <id>` returns only commits touching that entity, rendered in order | TS-INT-004-02 |
| STORY-INT-004 | AC-003 | Distinct from supersession history: `khub log <record>` returns git commit history, not the `supersedes` chain (that is `history`'s job) | TS-INT-004-03 |
| STORY-INT-004 | AC-004 | No git history: in a workspace with no git history, `khub log` displays `No git history available` and exits 0 | TS-INT-004-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-INT-003 | REQ-INT003-01 | EARS-E | When stale runs, the system shall return entities past the `updated` threshold, oldest first | TS-INT-003-U01, TS-INT-003-U02 |
| STORY-INT-003 | REQ-INT003-02 | EARS-O | Where `--days <n>` is set, the system shall apply that threshold instead of 30 | TS-INT-003-U03 |
| STORY-INT-003 | REQ-INT003-03 | EARS-W | If `updated` is missing, then the system shall read the date from `git log` for the comparison | TS-INT-003-U04 |
| STORY-INT-003 | REQ-INT003-04 | EARS-W | If the workspace has no git history, then the system shall fall back to the `updated` field | TS-INT-003-U06 |
| STORY-INT-004 | REQ-INT004-01 | EARS-E | When log runs, the system shall render git history mapped to entities and relations | TS-INT-004-U01, TS-INT-004-U02, TS-INT-004-U04 |
| STORY-INT-004 | REQ-INT004-02 | EARS-O | Where an id is given, the system shall return only commits touching that entity | TS-INT-004-U03 |
| STORY-INT-004 | REQ-INT004-03 | EARS-U | The system shall present log as git history, distinct from `history`'s supersession chain | TS-INT-004-U05 |
| STORY-INT-004 | REQ-INT004-04 | EARS-W | If no git history exists, then the system shall report it and exit 0 | TS-INT-004-U06 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-INT-003 | INT-007 | The default staleness threshold is 30 days | Constraint | TS-INT-003-U01, TS-INT-003-U03 |
| STORY-INT-003 | INT-008 | `stale` reads git dates but does not write them | Constraint | TS-INT-003-U05 |
| STORY-INT-004 | INT-009 | `log` is derived from git, rendered at ontology altitude | Constraint | TS-INT-004-U02 |
| STORY-INT-004 | INT-010 | `log` (git history) is distinct from `history` (graph supersession chain) | Constraint | TS-INT-004-U05 |
| STORY-INT-003, STORY-INT-004 | INT-SHARED-002 | History and staleness are derived from git, never hand-maintained | Constraint | TS-INT-003-U04, TS-INT-004-U01 |
| STORY-INT-003 | INT-SHARED-004 | Stale is a core projection property, surfaced by default in `status` and `query`; `stale` gates on the same computation | Constraint | TS-INT-003-01 |

---

## Entities

The loop reads the firm-ops graph and emits report records. The two report shapes below are what `stale` and `log` return; the typed entities they read (fragment, project, opportunity) come from the firm-ops preset and are seeded via `core.create`.

### Stale Entry

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Entity | Reference | Yes | The entity past the `updated` threshold |
| Effective Date | Date | Yes | `updated` if present, else the git last-commit date (read, never written) |
| Age | Number | Yes | Days since the effective date; drives the oldest-first sort |
| Source | Type | Yes | Where the date came from: `updated` field or `git log` |

### Log Entry

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Commit | Identifier | Yes | The git commit |
| Entity | Reference | Yes | The entity the changed file maps to (ontology altitude, not file path) |
| Relations | Collection | No | The relations touched by the change |
| Date | Date/Time | Yes | Commit author date |

### Date Source *(named enumeration)*

| Value | Description |
|-------|-------------|
| updated field | The hand-kept `updated` frontmatter value |
| git log | The last-commit date read from git when `updated` is missing |
| none | No git history and no `updated` — fallback path for `stale`; `log` reports `No git history available` |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

Not applicable — `khub stale` and `khub log` are read-only, git-derived projections. They transition no entity state. The read-only guarantee for `stale` (INT-008) is the load-bearing constraint: git dates are read for the comparison but never written back to the file.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). Each story maps to one `khub` subcommand. Unlike `validate`/`check`, neither gates — `stale` reports and `log` is a no-op success even with no history.

### STORY-INT-003: Find Stale Entities (`khub stale`)

- **Library verb:** `core.stale(days)` reading `git log` via subprocess (shares the `git log` date helper with `backfill`, FS-005)
- **Command:** `khub stale`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| --days | int (default 30) | No | Staleness threshold in days |
| --format | enum(table, json) | No | Rich table on a TTY (default) or JSON, sorted by age |

- **Reads:** entities with an `updated` field; `git log` (read-only) for any entity missing `updated`
- **Output (success):** entities past the threshold, oldest first, as a Rich table or JSON. Non-git workspace: falls back to the `updated` field and displays `No git history; using updated field only`.
- **Errors:** (none — an empty stale set is a success)

### STORY-INT-004: Render Git History at Ontology Altitude (`khub log`)

- **Library verb:** `core.log(id)` reading `git log` via subprocess
- **Command:** `khub log [id]`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| id | string (positional) | No | Bare slug or `type/slug`; filters to commits touching that entity |
| --limit | int | No | Cap the number of rendered commits |
| --since | date | No | Only commits on/after this date |
| --format | enum(table, json) | No | Ontology-altitude change list as table or JSON |

- **Reads:** `git log` (read-only); maps changed files to entity ids and relations — never the supersession chain
- **Output (success):** changes described by entity and relation (not file path), honoring `--limit`/`--since`; `log <id>` returns only that entity's commits. No git history: `No git history available`, exit 0.
- **Errors:** (none — no-git is a reported no-op success, exit 0)

---

## Test Data

### fragment

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `old-note` (`updated: 2026-01-10`), `older-note` (`updated: 2025-12-01`) | Stale set past 30 days, oldest-first sort (TS-INT-003-01, -02) |
| Boundary | `fresh-note` (`updated: 2026-06-20`) | Within the 30-day window from the 2026-06-27 run — excluded (TS-INT-003-01) |
| Missing date | `undated` with no `updated`, last commit > 30 days old | Git-date backfill used in comparison, not written to disk (TS-INT-003-03) |

### project / opportunity (log subjects)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `project/initech-pov` and `opportunity/initech-deal` with a real commit history (an `owner` edge added in one commit, a `stage` change in another) | Ontology-altitude rendering; per-entity filtering (TS-INT-004-01, -02) |

### supersession fixture (generic, non-firm-ops)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | generic type with a self-referential `supersedes` edge plus a git edit history | `log` returns git commits, not the `supersedes` chain (TS-INT-004-03) |

### no-git workspace

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Boundary | workspace scaffolded in `tmp_path` with no `.git`; entities carry hand-kept `updated` | `stale` fallback message; `log` no-op success exit 0 (TS-INT-003-04, TS-INT-004-04) |

---

## Implementation Notes

- **Shared `git log` date helper.** Both verbs read git via subprocess through one date helper, shared with `backfill` (FS-005). `stale` uses it only to fill a missing `updated`; `log` uses it for the full commit list. Keep the helper read-only — see INT-008.
- **Read-only is load-bearing (INT-008 / TS-INT-003-U05).** `stale` reads git dates but must never write them — that is `backfill`'s job. A silent write corrupts the source and blurs the two commands. Re-read the entity file on disk after `stale` and assert the frontmatter is byte-unchanged (no `updated` inserted).
- **Stale is a projection property (INT-SHARED-004).** Orphan and stale are computed by the projection and surfaced by default in `status` and `query` (WPK-001-2, WPK-003-1); `stale` the command gates on the same computation. Reuse it rather than recomputing staleness independently.
- **Effective date precedence (REQ-INT003-03).** Use `updated` when present; else the git last-commit date; else (no git, no `updated`) fall back per AC-004. The default threshold is 30 days (INT-007), overridable with `--days`.
- **Ontology altitude (INT-009 / TS-INT-004-U02).** `log` must describe changes by entity and relation — `project/initech-pov`'s `owner` edge added — never `projects/initech-pov/_index.md`. Assert the rendered output names the entity/relation and does not contain the raw file path.
- **`log` is not `history` (INT-010 / REQ-INT004-03).** `log` reads git; `history` (FS-003) reads the graph supersession chain. The distinction scenario uses a generic `supersedes` fixture (firm-ops v1 carries none) and asserts the `supersedes` chain is absent from `log` output — git is the only source `log` touches.
- **No-git is a success for `log` (REQ-INT004-04).** Unlike `validate`/`check`, a no-git `log` is a reported no-op that exits 0 (`No git history available`). `stale` in a non-git workspace falls back to `updated` and reports `No git history; using updated field only`.
- **YAML I/O uses `ruamel.yaml`** per the repo rule — never PyYAML in `src/` or `tests/`.
- **Located messages are brittle.** `No git history; using updated field only` and `No git history available` are asserted verbatim in the spec. Assert on a message substring plus the variable fields (entity id, date, count), keeping the exact text as the canonical example.

---

## Done Criteria

- [ ] All 8 acceptance criteria pass (INT-003 AC-001 through AC-004; INT-004 AC-001 through AC-004)
- [ ] All 8 EARS requirements tested (REQ-INT003-01 through -04; REQ-INT004-01 through -04)
- [ ] All 6 business rules enforced (INT-007 through INT-010), plus INT-SHARED-002/004 where they apply
- [ ] All 20 test scenarios pass (TS-INT-003-01 through -04 + U01–U06; TS-INT-004-01 through -04 + U01–U06)
- [ ] `stale` returns the stale set oldest-first under the default 30-day threshold; `--days 90` shifts the window
- [ ] `stale` reads a git date for a missing `updated` and the entity file is byte-unchanged on disk afterward (no write-back)
- [ ] `stale` in a non-git workspace falls back to `updated` and reports the fallback message
- [ ] `log` describes a recent edit by entity and relation, not file path; `log <id>` filters to one entity; `--limit`/`--since` bound the output
- [ ] `log` returns git commits, not the `supersedes` chain (generic fixture); no-git `log` reports the absence and exits 0
- [ ] `stale --days 30` surfaces the stale set and `log` renders a recent edit on a seeded firm-ops snapshot (E2E)
- [ ] No regressions in existing tests
