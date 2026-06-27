"""TS-ENT-003 — Edit an Entity (WPK-002-2).

Covers enum re-validation, the ``updated`` bump, minimal-diff round-trips
(key order + comments preserved), manual draft toggling (no auto-promote),
and strict-mode field rejection.
"""

from __future__ import annotations

from datetime import date
from pathlib import Path
from typing import Callable

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.entity import create, update
from khub.core.errors import LocatedError

runner = CliRunner()

Seed = Callable[..., None]


def _prereqs(ws: Path, seed: Seed) -> None:
    seed(ws, "clients/initech.md", type="client", name="Initech")
    seed(ws, "identity/team/noor.md", type="person", name="Noor", role="partner")


def _deal(ws: Path, seed: Seed, **over: object) -> Path:
    meta: dict[str, object] = dict(
        type="opportunity",
        created="2026-01-01",
        updated="2026-01-01",
        draft=False,
        stage="prospect",
        client="initech",
        owner="noor",
    )
    meta.update(over)
    seed(ws, "opportunities/initech-deal/_index.md", **meta)
    return ws / "opportunities" / "initech-deal" / "_index.md"


def _changed_keys(before: str, after: str) -> set[str]:
    keys: set[str] = set()
    for bl, al in zip(before.split("\n"), after.split("\n")):
        if bl != al:
            keys.add((al or bl).split(":", 1)[0].strip())
    return keys


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_enum_revalidation_leaves_file_unchanged(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-003-U01: an out-of-enum edit raises and writes nothing."""
    _prereqs(fresh_ws, seed)
    path = _deal(fresh_ws, seed)
    before = path.read_bytes()
    with pytest.raises(LocatedError) as err:
        update(fresh_ws, "initech-deal", {"stage": "banana"})
    assert err.value.code == "enum_violation"
    assert path.read_bytes() == before


@pytest.mark.unit
def test_updated_is_bumped(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-003-U02: a successful edit bumps updated to today."""
    _prereqs(fresh_ws, seed)
    path = _deal(fresh_ws, seed)
    update(fresh_ws, "initech-deal", {"stage": "won"})
    assert frontmatter.load(str(path)).metadata["updated"] == date.today()


@pytest.mark.unit
def test_minimal_diff_preserves_order_and_comments(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-003-U03: a round-trip edit preserves comments and confines the diff."""
    _prereqs(fresh_ws, seed)
    path = fresh_ws / "opportunities" / "initech-deal" / "_index.md"
    path.parent.mkdir(parents=True, exist_ok=True)
    before = (
        "---\n"
        "type: opportunity\n"
        "created: 2026-01-01\n"
        "updated: 2026-01-01\n"
        "draft: false\n"
        "stage: prospect  # current pipeline stage\n"
        "client: initech\n"
        "owner: noor\n"
        "---\n"
    )
    path.write_text(before)
    update(fresh_ws, "initech-deal", {"stage": "won"})
    after = path.read_text()
    assert "# current pipeline stage" in after
    assert _changed_keys(before, after) == {"stage", "updated"}


@pytest.mark.unit
def test_create_then_edit_is_minimal_diff(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-003-U03: a create-authored file edits to a 2-line diff and emits no YAML anchors."""
    _prereqs(fresh_ws, seed)
    result = create(
        fresh_ws, "opportunity", {"client": "initech", "owner": "noor", "stage": "prospect"}
    )
    before = result.path.read_text()
    assert "&id" not in before and "*id" not in before  # created/updated must not alias
    update(fresh_ws, result.slug, {"stage": "won"})
    after = result.path.read_text()
    changed = _changed_keys(before, after)
    # created is untouched; only stage (and updated, if a different day) may move.
    assert "stage" in changed and changed <= {"stage", "updated"}


@pytest.mark.unit
def test_field_edit_does_not_touch_draft(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-003-U04: a field edit never flips draft — no auto-promote."""
    _prereqs(fresh_ws, seed)
    path = _deal(fresh_ws, seed, draft=True)
    result = update(fresh_ws, "initech-deal", {"stage": "won"})
    assert result.draft is True
    assert frontmatter.load(str(path)).metadata["draft"] is True


@pytest.mark.unit
def test_edit_toggles_draft(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-003-U04: `edit <id> draft true|false` sets the flag by hand."""
    _prereqs(fresh_ws, seed)
    path = _deal(fresh_ws, seed, draft=False)
    update(fresh_ws, "initech-deal", {"draft": "true"})
    assert frontmatter.load(str(path)).metadata["draft"] is True
    update(fresh_ws, "initech-deal", {"draft": "false"})
    assert frontmatter.load(str(path)).metadata["draft"] is False


@pytest.mark.unit
def test_strict_editor_rejects_unknown(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-003-U05: --strict rejects an undeclared field; otherwise it is preserved."""
    _prereqs(fresh_ws, seed)
    path = _deal(fresh_ws, seed)
    with pytest.raises(LocatedError) as err:
        update(fresh_ws, "initech-deal", {"vibe": "high"}, strict=True)
    assert err.value.code == "strict_unknown_field"
    update(fresh_ws, "initech-deal", {"vibe": "high"})
    assert frontmatter.load(str(path)).metadata["vibe"] == "high"


# --- integration / e2e -------------------------------------------------------


@pytest.mark.integration
def test_cli_edit_field(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-003-01: edit a field, bump updated, minimal diff, confirmation line."""
    _prereqs(fresh_ws, seed)
    path = _deal(fresh_ws, seed)
    before = path.read_text()
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["edit", "initech-deal", "stage", "proposal-sent"])
    assert result.exit_code == 0
    assert "Updated opportunity 'initech-deal'" in result.output
    after = path.read_text()
    assert _changed_keys(before, after) == {"stage", "updated"}
    assert frontmatter.load(str(path)).metadata["stage"] == "proposal-sent"


@pytest.mark.e2e
def test_cli_edit_publishes_draft(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-003-02: a field edit leaves draft alone; `edit <id> draft false` publishes."""
    _prereqs(fresh_ws, seed)
    seed(
        fresh_ws,
        "opportunities/some-opp/_index.md",
        type="opportunity",
        created="2026-01-01",
        updated="2026-01-01",
        draft=True,
        stage="prospect",
        client="initech",
    )
    path = fresh_ws / "opportunities" / "some-opp" / "_index.md"
    monkeypatch.chdir(fresh_ws)
    # A field edit fills owner but does NOT auto-promote.
    runner.invoke(app, ["edit", "some-opp", "--owner", "noor"])
    assert frontmatter.load(str(path)).metadata["draft"] is True
    # Publishing is an explicit, manual step.
    result = runner.invoke(app, ["edit", "some-opp", "draft", "false"])
    assert result.exit_code == 0
    meta = frontmatter.load(str(path)).metadata
    assert meta["draft"] is False and meta["owner"] == "noor"


@pytest.mark.integration
def test_cli_edit_invalid_enum(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-003-03: an out-of-enum value is rejected and the file left unchanged."""
    _prereqs(fresh_ws, seed)
    path = _deal(fresh_ws, seed)
    before = path.read_bytes()
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["edit", "initech-deal", "stage", "banana"])
    assert result.exit_code == 1
    assert "'banana' is not a valid stage (prospect, proposal-sent, won, signed, lost)" in result.output
    assert path.read_bytes() == before


@pytest.mark.integration
def test_cli_edit_strict_unknown_field(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-003-04: --strict rejects an undeclared field; without it the field persists."""
    _prereqs(fresh_ws, seed)
    path = _deal(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    strict = runner.invoke(app, ["edit", "initech-deal", "vibe", "high", "--strict"])
    assert strict.exit_code == 1
    assert "Unknown field 'vibe' rejected under --strict" in strict.output
    loose = runner.invoke(app, ["edit", "initech-deal", "vibe", "high"])
    assert loose.exit_code == 0
    assert frontmatter.load(str(path)).metadata["vibe"] == "high"
