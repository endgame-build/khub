---
id: TS-005
name: Projection Test Spec
spec: plan/specs/FS-005-projection.md
updated: 2026-07-04
---

# Projection — Test Spec

## Summary

Tests the projection surface — the output side of khub that renders the typed graph into navigable artifacts. `reindex` walks the graph and regenerates an OKF `index.md`, grouping entities by type with cross-links and stamping the OKF version; `backfill` adds missing `created`/`updated` from git history and missing per-type frontmatter scaffolding, round-trip-preserving authored data; `viz` writes a self-contained Cytoscape HTML with every asset inlined. Coverage runs from the derived-not-stored invariant (every projection is computed from the graph, never hand-edited or read from a stored copy), through the two cutover-critical writers — `reindex`'s grouping, cross-linking, OKF-version stamp, and dry-run diff, and `backfill`'s missing-only writes with git-sourced dates and key/comment preservation — to `viz`'s node/edge serialization, asset inlining, and type filtering, all under the cross-cutting rule that regenerating a projection overwrites the prior artifact because projection output is disposable.

**Feature Spec:** FS-005: Projection
**Stories Covered:** 3
**Test Scenarios:** 12 (plus 18 unit tests)

---

## Test Strategy

### Approach

**Philosophy:** Hybrid — `reindex` and `backfill` are cutover requirements (functional, not byte-parity with `kb.py`), so the discrete mechanics (grouping entities by type, one cross-link, the OKF-version stamp, a single dry-run diff hunk, the missing-field predicate, one first-commit/last-commit date read, a round-trip write that keeps key order and comments, one node/edge serialized to Cytoscape JSON, an asset-inline check, one type filter) are unit-testable in isolation against a built index or a single file, but the guarantees that gate the cutover only hold end to end: `reindex` derives the index from the live graph and never from a stored copy, `--dry-run` writes nothing, an empty workspace still yields a valid OKF `index.md`, `backfill` writes only where a value is missing and never overwrites authored data, a no-git workspace skips date backfill instead of failing, and `viz` opens with no network. Unit tests pin the rules and serializers; integration tests prove the command surface, the displayed counts, the OKF-version stamp, the dry-run diff, git first/last-commit extraction, round-trip preservation, and asset inlining; E2E proves `reindex` and `backfill` run clean on a seeded firm-ops snapshot and `viz` renders the same graph.

**Test Pyramid:**

| Level | Share | Scope |
|-------|-------|-------|
| Unit | ~60% | Group-by-type, cross-link generation, OKF-version stamp, derived-not-stored guard, dry-run diff computation, empty-index index body, missing-field detection, first-commit→`created` / last-commit→`updated` mapping, preserve-existing guard, round-trip key/comment preservation, no-git fallback, node serialization, edge serialization, asset inlining, empty-canvas HTML, type filter + incident-edge selection, out-path default |
| Integration | ~30% | `reindex` command surface + displayed count + OKF stamp, `reindex --dry-run` diff-and-no-write, empty-workspace index, `backfill` date write from git, `backfill --type` frontmatter scaffolding, `backfill --dry-run` list-and-no-write, `backfill` no-git message, `viz` self-contained write + displayed counts, `viz --out --open`, `viz --type` filtering, `viz` empty-graph |
| E2E | ~10% | `reindex` on an HQ snapshot produces a valid OKF `index.md`; `backfill` on an HQ snapshot writes missing dates and frontmatter; `viz` over a seeded firm-ops tree opens and renders the graph |

### Coverage Targets

| Target | Goal |
|--------|------|
| Acceptance criteria | 100% of ACs from FS-005 (12 ACs across 3 stories) |
| EARS requirements | 100% of REQ-PRJ* requirements (12) |
| Business rules | 100% of rule enforcement (PRJ-001..006, PRJ-SHARED-001..003) |
| Edge cases | Empty workspace reindexes to a valid empty index, dry-run writes nothing (both `reindex` and `backfill`), backfill never overwrites an existing value, key order and comments survive a round-trip write, no-git workspace skips date backfill, viz opens with no network, viz empty graph renders an empty canvas, type filter keeps only incident edges |

---

## Test Scenarios

---

### STORY-PRJ-001: Regenerate the OKF Index

**Spec:** As an Operator or Agent, I want to rebuild the OKF `index.md` navigation from the live graph, So that the workspace keeps a current, conformant entry point without hand-maintenance

> **Preset note:** the index cross-links draw on the OKF core fields (`title`, `description`, `resource`) that `core.yaml`'s base block supplies on **every** type at `khub init` — not on firm-ops-specific fields. FS-005's "OKF fields" cross-feature row cites FS-001, but the fields are core-supplied; `title` is required on `case-study` and optional elsewhere, so index rows fall back to the slug when `title` is absent. Scenarios seed real firm-ops entities and assert grouping by `type` plus cross-links along resolved edges, which every firm-ops entity carries. See Risks.

#### TS-PRJ-001-01: Regenerate the Index

**Validates:** AC-001
**Level:** E2E

**Given** a firm-ops workspace whose entities have changed since the last index
**When** the operator runs `khub reindex`
**Then:**
- [ ] the typed graph is walked from the live tree, not a stored copy
- [ ] an OKF `index.md` is written, grouping entities by type with cross-links between related entities
- [ ] the OKF version is stamped on the index
- [ ] `Reindexed N entities into index.md` is displayed
- [ ] exit 0

**Test Data:** a seeded firm-ops workspace — `person/noor`, `client/initech`, `opportunity/initech-deal` (`client → initech`, `owner → noor`, `stage prospect`), `project/initech-pov` (`client → initech`, `owner → noor`, `active: true`), `meeting/kickoff` (`engagement → initech-pov`) — each carrying a `title`; the rendered index groups the five under `opportunity`/`project`/`meeting`/`person`/`client` headings with cross-links along the resolved `client`/`owner`/`engagement` edges

#### TS-PRJ-001-02: Dry Run Prints a Diff and Writes Nothing

**Validates:** AC-002
**Level:** Integration

**Given** an operator who wants to preview the change
**When** they run `khub reindex --dry-run`
**Then:**
- [ ] the new index is computed
- [ ] the diff against the current `index.md` is printed
- [ ] `index.md` on disk is byte-unchanged after the run

**Test Data:** the seed from TS-PRJ-001-01 plus a stale `index.md` on disk missing the `meeting/kickoff` row; `--dry-run` prints a diff that adds the meeting row and the file is re-read afterward and asserted unchanged

#### TS-PRJ-001-03: Empty Workspace

**Validates:** AC-003
**Level:** Integration

**Given** a workspace with no entities
**When** the operator runs `khub reindex`
**Then:**
- [ ] a valid, empty OKF `index.md` is written (well-formed, OKF version stamped, no entity rows)
- [ ] `Reindexed 0 entities` is displayed
- [ ] exit 0

**Test Data:** an `init firm-ops` workspace scaffolded in a `tmp_path` with no entities added — the index renders its type headings (or an explicit empty marker) with zero rows and the OKF-version stamp present

#### TS-PRJ-001-04: HQ Cutover Snapshot

**Validates:** AC-004
**Level:** E2E

**Given** an HQ snapshot
**When** the operator runs `khub reindex`
**Then:**
- [ ] a valid, current OKF `index.md` is produced for the same tree (functional cutover, not byte-parity with `kb.py reindex`)

**Test Data:** a seeded firm-ops snapshot standing in for the HQ tree (the same fixture the FS-004 E2E uses); the produced `index.md` validates as OKF-conformant and every entity in the snapshot appears under its type grouping

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-PRJ-001-U01 | Type grouper | Groups scanned entities into one section per type in a stable order | REQ-PRJ001-01, PRJ-001 |
| TS-PRJ-001-U02 | Cross-link generator | Emits a link between two entities for each resolved relation edge; falls back to slug when `title` is absent | REQ-PRJ001-01 |
| TS-PRJ-001-U03 | OKF-version stamper | Stamps the OKF version the index conforms to onto the generated index | REQ-PRJ001-01, PRJ-002 |
| TS-PRJ-001-U04 | Derived-not-stored guard | Builds the index from the walked graph, never reading the prior `index.md` as input | REQ-PRJ001-03, PRJ-001 |
| TS-PRJ-001-U05 | Dry-run differ | Computes the diff of the new index against the current file without writing | REQ-PRJ001-02 |
| TS-PRJ-001-U06 | Empty-index renderer | Produces a valid, OKF-stamped index body with zero entity rows | REQ-PRJ001-01 |

---

### STORY-PRJ-002: Backfill Frontmatter and Dates

**Spec:** As an Operator, I want to add missing frontmatter and dates from git history, So that an imported or legacy tree reaches a consistent, validatable state at cutover

> **Preset note:** `backfill` shares the `git log` date helper with `stale` (FS-004), but `stale`/`log` read only the **last**-commit date (`core.gitlog.last_commit_date`). `backfill` additionally derives `created` from the **first** commit, which the shared helper does not yet expose — the first-commit read is `backfill`'s extension of the shared surface, exercised by TS-PRJ-002-U03. The no-git fallback reuses `core.gitlog.has_git_history`. See Risks.

#### TS-PRJ-002-01: Backfill Missing Dates From Git

**Validates:** AC-001
**Level:** Integration

**Given** entities with missing `created` or `updated` fields
**When** the operator runs `khub backfill`
**Then:**
- [ ] the first-commit date is read from `git log` per file and written to `created` where missing
- [ ] the last-commit date is written to `updated` where missing
- [ ] existing values, key order, and comments are preserved
- [ ] `Backfilled dates on N entities` is displayed

**Test Data:** a `git init` firm-ops workspace where `fragment/undated` was committed (first commit) then edited (last commit) with neither `created` nor `updated` in frontmatter, and `fragment/dated` carries a hand-kept `created: 2025-01-01` plus an inline comment; after the run `undated` has `created` = first-commit date and `updated` = last-commit date, and `dated`'s `created` and comment are byte-unchanged

#### TS-PRJ-002-02: Backfill Missing Frontmatter Scaffolding

**Validates:** AC-002
**Level:** Integration

**Given** an entity inferable as a type but missing required scaffolding
**When** the operator runs `khub backfill --type opportunity`
**Then:**
- [ ] the missing frontmatter scaffolding for `opportunity` is added
- [ ] entities that already validate are left untouched

**Test Data:** `opportunity/legacy-deal` carrying only `type: opportunity` and a `client` edge but missing the required `stage`/`owner`/`created`/`updated` scaffolding, alongside a complete `opportunity/initech-deal`; `backfill --type opportunity` scaffolds the missing keys on `legacy-deal` and re-reads `initech-deal` asserting it is byte-unchanged

#### TS-PRJ-002-03: Dry Run Lists Changes and Writes Nothing

**Validates:** AC-003
**Level:** Integration

**Given** an operator who wants to preview the writes
**When** they run `khub backfill --dry-run`
**Then:**
- [ ] the entities and fields that would change are listed
- [ ] no file on disk is modified

**Test Data:** the TS-PRJ-002-01 seed; `--dry-run` lists `fragment/undated` with `created` and `updated` as the fields that would change, and every entity file is re-read afterward and asserted byte-unchanged

#### TS-PRJ-002-04: No Git History

**Validates:** AC-004
**Level:** Integration

**Given** a workspace with no git history
**When** the operator runs `khub backfill`
**Then:**
- [ ] date backfill is skipped
- [ ] `No git history; dates not backfilled` is displayed

**Test Data:** a firm-ops workspace scaffolded in a `tmp_path` with no `.git`; `has_git_history` returns false, entities keep their absent dates, and the skip message is displayed (frontmatter scaffolding via `--type` is unaffected by the absence of git)

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-PRJ-002-U01 | Missing-field detector | Flags an entity missing `created`/`updated` or missing required per-type scaffolding | REQ-PRJ002-01 |
| TS-PRJ-002-U02 | Preserve-existing guard | Writes only where a value is missing; never overwrites an existing value | REQ-PRJ002-02, PRJ-003 |
| TS-PRJ-002-U03 | Git-date mapper | Maps first-commit date → `created` and last-commit date → `updated` from `git log` | REQ-PRJ002-01, PRJ-004 |
| TS-PRJ-002-U04 | Round-trip writer | Preserves key order and comments across the backfill write (minimal-diff round-trip) | REQ-PRJ002-02, PRJ-003 |
| TS-PRJ-002-U05 | No-git fallback | Skips date backfill when the workspace has no git history | REQ-PRJ002-04 |
| TS-PRJ-002-U06 | Dry-run collector | Lists the entities and fields that would change and returns without writing | REQ-PRJ002-03 |

---

### STORY-PRJ-003: Visualize the Typed Graph

**Spec:** As an Operator or Agent, I want to render the typed graph as a self-contained interactive HTML, So that I can see the entity and relation structure without a server or extra tooling

#### TS-PRJ-003-01: Write a Visualization

**Validates:** AC-001
**Level:** E2E

**Given** a workspace with entities and edges
**When** the operator runs `khub viz`
**Then:**
- [ ] nodes are rendered by type and edges by predicate
- [ ] a self-contained Cytoscape HTML file is written (default `viz.html`)
- [ ] all assets are inlined so the file opens with no network
- [ ] `Wrote viz.html (N nodes, M edges)` is displayed
- [ ] exit 0

**Test Data:** the TS-PRJ-001-01 firm-ops seed — five nodes across five types, with `client`/`owner`/`engagement` edges; the written `viz.html` contains the Cytoscape node/edge JSON inline, references no `http(s)://` or `//` external asset, and the displayed counts equal the node and edge totals

#### TS-PRJ-003-02: Custom Output and Open

**Validates:** AC-002
**Level:** Integration

**Given** a chosen output path
**When** the operator runs `khub viz --out graph.html --open`
**Then:**
- [ ] the HTML is written to `graph.html`
- [ ] the file is opened in the default browser

**Test Data:** the same seed; `--out graph.html` writes to that path and the browser-open call is stubbed (`webbrowser.open`) and asserted to have been invoked with the `graph.html` path — no real browser launched under test

#### TS-PRJ-003-03: Filter by Type

**Validates:** AC-003
**Level:** Integration

**Given** a large graph
**When** the operator runs `khub viz --type project`
**Then:**
- [ ] only `project` nodes and their incident edges are rendered

**Test Data:** the seed extended with a second unrelated `person/other`; `--type project` renders `project/initech-pov` and the edges incident to it (`client → initech`, `owner → noor`), pulling in only the endpoints of those edges, and excludes `meeting/kickoff` and `person/other` which are not incident to a project node

#### TS-PRJ-003-04: Empty Graph

**Validates:** AC-004
**Level:** Integration

**Given** a workspace with no entities
**When** the operator runs `khub viz`
**Then:**
- [ ] a valid HTML with an empty canvas is written
- [ ] `Wrote viz.html (0 nodes, 0 edges)` is displayed

**Test Data:** an `init firm-ops` workspace with no entities; `viz.html` is written with an empty Cytoscape elements array and still opens standalone (assets inlined), and the zero-count message is displayed

#### Unit Tests

| ID | Component | Behavior | Validates |
|----|-----------|----------|-----------|
| TS-PRJ-003-U01 | Node serializer | Serializes a node to Cytoscape JSON tagged with its type (for by-type coloring) | REQ-PRJ003-01, PRJ-006 |
| TS-PRJ-003-U02 | Edge serializer | Serializes an edge to Cytoscape JSON labeled with its predicate | REQ-PRJ003-01, PRJ-006 |
| TS-PRJ-003-U03 | Asset inliner | Inlines all CSS/JS/assets so the output references no external host | REQ-PRJ003-02, PRJ-005 |
| TS-PRJ-003-U04 | Type filter | Keeps only nodes of the given type and their incident edges | REQ-PRJ003-03 |
| TS-PRJ-003-U05 | Out-path default | Defaults the output path to `viz.html` when `--out` is absent | REQ-PRJ003-01 |
| TS-PRJ-003-U06 | Empty-canvas renderer | Emits valid, self-contained HTML with an empty elements array for a graph with no nodes | REQ-PRJ003-01, PRJ-005 |

---

## Coverage Matrix

### Acceptance Criteria → Test Scenarios

| Story | AC | Description | Test Scenarios |
|-------|----|-------------|----------------|
| STORY-PRJ-001 | AC-001 | Regenerate the index | TS-PRJ-001-01 |
| STORY-PRJ-001 | AC-002 | Dry run | TS-PRJ-001-02 |
| STORY-PRJ-001 | AC-003 | Empty workspace | TS-PRJ-001-03 |
| STORY-PRJ-001 | AC-004 | HQ cutover | TS-PRJ-001-04 |
| STORY-PRJ-002 | AC-001 | Backfill missing dates | TS-PRJ-002-01 |
| STORY-PRJ-002 | AC-002 | Backfill missing frontmatter | TS-PRJ-002-02 |
| STORY-PRJ-002 | AC-003 | Dry run | TS-PRJ-002-03 |
| STORY-PRJ-002 | AC-004 | No git history | TS-PRJ-002-04 |
| STORY-PRJ-003 | AC-001 | Write a visualization | TS-PRJ-003-01 |
| STORY-PRJ-003 | AC-002 | Custom output and open | TS-PRJ-003-02 |
| STORY-PRJ-003 | AC-003 | Filter by type | TS-PRJ-003-03 |
| STORY-PRJ-003 | AC-004 | Empty graph | TS-PRJ-003-04 |

### EARS Requirements → Test Scenarios

| Requirement | Type | Description | Test Scenarios |
|-------------|------|-------------|----------------|
| REQ-PRJ001-01 | EARS-E | Reindex → regenerate the OKF `index.md` from the graph and stamp the OKF version | TS-PRJ-001-01, TS-PRJ-001-03, TS-PRJ-001-U01, TS-PRJ-001-U02, TS-PRJ-001-U03, TS-PRJ-001-U06 |
| REQ-PRJ001-02 | EARS-O | `--dry-run` → print the diff and write nothing | TS-PRJ-001-02, TS-PRJ-001-U05 |
| REQ-PRJ001-03 | EARS-U | Derive the index from the graph, never from a stored copy | TS-PRJ-001-01, TS-PRJ-001-U04 |
| REQ-PRJ001-04 | EARS-E | Run against an HQ snapshot → produce a valid, current OKF `index.md` | TS-PRJ-001-04 |
| REQ-PRJ002-01 | EARS-E | Backfill → write missing `created`/`updated` from `git log` | TS-PRJ-002-01, TS-PRJ-002-U01, TS-PRJ-002-U03 |
| REQ-PRJ002-02 | EARS-U | Preserve existing values, key order, and comments on backfill | TS-PRJ-002-01, TS-PRJ-002-02, TS-PRJ-002-U02, TS-PRJ-002-U04 |
| REQ-PRJ002-03 | EARS-O | `--dry-run` → list changes and write nothing | TS-PRJ-002-03, TS-PRJ-002-U06 |
| REQ-PRJ002-04 | EARS-W | No git history → skip date backfill | TS-PRJ-002-04, TS-PRJ-002-U05 |
| REQ-PRJ003-01 | EARS-E | Viz → write a self-contained Cytoscape HTML over the typed graph | TS-PRJ-003-01, TS-PRJ-003-04, TS-PRJ-003-U01, TS-PRJ-003-U02, TS-PRJ-003-U05, TS-PRJ-003-U06 |
| REQ-PRJ003-02 | EARS-U | Inline all assets so the output opens with no network access | TS-PRJ-003-01, TS-PRJ-003-U03 |
| REQ-PRJ003-03 | EARS-O | `--type <t>` → render only that type and its incident edges | TS-PRJ-003-03, TS-PRJ-003-U04 |
| REQ-PRJ003-04 | EARS-O | `--open` → open the file in the default browser | TS-PRJ-003-02 |

### Business Rules → Test Scenarios

| Rule | Description | Enforcement | Test Scenarios |
|------|-------------|-------------|----------------|
| PRJ-001 | `index.md` is derived from the graph and regenerated, never hand-edited | Constraint | TS-PRJ-001-01, TS-PRJ-001-U01, TS-PRJ-001-U04 |
| PRJ-002 | The index is stamped with the OKF version it conforms to | Automation | TS-PRJ-001-01, TS-PRJ-001-U03 |
| PRJ-003 | Backfill writes only where a value is missing; it never overwrites existing data | Constraint | TS-PRJ-002-01, TS-PRJ-002-02, TS-PRJ-002-U02, TS-PRJ-002-U04 |
| PRJ-004 | Backfilled dates come from `git log`, the authoritative history | Automation | TS-PRJ-002-01, TS-PRJ-002-U03 |
| PRJ-005 | The visualization is self-contained; no external assets or server | Constraint | TS-PRJ-003-01, TS-PRJ-003-04, TS-PRJ-003-U03, TS-PRJ-003-U06 |
| PRJ-006 | Nodes are colored by type and edges labeled by predicate | Constraint | TS-PRJ-003-01, TS-PRJ-003-U01, TS-PRJ-003-U02 |
| PRJ-SHARED-001 | Every projection is derived from the graph and regenerated on demand | Constraint | TS-PRJ-001-01, TS-PRJ-003-01 |
| PRJ-SHARED-002 | Writes preserve authored data; round-trip keeps key order and comments | Constraint | TS-PRJ-002-01, TS-PRJ-002-U04 |
| PRJ-SHARED-003 | `reindex` and `backfill` are cutover requirements (functional, not byte-parity) | Constraint | TS-PRJ-001-04, TS-PRJ-002-01 |

---

## Test Data

### Entities

#### person

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `noor`, `role partner`, `title` set, reached by inbound `owner` edges | Index grouping + cross-link target, viz node (TS-PRJ-001-01, TS-PRJ-003-01) |
| Extra | `other`, unrelated to any project | Excluded by `viz --type project` filter (TS-PRJ-003-03) |

#### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech`, `title` set, reached by inbound `client` edges | Index grouping + cross-link target, viz node, incident to `project` (TS-PRJ-001-01, TS-PRJ-003-01, TS-PRJ-003-03) |

#### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-deal`, `client → initech`, `owner → noor`, `stage prospect`, complete scaffolding | Index grouping, backfill leaves-untouched control (TS-PRJ-001-01, TS-PRJ-002-02) |
| Incomplete | `legacy-deal`, only `type` + `client`, missing `stage`/`owner`/`created`/`updated` | Frontmatter scaffolding backfill (TS-PRJ-002-02) |

#### project

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-pov`, `client → initech`, `owner → noor`, `active: true`, `title` set | Index grouping, viz `--type` filter subject (TS-PRJ-001-01, TS-PRJ-003-03) |

#### meeting

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `kickoff`, `engagement → initech-pov` | Index cross-link (engagement edge), added by dry-run diff, excluded by project filter (TS-PRJ-001-01, TS-PRJ-001-02, TS-PRJ-003-03) |

#### fragment

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Missing dates | `undated`, no `created`/`updated`, first + last commit dated in git | Git first/last-commit date backfill (TS-PRJ-002-01, -03) |
| Preserved | `dated`, hand-kept `created: 2025-01-01` + inline comment | Preserve-existing + round-trip comment preservation (TS-PRJ-002-01) |

#### OKF index (generated artifact)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Populated | type-grouped sections, cross-links, OKF-version stamp | Regenerate, HQ snapshot (TS-PRJ-001-01, -04) |
| Stale on disk | prior `index.md` missing the `meeting/kickoff` row | Dry-run diff subject, unchanged-after-run assertion (TS-PRJ-001-02) |
| Empty | valid OKF body, version stamp, zero rows | Empty-workspace reindex (TS-PRJ-001-03) |

#### Visualization (generated artifact)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Populated | inline Cytoscape node/edge JSON, no external host reference | Self-contained write, custom out, type filter (TS-PRJ-003-01, -02, -03) |
| Empty | inline assets, empty elements array | Empty-graph canvas (TS-PRJ-003-04) |

### Management

- **Setup:** firm-ops scenarios seed the workspace via the FS-001 `init firm-ops` scaffold into a per-test temp dir, then write the prerequisite entities (`person/noor`, `client/initech`, `opportunity/initech-deal`, `project/initech-pov`, `meeting/kickoff`) through `core.create` so referential integrity holds and the in-memory index resolves, then invoke the projection verb under test via the Typer `CliRunner`. Backfill scenarios `git init` the temp workspace and commit entities with controlled author dates (a distinct first and last commit for `fragment/undated`) so the `git log` helper has a real first/last-commit history to read; incomplete entities are written directly to disk (bypassing `create`) so the missing scaffolding is genuinely absent. The stale-index dry-run scenario writes a hand-authored `index.md` before the run. The `viz --open` scenario stubs `webbrowser.open` so no real browser launches.
- **Cleanup:** each workspace is written into a `tmp_path` and torn down after the test; no shared `.khub/`, index, git repo, or generated artifact between tests.
- **Isolation:** each scenario scaffolds and projects its own workspace; the no-write assertions (`reindex --dry-run`, `backfill --dry-run`) re-read the target files after the run and assert them byte-unchanged; the preserve-existing assertions re-read the entity file and confirm authored values, key order, and comments survive.

---

## Test Environment

### Stack

| Tool | Purpose |
|------|---------|
| pytest | Unit and integration tests |
| pytest `tmp_path` fixtures | Per-test workspace, index, git-repo, and artifact isolation |
| Typer / Click `CliRunner` | Drive `reindex`, `backfill`, `viz` and capture exit code + output |
| networkx | The in-memory index `reindex` and `viz` walk for grouping, cross-links, and node/edge serialization |
| ruamel.yaml (round-trip) | `backfill` writes that preserve key order and comments (repo YAML rule) |
| git (real, via subprocess) | `git init` + dated commits so `backfill` reads real first/last-commit dates |
| `webbrowser` (stubbed) | Assert `viz --open` invokes the default-browser open without launching one |

### Mocks

| Service | Strategy | Rationale |
|---------|----------|-----------|
| In-memory index | Real, built from the seeded tree | Grouping, cross-link resolution, and node/edge serialization need a real `networkx` build, not a stub |
| Filesystem `.khub/` + entity tree | Real temp dir (`tmp_path`) | `reindex`/`backfill`/`viz` read real Markdown and write real artifacts; round-trip preservation depends on real files |
| git | Real repo in `tmp_path`, dated commits via subprocess | `backfill`'s first/last-commit date reads need genuine commit history; the no-git scenario omits `git init` to exercise the fallback |
| `webbrowser.open` | Stubbed | `viz --open` must be assertable without launching a browser in CI |

---

## Execution Plan

| Phase | Tests | Gate | Target |
|-------|-------|------|--------|
| 1. Unit | Group-by-type, cross-link, OKF stamp, derived-not-stored, dry-run diff, empty index, missing-field, git first/last mapping, preserve-existing, round-trip, no-git fallback, node/edge serialization, asset inlining, type filter, out-path default, empty canvas | Block PR | < 30s |
| 2. Integration | `reindex` surface + count + OKF stamp, `reindex --dry-run` diff-and-no-write, empty-workspace index, `backfill` git-date write, `backfill --type` scaffolding, `backfill --dry-run` list-and-no-write, `backfill` no-git message, `viz` self-contained write + counts, `viz --out --open`, `viz --type` filter, `viz` empty-graph | Block PR | < 2min |
| 3. E2E | `reindex` an HQ snapshot to a valid OKF `index.md`; `backfill` an HQ snapshot writes missing dates and frontmatter; `viz` over a seeded firm-ops tree renders the graph | Block merge | < 5min |

### CI Triggers

- **Pull request:** Phases 1-2 (fast feedback)
- **Main branch:** Phases 1-3 (full coverage, including the snapshot gate)
- **Pre-release:** Phases 1-3 (no separate performance phase; the v1 in-memory index is rebuilt per projection and is not yet a runtime hot path — see Risks)

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `reindex`, `backfill`, and `viz` are unimplemented at authoring time (no `reindex`/`backfill`/`viz` command in `cli/main.py`; `core/project.py` is the FS-002 `status` projection, not these verbs) | Scenarios describe a command surface that does not yet exist; message strings and flag names are the spec's, not observed | Treat this as a TDD-forward spec: the scenarios define the contract implementation must meet. Ground the seed, edges, and types on the real firm-ops preset (which exists), and pin displayed strings to FS-005's exact text as canonical examples, asserting on the variable fields (count, path) plus a message substring |
| The index cross-links depend on OKF core fields (`title`, `description`, `resource`) from `core.yaml`'s base block; `title` is required only on `case-study`, optional elsewhere, so firm-ops entities may lack it | TS-PRJ-001-01/U02 could assert a `title`-based link that is absent on a real entity | Seed each index entity with a `title`, and specify the slug fallback in TS-PRJ-001-U02 so the cross-link generator is proven both with and without `title`; assert links along resolved edges (always present) rather than on any single optional field |
| `backfill` shares the `git log` date helper with `stale`, but `stale`/`log` read only `last_commit_date`; `created` from the first commit is a new read the shared helper does not expose | TS-PRJ-002-01/U03 could assume a first-commit helper that does not exist, or silently map both dates to last-commit | Commit `fragment/undated` twice with distinct dates and assert `created` equals the **first** commit date and `updated` the **last** — the two must differ, forcing a genuine first-commit read rather than a last-commit alias |
| Displayed messages (`Reindexed N entities into index.md`, `Reindexed 0 entities`, `Backfilled dates on N entities`, `No git history; dates not backfilled`, `Wrote viz.html (N nodes, M edges)`) are asserted verbatim and brittle to wording | Every integration scenario breaks on a cosmetic edit | Assert on the variable fields (count, node/edge totals, path) plus a stable substring, keeping the spec's exact text as the canonical example |
| A `backfill` regression could overwrite an authored value or reorder keys/strip comments while writing missing ones | TS-PRJ-002-01/02 would silently corrupt the source, the opposite of the round-trip guarantee | Assert both directions: the missing field is written AND a re-read of an entity with an existing value confirms its value, key order, and inline comment are byte-unchanged (ruamel round-trip, per the repo YAML rule) |
| `viz` "self-contained" is easy to assert weakly (file exists) and hard to assert strongly (opens with no network) | TS-PRJ-003-01/04 could pass while the HTML still fetches Cytoscape from a CDN | Assert the written HTML contains no `http://`, `https://`, or protocol-relative `//` asset reference and that the Cytoscape library/CSS bytes are present inline; treat any external host reference as a failure |
| The in-memory index is rebuilt from Markdown on every projection; a large snapshot makes `reindex`/`viz` slow as the seed grows toward live HQ scale (140+ transcripts) | Execution-plan targets slip on the E2E snapshot phase | Keep seeded trees minimal per scenario; track the SQLite/FTS fast-follow (deferred) as the projection that retires per-call rebuilds |
| `sqlite` and `okf-bundle` projection artifacts and preset-drift/promote-back are deferred out of v1 (FS-005 Scope Changes) | Tests here could over-reach into fast-follow surface not yet specced | Scope this spec to `reindex`/`backfill`/`viz` only; the deferred artifacts get their own test spec when their features land |
