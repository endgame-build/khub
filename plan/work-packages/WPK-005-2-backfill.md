---
id: WPK-005-2
name: Backfill Frontmatter and Dates
feature-spec: plan/specs/FS-005-projection.md
test-spec: plan/tests/TS-005-projection.md
stories: [STORY-PRJ-002]
test-scenarios: [TS-PRJ-002-01, TS-PRJ-002-02, TS-PRJ-002-03, TS-PRJ-002-04, TS-PRJ-002-U01, TS-PRJ-002-U02, TS-PRJ-002-U03, TS-PRJ-002-U04, TS-PRJ-002-U05, TS-PRJ-002-U06]
depends-on: [WPK-002-1, WPK-002-2, WPK-004-2]
updated: 2026-07-04
---

# Backfill Frontmatter and Dates

## Objective

Delivers `khub backfill` (`core.backfill`) — the write path that brings an imported or legacy tree to a consistent, validatable state at cutover. It reads first- and last-commit dates from `git log` per file and writes `created` and `updated` where missing, and with `--type <t>` adds the missing per-type frontmatter scaffolding for entities inferable as that type. Every write is additive and minimal: backfill writes only where a value is absent, never overwrites an existing value, and round-trips through `ruamel.yaml` so authored values, key order, and inline comments survive byte-for-byte. `--dry-run` lists the entities and fields that would change and writes nothing; a workspace with no git history skips date backfill and reports it rather than failing. `backfill` shares the `git log` date helper with `stale`/`log` (WPK-004-2) and extends it with a first-commit read the shared helper does not yet expose. `backfill` is a cutover requirement (functional, not byte-parity with `kb.py backfill`); at cutover it runs first, before `reindex` (WPK-005-1) renders the current navigation.

> **Preset note:** `backfill` shares the `git log` date helper with `stale` (FS-004 / WPK-004-2), but `stale`/`log` read only the **last**-commit date (`core.gitlog.last_commit_date`). `backfill` additionally derives `created` from the **first** commit, which the shared helper does not yet expose — the first-commit read is `backfill`'s extension of the shared surface, exercised by TS-PRJ-002-U03. The no-git fallback reuses `core.gitlog.has_git_history`. The preset is source of truth — see `docs/firm-ops-preset.md`.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-PRJ-002 | AC-001 | Backfill missing dates: `khub backfill` reads first- and last-commit dates from `git log` per file, writes `created`/`updated` where missing, preserves existing values, key order, and comments, and displays `Backfilled dates on N entities` | TS-PRJ-002-01 |
| STORY-PRJ-002 | AC-002 | Backfill missing frontmatter: `khub backfill --type opportunity` adds the missing per-type scaffolding for that type and leaves entities that already validate untouched | TS-PRJ-002-02 |
| STORY-PRJ-002 | AC-003 | Dry run: `khub backfill --dry-run` lists the entities and fields that would change and writes nothing | TS-PRJ-002-03 |
| STORY-PRJ-002 | AC-004 | No git history: `khub backfill` in a workspace with no git history skips date backfill and displays `No git history; dates not backfilled` | TS-PRJ-002-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-PRJ-002 | REQ-PRJ002-01 | EARS-E | When backfill runs, the system shall write missing `created`/`updated` from `git log` | TS-PRJ-002-U01, TS-PRJ-002-U03 |
| STORY-PRJ-002 | REQ-PRJ002-02 | EARS-U | The system shall preserve existing values, key order, and comments on backfill | TS-PRJ-002-U02, TS-PRJ-002-U04 |
| STORY-PRJ-002 | REQ-PRJ002-03 | EARS-O | Where `--dry-run` is set, the system shall list changes and write nothing | TS-PRJ-002-U06 |
| STORY-PRJ-002 | REQ-PRJ002-04 | EARS-W | If the workspace has no git history, then the system shall skip date backfill | TS-PRJ-002-U05 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-PRJ-002 | PRJ-003 | Backfill writes only where a value is missing; it never overwrites existing data | Constraint | TS-PRJ-002-U02, TS-PRJ-002-U04 |
| STORY-PRJ-002 | PRJ-004 | Backfilled dates come from `git log`, the authoritative history | Automation | TS-PRJ-002-U03 |
| STORY-PRJ-002 | PRJ-SHARED-002 | Writes preserve authored data; round-trip keeps key order and comments | Constraint | TS-PRJ-002-01, TS-PRJ-002-U04 |
| STORY-PRJ-002 | PRJ-SHARED-003 | `reindex` and `backfill` are cutover requirements (functional, not byte-parity) | Constraint | TS-PRJ-002-01 |

---

## Entities

`backfill` reads the firm-ops entity tree and `git log`, and emits additive frontmatter writes. The typed entities it touches (fragment, opportunity) come from the firm-ops preset — valid entities are seeded via `core.create`; incomplete entities are written directly to disk (bypassing `create`) so the missing scaffolding is genuinely absent. The change shape below is what a single backfill write records.

### Backfill Change

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Entity | Reference | Yes | The entity file receiving the addition |
| Field | Type | Yes | The frontmatter key written: `created`, `updated`, or a per-type scaffolding key |
| Value | Text | Yes | The written value — a git date (dates) or scaffolding placeholder (`--type`) |
| Source | Type | Yes | Where the value came from: `git log` (dates) or the type schema (scaffolding) |

### Git Date Source *(named enumeration)*

| Value | Description |
|-------|-------------|
| first commit | First-commit date from `git log` → `created` (backfill's extension of the shared helper) |
| last commit | Last-commit date from `git log` → `updated` (the date `stale`/`log` already read) |
| none | No git history — date backfill skipped; frontmatter scaffolding via `--type` is unaffected |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

Not applicable — `khub backfill` performs additive frontmatter writes but drives no entity state machine. The load-bearing constraint is missing-only, preserve-everything (PRJ-003 / PRJ-SHARED-002): backfill writes only absent fields and never overwrites an authored value, reorders keys, or strips comments. A regression here silently corrupts the source — the opposite of the round-trip guarantee.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). The story maps to one `khub` subcommand. `backfill` does not gate — an entity that already validates is left untouched, and a no-git workspace is a reported no-op for dates.

### STORY-PRJ-002: Backfill Frontmatter and Dates (`khub backfill`)

- **Library verb:** `core.backfill(type)` reading `git log` via subprocess (shares the `git log` date helper with `stale`/`log`, WPK-004-2; extends it with a first-commit read), writing via round-trip YAML
- **Command:** `khub backfill`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| --type | string | No | Add missing per-type frontmatter scaffolding for entities inferable as that type |
| --dry-run | flag | No | List the entities and fields that would change; write nothing |

- **Reads:** entity frontmatter; `git log` (first + last commit dates) per file
- **Writes:** missing `created`/`updated` and (with `--type`) missing per-type scaffolding, via `ruamel.yaml` round-trip (minimal diff) — only where a value is absent
- **Output (success):** count of entities changed — `Backfilled dates on N entities` — or a dry-run list of entities and fields; a non-git workspace skips date backfill and displays `No git history; dates not backfilled`
- **Errors:** (none — an already-valid tree and a non-git workspace are both success)

---

## Test Data

### fragment

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Missing dates | `undated`, no `created`/`updated`, committed first then edited (distinct first + last commit dates in git) | Git first/last-commit date backfill; forces a genuine first-commit read (TS-PRJ-002-01, -03) |
| Preserved | `dated`, hand-kept `created: 2025-01-01` + inline comment | Preserve-existing + round-trip comment preservation, byte-unchanged after run (TS-PRJ-002-01) |

### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Incomplete | `legacy-deal`, only `type: opportunity` + a `client` edge, missing `stage`/`owner`/`created`/`updated` | Frontmatter scaffolding backfill via `--type` (TS-PRJ-002-02) |
| Valid | `initech-deal`, `client → initech`, `owner → noor`, `stage prospect`, complete scaffolding | Leaves-untouched control, re-read and asserted byte-unchanged (TS-PRJ-002-02) |

### no-git workspace

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Boundary | firm-ops workspace scaffolded in `tmp_path` with no `.git`; entities carry absent dates | `has_git_history` false → date backfill skipped, fallback message; `--type` scaffolding unaffected (TS-PRJ-002-04) |

---

## Implementation Notes

- **Shared `git log` helper, extended with a first-commit read (PRJ-004 / TS-PRJ-002-U03).** `backfill` reads git through the same date helper as `stale`/`log` (WPK-004-2), which today reads only `last_commit_date`. `backfill` additionally derives `created` from the **first** commit — a new read to add to `core.gitlog`. Commit `fragment/undated` twice with distinct dates and assert `created` = the first-commit date and `updated` = the last-commit date; the two must differ, forcing a genuine first-commit read rather than a last-commit alias.
- **Missing-only write is load-bearing (PRJ-003 / TS-PRJ-002-U02).** Write only where a value is absent; never overwrite an existing value. Re-read an entity with a hand-kept `created` and assert its value is unchanged after the run.
- **Round-trip preservation (PRJ-SHARED-002 / REQ-PRJ002-02 / TS-PRJ-002-U04).** Write through `ruamel.yaml` round-trip per the repo YAML rule — never PyYAML in `src/` or `tests/`. Key order and inline comments must survive. Assert both directions: the missing field is written **and** a re-read of an entity with an existing value confirms its value, key order, and inline comment are byte-unchanged.
- **Frontmatter scaffolding via `--type` (AC-002 / TS-PRJ-002-U01).** `backfill --type opportunity` adds the missing per-type scaffolding (`stage`/`owner`/`created`/`updated`) using the same required-field knowledge `validate` uses (WPK-004-1). Entities that already validate are left untouched — re-read `initech-deal` and assert it is byte-unchanged. Scaffolding via `--type` is unaffected by the absence of git.
- **Dry-run lists and writes nothing (REQ-PRJ002-03 / TS-PRJ-002-U06).** List the entities and fields that would change — `fragment/undated` with `created` and `updated` — and write nothing. Re-read every entity file after the run and assert byte-unchanged.
- **No-git fallback (REQ-PRJ002-04 / TS-PRJ-002-U05).** Reuse `core.gitlog.has_git_history` (WPK-004-2). When false, skip date backfill, leave absent dates absent, and display `No git history; dates not backfilled`. `--type` scaffolding still runs.
- **Cutover run-order is operational, not a build dependency.** At cutover `backfill` runs first to bring an imported tree to a consistent, validatable state; `reindex` (WPK-005-1) then renders the current navigation. The two touch different code paths and carry no build-time dependency on each other — the ordering lives in the cutover runbook, not in `depends-on`.
- **Not yet implemented (TDD-forward).** There is no `backfill` command in `cli/main.py`; `core/project.py` is the FS-002 `status` projection, not this verb. The scenarios define the contract implementation must meet.
- **Located messages are brittle.** `Backfilled dates on N entities` and `No git history; dates not backfilled` are asserted verbatim in the spec. Assert on the variable field (count) plus a stable substring, keeping the exact text as the canonical example.

---

## Done Criteria

- [ ] All 4 acceptance criteria pass (PRJ-002 AC-001 through AC-004)
- [ ] All 4 EARS requirements tested (REQ-PRJ002-01 through -04)
- [ ] All 2 business rules enforced (PRJ-003, PRJ-004), plus PRJ-SHARED-002/003 where they apply
- [ ] All 10 test scenarios pass (TS-PRJ-002-01 through -04 + U01–U06)
- [ ] `backfill` writes `created` from the first commit and `updated` from the last commit (the two distinct) for an undated entity, sourced from `git log`
- [ ] `backfill` writes only missing fields; an entity with an authored `created` + inline comment is byte-unchanged on disk afterward (ruamel round-trip)
- [ ] `backfill --type opportunity` scaffolds the missing per-type keys and leaves an already-valid entity untouched
- [ ] `backfill --dry-run` lists the entities and fields that would change and leaves every file byte-unchanged
- [ ] `backfill` in a non-git workspace skips date backfill and reports the fallback message
- [ ] `backfill` on a seeded firm-ops snapshot writes missing dates and frontmatter (E2E, per the execution plan)
- [ ] No regressions in existing tests
