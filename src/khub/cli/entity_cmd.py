"""Authoring commands — thin Typer adapters over ``khub.core.entity`` (FS-002).

``add`` and ``get`` here; ``edit``/``link``/``unlink``/``remove`` join in
WPK-002-2/3. ``add`` and ``edit`` accept arbitrary per-type ``--field value``
pairs, so their commands run with ``ignore_unknown_options`` and parse the schema
fields out of the extra args (the known flags — ``--id``, ``--strict``,
``--format`` — still bind normally).
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

import typer
from rich.console import Console
from rich.table import Table

from khub.core.entity import (
    CreateResult,
    EntityView,
    LinkResult,
    UpdateResult,
    create,
    delete,
    get,
    link,
    unlink,
    update,
)
from khub.core.errors import LocatedError
from khub.core.locate import find_workspace

# Shared context settings for commands that take dynamic --field value pairs.
DYNAMIC_FIELDS = {"allow_extra_args": True, "ignore_unknown_options": True}


def add_command(
    ctx: typer.Context,
    type_: str = typer.Argument(..., metavar="TYPE", help="The entity type to create."),
    id_: str = typer.Option(None, "--id", help="Explicit slug (else minted from name/type)."),
    draft: bool = typer.Option(False, "--draft", help="Mark the entity unpublished (default: active)."),
    strict: bool = typer.Option(False, "--strict", help="Reject fields the schema does not declare."),
    body_file: str = typer.Option(None, "--body-file", help="Read the body from a file ('-' for stdin)."),
    fmt: str = typer.Option("text", "--format", help="text or json (emits the written record)."),
) -> None:
    """Create an entity: khub add opportunity --client initech --owner noor --stage prospect."""
    fields = _parse_fields(ctx.args)
    body = _read_body(body_file)
    try:
        root = find_workspace(Path.cwd())
        result = create(root, type_, fields, id_=id_, strict=strict, body=body, draft=draft)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if fmt == "json":
        typer.echo(json.dumps(_create_record(root, result), default=str))
    else:
        typer.echo(str(result.path.relative_to(root)))
        typer.echo(_created_message(result))


def get_command(
    id_: str = typer.Argument(..., metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    edges: bool = typer.Option(False, "--edges", help="Include stored and derived edges."),
    fmt: str = typer.Option("text", "--format", help="json, table, raw, or text (Rich on a TTY)."),
) -> None:
    """Read an entity's frontmatter and body, optionally with derived edges."""
    try:
        root = find_workspace(Path.cwd())
        view = get(root, id_, edges=edges)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if fmt == "raw":
        typer.echo(view.raw, nl=False)
    elif fmt == "json":
        typer.echo(json.dumps(_get_record(root, view), default=str))
    elif fmt == "table" or Console().is_terminal:
        Console().print(_entity_table(view))
    else:
        typer.echo(json.dumps(_get_record(root, view), default=str))


def edit_command(
    ctx: typer.Context,
    id_: str = typer.Argument(..., metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    strict: bool = typer.Option(False, "--strict", help="Reject fields the schema does not declare."),
    body_file: str = typer.Option(None, "--body-file", help="Replace the body from a file ('-' for stdin)."),
    fmt: str = typer.Option("text", "--format", help="text or json (emits the updated record)."),
) -> None:
    """Edit an entity: khub edit initech-deal stage proposal-sent  (or --field value)."""
    fields = _parse_edit(ctx.args)
    body = None if body_file is None else _read_body(body_file)
    try:
        root = find_workspace(Path.cwd())
        result = update(root, id_, fields, strict=strict, body=body)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if fmt == "json":
        typer.echo(json.dumps(_update_record(root, result), default=str))
    else:
        typer.echo(f"Updated {result.type} '{result.slug}'")


def link_command(
    id_: str = typer.Argument(..., metavar="ID"),
    predicate: str = typer.Argument(..., metavar="PREDICATE"),
    target: str = typer.Argument(..., metavar="TARGET"),
) -> None:
    """Add a relation: khub link initech-pov partner northwind."""
    try:
        root = find_workspace(Path.cwd())
        result = link(root, id_, predicate, target)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None
    typer.echo(_edge_message("Linked", result))


def unlink_command(
    id_: str = typer.Argument(..., metavar="ID"),
    predicate: str = typer.Argument(..., metavar="PREDICATE"),
    target: str = typer.Argument(..., metavar="TARGET"),
) -> None:
    """Remove a relation: khub unlink initech-pov partner northwind."""
    try:
        root = find_workspace(Path.cwd())
        result = unlink(root, id_, predicate, target)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None
    typer.echo(_edge_message("Unlinked", result))


def remove_command(
    id_: str = typer.Argument(..., metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    force: bool = typer.Option(False, "--force", help="Delete despite inbound edges (leaves them dangling)."),
) -> None:
    """Remove an entity, guarded by inbound edges: khub remove old-fragment [--force]."""
    try:
        root = find_workspace(Path.cwd())
        result = delete(root, id_, force=force)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if result.removed:
        typer.echo(f"Removed {result.type} '{result.slug}'")
        return
    typer.echo(
        LocatedError.inbound_edge_refusal(result.type, result.slug, len(result.inbound)).message,
        err=True,
    )
    for edge in result.inbound:
        typer.echo(f"  {edge.source_type}/{edge.source_slug} --{edge.predicate}-->", err=True)
    raise typer.Exit(1)


# --- helpers -----------------------------------------------------------------


def _parse_fields(extra: list[str]) -> dict[str, str]:
    """Parse leftover ``--field value`` / ``--field=value`` args into a field map.

    A ``--call-type client`` arrives as ``{'call_type': 'client'}`` — CLI dashes
    become schema underscores.
    """
    fields: dict[str, str] = {}
    i = 0
    while i < len(extra):
        token = extra[i]
        if not token.startswith("--"):
            raise typer.BadParameter(f"Expected --field, got '{token}'")
        key = token[2:]
        if "=" in key:
            key, value = key.split("=", 1)
        else:
            i += 1
            value = extra[i] if i < len(extra) else ""
        fields[key.replace("-", "_")] = value
        i += 1
    return fields


def _parse_edit(extra: list[str]) -> dict[str, str]:
    """Parse edit args, accepting both ``field value`` pairs and ``--field value``."""
    fields: dict[str, str] = {}
    i = 0
    while i < len(extra):
        token = extra[i]
        if token.startswith("--"):
            key = token[2:]
            if "=" in key:
                key, value = key.split("=", 1)
            else:
                i += 1
                value = extra[i] if i < len(extra) else ""
        else:
            key = token
            i += 1
            value = extra[i] if i < len(extra) else ""
        fields[key.replace("-", "_")] = value
        i += 1
    return fields


def _read_body(body_file: str | None) -> str:
    if body_file is None:
        return ""
    if body_file == "-":
        return sys.stdin.read()
    return Path(body_file).read_text()


def _created_message(result: CreateResult) -> str:
    state = "draft" if result.draft else "active"
    return f"Created {result.type} '{result.slug}' ({state})"


def _create_record(root: Path, result: CreateResult) -> dict[str, Any]:
    return {
        "id": result.slug,
        "type": result.type,
        "path": str(result.path.relative_to(root)),
        "draft": result.draft,
    }


def _update_record(root: Path, result: UpdateResult) -> dict[str, Any]:
    return {
        "id": result.slug,
        "type": result.type,
        "path": str(result.path.relative_to(root)),
        "draft": result.draft,
    }


def _edge_message(verb: str, result: LinkResult) -> str:
    return f"{verb} {result.slug} --{result.predicate}--> {result.target}"


def _get_record(root: Path, view: EntityView) -> dict[str, Any]:
    record: dict[str, Any] = {
        "id": view.slug,
        "type": view.type,
        "path": str(view.path.relative_to(root)),
        "frontmatter": view.meta,
        "body": view.body,
    }
    if view.edges is not None:
        record["edges"] = [
            {"predicate": e.predicate, "target": e.target, "derived": e.derived}
            for e in view.edges
        ]
    return record


def _entity_table(view: EntityView) -> Table:
    table = Table(title=f"{view.type}/{view.slug}")
    table.add_column("field")
    table.add_column("value")
    for key, value in view.meta.items():
        table.add_row(str(key), str(value))
    if view.edges is not None:
        table.add_section()
        for edge in view.edges:
            kind = "derived" if edge.derived else "stored"
            table.add_row(f"{edge.predicate} ({kind})", edge.target)
    if view.body.strip():
        table.add_section()
        table.add_row("body", view.body.strip())
    return table
