"""Singletons + body templates, end to end.

Covers: the `layout: singleton` storage matrix, singleton scan/get/add
semantics, template loading (including reserved-key rejection), body seeding at
`add`, singleton creation at init (creations only — WS-003 as amended), the
body-structure validate finding, and the missing-required-singleton check
finding. Mirrors the patterns of test_entity_create / test_init /
test_integrity.
"""

from __future__ import annotations

from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.entity import create, get
from khub.core.errors import LocatedError
from khub.core.integrity import check, validate
from khub.core.schema_model import TypeDecl
from khub.core.template import BodyTemplate, Section, load_template, missing_heading
from khub.core.workspace import init_workspace

runner = CliRunner()

PRESET = """
version: "0.1.0"
entities:
  prd:
    layout: singleton
    path: knowledge/prd.md
    required: true
    attributes:
      title: { required: true }
  note:
    layout: file
    path: notes
    attributes:
      title: { required: true }
"""

PRD_TEMPLATE = """\
title: Product requirements
sections:
  - heading: Vision
    hint: one paragraph
  - heading: Non-goals
  - heading: Success metrics
    text: |
      Nothing measured yet.
"""

NOTE_TEMPLATE = """\
sections:
  - heading: Summary
  - heading: Details
"""


@pytest.fixture
def ws(tmp_path: Path) -> Path:
    """A workspace scaffolded from a singleton-bearing preset with templates."""
    src = tmp_path / "presets"
    tpl = src / "fixture" / "templates"
    tpl.mkdir(parents=True)
    (src / "fixture" / "schema.yaml").write_text(PRESET)
    (tpl / "prd.yaml").write_text(PRD_TEMPLATE)
    (tpl / "note.yaml").write_text(NOTE_TEMPLATE)
    target = tmp_path / "ws"
    init_workspace("fixture", target, preset_source=src)
    return target


# --- storage matrix --------------------------------------------------------------


def test_singleton_needs_path() -> None:
    with pytest.raises(ValueError, match="exact file"):
        TypeDecl(layout="singleton")


def test_singleton_suffix_must_agree() -> None:
    with pytest.raises(ValueError, match="disagrees"):
        TypeDecl(layout="singleton", path="prd.md", format="yaml")


def test_required_is_singleton_only() -> None:
    with pytest.raises(ValueError, match="singleton-only"):
        TypeDecl(layout="file", path="notes", required=True)


# --- init: flatten + creation ----------------------------------------------------


def test_init_flattens_templates_and_creates_singletons(ws: Path) -> None:
    assert (ws / ".khub" / "templates" / "prd.yaml").is_file()
    prd = ws / "knowledge" / "prd.md"
    assert prd.is_file()
    text = prd.read_text()
    assert "title: Product requirements" in text
    assert "## Vision" in text
    assert "<!-- one paragraph -->" in text
    assert "Nothing measured yet." in text
    # note is not a singleton: no file created, just its dir
    assert (ws / "notes").is_dir()


def test_reinit_never_touches_an_existing_singleton(ws: Path) -> None:
    prd = ws / "knowledge" / "prd.md"
    prd.write_text(prd.read_text().replace("<!-- one paragraph -->", "We watch water."))
    before = prd.read_bytes()
    src = ws.parent / "presets"
    result = init_workspace("fixture", ws, preset_source=src, force=True)
    assert prd.read_bytes() == before
    assert result.entity_files_modified == 0
    assert result.singletons_created == ()


def test_init_reports_singletons_created(tmp_path: Path, ws: Path) -> None:
    # ws fixture already ran init; a fresh target reports the creation
    src = ws.parent / "presets"
    result = init_workspace("fixture", tmp_path / "ws2", preset_source=src)
    assert result.singletons_created == ("prd",)


# --- singleton entity semantics --------------------------------------------------


def test_singleton_resolves_by_bare_type_name(ws: Path) -> None:
    rec = get(ws, "prd")
    assert rec.meta["type"] == "prd"
    assert rec.body.startswith("## Vision")


def test_add_refuses_second_singleton(ws: Path) -> None:
    with pytest.raises(LocatedError) as ei:
        create(ws, "prd", {"title": "Another"})
    assert ei.value.code == "singleton_exists"


def test_add_refuses_foreign_singleton_id(ws: Path) -> None:
    (ws / "knowledge" / "prd.md").unlink()
    with pytest.raises(LocatedError) as ei:
        create(ws, "prd", {"title": "PRD"}, id_="my-prd")
    assert ei.value.code == "singleton_id"


def test_add_creates_missing_singleton_with_template(ws: Path) -> None:
    (ws / "knowledge" / "prd.md").unlink()
    result = create(ws, "prd", {"title": "PRD"})
    assert result.slug == "prd"
    assert "## Non-goals" in (ws / "knowledge" / "prd.md").read_text()


# --- add: template seeding -------------------------------------------------------


def test_add_seeds_body_from_template(ws: Path) -> None:
    result = create(ws, "note", {"title": "First"})
    text = result.path.read_text()
    assert "## Summary" in text and "## Details" in text


def test_add_explicit_body_wins_over_template(ws: Path) -> None:
    result = create(ws, "note", {"title": "Second"}, body="just prose\n")
    assert "## Summary" not in result.path.read_text()


def test_cli_no_template_flag(ws: Path, monkeypatch) -> None:
    monkeypatch.chdir(ws)
    r = runner.invoke(app, ["add", "note", "--title", "Third", "--no-template"])
    assert r.exit_code == 0, r.output
    body = (ws / "notes" / "third.md").read_text()
    assert "## Summary" not in body


# --- template loading ------------------------------------------------------------


def test_template_reserved_keys_rejected(ws: Path) -> None:
    (ws / ".khub" / "templates" / "note.yaml").write_text(
        "sections:\n  - heading: Summary\n    optional: true\n"
    )
    with pytest.raises(LocatedError) as ei:
        load_template(ws, "note")
    assert ei.value.code == "template_invalid"
    assert "reserved" in ei.value.message


def test_missing_heading_subsequence() -> None:
    tpl = BodyTemplate(type="t", sections=(Section("Vision"), Section("Non-goals")))
    assert missing_heading(tpl, "## Vision\n\n## Non-goals\n") is None
    assert missing_heading(tpl, "## Vision\n\n## Extra\n\n## Non-goals\n") is None
    assert missing_heading(tpl, "## 1. Vision\n\n## 2. Non-goals\n") is None  # numbering stripped
    assert missing_heading(tpl, "## Non-goals\n\n## Vision\n") == "Non-goals"  # order matters
    assert missing_heading(tpl, "## Vision\n") == "Non-goals"


# --- validate: body structure ----------------------------------------------------


def test_validate_clean_scaffolded_bodies(ws: Path) -> None:
    create(ws, "note", {"title": "Clean"})
    report = validate(ws)
    assert report.ok, [e.reason for e in report.errors]


def test_validate_reports_missing_section(ws: Path) -> None:
    result = create(ws, "note", {"title": "Broken"})
    text = result.path.read_text().replace("## Details", "## Detours")
    result.path.write_text(text)
    report = validate(ws)
    assert not report.ok
    err = next(e for e in report.errors if e.slug == "broken")
    assert err.field == "body"
    assert "Details" in err.reason


# --- check: required singleton ---------------------------------------------------


def test_check_reports_missing_required_singleton(ws: Path) -> None:
    (ws / "knowledge" / "prd.md").unlink()
    report = check(ws)
    assert report.missing_singletons == ["prd"]
    assert not report.passed
