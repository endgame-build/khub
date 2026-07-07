"""TS-SRCH-001 — Full-Text Search over Title and Body (FS-003 fast-follow, FTS5).

Covers body-prose matching (the sanctioned exception to QRY-001), title
matching, BM25 ranking, `--type` narrowing (unknown type is a located error),
`--limit`, empty-result success, the malformed-MATCH located error, `--format
ids`, the JSON record contract, and crash containment on a malformed entity
file. The projection is in-memory per invocation — there is no cache file to
assert on, only results.
"""

from __future__ import annotations

import json
from datetime import date
from pathlib import Path
from typing import Callable

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.errors import LocatedError
from khub.core.search import search

runner = CliRunner()
Seed = Callable[..., None]


@pytest.fixture
def sws(fresh_ws: Path, seed: Seed) -> Path:
    """A firm-ops workspace with distinctive prose in file- and folder-layout bodies."""
    seed(fresh_ws, "clients/initech.md", type="client", name="Initech",
         created=date(2026, 6, 1))
    # file layout with a title and a body-only word ("mainframe" lives nowhere else)
    (fresh_ws / "clients" / "acme.md").write_text(
        "---\ntype: client\nname: Acme\ntitle: Acme Corp\ncreated: 2026-06-01\n---\n"
        "Legacy assessment of the mainframe estate before any rewrite.\n"
    )
    # folder layout, term-dense for the ranking test
    op = fresh_ws / "opportunities" / "op-modern"
    op.mkdir(parents=True)
    (op / "_index.md").write_text(
        "---\ntype: opportunity\nstage: prospect\ncreated: 2026-06-01\n---\n"
        "Modernization, modernization, modernization.\n"
    )
    # mentions the same term once, in a longer body (should rank below op-modern)
    (fresh_ws / "clients" / "sparse.md").write_text(
        "---\ntype: client\nname: Sparse\ncreated: 2026-06-01\n---\n"
        "A long engagement note that mentions modernization exactly once while "
        "otherwise talking about invoicing, staffing, travel, and scheduling.\n"
    )
    # a malformed file (unterminated frontmatter) must not brick the search
    (fresh_ws / "clients" / "broken.md").write_text("---\ntype: client\nunclosed: [\n")
    return fresh_ws


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_search_matches_body_prose(sws: Path) -> None:
    """A body-only word finds its entity — the sanctioned exception to QRY-001."""
    hits = search(sws, "mainframe")
    assert [(h.type, h.slug) for h in hits] == [("client", "acme")]
    assert hits[0].title == "Acme Corp"
    assert hits[0].path == "clients/acme.md"
    assert "mainframe" in hits[0].snippet


@pytest.mark.unit
def test_search_matches_title(sws: Path) -> None:
    """A title-only word matches; the title column is indexed alongside the body."""
    assert [h.slug for h in search(sws, "corp")] == ["acme"]


@pytest.mark.unit
def test_search_ranks_denser_match_first(sws: Path) -> None:
    """BM25 puts the term-dense folder-layout body above the one-mention body."""
    hits = search(sws, "modernization")
    assert [h.slug for h in hits] == ["op-modern", "sparse"]
    assert hits[0].score <= hits[1].score  # lower BM25 = better


@pytest.mark.unit
def test_search_type_filter(sws: Path) -> None:
    """`--type` narrows the hits; an unknown type is the same located error as query."""
    hits = search(sws, "modernization", type_="client")
    assert [h.slug for h in hits] == ["sparse"]
    with pytest.raises(LocatedError) as err:
        search(sws, "modernization", type_="zzz")
    assert err.value.code == "unknown_type"


@pytest.mark.unit
def test_search_limit(sws: Path) -> None:
    assert len(search(sws, "modernization", limit=1)) == 1
    # a zero/negative cap returns nothing — SQLite's "LIMIT -1 = unbounded" never leaks
    assert search(sws, "modernization", limit=0) == []
    assert search(sws, "modernization", limit=-1) == []


@pytest.mark.unit
def test_search_empty_is_success(sws: Path) -> None:
    """No match returns an empty collection, never raising (QRY-002)."""
    assert search(sws, "chrysanthemum") == []


@pytest.mark.unit
def test_search_bad_match_syntax_is_located(sws: Path) -> None:
    """A malformed FTS5 expression raises a located error, not an OperationalError."""
    with pytest.raises(LocatedError) as err:
        search(sws, 'mainframe AND "')
    assert err.value.code == "bad_search_query"


@pytest.mark.unit
def test_search_survives_malformed_file(sws: Path) -> None:
    """The malformed clients/broken.md is not an entity: skipped, never indexed."""
    assert all(h.slug != "broken" for h in search(sws, "unclosed OR client OR mainframe"))


# --- integration / e2e ---------------------------------------------------------


@pytest.mark.e2e
def test_cli_search_json_contract(sws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """The JSON record carries the uniform id/type/slug plus title/score/snippet/path."""
    monkeypatch.chdir(sws)
    out = runner.invoke(app, ["search", "mainframe", "--format", "json"])
    assert out.exit_code == 0, out.output
    data = json.loads(out.output)
    assert [d["id"] for d in data] == ["client/acme"]
    assert set(data[0]) == {"id", "type", "slug", "title", "score", "snippet", "path"}
    assert data[0]["type"] == "client" and data[0]["slug"] == "acme"


@pytest.mark.integration
def test_cli_search_format_ids(sws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """`--format ids` emits bare slugs, one per line (parity with query)."""
    monkeypatch.chdir(sws)
    out = runner.invoke(app, ["search", "modernization", "--format", "ids"])
    assert out.exit_code == 0
    assert out.output.split() == ["op-modern", "sparse"]


@pytest.mark.integration
def test_cli_search_empty_result_is_success(sws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """No match → exit 0, `[]` under json, message on a TTY."""
    monkeypatch.chdir(sws)
    js = runner.invoke(app, ["search", "chrysanthemum", "--format", "json"])
    assert js.exit_code == 0 and json.loads(js.output) == []
    tty = runner.invoke(app, ["search", "chrysanthemum"], env={"FORCE_COLOR": "1"})
    assert tty.exit_code == 0 and "No entities match" in tty.output


@pytest.mark.integration
def test_cli_search_bad_syntax(sws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """A malformed MATCH expression exits 1 with the located message, no traceback."""
    monkeypatch.chdir(sws)
    out = runner.invoke(app, ["search", 'mainframe AND "'])
    assert out.exit_code == 1
    assert "Invalid search query" in out.output and "Traceback" not in out.output


@pytest.mark.integration
def test_cli_search_unknown_type(sws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(sws)
    out = runner.invoke(app, ["search", "mainframe", "--type", "zzz"])
    assert out.exit_code == 1
    assert "No type 'zzz'" in out.output
