"""TS-SKL — ``khub install-skills`` (a file copy out of the package, since 0.10.0).

Through 0.9.x this shelled out to ``npx skills add <private repo>``, because the
skills sat outside the wheel. They now ship as package data and the install is a
copy: offline, idempotent, and safe to re-run. The regression test for that whole
premise is ``test_install_never_spawns_a_subprocess`` — everything else here is
about where files land and what gets reported.
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.skill import available_skills, install_skills, skills_dir

runner = CliRunner()

# Every project dir the default install writes, and the skills it ships.
PROJECT_DIRS = (".claude/skills", ".agents/skills", ".opencode/skills")


@pytest.fixture
def no_subprocess(monkeypatch) -> None:
    """Any subprocess call is a regression: the copy install must never shell out."""
    import subprocess

    def forbid(*_a: object, **_k: object) -> object:
        raise AssertionError("install-skills must not spawn a subprocess")

    monkeypatch.setattr(subprocess, "run", forbid)


# --- the packaged source -----------------------------------------------------


@pytest.mark.unit
def test_skills_dir_carries_both_skills() -> None:
    """The packaged (or dev-checkout) skills directory is the install's source."""
    assert (skills_dir() / "khub" / "SKILL.md").is_file()
    assert (skills_dir() / "setup" / "SKILL.md").is_file()
    assert available_skills() == ["khub", "setup"]


# --- core.install_skills -----------------------------------------------------


@pytest.mark.unit
def test_installs_every_skill_into_every_project_dir(fresh_ws: Path, no_subprocess: None) -> None:
    """Default: both skills into all three agent dirs, reported per file."""
    report = install_skills(fresh_ws)
    assert report.scope == "project"
    assert {w.action for w in report.writes} == {"created"}
    for target in PROJECT_DIRS:
        for name in ("khub", "setup"):
            assert (fresh_ws / target / name / "SKILL.md").is_file(), target


@pytest.mark.unit
def test_second_run_is_unchanged_and_rewrites_nothing(fresh_ws: Path) -> None:
    """Idempotent: a re-install reports `unchanged` and leaves mtimes alone."""
    install_skills(fresh_ws)
    written = fresh_ws / ".agents/skills/khub/SKILL.md"
    before = written.stat().st_mtime_ns

    report = install_skills(fresh_ws)
    assert {w.action for w in report.writes} == {"unchanged"}
    assert written.stat().st_mtime_ns == before


@pytest.mark.unit
def test_drifted_file_is_updated(fresh_ws: Path) -> None:
    """An installed copy is managed: edited content is re-synced, not preserved."""
    install_skills(fresh_ws)
    drifted = fresh_ws / ".agents/skills/khub/SKILL.md"
    drifted.write_text("stale content")

    report = install_skills(fresh_ws)
    actions = {w.path: w.action for w in report.writes}
    assert actions[".agents/skills/khub/SKILL.md"] == "updated"
    assert drifted.read_text() != "stale content"


@pytest.mark.unit
def test_dry_run_reports_and_writes_nothing(fresh_ws: Path) -> None:
    report = install_skills(fresh_ws, dry_run=True)
    assert report.dry_run and report.writes
    assert not (fresh_ws / ".agents").exists()
    assert ".agents/skills/" not in (fresh_ws / ".gitignore").read_text()


@pytest.mark.unit
def test_target_and_skill_narrow_the_install(fresh_ws: Path) -> None:
    install_skills(fresh_ws, targets=["agents"], skills=["khub"])
    assert (fresh_ws / ".agents/skills/khub/SKILL.md").is_file()
    assert not (fresh_ws / ".agents/skills/setup").exists()
    assert not (fresh_ws / ".claude").exists()


@pytest.mark.unit
def test_project_scope_gitignores_only_what_khub_owns(fresh_ws: Path) -> None:
    """Installed skills are reproducible from the CLI, so they stay out of git —
    but ignore only khub's own skill directories. Ignoring the whole
    `.claude/skills/` would silently swallow a repo's own committed skills."""
    install_skills(fresh_ws)
    ignored = (fresh_ws / ".gitignore").read_text().splitlines()
    for target in PROJECT_DIRS:
        assert f"{target}/khub/" in ignored
        assert f"{target}/setup/" in ignored
        assert f"{target}/" not in ignored  # not the whole directory


@pytest.mark.unit
def test_global_scope_writes_home_dirs_and_no_gitignore(
    fresh_ws: Path, tmp_path: Path, monkeypatch
) -> None:
    """--global installs per machine; a home directory is not a git repo."""
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setattr(Path, "home", staticmethod(lambda: home))
    # This asserts the ~/.config fallback, so the ambient XDG_CONFIG_HOME must not
    # leak in — CI runners set it, which is how this failed there and not locally.
    monkeypatch.delenv("XDG_CONFIG_HOME", raising=False)

    report = install_skills(fresh_ws, scope_global=True)
    assert report.scope == "global"
    assert (home / ".agents/skills/khub/SKILL.md").is_file()
    assert (home / ".config/opencode/skills/khub/SKILL.md").is_file()  # opencode's home path differs
    assert not (fresh_ws / ".agents").exists()
    assert ".agents/skills/" not in (fresh_ws / ".gitignore").read_text()
    # Absolute, so a reader can tell a machine install from a workspace one; relative
    # to $HOME these would read identically to a project install.
    assert all(w.path.startswith(str(home)) for w in report.writes)


@pytest.mark.unit
def test_global_scope_needs_no_workspace(tmp_path: Path, monkeypatch) -> None:
    """A machine-wide install is what you run *before* any workspace exists."""
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setattr(Path, "home", staticmethod(lambda: home))

    report = install_skills(None, scope_global=True)
    assert report.scope == "global"
    assert (home / ".claude/skills/khub/SKILL.md").is_file()


@pytest.mark.unit
def test_global_opencode_honours_xdg_config_home(tmp_path: Path, monkeypatch) -> None:
    """opencode reads its skills under $XDG_CONFIG_HOME; writing to ~/.config
    regardless would report `created` for files opencode never loads."""
    home = tmp_path / "home"
    xdg = tmp_path / "xdg"
    home.mkdir()
    monkeypatch.setattr(Path, "home", staticmethod(lambda: home))
    monkeypatch.setenv("XDG_CONFIG_HOME", str(xdg))

    install_skills(None, targets=["opencode"], scope_global=True)
    assert (xdg / "opencode/skills/khub/SKILL.md").is_file()
    assert not (home / ".config").exists()


@pytest.mark.unit
@pytest.mark.parametrize(
    "kwargs, code",
    [({"skills": ["ghost"]}, "unknown_skill"), ({"targets": ["emacs"]}, "unknown_target")],
)
def test_unknown_selection_is_a_located_error(fresh_ws: Path, kwargs: dict, code: str) -> None:
    from khub.core.errors import LocatedError

    with pytest.raises(LocatedError) as err:
        install_skills(fresh_ws, **kwargs)
    assert err.value.code == code


# --- the CLI -----------------------------------------------------------------


@pytest.mark.integration
def test_cli_install_never_spawns_a_subprocess(
    fresh_ws: Path, monkeypatch, no_subprocess: None
) -> None:
    """The whole point of 0.10.0: no npx, no clone, no network — just a copy."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["install-skills"])
    assert result.exit_code == 0, result.output


@pytest.mark.integration
def test_cli_json_payload(fresh_ws: Path, monkeypatch) -> None:
    monkeypatch.chdir(fresh_ws)
    payload = json.loads(runner.invoke(app, ["install-skills", "--format", "json"]).output)
    assert payload["scope"] == "project"
    assert payload["skills"] == ["khub", "setup"]
    assert len(payload["writes"]) == 6  # 2 skills x 3 targets
    assert {w["action"] for w in payload["writes"]} == {"created"}


@pytest.mark.integration
def test_cli_unknown_target_is_a_clean_error(fresh_ws: Path, monkeypatch) -> None:
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["install-skills", "--target", "emacs"])
    assert result.exit_code == 1
    assert "Unknown target" in result.output
    assert "Traceback" not in result.output


@pytest.mark.integration
def test_cli_global_works_outside_a_workspace(tmp_path: Path, monkeypatch) -> None:
    """`--global` must not demand a `.khub/`: it is the pre-workspace install."""
    home = tmp_path / "home"
    elsewhere = tmp_path / "elsewhere"
    home.mkdir()
    elsewhere.mkdir()
    monkeypatch.setattr(Path, "home", staticmethod(lambda: home))
    monkeypatch.chdir(elsewhere)

    result = runner.invoke(app, ["install-skills", "--global", "--format", "json"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output)["scope"] == "global"
    assert (home / ".agents/skills/khub/SKILL.md").is_file()


@pytest.mark.integration
def test_cli_agent_flag_is_gone(fresh_ws: Path, monkeypatch) -> None:
    """`--agent` was the npx passthrough; with npx gone it has no meaning."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["install-skills", "--agent", "claude-code"])
    assert result.exit_code == 2
    assert "No such option" in result.output
