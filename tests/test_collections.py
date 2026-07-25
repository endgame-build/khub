"""TS-COL-001 — Single-File Collections (layout: collection, docs/collections-design.md).

One file holds every entity of a type as a row. Covers the document shapes
(jsonl slug-key rows; yaml/json mappings keyed by slug), the locked
read-modify-write path (create/mint/edit/link/delete with sibling-row byte
stability), whole-file malformed containment with write refusal and derivative
dangling suppression, stray rows by locator, the row locator in JSON records,
row-correct `log`, the stale/backfill git rules, init scaffolding, and the
schema matrix (format×layout).
"""

from __future__ import annotations

import json
import subprocess
from collections.abc import Callable
from datetime import date
from pathlib import Path
from typing import Any

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.errors import LocatedError
from khub.core.formats import dump_collection, load_collection, render_row
from khub.core.resolve import load_yaml
from khub.core.workspace import _dump_yaml, init_workspace

runner = CliRunner()
Seed = Callable[..., None]


def _edit_schema(ws: Path, mutate: Callable[[dict], None]) -> None:
    sp = ws / ".khub" / "schema.yaml"
    data = load_yaml(sp)
    mutate(data)
    sp.write_text(_dump_yaml(data))


def _add_repo_type(ws: Path, fmt: str = "jsonl", path: str | None = None) -> None:
    """A `repo` collection type, plus a project→repo edge for suppression tests."""

    def mutate(data: dict) -> None:
        decl: dict[str, Any] = {
            "layout": "collection",
            "format": fmt,
            "attributes": {
                "repo": {"type": "text", "required": True},
                "status": {"enum": ["active", "archived"]},
            },
            "relations": {"project": {"to": "project"}},
        }
        if path:
            decl["path"] = path
        data["entities"]["repo"] = decl
        data["entities"]["project"]["relations"]["code"] = {"to": "repo"}

    _edit_schema(ws, mutate)


@pytest.fixture
def cws(fresh_ws: Path) -> Path:
    _add_repo_type(fresh_ws)
    return fresh_ws


# --- unit: document shapes ------------------------------------------------------


@pytest.mark.unit
@pytest.mark.parametrize("fmt", ["jsonl", "yaml", "json"])
def test_collection_roundtrip(fmt: str) -> None:
    rows = {"acme": {"repo": "endgame-build/acme", "status": "active"}}
    text = dump_collection(rows, fmt)
    assert load_collection(text, fmt) == rows
    assert load_collection("", fmt) == {}  # empty file = zero rows, never malformed


@pytest.mark.unit
def test_jsonl_duplicate_slug_is_malformed() -> None:
    text = '{"slug": "a", "x": 1}\n{"slug": "a", "x": 2}\n'
    with pytest.raises(LocatedError) as err:
        load_collection(text, "jsonl")
    assert "duplicate slug" in err.value.message


@pytest.mark.unit
def test_yaml_duplicate_key_is_malformed() -> None:
    from ruamel.yaml.constructor import DuplicateKeyError

    with pytest.raises(DuplicateKeyError):  # the whole-file contract, named
        load_collection("a:\n  x: 1\na:\n  x: 2\n", "yaml")


@pytest.mark.unit
def test_json_duplicate_key_is_malformed() -> None:
    """json.loads last-wins by default — the loader must refuse, matching yaml."""
    with pytest.raises(LocatedError) as err:
        load_collection('{"a": {"x": 1}, "a": {"x": 2}}', "json")
    assert "duplicate key" in err.value.message


@pytest.mark.unit
def test_agreeing_inner_slug_never_reaches_meta() -> None:
    """A mapping row carrying its own agreeing slug key sheds it (parity with jsonl)."""
    rows = load_collection("acme:\n  slug: acme\n  name: Acme\n", "yaml")
    assert "slug" not in rows["acme"]


@pytest.mark.unit
def test_row_shape_violations_are_malformed() -> None:
    with pytest.raises(LocatedError):  # jsonl row without slug
        load_collection('{"x": 1}\n', "jsonl")
    with pytest.raises(LocatedError):  # non-mapping row value
        load_collection("a: [1, 2]\n", "yaml")
    with pytest.raises(LocatedError):  # disagreeing inner slug key
        load_collection('{"a": {"slug": "b"}}', "json")
    with pytest.raises(LocatedError):  # non-string body in a row
        load_collection('{"slug": "a", "body": 5}\n', "jsonl")


@pytest.mark.unit
def test_jsonl_untouched_rows_byte_identical() -> None:
    text = dump_collection(
        {"a": {"repo": "endgame-build/a"}, "b": {"repo": "endgame-build/b"}}, "jsonl"
    )
    rows = load_collection(text, "jsonl")
    rows["b"]["status"] = "active"
    line_a = text.splitlines()[0]
    assert dump_collection(rows, "jsonl").splitlines()[0] == line_a


@pytest.mark.unit
def test_yaml_comments_survive_row_mutation() -> None:
    text = "acme:  # the flagship\n  repo: endgame-build/acme\nother:\n  repo: endgame-build/o\n"
    rows = load_collection(text, "yaml")
    rows["other"]["status"] = "active"
    out = dump_collection(rows, "yaml")
    assert "# the flagship" in out


@pytest.mark.unit
def test_render_row_is_one_row() -> None:
    assert render_row("a", {"x": 1}, "jsonl") == '{"slug": "a", "x": 1}\n'
    assert "a:" in render_row("a", {"x": 1}, "yaml") and "x: 1" in render_row("a", {"x": 1}, "yaml")


# --- e2e / integration: verbs over a live collection ----------------------------


@pytest.mark.e2e
def test_collection_full_verb_roundtrip(
    cws: Path, seed: Seed, monkeypatch: pytest.MonkeyPatch
) -> None:
    """add creates the file; get/edit/link/remove operate on rows; locator emitted."""
    monkeypatch.chdir(cws)
    seed(cws, "projects/demo/_index.md", type="project", updated=date(2026, 6, 1),
         created=date(2026, 6, 1))
    assert not (cws / "repo.jsonl").exists()

    out = runner.invoke(app, ["add", "repo", "--id", "acme", "--repo", "endgame-build/acme",
                              "--status", "active", "--project", "demo",
                              "--body", "row prose", "--format", "json"])
    assert out.exit_code == 0, out.output
    record = json.loads(out.output)
    assert record["path"] == "repo.jsonl" and record["locator"] == "repo.jsonl#acme"

    stored = load_collection((cws / "repo.jsonl").read_text(), "jsonl")
    assert stored["acme"]["body"] == "row prose" and "type" not in stored["acme"]

    got = json.loads(runner.invoke(app, ["get", "acme", "--format", "json"]).output)
    assert got["locator"] == "repo.jsonl#acme" and got["body"] == "row prose"
    assert got["frontmatter"]["type"] == "repo" and "body" not in got["frontmatter"]

    raw = runner.invoke(app, ["get", "acme", "--format", "raw"]).output
    assert raw.startswith('{"slug": "acme"') and "\n" not in raw.strip()  # the row, not the file

    # a second add mints past the taken slug; explicit --id collision refuses
    minted = json.loads(runner.invoke(app, ["add", "repo", "--repo", "endgame-build/x",
                                            "--status", "active", "--format", "json"]).output)
    assert minted["slug"] == "repo"
    dup = runner.invoke(app, ["add", "repo", "--id", "acme", "--repo", "endgame-build/y",
                              "--status", "active"])
    assert dup.exit_code == 1 and "already taken" in dup.output

    # edit one row; the sibling row's line stays byte-identical
    before = {ln.split('"slug": "')[1].split('"')[0]: ln
              for ln in (cws / "repo.jsonl").read_text().splitlines()}
    assert runner.invoke(app, ["edit", "acme", "status", "archived"]).exit_code == 0
    after = {ln.split('"slug": "')[1].split('"')[0]: ln
             for ln in (cws / "repo.jsonl").read_text().splitlines()}
    assert after["repo"] == before["repo"] and after["acme"] != before["acme"]

    # idempotent no-op unlink leaves the file bytes untouched
    text_before = (cws / "repo.jsonl").read_text()
    noop = runner.invoke(app, ["unlink", "acme", "related", "demo"])
    assert noop.exit_code == 0 and (cws / "repo.jsonl").read_text() == text_before

    # remove refuses while an inbound edge resolves; --force removes the row only
    assert runner.invoke(app, ["link", "demo", "code", "repo/acme"]).exit_code == 0
    refused = runner.invoke(app, ["remove", "repo/acme"])
    assert refused.exit_code == 1 and "inbound" in refused.output
    removed = runner.invoke(app, ["remove", "repo/acme", "--force"])
    assert removed.exit_code == 0
    left = load_collection((cws / "repo.jsonl").read_text(), "jsonl")
    assert "acme" not in left and "repo" in left  # file survives with the other row


@pytest.mark.integration
def test_malformed_collection_contained_and_write_refused(
    cws: Path, seed: Seed, monkeypatch: pytest.MonkeyPatch
) -> None:
    """A bad row makes the WHOLE file malformed: no rows load, writes refuse,
    derivative dangling reports are suppressed into the malformed finding."""
    monkeypatch.chdir(cws)
    seed(cws, "projects/demo/_index.md", type="project", updated=date(2026, 6, 1),
         created=date(2026, 6, 1), code="ghost-repo")
    (cws / "repo.jsonl").write_text('{"slug": "ok", "repo": "endgame-build/ok"}\nnot json\n')

    checked = json.loads(runner.invoke(app, ["check", "--format", "json"]).output)
    assert "repo.jsonl" in checked["malformed"]
    assert checked["dangling"] == [] and checked["suppressed_dangling"] == 1
    empty = runner.invoke(app, ["query", "--type", "repo", "--format", "json"])
    assert json.loads(empty.output) == []

    blocked = runner.invoke(app, ["add", "repo", "--id", "x", "--repo", "endgame-build/x"])
    assert blocked.exit_code == 1 and "Refusing to write" in blocked.output

    # validate targeted at a row of the broken type reports the malformed file,
    # never the "target is a typo" misdiagnosis
    targeted = runner.invoke(app, ["validate", "repo/ok", "--format", "json"])
    payload = json.loads(targeted.output)
    assert any("repo.jsonl" in e["reason"] for e in payload["errors"])


@pytest.mark.integration
def test_stray_row_reported_by_locator(cws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """A row whose `type` disagrees with the binding is a stray, addressed path#slug."""
    monkeypatch.chdir(cws)
    (cws / "repo.jsonl").write_text(
        '{"slug": "good", "repo": "endgame-build/g"}\n'
        '{"slug": "impostor", "type": "client", "repo": "endgame-build/i"}\n'
    )
    checked = json.loads(runner.invoke(app, ["check", "--format", "json"]).output)
    assert checked["strays"] == ["repo.jsonl#impostor"]
    rows = json.loads(runner.invoke(app, ["query", "--type", "repo", "--format", "json"]).output)
    assert [r["slug"] for r in rows] == ["good"]


@pytest.mark.integration
def test_collection_search_and_reindex(cws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """Multi-row collections are searchable per row (jsonl regression: 2+ rows
    must not crash the FTS build, and each row indexes its own body/fields)."""
    monkeypatch.chdir(cws)
    runner.invoke(app, ["add", "repo", "--id", "acme", "--repo", "endgame-build/acme",
                        "--status", "active", "--body", "flagship mainframe estate"])
    runner.invoke(app, ["add", "repo", "--id", "beta", "--repo", "endgame-build/beta",
                        "--status", "active", "--body", "skunkworks zeppelin lab"])
    hits = json.loads(runner.invoke(app, ["search", "mainframe", "--format", "json"]).output)
    assert [h["id"] for h in hits] == ["repo/acme"] and hits[0]["path"] == "repo.jsonl"
    assert hits[0]["locator"] == "repo.jsonl#acme"
    other = json.loads(runner.invoke(app, ["search", "zeppelin", "--format", "json"]).output)
    assert [h["id"] for h in other] == ["repo/beta"]
    by_field = json.loads(runner.invoke(app, ["search", '"endgame-build/acme"',
                                              "--format", "json"]).output)
    assert [h["id"] for h in by_field] == ["repo/acme"]
    assert runner.invoke(app, ["reindex"]).exit_code == 0
    assert "- [acme](repo.jsonl)" in (cws / "index.md").read_text()


@pytest.mark.integration
def test_yaml_collection_rows_are_searchable(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """yaml collections index each row's body — never the whole mapping as one doc."""
    _add_repo_type(fresh_ws, fmt="yaml")
    monkeypatch.chdir(fresh_ws)
    runner.invoke(app, ["add", "repo", "--id", "acme", "--repo", "endgame-build/acme",
                        "--body", "unique dirigible prose"])
    runner.invoke(app, ["add", "repo", "--id", "beta", "--repo", "endgame-build/beta"])
    hits = json.loads(runner.invoke(app, ["search", "dirigible", "--format", "json"]).output)
    assert [h["id"] for h in hits] == ["repo/acme"]


@pytest.mark.integration
def test_collection_backfill_skips_and_stale_never_lies(
    cws: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.chdir(cws)
    # a hand-authored row with no dates at all
    (cws / "repo.jsonl").write_text('{"slug": "olde", "repo": "endgame-build/olde"}\n')
    subprocess.run(["git", "init", "-q", "."], check=True)
    subprocess.run(["git", "add", "-A"], check=True)
    subprocess.run(["git", "-c", "user.email=t@t", "-c", "user.name=t",
                    "commit", "-qm", "seed"], check=True)
    out = runner.invoke(app, ["backfill"])
    assert "Skipped collection types (repo)" in out.output
    assert "created" not in (cws / "repo.jsonl").read_text()  # no file-date attribution
    stale = json.loads(runner.invoke(app, ["stale", "--days", "0", "--format", "json"]).output)
    assert all(e["id"] != "repo/olde" for e in stale)  # undated row skipped, not aged


@pytest.mark.integration
def test_init_scaffolds_no_collection_file(tmp_path: Path) -> None:
    """init creates a collection's parent dir only — never the file, never a dir named like it."""
    preset = tmp_path / "presets"
    (preset / "mini").mkdir(parents=True)
    preset.joinpath("mini", "schema.yaml").write_text(
        "version: 0.1.0\n"
        "entities:\n"
        "  note: { layout: file, path: notes }\n"
        "  repo: { layout: collection, path: data/repos.jsonl }\n"
    )
    ws = tmp_path / "ws"
    init_workspace("mini", ws, preset_source=preset)
    assert (ws / "data").is_dir()
    assert not (ws / "data" / "repos.jsonl").exists()
    # locks live under .khub/generated/ — covered by the one existing ignore entry
    assert ".khub/generated/" in (ws / ".gitignore").read_text()


@pytest.mark.unit
def test_schema_matrix(cws: Path) -> None:
    """collection×md rejected; formatless collection rejected; suffix drives format."""
    from khub.core.introspect import load_schema

    def set_repo(**decl: Any) -> None:
        _edit_schema(cws, lambda d: d["entities"].__setitem__("repo", decl))

    set_repo(layout="collection", format="md", path="repo.md")
    with pytest.raises(LocatedError):
        load_schema(cws)
    set_repo(layout="collection", format="md", path="repo.yaml")  # explicit md never rebinds
    with pytest.raises(LocatedError):
        load_schema(cws)
    set_repo(layout="collection")  # no format, no path suffix
    with pytest.raises(LocatedError):
        load_schema(cws)
    set_repo(layout="collection", format="yaml", path="repo.jsonl")  # disagreement
    with pytest.raises(LocatedError):
        load_schema(cws)
    set_repo(layout="collection", format="jsonl",
             attributes={"slug": {"type": "text"}})  # reserved row key
    with pytest.raises(LocatedError):
        load_schema(cws)
    set_repo(layout="collection", path="stuff/repo.yaml")  # format derived from suffix
    assert load_schema(cws).types["repo"].storage.fmt == "yaml"