"""``khub compile`` — operator trigger for ``core.compile_schema`` (WPK-000-2). Thin wiring."""

from __future__ import annotations

from pathlib import Path

import typer
from rich.console import Console
from rich.markup import escape

from khub.core.errors import LocatedError


def compile_command(
    ctx: typer.Context,
    schema: Path | None = typer.Option(
        None, "--schema", help="Path to the schema.yaml (default: the workspace's)."
    ),
    out: Path | None = typer.Option(
        None, "--out", help="Output directory (default: the workspace's .khub/generated)."
    ),
) -> None:
    """Compile the khub schema into .khub/generated/ (LinkML, Pydantic v2, JSON Schema)."""
    from khub.cli._render import resolve_root
    from khub.core.compile import compile_schema

    try:
        # Default paths come from the resolved workspace, so -C/--workspace works
        # here like everywhere else; explicit --schema/--out still win.
        if schema is None or out is None:
            root = resolve_root(ctx)
            schema = schema if schema is not None else root / ".khub" / "schema.yaml"
            out = out if out is not None else root / ".khub" / "generated"
        if not schema.exists():
            raise LocatedError(
                code="schema_error", message=f"No schema file at {schema}"
            )
        result = compile_schema(schema, out)
    except LocatedError as err:
        # escape(): an error message may carry literal brackets (e.g. `khub[compile]`)
        # that Rich would otherwise swallow as markup tags.
        Console(stderr=True).print(f"[red]compile failed:[/] {escape(err.message)}")
        raise typer.Exit(1) from None
    Console().print(f"[green]compiled[/] {len(result.artifacts)} artifacts into {result.out_dir}")
