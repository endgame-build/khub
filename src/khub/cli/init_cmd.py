"""``khub init`` — operator trigger for ``core.init_workspace`` (WPK-001-1). Thin wiring.

After scaffolding, ``init`` wires the workspace into the selected agent context files
(``CLAUDE.md`` / ``AGENTS.md``, both by default). The wire tail is best-effort and
skippable (``--no-wire``); it never unwinds a successful scaffold.

Until 0.9.0 ``init`` also shelled out to ``npx skills`` to install the agent skill,
which made scaffolding depend on Node and on SSH access to the skill repo. That is
now ``khub install-skills``; ``init`` ends by naming it.
"""

from __future__ import annotations

import dataclasses
from pathlib import Path

import typer

from khub.cli._render import emit, guard
from khub.core.errors import LocatedError

# Printed after a scaffold, and carried as `skill_hint` in the JSON payload, so an
# agent driving `init --format json` learns the follow-up without parsing prose.
SKILL_HINT = "khub install-skills"


def _skill_hint(scaffolded: Path) -> str:
    """The follow-up command, aimed at the workspace that was just scaffolded.

    ``install-skills`` resolves its root by walking up from the working directory, so a
    bare hint after ``khub init firm-ops ./my-hub`` would either find no workspace or —
    worse — find an unrelated one above cwd. Point it at the target unless that target
    IS cwd.
    """
    if scaffolded.resolve() == Path.cwd().resolve():
        return SKILL_HINT
    return f"khub -C {scaffolded} install-skills"



@guard
def init_command(
    ctx: typer.Context,
    preset: str | None = typer.Argument(None, help="Named preset to seed from (e.g. firm-ops)."),
    path: Path | None = typer.Argument(None, help="Target directory (default: .)."),
    preset_source: Path = typer.Option(
        None, "--preset-source", help="Where to resolve the preset if not packaged with khub."
    ),
    name: str = typer.Option(None, "--name", help="Workspace name (default: the target dir name)."),
    force: bool = typer.Option(False, "--force", help="Scaffold into a non-empty target."),
    no_wire: bool = typer.Option(
        False, "--no-wire", help="Skip wiring the schema into the agent files."
    ),
    fmt: str = typer.Option(
        "text", "--format", help="text confirmation (default); json emits resolved provenance."
    ),
) -> None:
    """Scaffold a workspace from a preset and wire it into the agent context files.

    A missing PRESET is a usage error; PATH defaults to the working directory.
    Installing the agent skill is a separate step: ``khub install-skills``.
    """
    from khub.core.wire import wire
    from khub.core.workspace import init_workspace  # heavy (compile path); imported lazily

    if preset is None:
        typer.echo("Missing argument 'PRESET' (e.g. firm-ops).", err=True)
        raise typer.Exit(2)

    do_wire = not no_wire
    result = init_workspace(preset, path or Path("."), preset_source=preset_source, name=name, force=force)

    # Best-effort tail: a wire hiccup does not unwind the scaffold above.
    wire_result = None
    wire_error = None
    if do_wire:
        try:  # no selection to make without a wizard: seed both agent files
            wire_result = wire(result.path, claude=True, agents=True)
        except LocatedError as err:
            wire_error = err.message
        except OSError as err:  # e.g. a read-only agent file; report, don't unwind
            wire_error = str(err)

    payload = dataclasses.asdict(result)
    if wire_result is not None:
        payload["wire"] = [dataclasses.asdict(o) for o in wire_result.outcomes]
    if wire_error is not None:
        payload["wire_error"] = wire_error
    payload["skill_hint"] = _skill_hint(result.path)

    def human() -> None:
        if result.seeded_over_corpus:
            typer.echo(
                f"Initialized {result.preset} workspace; "
                f"{result.entity_files_modified} entity files modified"
            )
        else:
            typer.echo(f"Initialized {result.preset} workspace at {result.path}")
        if result.singletons_created:
            typer.echo("created singletons: " + ", ".join(result.singletons_created))
        if result.preserved:
            # Say it out loud: a re-init leaves the workspace's own schema/templates in
            # place, so nobody has to wonder whether --force just restored the preset.
            typer.echo(f"preserved {len(result.preserved)} workspace-owned file(s): "
                       + ", ".join(result.preserved[:3])
                       + (" …" if len(result.preserved) > 3 else ""))
        if wire_result is not None:
            for outcome in wire_result.outcomes:
                typer.echo(f"{outcome.action} {outcome.path.name}")
        if wire_error is not None:
            typer.echo(f"wire skipped: {wire_error}", err=True)
        typer.echo(f"\nAgent skill not installed. To install:\n  {_skill_hint(result.path)}")

    emit(payload, fmt, human)
