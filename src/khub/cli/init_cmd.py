"""``khub init`` — operator trigger for ``core.init_workspace`` (WPK-001-1). Thin wiring.

After scaffolding, ``init`` wires the workspace into ``CLAUDE.md`` and installs the
khub agent skill (agnostically, via ``npx skills``), so a fresh workspace is
agent-ready in one command. Both tails are best-effort and independently skippable
(``--no-wire`` / ``--no-skill``); neither unwinds a successful scaffold.
"""

from __future__ import annotations

import dataclasses
import json
from pathlib import Path

import typer

from khub.core.errors import LocatedError
from khub.core.skill import SKILLS_SOURCE, SkillOutcome


def init_command(
    preset: str = typer.Argument(..., help="Named preset to seed from (e.g. firm-ops)."),
    path: Path = typer.Argument(Path("."), help="Target directory (default: .)."),
    preset_source: Path = typer.Option(
        None, "--preset-source", help="Where to resolve the preset if not packaged with khub."
    ),
    name: str = typer.Option(None, "--name", help="Workspace name (default: the target dir name)."),
    force: bool = typer.Option(False, "--force", help="Scaffold into a non-empty target."),
    no_wire: bool = typer.Option(False, "--no-wire", help="Skip wiring the schema into CLAUDE.md."),
    no_skill: bool = typer.Option(
        False, "--no-skill", help="Skip installing the agent skill via npx skills."
    ),
    fmt: str = typer.Option(
        "text", "--format", help="text confirmation (default); json emits resolved provenance."
    ),
) -> None:
    """Scaffold a workspace from a preset, then wire it and install the agent skill."""
    from khub.core.skill import install_skill
    from khub.core.wire import wire
    from khub.core.workspace import init_workspace  # heavy (compile path); imported lazily

    try:
        result = init_workspace(
            preset, path, preset_source=preset_source, name=name, force=force
        )
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    # Best-effort tails: a wire/skill hiccup does not unwind the scaffold above.
    wire_result = None
    wire_error = None
    if not no_wire:
        try:
            wire_result = wire(result.path)
        except LocatedError as err:
            wire_error = err.message
        except OSError as err:  # e.g. a read-only CLAUDE.md; report, don't unwind
            wire_error = str(err)
    # In json mode capture npx output so it never precedes the JSON document.
    skill_outcome = None if no_skill else install_skill(result.path, quiet=(fmt == "json"))

    if fmt == "json":
        payload = dataclasses.asdict(result)
        if wire_result is not None:
            payload["wire"] = [dataclasses.asdict(o) for o in wire_result.outcomes]
        if wire_error is not None:
            payload["wire_error"] = wire_error
        if skill_outcome is not None:
            payload["skill"] = dataclasses.asdict(skill_outcome)
        typer.echo(json.dumps(payload, default=str))
        return

    if result.seeded_over_corpus:
        typer.echo(
            f"Initialized {result.preset} workspace; "
            f"{result.entity_files_modified} entity files modified"
        )
    else:
        typer.echo(f"Initialized {result.preset} workspace at {result.path}")
    if not result.compiled:
        typer.echo(
            "Generated artifacts skipped (no LinkML backend); "
            "install khub[compile] and run `khub compile`"
        )
    if wire_result is not None:
        for outcome in wire_result.outcomes:
            typer.echo(f"{outcome.action} {outcome.path.name}")
    if wire_error is not None:
        typer.echo(f"wire skipped: {wire_error}", err=True)
    if skill_outcome is not None:
        # A failure is an error stream; installed/skipped are informational (stdout).
        typer.echo(_skill_line(skill_outcome), err=(skill_outcome.action == "failed"))


def _skill_line(outcome: SkillOutcome) -> str:
    """A human line for the skill-install outcome."""
    if outcome.action == "installed":
        return "installed khub agent skill (npx skills)"
    if outcome.action == "skipped-no-npx":
        return "skill install skipped: npx not found (install Node, or run `npx skills add …`)"
    return f"skill install failed: run `npx skills add {SKILLS_SOURCE} -s khub -s setup` by hand"
