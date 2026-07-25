"""TS-QRY-001 — Filter Entities by Frontmatter (WPK-003-1).

Covers filter AND semantics, `--missing`/`--has` gap finding, the unknown-field
filter error, empty-result success, `--format ids`, orphan/stale annotation and
filters, and draft scoping. firm-ops opportunities/projects are the field and
gap targets; the located text is asserted as a substring plus the variable field.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from datetime import date
from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.errors import LocatedError
from khub.core.query import QueryFilters, query

runner = CliRunner()
Seed = Callable[..., None]
NOW = date(2026, 6, 27)


@pytest.fixture
def qws(fresh_ws: Path, seed: Seed) -> Path:
    """A firm-ops workspace with opportunities across stages and a project gap."""
    recent = {"created": date(2026, 6, 1), "updated": date(2026, 6, 1)}
    seed(fresh_ws, "clients/initech.md", type="client", name="Initech", **recent)
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor", role="partner",
         created=date(2026, 6, 1))
    for slug, stage in [("op-prospect", "prospect"), ("op-proposal", "proposal-sent"),
                        ("op-won", "won")]:
        seed(fresh_ws, f"opportunities/{slug}/_index.md", type="opportunity", stage=stage,
             client="initech", owner="noor", **recent)
    # one active project whose owner resolves (with a bool attr + a many-valued
    # relation for the coercion tests); one draft project missing owner (the gap)
    seed(fresh_ws, "projects/has-owner/_index.md", type="project", client="initech",
         owner="noor", active=True, team=["noor"], **recent)
    seed(fresh_ws, "projects/no-owner/_index.md", type="project", client="initech",
         draft=True, **recent)
    # an orphan (no edges) and a stale (old, but edged) client for the flag filters
    seed(fresh_ws, "clients/orphan-client.md", type="client", name="Orphan", **recent)
    seed(fresh_ws, "clients/stale-client.md", type="client", name="Stale",
         depends_on=["initech"], created=date(2000, 1, 1), updated=date(2000, 1, 1))
    # a tagged (non-orphan) client — the positive control for the tag/body tests
    seed(fresh_ws, "clients/tagged.md", type="client", name="Tagged",
         tags=["vip", "active"], depends_on=["initech"], **recent)
    return fresh_ws


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_query_ands_filters(qws: Path) -> None:
    """TS-QRY-001-U01 (QRY-001): every filter ANDs — type + field discriminates."""
    res = query(qws, QueryFilters(type="opportunity", fields={"stage": "prospect"}), now=NOW)
    assert [m.slug for m in res] == ["op-prospect"]
    # A second, non-matching field rules it out (AND, not OR).
    none = query(qws, QueryFilters(type="opportunity", fields={"stage": "prospect", "source": "event"}), now=NOW)
    assert none == []


@pytest.mark.unit
def test_query_never_matches_body_prose(qws: Path) -> None:
    """QRY-001: filters read frontmatter/edges only — a body-only string never matches."""
    (qws / "clients" / "bodyword.md").write_text(
        "---\ntype: client\nname: Plain\ncreated: 2026-06-01\nupdated: 2026-06-01\n"
        "tags: []\n---\nhiddenword lives only in the body\n"
    )
    assert query(qws, QueryFilters(type="client", tag="hiddenword"), now=NOW) == []


@pytest.mark.unit
def test_query_missing_and_has(qws: Path) -> None:
    """TS-QRY-001-U02 (REQ-QRY001-02): `--missing`/`--has` resolve the edge."""
    missing = query(qws, QueryFilters(type="project", missing="owner"), now=NOW)
    assert [m.slug for m in missing] == ["no-owner"]
    has = query(qws, QueryFilters(type="project", has="owner"), now=NOW)
    assert [m.slug for m in has] == ["has-owner"]


@pytest.mark.unit
def test_query_unknown_field_raises(qws: Path) -> None:
    """TS-QRY-001-U03 (REQ-QRY001-03): an undeclared field is a located filter error."""
    with pytest.raises(LocatedError) as err:
        query(qws, QueryFilters(type="opportunity", fields={"vibe": "high"}), now=NOW)
    assert err.value.code == "filter_error"
    assert err.value.target == "vibe" and err.value.type == "opportunity"


@pytest.mark.unit
def test_query_unknown_predicate_raises(qws: Path) -> None:
    """QRY-001: a mistyped --has/--missing predicate errors, not a silent empty set."""
    with pytest.raises(LocatedError) as err:
        query(qws, QueryFilters(type="project", missing="ownre"), now=NOW)
    assert err.value.code == "filter_error" and err.value.target == "ownre"


@pytest.mark.unit
def test_query_tag_matches_membership(qws: Path) -> None:
    """QRY-001: --tag matches list membership exactly — never a substring (positive control)."""
    assert [m.slug for m in query(qws, QueryFilters(type="client", tag="vip"), now=NOW)] == ["tagged"]
    # a tag prefix must NOT substring-match the real "vip" tag
    assert query(qws, QueryFilters(type="client", tag="vi"), now=NOW) == []


@pytest.mark.unit
def test_query_matches_list_and_bool_fields(qws: Path) -> None:
    """A many-valued relation matches on membership; a bool matches case-insensitively."""
    team = query(qws, QueryFilters(type="project", fields={"team": "noor"}), now=NOW)
    assert [m.slug for m in team] == ["has-owner"]
    active = query(qws, QueryFilters(type="project", fields={"active": "true"}), now=NOW)
    assert [m.slug for m in active] == ["has-owner"]


@pytest.mark.unit
def test_query_carries_and_filters_flags(qws: Path) -> None:
    """TS-QRY-001-U05 (QRY-009): every match carries orphan/stale; the flags filter."""
    every = query(qws, QueryFilters(type="client"), now=NOW)
    assert every and all(isinstance(m.orphan, bool) and isinstance(m.stale, bool) for m in every)
    orphans = query(qws, QueryFilters(orphan=True), now=NOW)
    assert {m.slug for m in orphans} == {"orphan-client"} and all(m.orphan for m in orphans)
    stales = query(qws, QueryFilters(stale=True), now=NOW)
    assert "stale-client" in {m.slug for m in stales} and all(m.stale for m in stales)


@pytest.mark.unit
def test_query_empty_is_success(qws: Path) -> None:
    """TS-QRY-001-U06 (QRY-002): no match returns an empty collection, never raising."""
    assert query(qws, QueryFilters(type="opportunity", fields={"stage": "lost"}), now=NOW) == []


@pytest.mark.unit
def test_query_draft_scoping(qws: Path) -> None:
    """TS-QRY-001-U07 (QRY-SHARED-003): default includes drafts; `--active`/`--draft` narrow."""
    default = {m.slug for m in query(qws, QueryFilters(type="project"), now=NOW)}
    assert default == {"has-owner", "no-owner"}
    active = {m.slug for m in query(qws, QueryFilters(type="project", active_only=True), now=NOW)}
    assert active == {"has-owner"}
    drafts = {m.slug for m in query(qws, QueryFilters(type="project", draft_only=True), now=NOW)}
    assert drafts == {"no-owner"}


@pytest.mark.unit
def test_query_missing_surfaces_gap_regardless_of_draft(qws: Path) -> None:
    """QRY-SHARED-003: `--missing` surfaces incompleteness even on a draft entity."""
    # no-owner is a draft; --missing owner still surfaces it (gap is orthogonal to draft).
    res = query(qws, QueryFilters(type="project", missing="owner"), now=NOW)
    assert [m.slug for m in res] == ["no-owner"]


# --- integration / e2e -------------------------------------------------------


@pytest.mark.e2e
def test_cli_query_type_and_field(qws: Path, monkeypatch) -> None:
    """TS-QRY-001-01 (AC-001): type+field filter, orphan/stale annotation, JSON, limit."""
    monkeypatch.chdir(qws)
    out = runner.invoke(app, ["query", "--type", "opportunity", "--stage", "prospect", "--format", "json"])
    assert out.exit_code == 0, out.output
    data = json.loads(out.output)
    # the JSON id is now the qualified type/slug, with type/slug also carried separately
    assert [d["id"] for d in data] == ["opportunity/op-prospect"]
    assert data[0]["type"] == "opportunity" and data[0]["slug"] == "op-prospect"
    assert "orphan" in data[0] and "stale" in data[0]
    capped = runner.invoke(app, ["query", "--type", "opportunity", "--limit", "2", "--format", "json"])
    assert len(json.loads(capped.output)) == 2


@pytest.mark.integration
def test_cli_query_missing_and_has(qws: Path, monkeypatch) -> None:
    """TS-QRY-001-02 (AC-002): `--missing owner` is the gap; `--has owner` the inverse."""
    monkeypatch.chdir(qws)
    missing = runner.invoke(app, ["query", "--type", "project", "--missing", "owner", "--format", "json"])
    assert [d["id"] for d in json.loads(missing.output)] == ["project/no-owner"]
    has = runner.invoke(app, ["query", "--type", "project", "--has", "owner", "--format", "json"])
    assert [d["id"] for d in json.loads(has.output)] == ["project/has-owner"]


@pytest.mark.integration
def test_cli_query_empty_result_is_success(qws: Path, monkeypatch) -> None:
    """TS-QRY-001-03 (AC-003): no match → exit 0, `[]` under json, message on a TTY."""
    monkeypatch.chdir(qws)
    js = runner.invoke(app, ["query", "--type", "opportunity", "--stage", "lost", "--format", "json"])
    assert js.exit_code == 0 and json.loads(js.output) == []
    tty = runner.invoke(app, ["query", "--type", "opportunity", "--stage", "lost"], env={"FORCE_COLOR": "1"})
    assert tty.exit_code == 0 and "No entities match" in tty.output


@pytest.mark.integration
def test_cli_query_unknown_field(qws: Path, monkeypatch) -> None:
    """TS-QRY-001-04 (AC-004): an undeclared filter field reports the located error."""
    monkeypatch.chdir(qws)
    out = runner.invoke(app, ["query", "--type", "opportunity", "--vibe", "high"])
    assert out.exit_code == 1
    assert "No field 'vibe' on type 'opportunity'" in out.output


@pytest.mark.integration
def test_cli_query_format_ids(qws: Path, monkeypatch) -> None:
    """TS-QRY-001-U04 (REQ-QRY001-04): `--format ids` emits bare ids, one per line."""
    monkeypatch.chdir(qws)
    out = runner.invoke(app, ["query", "--type", "opportunity", "--format", "ids"])
    assert out.exit_code == 0
    assert set(out.output.split()) == {"op-prospect", "op-proposal", "op-won"}
