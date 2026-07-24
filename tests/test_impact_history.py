"""TS-QRY-003 / TS-QRY-004 — Blast radius and supersession history (WPK-003-3).

`impact` walks the transitive `depends_on` closure (descendants, or `--reverse`
ancestors), cycle-safe and depth-marked, over a seeded firm-ops chain. `history`
follows a self-referential `supersedes` chain back in order with the derived
`superseded_by` direction — run against a generic fixture preset, since firm-ops
v1 declares neither the type nor the predicate.
"""

from __future__ import annotations

import json
from datetime import date
from pathlib import Path
from typing import Callable

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.errors import LocatedError
from khub.core.graph import history, impact
from khub.core.index import build_index
from khub.core.introspect import load_schema
from khub.core.workspace import init_workspace

runner = CliRunner()
Seed = Callable[..., None]

HISTORY_PRESET = """
version: "0.1.0"
entities:
  decision:
    layout: file
    path: decisions
    relations:
      supersedes: { to: decision, inverse: superseded_by }
"""


@pytest.fixture
def impact_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A `depends_on` chain, a leaf, and a cycle — all on the universal edge."""
    recent = {"created": date(2026, 6, 1), "updated": date(2026, 6, 1)}
    seed(fresh_ws, "clients/node-a.md", type="client", name="A", depends_on=["node-b"], **recent)
    seed(fresh_ws, "clients/node-b.md", type="client", name="B", depends_on=["node-c"], **recent)
    seed(fresh_ws, "clients/node-c.md", type="client", name="C", **recent)
    seed(fresh_ws, "clients/leaf-node.md", type="client", name="Leaf", **recent)
    seed(fresh_ws, "clients/node-x.md", type="client", name="X", depends_on=["node-y"], **recent)
    seed(fresh_ws, "clients/node-y.md", type="client", name="Y", depends_on=["node-z"], **recent)
    seed(fresh_ws, "clients/node-z.md", type="client", name="Z", depends_on=["node-x"], **recent)
    return fresh_ws


@pytest.fixture
def history_ws(tmp_path: Path, seed: Seed) -> Path:
    """A generic workspace with a four-link `supersedes` chain (derived inverse)."""
    src = tmp_path / "presets"
    src.mkdir()
    (src / "fixture").mkdir()
    (src / "fixture" / "schema.yaml").write_text(HISTORY_PRESET)
    ws = tmp_path / "ws"
    init_workspace("fixture", ws, preset_source=src)
    for a, b in [("decision-0012", "decision-0008"), ("decision-0008", "decision-0005"),
                 ("decision-0005", "decision-0001")]:
        seed(ws, f"decisions/{a}.md", type="decision", supersedes=b)
    seed(ws, "decisions/decision-0001.md", type="decision")
    return ws


def _index(ws: Path):
    return build_index(ws, load_schema(ws))


# --- impact unit -------------------------------------------------------------


@pytest.mark.unit
def test_impact_descendants_default_predicate(impact_ws: Path) -> None:
    """TS-QRY-003-U01/U04 (QRY-005): forward closure over the default `depends_on`."""
    res = impact(_index(impact_ws), "node-a")
    assert {n.slug for n in res if n.depth > 0} == {"node-b", "node-c"}
    assert any(n.slug == "node-a" and n.depth == 0 for n in res)  # source at depth 0


@pytest.mark.unit
def test_impact_ancestors_on_reverse(impact_ws: Path) -> None:
    """TS-QRY-003-U02 (REQ-QRY003-02): `--reverse` walks ancestors, not descendants."""
    res = impact(_index(impact_ws), "node-c", reverse=True)
    assert {n.slug for n in res if n.depth > 0} == {"node-b", "node-a"}


@pytest.mark.unit
def test_impact_cycle_safe(impact_ws: Path) -> None:
    """TS-QRY-003-U03 (QRY-006): a cycle terminates and each node appears once."""
    # Structural termination: the BFS visited-set bounds the walk, so this cannot loop.
    res = impact(_index(impact_ws), "node-x")
    slugs = [n.slug for n in res]
    assert sorted(slugs) == ["node-x", "node-y", "node-z"]
    assert len(slugs) == len(set(slugs))


@pytest.mark.unit
def test_impact_tree_depth(impact_ws: Path) -> None:
    """TS-QRY-003-U05 (REQ-QRY003-04): each node carries its depth from the source."""
    depth = {n.slug: n.depth for n in impact(_index(impact_ws), "node-a")}
    assert depth == {"node-a": 0, "node-b": 1, "node-c": 2}


@pytest.mark.unit
def test_impact_predicate_override(impact_ws: Path) -> None:
    """TS-QRY-003-U06 (REQ-QRY003-02): `--predicate` retargets the closure."""
    res = impact(_index(impact_ws), "node-a", predicate="related")
    assert {n.slug for n in res if n.depth > 0} == set()  # no `related` edges to walk


# --- impact integration / e2e ------------------------------------------------


@pytest.mark.e2e
def test_cli_impact_tree(impact_ws: Path, monkeypatch) -> None:
    """TS-QRY-003-01 (AC-001): `--format tree` renders the closure, depth-marked."""
    monkeypatch.chdir(impact_ws)
    out = runner.invoke(app, ["impact", "node-a", "--format", "tree"])
    assert out.exit_code == 0, out.output
    lines = out.output.splitlines()
    assert any(line == "client/node-a" for line in lines)  # source, depth 0
    assert any(line == "  client/node-b" for line in lines)  # depth 1
    assert any(line == "    client/node-c" for line in lines)  # depth 2
    closure = json.loads(runner.invoke(app, ["impact", "node-a", "--format", "json"]).output)
    assert {d["slug"] for d in closure if d["depth"] > 0} == {"node-b", "node-c"}


@pytest.mark.integration
def test_cli_impact_reverse(impact_ws: Path, monkeypatch) -> None:
    """TS-QRY-003-02 (AC-002): `--reverse` returns ancestors; `--predicate` retargets."""
    monkeypatch.chdir(impact_ws)
    rev = json.loads(runner.invoke(app, ["impact", "node-c", "--reverse", "--format", "json"]).output)
    assert {d["slug"] for d in rev if d["depth"] > 0} == {"node-b", "node-a"}
    other = json.loads(runner.invoke(app, ["impact", "node-a", "--predicate", "related", "--format", "json"]).output)
    assert {d["slug"] for d in other if d["depth"] > 0} == set()


@pytest.mark.integration
def test_cli_impact_no_downstream(impact_ws: Path, monkeypatch) -> None:
    """TS-QRY-003-03 (AC-003): a leaf returns only the source, `No downstream impact`."""
    monkeypatch.chdir(impact_ws)
    tree = runner.invoke(app, ["impact", "leaf-node", "--format", "tree"])
    assert tree.exit_code == 0 and "No downstream impact" in tree.output
    js = json.loads(runner.invoke(app, ["impact", "leaf-node", "--format", "json"]).output)
    assert {d["slug"] for d in js} == {"leaf-node"}


@pytest.mark.integration
def test_cli_impact_pipe_emits_json(impact_ws: Path, monkeypatch) -> None:
    """impact honors the read-command contract: JSON on a pipe even without --format."""
    monkeypatch.chdir(impact_ws)
    out = runner.invoke(app, ["impact", "node-a"])  # non-TTY pipe, default format
    data = json.loads(out.output)  # parses cleanly → JSON, not the text tree
    assert {d["slug"] for d in data if d["depth"] > 0} == {"node-b", "node-c"}


@pytest.mark.integration
def test_cli_impact_cycle_terminates(impact_ws: Path, monkeypatch) -> None:
    """TS-QRY-003-04 (AC-004): the cyclic graph terminates, each node once."""
    monkeypatch.chdir(impact_ws)
    data = json.loads(runner.invoke(app, ["impact", "node-x", "--format", "json"]).output)
    slugs = [d["slug"] for d in data]
    assert sorted(slugs) == ["node-x", "node-y", "node-z"] and len(slugs) == len(set(slugs))


# --- history unit ------------------------------------------------------------


@pytest.mark.unit
def test_history_chain_order(history_ws: Path) -> None:
    """TS-QRY-004-U01/U04 (QRY-007): follow the default `supersedes` chain in order."""
    res = history(_index(history_ws), "decision-0012")
    assert [link.slug for link in res] == [
        "decision-0012", "decision-0008", "decision-0005", "decision-0001"
    ]


@pytest.mark.unit
def test_history_limit(history_ws: Path) -> None:
    """TS-QRY-004-U02 (REQ-QRY004-02): `--limit` caps to the N most recent links."""
    res = history(_index(history_ws), "decision-0012", limit=2)
    assert [link.slug for link in res] == ["decision-0012", "decision-0008"]


@pytest.mark.unit
def test_history_derived_inverse_not_stored(history_ws: Path) -> None:
    """TS-QRY-004-U03 (QRY-SHARED-002): `superseded_by` is derived, never persisted."""
    res = history(_index(history_ws), "decision-0012")
    by_slug = {link.slug: link.superseded_by for link in res}
    assert by_slug["decision-0008"] == "decision/decision-0012"
    assert by_slug["decision-0012"] is None  # the head supersedes-nothing-above it
    stored = frontmatter.load(str(history_ws / "decisions" / "decision-0008.md")).metadata
    assert "superseded_by" not in stored


@pytest.mark.unit
def test_history_reads_graph_not_git(history_ws: Path) -> None:
    """TS-QRY-004-U06 (QRY-008): `history` reads the graph chain, not git."""
    # The fixture workspace is not a git repo; history still returns the chain,
    # proving it reads the index — `log` (not implemented here) is the git surface.
    assert not (history_ws / ".git").exists()
    res = history(_index(history_ws), "decision-0012")
    assert [link.slug for link in res][:2] == ["decision-0012", "decision-0008"]


@pytest.mark.unit
def test_history_lookup_error(history_ws: Path) -> None:
    """TS-QRY-004-U05 (REQ-QRY004-03): an unresolvable id raises a lookup error."""
    with pytest.raises(LocatedError) as err:
        history(_index(history_ws), "ghost")
    assert err.value.code == "lookup_error"


# --- history integration / e2e -----------------------------------------------


@pytest.mark.e2e
def test_cli_history_chain(history_ws: Path, monkeypatch) -> None:
    """TS-QRY-004-01 (AC-001): the chain in order, with the derived `superseded_by`."""
    monkeypatch.chdir(history_ws)
    data = json.loads(runner.invoke(app, ["history", "decision-0012", "--format", "json"]).output)
    assert [d["slug"] for d in data] == [
        "decision-0012", "decision-0008", "decision-0005", "decision-0001"
    ]
    by_slug = {d["slug"]: d["superseded_by"] for d in data}
    assert by_slug["decision-0008"] == "decision/decision-0012"
    stored = frontmatter.load(str(history_ws / "decisions" / "decision-0008.md")).metadata
    assert "superseded_by" not in stored


@pytest.mark.integration
def test_cli_history_limit(history_ws: Path, monkeypatch) -> None:
    """TS-QRY-004-02 (AC-002): `--limit 3` returns the three most recent links."""
    monkeypatch.chdir(history_ws)
    data = json.loads(runner.invoke(app, ["history", "decision-0012", "--limit", "3", "--format", "json"]).output)
    assert [d["slug"] for d in data] == ["decision-0012", "decision-0008", "decision-0005"]


@pytest.mark.integration
def test_cli_history_no_history(history_ws: Path, monkeypatch) -> None:
    """TS-QRY-004-03 (AC-003): a chain root returns only itself, `No supersession history`."""
    monkeypatch.chdir(history_ws)
    tty = runner.invoke(app, ["history", "decision-0001"], env={"FORCE_COLOR": "1"})
    assert tty.exit_code == 0 and "No supersession history" in tty.output
    js = json.loads(runner.invoke(app, ["history", "decision-0001", "--format", "json"]).output)
    assert [d["slug"] for d in js] == ["decision-0001"]


@pytest.mark.integration
def test_cli_history_unknown_id(history_ws: Path, monkeypatch) -> None:
    """TS-QRY-004-04 (AC-004): an unresolvable id reports the located lookup error."""
    monkeypatch.chdir(history_ws)
    out = runner.invoke(app, ["history", "ghost"])
    assert out.exit_code == 1 and "No entity 'ghost' found" in out.output
