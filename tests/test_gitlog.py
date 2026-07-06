"""TS-INT-003 / TS-INT-004 — the git-derived reads (WPK-004-2).

`stale` lists entities past an `updated` threshold (oldest first), backfilling a
missing date from `git log` without writing it. `log` renders git history at
ontology altitude — entities and the relations a commit touched, never file paths.
Date-sensitive logic is tested against a fixed `now`; the CLI's `date.today()` is
pinned via a `date` subclass.
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
from khub.core import entity
from khub.core.errors import LocatedError
from khub.core.gitlog import log, stale
from khub.core.graph import history
from khub.core.index import build_index
from khub.core.introspect import load_schema
from khub.core.project import is_stale
from khub.core.workspace import init_workspace

runner = CliRunner()
Seed = Callable[..., None]
NOW = date(2026, 6, 27)

HISTORY_PRESET = """
version: "0.1.0"
entities:
  decision:
    layout: file
    path: decisions
    relations:
      supersedes: { to: decision, inverse: superseded_by }
"""


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


# --- log: history at ontology altitude ---------------------------------------


@pytest.fixture
def log_ws(fresh_ws: Path) -> Path:
    """A real commit history: owner added to a project, then a stage change on a deal."""
    entity.create(fresh_ws, "person", {"name": "Noor", "role": "partner"}, id_="noor")
    entity.create(fresh_ws, "client", {"name": "Initech"}, id_="initech")
    entity.create(fresh_ws, "opportunity",
                  {"stage": "prospect", "client": "initech", "owner": "noor"}, id_="initech-deal")
    entity.create(fresh_ws, "project", {"client": "initech"}, id_="initech-pov")  # no owner yet
    _git_init(fresh_ws)
    _commit(fresh_ws, "seed", when="2026-05-01T12:00:00")

    entity.link(fresh_ws, "initech-pov", "owner", "noor")  # adds the owner edge
    _commit(fresh_ws, "add owner to pov", when="2026-05-10T12:00:00")

    entity.update(fresh_ws, "initech-deal", {"stage": "won"})  # an attribute-only change
    _commit(fresh_ws, "change deal stage", when="2026-05-20T12:00:00")
    return fresh_ws


@pytest.mark.unit
def test_log_maps_commits_to_entities(log_ws: Path) -> None:
    """TS-INT-004-U01 (REQ-INT004-01): each commit's files map to entity ids."""
    entries = log(log_ws)
    assert entries is not None
    assert any(e.type == "project" and e.slug == "initech-pov" for e in entries)
    assert all(e.commit for e in entries)  # every entry carries its commit hash


@pytest.mark.unit
def test_log_renders_relation_touched(log_ws: Path) -> None:
    """TS-INT-004-U02 (INT-009): the owner-add commit names the `owner` relation."""
    entries = log(log_ws)
    assert entries is not None
    owner_adds = [e for e in entries if e.slug == "initech-pov" and "owner" in e.relations]
    assert owner_adds  # the add-owner commit surfaced the owner relation at altitude


@pytest.mark.unit
def test_log_filters_to_one_entity(log_ws: Path) -> None:
    """TS-INT-004-U03 (REQ-INT004-02): log <id> returns only that entity's commits."""
    entries = log(log_ws, "initech-pov")
    assert entries is not None and entries
    assert all(e.slug == "initech-pov" for e in entries)  # the deal-only commit is excluded


@pytest.mark.unit
def test_log_window_limit_and_since(log_ws: Path) -> None:
    """TS-INT-004-U04 (REQ-INT004-01): --limit and --since bound the rendered history."""
    full = log(log_ws)
    assert full is not None
    newest = log(log_ws, limit=1)
    assert newest is not None
    assert {e.commit for e in newest} == {full[0].commit}  # only the newest commit
    since = log(log_ws, since="2026-05-15")
    assert since is not None
    assert all(e.slug == "initech-deal" for e in since)  # only the 2026-05-20 commit


@pytest.mark.integration
def test_cli_log_altitude(log_ws: Path, monkeypatch) -> None:
    """TS-INT-004-01 (AC-001): the rendered change names entity and relation, not a path."""
    monkeypatch.chdir(log_ws)
    payload = json.loads(runner.invoke(app, ["log", "--format", "json"]).output)
    assert payload["git_available"] is True  # one JSON shape in every state
    pov = [e for e in payload["entries"] if e["slug"] == "initech-pov"]
    assert any("owner" in e["relations"] for e in pov)
    out = runner.invoke(app, ["log"], env={"FORCE_COLOR": "1"}).output
    assert "initech-pov" in out
    assert "projects/initech-pov/_index.md" not in out  # ontology altitude, never the path


@pytest.mark.integration
def test_cli_log_per_entity(log_ws: Path, monkeypatch) -> None:
    """TS-INT-004-02 (AC-002): log <id> returns only that entity's commits, in order."""
    monkeypatch.chdir(log_ws)
    payload = json.loads(runner.invoke(app, ["log", "initech-pov", "--format", "json"]).output)
    data = payload["entries"]
    assert data and all(e["slug"] == "initech-pov" for e in data)


# --- log: distinct from supersession history ---------------------------------


@pytest.fixture
def supersedes_ws(tmp_path: Path, seed: Seed) -> Path:
    """A generic decision with a supersedes chain plus a git edit history of one record."""
    src = tmp_path / "presets"
    src.mkdir()
    (src / "fixture.yaml").write_text(HISTORY_PRESET)
    ws = tmp_path / "ws"
    init_workspace("fixture", ws, preset_source=src)
    seed(ws, "decisions/decision-0001.md", type="decision")
    seed(ws, "decisions/decision-0005.md", type="decision", supersedes="decision-0001")
    seed(ws, "decisions/decision-0008.md", type="decision", supersedes="decision-0005")
    seed(ws, "decisions/decision-0012.md", type="decision", supersedes="decision-0008")
    _git_init(ws)
    _commit(ws, "seed decisions", when="2026-05-01T12:00:00")
    seed(ws, "decisions/decision-0012.md", type="decision", supersedes="decision-0008", title="edit")
    _commit(ws, "edit 0012", when="2026-05-10T12:00:00")
    return ws


@pytest.mark.unit
def test_log_is_git_not_supersession(supersedes_ws: Path) -> None:
    """TS-INT-004-U05 (REQ-INT004-03, INT-010): log reads git, distinct from the chain."""
    git_entries = log(supersedes_ws, "decision-0012")
    assert git_entries is not None
    assert {e.slug for e in git_entries} == {"decision-0012"}  # only 0012's git commits
    chain = [link.slug for link in history(_index(supersedes_ws), "decision-0012")]
    assert chain == ["decision-0012", "decision-0008", "decision-0005", "decision-0001"]
    assert {e.commit for e in git_entries} != set(chain)  # git commits, not chain links


@pytest.mark.integration
def test_cli_log_distinct_from_history(supersedes_ws: Path, monkeypatch) -> None:
    """TS-INT-004-03 (AC-003): log <record> returns git commits, not the supersedes chain."""
    monkeypatch.chdir(supersedes_ws)
    payload = json.loads(runner.invoke(app, ["log", "decision-0012", "--format", "json"]).output)
    data = payload["entries"]
    assert data and all(e["slug"] == "decision-0012" for e in data)
    assert all("decision-0008" not in json.dumps(e) for e in data)  # the chain is absent


# --- log: no git history -----------------------------------------------------


@pytest.mark.unit
def test_log_no_git_returns_none(stale_ws: Path) -> None:
    """TS-INT-004-U06 (REQ-INT004-04): a non-git workspace yields no history."""
    assert log(stale_ws) is None


@pytest.mark.integration
def test_cli_log_no_git(stale_ws: Path, monkeypatch) -> None:
    """TS-INT-004-04 (AC-004): the absence is reported and exits 0 (a no-op success).

    On a non-TTY pipe (the runner default, no --format) the no-op stays valid JSON:
    an empty payload flagged git_available=false, not the bare human notice.
    """
    monkeypatch.chdir(stale_ws)
    out = runner.invoke(app, ["log"])
    assert out.exit_code == 0
    assert json.loads(out.output) == {"entries": [], "git_available": False}
    # the human notice is the TTY branch
    tty = runner.invoke(app, ["log"], env={"FORCE_COLOR": "1"})
    assert tty.exit_code == 0 and "No git history available" in tty.output


# --- review-fix regressions --------------------------------------------------


@pytest.mark.unit
def test_log_bad_id_no_git_raises(stale_ws: Path) -> None:
    """Review #11: a bad id is resolved before the git check, so it errors with or without git."""
    with pytest.raises(LocatedError) as err:
        log(stale_ws, "ghost")
    assert err.value.code == "lookup_error"


@pytest.mark.integration
def test_cli_log_bad_id_no_git(stale_ws: Path, monkeypatch) -> None:
    """Review #11: `khub log ghost` in a non-git workspace reports the lookup error, exit 1."""
    monkeypatch.chdir(stale_ws)
    out = runner.invoke(app, ["log", "ghost"])
    assert out.exit_code == 1 and "No entity 'ghost' found" in out.output


@pytest.mark.unit
def test_log_ignores_relation_reorder(fresh_ws: Path, seed: Seed) -> None:
    """Review #7: a pure reorder of a many-valued relation is not flagged as a change."""
    entity.create(fresh_ws, "person", {"name": "W", "role": "consultant"}, id_="writer")
    seed(fresh_ws, "fragments/frag1.md", type="fragment", stage="raw", owner="writer", created="2026-05-01")
    seed(fresh_ws, "fragments/frag2.md", type="fragment", stage="raw", owner="writer", created="2026-05-01")
    seed(fresh_ws, "fragments/main.md", type="fragment", stage="raw", owner="writer",
         depends_on=["frag1", "frag2"], created="2026-05-01")
    _git_init(fresh_ws)
    _commit(fresh_ws, "seed", when="2026-05-01T12:00:00")
    seed(fresh_ws, "fragments/main.md", type="fragment", stage="raw", owner="writer",
         depends_on=["frag2", "frag1"], created="2026-05-01")  # same members, reordered
    _commit(fresh_ws, "reorder", when="2026-05-02T12:00:00")

    entries = log(fresh_ws, "main")
    assert entries is not None and entries
    assert "depends_on" not in entries[0].relations  # newest = the reorder commit


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
