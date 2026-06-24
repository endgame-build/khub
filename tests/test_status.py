"""STORY-WS-003 — `khub status`: report workspace status.

Covers TS-WS-003-01..04 and the unit rows TS-WS-003-U05/U06 (workspace
resolution error, JSON output). The seeded tree mirrors test_project.py.
"""

from __future__ import annotations

from datetime import date
from pathlib import Path
from typing import Callable

import json

import pytest
from typer.testing import CliRunner

from khub.cli.main import app

runner = CliRunner()


@pytest.fixture
def seeded(fresh_ws: Path, seed: Callable[..., None]) -> Path:
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme",
         created=date(2025, 1, 1), updated=date(2000, 1, 1))
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor",
         role="manager", created=date(2026, 6, 1))
    seed(fresh_ws, "identity/team/nobody.md", type="person", name="Nobody",
         role="consultant", draft=True, created=date(2026, 6, 1))
    seed(fresh_ws, "opportunities/acme-pov/_index.md", type="opportunity",
         stage="prospect", created=date(2026, 6, 20), updated=date(2026, 6, 20),
         client="acme", owner="noor")
    return fresh_ws


@pytest.mark.integration
def test_status_json(seeded: Path, monkeypatch) -> None:
    """TS-WS-003-04 (AC-004): per-type counts, draft/active, orphan/stale, OKF flag as JSON."""
    monkeypatch.chdir(seeded)
    result = runner.invoke(app, ["status", "--format", "json"])
    assert result.exit_code == 0, result.output
    data = json.loads(result.output)
    assert data["counts"]["person"] == 2
    assert data["draft"] == 1 and data["active"] == 3
    assert data["orphan"] == 1
    assert "stale" in data and "okf_conformant" in data


@pytest.mark.integration
def test_status_table_on_tty(seeded: Path, monkeypatch) -> None:
    """TS-WS-003-01 (AC-001): on a TTY the operator gets a Rich table, not JSON."""
    monkeypatch.chdir(seeded)
    result = runner.invoke(app, ["status"], env={"FORCE_COLOR": "1"})
    assert result.exit_code == 0, result.output
    assert "opportunity" in result.output
    with pytest.raises(json.JSONDecodeError):
        json.loads(result.output)


@pytest.mark.integration
def test_status_default_on_pipe_is_json(seeded: Path, monkeypatch) -> None:
    """The agent contract: `khub status` over a pipe (no --format) emits JSON, not a table."""
    monkeypatch.chdir(seeded)
    result = runner.invoke(app, ["status"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output)["total"] == 4  # parses cleanly


@pytest.mark.e2e
def test_status_empty_workspace(fresh_ws: Path, monkeypatch) -> None:
    """TS-WS-003-02 (AC-002): an initialized-but-empty workspace reports the empty line."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["status"], env={"FORCE_COLOR": "1"})
    assert result.exit_code == 0, result.output
    assert "Workspace initialized; no entities yet" in result.output


@pytest.mark.integration
def test_status_tolerates_null_and_string_config(seeded: Path, monkeypatch) -> None:
    """A hand-edited config with `defaults:` null or a quoted stale_days does not crash."""
    monkeypatch.chdir(seeded)
    (seeded / ".khub" / "config.yaml").write_text("name: hq\npreset: firm-ops\ndefaults:\n")
    assert runner.invoke(app, ["status", "--format", "json"]).exit_code == 0
    (seeded / ".khub" / "config.yaml").write_text(
        "name: hq\npreset: firm-ops\ndefaults:\n  stale_days: \"120\"\n"
    )
    assert runner.invoke(app, ["status", "--format", "json"]).exit_code == 0


@pytest.mark.integration
def test_status_no_workspace(tmp_path: Path, monkeypatch) -> None:
    """TS-WS-003-03 (AC-003, REQ-WS003-03): no .khub above cwd → resolution error."""
    monkeypatch.chdir(tmp_path)
    result = runner.invoke(app, ["status"])
    assert result.exit_code == 1
    assert "No .khub workspace found. Run khub init <preset>" in result.output
