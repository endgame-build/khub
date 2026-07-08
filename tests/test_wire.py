"""Tests for ``khub wire`` — the managed CLAUDE.md block + schema import."""

from __future__ import annotations

from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.wire import BEGIN, END, wire

runner = CliRunner()


@pytest.mark.integration
def test_wire_creates_claude_md(fresh_ws: Path) -> None:
    result = wire(fresh_ws)
    claude = fresh_ws / "CLAUDE.md"
    assert claude.exists()
    text = claude.read_text(encoding="utf-8")
    assert BEGIN in text and END in text
    assert "@.khub/schema.yaml" in text  # the schema import — reason without the CLI
    assert "firm-ops" in text  # the active preset
    assert "`client`" in text  # a declared type, read from the live schema
    assert [o.action for o in result.outcomes] == ["created"]


@pytest.mark.integration
def test_wire_is_idempotent(fresh_ws: Path) -> None:
    wire(fresh_ws)
    before = (fresh_ws / "CLAUDE.md").read_text(encoding="utf-8")
    result = wire(fresh_ws)
    after = (fresh_ws / "CLAUDE.md").read_text(encoding="utf-8")
    assert after == before
    assert [o.action for o in result.outcomes] == ["unchanged"]


@pytest.mark.integration
def test_wire_replaces_block_preserving_surroundings(fresh_ws: Path) -> None:
    claude = fresh_ws / "CLAUDE.md"
    claude.write_text(
        f"# Project\n\nHouse rules.\n\n{BEGIN}\nstale khub block\n{END}\n\nMore rules.\n",
        encoding="utf-8",
    )
    result = wire(fresh_ws)
    text = claude.read_text(encoding="utf-8")
    assert "# Project" in text and "House rules." in text and "More rules." in text
    assert "stale khub block" not in text
    assert "@.khub/schema.yaml" in text
    assert text.count(BEGIN) == 1 and text.count(END) == 1
    assert [o.action for o in result.outcomes] == ["updated"]


@pytest.mark.integration
def test_wire_dry_run_writes_nothing(fresh_ws: Path) -> None:
    result = wire(fresh_ws, dry_run=True)
    assert not (fresh_ws / "CLAUDE.md").exists()
    assert BEGIN in result.block and "@.khub/schema.yaml" in result.block


@pytest.mark.integration
def test_wire_agents_flag_mirrors_the_block(fresh_ws: Path) -> None:
    result = wire(fresh_ws, agents=True)
    agents = fresh_ws / "AGENTS.md"
    assert (fresh_ws / "CLAUDE.md").exists()
    assert agents.exists()
    assert "@.khub/schema.yaml" in agents.read_text(encoding="utf-8")
    assert {o.path.name for o in result.outcomes} == {"CLAUDE.md", "AGENTS.md"}


@pytest.mark.integration
def test_wire_cli_no_workspace_errors(tmp_path: Path) -> None:
    result = runner.invoke(app, ["-C", str(tmp_path), "wire"])
    assert result.exit_code == 1
    assert "khub init" in result.output


@pytest.mark.integration
def test_wire_cli_dry_run_prints_block(fresh_ws: Path) -> None:
    result = runner.invoke(app, ["-C", str(fresh_ws), "wire", "--dry-run"])
    assert result.exit_code == 0
    assert "@.khub/schema.yaml" in result.output
    assert not (fresh_ws / "CLAUDE.md").exists()
