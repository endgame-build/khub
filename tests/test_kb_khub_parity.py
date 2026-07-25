"""kb is a behavioural subset of khub: the same command over the same corpus agrees.

The point of a separate implementation is that a corpus graduates. That only holds
if the two tools *behave* the same, not merely if they read the same schema — so
this builds one corpus, runs both CLIs over it, and diffs their JSON.

Where they legitimately differ, the difference is named here rather than left to be
discovered: kb has no `draft` write path, no git-derived dates, and no collections,
so `stale` and the `draft` count are structurally zero in a kb-authored corpus.
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import sys
from pathlib import Path
from typing import Any

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.workspace import init_workspace

SCRIPTS = Path(__file__).resolve().parents[1] / "kb" / "scripts"
runner = CliRunner()


@pytest.fixture(scope="module")
def kb() -> Any:
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("kb_parity", SCRIPTS / "kb.py")
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules["kb_parity"] = module
    spec.loader.exec_module(module)
    return module


def kb_json(kb: Any, root: Path, *argv: str) -> Any:
    out = io.StringIO()
    with contextlib.redirect_stdout(out):
        code = kb.main(["-C", str(root), *argv, "--format", "json"])
    return json.loads(out.getvalue()), code


def khub_json(root: Path, *argv: str) -> Any:
    result = runner.invoke(app, ["-C", str(root), *argv, "--format", "json"])
    return json.loads(result.output), result.exit_code


@pytest.fixture
def corpus(kb: Any, tmp_path: Path) -> Path:
    """One workspace, authored through kb, then read by both tools."""
    root = tmp_path / "ws"
    root.mkdir()
    init_workspace("build-lite", root)
    with contextlib.redirect_stdout(io.StringIO()):
        for argv in (
            ["add", "component", "--title", "Public API", "--kind", "service"],
            ["add", "component", "--title", "Stripe", "--kind", "external"],
            ["add", "requirement", "--title", "Pay by card", "--kind", "functional"],
            ["add", "requirement", "--title", "Settle in 2s", "--kind", "constraint"],
            ["add", "adr", "--title", "Use Stripe", "--status", "accepted",
             "--affects", "cmp-001-public-api"],
            ["add", "feature-spec", "--title", "Checkout", "--status", "active",
             "--requirements", "fr-001-pay-by-card"],
            ["link", "fr-001-pay-by-card", "realized_in", "cmp-001-public-api"],
            ["link", "cst-001-settle-in-2s", "realized_in", "cmp-001-public-api"],
            ["link", "cmp-001-public-api", "depends_on", "cmp-002-stripe"],
        ):
            assert kb.main(["-C", str(root), *argv]) == 0, argv
    return root


# ------------------------------------------------------------------- the gates


@pytest.mark.e2e
def test_validate_agrees(kb: Any, corpus: Path) -> None:
    ours, our_code = kb_json(kb, corpus, "validate")
    theirs, their_code = khub_json(corpus, "validate")
    assert (ours, our_code) == (theirs, their_code)


@pytest.mark.e2e
def test_check_agrees(kb: Any, corpus: Path) -> None:
    for extra in ([], ["--strict"]):
        ours, our_code = kb_json(kb, corpus, "check", *extra)
        theirs, their_code = khub_json(corpus, "check", *extra)
        assert (ours, our_code) == (theirs, their_code), extra


@pytest.mark.e2e
def test_both_gates_agree_on_a_broken_corpus(kb: Any, corpus: Path) -> None:
    """A dangling edge, a bad enum and a stray file: same findings, same exit codes."""
    (corpus / "knowledge" / "decisions" / "ad-002-dangling.md").write_text(
        "---\ntype: adr\ntitle: Dangling\nstatus: proposed\ncreated: 2026-07-25\n"
        "supersedes: ad-999-ghost\n---\n"
    )
    (corpus / "knowledge" / "components" / "cmp-003-bad.md").write_text(
        "---\ntype: component\ntitle: Bad kind\nkind: nope\ncreated: 2026-07-25\n---\n"
    )
    (corpus / "specs" / "notes.txt").write_text("not an entity\n")

    ours, our_code = kb_json(kb, corpus, "validate")
    theirs, their_code = khub_json(corpus, "validate")
    assert our_code == their_code == 1
    assert {(e["id"], e["field"]) for e in ours["errors"]} == {
        (e["id"], e["field"]) for e in theirs["errors"]
    }

    ours, our_code = kb_json(kb, corpus, "check")
    theirs, their_code = khub_json(corpus, "check")
    assert our_code == their_code == 1
    for key in ("passed", "strays", "orphans", "cycles"):
        assert ours[key] == theirs[key], key
    assert {(d["id"], d["predicate"], d["target"]) for d in ours["dangling"]} == {
        (d["id"], d["predicate"], d["target"]) for d in theirs["dangling"]
    }


@pytest.mark.e2e
def test_an_undeclared_key_passes_both_and_fails_both_under_strict(
    kb: Any, corpus: Path
) -> None:
    """khub's schema is open. kb's used to be closed, which rejected khub's own writes."""
    path = corpus / "knowledge" / "components" / "cmp-001-public-api.md"
    path.write_text(path.read_text().replace("kind: service", "kind: service\nowner: noor"))

    assert kb_json(kb, corpus, "validate")[1] == khub_json(corpus, "validate")[1] == 0
    ours, our_code = kb_json(kb, corpus, "validate", "--strict")
    theirs, their_code = khub_json(corpus, "validate", "--strict")
    assert our_code == their_code == 1
    assert {e["id"] for e in ours["errors"]} == {e["id"] for e in theirs["errors"]}


# -------------------------------------------------------------------- the reads


@pytest.mark.e2e
def test_query_agrees_on_identity(kb: Any, corpus: Path) -> None:
    """The uniform record: qualified id, type, slug — for every entity, both tools."""
    ours, _ = kb_json(kb, corpus, "query")
    theirs, _ = khub_json(corpus, "query")
    assert [(r["id"], r["type"], r["slug"]) for r in ours] == [
        (r["id"], r["type"], r["slug"]) for r in theirs
    ]


@pytest.mark.e2e
def test_search_ranks_identically(kb: Any, corpus: Path) -> None:
    """Same FTS5 table, same MATCH, same bm25 ordering — so the ranking is the
    same computation, not merely a similar one."""
    for term in ("stripe", "settle", "api OR checkout"):
        ours, _ = kb_json(kb, corpus, "search", term)
        theirs, _ = khub_json(corpus, "search", term)
        assert [r["id"] for r in ours] == [r["id"] for r in theirs], term
        assert [round(r["score"], 6) for r in ours] == [
            round(r["score"], 6) for r in theirs
        ], term


@pytest.mark.e2e
def test_status_reports_the_same_shape_and_counts(kb: Any, corpus: Path) -> None:
    ours, _ = kb_json(kb, corpus, "status")
    theirs, _ = khub_json(corpus, "status")
    assert set(ours) == set(theirs)
    for key in ("counts", "total", "draft", "active", "orphan", "stray", "malformed",
                "okf_conformant"):
        assert ours[key] == theirs[key], key


@pytest.mark.e2e
def test_schema_types_agree(kb: Any, corpus: Path) -> None:
    ours, _ = kb_json(kb, corpus, "schema", "types")
    theirs, _ = khub_json(corpus, "schema", "types")
    assert ours == theirs


# -------------------------------------------------------------------- writes


@pytest.mark.e2e
def test_reindex_writes_the_same_index(kb: Any, corpus: Path) -> None:
    """OKF index.md: same stamp, same grouping, same cross-links."""
    with contextlib.redirect_stdout(io.StringIO()):
        kb.main(["-C", str(corpus), "reindex"])
    ours = (corpus / "index.md").read_text()

    (corpus / "index.md").unlink()
    runner.invoke(app, ["-C", str(corpus), "reindex"])
    theirs = (corpus / "index.md").read_text()
    assert ours == theirs
