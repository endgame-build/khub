"""``khub compile`` — operator trigger for ``core.compile_schema`` (WPK-000-2). Thin wiring."""

from __future__ import annotations

from pathlib import Path

import typer
from rich.console import Console

from khub.core.errors import LocatedError


def compile_command(
    schema: Path = typer.Option(
        Path(".khub/schema.yaml"), "--schema", help="Path to the workspace schema.yaml."
    ),
    out: Path = typer.Option(
        Path(".khub/generated"), "--out", help="Output directory for generated artifacts."
    ),
) -> None:
    """Compile the khub schema into .khub/generated/ (LinkML, Pydantic v2, JSON Schema)."""
    from khub.core.compile import compile_schema

    try:
        result = compile_schema(schema, out)
    except LocatedError as err:
        Console(stderr=True).print(f"[red]compile failed:[/] {err.message}")
        raise typer.Exit(1) from None
    Console().print(f"[green]compiled[/] {len(result.artifacts)} artifacts into {result.out_dir}")
