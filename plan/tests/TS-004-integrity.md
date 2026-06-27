---
id: TS-004
name: Integrity Loop Test Spec
spec: plan/specs/FS-004-integrity.md
updated: 2026-06-27
---

# Integrity Loop — Test Spec

## Summary

Tests the integrity loop — the v1 acceptance signal for the HQ cutover. `validate` checks per-entity well-formedness and referential integrity over a declared subset; `check` runs graph-wide over the active (`draft: false`) subgraph, computing required-completeness from the schema; `stale` flags entities past an `updated` threshold with dates backfilled from git; `log` renders git history at ontology altitude. Coverage runs from per-entity field/enum/pattern/cardinality checks and `--strict` schema closure, through the graph-wide gaps that define `check` — active-but-incomplete computed from the schema (never the `draft` flag), a draft never satisfying a required relation, orphans, dangling edges, stray files, and edge cycles — to the two git-derived reads (`stale` and `log`), all under the cross-cutting invariant that khub guarantees structural integrity, never semantic truth: a false-but-legal write passes both `validate` and `check`, with git history as the only backstop.

**Feature Spec:** FS-004: Integrity Loop
**Stories Covered:** 4
**Test Scenarios:** 17 (plus 30 unit tests)

---

## Test Strategy

### Approach

**Philosophy:** Hybrid — `validate` and `check` are the v1 gate, so the discrete mechanics (a single field/enum/pattern/cardinality check, one relation resolution, the `--strict` closure decision, the orphan predicate, the completeness computation, the cycle guard, a threshold comparison, a commit-to-entity mapping) are unit-testable in isolation against a built model or index, but the guarantees that actually gate the cutover only hold end to end: `validate` continues past the first error and reports every one, `check` derives completeness from the schema over the active subgraph and a draft target never satisfies a required relation, a removed target surfaces as a dangling edge, a cycle terminates instead of hanging, `stale` reads git dates without writing them, `log` describes changes by entity and relation rather than file path, and a false-but-legal write passes clean. Unit tests pin the rules and algorithms; integration tests prove the command surface, the located error messages, exit codes, and the git-backed reads; E2E proves `validate` and `check` run clean on a seeded firm-ops snapshot (functional cutover, not byte-parity with `kb.py`).

**Test Pyramid:**

| Level | Share | Scope |
|-------|-------|-------|
| Unit | ~65% | Field/enum/pattern/cardinality check, referential resolution, extension passthrough, `--strict` rejection, no-frontmatter skip, collect-all-errors, `--fix` date backfill, orphan detection, completeness-from-schema, draft exemption, draft-does-not-satisfy, dangling-edge detection, stray-file detection, cycle guard, threshold comparison + sort, git-date backfill (read-only), no-git fallback, commit→entity mapping, ontology-altitude rendering, `--limit`/`--since`, log-vs-history distinction |
| Integration | ~25% | Command surface for all four verbs, per-entity error reporting + non-zero exit, `--strict` close vs open, reference-file skip, active-but-incomplete report, draft-does-not-satisfy report, orphan/dangling/stray report, cycle report, `--days` threshold, git-date backfill, no-git message, per-entity `log` filtering, `--since` window, `--format json` shape |
| E2E | ~10% | `validate` an HQ snapshot cleanly; `check` surfaces the firm-ops structural gaps on a snapshot; `stale --days 30` surfaces the stale set; `log` describes a recent edit by entity and relation, not file path |

### Coverage Targets

| Target | Goal |
|--------|------|
| Acceptance criteria | 100% of ACs from FS-004 (17 ACs across 4 stories) |
| EARS requirements | 100% of REQ-INT* requirements (17) |
| Business rules | 100% of rule enforcement (INT-001..012, INT-SHARED-001..004) |
| Edge cases | Clean tree passes, all errors reported not just the first, strict rejects extensions, no-frontmatter skipped, active-but-incomplete, draft target unsatisfied, removed target dangles, edge cycle terminates, custom threshold, missing `updated` backfilled but not written, no-git fallback, `log` distinct from `history`, false-but-legal write passes |

---

## Test Scenarios

---

### STORY-INT-001: Validate Entities

**Spec:** As an Agent or Operator, I want to check entities for well-formedness and referential integrity over the declared subset, So that I know every present field is legal and every relation resolves before trusting the data

#### TS-INT-001-01: Validate a Clean Tree

**Validates:** AC-001
**Level:** E2E

**Given** a firm-ops workspace where every declared field is legal and every relation resolves
**When** the agent runs `khub validate`
**Then:**
- [ ] each present declared field is checked against its type, enum, pattern, and cardinality
- [ ] every relation resolves to an existing target
- [ ] undeclared extension fields are left unchecked
- [ ] `Validated N entities; 0 errors` is displayed
- [ ] exit 0

**Test Data:** a seeded firm-ops workspace — `person/noor`, `client/initech`, `opportunity/initech-deal` (`client → initech`, `owner → noor`, `stage prospect`), `project/initech-pov` (`client → initech`, `owner → noor`, `active: true`), `meeting/kickoff` (`engagement → initech-pov`) — every required field present, every edge resolving, one entity carrying an undeclared extension field to prove passthrough

#### TS-INT-001-02: Report Per-Entity Errors

**Validates:** AC-002
**Level:** Integration

**Given** an entity with a malformed value and another with an unresolved relation
**When** the agent runs `khub validate`
**Then:**
- [ ] each error is reported with its entity id, field, and reason
- [ ] validation continues past the first error and reports both
- [ ] exit non-zero

**Test Data:** `opportunity/bad-stage` with `stage: vibing` (not in the CRM enum) and `project/ghost-owner` whose `owner → person/nobody` does not resolve — two distinct entities so the all-errors behavior is observable

#### TS-INT-001-03: Strict Closes the Schema

**Validates:** AC-003
**Level:** Integration

**Given** an entity carrying undeclared fields
**When** the agent runs `khub validate --strict`
**Then:**
- [ ] undeclared keys are rejected as errors under `--strict`
- [ ] the same tree passes without `--strict` (extensions allowed)

**Test Data:** `client/initech` carrying an undeclared `vibe: high` key; validated once with `--strict` (fails on the undeclared key) and once without (passes)

#### TS-INT-001-04: Skip Reference Files Cleanly

**Validates:** AC-004
**Level:** Integration

**Given** the tree holds reference markdown with no frontmatter
**When** the agent runs `khub validate`
**Then:**
- [ ] files with no recognized frontmatter are skipped, not errored
- [ ] only typed entities are counted

**Test Data:** a firm-ops workspace plus a reference doc (`identity/mission.md`) with no frontmatter sitting outside every type layout; the `N` in `Validated N entities` counts only the typed entities

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-INT-001-U01 | Field checker | Checks each present declared field against its type, enum, pattern, and cardinality | REQ-INT001-01, INT-001 |
| TS-INT-001-U02 | Referential resolver | Reports a referential-integrity error when a relation does not resolve to an existing target | REQ-INT001-02, INT-002 |
| TS-INT-001-U03 | Extension passthrough | Leaves undeclared keys unchecked unless `--strict` is set | REQ-INT001-04, INT-001 |
| TS-INT-001-U04 | Strict closer | Rejects undeclared keys as errors when `--strict` is set | REQ-INT001-03 |
| TS-INT-001-U05 | Frontmatter skipper | Skips files with no recognized frontmatter, counting only typed entities | INT-003 |
| TS-INT-001-U06 | Error collector | Continues past the first failure and accumulates every error before returning | REQ-INT001-02 |
| TS-INT-001-U07 | Fix backfiller | `--fix` backfills a missing `updated` date via the `git log` helper (v1 `--fix` scope is date backfill only) | REQ-INT001-01 |

---

### STORY-INT-002: Check the Graph

**Spec:** As an Agent or Operator, I want to run a graph-wide integrity pass over the active subgraph, So that published entities are complete, no relation dangles, no entity is orphaned, no file is stray, and no edge cycles

> **Preset note:** the cycle scenario (AC-005) runs on the universal `depends_on` edge — the "hard dependency" relation, naturally a DAG and present in v1. FS-004 (reconciled) names `depends_on`; the HQ-only `supersedes`/`decision` it originally cited were dropped from the preset. Same substitution TS-003 makes for `impact` cycle tests. See Risks.

#### TS-INT-002-01: Check a Sound Graph

**Validates:** AC-001
**Level:** E2E

**Given** a published subgraph where every required relation resolves to a published target
**When** the agent runs `khub check`
**Then:**
- [ ] every relation resolves
- [ ] required-completeness holds for every active (`draft: false`) entity, computed from the schema
- [ ] no orphans (entities with neither an inbound nor an outbound relation)
- [ ] no stray files
- [ ] no edge cycles
- [ ] `Graph check passed` is displayed
- [ ] exit 0

**Test Data:** the clean firm-ops seed from TS-INT-001-01 — every active entity complete, `person/noor` reached by inbound `owner` edges, `client/initech` reached by inbound `client` edges, `meeting/kickoff` carrying an outbound `engagement`, so no node is edge-isolated; no cycles; no malformed files inside a type layout

#### TS-INT-002-02: Report Active-but-Incomplete Entities

**Validates:** AC-002
**Level:** Integration

**Given** a published entity (`draft: false`) missing a required field or relation
**When** the agent runs `khub check`
**Then:**
- [ ] completeness is computed from the schema, never from the `draft` flag
- [ ] the entity is reported as active-but-incomplete, naming each missing required field and unresolved required relation
- [ ] exit non-zero

**Test Data:** an active `project/orphaned-pov` created with `add` (active by default) but saved missing its required `owner` edge — reported as active-but-incomplete on `owner`, proving the completeness verdict comes from the schema, not the published flag

#### TS-INT-002-03: Drafts Do Not Satisfy Required Relations

**Validates:** AC-003
**Level:** Integration

**Given** an active entity whose required relation points at a `draft`
**When** the agent runs `khub check`
**Then:**
- [ ] the required relation is reported as incomplete (a draft does not satisfy it)
- [ ] the active entity and the unsatisfied predicate are named

**Test Data:** active `opportunity/initech-deal` whose required `owner → person/newhire`, where `person/newhire` is `draft: true` — `check` reports `initech-deal`'s `owner` as unsatisfied

#### TS-INT-002-04: Report Orphans, Dangling Edges, and Stray Files

**Validates:** AC-004
**Level:** Integration

**Given** an orphan entity, a relation whose target was removed, and a file outside any type layout
**When** the agent runs `khub check`
**Then:**
- [ ] orphan entities are listed
- [ ] dangling edges are listed (a relation whose target no longer resolves)
- [ ] stray files are listed
- [ ] exit non-zero

**Test Data:** `person/orphan-person` that nothing owns and that carries no outbound edge (orphan, zero in/out); `project/initech-pov` whose `client → initech` after `client/initech` was dropped via `remove --force` (dangling); a malformed `clients/notes.md` sitting inside the `client` layout that does not parse as a `client` (stray — distinct from a frontmatter-less reference doc outside every layout, which is skipped)

#### TS-INT-002-05: Detect an Edge Cycle

**Validates:** AC-005
**Level:** Integration

**Given** a cycle on a predicate that should be acyclic
**When** the agent runs `khub check`
**Then:**
- [ ] the cycle is reported with the participating ids
- [ ] exit non-zero

**Test Data:** a seeded `depends_on` cycle `fragment/a → fragment/b → fragment/c → fragment/a` over the universal acyclic `depends_on` edge (FS-004, reconciled, names `depends_on`; the HQ-only `supersedes` it originally cited was dropped — see Risks)

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-INT-002-U01 | Orphan detector | Flags an entity with neither an inbound nor an outbound relation | REQ-INT002-04, INT-006 |
| TS-INT-002-U02 | Completeness computer | Computes required-completeness from the schema over the active subgraph | REQ-INT002-01, REQ-INT002-05, INT-004 |
| TS-INT-002-U03 | Active-but-incomplete reporter | Reports an active entity missing a required field/relation, naming each gap; never infers completeness from `draft` | REQ-INT002-02, INT-012 |
| TS-INT-002-U04 | Draft exempter | Excludes `draft` entities from required-completeness | REQ-INT002-05, INT-004 |
| TS-INT-002-U05 | Draft-satisfaction guard | Treats a `draft` target as not satisfying another entity's required relation | REQ-INT002-03, INT-005 |
| TS-INT-002-U06 | Dangling-edge detector | Flags a relation whose target no longer resolves | REQ-INT002-04 |
| TS-INT-002-U07 | Stray-file detector | Flags a file inside a type's layout path that does not parse as that type; skips reference markdown outside every layout | REQ-INT002-04, INT-011 |
| TS-INT-002-U08 | Cycle guard | Detects an edge cycle on an acyclic predicate and returns the participating ids | REQ-INT002-01, REQ-INT002-04 |

---

### STORY-INT-003: Find Stale Entities

**Spec:** As an Operator or Agent, I want to list entities whose `updated` is past a threshold with dates backfilled from git, So that I can find context that has drifted out of date without trusting hand-kept timestamps

#### TS-INT-003-01: List Stale Entities

**Validates:** AC-001
**Level:** E2E

**Given** entities older than the default threshold
**When** the operator runs `khub stale`
**Then:**
- [ ] entities whose effective date is more than 30 days old are returned
- [ ] results are sorted by age, oldest first
- [ ] a Rich table is rendered on a TTY

**Test Data:** a firm-ops workspace where `fragment/old-note` (`updated: 2026-01-10`) and `fragment/older-note` (`updated: 2025-12-01`) are past 30 days from the 2026-06-27 run date, and `fragment/fresh-note` (`updated: 2026-06-20`) is within it — the two stale ones returned oldest-first

#### TS-INT-003-02: Custom Threshold

**Validates:** AC-002
**Level:** Integration

**Given** a different staleness window
**When** the operator runs `khub stale --days 90`
**Then:**
- [ ] the 90-day threshold is applied instead of 30

**Test Data:** the same seed; with `--days 90` only `fragment/older-note` and `fragment/old-note` past 90 days surface, demonstrating the window shift versus the default

#### TS-INT-003-03: Backfill Missing Dates From Git

**Validates:** AC-003
**Level:** Integration

**Given** an entity with no `updated` field
**When** the operator runs `khub stale`
**Then:**
- [ ] the last-commit date is read from `git log` for that file
- [ ] the git date is used in the staleness comparison
- [ ] the backfilled date is not written (that is `backfill`'s job)

**Test Data:** `fragment/undated` committed with no `updated` field, its last commit dated more than 30 days before the run; the file on disk is asserted unchanged after the run (no `updated` written)

#### TS-INT-003-04: No Git History

**Validates:** AC-004
**Level:** Integration

**Given** a workspace that is not a git repository
**When** the operator runs `khub stale`
**Then:**
- [ ] the comparison falls back to the `updated` field only
- [ ] `No git history; using updated field only` is displayed

**Test Data:** a workspace scaffolded in a `tmp_path` with no `.git`; entities carry hand-kept `updated` fields and staleness is computed from those alone

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-INT-003-U01 | Threshold comparator | Returns entities whose effective date exceeds the threshold; default is 30 days | REQ-INT003-01, INT-007 |
| TS-INT-003-U02 | Age sorter | Sorts the stale set by age, oldest first | REQ-INT003-01 |
| TS-INT-003-U03 | Days overrider | Applies `--days <n>` in place of the 30-day default | REQ-INT003-02, INT-007 |
| TS-INT-003-U04 | Git-date reader | Reads the last-commit date from `git log` for an entity missing `updated`, using it in the comparison | REQ-INT003-03 |
| TS-INT-003-U05 | Read-only guard | Reads git dates without writing them back to the file | INT-008 |
| TS-INT-003-U06 | No-git fallback | Falls back to the `updated` field when the workspace has no git history | REQ-INT003-04 |

---

### STORY-INT-004: Render Git History at Ontology Altitude

**Spec:** As an Agent or Operator, I want to read git history described in entities and relations, not files, So that I can orient on what changed without a gate, distinct from the supersession chain

> **Preset note:** FS-004's `khub log decision-0012` example and the supersession-chain comparison (AC-003) reference the `decision` type and `supersedes` predicate, both HQ-only and dropped from v1 firm-ops. The git-log scenarios run against valid firm-ops entities (`project/initech-pov`); the `log`-vs-`history` distinction (AC-003, INT-010) uses a generic schema fixture declaring a self-referential `supersedes`, the same fixture pattern TS-003 uses for the `history` surface. See Risks.

#### TS-INT-004-01: Render Recent History

**Validates:** AC-001
**Level:** E2E

**Given** a git history of entity edits
**When** the agent runs `khub log`
**Then:**
- [ ] `git log` is read and each commit's files are mapped to entity ids and relations
- [ ] changes are described at ontology altitude (which entity, which relation), not raw file paths
- [ ] `--limit` and `--since` are honored

**Test Data:** a firm-ops workspace with a real commit history touching `project/initech-pov` and `opportunity/initech-deal` (an `owner` edge added in one commit, a `stage` change in another); the rendered output names the entities and relations, not `projects/initech-pov/_index.md`

#### TS-INT-004-02: History for One Entity

**Validates:** AC-002
**Level:** Integration

**Given** an entity with an edit history
**When** the agent runs `khub log initech-pov`
**Then:**
- [ ] only commits touching that entity are returned
- [ ] the edit history is rendered in order

**Test Data:** the same commit history; `khub log initech-pov` returns only the commits that touched `project/initech-pov`, excluding the `opportunity/initech-deal`-only commits

#### TS-INT-004-03: Distinct From Supersession History

**Validates:** AC-003
**Level:** Integration

**Given** a record with both git edits and a supersession chain
**When** the agent runs `khub log <record>`
**Then:**
- [ ] git commit history is returned (who changed what, when)
- [ ] the `supersedes` chain is not returned (that is `history`'s job)

**Test Data:** a generic fixture type carrying a self-referential `supersedes` edge and a git edit history; `khub log <record>` returns the git commits, and the `supersedes` chain is asserted absent from the output (firm-ops v1 carries no `supersedes` — see Risks)

#### TS-INT-004-04: No Git History

**Validates:** AC-004
**Level:** Integration

**Given** a workspace with no git history
**When** the agent runs `khub log`
**Then:**
- [ ] `No git history available` is displayed
- [ ] exit 0

**Test Data:** a workspace scaffolded in a `tmp_path` with no `.git` — `log` reports the absence and exits 0 (a no-op success, unlike `validate`/`check` which gate)

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-INT-004-U01 | Commit mapper | Maps each commit's changed files to entity ids and their relations | REQ-INT004-01 |
| TS-INT-004-U02 | Altitude renderer | Describes changes by entity and relation, never by raw file path | REQ-INT004-01, INT-009 |
| TS-INT-004-U03 | Entity filter | `log <id>` returns only commits touching that entity, in order | REQ-INT004-02 |
| TS-INT-004-U04 | Window applier | Honors `--limit` and `--since` to bound the rendered history | REQ-INT004-01 |
| TS-INT-004-U05 | History-vs-log distinction | `log` reads git history; `history` reads the graph supersession chain — the two stay distinct | REQ-INT004-03, INT-010 |
| TS-INT-004-U06 | No-git exit | Reports the absence of git history and exits 0 | REQ-INT004-04 |

---

## Coverage Matrix

### Acceptance Criteria → Test Scenarios

| Story | AC | Description | Test Scenarios |
|-------|----|-------------|----------------|
| STORY-INT-001 | AC-001 | Validate a clean tree | TS-INT-001-01 |
| STORY-INT-001 | AC-002 | Report per-entity errors | TS-INT-001-02 |
| STORY-INT-001 | AC-003 | Strict closes the schema | TS-INT-001-03 |
| STORY-INT-001 | AC-004 | Skip reference files cleanly | TS-INT-001-04 |
| STORY-INT-002 | AC-001 | Check a sound graph | TS-INT-002-01 |
| STORY-INT-002 | AC-002 | Report active-but-incomplete entities | TS-INT-002-02 |
| STORY-INT-002 | AC-003 | Drafts do not satisfy required relations | TS-INT-002-03 |
| STORY-INT-002 | AC-004 | Report orphans, dangling edges, stray files | TS-INT-002-04 |
| STORY-INT-002 | AC-005 | Detect an edge cycle | TS-INT-002-05 |
| STORY-INT-003 | AC-001 | List stale entities | TS-INT-003-01 |
| STORY-INT-003 | AC-002 | Custom threshold | TS-INT-003-02 |
| STORY-INT-003 | AC-003 | Backfill missing dates from git | TS-INT-003-03 |
| STORY-INT-003 | AC-004 | No git history | TS-INT-003-04 |
| STORY-INT-004 | AC-001 | Render recent history | TS-INT-004-01 |
| STORY-INT-004 | AC-002 | History for one entity | TS-INT-004-02 |
| STORY-INT-004 | AC-003 | Distinct from supersession history | TS-INT-004-03 |
| STORY-INT-004 | AC-004 | No git history | TS-INT-004-04 |

### EARS Requirements → Test Scenarios

| Requirement | Type | Description | Test Scenarios |
|-------------|------|-------------|----------------|
| REQ-INT001-01 | EARS-E | Validate → check each present declared field against the schema | TS-INT-001-01, TS-INT-001-U01, TS-INT-001-U07 |
| REQ-INT001-02 | EARS-W | Relation does not resolve → referential-integrity error | TS-INT-001-02, TS-INT-001-U02, TS-INT-001-U06 |
| REQ-INT001-03 | EARS-O | `--strict` → reject undeclared keys | TS-INT-001-03, TS-INT-001-U04 |
| REQ-INT001-04 | EARS-U | Validate only the declared subset; leave extensions alone unless strict | TS-INT-001-01, TS-INT-001-U03 |
| REQ-INT002-01 | EARS-E | Check → relations resolve, required-completeness holds for active, no cycles | TS-INT-002-01, TS-INT-002-U02, TS-INT-002-U08 |
| REQ-INT002-02 | EARS-W | Active entity missing a required field/relation → active-but-incomplete, exit non-zero | TS-INT-002-02, TS-INT-002-U03 |
| REQ-INT002-03 | EARS-S | While checking completeness → a `draft` target does not satisfy a required relation | TS-INT-002-03, TS-INT-002-U05 |
| REQ-INT002-04 | EARS-W | Orphan, dangling edge, stray file, or cycle → report it, exit non-zero | TS-INT-002-04, TS-INT-002-05, TS-INT-002-U01, TS-INT-002-U06, TS-INT-002-U07, TS-INT-002-U08 |
| REQ-INT002-05 | EARS-U | Compute completeness from the schema over the active subgraph only, never from `draft` | TS-INT-002-02, TS-INT-002-U02, TS-INT-002-U04 |
| REQ-INT003-01 | EARS-E | Stale → return entities past the `updated` threshold, oldest first | TS-INT-003-01, TS-INT-003-U01, TS-INT-003-U02 |
| REQ-INT003-02 | EARS-O | `--days <n>` → apply that threshold instead of 30 | TS-INT-003-02, TS-INT-003-U03 |
| REQ-INT003-03 | EARS-W | `updated` missing → read the date from `git log` for the comparison | TS-INT-003-03, TS-INT-003-U04 |
| REQ-INT003-04 | EARS-W | No git history → fall back to the `updated` field | TS-INT-003-04, TS-INT-003-U06 |
| REQ-INT004-01 | EARS-E | Log → render git history mapped to entities and relations | TS-INT-004-01, TS-INT-004-U01, TS-INT-004-U02, TS-INT-004-U04 |
| REQ-INT004-02 | EARS-O | Id given → return only commits touching that entity | TS-INT-004-02, TS-INT-004-U03 |
| REQ-INT004-03 | EARS-U | Present `log` as git history, distinct from `history`'s supersession chain | TS-INT-004-03, TS-INT-004-U05 |
| REQ-INT004-04 | EARS-W | No git history → report it and exit 0 | TS-INT-004-04, TS-INT-004-U06 |

### Business Rules → Test Scenarios

| Rule | Description | Enforcement | Test Scenarios |
|------|-------------|-------------|----------------|
| INT-001 | Validation covers the declared subset; undeclared keys pass unless `--strict` | Validation | TS-INT-001-01, TS-INT-001-U01, TS-INT-001-U03 |
| INT-002 | Referential integrity is part of validate: every relation must resolve | Validation | TS-INT-001-02, TS-INT-001-U02 |
| INT-003 | Files with no recognized frontmatter are skipped, not failed | Constraint | TS-INT-001-04, TS-INT-001-U05 |
| INT-004 | Required-completeness is enforced over active entities only, computed from the schema | Validation | TS-INT-002-02, TS-INT-002-U02, TS-INT-002-U04 |
| INT-005 | A `draft` does not satisfy another entity's required relation | Validation | TS-INT-002-03, TS-INT-002-U05 |
| INT-006 | `check` is the structural gap query: orphans (zero relations) and missing required relations | Validation | TS-INT-002-04, TS-INT-002-U01 |
| INT-011 | A stray file sits inside a type's layout but does not parse as that type; markdown outside every layout is skipped | Validation | TS-INT-002-04, TS-INT-002-U07 |
| INT-012 | An active entity missing a required field/relation is active-but-incomplete; completeness derived from the schema, not `draft` | Validation | TS-INT-002-02, TS-INT-002-U03 |
| INT-007 | The default staleness threshold is 30 days | Constraint | TS-INT-003-01, TS-INT-003-U01, TS-INT-003-U03 |
| INT-008 | `stale` reads git dates but does not write them | Constraint | TS-INT-003-03, TS-INT-003-U05 |
| INT-009 | `log` is derived from git, rendered at ontology altitude | Constraint | TS-INT-004-01, TS-INT-004-U02 |
| INT-010 | `log` (git history) is distinct from `history` (graph supersession chain) | Constraint | TS-INT-004-03, TS-INT-004-U05 |
| INT-SHARED-001 | Completeness is evaluated over the active subgraph only, from the schema; drafts excluded and never satisfy a required relation | Constraint | TS-INT-002-02, TS-INT-002-03, TS-INT-002-U04, TS-INT-002-U05 |
| INT-SHARED-002 | History and staleness are derived from git, never hand-maintained | Constraint | TS-INT-003-03, TS-INT-004-01 |
| INT-SHARED-003 | khub guarantees structural integrity, not semantic correctness | Constraint | TS-INT-001-01, TS-INT-002-01 |
| INT-SHARED-004 | Orphan and stale are core projection properties; `check`/`stale` gate on the same computation | Constraint | TS-INT-002-04, TS-INT-003-01 |

---

## Test Data

### Entities

#### person

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `noor`, `role partner`, reached by inbound `owner` edges | Clean tree, sound graph (TS-INT-001-01, TS-INT-002-01) |
| Draft | `newhire`, `draft: true` | Draft target that does not satisfy a required `owner` relation (TS-INT-002-03) |
| Boundary | `orphan-person` nothing owns, no outbound edge | Orphan (zero in/out) detection (TS-INT-002-04) |

#### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech`, reached by inbound `client` edges | Clean tree, sound graph (TS-INT-001-01, TS-INT-002-01) |
| Invalid | `initech` carrying undeclared `vibe: high` | Strict rejects, non-strict passes (TS-INT-001-03) |
| Removed | `initech` dropped via `remove --force`, leaving `initech-pov`'s `client` dangling | Dangling-edge detection (TS-INT-002-04) |

#### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-deal`, `client → initech`, `owner → noor`, `stage prospect` | Clean tree, sound graph, log subject (TS-INT-001-01, TS-INT-002-01, TS-INT-004-01) |
| Invalid | `bad-stage` with `stage: vibing` (off-enum) | Per-entity error reporting (TS-INT-001-02) |
| Incomplete | `initech-deal` whose required `owner → person/newhire` (draft) | Draft does not satisfy required relation (TS-INT-002-03) |

#### project

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-pov`, `client → initech`, `owner → noor`, `active: true` | Clean tree, sound graph, per-entity `log` filtering (TS-INT-001-01, TS-INT-002-01, TS-INT-004-02) |
| Invalid | `ghost-owner` whose `owner → person/nobody` does not resolve | Unresolved-relation error (TS-INT-001-02) |
| Incomplete | `orphaned-pov` active (`add` default) but missing required `owner` | Active-but-incomplete from the schema (TS-INT-002-02) |

#### meeting

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `kickoff`, `engagement → initech-pov`, `call_type client`, `source recording` | Clean tree, sound graph (explicit engagement edge) (TS-INT-001-01, TS-INT-002-01) |

#### fragment

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `old-note` (`updated: 2026-01-10`), `older-note` (`updated: 2025-12-01`) | Stale set, oldest-first sort (TS-INT-003-01, -02) |
| Boundary | `fresh-note` (`updated: 2026-06-20`) | Within the 30-day window — excluded (TS-INT-003-01) |
| Missing date | `undated` with no `updated`, last commit > 30 days old | Git-date backfill, not written to disk (TS-INT-003-03) |
| Boundary | `a → b → c → a` via universal `depends_on` | Edge cycle on an acyclic predicate (TS-INT-002-05) |

#### reference doc (untyped)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Skipped | `identity/mission.md`, no frontmatter, outside every type layout | Skipped not errored; not counted (TS-INT-001-04) |
| Stray | `clients/notes.md`, malformed, inside the `client` layout | Stray file flagged by `check` (TS-INT-002-04) |

#### supersession fixture (generic, non-firm-ops)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | generic type with self-referential `supersedes` plus a git edit history | `log` returns git commits, not the `supersedes` chain (TS-INT-004-03) |

### Management

- **Setup:** firm-ops scenarios seed the workspace via the FS-001 `init firm-ops` scaffold into a per-test temp dir, then write the prerequisite entities (`person/noor`, `client/initech`, `opportunity/initech-deal`, `project/initech-pov`, `meeting/kickoff`) through `core.create` so referential integrity holds, then build the in-memory index before the `check` under test runs; the Typer `CliRunner` drives the command. Error scenarios write deliberately malformed or unresolved entities directly to disk (bypassing `create`) so `validate`/`check` see the break. Git-derived scenarios (`stale`, `log`) `git init` the temp workspace and commit entities with controlled author dates so the `git log` helper has a real history to read. The supersession-distinction scenario (TS-INT-004-03) instead seeds a generic schema fixture declaring a `supersedes` edge, since firm-ops v1 carries none.
- **Cleanup:** each workspace is written into a `tmp_path` and torn down after the test; no shared `.khub/`, index, or git repo between tests.
- **Isolation:** each scenario scaffolds and validates/checks its own workspace; the read-only assertion for `stale` (INT-008) re-reads the entity file after the run and confirms no `updated` date was written back.

---

## Test Environment

### Stack

| Tool | Purpose |
|------|---------|
| pytest | Unit and integration tests |
| pytest `tmp_path` fixtures | Per-test workspace, index, and git-repo isolation |
| Typer / Click `CliRunner` | Drive `validate`, `check`, `stale`, `log` and capture exit code + output |
| FS-000 compiled schema + generated Pydantic model | The real contract `validate` checks fields, enums, patterns, and cardinality against |
| networkx | The in-memory index `check` walks for orphans, completeness, dangling edges, and cycles |
| git (real, via subprocess) | `git init` + dated commits so `stale` backfill and `log` read a real history |
| Rich (console capture, `force_terminal` toggle) | Assert the TTY table branch (`stale`) and the non-TTY JSON branch |

### Mocks

| Service | Strategy | Rationale |
|---------|----------|-----------|
| Compiled schema | Real, not mocked | Field legality, cardinality, and required-relation completeness only hold against the real compiled contract |
| In-memory index | Real, built from the seeded tree | Orphan, dangling-edge, completeness, and cycle detection need a real `networkx` build, not a stub |
| Filesystem `.khub/` + entity tree | Real temp dir (`tmp_path`) | `validate`/`check` read real Markdown; stray-file and skip behavior depend on real layout paths |
| git | Real repo in `tmp_path`, dated commits via subprocess | `stale` git-backfill and `log` ontology-altitude rendering need genuine commit history; the no-git scenarios omit `git init` to exercise the fallback |

---

## Execution Plan

| Phase | Tests | Gate | Target |
|-------|-------|------|--------|
| 1. Unit | Field/enum/pattern/cardinality, referential resolution, extension passthrough, strict rejection, no-frontmatter skip, collect-all-errors, `--fix` backfill, orphan, completeness-from-schema, draft exemption, draft-does-not-satisfy, dangling, stray, cycle, threshold + sort, git-date read, no-git fallback, commit→entity mapping, altitude render, `--limit`/`--since`, log-vs-history | Block PR | < 30s |
| 2. Integration | Four-verb command surface, per-entity errors + non-zero exit, strict close vs open, reference skip, active-but-incomplete, draft-unsatisfied, orphan/dangling/stray report, cycle report, `--days`, git backfill, no-git message, per-entity `log`, `--since` window, `--format json` shape | Block PR | < 2min |
| 3. E2E | `validate` an HQ snapshot cleanly; `check` surfaces the firm-ops structural gaps; `stale --days 30` surfaces the stale set; `log` describes a recent edit by entity and relation | Block merge | < 5min |

### CI Triggers

- **Pull request:** Phases 1-2 (fast feedback)
- **Main branch:** Phases 1-3 (full coverage, including the snapshot gate)
- **Pre-release:** Phases 1-3 (no separate performance phase; the v1 in-memory index is rebuilt per call and is not yet a runtime hot path — see Risks)

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| FS-004 originally cited HQ-only `decision`/`supersedes` (the AC-005 cycle example and `log decision-0012` in AC-003), both dropped from v1 firm-ops | TS-INT-002-05 and TS-INT-004-03 would assert against constructs the preset cannot produce | Reconciled: FS-004's cycle example now reads `depends_on`, so TS-INT-002-05 runs the universal acyclic `depends_on`; TS-INT-004-03 runs the `log`-vs-`history` distinction against a generic `supersedes` fixture (the TS-003 pattern). FS-004 AC-003 keeps `decision-0012` as an illustration under its preset note |
| Located messages (`Validated N entities; 0 errors`, `Graph check passed`, `No git history; using updated field only`, `No git history available`) and the per-entity error strings (id/field/reason) are asserted verbatim and brittle to wording | TS-INT-001-02/04, TS-INT-002-01, TS-INT-003-04, TS-INT-004-04 break on cosmetic edits | Assert on the variable fields (entity id, field name, count) plus a message substring, keeping the spec's exact text as the canonical example |
| `check` derives completeness from the schema, but a regression could silently read the `draft` flag instead — passing the active-but-incomplete assertion for the wrong reason | TS-INT-002-02/03 would give false confidence that completeness is schema-derived | Assert both directions: an active entity missing a required field FAILS even when published, and a `draft` entity with the same gap is EXEMPT — the flag and the verdict must move independently |
| `stale` and `validate --fix` both read git dates; a regression could let `stale` write the backfilled date, blurring it with `backfill` (FS-005) | TS-INT-003-03 validates the comparison but a silent write corrupts the source | Re-read the entity file on disk after `stale` and assert the frontmatter is byte-unchanged (no `updated` inserted) |
| A false-but-legal write (structurally valid, semantically wrong) passes `validate` and `check` by design; a test that over-asserts could mistake this invariant for a bug | The INT-SHARED-003 guarantee (structural integrity, not semantic truth) could be eroded by a well-meaning "stricter" assertion | Keep one explicit scenario where a legal-but-false entity passes both gates, documenting that git revert — not a gate — is the backstop |
| Cycle safety in `check` relies on the same visited-set discipline as `impact`; a regression loops instead of failing fast | TS-INT-002-05 could hang CI rather than reporting a cycle | Run the cycle scenario under a per-test timeout and assert the reported cycle equals the participating id set exactly |
| The in-memory v1 index is rebuilt from Markdown on every `check`; a large tree makes the graph-wide pass slow as the seed grows toward live HQ scale (140+ transcripts) | Execution-plan targets slip on the E2E snapshot phase | Keep seeded trees minimal per scenario; track the SQLite/FTS fast-follow as the projection that retires per-call rebuilds |
