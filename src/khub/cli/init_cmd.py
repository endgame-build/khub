"""``khub init`` — operator trigger for ``core.init_workspace`` (WPK-001-1). Thin wiring."""

from __future__ import annotations

import dataclasses
import json
from pathlib import Path

import typer

from khub.core.errors import LocatedError


def init_command(
    preset: str = typer.Argument(..., help="Named preset to seed from (e.g. firm-ops)."),
    path: Path = typer.Argument(Path("."), help="Target directory (default: .)."),
    preset_source: Path = typer.Option(
        None, "--preset-source", help="Where to resolve the preset if not packaged with khub."
    ),
    name: str = typer.Option(None, "--name", help="Workspace name (default: the target dir name)."),
    force: bool = typer.Option(False, "--force", help="Scaffold into a non-empty target."),
    fmt: str = typer.Option(
        "text", "--format", help="text confirmation (default); json emits resolved provenance."
    ),
) -> None:
    """Scaffold a workspace from a preset: flatten, compile, stamp provenance, lay down the tree."""
    from khub.core.workspace import init_workspace

    try:
        result = init_workspace(
            preset, path, preset_source=preset_source, name=name, force=force
        )
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if fmt == "json":
        typer.echo(json.dumps(dataclasses.asdict(result), default=str))
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
