---
id: WPK-005-1
name: Regenerate the Index and Visualize the Graph
feature-spec: plan/specs/FS-005-projection.md
test-spec: plan/tests/TS-005-projection.md
stories: [STORY-PRJ-001, STORY-PRJ-003]
test-scenarios: [TS-PRJ-001-01, TS-PRJ-001-02, TS-PRJ-001-03, TS-PRJ-001-04, TS-PRJ-001-U01, TS-PRJ-001-U02, TS-PRJ-001-U03, TS-PRJ-001-U04, TS-PRJ-001-U05, TS-PRJ-001-U06, TS-PRJ-003-01, TS-PRJ-003-02, TS-PRJ-003-03, TS-PRJ-003-04, TS-PRJ-003-U01, TS-PRJ-003-U02, TS-PRJ-003-U03, TS-PRJ-003-U04, TS-PRJ-003-U05, TS-PRJ-003-U06]
depends-on: [WPK-002-1, WPK-003-1]
updated: 2026-07-04
---

# Regenerate the Index and Visualize the Graph

## Objective

Delivers the two derived reads on the output side of khub, both computed from the live `networkx` graph and both disposable: `khub reindex` (`core.reindex`) walks the typed graph and regenerates an OKF `index.md`, grouping entities by type with cross-links along resolved edges and stamping the OKF version it conforms to; `khub viz` (`core.viz`) renders the same graph as a self-contained Cytoscape HTML with every asset inlined, nodes colored by type and edges labeled by predicate. `reindex` derives the index from the walked graph and never from the prior `index.md`, honors `--dry-run` (diff-and-no-write), and yields a valid, OKF-stamped, zero-row index for an empty workspace. `viz` defaults its output to `viz.html`, honors `--out`, `--open`, and `--type <t>` (only that type and its incident edges), and opens with no network. Both uphold the projection invariant — every projection is derived from the graph and regenerated on demand, never hand-edited. `reindex` is a cutover requirement (functional, not byte-parity with `kb.py reindex`); `viz` is the lower-criticality member.

> **Preset note:** the index cross-links draw on the OKF core fields (`title`, `description`, `resource`) that `core.yaml`'s base block supplies on **every** type at `khub init` — not on firm-ops-specific fields. `title` is required on `case-study` and optional elsewhere, so index rows fall back to the slug when `title` is absent. Scenarios seed real firm-ops entities each carrying a `title` and assert grouping by `type` plus cross-links along resolved `client`/`owner`/`engagement` edges — which every firm-ops entity carries. The preset is source of truth — see `docs/firm-ops-preset.md`.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-PRJ-001 | AC-001 | Regenerate the index: `khub reindex` walks the typed graph, writes an OKF `index.md` grouping entities by type with cross-links, stamps the OKF version, and displays `Reindexed N entities into index.md` | TS-PRJ-001-01 |
| STORY-PRJ-001 | AC-002 | Dry run: `khub reindex --dry-run` computes the new index, prints the diff against the current `index.md`, and writes nothing | TS-PRJ-001-02 |
| STORY-PRJ-001 | AC-003 | Empty workspace: `khub reindex` with no entities writes a valid, empty OKF `index.md` and displays `Reindexed 0 entities` | TS-PRJ-001-03 |
| STORY-PRJ-001 | AC-004 | HQ cutover: `khub reindex` on an HQ snapshot produces a valid, current OKF `index.md` for the same tree (functional, not byte-parity) | TS-PRJ-001-04 |
| STORY-PRJ-003 | AC-001 | Write a visualization: `khub viz` renders nodes by type and edges by predicate, writes a self-contained Cytoscape HTML (default `viz.html`) with all assets inlined, and displays `Wrote viz.html (N nodes, M edges)` | TS-PRJ-003-01 |
| STORY-PRJ-003 | AC-002 | Custom output and open: `khub viz --out graph.html --open` writes to `graph.html` and opens it in the default browser | TS-PRJ-003-02 |
| STORY-PRJ-003 | AC-003 | Filter by type: `khub viz --type project` renders only `project` nodes and their incident edges | TS-PRJ-003-03 |
| STORY-PRJ-003 | AC-004 | Empty graph: `khub viz` with no entities writes a valid HTML with an empty canvas and displays `Wrote viz.html (0 nodes, 0 edges)` | TS-PRJ-003-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-PRJ-001 | REQ-PRJ001-01 | EARS-E | When reindex runs, the system shall regenerate the OKF `index.md` from the graph and stamp the OKF version | TS-PRJ-001-U01, TS-PRJ-001-U02, TS-PRJ-001-U03, TS-PRJ-001-U06 |
| STORY-PRJ-001 | REQ-PRJ001-02 | EARS-O | Where `--dry-run` is set, the system shall print the diff and write nothing | TS-PRJ-001-U05 |
| STORY-PRJ-001 | REQ-PRJ001-03 | EARS-U | The system shall derive the index from the graph, never from a stored copy | TS-PRJ-001-U04 |
| STORY-PRJ-001 | REQ-PRJ001-04 | EARS-E | When run against an HQ snapshot, the system shall produce a valid, current OKF `index.md` | TS-PRJ-001-04 |
| STORY-PRJ-003 | REQ-PRJ003-01 | EARS-E | When viz runs, the system shall write a self-contained Cytoscape HTML over the typed graph | TS-PRJ-003-U01, TS-PRJ-003-U02, TS-PRJ-003-U05, TS-PRJ-003-U06 |
| STORY-PRJ-003 | REQ-PRJ003-02 | EARS-U | The system shall inline all assets so the output opens with no network access | TS-PRJ-003-U03 |
| STORY-PRJ-003 | REQ-PRJ003-03 | EARS-O | Where `--type <t>` is set, the system shall render only that type and its incident edges | TS-PRJ-003-U04 |
| STORY-PRJ-003 | REQ-PRJ003-04 | EARS-O | Where `--open` is set, the system shall open the file in the default browser | TS-PRJ-003-02 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-PRJ-001 | PRJ-001 | `index.md` is derived from the graph and regenerated, never hand-edited | Constraint | TS-PRJ-001-U01, TS-PRJ-001-U04 |
| STORY-PRJ-001 | PRJ-002 | The index is stamped with the OKF version it conforms to | Automation | TS-PRJ-001-U03 |
| STORY-PRJ-003 | PRJ-005 | The visualization is self-contained; no external assets or server | Constraint | TS-PRJ-003-U03, TS-PRJ-003-U06 |
| STORY-PRJ-003 | PRJ-006 | Nodes are colored by type and edges labeled by predicate | Constraint | TS-PRJ-003-U01, TS-PRJ-003-U02 |
| STORY-PRJ-001, STORY-PRJ-003 | PRJ-SHARED-001 | Every projection is derived from the graph and regenerated on demand | Constraint | TS-PRJ-001-01, TS-PRJ-003-01 |
| STORY-PRJ-001 | PRJ-SHARED-003 | `reindex` and `backfill` are cutover requirements (functional, not byte-parity) | Constraint | TS-PRJ-001-04 |

---

## Entities

Projection reads the firm-ops graph and emits two generated artifacts. The typed entities they render (person, client, opportunity, project, meeting) come from the firm-ops preset and are seeded via `core.create`; the two artifact shapes below are what `reindex` and `viz` produce.

### OKF Index

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| OKF Version | Text | Yes | The OKF version stamped on the index |
| Sections | Collection | Yes | One group per entity type |
| Cross Links | Collection | Yes | Links between related entities, along resolved edges |
| Entity Count | Number | Yes | Number of entities indexed |

### Visualization

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

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

Not applicable — `khub reindex` and `khub viz` are read-only, derived projections. They walk the `networkx` graph and transition no entity state; their output artifacts are disposable and overwritten wholesale on each run. The load-bearing constraint is derived-not-stored (PRJ-001 / REQ-PRJ001-03): `reindex` builds the index from the walked graph and never reads the prior `index.md` as input.

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). Each story maps to one `khub` subcommand. Neither gates — both are no-op-safe reads that write a derived artifact.

### STORY-PRJ-001: Regenerate the OKF Index (`khub reindex`)

- **Library verb:** `core.reindex()` over the `networkx` index
- **Command:** `khub reindex`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| --dry-run | flag | No | Compute the new index, print the diff against the current `index.md`, write nothing |

- **Reads:** the in-memory `networkx` projection (rebuilt from the Markdown entity tree on demand) — never the prior `index.md`
- **Writes:** an OKF `index.md` grouping entities by type with cross-links, stamped with the OKF version
- **Output (success):** written `index.md` and `Reindexed N entities into index.md`; an empty workspace writes a valid, OKF-stamped, zero-row index and displays `Reindexed 0 entities`; under `--dry-run`, the diff against the current file and no write
- **Errors:** (none — an empty workspace is a success)

### STORY-PRJ-003: Visualize the Typed Graph (`khub viz`)

- **Library verb:** `core.viz(out, type)` rendering the `networkx` index to Cytoscape
- **Command:** `khub viz`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| --out | string (default `viz.html`) | No | Output path for the HTML file |
| --open | flag | No | Open the written file in the default browser |
| --type | string | No | Render only that type and its incident edges |

- **Reads:** the in-memory `networkx` projection (rebuilt from Markdown on demand)
- **Writes:** a standalone Cytoscape HTML with every asset inlined — nodes by type, edges by predicate
- **Output (success):** written HTML and `Wrote viz.html (N nodes, M edges)`; an empty graph writes a valid, self-contained HTML with an empty canvas and displays `Wrote viz.html (0 nodes, 0 edges)`
- **Errors:** (none — an empty graph is a success)

---

## Test Data

### person

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `noor`, `role partner`, `title` set, reached by inbound `owner` edges | Index grouping + cross-link target, viz node (TS-PRJ-001-01, TS-PRJ-003-01) |
| Extra | `other`, unrelated to any project | Excluded by `viz --type project` filter (TS-PRJ-003-03) |

### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech`, `title` set, reached by inbound `client` edges | Index grouping + cross-link target, viz node, incident to `project` (TS-PRJ-001-01, TS-PRJ-003-01, TS-PRJ-003-03) |

### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-deal`, `client → initech`, `owner → noor`, `stage prospect`, complete scaffolding | Index grouping (TS-PRJ-001-01) |

### project

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-pov`, `client → initech`, `owner → noor`, `active: true`, `title` set | Index grouping, viz `--type` filter subject (TS-PRJ-001-01, TS-PRJ-003-03) |

### meeting

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `kickoff`, `engagement → initech-pov` | Index cross-link (engagement edge), added by dry-run diff, excluded by project filter (TS-PRJ-001-01, TS-PRJ-001-02, TS-PRJ-003-03) |

### OKF index (generated artifact)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Populated | type-grouped sections, cross-links, OKF-version stamp | Regenerate, HQ snapshot (TS-PRJ-001-01, -04) |
| Stale on disk | prior `index.md` missing the `meeting/kickoff` row | Dry-run diff subject, unchanged-after-run assertion (TS-PRJ-001-02) |
| Empty | valid OKF body, version stamp, zero rows | Empty-workspace reindex (TS-PRJ-001-03) |

### Visualization (generated artifact)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Populated | inline Cytoscape node/edge JSON, no external host reference | Self-contained write, custom out, type filter (TS-PRJ-003-01, -02, -03) |
| Empty | inline assets, empty elements array | Empty-graph canvas (TS-PRJ-003-04) |

---

## Implementation Notes

- **One graph-walk, two renderers (PRJ-SHARED-001).** Both verbs read the same in-memory `networkx` projection, rebuilt from Markdown on demand — `reindex` groups nodes by type and emits cross-links along resolved edges; `viz` serializes nodes-by-type and edges-by-predicate to Cytoscape JSON. Share the graph iteration and the group-by-type step; keep the render step (Markdown vs HTML) separate.
- **Derived-not-stored is load-bearing (PRJ-001 / REQ-PRJ001-03 / TS-PRJ-001-U04).** `reindex` must build the index from the walked graph and never read the prior `index.md` as input. A regression that seeds from the existing file would silently freeze stale content — assert the generator never opens the current `index.md` for reading.
- **Cross-links along edges, not on optional fields (TS-PRJ-001-U02).** Index cross-links draw on OKF core fields (`title`, `description`, `resource`) supplied by `core.yaml`'s base on every type; `title` is required only on `case-study`, optional elsewhere. Seed each index entity with a `title`, prove the slug fallback when `title` is absent, and assert links along resolved `client`/`owner`/`engagement` edges (always present) rather than on any single optional field.
- **OKF-version stamp (PRJ-002 / TS-PRJ-001-U03).** Stamp the OKF version the index conforms to. An empty workspace still writes a valid, OKF-stamped index with its type headings (or an explicit empty marker) and zero rows (AC-003 / TS-PRJ-001-U06).
- **Dry-run diff-and-no-write (REQ-PRJ001-02 / TS-PRJ-001-U05).** Compute the new index, diff it against the current `index.md`, print the diff, write nothing. The stale-on-disk fixture is missing the `meeting/kickoff` row; the diff adds it. Re-read `index.md` after the run and assert it is byte-unchanged.
- **viz self-contained is easy to fake, hard to prove (PRJ-005 / REQ-PRJ003-02 / TS-PRJ-003-U03).** Inline all CSS/JS/assets. Assert the written HTML contains no `http://`, `https://`, or protocol-relative `//` asset reference **and** that the Cytoscape library/CSS bytes are present inline — treat any external host reference as a failure. An empty graph writes a valid HTML with an empty elements array that still opens standalone (AC-004 / TS-PRJ-003-U06).
- **Type filter keeps only incident edges (REQ-PRJ003-03 / TS-PRJ-003-U04).** `viz --type project` renders `project` nodes and the edges incident to them, pulling in only the endpoints of those edges; nodes not incident to a project (`meeting/kickoff`, `person/other`) are excluded. Keep the filter on a code path distinct from the unfiltered render.
- **`--open` is stubbed under test (REQ-PRJ003-04 / TS-PRJ-003-02).** Stub `webbrowser.open` and assert it was invoked with the output path — never launch a real browser in CI. `--out graph.html` writes to that path; the default is `viz.html` (TS-PRJ-003-U05).
- **HQ cutover is functional, not byte-parity (REQ-PRJ001-04 / PRJ-SHARED-003).** The produced `index.md` validates as OKF-conformant and every entity in the snapshot appears under its type grouping — no byte comparison with `kb.py reindex`.
- **YAML I/O uses `ruamel.yaml`** per the repo rule — never PyYAML in `src/` or `tests/`. `reindex` reads frontmatter for the cross-link fields.
- **Not yet implemented (TDD-forward).** There is no `reindex`/`viz` command in `cli/main.py`; `core/project.py` is the FS-002 `status` projection, not these verbs. The scenarios define the contract implementation must meet.
- **Located messages are brittle.** `Reindexed N entities into index.md`, `Reindexed 0 entities`, and `Wrote viz.html (N nodes, M edges)` are asserted verbatim in the spec. Assert on the variable fields (count, node/edge totals, path) plus a stable substring, keeping the exact text as the canonical example.

---

## Done Criteria

- [ ] All 8 acceptance criteria pass (PRJ-001 AC-001 through AC-004; PRJ-003 AC-001 through AC-004)
- [ ] All 8 EARS requirements tested (REQ-PRJ001-01 through -04; REQ-PRJ003-01 through -04)
- [ ] All 4 business rules enforced (PRJ-001, PRJ-002, PRJ-005, PRJ-006), plus PRJ-SHARED-001/003 where they apply
- [ ] All 20 test scenarios pass (TS-PRJ-001-01 through -04 + U01–U06; TS-PRJ-003-01 through -04 + U01–U06)
- [ ] `reindex` writes an OKF `index.md` grouping entities by type with cross-links and the OKF-version stamp; an empty workspace yields a valid, zero-row index
- [ ] `reindex` derives from the walked graph, never reading the prior `index.md`; `--dry-run` prints a diff and leaves `index.md` byte-unchanged on disk
- [ ] `viz` writes a self-contained Cytoscape HTML with no external host reference and the library bytes inline; an empty graph renders a valid, self-contained empty canvas
- [ ] `viz --type project` renders only that type and its incident edges; `--open` invokes the stubbed browser-open with the output path; `--out` overrides the default `viz.html`
- [ ] `reindex` produces a valid OKF `index.md` and `viz` renders the graph on a seeded firm-ops snapshot (E2E)
- [ ] No regressions in existing tests
