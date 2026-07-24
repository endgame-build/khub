"""STORY-WS-001 — `khub init`: scaffold a workspace from a preset.

Covers TS-WS-001-01 (scaffold), -02 (unknown preset), -03 (non-empty target),
-04 (force-seed over a live corpus) and the unit rows TS-WS-001-U01..U08.

Unit/integration use a tiny `note` preset (fast); the E2E uses the real
firm-ops preset. Counts are read from the compiled schema, never literals
(TS-001 Risk: 12/17 is stale prose vs the 9-type preset).
"""

from __future__ import annotations

import json
import types
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
    """A preset-source dir holding the tiny `note` preset (directory layout)."""
    src = tmp_path / "presets"
    (src / "note").mkdir(parents=True)
    (src / "note" / "schema.yaml").write_text(NOTE_PRESET)
    return src


# --- TS-WS-001-U01 / U06: preset resolution -------------------------------------


@pytest.mark.unit
def test_resolve_known_preset_from_package() -> None:
    """TS-WS-001-U01: the packaged firm-ops preset resolves (dir layout)."""
    resolved = resolve_preset("firm-ops")
    assert resolved.name == "schema.yaml"
    assert resolved.parent.name == "firm-ops"


@pytest.mark.unit
def test_resolve_preset_from_source(preset_source: Path) -> None:
    """TS-WS-001-U01: a preset resolves from --preset-source."""
    assert resolve_preset("note", preset_source) == preset_source / "note" / "schema.yaml"


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
    # `defaults` carries stale_days only — no `format` default is written (nothing reads it).
    assert "stale_days" in cfg["defaults"]
    assert "format" not in cfg["defaults"]


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
    assert "Unknown preset 'bogus'. Known presets: build-hub, firm-ops" in result.output
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
    result = runner.invoke(app, ["init", "firm-ops", str(target), "--no-wire", "--no-skill"])
    assert result.exit_code == 0, result.output
    assert f"Initialized firm-ops workspace at {target}" in result.output
    head = (target / ".khub" / "schema.yaml").read_text().splitlines()[0]
    assert head.startswith("# khub-preset: firm-ops@")


@pytest.mark.unit
def test_entity_less_preset_rejected(tmp_path: Path, preset_source: Path) -> None:
    """A preset declaring no entities is rejected, not silently scaffolded empty."""
    (preset_source / "hollow").mkdir()
    (preset_source / "hollow" / "schema.yaml").write_text('version: "1.0.0"\nentities: {}\n')
    with pytest.raises(LocatedError) as ei:
        init_workspace("hollow", tmp_path / "ws", preset_source=preset_source)
    assert ei.value.code == "empty_preset"


@pytest.mark.unit
def test_failed_init_leaves_no_partial_khub(
    tmp_path: Path, preset_source: Path, monkeypatch
) -> None:
    """A failure mid-scaffold cleans up the partial .khub it created."""
    import khub.core.workspace as wsmod

    def boom(*_a: object, **_k: object) -> None:
        raise RuntimeError("mid-init failure")

    monkeypatch.setattr(wsmod, "_append_gitignore", boom)
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

    real_append = wsmod._append_gitignore

    def tampering(gitignore, line):  # simulate a regression clobbering a file mid-init
        entity.write_text("TAMPERED")
        return real_append(gitignore, line)

    monkeypatch.setattr(wsmod, "_append_gitignore", tampering)
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
    runner.invoke(app, ["init", "firm-ops", str(target), "--no-wire", "--no-skill"])
    result = runner.invoke(
        app, ["init", "firm-ops", str(target), "--force", "--no-wire", "--no-skill"]
    )
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
    result = runner.invoke(
        app, ["init", "firm-ops", str(target), "--force", "--no-wire", "--no-skill"]
    )
    assert result.exit_code == 0, result.output
    assert "Initialized firm-ops workspace; 0 entity files modified" in result.output
    assert corpus.read_bytes() == before


@pytest.mark.unit
def test_force_seed_tolerates_directory_named_md(
    tmp_path: Path, preset_source: Path
) -> None:
    """HQ-port regression: rglob("*.md") matches directories; init must not crash."""
    (tmp_path / "archive" / "docs" / "data-backup.md").mkdir(parents=True)
    result = init_workspace("note", tmp_path, preset_source=preset_source, force=True)
    assert result.entity_files_modified == 0


# --- init tail: wire + agnostic skill install via npx skills --------------------


@pytest.mark.unit
def test_install_skill_skipped_without_npx(tmp_path: Path, monkeypatch) -> None:
    """No npx on PATH → skipped-no-npx, and subprocess is never invoked."""
    from khub.core import skill as skillmod

    monkeypatch.setattr(skillmod.shutil, "which", lambda _: None)

    def forbid(*_a: object, **_k: object) -> object:  # would be a real network clone
        raise AssertionError("subprocess.run must not run when npx is absent")

    monkeypatch.setattr(skillmod.subprocess, "run", forbid)
    outcome = skillmod.install_skill(tmp_path)
    assert outcome.action == "skipped-no-npx"


@pytest.mark.unit
def test_install_skill_runs_npx_in_root(tmp_path: Path, monkeypatch) -> None:
    """npx present → run `npx skills add <source> -s khub -s setup` with cwd=root."""
    from khub.core import skill as skillmod

    calls: list[tuple[list[str], Path]] = []

    def fake_run(cmd: list[str], cwd: Path = None, **_k: object):  # type: ignore[assignment]
        calls.append((cmd, cwd))
        return types.SimpleNamespace(returncode=0)

    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(skillmod.subprocess, "run", fake_run)
    outcome = skillmod.install_skill(tmp_path)

    assert outcome.action == "installed"
    (cmd, cwd), = calls
    assert cwd == tmp_path
    assert cmd[:4] == ["npx", "-y", "skills", "add"]
    assert skillmod.SKILLS_SOURCE in cmd
    assert cmd.count("--skill") == 2 and "khub" in cmd and "setup" in cmd


@pytest.mark.unit
def test_install_skill_reports_failure(tmp_path: Path, monkeypatch) -> None:
    """A non-zero npx exit is reported as failed, never raised."""
    from khub.core import skill as skillmod

    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(
        skillmod.subprocess, "run", lambda *_a, **_k: types.SimpleNamespace(returncode=1)
    )
    assert skillmod.install_skill(tmp_path).action == "failed"


@pytest.mark.unit
def test_install_skill_survives_exec_error(tmp_path: Path, monkeypatch) -> None:
    """subprocess exec failure (Windows .cmd, broken PATH) is failed, never raised."""
    from khub.core import skill as skillmod

    def boom(*_a: object, **_k: object) -> object:
        raise OSError("cannot spawn npx")

    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(skillmod.subprocess, "run", boom)
    assert skillmod.install_skill(tmp_path).action == "failed"  # no traceback


@pytest.mark.unit
def test_install_skill_gitignores_artifacts(tmp_path: Path, monkeypatch) -> None:
    """A successful install gitignores the per-machine skill dirs, not skills-lock.json."""
    from khub.core import skill as skillmod

    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(
        skillmod.subprocess, "run", lambda *_a, **_k: types.SimpleNamespace(returncode=0)
    )
    skillmod.install_skill(tmp_path)
    ignored = (tmp_path / ".gitignore").read_text().splitlines()
    assert ".claude/skills/" in ignored and ".agents/skills/" in ignored
    assert "skills-lock.json" not in ignored


@pytest.fixture
def _no_network_skill(monkeypatch):
    """Stub the npx skill install for CLI tests: record (cwd, capture_output), no network."""
    from khub.core import skill as skillmod

    calls: list[tuple[Path, bool]] = []

    def fake_run(cmd: list[str], cwd: Path = None, capture_output: bool = False, **_k: object):  # type: ignore[assignment]
        calls.append((cwd, capture_output))
        return types.SimpleNamespace(returncode=0)

    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(skillmod.subprocess, "run", fake_run)
    return calls


@pytest.mark.integration
def test_init_wires_and_installs_skill(
    tmp_path: Path, preset_source: Path, _no_network_skill: list
) -> None:
    """A bare (non-interactive) `init` scaffolds, wires both agent files, installs the skill."""
    ws = tmp_path / "ws"
    result = runner.invoke(app, ["init", "note", str(ws), "--preset-source", str(preset_source)])
    assert result.exit_code == 0, result.output
    assert (ws / "CLAUDE.md").exists()  # wire ran; no selection → both files
    assert (ws / "AGENTS.md").exists()
    assert "@.khub/schema.yaml" not in (ws / "AGENTS.md").read_text()  # pointer, not import
    assert "installed khub agent skill" in result.output
    assert _no_network_skill == [(ws, False)]  # npx run once in the workspace; not captured (text mode)


@pytest.mark.integration
def test_init_no_skill_skips_install(
    tmp_path: Path, preset_source: Path, _no_network_skill: list
) -> None:
    """--no-skill wires but never invokes npx."""
    ws = tmp_path / "ws"
    result = runner.invoke(
        app, ["init", "note", str(ws), "--preset-source", str(preset_source), "--no-skill"]
    )
    assert result.exit_code == 0, result.output
    assert (ws / "CLAUDE.md").exists()
    assert _no_network_skill == []


@pytest.mark.integration
def test_init_no_wire_skips_wiring(
    tmp_path: Path, preset_source: Path, _no_network_skill: list
) -> None:
    """--no-wire leaves no agent files."""
    ws = tmp_path / "ws"
    result = runner.invoke(
        app,
        ["init", "note", str(ws), "--preset-source", str(preset_source), "--no-wire", "--no-skill"],
    )
    assert result.exit_code == 0, result.output
    assert not (ws / "CLAUDE.md").exists()
    assert not (ws / "AGENTS.md").exists()


@pytest.mark.integration
def test_init_json_carries_wire_and_skill(
    tmp_path: Path, preset_source: Path, _no_network_skill: list
) -> None:
    """--format json folds wire + skill into the payload, and stdout stays pure JSON."""
    ws = tmp_path / "ws"
    result = runner.invoke(
        app,
        ["init", "note", str(ws), "--preset-source", str(preset_source), "--format", "json"],
    )
    assert result.exit_code == 0, result.output
    payload = json.loads(result.output)  # whole stdout parses — no npx noise ahead of it
    assert payload["skill"]["action"] == "installed"
    wired = {Path(o["path"]).name for o in payload["wire"]}
    assert wired == {"CLAUDE.md", "AGENTS.md"}  # non-interactive → both files
    assert _no_network_skill == [(ws, True)]  # json mode captures npx output (quiet)


@pytest.mark.integration
def test_init_wire_failure_is_best_effort(
    tmp_path: Path, preset_source: Path, monkeypatch, _no_network_skill: list
) -> None:
    """A wire that raises during init does not unwind the scaffold: exit 0, error surfaced."""
    def boom(_root: Path, **_k: object) -> object:
        raise LocatedError(code="schema_error", message="schema went missing")

    # init_cmd imports wire lazily from khub.core.wire, so patch it at the source.
    monkeypatch.setattr("khub.core.wire.wire", boom)
    ws = tmp_path / "ws"
    result = runner.invoke(
        app,
        ["init", "note", str(ws), "--preset-source", str(preset_source), "--format", "json"],
    )
    assert result.exit_code == 0, result.output  # scaffold survived the wire failure
    payload = json.loads(result.output)
    assert payload["wire_error"] == "schema went missing"
    assert "wire" not in payload  # no outcomes recorded when wire raised
    assert payload["skill"]["action"] == "installed"  # skill tail still ran


@pytest.mark.unit
def test_wire_targets_mapping() -> None:
    """init's seed choice: claude-code→CLAUDE, other→AGENTS, no selection→both."""
    from khub.cli.init_cmd import _wire_targets

    assert _wire_targets(None) == (True, True)
    assert _wire_targets([]) == (True, True)
    assert _wire_targets(["claude-code"]) == (True, False)
    assert _wire_targets(["codex"]) == (False, True)
    assert _wire_targets(["claude-code", "cursor"]) == (True, True)
