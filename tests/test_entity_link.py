"""TS-ENT-004 — Link and Unlink Relations (WPK-002-2).

Covers predicate legality, target resolution, cardinality enforcement, single-
sided storage, and edge removal (with derived inverses left to recompute).
"""

from __future__ import annotations

from collections.abc import Callable
from pathlib import Path

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.entity import link, unlink
from khub.core.errors import LocatedError

runner = CliRunner()

Seed = Callable[..., None]


def _prereqs(ws: Path, seed: Seed) -> None:
    seed(ws, "clients/initech.md", type="client", name="Initech")
    seed(ws, "identity/team/noor.md", type="person", name="Noor", role="partner")
    seed(ws, "identity/team/dana.md", type="person", name="Dana", role="consultant")
    seed(ws, "partnerships/northwind/_index.md", type="partnership", partner="Northwind", owner="noor")


def _project(ws: Path, seed: Seed, **over: object) -> Path:
    meta: dict[str, object] = {
        "type": "project", "created": "2026-01-01", "updated": "2026-01-01", "draft": False,
        "client": "initech", "owner": "noor",
    }
    meta.update(over)
    seed(ws, "projects/initech-pov/_index.md", **meta)
    return ws / "projects" / "initech-pov" / "_index.md"


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_predicate_legality(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-004-U01: an undeclared predicate for the source type is rejected."""
    _prereqs(fresh_ws, seed)
    _project(fresh_ws, seed)
    with pytest.raises(LocatedError) as err:
        link(fresh_ws, "initech-pov", "engagement", "anything")
    assert err.value.code == "illegal_predicate"


@pytest.mark.unit
def test_cardinality_enforced(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-004-U02: a single-valued predicate refuses a second value."""
    _prereqs(fresh_ws, seed)
    _project(fresh_ws, seed)  # owner already noor
    with pytest.raises(LocatedError) as err:
        link(fresh_ws, "initech-pov", "owner", "dana")
    assert err.value.code == "cardinality_violation"


@pytest.mark.unit
def test_single_sided_storage(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-004-U03: the forward edge lands on the source only, never the target."""
    _prereqs(fresh_ws, seed)
    source = _project(fresh_ws, seed)
    target = fresh_ws / "partnerships" / "northwind" / "_index.md"
    before_target = target.read_bytes()
    link(fresh_ws, "initech-pov", "partner", "northwind")
    assert frontmatter.load(str(source)).metadata["partner"] == "northwind"
    assert target.read_bytes() == before_target


@pytest.mark.unit
def test_target_resolver(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-004-U04: an unresolvable target is rejected."""
    _prereqs(fresh_ws, seed)
    _project(fresh_ws, seed)
    with pytest.raises(LocatedError) as err:
        link(fresh_ws, "initech-pov", "partner", "ghost")
    assert err.value.code == "referential_integrity"


@pytest.mark.unit
def test_unlink_removes_forward_edge(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-004-U05: unlink drops the forward edge (derived inverses recompute)."""
    _prereqs(fresh_ws, seed)
    source = _project(fresh_ws, seed, partner="northwind")
    unlink(fresh_ws, "initech-pov", "partner", "northwind")
    assert "partner" not in frontmatter.load(str(source)).metadata


# --- integration / e2e -------------------------------------------------------


@pytest.mark.e2e
def test_cli_link(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-004-01: link checks the predicate, resolves the target, stores single-sided."""
    _prereqs(fresh_ws, seed)
    source = _project(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["link", "initech-pov", "partner", "northwind"])
    assert result.exit_code == 0
    assert "Linked initech-pov --partner--> northwind" in result.output
    assert frontmatter.load(str(source)).metadata["partner"] == "northwind"


@pytest.mark.integration
def test_cli_link_illegal_predicate(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-004-02: a predicate not legal for the source type is rejected."""
    _prereqs(fresh_ws, seed)
    _project(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["link", "initech-pov", "engagement", "some-meeting"])
    assert result.exit_code == 1
    assert "Predicate 'engagement' is not legal for type 'project'" in result.output


@pytest.mark.integration
def test_cli_link_unresolvable_target(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-004-03: an unresolvable target is rejected (referential integrity)."""
    _prereqs(fresh_ws, seed)
    _project(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["link", "initech-pov", "owner", "ghost"])
    assert result.exit_code == 1
    assert "No person 'ghost' to satisfy predicate 'owner'" in result.output


@pytest.mark.integration
def test_cli_unlink(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-004-04: unlink removes the edge and confirms."""
    _prereqs(fresh_ws, seed)
    source = _project(fresh_ws, seed, partner="northwind")
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["unlink", "initech-pov", "partner", "northwind"])
    assert result.exit_code == 0
    assert "Unlinked initech-pov --partner--> northwind" in result.output
    assert "partner" not in frontmatter.load(str(source)).metadata


@pytest.mark.integration
def test_cli_link_cardinality_violation(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-004-05: a second value on a single-valued predicate is refused."""
    _prereqs(fresh_ws, seed)
    _project(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["link", "initech-pov", "owner", "dana"])
    assert result.exit_code == 1
    assert "Predicate 'owner' is single-valued; use edit to replace" in result.output
