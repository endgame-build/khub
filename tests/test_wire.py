"""Tests for ``khub wire`` — the managed agent-file block(s), import vs pointer."""

from __future__ import annotations

from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.wire import BEGIN, END, wire
from khub.core.workspace import init_workspace

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
def test_wire_bare_creates_the_file_that_is_missing(fresh_ws: Path) -> None:
    """A repo carrying only one context file was wired only for the agents that read
    that one; the other stayed blind to a workspace sitting right there."""
    wire(fresh_ws, claude=True)  # seed CLAUDE.md
    result = wire(fresh_ws)  # bare: updates CLAUDE.md, creates AGENTS.md
    assert {o.path.name for o in result.outcomes} == {"CLAUDE.md", "AGENTS.md"}
    actions = {o.path.name: o.action for o in result.outcomes}
    assert actions == {"CLAUDE.md": "unchanged", "AGENTS.md": "created"}
    assert (fresh_ws / "AGENTS.md").is_file()


@pytest.mark.integration
def test_wire_bare_updates_both_when_both_exist(fresh_ws: Path) -> None:
    wire(fresh_ws, claude=True, agents=True)  # seed both
    result = wire(fresh_ws)  # bare updates both
    assert {o.path.name for o in result.outcomes} == {"CLAUDE.md", "AGENTS.md"}


@pytest.mark.integration
def test_wire_bare_seeds_both_when_neither_exists(fresh_ws: Path) -> None:
    result = wire(fresh_ws)
    assert [o.action for o in result.outcomes] == ["created", "created"]
    assert (fresh_ws / "CLAUDE.md").is_file() and (fresh_ws / "AGENTS.md").is_file()


@pytest.mark.integration
@pytest.mark.integration
def test_singleton_cues_carry_their_file_link(tmp_path: Path) -> None:
    """The block tells an agent to edit the existing document — and now says which.

    build-lite's two singletons are the case: without the link, "edit the existing
    document, never add a second" leaves the agent to introspect or guess the path.
    """
    ws = tmp_path / "ws"
    init_workspace("build-lite", ws)
    wire(ws, claude=True, agents=True)

    for name in ("CLAUDE.md", "AGENTS.md"):
        text = (ws / name).read_text()
        assert "[knowledge/prd.md](knowledge/prd.md)" in text, name
        assert "[knowledge/arc42.md](knowledge/arc42.md)" in text, name
        # a non-singleton is written by `khub add`, so its cue names no path
        adr_line = next(ln for ln in text.splitlines() if ln.startswith("- `adr`"))
        assert "](" not in adr_line


def test_wire_is_idempotent(fresh_ws: Path) -> None:
    wire(fresh_ws)
    before = (fresh_ws / "CLAUDE.md").read_text(encoding="utf-8")
    result = wire(fresh_ws)  # bare re-wire
    after = (fresh_ws / "CLAUDE.md").read_text(encoding="utf-8")
    assert after == before
    assert [o.action for o in result.outcomes] == ["unchanged", "unchanged"]


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
    actions = {o.path.name: o.action for o in result.outcomes}
    assert actions == {"CLAUDE.md": "updated", "AGENTS.md": "created"}


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
def test_wire_cli_bare_seeds_both(fresh_ws: Path) -> None:
    """There is no "nothing to wire" state any more — bare wire always has both."""
    result = runner.invoke(app, ["-C", str(fresh_ws), "wire"])
    assert result.exit_code == 0
    assert "created CLAUDE.md" in result.output and "created AGENTS.md" in result.output
    assert (fresh_ws / "CLAUDE.md").is_file() and (fresh_ws / "AGENTS.md").is_file()


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
