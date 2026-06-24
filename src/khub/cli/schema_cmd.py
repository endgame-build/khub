"""``khub schema`` — introspect the active schema (WPK-001-2). Thin wiring.

Bare `schema` shows the full effective schema; `types` lists names; `show <type>`
details one type; `edges` lists the relation vocabulary. Rich table on a TTY,
JSON otherwise.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

import typer
from rich.table import Table

from khub.cli._render import emit
from khub.core.errors import LocatedError
from khub.core.introspect import edges_view, load_schema, schema_view, type_view, types_list
from khub.core.locate import find_workspace, provenance

schema_app = typer.Typer(help="Introspect the active schema.")

FormatOpt = typer.Option("text", "--format", help="text (Rich table on a TTY) or json.")


def _workspace() -> Path:
    try:
        return find_workspace(Path.cwd())
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None


@schema_app.callback(invoke_without_command=True)
def schema_root(ctx: typer.Context, fmt: str = FormatOpt) -> None:
    """Show the full effective schema when no subcommand is given."""
    if ctx.invoked_subcommand is not None:
        return
    root = _workspace()
    emit(schema_view(load_schema(root), provenance(root)), fmt, _schema_table)


@schema_app.command("types")
def schema_types(fmt: str = FormatOpt) -> None:
    """List the declared type names."""
    root = _workspace()
    emit(types_list(load_schema(root)), fmt, _types_table)


@schema_app.command("show")
def schema_show(type: str = typer.Argument(..., help="Type name."), fmt: str = FormatOpt) -> None:
    """Detail one type: fields, enums, required flags, relations, layout."""
    root = _workspace()
    preset = provenance(root)["preset"]
    try:
        view = type_view(load_schema(root), type, preset)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None
    emit(view, fmt, _type_table)


@schema_app.command("edges")
def schema_edges(fmt: str = FormatOpt) -> None:
    """List the relation vocabulary by predicate."""
    root = _workspace()
    emit(edges_view(load_schema(root)), fmt, _edges_table)


def _schema_table(view: dict[str, Any]) -> Table:
    table = Table(title=f"schema: {view['provenance']['preset']}@{view['provenance']['version']}")
    table.add_column("type")
    table.add_column("fields", justify="right")
    table.add_column("relations", justify="right")
    table.add_column("layout")
    for t in view["types"]:
        table.add_row(t["name"], str(len(t["fields"])), str(len(t["relations"])), t["layout"])
    return table


def _types_table(names: list[str]) -> Table:
    table = Table(title="types")
    table.add_column("type")
    for name in names:
        table.add_row(name)
    return table


def _type_table(view: dict[str, Any]) -> Table:
    table = Table(title=f"{view['name']} ({view['layout']})")
    table.add_column("field")
    table.add_column("type")
    table.add_column("required")
    table.add_column("enum")
    for f in view["fields"]:
        table.add_row(
            f["name"], f["type"], "✓" if f["required"] else "", ", ".join(f["enum"] or [])
        )
    for r in view["relations"]:
        table.add_row(
            r["predicate"], "→ " + ", ".join(r["to"]), "✓" if r["required"] else "", r["kind"]
        )
    return table


def _edges_table(edges: list[dict[str, Any]]) -> Table:
    table = Table(title="edges")
    table.add_column("predicate")
    table.add_column("from")
    table.add_column("to")
    table.add_column("card")
    table.add_column("required")
    for e in edges:
        table.add_row(
            e["predicate"],
            ", ".join(e["from"]),
            ", ".join(e["to"]),
            "many" if e["many"] else "one",
            "✓" if e["required"] else "",
        )
    return table
