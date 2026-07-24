"""TS-SKL — ``khub install-skills`` (the agent-skill install, split out of `init` in 0.9.0).

`core.skill` shells out to `npx skills add`; every test here stubs `shutil.which`
and `subprocess.run`, so no test ever reaches the network or the private repo.
The unit tests moved here from `test_init.py` unchanged in intent — same
behaviour, new entry point — and the CLI tests cover what the split added:
`--dry-run`, repeatable `--agent`, and exit 1 on failure (inside `init` the same
failure was best-effort).
"""

from __future__ import annotations

import json
import types
from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core import skill as skillmod

runner = CliRunner()


@pytest.fixture
def npx_ok(monkeypatch) -> list[tuple[list[str], Path, bool]]:
    """npx present and succeeding; records (cmd, cwd, capture_output) per call."""
    calls: list[tuple[list[str], Path, bool]] = []

    def fake_run(cmd: list[str], cwd: Path = None, capture_output: bool = False, **_k: object):  # type: ignore[assignment]
        calls.append((cmd, cwd, capture_output))
        return types.SimpleNamespace(returncode=0)

    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(skillmod.subprocess, "run", fake_run)
    return calls


# --- core.skill (moved from test_init.py) ------------------------------------


@pytest.mark.unit
def test_install_skill_skipped_without_npx(tmp_path: Path, monkeypatch) -> None:
    """No npx on PATH → skipped-no-npx, and subprocess is never invoked."""
    monkeypatch.setattr(skillmod.shutil, "which", lambda _: None)

    def forbid(*_a: object, **_k: object) -> object:  # would be a real network clone
        raise AssertionError("subprocess.run must not run when npx is absent")

    monkeypatch.setattr(skillmod.subprocess, "run", forbid)
    outcome = skillmod.install_skill(tmp_path)
    assert outcome.action == "skipped-no-npx"


@pytest.mark.unit
def test_install_skill_runs_npx_in_root(tmp_path: Path, npx_ok: list) -> None:
    """npx present → run `npx skills add <source> -s khub -s setup` with cwd=root."""
    outcome = skillmod.install_skill(tmp_path)

    assert outcome.action == "installed"
    (cmd, cwd, _), = npx_ok
    assert cwd == tmp_path
    assert cmd[:4] == ["npx", "-y", "skills", "add"]
    assert skillmod.SKILLS_SOURCE in cmd
    assert cmd.count("--skill") == 2 and "khub" in cmd and "setup" in cmd


@pytest.mark.unit
def test_install_skill_reports_failure(tmp_path: Path, monkeypatch) -> None:
    """A non-zero npx exit is reported as failed, never raised."""
    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(
        skillmod.subprocess, "run", lambda *_a, **_k: types.SimpleNamespace(returncode=1)
    )
    assert skillmod.install_skill(tmp_path).action == "failed"


@pytest.mark.unit
def test_install_skill_survives_exec_error(tmp_path: Path, monkeypatch) -> None:
    """subprocess exec failure (Windows .cmd, broken PATH) is failed, never raised."""

    def boom(*_a: object, **_k: object) -> object:
        raise OSError("cannot spawn npx")

    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(skillmod.subprocess, "run", boom)
    assert skillmod.install_skill(tmp_path).action == "failed"  # no traceback


@pytest.mark.unit
def test_install_skill_gitignores_artifacts(tmp_path: Path, npx_ok: list) -> None:
    """A successful install gitignores the per-machine skill dirs, not skills-lock.json."""
    skillmod.install_skill(tmp_path)
    ignored = (tmp_path / ".gitignore").read_text().splitlines()
    assert ".claude/skills/" in ignored and ".agents/skills/" in ignored
    assert "skills-lock.json" not in ignored


# --- the CLI command ---------------------------------------------------------


@pytest.mark.integration
def test_cli_install_skills_runs_in_workspace(fresh_ws: Path, npx_ok: list, monkeypatch) -> None:
    """`khub install-skills` runs npx once, with cwd = the resolved workspace root."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["install-skills"])
    assert result.exit_code == 0, result.output
    (_, cwd, _), = npx_ok
    assert cwd == fresh_ws


@pytest.mark.integration
def test_cli_install_skills_dry_run_writes_nothing(
    fresh_ws: Path, npx_ok: list, monkeypatch
) -> None:
    """--dry-run prints the exact npx command and never invokes it."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["install-skills", "--dry-run", "--format", "json"])
    assert result.exit_code == 0, result.output
    payload = json.loads(result.output)
    assert payload["action"] == "dry-run"
    assert payload["command"][:4] == ["npx", "-y", "skills", "add"]
    assert npx_ok == []  # dry really is dry
    assert not (fresh_ws / ".gitignore").read_text().count(".claude/skills/")


@pytest.mark.integration
def test_cli_install_skills_repeatable_agent(fresh_ws: Path, npx_ok: list, monkeypatch) -> None:
    """Each --agent is passed through to npx's own --agent; omitted keeps auto-detect."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(
        app, ["install-skills", "--agent", "claude-code", "--agent", "cursor"]
    )
    assert result.exit_code == 0, result.output
    (cmd, _, _), = npx_ok
    assert cmd.count("--agent") == 2
    assert "claude-code" in cmd and "cursor" in cmd


@pytest.mark.integration
def test_cli_install_skills_exits_1_on_failure(fresh_ws: Path, monkeypatch) -> None:
    """Asked for explicitly, a failed install is the outcome: stderr + exit 1.

    Inside `khub init` (<=0.8.0) the same failure was best-effort — the scaffold
    had to survive it. A command whose only job is the install does not.
    """
    monkeypatch.chdir(fresh_ws)
    monkeypatch.setattr(skillmod.shutil, "which", lambda _: "/opt/npx")
    monkeypatch.setattr(
        skillmod.subprocess, "run", lambda *_a, **_k: types.SimpleNamespace(returncode=1)
    )
    result = runner.invoke(app, ["install-skills"])
    assert result.exit_code == 1
    # CliRunner is not a TTY, so the read contract emits the machine payload, not prose.
    assert json.loads(result.output)["action"] == "failed"
