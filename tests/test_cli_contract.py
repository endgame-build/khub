"""Machine-facing CLI contract — the audit-2026-07-05 fixes.

Exercises the thin Typer adapter through ``CliRunner`` (no TTY, so reads emit JSON):
global options (``-C/--workspace``, ``--version``), the no-git ``log`` JSON payload,
the unified qualified ``id`` contract, ``get`` downgrading a piped table to JSON, the
removed ``--both`` flag, defensive ``malformed`` rendering, config-driven ``stale``
default, and the idempotent link/unlink no-op messages.
"""

from __future__ import annotations

import json
from datetime import date, timedelta
from pathlib import Path
from typing import Callable

import pytest
from typer.testing import CliRunner

from khub.cli.main import app

runner = CliRunner()

Seed = Callable[..., None]


@pytest.fixture
def seeded(fresh_ws: Path, seed: Seed) -> Path:
    """A firm-ops workspace with a client, a person, and an opportunity edge."""
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme",
         created=date(2025, 1, 1), updated=date(2025, 1, 1))
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor",
         role="manager", created=date(2025, 1, 1))
    seed(fresh_ws, "opportunities/acme-pov/_index.md", type="opportunity",
         stage="prospect", created=date(2025, 1, 1), updated=date(2025, 1, 1),
         client="acme", owner="noor")
    return fresh_ws


# --- fix 1: --version / --workspace -----------------------------------------


@pytest.mark.integration
def test_version_flag_prints_version() -> None:
    from importlib.metadata import version as pkg_version

    result = runner.invoke(app, ["--version"])
    assert result.exit_code == 0, result.output
    assert result.output.strip() == pkg_version("khub")


@pytest.mark.integration
def test_workspace_option_targets_path(
    seeded: Path, tmp_path_factory: pytest.TempPathFactory, monkeypatch: pytest.MonkeyPatch
) -> None:
    outside = tmp_path_factory.mktemp("outside")  # no .khub above it
    monkeypatch.chdir(outside)

    # Without -C the working directory has no workspace: a clean located failure.
    bare = runner.invoke(app, ["status", "--format", "json"])
    assert bare.exit_code == 1

    # With -C it runs against the named workspace regardless of cwd.
    scoped = runner.invoke(app, ["-C", str(seeded), "status", "--format", "json"])
    assert scoped.exit_code == 0, scoped.output
    data = json.loads(scoped.output)
    assert data["counts"]["client"] == 1 and data["counts"]["person"] == 1


# --- fix 2: log --format json on a no-git workspace -------------------------


@pytest.mark.integration
def test_log_json_no_git_is_valid_json(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(fresh_ws)  # init_workspace does not git-init
    result = runner.invoke(app, ["log", "--format", "json"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {"entries": [], "git_available": False}


# --- fix 3: qualified id contract -------------------------------------------


@pytest.mark.integration
def test_add_get_query_emit_qualified_id(seeded: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(seeded)

    added = runner.invoke(app, ["add", "client", "--name", "Beta", "--format", "json"])
    assert added.exit_code == 0, added.output
    rec = json.loads(added.output)
    assert (rec["id"], rec["type"], rec["slug"]) == ("client/beta", "client", "beta")

    got = runner.invoke(app, ["get", "client/acme", "--format", "json"])
    assert got.exit_code == 0, got.output
    grec = json.loads(got.output)
    assert (grec["id"], grec["type"], grec["slug"]) == ("client/acme", "client", "acme")

    queried = runner.invoke(app, ["query", "--type", "opportunity", "--format", "json"])
    assert queried.exit_code == 0, queried.output
    qrec = json.loads(queried.output)[0]
    assert (qrec["id"], qrec["type"], qrec["slug"]) == (
        "opportunity/acme-pov", "opportunity", "acme-pov",
    )

    edited = runner.invoke(app, ["edit", "client/acme", "title", "Acme Inc", "--format", "json"])
    assert edited.exit_code == 0, edited.output
    erec = json.loads(edited.output)
    assert (erec["id"], erec["type"], erec["slug"]) == ("client/acme", "client", "acme")


# --- fix 4: get --format table downgrades to JSON on a pipe -----------------


@pytest.mark.integration
def test_get_table_downgrades_to_json_on_pipe(seeded: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(seeded)
    result = runner.invoke(app, ["get", "client/acme", "--format", "table"])
    assert result.exit_code == 0, result.output
    # A Rich box-table would not parse; the piped output must be the JSON record.
    assert json.loads(result.output)["id"] == "client/acme"


# --- fix 5: idempotent link / unlink ----------------------------------------


@pytest.mark.integration
def test_link_noop_reports_already_present(seeded: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(seeded)
    first = runner.invoke(app, ["link", "acme-pov", "related", "acme"])
    assert first.exit_code == 0, first.output
    assert "Linked" in first.output

    again = runner.invoke(app, ["link", "acme-pov", "related", "acme"])
    assert again.exit_code == 0, again.output
    assert again.output.strip() == "Edge already present"


@pytest.mark.integration
def test_unlink_noop_reports_no_edge(seeded: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(seeded)
    result = runner.invoke(app, ["unlink", "acme-pov", "related", "acme"])
    assert result.exit_code == 0, result.output
    assert result.output.strip() == "No edge related -> acme on acme-pov"


# --- fix 7: --both is gone ---------------------------------------------------


@pytest.mark.integration
def test_neighbors_rejects_removed_both_flag(seeded: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(seeded)
    result = runner.invoke(app, ["neighbors", "acme-pov", "--both"])
    assert result.exit_code == 2  # unknown option
    assert "--both" in result.output


# --- fix 12: malformed key rendered in check json ---------------------------


@pytest.mark.integration
def test_check_json_carries_malformed_key(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["check", "--format", "json"])
    data = json.loads(result.output)
    assert "malformed" in data and isinstance(data["malformed"], list)


# --- contract d: stale --days defaults to the workspace stale_days (90) ------


@pytest.mark.integration
def test_stale_default_uses_config_threshold(fresh_ws: Path, seed: Seed, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(fresh_ws)
    old = date.today() - timedelta(days=60)  # older than 30, younger than the config 90
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme",
         created=old, updated=old)

    default = runner.invoke(app, ["stale", "--format", "json"])
    assert default.exit_code == 0, default.output
    assert json.loads(default.output) == []  # 60 < stale_days(90): not stale by default

    tightened = runner.invoke(app, ["stale", "--days", "30", "--format", "json"])
    assert tightened.exit_code == 0, tightened.output
    ids = [e["id"] for e in json.loads(tightened.output)]
    assert "client/acme" in ids  # 60 > 30: stale under the override


# --- review-round fixes (code-review findings on the audit batch) -------------


@pytest.mark.integration
def test_schema_commands_clean_error_on_corrupt_schema(
    fresh_ws: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """`schema`/`schema types`/`schema edges` render a corrupt schema.yaml as a clean error."""
    monkeypatch.chdir(fresh_ws)
    (fresh_ws / ".khub" / "schema.yaml").write_text("this: [is: not: valid\n  bad\n")
    for args in (["schema"], ["schema", "types"], ["schema", "edges"]):
        result = runner.invoke(app, args)
        assert result.exit_code == 1, args
        assert "Traceback" not in (result.output or "")
        assert result.exception is None or isinstance(result.exception, SystemExit)


@pytest.mark.integration
def test_stray_field_token_is_a_usage_error(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """A trailing field token with no value errors instead of becoming a silent ''-filter."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["query", "--type", "client", "foo"])
    assert result.exit_code != 0
    assert "no value" in result.output


@pytest.mark.integration
def test_log_json_one_shape_with_git(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """`log --format json` emits {"entries": [...], "git_available": true} on a git workspace."""
    import subprocess

    monkeypatch.chdir(fresh_ws)
    runner.invoke(app, ["add", "client", "--name", "Acme"])
    subprocess.run(["git", "init", "-q", "."], check=True)
    subprocess.run(["git", "add", "-A"], check=True)
    subprocess.run(
        ["git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "seed"], check=True
    )
    payload = json.loads(runner.invoke(app, ["log", "--format", "json"]).output)
    assert payload["git_available"] is True
    assert isinstance(payload["entries"], list) and payload["entries"]
