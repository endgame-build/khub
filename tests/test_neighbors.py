"""TS-QRY-002 — Walk One-Hop Neighbors (WPK-003-2).

Covers outbound/inbound adjacency with derived inverses (computed not stored),
direction and predicate filtering, bounded `--depth` vs the unbounded `impact`
closure, the isolated-entity empty set, and the located lookup error. firm-ops
`project/initech-pov` is the walk source; a meeting's `engagement` provides the
inbound derived inverse.
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
from khub.core.graph import impact, neighbors
from khub.core.index import build_index
from khub.core.introspect import load_schema

runner = CliRunner()
Seed = Callable[..., None]


@pytest.fixture
def nws(fresh_ws: Path, seed: Seed) -> Path:
    """A firm-ops workspace: initech with two inbound `client` edges and a meeting."""
    recent = {"created": date(2026, 6, 1), "updated": date(2026, 6, 1)}
    seed(fresh_ws, "clients/initech.md", type="client", name="Initech", **recent)
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor", role="partner",
         created=date(2026, 6, 1))
    seed(fresh_ws, "projects/initech-pov/_index.md", type="project", client="initech",
         owner="noor", **recent)
    seed(fresh_ws, "opportunities/initech-deal/_index.md", type="opportunity", stage="prospect",
         client="initech", owner="noor", **recent)
    seed(fresh_ws, "meetings/kickoff.md", type="meeting", engagement="initech-pov",
         date="2026-06-20T10:00:00", call_type="client", source="recording")
    seed(fresh_ws, "clients/lonely-client.md", type="client", name="Lonely", **recent)
    return fresh_ws


@pytest.fixture
def depends_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A four-deep `depends_on` chain for the bounded-depth vs closure distinction."""
    recent = {"created": date(2026, 6, 1), "updated": date(2026, 6, 1)}
    for a, b in [("dep-a", "dep-b"), ("dep-b", "dep-c"), ("dep-c", "dep-d")]:
        seed(fresh_ws, f"clients/{a}.md", type="client", name=a, depends_on=[b], **recent)
    seed(fresh_ws, "clients/dep-d.md", type="client", name="dep-d", **recent)
    return fresh_ws


@pytest.fixture
def parallel_ws(fresh_ws: Path, seed: Seed) -> Path:
    """Two clients joined by two predicates, plus a mutual inbound edge."""
    recent = {"created": date(2026, 6, 1), "updated": date(2026, 6, 1)}
    seed(fresh_ws, "clients/pa.md", type="client", name="pa",
         related=["pb"], depends_on=["pb"], **recent)
    seed(fresh_ws, "clients/pb.md", type="client", name="pb", related=["pa"], **recent)
    return fresh_ws


def _index(ws: Path):
    return build_index(ws, load_schema(ws))


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_direction_filter(nws: Path) -> None:
    """TS-QRY-002-U01 (QRY-004): default is both; `--in`/`--out` narrow direction."""
    index = _index(nws)
    both = neighbors(index, "initech-pov")
    assert any(n.direction == "out" for n in both) and any(n.direction == "in" for n in both)
    assert all(n.direction == "out" for n in neighbors(index, "initech-pov", direction="out"))
    in_only = neighbors(index, "initech-pov", direction="in")
    assert in_only and all(n.direction == "in" for n in in_only)


@pytest.mark.unit
def test_predicate_filter(nws: Path) -> None:
    """TS-QRY-002-U02 (REQ-QRY002-02): `--predicate` restricts adjacency to one relation."""
    res = neighbors(_index(nws), "initech-pov", predicate="owner")
    assert {(n.slug, n.predicate) for n in res} == {("noor", "owner")}


@pytest.mark.unit
def test_inbound_includes_derived_inverse(nws: Path) -> None:
    """TS-QRY-002-U03 (QRY-003/SHARED-002): inbound is a derived inverse, never stored."""
    res = neighbors(_index(nws), "initech-pov", direction="in")
    assert any(n.predicate == "engagement" and n.derived and n.slug == "kickoff" for n in res)
    stored = frontmatter.load(str(nws / "projects" / "initech-pov" / "_index.md")).metadata
    assert "engagement" not in stored  # the inverse is computed, not persisted


@pytest.mark.unit
def test_depth_bounded_vs_impact_closure(depends_ws: Path) -> None:
    """TS-QRY-002-U04 (QRY-010): `--depth` is bounded; `impact` is the unbounded closure."""
    index = _index(depends_ws)
    near = {n.slug for n in neighbors(index, "dep-a", direction="out", predicate="depends_on", depth=2)}
    assert near == {"dep-b", "dep-c"}  # stops at two hops
    full = {n.slug for n in impact(index, "dep-a") if n.depth > 0}
    assert full == {"dep-b", "dep-c", "dep-d"}  # exhausts the predicate
    assert "dep-d" in full and "dep-d" not in near  # the two surfaces stay distinct


@pytest.mark.unit
def test_neighbors_parallel_edges(parallel_ws: Path) -> None:
    """QRY-003: two predicates joining one pair are two adjacencies, neither collapsed."""
    index = _index(parallel_ws)
    out = neighbors(index, "pa", direction="out")
    assert {(n.slug, n.predicate) for n in out} == {("pb", "related"), ("pb", "depends_on")}
    triples = {(n.slug, n.predicate, n.direction) for n in neighbors(index, "pa")}
    assert ("pb", "related", "in") in triples  # the mutual inbound edge survives too


@pytest.mark.unit
def test_neighbors_lookup_error(nws: Path) -> None:
    """TS-QRY-002-U05 (REQ-QRY002-03): an unresolvable id raises a lookup error."""
    with pytest.raises(LocatedError) as err:
        neighbors(_index(nws), "ghost")
    assert err.value.code == "lookup_error"


@pytest.mark.unit
def test_adjacency_labeled(nws: Path) -> None:
    """TS-QRY-002-U06 (REQ-QRY002-01): every neighbor carries a predicate and direction."""
    res = neighbors(_index(nws), "initech-pov")
    assert res and all(n.predicate and n.direction in ("in", "out") for n in res)


# --- integration / e2e -------------------------------------------------------


@pytest.mark.e2e
def test_cli_neighbors_both_directions(nws: Path, monkeypatch) -> None:
    """TS-QRY-002-01 (AC-001): outbound relations + the inbound derived inverse."""
    monkeypatch.chdir(nws)
    out = runner.invoke(app, ["neighbors", "initech-pov", "--format", "json"])
    assert out.exit_code == 0, out.output
    data = json.loads(out.output)
    labeled = {(d["predicate"], d["direction"], d["id"]) for d in data}
    assert ("client", "out", "client/initech") in labeled
    assert ("owner", "out", "person/noor") in labeled
    assert ("engagement", "in", "meeting/kickoff") in labeled
    assert any(d["derived"] and d["direction"] == "in" for d in data)


@pytest.mark.integration
def test_cli_neighbors_predicate_and_depth(nws: Path, monkeypatch) -> None:
    """TS-QRY-002-02 (AC-002): `--predicate client --in`, then `--depth 2` second hop."""
    monkeypatch.chdir(nws)
    inbound = runner.invoke(app, ["neighbors", "initech", "--predicate", "client", "--in", "--format", "json"])
    data = json.loads(inbound.output)
    assert {d["id"] for d in data} == {"project/initech-pov", "opportunity/initech-deal"}
    assert all(d["predicate"] == "client" and d["direction"] == "in" for d in data)
    deep = json.loads(runner.invoke(app, ["neighbors", "initech", "--depth", "2", "--format", "json"]).output)
    assert "person/noor" in {d["id"] for d in deep}  # reached only at the second hop


@pytest.mark.integration
def test_cli_neighbors_isolated_entity(nws: Path, monkeypatch) -> None:
    """TS-QRY-002-03 (AC-003): an edge-less entity returns an empty set, `No neighbors`."""
    monkeypatch.chdir(nws)
    tty = runner.invoke(app, ["neighbors", "lonely-client"], env={"FORCE_COLOR": "1"})
    assert tty.exit_code == 0 and "No neighbors" in tty.output
    js = runner.invoke(app, ["neighbors", "lonely-client", "--format", "json"])
    assert json.loads(js.output) == []


@pytest.mark.integration
def test_cli_neighbors_unknown_id(nws: Path, monkeypatch) -> None:
    """TS-QRY-002-04 (AC-004): an unresolvable id reports the located lookup error."""
    monkeypatch.chdir(nws)
    out = runner.invoke(app, ["neighbors", "ghost"])
    assert out.exit_code == 1 and "No entity 'ghost' found" in out.output
