"""Machine-facing CLI contract — the audit-2026-07-05 fixes.

Exercises the thin Typer adapter through ``CliRunner`` (no TTY, so reads emit JSON):
global options (``-C/--workspace``, ``--version``), the no-git ``log`` JSON payload,
the unified qualified ``id`` contract, ``get`` downgrading a piped table to JSON, the
removed ``--both`` flag, defensive ``malformed`` rendering, config-driven ``stale``
default, and the idempotent link/unlink no-op messages.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from datetime import date, timedelta
from pathlib import Path

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
    first = runner.invoke(app, ["link", "acme-pov", "related", "acme"], env={"FORCE_COLOR": "1"})
    assert first.exit_code == 0, first.output
    assert "Linked" in first.output

    again = runner.invoke(app, ["link", "acme-pov", "related", "acme"], env={"FORCE_COLOR": "1"})
    assert again.exit_code == 0, again.output
    assert again.output.strip() == "Edge already present"


@pytest.mark.integration
def test_unlink_noop_reports_no_edge(seeded: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(seeded)
    result = runner.invoke(app, ["unlink", "acme-pov", "related", "acme"], env={"FORCE_COLOR": "1"})
    assert result.exit_code == 0, result.output
    assert result.output.strip() == "No edge related -> acme on acme-pov"


# --- fix 7: --both is gone ---------------------------------------------------


@pytest.mark.integration
def test_neighbors_rejects_removed_both_flag(seeded: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(seeded)
    result = runner.invoke(app, ["neighbors", "acme-pov", "--both"])
    # Exit 2 is Click's "unknown option" — the robust contract. The rendered error text
    # wraps the flag token differently across terminals/CI, so don't assert on it.
    assert result.exit_code == 2


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


# --- surfaces removed in 0.9.0 ------------------------------------------------
# Each asserts the surface is gone AND that its absence is a clean usage error
# (exit 2), never a traceback and never a silently accepted no-op.


@pytest.mark.integration
def test_log_command_is_gone(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """`khub log` was removed in 0.9.0: `khub history` covers the graph side, `git log
    -- <path>` the rest. `stale` — the other git-derived read — is untouched."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["log"])
    assert result.exit_code == 2
    assert "No such command" in result.output
    assert runner.invoke(app, ["stale", "--format", "json"]).exit_code == 0


@pytest.mark.integration
def test_validate_fix_flag_is_gone(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """`--fix` was removed in 0.9.0; date repair is `khub backfill`, and validate never writes."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["validate", "--fix"])
    assert result.exit_code == 2
    assert "No such option" in result.output


@pytest.mark.integration
def test_validate_json_drops_fixed_key(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """The validate payload no longer carries `fixed` — nothing to report when nothing writes."""
    monkeypatch.chdir(fresh_ws)
    payload = json.loads(runner.invoke(app, ["validate", "--format", "json"]).output)
    assert "fixed" not in payload
    assert {"count", "errors"} <= payload.keys()


@pytest.mark.integration
def test_agent_flag_is_gone(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """`--agent` existed only to switch prompting off; with no prompts it has no meaning.

    Removed rather than kept as a silent no-op: a script still passing it should fail
    loudly, not appear to work.
    """
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["--agent", "status"])
    assert result.exit_code == 2
    assert "No such option" in result.output
    assert runner.invoke(app, ["status", "--format", "json"]).exit_code == 0


@pytest.mark.integration
@pytest.mark.parametrize(
    "argv, expected",
    [
        (["add"], "Missing argument 'TYPE'"),
        (["get"], "Missing argument 'ID'"),
        (["edit"], "Missing argument 'ID'"),
        (["remove"], "Missing argument 'ID'"),
        (["link", "acme"], "Provide ID PREDICATE TARGET."),
        (["unlink", "acme", "partner"], "Provide ID PREDICATE TARGET."),
    ],
)
def test_missing_input_is_a_usage_error(
    fresh_ws: Path, monkeypatch: pytest.MonkeyPatch, argv: list[str], expected: str
) -> None:
    """A missing input opened a wizard until 0.9.0; it is now the usage error it always
    was headlessly. Same message on a TTY and off it — there is one path now."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, argv)
    assert result.exit_code == 2
    assert expected in result.output


@pytest.mark.integration
@pytest.mark.parametrize("argv", [["add"], ["init"], ["get"], ["edit"]])
def test_missing_input_never_reads_stdin(fresh_ws: Path, argv: list[str]) -> None:
    """The no-hang guarantee, asserted the only way it can actually fail.

    CliRunner would pass even if the code still tried to read stdin, so this runs a real
    subprocess with stdin closed: a surviving prompt blocks forever instead of exiting.
    """
    import subprocess
    import sys

    result = subprocess.run(  # noqa: PLW1510 — the exit code IS the assertion below
        [sys.executable, "-m", "khub.cli.main", *argv],
        cwd=fresh_ws,
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 2, result.stderr


# --- failures are machine-readable under --format json (0.11.0) ----------------


@pytest.mark.integration
def test_failure_under_format_json_is_json(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """An agent that asked for machine output should not have to parse prose to learn
    what went wrong. `guard` echoed err.message regardless of --format."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["get", "ghost-entity", "--format", "json"])
    assert result.exit_code == 1
    payload = json.loads(result.output)
    assert payload["error"]["code"]
    assert "ghost-entity" in payload["error"]["message"]


@pytest.mark.integration
def test_failure_matches_the_success_output_gate(
    fresh_ws: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Failures use the same gate as successes: JSON under --format json OR on any
    pipe, prose on a TTY. Keying failures on --format alone meant a piped agent got a
    JSON record when the command worked and prose when it did not."""
    monkeypatch.chdir(fresh_ws)

    piped = runner.invoke(app, ["get", "ghost-entity"])  # CliRunner is not a TTY
    assert piped.exit_code == 1
    assert json.loads(piped.output)["error"]["code"] == "lookup_error"

    tty = runner.invoke(app, ["get", "ghost-entity"], env={"FORCE_COLOR": "1"})
    assert tty.exit_code == 1
    assert not tty.output.strip().startswith("{")


@pytest.mark.integration
def test_write_verbs_emit_json_on_a_pipe(seeded: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """Every write verb obeys the same output gate as the read verbs.

    Until 0.13.0 these gated on a literal ``fmt == "json"`` while reads used ``want_json``,
    so a piped agent got PROSE when the command succeeded and JSON when it failed — the
    exact asymmetry ``_fail`` exists to prevent. An agent never has a TTY, so this is the
    contract it actually consumes.
    """
    monkeypatch.chdir(seeded)

    added = runner.invoke(app, ["add", "client", "--name", "Beta", "--id", "beta"])
    assert added.exit_code == 0, added.output
    assert json.loads(added.output)["id"] == "client/beta"

    edited = runner.invoke(app, ["edit", "beta", "name", "Beta Inc"])
    assert edited.exit_code == 0, edited.output
    assert json.loads(edited.output)["slug"] == "beta"

    linked = runner.invoke(app, ["link", "acme-pov", "related", "beta"])
    assert linked.exit_code == 0, linked.output
    assert json.loads(linked.output) == {
        "id": "opportunity/acme-pov", "type": "opportunity", "slug": "acme-pov",
        "predicate": "related", "target": "beta", "changed": True,
    }

    # `changed` is the whole reason link/unlink needed a record: both are idempotent and
    # exit 0 either way, so prose alone could not distinguish a no-op from a write.
    again = runner.invoke(app, ["link", "acme-pov", "related", "beta"])
    assert json.loads(again.output)["changed"] is False

    unlinked = runner.invoke(app, ["unlink", "acme-pov", "related", "beta"])
    assert json.loads(unlinked.output)["changed"] is True

    removed = runner.invoke(app, ["remove", "beta"])
    assert removed.exit_code == 0, removed.output
    assert json.loads(removed.output)["removed"] is True


@pytest.mark.e2e
def test_init_emits_json_on_a_pipe(tmp_path: Path) -> None:
    """`init` shares the gate too — the first command an agent runs in a new workspace."""
    target = tmp_path / "hq"
    result = runner.invoke(app, ["init", "firm-ops", str(target), "--no-wire"])
    assert result.exit_code == 0, result.output
    payload = json.loads(result.output)
    assert payload["preset"] == "firm-ops"
    assert payload["skill_hint"].endswith("install-skills")


@pytest.mark.integration
def test_remove_accepts_format_json(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """`remove` was the one write verb with no --format, breaking a JSON-driven loop."""
    monkeypatch.chdir(fresh_ws)
    runner.invoke(app, ["add", "client", "--name", "Acme", "--id", "acme"])
    result = runner.invoke(app, ["remove", "acme", "--format", "json"])
    assert result.exit_code == 0, result.output
    payload = json.loads(result.output)
    assert payload == {"id": "client/acme", "type": "client", "slug": "acme", "removed": True}


@pytest.mark.integration
def test_check_payload_reports_strict(fresh_ws: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """`orphans` populated with passed=true is only interpretable if you know the mode."""
    monkeypatch.chdir(fresh_ws)
    assert json.loads(runner.invoke(app, ["check", "--format", "json"]).output)["strict"] is False
    strict = runner.invoke(app, ["check", "--strict", "--format", "json"])
    assert json.loads(strict.output)["strict"] is True
