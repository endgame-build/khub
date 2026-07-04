"""TS-005 — the projection surface (WPK-005-1 reindex/viz, WPK-005-2 backfill).

`reindex` regenerates the OKF `index.md` from the live graph; `viz` writes a
self-contained Cytoscape HTML; `backfill` adds missing dates (from git) and per-type
scaffolding, round-trip-preserving authored data. Firm-ops seeds go through
`core.create` so referential integrity holds; incomplete/legacy entities are written
directly so the missing scaffolding is genuinely absent. Git fixtures commit with
controlled author dates so the first/last-commit reads have real history.
"""

from __future__ import annotations

import os
import subprocess
from pathlib import Path
from typing import Callable

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core import entity
from khub.core.backfill import backfill
from khub.core.graph import build_graph
from khub.core.index import build_index, filter_index, stray_nodes
from khub.core.integrity import validate
from khub.core.introspect import load_schema
from khub.core.reindex import OKF_VERSION, build_index_doc, cross_links, group_by_type, reindex
from khub.core.viz import _select, render_html, to_cytoscape, viz

runner = CliRunner()
Seed = Callable[..., None]


# --- helpers -----------------------------------------------------------------


def seed_firm_ops(root: Path) -> None:
    """The shared firm-ops seed: five entities across five types, each carrying a title."""
    entity.create(root, "person", {"name": "Noor", "role": "partner", "title": "Noor P"}, id_="noor")
    entity.create(root, "client", {"name": "Initech", "title": "Initech"}, id_="initech")
    entity.create(
        root, "opportunity",
        {"stage": "prospect", "client": "initech", "owner": "noor", "title": "Initech Deal"},
        id_="initech-deal",
    )
    entity.create(
        root, "project",
        {"client": "initech", "owner": "noor", "active": "true", "title": "Initech PoV"},
        id_="initech-pov",
    )
    entity.create(
        root, "meeting",
        {"date": "2026-06-01T10:00:00", "call_type": "client", "source": "recording",
         "engagement": "initech-pov", "title": "Kickoff"},
        id_="kickoff",
    )


def _live(root: Path):  # noqa: ANN202
    """(resolved, valid index, graph) over the live tree — the projection's input."""
    resolved = load_schema(root)
    scanned = build_index(root, resolved)
    index = filter_index(scanned, stray_nodes(scanned))
    return resolved, index, build_graph(index)


def _git(root: Path, *args: str, when: str | None = None) -> None:
    env = None
    if when:
        env = os.environ.copy()
        env["GIT_AUTHOR_DATE"] = env["GIT_COMMITTER_DATE"] = when
    subprocess.run(["git", "-C", str(root), *args], check=True, capture_output=True, text=True, env=env)


def _git_init(root: Path) -> None:
    _git(root, "init")
    _git(root, "config", "user.email", "t@t")
    _git(root, "config", "user.name", "t")


def _commit(root: Path, message: str, when: str) -> None:
    _git(root, "add", "-A")
    _git(root, "commit", "-m", message, when=when)


def _no_external_assets(html: str) -> None:
    """No `<script src>` / `<link href>` points at a host — self-containment (PRJ-005)."""
    for attr in ("http", "//"):
        for tag in (f'src="{attr}', f"src='{attr}", f'href="{attr}', f"href='{attr}"):
            assert tag not in html, f"external asset reference {tag!r} in output"


# ============================================================================
# STORY-PRJ-001 — Regenerate the OKF Index
# ============================================================================


@pytest.fixture
def firm_ops_ws(fresh_ws: Path) -> Path:
    """A seeded firm-ops workspace: the five-entity graph reindex and viz render."""
    seed_firm_ops(fresh_ws)
    return fresh_ws


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_group_by_type_stable_order(firm_ops_ws: Path) -> None:
    """TS-PRJ-001-U01 (REQ-PRJ001-01, PRJ-001): one section per type, in schema order."""
    resolved, index, _ = _live(firm_ops_ws)
    groups = group_by_type(index.nodes, resolved)
    # firm-ops declares opportunity, project, meeting, ... person, client — that order.
    assert list(groups) == ["opportunity", "project", "meeting", "person", "client"]
    assert groups["opportunity"] == ["initech-deal"]


@pytest.mark.unit
def test_cross_links_edges_and_slug_fallback(fresh_ws: Path, seed: Seed) -> None:
    """TS-PRJ-001-U02 (REQ-PRJ001-01): a link per resolved edge; slug fallback when no title."""
    seed(fresh_ws, "fragments/titled.md", type="fragment", stage="raw", title="Titled")
    seed(fresh_ws, "fragments/plain.md", type="fragment", stage="raw")  # no title
    seed(fresh_ws, "fragments/src.md", type="fragment", stage="raw", related=["titled", "plain"])
    resolved, index, graph = _live(fresh_ws)
    links = cross_links(fresh_ws, resolved, index, graph, ("fragment", "src"))
    assert "related → [Titled](fragments/titled.md)" in links  # title used when present
    assert "related → [plain](fragments/plain.md)" in links  # slug fallback when absent


@pytest.mark.unit
def test_index_stamps_okf_version(firm_ops_ws: Path) -> None:
    """TS-PRJ-001-U03 (REQ-PRJ001-01, PRJ-002): the OKF version is stamped on the index."""
    content, count = build_index_doc(firm_ops_ws)
    meta = frontmatter.loads(content).metadata
    assert meta["okf_version"] == OKF_VERSION
    assert meta["entity_count"] == count == 5


@pytest.mark.unit
def test_reindex_derived_not_stored(firm_ops_ws: Path) -> None:
    """TS-PRJ-001-U04 (REQ-PRJ001-03, PRJ-001): the index is built from the graph, not a stored copy."""
    stale = firm_ops_ws / "index.md"
    stale.write_text("---\nokf_version: '0.1'\n---\n# Index\n\nSTALE-MARKER only\n")
    content, _ = build_index_doc(firm_ops_ws)
    assert "STALE-MARKER" not in content  # the prior file was never read as input
    assert "initech-deal" in content  # the live graph drives the output


@pytest.mark.unit
def test_dry_run_diff_computed_no_write(firm_ops_ws: Path) -> None:
    """TS-PRJ-001-U05 (REQ-PRJ001-02): the dry-run diff is computed and nothing is written."""
    path = firm_ops_ws / "index.md"
    path.write_text("---\nokf_version: '0.1'\nentity_count: 0\n---\n# Index\n\n_No entities._\n")
    before = path.read_bytes()
    result = reindex(firm_ops_ws, dry_run=True)
    assert not result.wrote and result.diff
    assert "initech-deal" in result.diff  # the added rows show as additions
    assert path.read_bytes() == before  # byte-unchanged


@pytest.mark.unit
def test_empty_index_renderer(fresh_ws: Path) -> None:
    """TS-PRJ-001-U06 (REQ-PRJ001-01): an empty workspace renders a valid, stamped, zero-row index."""
    content, count = build_index_doc(fresh_ws)
    meta = frontmatter.loads(content).metadata
    assert count == 0 and meta["okf_version"] == OKF_VERSION and meta["entity_count"] == 0
    assert "_No entities._" in content


# --- integration / e2e -------------------------------------------------------


@pytest.mark.e2e
def test_reindex_regenerates_index(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-PRJ-001-01 (AC-001): reindex writes a grouped, cross-linked, stamped OKF index.md."""
    monkeypatch.chdir(firm_ops_ws)
    out = runner.invoke(app, ["reindex"])
    assert out.exit_code == 0
    assert "Reindexed 5 entities into index.md" in out.output
    text = (firm_ops_ws / "index.md").read_text()
    meta = frontmatter.loads(text).metadata
    assert meta["okf_version"] == OKF_VERSION  # stamped
    for heading in ("## opportunity", "## project", "## meeting", "## person", "## client"):
        assert heading in text  # grouped by type
    assert "owner → [Noor P](identity/team/noor.md)" in text  # cross-link along the owner edge
    assert "engagement → [Initech PoV](projects/initech-pov/_index.md)" in text  # engagement edge


@pytest.mark.integration
def test_reindex_dry_run_prints_diff_no_write(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-PRJ-001-02 (AC-002): --dry-run prints a diff adding the meeting row; index.md unchanged."""
    stale = firm_ops_ws / "index.md"
    # A prior index missing the meeting/kickoff row.
    stale.write_text(
        "---\nokf_version: '0.1'\nentity_count: 4\n---\n# Index\n\n"
        "## opportunity\n\n- [Initech Deal](opportunities/initech-deal/_index.md)\n"
    )
    before = stale.read_bytes()
    monkeypatch.chdir(firm_ops_ws)
    out = runner.invoke(app, ["reindex", "--dry-run"])
    assert out.exit_code == 0
    assert "+## meeting" in out.output and "kickoff" in out.output  # the added meeting row
    assert stale.read_bytes() == before  # byte-unchanged after a dry run


@pytest.mark.integration
def test_reindex_empty_workspace(fresh_ws: Path, monkeypatch) -> None:
    """TS-PRJ-001-03 (AC-003): an empty workspace writes a valid empty index and says so."""
    monkeypatch.chdir(fresh_ws)
    out = runner.invoke(app, ["reindex"])
    assert out.exit_code == 0
    assert "Reindexed 0 entities" in out.output
    meta = frontmatter.loads((fresh_ws / "index.md").read_text()).metadata
    assert meta["okf_version"] == OKF_VERSION and meta["entity_count"] == 0


@pytest.mark.e2e
def test_reindex_hq_cutover_snapshot(firm_ops_ws: Path) -> None:
    """TS-PRJ-001-04 (AC-004): a seeded snapshot produces a valid OKF index with every entity."""
    result = reindex(firm_ops_ws)
    text = result.path.read_text()
    meta = frontmatter.loads(text).metadata
    assert meta["okf_version"] == OKF_VERSION and meta["entity_count"] == 5  # OKF-conformant
    for slug in ("noor", "initech", "initech-deal", "initech-pov", "kickoff"):
        assert slug in text  # every snapshot entity appears under its type grouping


# ============================================================================
# STORY-PRJ-003 — Visualize the Typed Graph
# ============================================================================


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_node_serializer_tags_type(firm_ops_ws: Path) -> None:
    """TS-PRJ-003-U01 (REQ-PRJ003-01, PRJ-006): a node serializes tagged with its type."""
    _, _, graph = _live(firm_ops_ws)
    nodes, edges = _select(graph, None)
    els = to_cytoscape(nodes, edges)
    deal = next(n for n in els["nodes"] if n["data"]["id"] == "opportunity/initech-deal")
    assert deal["data"]["type"] == "opportunity"  # drives by-type coloring


@pytest.mark.unit
def test_edge_serializer_labels_predicate(firm_ops_ws: Path) -> None:
    """TS-PRJ-003-U02 (REQ-PRJ003-01, PRJ-006): an edge serializes labeled with its predicate."""
    _, _, graph = _live(firm_ops_ws)
    nodes, edges = _select(graph, None)
    els = to_cytoscape(nodes, edges)
    owner_edges = [e for e in els["edges"] if e["data"]["label"] == "owner"]
    assert owner_edges  # the owner predicate is carried as the edge label


@pytest.mark.unit
def test_asset_inliner_no_external_host(firm_ops_ws: Path) -> None:
    """TS-PRJ-003-U03 (REQ-PRJ003-02, PRJ-005): all assets inline; no external host reference."""
    _, _, graph = _live(firm_ops_ws)
    nodes, edges = _select(graph, None)
    html = render_html(to_cytoscape(nodes, edges), types=["opportunity"])
    _no_external_assets(html)
    assert "cytoscape" in html and len(html) > 200_000  # the library bytes are inline


@pytest.mark.unit
def test_type_filter_keeps_incident_only(firm_ops_ws: Path, seed: Seed) -> None:
    """TS-PRJ-003-U04 (REQ-PRJ003-03): --type keeps that type, its outgoing edges, and their targets."""
    seed(firm_ops_ws, "identity/team/other.md", type="person", name="Other", role="consultant")
    _, _, graph = _live(firm_ops_ws)
    nodes, edges = _select(graph, "project")
    node_set = set(nodes)
    assert ("project", "initech-pov") in node_set
    assert {("client", "initech"), ("person", "noor")} <= node_set  # the edges' endpoints
    assert ("meeting", "kickoff") not in node_set  # inbound-only, not pulled in
    assert ("person", "other") not in node_set  # unrelated
    assert all(u[0] == "project" for (u, _v, _p) in edges)  # only edges leaving a project


@pytest.mark.unit
def test_out_path_default(firm_ops_ws: Path) -> None:
    """TS-PRJ-003-U05 (REQ-PRJ003-01): the default output path is viz.html."""
    result = viz(firm_ops_ws)
    assert result.path == firm_ops_ws / "viz.html" and result.path.exists()


@pytest.mark.unit
def test_empty_canvas_renderer(fresh_ws: Path) -> None:
    """TS-PRJ-003-U06 (REQ-PRJ003-01, PRJ-005): an empty graph renders a valid, self-contained canvas."""
    result = viz(fresh_ws)
    assert result.nodes == 0 and result.edges == 0
    html = result.path.read_text()
    _no_external_assets(html)
    assert "var elements = [];" in html and "cytoscape" in html


# --- integration / e2e -------------------------------------------------------


@pytest.mark.e2e
def test_viz_writes_self_contained(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-PRJ-003-01 (AC-001): viz writes a self-contained HTML and reports node/edge counts."""
    monkeypatch.chdir(firm_ops_ws)
    out = runner.invoke(app, ["viz"])
    assert out.exit_code == 0
    html = (firm_ops_ws / "viz.html").read_text()
    _no_external_assets(html)
    assert "cytoscape" in html and len(html) > 200_000
    # 5 nodes; edges = opportunity(client,owner) + project(client,owner) + meeting(engagement) = 5.
    assert "Wrote viz.html (5 nodes, 5 edges)" in out.output


@pytest.mark.integration
def test_viz_custom_out_and_open(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-PRJ-003-02 (AC-002, REQ-PRJ003-04): --out writes graph.html and --open opens it."""
    opened: list[str] = []
    monkeypatch.setattr("khub.cli.projection_cmd.webbrowser.open", lambda url: opened.append(url))
    monkeypatch.chdir(firm_ops_ws)
    out = runner.invoke(app, ["viz", "--out", "graph.html", "--open"])
    assert out.exit_code == 0
    assert (firm_ops_ws / "graph.html").exists()
    assert "Wrote graph.html" in out.output
    # the stubbed browser-open, never launched — and a file:// URI, not a bare path
    assert opened and opened[0].startswith("file://") and opened[0].endswith("graph.html")


@pytest.mark.integration
def test_viz_type_filter(firm_ops_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-PRJ-003-03 (AC-003): --type project renders only the project and its incident edges."""
    seed(firm_ops_ws, "identity/team/other.md", type="person", name="Other", role="consultant")
    monkeypatch.chdir(firm_ops_ws)
    out = runner.invoke(app, ["viz", "--type", "project"])
    assert out.exit_code == 0
    html = (firm_ops_ws / "viz.html").read_text()
    assert '"id": "project/initech-pov"' in html
    assert '"id": "meeting/kickoff"' not in html  # inbound-only, excluded
    assert '"id": "person/other"' not in html  # unrelated, excluded


@pytest.mark.integration
def test_viz_empty_graph(fresh_ws: Path, monkeypatch) -> None:
    """TS-PRJ-003-04 (AC-004): an empty graph writes a valid empty canvas and reports zero counts."""
    monkeypatch.chdir(fresh_ws)
    out = runner.invoke(app, ["viz"])
    assert out.exit_code == 0
    assert "Wrote viz.html (0 nodes, 0 edges)" in out.output
    html = (fresh_ws / "viz.html").read_text()
    _no_external_assets(html)
    assert "var elements = [];" in html


# ============================================================================
# STORY-PRJ-002 — Backfill Frontmatter and Dates
# ============================================================================


@pytest.fixture
def dated_git_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A git history: `undated` committed then edited (distinct dates); `dated` hand-kept + comment."""
    seed(fresh_ws, "fragments/undated.md", type="fragment", stage="raw")  # no created/updated
    (fresh_ws / "fragments" / "dated.md").write_text(
        "---\ntype: fragment\nstage: raw\ncreated: 2025-01-01  # hand-kept, do not touch\n---\nbody\n"
    )
    _git_init(fresh_ws)
    _commit(fresh_ws, "seed", when="2026-01-01T12:00:00")  # first commit
    seed(fresh_ws, "fragments/undated.md", type="fragment", stage="mature")  # edit undated
    _commit(fresh_ws, "edit undated", when="2026-03-01T12:00:00")  # last commit (distinct)
    return fresh_ws


@pytest.fixture
def scaffold_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A complete opportunity (via create) beside an incomplete legacy one (written directly)."""
    entity.create(fresh_ws, "person", {"name": "Noor", "role": "partner"}, id_="noor")
    entity.create(fresh_ws, "client", {"name": "Initech"}, id_="initech")
    entity.create(
        fresh_ws, "opportunity", {"stage": "prospect", "client": "initech", "owner": "noor"},
        id_="initech-deal",
    )
    seed(fresh_ws, "opportunities/legacy-deal/_index.md", type="opportunity", client="initech")
    return fresh_ws


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_missing_field_detector(dated_git_ws: Path) -> None:
    """TS-PRJ-002-U01 (REQ-PRJ002-01): an entity missing created/updated is flagged for both."""
    report = backfill(dated_git_ws, dry_run=True)
    fields = {(c.slug, c.field) for c in report.changes}
    assert ("undated", "created") in fields and ("undated", "updated") in fields


@pytest.mark.unit
def test_preserve_existing_guard(dated_git_ws: Path) -> None:
    """TS-PRJ-002-U02 (REQ-PRJ002-02, PRJ-003): an authored created is never overwritten."""
    backfill(dated_git_ws)
    meta = frontmatter.load(str(dated_git_ws / "fragments" / "dated.md")).metadata
    assert str(meta["created"]) == "2025-01-01"  # hand-kept value survived


@pytest.mark.unit
def test_git_date_mapper_first_and_last(dated_git_ws: Path) -> None:
    """TS-PRJ-002-U03 (REQ-PRJ002-01, PRJ-004): created←first commit, updated←last, and they differ."""
    backfill(dated_git_ws)
    meta = frontmatter.load(str(dated_git_ws / "fragments" / "undated.md")).metadata
    assert str(meta["created"]) == "2026-01-01"  # first commit
    assert str(meta["updated"]) == "2026-03-01"  # last commit
    assert meta["created"] != meta["updated"]  # a genuine first-commit read, not a last alias


@pytest.mark.unit
def test_round_trip_preserves_order_and_comment(dated_git_ws: Path) -> None:
    """TS-PRJ-002-U04 (REQ-PRJ002-02, PRJ-SHARED-002): key order and inline comments survive."""
    backfill(dated_git_ws)
    text = (dated_git_ws / "fragments" / "dated.md").read_text()
    assert "created: 2025-01-01  # hand-kept, do not touch" in text  # comment + value intact
    assert text.index("type:") < text.index("stage:") < text.index("created:")  # order kept
    assert "updated:" in text  # the one missing field was still added


@pytest.mark.unit
def test_no_git_fallback_skips_dates(fresh_ws: Path, seed: Seed) -> None:
    """TS-PRJ-002-U05 (REQ-PRJ002-04): a workspace with no git history writes no dates."""
    seed(fresh_ws, "fragments/undated.md", type="fragment", stage="raw")
    report = backfill(fresh_ws)
    assert not report.git_available
    assert not any(c.source == "git log" for c in report.changes)
    meta = frontmatter.load(str(fresh_ws / "fragments" / "undated.md")).metadata
    assert "created" not in meta and "updated" not in meta


@pytest.mark.unit
def test_dry_run_collector_writes_nothing(dated_git_ws: Path) -> None:
    """TS-PRJ-002-U06 (REQ-PRJ002-03): dry-run lists the changes and touches no file."""
    before = {p: p.read_bytes() for p in (dated_git_ws / "fragments").glob("*.md")}
    report = backfill(dated_git_ws, dry_run=True)
    assert report.dry_run and report.changes
    for p, data in before.items():
        assert p.read_bytes() == data  # every file byte-unchanged


# --- integration -------------------------------------------------------------


@pytest.mark.integration
def test_cli_backfill_dates_from_git(dated_git_ws: Path, monkeypatch) -> None:
    """TS-PRJ-002-01 (AC-001): backfill writes git dates, preserves authored data, reports a count."""
    monkeypatch.chdir(dated_git_ws)
    out = runner.invoke(app, ["backfill"])
    assert out.exit_code == 0
    assert "Backfilled dates on 2 entities" in out.output  # undated (created+updated) + dated (updated)
    undated = frontmatter.load(str(dated_git_ws / "fragments" / "undated.md")).metadata
    assert str(undated["created"]) == "2026-01-01" and str(undated["updated"]) == "2026-03-01"
    dated = (dated_git_ws / "fragments" / "dated.md").read_text()
    assert "created: 2025-01-01  # hand-kept, do not touch" in dated  # untouched value + comment


@pytest.mark.integration
def test_cli_backfill_type_scaffolding(scaffold_ws: Path, monkeypatch) -> None:
    """TS-PRJ-002-02 (AC-002): --type scaffolds the incomplete entity, leaves the valid one untouched."""
    valid = scaffold_ws / "opportunities" / "initech-deal" / "_index.md"
    before = valid.read_bytes()
    monkeypatch.chdir(scaffold_ws)
    out = runner.invoke(app, ["backfill", "--type", "opportunity"])
    assert out.exit_code == 0
    # the scaffold writes are surfaced even with no git (not hidden behind "not backfilled")
    assert "Scaffolded frontmatter on 1 entities" in out.output
    legacy = frontmatter.load(
        str(scaffold_ws / "opportunities" / "legacy-deal" / "_index.md")
    ).metadata
    for key in ("stage", "owner", "created", "updated"):
        assert key in legacy  # the missing required scaffolding was added
    assert valid.read_bytes() == before  # an already-valid entity is left byte-unchanged
    # null placeholders keep the tree validate-clean (check still flags the gaps)
    assert validate(scaffold_ws).ok


@pytest.mark.integration
def test_cli_backfill_dry_run_lists_no_write(dated_git_ws: Path, monkeypatch) -> None:
    """TS-PRJ-002-03 (AC-003): --dry-run lists undated's created/updated and writes nothing."""
    before = {p: p.read_bytes() for p in (dated_git_ws / "fragments").glob("*.md")}
    monkeypatch.chdir(dated_git_ws)
    out = runner.invoke(app, ["backfill", "--dry-run"])
    assert out.exit_code == 0
    assert "fragment/undated: created, updated" in out.output
    for p, data in before.items():
        assert p.read_bytes() == data  # nothing written


@pytest.mark.integration
def test_cli_backfill_no_git(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-PRJ-002-04 (AC-004, REQ-PRJ002-04): a non-git workspace skips date backfill with a notice."""
    seed(fresh_ws, "fragments/undated.md", type="fragment", stage="raw")
    monkeypatch.chdir(fresh_ws)
    out = runner.invoke(app, ["backfill"])
    assert out.exit_code == 0
    assert "No git history; dates not backfilled" in out.output
    meta = frontmatter.load(str(fresh_ws / "fragments" / "undated.md")).metadata
    assert "created" not in meta and "updated" not in meta


# ============================================================================
# Review regressions (WPK-005 code-review findings)
# ============================================================================


@pytest.mark.unit
def test_reindex_escapes_markdown_title(fresh_ws: Path, seed: Seed) -> None:
    """Finding #2: a title with `](url)` is escaped, not emitted as a live link target."""
    seed(fresh_ws, "fragments/evil.md", type="fragment", stage="raw",
         title="Deal](http://evil.example)")
    content, _ = build_index_doc(fresh_ws)
    assert "Deal\\](http://evil.example)" in content  # the ] is backslash-escaped
    assert "[Deal](http://evil.example)" not in content  # never a link to the injected target


@pytest.mark.integration
def test_reindex_dry_run_up_to_date(firm_ops_ws: Path, monkeypatch) -> None:
    """Finding #7: an up-to-date --dry-run reports it rather than printing nothing."""
    monkeypatch.chdir(firm_ops_ws)
    runner.invoke(app, ["reindex"])  # write the current index
    out = runner.invoke(app, ["reindex", "--dry-run"])
    assert out.exit_code == 0 and "up to date" in out.output


@pytest.mark.unit
def test_viz_out_creates_subdir(firm_ops_ws: Path) -> None:
    """Finding #4: --out under a non-existent directory creates the directory, no crash."""
    result = viz(firm_ops_ws, out="sub/deep/graph.html")
    assert result.path == firm_ops_ws / "sub" / "deep" / "graph.html" and result.path.exists()


@pytest.mark.integration
def test_cli_backfill_unknown_type(scaffold_ws: Path, monkeypatch) -> None:
    """Finding #5: a misspelled --type fails loudly instead of a silent no-op success."""
    monkeypatch.chdir(scaffold_ws)
    out = runner.invoke(app, ["backfill", "--type", "opportunty"])  # typo
    assert out.exit_code == 1 and "opportunty" in out.output


@pytest.mark.integration
def test_cli_viz_unknown_type(firm_ops_ws: Path, monkeypatch) -> None:
    """Finding #5: viz --type with an unknown type fails loudly, not an empty 'success'."""
    monkeypatch.chdir(firm_ops_ws)
    out = runner.invoke(app, ["viz", "--type", "bogus"])
    assert out.exit_code == 1 and "bogus" in out.output
