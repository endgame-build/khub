"""STORY-WS-001 — `khub init`: scaffold a workspace from a preset.

Covers TS-WS-001-01 (scaffold), -02 (unknown preset), -03 (non-empty target),
-04 (force-seed over a live corpus) and the unit rows TS-WS-001-U01..U08.

Unit/integration use a tiny `note` preset (fast); the E2E uses the real
firm-ops preset. Counts are read from the compiled schema, never literals
(TS-001 Risk: 12/17 is stale prose vs the 9-type preset).
"""

from __future__ import annotations

from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.errors import LocatedError
from khub.core.resolve import load_yaml
from khub.core.workspace import init_workspace, known_presets, resolve_preset

runner = CliRunner()

# A minimal, self-contained preset for the fast paths.
NOTE_PRESET = """
version: "9.9.9"
entities:
  note:
    layout: file
    path: notes
    attributes:
      body: { type: text }
"""


@pytest.fixture
def preset_source(tmp_path: Path) -> Path:
    """A preset-source dir holding the tiny `note` preset."""
    src = tmp_path / "presets"
    src.mkdir()
    (src / "note.yaml").write_text(NOTE_PRESET)
    return src


# --- TS-WS-001-U01 / U06: preset resolution -------------------------------------


@pytest.mark.unit
def test_resolve_known_preset_from_package() -> None:
    """TS-WS-001-U01: the packaged firm-ops preset resolves."""
    assert resolve_preset("firm-ops").name == "firm-ops.yaml"


@pytest.mark.unit
def test_resolve_preset_from_source(preset_source: Path) -> None:
    """TS-WS-001-U01: a preset resolves from --preset-source."""
    assert resolve_preset("note", preset_source) == preset_source / "note.yaml"


@pytest.mark.unit
def test_unknown_preset_lists_known() -> None:
    """TS-WS-001-U06: an unknown preset is rejected, listing the known presets."""
    with pytest.raises(LocatedError) as ei:
        resolve_preset("bogus")
    assert ei.value.code == "unknown_preset"
    assert "bogus" in ei.value.message
    assert "firm-ops" in ei.value.message
    assert "firm-ops" in known_presets()


# --- TS-WS-001-U02..U05 / U08: scaffold a workspace -----------------------------


@pytest.mark.unit
def test_flatten_writes_one_schema(tmp_path: Path, preset_source: Path) -> None:
    """TS-WS-001-U02 (WS-001): core + preset flatten into one .khub/schema.yaml."""
    ws = tmp_path / "ws"
    init_workspace("note", ws, preset_source=preset_source)
    schema = ws / ".khub" / "schema.yaml"
    assert schema.exists()
    data = load_yaml(schema)
    assert "base" in data and "note" in data["entities"]  # core base + preset entity


@pytest.mark.unit
def test_provenance_header_stamped(tmp_path: Path, preset_source: Path) -> None:
    """TS-WS-001-U03 (WS-009): the schema header carries preset@version."""
    ws = tmp_path / "ws"
    init_workspace("note", ws, preset_source=preset_source)
    head = (ws / ".khub" / "schema.yaml").read_text().splitlines()[0]
    assert head == "# khub-preset: note@9.9.9"


@pytest.mark.unit
def test_config_carries_provenance_and_defaults(tmp_path: Path, preset_source: Path) -> None:
    """TS-WS-001-U04 (WS-009): config.yaml has provenance, name, and defaults."""
    ws = tmp_path / "ws"
    init_workspace("note", ws, preset_source=preset_source, name="acme")
    cfg = load_yaml(ws / ".khub" / "config.yaml")
    assert cfg["preset"] == "note"
    assert cfg["version"] == "9.9.9"
    assert cfg["name"] == "acme"
    assert cfg["defaults"]["format"] == "text"
    assert "stale_days" in cfg["defaults"]


@pytest.mark.unit
def test_name_defaults_to_target_dir(tmp_path: Path, preset_source: Path) -> None:
    """TS-WS-001-U04: name defaults to the target directory name."""
    target = tmp_path / "hq"
    init_workspace("note", target, preset_source=preset_source)
    assert load_yaml(target / ".khub" / "config.yaml")["name"] == "hq"


@pytest.mark.unit
def test_gitignore_excludes_generated(tmp_path: Path, preset_source: Path) -> None:
    """TS-WS-001-U05 (WS-002): .khub/generated/ is gitignored."""
    ws = tmp_path / "ws"
    init_workspace("note", ws, preset_source=preset_source)
    assert ".khub/generated/" in (ws / ".gitignore").read_text()


@pytest.mark.unit
def test_entity_tree_laid_down(tmp_path: Path, preset_source: Path) -> None:
    """TS-WS-001-U02: one directory per type's storage path."""
    ws = tmp_path / "ws"
    init_workspace("note", ws, preset_source=preset_source)
    assert (ws / "notes").is_dir()


@pytest.mark.unit
def test_unknown_preset_writes_nothing(tmp_path: Path) -> None:
    """TS-WS-001-U06 (REQ-WS001-03): a bad preset leaves the target empty."""
    with pytest.raises(LocatedError):
        init_workspace("bogus", tmp_path)
    assert list(tmp_path.iterdir()) == []


@pytest.mark.unit
def test_nonempty_target_guarded(tmp_path: Path, preset_source: Path) -> None:
    """TS-WS-001-U07 (REQ-WS001-04): a non-empty target needs --force."""
    (tmp_path / "stray.txt").write_text("keep me")
    with pytest.raises(LocatedError) as ei:
        init_workspace("note", tmp_path, preset_source=preset_source)
    assert ei.value.code == "target_not_empty"
    assert (tmp_path / "stray.txt").read_text() == "keep me"
    assert not (tmp_path / ".khub").exists()


@pytest.mark.unit
def test_force_never_overwrites_entity_md(tmp_path: Path, preset_source: Path) -> None:
    """TS-WS-001-U08 (REQ-WS001-05, WS-003): force-seed leaves entity .md untouched."""
    notes = tmp_path / "notes"
    notes.mkdir()
    entity = notes / "first.md"
    entity.write_text("---\ntype: note\n---\nbody\n")
    before = entity.read_bytes()
    result = init_workspace("note", tmp_path, preset_source=preset_source, force=True)
    assert entity.read_bytes() == before
    assert result.entity_files_modified == 0
    assert (tmp_path / ".khub").exists()


# --- TS-WS-001-02 / -03: command surface + error messages -----------------------


@pytest.mark.integration
def test_cli_unknown_preset_message(tmp_path: Path) -> None:
    """TS-WS-001-02 (AC-002): the exact unknown-preset line; nothing written."""
    target = tmp_path / "hq"
    target.mkdir()
    result = runner.invoke(app, ["init", "bogus", str(target)])
    assert result.exit_code == 1
    assert "Unknown preset 'bogus'. Known presets: firm-ops" in result.output
    assert list(target.iterdir()) == []


@pytest.mark.integration
def test_cli_nonempty_target_message(tmp_path: Path) -> None:
    """TS-WS-001-03 (AC-003): the exact non-empty-target line; files untouched."""
    target = tmp_path / "hq"
    target.mkdir()
    (target / "stray.txt").write_text("keep me")
    result = runner.invoke(app, ["init", "firm-ops", str(target)])
    assert result.exit_code == 1
    assert f"Target {target} is not empty. Pass --force to scaffold anyway" in result.output
    assert (target / "stray.txt").read_text() == "keep me"


# --- TS-WS-001-01 / -04: end to end with the real firm-ops preset ----------------


@pytest.mark.e2e
def test_cli_scaffold_firm_ops(tmp_path: Path) -> None:
    """TS-WS-001-01 (AC-001): a clean firm-ops scaffold compiles and confirms."""
    target = tmp_path / "hq"
    result = runner.invoke(app, ["init", "firm-ops", str(target)])
    assert result.exit_code == 0, result.output
    assert f"Initialized firm-ops workspace at {target}" in result.output
    for artifact in ("schema.linkml.yaml", "models.py", "schema.json"):
        assert (target / ".khub" / "generated" / artifact).exists()
    head = (target / ".khub" / "schema.yaml").read_text().splitlines()[0]
    assert head.startswith("# khub-preset: firm-ops@")


@pytest.mark.unit
def test_entity_less_preset_rejected(tmp_path: Path, preset_source: Path) -> None:
    """A preset declaring no entities is rejected, not silently scaffolded empty."""
    (preset_source / "hollow.yaml").write_text('version: "1.0.0"\nentities: {}\n')
    with pytest.raises(LocatedError) as ei:
        init_workspace("hollow", tmp_path / "ws", preset_source=preset_source)
    assert ei.value.code == "empty_preset"


@pytest.mark.unit
def test_failed_compile_leaves_no_partial_khub(
    tmp_path: Path, preset_source: Path, monkeypatch
) -> None:
    """A non-LocatedError during compile cleans up the partial .khub it created."""
    def boom(*_a: object, **_k: object) -> None:
        raise RuntimeError("linkml exploded")

    monkeypatch.setattr("khub.core.workspace.compile_schema", boom)
    ws = tmp_path / "ws"
    with pytest.raises(RuntimeError):
        init_workspace("note", ws, preset_source=preset_source)
    assert not (ws / ".khub").exists()


@pytest.mark.unit
def test_entity_files_modified_is_measured(
    tmp_path: Path, preset_source: Path, monkeypatch
) -> None:
    """The cutover count reflects real modifications: inject a tamper mid-init and
    it reports 1 (a hardcoded 0 would not catch it)."""
    import khub.core.workspace as wsmod

    ws = tmp_path / "ws"
    ws.mkdir()
    (ws / "notes").mkdir()
    entity = ws / "notes" / "a.md"
    entity.write_text("---\ntype: note\n---\noriginal\n")

    real_compile = wsmod.compile_schema

    def tampering(schema: object, out: object):  # simulate a regression clobbering a file
        entity.write_text("TAMPERED")
        return real_compile(schema, out)

    monkeypatch.setattr(wsmod, "compile_schema", tampering)
    result = init_workspace("note", ws, preset_source=preset_source, force=True)
    assert result.entity_files_modified == 1


@pytest.mark.unit
def test_name_falls_back_when_blank(tmp_path: Path, preset_source: Path) -> None:
    """A blank --name falls back to the target dir name, never an empty name."""
    result = init_workspace("note", tmp_path / "ws", preset_source=preset_source, name="")
    assert result.name == "ws"


@pytest.mark.e2e
def test_reinit_empty_workspace_is_not_a_cutover(tmp_path: Path) -> None:
    """Re-running --force on an entity-empty workspace is not reported as a corpus cutover."""
    target = tmp_path / "hq"
    runner.invoke(app, ["init", "firm-ops", str(target)])
    result = runner.invoke(app, ["init", "firm-ops", str(target), "--force"])
    assert result.exit_code == 0, result.output
    assert "entity files modified" not in result.output
    assert f"Initialized firm-ops workspace at {target}" in result.output


@pytest.mark.e2e
def test_cli_force_seed_modifies_no_entities(tmp_path: Path) -> None:
    """TS-WS-001-04 (AC-004): force-seed over a corpus modifies 0 entity files."""
    target = tmp_path / "hq"
    target.mkdir()
    corpus = target / "clients" / "acme.md"
    corpus.parent.mkdir(parents=True)
    corpus.write_text("---\ntype: client\nname: Acme\ncreated: 2025-01-01\n---\n")
    before = corpus.read_bytes()
    result = runner.invoke(app, ["init", "firm-ops", str(target), "--force"])
    assert result.exit_code == 0, result.output
    assert "Initialized firm-ops workspace; 0 entity files modified" in result.output
    assert corpus.read_bytes() == before
