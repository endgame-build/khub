"""TS-INT-003 — the git-derived read (WPK-004-2).

`stale` lists entities past an `updated` threshold (oldest first), backfilling a
missing date from `git log` without writing it. Date-sensitive logic is tested
against a fixed `now`; the CLI's `date.today()` is pinned via a `date` subclass.
(TS-INT-004 covered `khub log`, removed in 0.9.0.)
"""

from __future__ import annotations

import json
import os
import subprocess
from datetime import date
from pathlib import Path
from typing import Callable

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.gitlog import stale
from khub.core.index import build_index
from khub.core.introspect import load_schema
from khub.core.project import is_stale

runner = CliRunner()
Seed = Callable[..., None]
NOW = date(2026, 6, 27)

def _git(root: Path, *args: str, when: str | None = None) -> None:
    env = None
    if when:
        env = os.environ.copy()
        env["GIT_AUTHOR_DATE"] = env["GIT_COMMITTER_DATE"] = when
    subprocess.run(
        ["git", "-C", str(root), *args], check=True, capture_output=True, text=True, env=env
    )


def _git_init(root: Path) -> None:
    _git(root, "init")
    _git(root, "config", "user.email", "t@t")
    _git(root, "config", "user.name", "t")


def _commit(root: Path, message: str, when: str) -> None:
    _git(root, "add", "-A")
    _git(root, "commit", "-m", message, when=when)


class _FixedDate(date):
    """A `date` whose `today()` is pinned, so the CLI's threshold is deterministic."""

    @classmethod
    def today(cls) -> date:
        return NOW


# --- stale: threshold, sort, override ----------------------------------------


@pytest.fixture
def stale_ws(fresh_ws: Path, seed: Seed) -> Path:
    """Four fragments spanning the 30- and 90-day windows from the 2026-06-27 run."""
    seed(fresh_ws, "fragments/older-note.md", type="fragment", stage="raw", updated="2025-12-01")
    seed(fresh_ws, "fragments/old-note.md", type="fragment", stage="raw", updated="2026-01-10")
    seed(fresh_ws, "fragments/mid-note.md", type="fragment", stage="raw", updated="2026-04-01")
    seed(fresh_ws, "fragments/fresh-note.md", type="fragment", stage="raw", updated="2026-06-20")
    return fresh_ws


@pytest.mark.unit
def test_stale_default_threshold(stale_ws: Path) -> None:
    """TS-INT-003-U01 (REQ-INT003-01): a 30-day window returns the past set, fresh excluded.

    The bare `days` default now resolves to the workspace stale_days (one source,
    INT-007 unified); this row pins the 30-day window explicitly.
    """
    slugs = {e.slug for e in stale(stale_ws, days=30, now=NOW).entries}
    assert slugs == {"older-note", "old-note", "mid-note"}  # fresh-note within 30 days


@pytest.mark.unit
def test_stale_sorted_oldest_first(stale_ws: Path) -> None:
    """TS-INT-003-U02 (REQ-INT003-01): the stale set is sorted oldest first."""
    order = [e.slug for e in stale(stale_ws, days=30, now=NOW).entries]
    assert order == ["older-note", "old-note", "mid-note"]


@pytest.mark.unit
def test_stale_days_override(stale_ws: Path) -> None:
    """TS-INT-003-U03 (REQ-INT003-02, INT-007): --days 90 shifts the window."""
    slugs = {e.slug for e in stale(stale_ws, days=90, now=NOW).entries}
    assert slugs == {"older-note", "old-note"}  # mid-note (87 days) now within the window


@pytest.mark.e2e
def test_cli_stale_table(stale_ws: Path, monkeypatch) -> None:
    """TS-INT-003-01 (AC-001): a Rich table on a TTY lists the stale set."""
    monkeypatch.setattr("khub.cli.gitlog_cmd.date", _FixedDate)
    monkeypatch.chdir(stale_ws)
    out = runner.invoke(app, ["stale"], env={"FORCE_COLOR": "1"})
    assert out.exit_code == 0
    assert "older-note" in out.output and "old-note" in out.output
    assert "fresh-note" not in out.output


@pytest.mark.integration
def test_cli_stale_days(stale_ws: Path, monkeypatch) -> None:
    """TS-INT-003-02 (AC-002): --days 90 applied via the command surface."""
    monkeypatch.setattr("khub.cli.gitlog_cmd.date", _FixedDate)
    monkeypatch.chdir(stale_ws)
    data = json.loads(runner.invoke(app, ["stale", "--days", "90", "--format", "json"]).output)
    assert {e["slug"] for e in data} == {"older-note", "old-note"}


# --- stale: git-date backfill (read-only) ------------------------------------


@pytest.fixture
def undated_git_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A fragment with no `updated`, committed with a >30-day-old author date."""
    seed(fresh_ws, "fragments/undated.md", type="fragment", stage="raw")  # no `updated`
    _git_init(fresh_ws)
    _commit(fresh_ws, "seed", when="2026-01-01T12:00:00")
    return fresh_ws


@pytest.mark.unit
def test_stale_backfills_git_date(undated_git_ws: Path) -> None:
    """TS-INT-003-U04 (REQ-INT003-03): a missing `updated` is read from git for the compare."""
    entries = {e.slug: e for e in stale(undated_git_ws, now=NOW).entries}
    assert entries["undated"].source == "git log"
    assert entries["undated"].effective_date == date(2026, 1, 1)


@pytest.mark.unit
def test_stale_is_read_only(undated_git_ws: Path) -> None:
    """TS-INT-003-U05 (INT-008): the git date is read, never written back to disk."""
    before = (undated_git_ws / "fragments" / "undated.md").read_bytes()
    stale(undated_git_ws, now=NOW)
    after = (undated_git_ws / "fragments" / "undated.md").read_bytes()
    assert before == after  # no `updated` inserted
    assert "updated" not in frontmatter.loads(after.decode()).metadata


@pytest.mark.integration
def test_cli_stale_no_git(stale_ws: Path, monkeypatch) -> None:
    """TS-INT-003-04 (AC-004, REQ-INT003-04): a non-git workspace falls back with a notice."""
    monkeypatch.setattr("khub.cli.gitlog_cmd.date", _FixedDate)
    monkeypatch.chdir(stale_ws)
    out = runner.invoke(app, ["stale"], env={"FORCE_COLOR": "1"})  # human view shows the notice
    assert out.exit_code == 0
    assert not stale(stale_ws, now=NOW).git_available
    assert "No git history; using updated field only" in out.output


# --- review-fix regressions --------------------------------------------------


@pytest.mark.unit
def test_stale_uses_created_like_status(fresh_ws: Path, seed: Seed) -> None:
    """Review #5: stale and status share one staleness definition (created fallback)."""
    seed(fresh_ws, "fragments/c.md", type="fragment", stage="raw", created="2025-01-01")  # only created
    entries = {e.slug: e for e in stale(fresh_ws, now=NOW).entries}
    assert "c" in entries and entries["c"].source == "created"
    meta = _index(fresh_ws).meta[("fragment", "c")]
    assert is_stale(meta, now=NOW, stale_days=30)  # the projection agrees


def _index(ws: Path):
    return build_index(ws, load_schema(ws))
