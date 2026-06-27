"""TS-ENT-001 — Create an Entity (WPK-002-1).

Covers slug minting and collision, field/enum/pattern validation, the manual
draft flag (default active; --draft to mark unpublished), the referential-
integrity hard-fail (writes no file), strict-mode field filtering, and layout
resolution (folder vs flat).
"""

from __future__ import annotations

from datetime import date
from pathlib import Path
from typing import Callable

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.entity import CreateResult, create, slugify
from khub.core.errors import LocatedError

runner = CliRunner()

Seed = Callable[..., None]


def _prereqs(ws: Path, seed: Seed) -> None:
    """The targets every create scenario can relate to."""
    seed(ws, "clients/initech.md", type="client", name="Initech")
    seed(ws, "identity/team/noor.md", type="person", name="Noor", role="partner")
    seed(ws, "projects/initech-pov/_index.md", type="project", client="initech", owner="noor")
    seed(ws, "partnerships/northwind/_index.md", type="partnership", partner="Northwind", owner="noor")


def _md_files(ws: Path) -> list[str]:
    return sorted(str(p.relative_to(ws)) for p in ws.rglob("*.md") if ".khub" not in p.parts)


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_slugify_normalizes() -> None:
    """slugify lowercases, collapses runs, trims."""
    assert slugify("Acme Corp") == "acme-corp"
    assert slugify("  Hello, World!  ") == "hello-world"
    assert slugify("acme") == "acme"


@pytest.mark.unit
def test_slug_minted_from_id_name_or_type(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-001-U01: id wins, else name, else the type name."""
    _prereqs(fresh_ws, seed)
    by_id = create(fresh_ws, "client", {"name": "Acme Corp"}, id_="explicit")
    assert by_id.slug == "explicit"
    by_name = create(fresh_ws, "client", {"name": "Beta Corp"})
    assert by_name.slug == "beta-corp"
    by_type = create(fresh_ws, "opportunity", {"client": "initech", "owner": "noor", "stage": "prospect"})
    assert by_type.slug == "opportunity"


@pytest.mark.unit
def test_field_validation_enum_and_pattern(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-001-U02: enum and pattern are validated on create."""
    _prereqs(fresh_ws, seed)
    with pytest.raises(LocatedError) as enum_err:
        create(fresh_ws, "opportunity", {"client": "initech", "owner": "noor", "stage": "banana"})
    assert enum_err.value.code == "enum_violation"
    with pytest.raises(LocatedError) as pat_err:
        create(
            fresh_ws,
            "opportunity",
            {"client": "initech", "owner": "noor", "stage": "prospect", "crm_id": "abc"},
        )
    assert pat_err.value.code == "pattern_violation"


@pytest.mark.unit
def test_draft_is_manual(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-001-U03: draft is the manual flag — default false even when required is missing."""
    _prereqs(fresh_ws, seed)
    # Missing required client/owner still saves, active by default (completeness is check's job).
    active = create(fresh_ws, "opportunity", {"stage": "prospect"})
    assert active.draft is False
    meta = frontmatter.load(str(active.path)).metadata
    assert meta["stage"] == "prospect" and meta["draft"] is False
    # --draft (passed via the create flag) marks it unpublished.
    drafted = create(
        fresh_ws, "opportunity", {"client": "initech", "owner": "noor", "stage": "prospect"}, draft=True
    )
    assert drafted.draft is True
    assert frontmatter.load(str(drafted.path)).metadata["draft"] is True


@pytest.mark.unit
def test_referential_integrity_hard_fail_writes_no_file(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-001-U04: an unresolvable relation hard-fails and writes nothing."""
    _prereqs(fresh_ws, seed)
    before = _md_files(fresh_ws)
    with pytest.raises(LocatedError) as err:
        create(fresh_ws, "opportunity", {"client": "ghost-co", "owner": "noor", "stage": "prospect"})
    assert err.value.code == "referential_integrity"
    assert _md_files(fresh_ws) == before


@pytest.mark.unit
def test_strict_filter(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-001-U05: --strict rejects an undeclared field; otherwise it is preserved."""
    _prereqs(fresh_ws, seed)
    with pytest.raises(LocatedError) as err:
        create(
            fresh_ws,
            "opportunity",
            {"client": "initech", "owner": "noor", "stage": "prospect", "vibe": "high"},
            strict=True,
        )
    assert err.value.code == "strict_unknown_field"
    loose = create(
        fresh_ws,
        "opportunity",
        {"client": "initech", "owner": "noor", "stage": "prospect", "vibe": "high"},
    )
    assert frontmatter.load(str(loose.path)).metadata["vibe"] == "high"


@pytest.mark.unit
def test_empty_slug_is_rejected(fresh_ws: Path, seed: Seed) -> None:
    """A source that slugifies to empty is refused, never written to a hidden path."""
    _prereqs(fresh_ws, seed)
    with pytest.raises(LocatedError) as err:
        create(fresh_ws, "client", {"name": "!!!"})
    assert err.value.code == "invalid_slug"


@pytest.mark.unit
def test_collision_suffix_is_deterministic(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-001-U06: a within-type collision appends -2, then -3."""
    _prereqs(fresh_ws, seed)
    first = create(fresh_ws, "client", {"name": "Acme"}, id_="acme")
    second = create(fresh_ws, "client", {"name": "Acme"}, id_="acme")
    third = create(fresh_ws, "client", {"name": "Acme"}, id_="acme")
    assert (first.slug, second.slug, third.slug) == ("acme", "acme-2", "acme-3")


@pytest.mark.unit
def test_layout_resolution_and_engagement_edge(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-001-U07: folder vs flat path; engagement stored as an explicit edge."""
    _prereqs(fresh_ws, seed)
    opp: CreateResult = create(
        fresh_ws, "opportunity", {"client": "initech", "owner": "noor", "stage": "prospect"}
    )
    assert opp.path == fresh_ws / "opportunities" / opp.slug / "_index.md"
    meeting = create(
        fresh_ws,
        "meeting",
        {"engagement": "initech-pov", "call_type": "client", "source": "recording", "date": "2026-06-19"},
    )
    assert meeting.path == fresh_ws / "meetings" / f"{meeting.slug}.md"
    assert frontmatter.load(str(meeting.path)).metadata["engagement"] == "initech-pov"


# --- integration / e2e -------------------------------------------------------


@pytest.mark.e2e
def test_cli_create_active(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-001-01: a well-formed entity lands active with today's timestamps."""
    _prereqs(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["add", "opportunity", "--client", "initech", "--owner", "noor", "--stage", "prospect"])
    assert result.exit_code == 0
    assert "Created opportunity 'opportunity' (active)" in result.output
    written = fresh_ws / "opportunities" / "opportunity" / "_index.md"
    assert written.exists()
    meta = frontmatter.load(str(written)).metadata
    assert meta["draft"] is False
    assert meta["created"] == date.today() and meta["updated"] == date.today()


@pytest.mark.integration
def test_cli_missing_required_saves_active(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-001-02: an omitted required relation never blocks capture; it saves active."""
    _prereqs(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["add", "opportunity", "--stage", "prospect"])
    assert result.exit_code == 0
    assert "Created opportunity 'opportunity' (active)" in result.output
    written = fresh_ws / "opportunities" / "opportunity" / "_index.md"
    assert written.exists()
    assert frontmatter.load(str(written)).metadata["draft"] is False


@pytest.mark.integration
def test_cli_add_draft_flag(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-001-02: --draft marks the new entity unpublished."""
    _prereqs(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["add", "opportunity", "--client", "initech", "--owner", "noor", "--stage", "prospect", "--draft"])
    assert result.exit_code == 0
    assert "Created opportunity 'opportunity' (draft)" in result.output
    written = fresh_ws / "opportunities" / "opportunity" / "_index.md"
    assert frontmatter.load(str(written)).metadata["draft"] is True


@pytest.mark.integration
def test_cli_referential_hard_fail_message_and_no_file(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-001-03: an unresolvable relation is refused with no file written."""
    _prereqs(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    before = _md_files(fresh_ws)
    result = runner.invoke(app, ["add", "opportunity", "--client", "ghost-co", "--owner", "noor", "--stage", "prospect"])
    assert result.exit_code == 1
    assert "No client 'ghost-co' to satisfy relation 'client'" in result.output
    assert _md_files(fresh_ws) == before


@pytest.mark.integration
def test_cli_strict_rejects_unknown_field(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-001-04: --strict rejects `vibe`; without it the field is preserved."""
    _prereqs(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    strict = runner.invoke(app, ["add", "opportunity", "--client", "initech", "--owner", "noor", "--stage", "prospect", "--vibe", "high", "--strict"])
    assert strict.exit_code == 1
    assert "Unknown field 'vibe' rejected under --strict" in strict.output
    loose = runner.invoke(app, ["add", "opportunity", "--client", "initech", "--owner", "noor", "--stage", "prospect", "--vibe", "high"])
    assert loose.exit_code == 0
    written = fresh_ws / "opportunities" / "opportunity" / "_index.md"
    assert frontmatter.load(str(written)).metadata["vibe"] == "high"


@pytest.mark.integration
def test_cli_meeting_flat_with_engagement(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-001-05: a meeting writes flat and stores its engagement union edge."""
    _prereqs(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["add", "meeting", "--engagement", "initech-pov", "--call-type", "client", "--source", "recording", "--date", "2026-06-19"])
    assert result.exit_code == 0
    written = fresh_ws / "meetings" / "meeting.md"
    assert written.exists()
    assert frontmatter.load(str(written)).metadata["engagement"] == "initech-pov"


@pytest.mark.integration
def test_cli_explicit_id_and_collision(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-001-06: explicit --id is used, collides to acme-2, mints from name when omitted."""
    _prereqs(fresh_ws, seed)
    monkeypatch.chdir(fresh_ws)
    first = runner.invoke(app, ["add", "client", "--id", "acme", "--name", "Acme Corp"])
    assert first.exit_code == 0 and (fresh_ws / "clients" / "acme.md").exists()
    second = runner.invoke(app, ["add", "client", "--id", "acme", "--name", "Acme Corp"])
    assert second.exit_code == 0 and (fresh_ws / "clients" / "acme-2.md").exists()
    minted = runner.invoke(app, ["add", "client", "--name", "Beta Corp"])
    assert minted.exit_code == 0 and (fresh_ws / "clients" / "beta-corp.md").exists()
