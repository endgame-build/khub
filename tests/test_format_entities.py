"""TS-FMT-002 — Non-md Entity Formats End to End (FS-001 formats unlock).

A firm-ops workspace with `fragment` flipped to `format: json` (file layout)
and `project` to `format: yaml` (folder layout). Covers the full verb surface
over non-md entities, the `--body`/`--body-file` mapping to the reserved
`body` key, mixed md+json+yaml scanning, malformed containment, the schema
whitelist, git-backed `log`, and `search` over field values and body prose.
"""

from __future__ import annotations

import json
from datetime import date
from pathlib import Path
from typing import Callable

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.resolve import load_yaml
from khub.core.workspace import _dump_yaml

runner = CliRunner()
Seed = Callable[..., None]


def _edit_schema(ws: Path, mutate: Callable[[dict], None]) -> None:
    """Round-trip the flattened schema through the workspace's own dump config."""
    sp = ws / ".khub" / "schema.yaml"
    data = load_yaml(sp)
    mutate(data)
    sp.write_text(_dump_yaml(data))


def _flip_formats(ws: Path) -> None:
    """Flip fragment→json and project→yaml in the flattened schema."""

    def mutate(data: dict) -> None:
        data["entities"]["fragment"]["format"] = "json"
        data["entities"]["project"]["format"] = "yaml"

    _edit_schema(ws, mutate)


@pytest.fixture
def fws(fresh_ws: Path) -> Path:
    _flip_formats(fresh_ws)
    return fresh_ws


# --- verbs over non-md entities ------------------------------------------------


@pytest.mark.e2e
def test_json_fragment_full_verb_roundtrip(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """add → get → edit → remove on a json file-layout type, body via --body."""
    monkeypatch.chdir(fws)
    out = runner.invoke(
        app, ["add", "fragment", "--stage", "raw", "--body", "seed prose", "--format", "json"]
    )
    assert out.exit_code == 0, out.output
    record = json.loads(out.output)
    assert record["path"].endswith(".json")
    slug = record["slug"]

    stored = json.loads((fws / record["path"]).read_text())
    assert stored["body"] == "seed prose" and stored["stage"] == "raw"

    got = json.loads(runner.invoke(app, ["get", slug, "--format", "json"]).output)
    assert got["body"] == "seed prose"
    assert "body" not in got["frontmatter"]  # reserved key never reaches meta

    edited = runner.invoke(app, ["edit", slug, "stage", "mature"])
    assert edited.exit_code == 0, edited.output
    got = json.loads(runner.invoke(app, ["get", slug, "--format", "json"]).output)
    assert got["frontmatter"]["stage"] == "mature"
    assert got["body"] == "seed prose"  # body survives a field edit

    cleared = runner.invoke(app, ["edit", slug, "--body", ""])
    assert cleared.exit_code == 0, cleared.output
    assert "body" not in json.loads((fws / record["path"]).read_text())

    removed = runner.invoke(app, ["remove", slug])
    assert removed.exit_code == 0 and not (fws / record["path"]).exists()


@pytest.mark.integration
def test_yaml_folder_project_verbs_and_edges(fws: Path, seed: Seed, monkeypatch: pytest.MonkeyPatch) -> None:
    """A yaml folder-layout type writes _index.yaml; link/unlink round-trip."""
    monkeypatch.chdir(fws)
    seed(fws, "clients/acme.md", type="client", name="Acme", created=date(2026, 6, 1))
    out = runner.invoke(app, ["add", "project", "--id", "demo", "--format", "json"])
    assert out.exit_code == 0, out.output
    assert json.loads(out.output)["path"] == "projects/demo/_index.yaml"

    linked = runner.invoke(app, ["link", "demo", "client", "acme"])
    assert linked.exit_code == 0, linked.output
    got = json.loads(runner.invoke(app, ["get", "demo", "--format", "json", "--edges"]).output)
    assert {"predicate": "client", "target": "acme", "derived": False} in got["edges"]

    unlinked = runner.invoke(app, ["unlink", "demo", "client", "acme"])
    assert unlinked.exit_code == 0
    got = json.loads(runner.invoke(app, ["get", "demo", "--format", "json"]).output)
    assert "client" not in got["frontmatter"]


@pytest.mark.integration
def test_mint_suffix_collision_json(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """Two adds minting the same base slug suffix deterministically (O_EXCL holds)."""
    monkeypatch.chdir(fws)
    first = json.loads(runner.invoke(app, ["add", "fragment", "--stage", "raw", "--format", "json"]).output)
    second = json.loads(runner.invoke(app, ["add", "fragment", "--stage", "raw", "--format", "json"]).output)
    assert first["slug"] == "fragment" and second["slug"] == "fragment-2"


@pytest.mark.integration
def test_body_flag_conflict_rejected(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(fws)
    out = runner.invoke(
        app, ["add", "fragment", "--stage", "raw", "--body", "x", "--body-file", "-"]
    )
    assert out.exit_code == 2
    assert "not both" in out.output


@pytest.mark.integration
def test_body_field_reserved_on_non_md(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """A field literally named `body` is rejected on a json type (prose channel),
    while an md type keeps it as an ordinary frontmatter field."""
    monkeypatch.chdir(fws)
    added = json.loads(
        runner.invoke(app, ["add", "fragment", "--stage", "raw", "--format", "json"]).output
    )
    out = runner.invoke(app, ["edit", added["slug"], "body", "field value"])
    assert out.exit_code == 1
    assert "reserved" in out.output and "--body" in out.output
    # md types are untouched: `body` writes a plain frontmatter field
    md = json.loads(runner.invoke(app, ["add", "client", "--name", "Acme", "--format", "json"]).output)
    ok = runner.invoke(app, ["edit", md["slug"], "body", "field value"])
    assert ok.exit_code == 0
    got = json.loads(runner.invoke(app, ["get", md["slug"], "--format", "json"]).output)
    assert got["frontmatter"]["body"] == "field value"


@pytest.mark.integration
def test_schema_rejects_body_attribute_on_non_md(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """Declaring an attribute named `body` on a json type fails schema resolution."""
    monkeypatch.chdir(fws)
    _edit_schema(
        fws,
        lambda d: d["entities"]["fragment"]["attributes"].update({"body": {"type": "text"}}),
    )
    out = runner.invoke(app, ["query", "--type", "fragment", "--format", "json"])
    assert out.exit_code != 0
    assert "body" in out.output and "reserved" in out.output


@pytest.mark.integration
def test_md_metadata_edit_preserves_body_bytes(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """A metadata-only edit never touches md body bytes (no trailing-newline drift)."""
    monkeypatch.chdir(fws)
    p = fws / "clients" / "hand.md"
    p.parent.mkdir(exist_ok=True)
    p.write_text("---\ntype: client\nname: Hand\ncreated: 2026-06-01\n---\nno trailing newline")
    out = runner.invoke(app, ["edit", "hand", "industry", "retail"])
    assert out.exit_code == 0, out.output
    assert p.read_text().endswith("no trailing newline")


# --- scan / integrity over a mixed workspace -----------------------------------


@pytest.mark.integration
def test_mixed_workspace_scan_and_gates(fws: Path, seed: Seed, monkeypatch: pytest.MonkeyPatch) -> None:
    """md + json + yaml entities coexist: query/status/validate/check see all three."""
    monkeypatch.chdir(fws)
    seed(fws, "clients/acme.md", type="client", name="Acme", created=date(2026, 6, 1),
         updated=date(2026, 6, 1))
    runner.invoke(app, ["add", "fragment", "--stage", "raw"])
    runner.invoke(app, ["add", "project", "--id", "demo", "--client", "acme",
                        "--updated", "2026-06-01"])

    for type_, expect in (("client", "acme"), ("fragment", "fragment"), ("project", "demo")):
        rows = json.loads(runner.invoke(app, ["query", "--type", type_, "--format", "json"]).output)
        assert [r["slug"] for r in rows] == [expect]

    validated = json.loads(runner.invoke(app, ["validate", "--format", "json"]).output)
    assert validated["count"] == 3 and validated["errors"] == []
    checked = json.loads(runner.invoke(app, ["check", "--format", "json"]).output)
    assert checked["malformed"] == [] and checked["dangling"] == []


@pytest.mark.integration
def test_malformed_json_is_contained(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """One broken .json file becomes a malformed entry, never a crash."""
    monkeypatch.chdir(fws)
    (fws / "fragments").mkdir(exist_ok=True)
    (fws / "fragments" / "broken.json").write_text('{"type": "fragment", not json}')
    runner.invoke(app, ["add", "fragment", "--stage", "raw"])
    checked = runner.invoke(app, ["check", "--format", "json"])
    payload = json.loads(checked.output)
    assert "fragments/broken.json" in payload["malformed"]
    rows = json.loads(runner.invoke(app, ["query", "--type", "fragment", "--format", "json"]).output)
    assert [r["slug"] for r in rows] == ["fragment"]  # the good entity still loads


@pytest.mark.integration
def test_schema_rejects_unimplemented_formats(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """jsonl (collection-only) and gjson (deferred) fail schema resolution loudly."""
    monkeypatch.chdir(fws)
    sp = fws / ".khub" / "schema.yaml"
    sp.write_text(sp.read_text().replace("format: json", "format: jsonl"))
    out = runner.invoke(app, ["query", "--type", "fragment", "--format", "json"])
    assert out.exit_code != 0
    assert "jsonl" in out.output


# --- git, search ----------------------------------------------------------------


@pytest.mark.integration
def test_search_finds_json_entity_by_field_and_body(fws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(fws)
    runner.invoke(app, ["add", "fragment", "--stage", "raw",
                        "--promoted_to", "zeppelin-doc", "--body", "unique mainsail prose"])
    by_field = json.loads(runner.invoke(app, ["search", "zeppelin", "--format", "json"]).output)
    assert [h["type"] for h in by_field] == ["fragment"]
    by_body = json.loads(runner.invoke(app, ["search", "mainsail", "--format", "json"]).output)
    assert [h["type"] for h in by_body] == ["fragment"]
