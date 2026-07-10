"""Tests for ``khub wire`` — the managed agent-file block(s), import vs pointer."""

from __future__ import annotations

from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.wire import BEGIN, END, wire

runner = CliRunner()


@pytest.mark.integration
def test_wire_creates_claude_md(fresh_ws: Path) -> None:
    result = wire(fresh_ws, claude=True)
    claude = fresh_ws / "CLAUDE.md"
    assert claude.exists()
    text = claude.read_text(encoding="utf-8")
    assert BEGIN in text and END in text
    assert "@.khub/schema.yaml" in text  # the Claude import — reason without the CLI
    assert "firm-ops" in text  # the active preset
    assert "`client`" in text  # a declared type, read from the live schema
    assert [o.action for o in result.outcomes] == ["created"]
    assert not (fresh_ws / "AGENTS.md").exists()  # claude=True targets CLAUDE.md only


@pytest.mark.integration
def test_wire_agents_pointer_not_import(fresh_ws: Path) -> None:
    result = wire(fresh_ws, agents=True)
    agents = fresh_ws / "AGENTS.md"
    assert agents.exists()
    text = agents.read_text(encoding="utf-8")
    assert BEGIN in text and END in text
    assert "@.khub/schema.yaml" not in text  # AGENTS.md has no import directive
    assert ".khub/schema.yaml" in text  # but it points at the schema file
    assert "`client`" in text  # types + command surface still present
    assert [o.action for o in result.outcomes] == ["created"]
    assert not (fresh_ws / "CLAUDE.md").exists()  # agents=True targets AGENTS.md only


@pytest.mark.integration
def test_wire_bare_updates_existing_only(fresh_ws: Path) -> None:
    wire(fresh_ws, claude=True)  # seed CLAUDE.md
    result = wire(fresh_ws)  # bare: update existing, create nothing
    assert {o.path.name for o in result.outcomes} == {"CLAUDE.md"}
    assert result.outcomes[0].action == "unchanged"
    assert not (fresh_ws / "AGENTS.md").exists()  # bare wire never creates AGENTS.md


@pytest.mark.integration
def test_wire_bare_updates_both_when_both_exist(fresh_ws: Path) -> None:
    wire(fresh_ws, claude=True, agents=True)  # seed both
    result = wire(fresh_ws)  # bare updates both
    assert {o.path.name for o in result.outcomes} == {"CLAUDE.md", "AGENTS.md"}


@pytest.mark.integration
def test_wire_bare_noop_when_none(fresh_ws: Path) -> None:
    result = wire(fresh_ws)  # nothing exists → nothing to do
    assert result.outcomes == []
    assert not (fresh_ws / "CLAUDE.md").exists()
    assert not (fresh_ws / "AGENTS.md").exists()


@pytest.mark.integration
def test_wire_is_idempotent(fresh_ws: Path) -> None:
    wire(fresh_ws, claude=True)
    before = (fresh_ws / "CLAUDE.md").read_text(encoding="utf-8")
    result = wire(fresh_ws)  # bare re-wire of the existing file
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
    result = wire(fresh_ws)  # CLAUDE.md exists → bare wire updates it in place
    text = claude.read_text(encoding="utf-8")
    assert "# Project" in text and "House rules." in text and "More rules." in text
    assert "stale khub block" not in text
    assert "@.khub/schema.yaml" in text
    assert text.count(BEGIN) == 1 and text.count(END) == 1
    assert [o.action for o in result.outcomes] == ["updated"]


@pytest.mark.integration
def test_wire_dry_run_writes_nothing(fresh_ws: Path) -> None:
    result = wire(fresh_ws, claude=True, dry_run=True)
    assert not (fresh_ws / "CLAUDE.md").exists()
    assert BEGIN in result.preview and "@.khub/schema.yaml" in result.preview


@pytest.mark.integration
def test_wire_cli_no_workspace_errors(tmp_path: Path) -> None:
    result = runner.invoke(app, ["-C", str(tmp_path), "wire"])
    assert result.exit_code == 1
    assert "khub init" in result.output


@pytest.mark.integration
def test_wire_cli_target_both_creates(fresh_ws: Path) -> None:
    result = runner.invoke(app, ["-C", str(fresh_ws), "wire", "--target", "both"])
    assert result.exit_code == 0
    assert (fresh_ws / "CLAUDE.md").exists() and (fresh_ws / "AGENTS.md").exists()


@pytest.mark.integration
def test_wire_cli_bare_hint_when_none(fresh_ws: Path) -> None:
    result = runner.invoke(app, ["-C", str(fresh_ws), "wire"])
    assert result.exit_code == 0
    assert "No CLAUDE.md or AGENTS.md" in result.output
    assert not (fresh_ws / "CLAUDE.md").exists()


@pytest.mark.integration
def test_wire_cli_bad_target_errors(fresh_ws: Path) -> None:
    result = runner.invoke(app, ["-C", str(fresh_ws), "wire", "--target", "bogus"])
    assert result.exit_code == 1
    assert "bogus" in result.output


@pytest.mark.integration
def test_wire_cli_dry_run_prints_block(fresh_ws: Path) -> None:
    result = runner.invoke(app, ["-C", str(fresh_ws), "wire", "--target", "claude", "--dry-run"])
    assert result.exit_code == 0
    assert "@.khub/schema.yaml" in result.output
    assert not (fresh_ws / "CLAUDE.md").exists()
