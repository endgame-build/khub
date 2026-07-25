"""TS-ENT-005 — Remove an Entity (WPK-002-3).

Covers inbound-edge detection, the removal guard (refuse unless --force), folder
vs flat deletion, and the force path that leaves dangling edges behind.

The force-path assertion checks the dangling edges remain *on disk* rather than
running `khub check` — `check` (FS-004) is not built yet (see the ponytail marker
in core.entity.delete). Wire it to a real check run when FS-004 lands.
"""

from __future__ import annotations

from collections.abc import Callable
from pathlib import Path

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.entity import _inbound_edges, delete
from khub.core.errors import LocatedError
from khub.core.index import build_index
from khub.core.introspect import load_schema

runner = CliRunner()

Seed = Callable[..., None]


def _referenced_client(ws: Path, seed: Seed) -> None:
    """A client/initech with three inbound `client` edges resolving to it."""
    seed(ws, "clients/initech.md", type="client", name="Initech")
    seed(ws, "opportunities/o1/_index.md", type="opportunity", stage="prospect", client="initech")
    seed(ws, "opportunities/o2/_index.md", type="opportunity", stage="won", client="initech")
    seed(ws, "projects/p1/_index.md", type="project", client="initech")


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_inbound_edge_detector(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-005-U01: every edge resolving to the target is found."""
    _referenced_client(fresh_ws, seed)
    resolved = load_schema(fresh_ws)
    index = build_index(fresh_ws, resolved)
    inbound = _inbound_edges(index, resolved, ("client", "initech"))
    assert len(inbound) == 3
    assert all(e.predicate == "client" for e in inbound)


@pytest.mark.unit
def test_removal_guard(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-005-U02: refused while inbound edges resolve, unless --force."""
    _referenced_client(fresh_ws, seed)
    refused = delete(fresh_ws, "initech")
    assert refused.removed is False and len(refused.inbound) == 3
    assert (fresh_ws / "clients" / "initech.md").exists()
    forced = delete(fresh_ws, "initech", force=True)
    assert forced.removed is True


@pytest.mark.unit
def test_folder_and_flat_deletion(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-005-U03: a folder-layout entity deletes its folder; a flat one its file."""
    seed(fresh_ws, "projects/lonely/_index.md", type="project", client="x", owner="y")
    seed(fresh_ws, "fragments/note.md", type="fragment", stage="raw", owner="y")
    delete(fresh_ws, "lonely")
    delete(fresh_ws, "note")
    assert not (fresh_ws / "projects" / "lonely").exists()
    assert not (fresh_ws / "fragments" / "note.md").exists()


@pytest.mark.unit
def test_force_leaves_dangling_edges(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-005-U04: --force deletes and leaves the inbound edges dangling on disk."""
    _referenced_client(fresh_ws, seed)
    result = delete(fresh_ws, "initech", force=True)
    assert result.removed is True and len(result.inbound) == 3
    assert not (fresh_ws / "clients" / "initech.md").exists()
    # The dangling references survive untouched — check (FS-004) will surface them.
    assert frontmatter.load(str(fresh_ws / "opportunities" / "o1" / "_index.md")).metadata["client"] == "initech"


@pytest.mark.unit
def test_lookup_error_for_missing(fresh_ws: Path) -> None:
    """TS-ENT-005-U05: an unresolvable id raises a lookup error."""
    with pytest.raises(LocatedError) as err:
        delete(fresh_ws, "ghost")
    assert err.value.code == "lookup_error"


# --- integration / e2e -------------------------------------------------------


@pytest.mark.e2e
def test_cli_remove_unreferenced(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-005-01: an unreferenced entity is deleted with a confirmation."""
    seed(fresh_ws, "fragments/old-fragment.md", type="fragment", stage="raw", owner="noor")
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["remove", "old-fragment"])
    assert result.exit_code == 0
    assert "Removed fragment 'old-fragment'" in result.output
    assert not (fresh_ws / "fragments" / "old-fragment.md").exists()


@pytest.mark.integration
def test_cli_remove_refused_lists_inbound(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-005-02: a referenced entity is refused with its inbound edges listed."""
    _referenced_client(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["remove", "initech"])
    assert result.exit_code == 1
    assert "Refusing to remove client 'initech': 3 inbound edges resolve to it. Pass --force to override" in result.output
    assert (fresh_ws / "clients" / "initech.md").exists()


@pytest.mark.integration
def test_cli_remove_force_leaves_dangling(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-005-03 (stubbed): --force deletes; dangling edges remain on disk for check."""
    _referenced_client(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["remove", "initech", "--force"])
    assert result.exit_code == 0
    assert "Removed client 'initech'" in result.output
    assert not (fresh_ws / "clients" / "initech.md").exists()
    # ponytail: assert on-disk dangling edges; real `khub check` lands in FS-004.
    danglers = [
        frontmatter.load(str(fresh_ws / p)).metadata["client"]
        for p in ("opportunities/o1/_index.md", "opportunities/o2/_index.md", "projects/p1/_index.md")
    ]
    assert danglers == ["initech", "initech", "initech"]


@pytest.mark.integration
def test_cli_remove_unknown_id(fresh_ws: Path, monkeypatch) -> None:
    """TS-ENT-005-04: an unresolvable id reports a lookup error."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["remove", "ghost"])
    assert result.exit_code == 1
    assert "No entity 'ghost' found" in result.output
