"""Authoring commands — thin Typer adapters over ``khub.core.entity`` (FS-002).

``add`` and ``get`` here; ``edit``/``link``/``unlink``/``remove`` join in
WPK-002-2/3. ``add`` and ``edit`` accept arbitrary per-type ``--field value``
pairs, so their commands run with ``ignore_unknown_options`` and parse the schema
fields out of the extra args (the known flags — ``--id``, ``--strict``,
``--format`` — still bind normally).

A missing required argument is a usage error (exit 2). These commands opened an
interactive wizard for it until 0.9.0; khub no longer prompts anywhere.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

import typer
from rich.console import Console
from rich.table import Table

from khub.cli._render import emit, guard, resolve_root
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

# Shared context settings for commands that take dynamic --field value pairs.
DYNAMIC_FIELDS = {"allow_extra_args": True, "ignore_unknown_options": True}


@guard
def add_command(
    ctx: typer.Context,
    type_: str | None = typer.Argument(None, metavar="TYPE", help="The entity type to create."),
    id_: str = typer.Option(None, "--id", help="Explicit slug (else minted from name/type)."),
    draft: bool = typer.Option(False, "--draft", help="Mark the entity unpublished (default: active)."),
    strict: bool = typer.Option(False, "--strict", help="Reject fields the schema does not declare."),
    body_text: str = typer.Option(None, "--body", help="Body prose as a string."),
    body_file: str = typer.Option(None, "--body-file", help="Read the body from a file ('-' for stdin)."),
    no_template: bool = typer.Option(
        False, "--no-template", help="Start with an empty body even when the type has a template."
    ),
    fmt: str = typer.Option("text", "--format", help="text or json (emits the written record)."),
) -> None:
    """Create an entity: khub add opportunity --client initech --owner noor --stage prospect.

    A missing TYPE is a usage error; every field is a flag.
    """
    root = resolve_root(ctx)
    fields = parse_fields(ctx.args)
    body = _pick_body(body_text, body_file) or ""

    if type_ is None:
        typer.echo("Missing argument 'TYPE' (e.g. project).", err=True)
        raise typer.Exit(2)

    result = create(
        root, type_, fields, id_=id_, strict=strict, body=body, draft=draft,
        use_template=not no_template,
    )

    if fmt == "json":
        typer.echo(json.dumps(_ref_record(root, result), default=str))
    else:
        typer.echo(str(result.path.relative_to(root)))
        typer.echo(_created_message(result))


@guard
def get_command(
    ctx: typer.Context,
    id_: str | None = typer.Argument(None, metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    edges: bool = typer.Option(False, "--edges", help="Include stored and derived edges."),
    fmt: str = typer.Option("text", "--format", help="json, table, raw, or text (Rich on a TTY)."),
) -> None:
    """Read an entity's frontmatter and body, optionally with derived edges."""
    root = resolve_root(ctx)
    if id_ is None:
        typer.echo("Missing argument 'ID'.", err=True)
        raise typer.Exit(2)
    view = get(root, id_, edges=edges)

    if fmt == "raw":
        typer.echo(view.raw, nl=False)
        return
    emit(_get_record(root, view), fmt, lambda: Console().print(_entity_table(view)))


@guard
def edit_command(
    ctx: typer.Context,
    id_: str | None = typer.Argument(None, metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    strict: bool = typer.Option(False, "--strict", help="Reject fields the schema does not declare."),
    body_text: str = typer.Option(None, "--body", help="Replace the body with this string ('' clears it)."),
    body_file: str = typer.Option(None, "--body-file", help="Replace the body from a file ('-' for stdin)."),
    fmt: str = typer.Option("text", "--format", help="text or json (emits the updated record)."),
) -> None:
    """Edit an entity: khub edit initech-deal stage proposal-sent  (or --field value).

    A missing ID is a usage error; the field and value are positional or flags.
    """
    root = resolve_root(ctx)
    fields = parse_fields(ctx.args)
    body = _pick_body(body_text, body_file)

    if id_ is None:
        typer.echo("Missing argument 'ID'.", err=True)
        raise typer.Exit(2)

    result = update(root, id_, fields, strict=strict, body=body)

    if fmt == "json":
        typer.echo(json.dumps(_ref_record(root, result), default=str))
    else:
        typer.echo(f"Updated {result.type} '{result.slug}'")


@guard
def link_command(
    ctx: typer.Context,
    id_: str | None = typer.Argument(None, metavar="ID"),
    predicate: str | None = typer.Argument(None, metavar="PREDICATE"),
    target: str | None = typer.Argument(None, metavar="TARGET"),
) -> None:
    """Add a relation: khub link initech-pov partner northwind."""
    root = resolve_root(ctx)
    id_, predicate, target = _edge_args(id_, predicate, target)
    result = link(root, id_, predicate, target)
    if not result.changed:  # the edge already existed — idempotent success (exit 0)
        typer.echo("Edge already present")
        return
    typer.echo(_edge_message("Linked", result))


@guard
def unlink_command(
    ctx: typer.Context,
    id_: str | None = typer.Argument(None, metavar="ID"),
    predicate: str | None = typer.Argument(None, metavar="PREDICATE"),
    target: str | None = typer.Argument(None, metavar="TARGET"),
) -> None:
    """Remove a relation: khub unlink initech-pov partner northwind."""
    root = resolve_root(ctx)
    id_, predicate, target = _edge_args(id_, predicate, target)
    result = unlink(root, id_, predicate, target)
    if not result.changed:  # no such edge — idempotent no-op success (exit 0)
        typer.echo(f"No edge {result.predicate} -> {result.target} on {result.slug}")
        return
    typer.echo(_edge_message("Unlinked", result))


@guard
def remove_command(
    ctx: typer.Context,
    id_: str | None = typer.Argument(None, metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    force: bool = typer.Option(False, "--force", help="Delete despite inbound edges (leaves them dangling)."),
    fmt: str = typer.Option("text", "--format", help="text or json (emits the removed record)."),
) -> None:
    """Remove an entity, guarded by inbound edges: khub remove old-fragment [--force]."""
    root = resolve_root(ctx)
    if id_ is None:
        typer.echo("Missing argument 'ID'.", err=True)
        raise typer.Exit(2)
    result = delete(root, id_, force=force)

    if result.removed:
        if fmt == "json":
            typer.echo(json.dumps({
                "id": f"{result.type}/{result.slug}",
                "type": result.type,
                "slug": result.slug,
                "removed": True,
            }, default=str))
        else:
            typer.echo(f"Removed {result.type} '{result.slug}'")
        return
    refusal = LocatedError.inbound_edge_refusal(result.type, result.slug, len(result.inbound))
    if fmt == "json":
        # A refusal is a failure: let `guard` render the shared {"error": …} envelope
        # rather than a bespoke payload no other command emits.
        raise refusal
    typer.echo(refusal.message, err=True)
    for edge in result.inbound:
        typer.echo(f"  {edge.source_type}/{edge.source_slug} --{edge.predicate}-->", err=True)
    raise typer.Exit(1)


# --- helpers -----------------------------------------------------------------


def _edge_args(
    id_: str | None, predicate: str | None, target: str | None
) -> tuple[str, str, str]:
    """An edge needs all three parts; a gap is a usage error (exit 2), never a prompt."""
    if id_ is None or predicate is None or target is None:
        typer.echo("Provide ID PREDICATE TARGET.", err=True)
        raise typer.Exit(2)
    return id_, predicate, target


def parse_fields(extra: list[str]) -> dict[str, str]:
    """Parse leftover args into a field map — the one parser for add/edit/query.

    Accepts three shapes interchangeably: ``--field value``, ``--field=value``, and a
    bare ``field value`` positional pair (so ``edit initech-deal stage proposal-sent``
    works). CLI dashes become schema underscores, so ``--call-type client`` arrives as
    ``{'call_type': 'client'}``.
    """
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
                if i >= len(extra):  # a trailing key with no value is a usage error,
                    raise typer.BadParameter(f"Field '{key}' has no value")
                value = extra[i]
        else:
            key = token
            i += 1
            if i >= len(extra):  # not a silent empty-string field (stray token)
                raise typer.BadParameter(f"Field '{key}' has no value")
            value = extra[i]
        fields[key.replace("-", "_")] = value
        i += 1
    return fields


def _pick_body(body_text: str | None, body_file: str | None) -> str | None:
    """The body from ``--body`` or ``--body-file`` — one source, never both.

    None means "no body supplied" (add writes empty, edit keeps the current body);
    ``--body ""`` is an explicit empty body (edit clears it).
    """
    if body_text is not None and body_file is not None:
        raise typer.BadParameter("Pass --body or --body-file, not both")
    if body_text is not None:
        return body_text
    if body_file is None:
        return None
    if body_file == "-":
        return sys.stdin.read()
    return Path(body_file).read_text()


def _created_message(result: CreateResult) -> str:
    state = "draft" if result.draft else "active"
    return f"Created {result.type} '{result.slug}' ({state})"


def _ref_record(root: Path, result: CreateResult | UpdateResult) -> dict[str, Any]:
    """The ``type/slug`` reference record ``add`` and ``edit`` emit — one builder."""
    record: dict[str, Any] = {
        "id": f"{result.type}/{result.slug}",
        "type": result.type,
        "slug": result.slug,
        "path": str(result.path.relative_to(root)),
        "draft": result.draft,
    }
    if result.locator:  # collection rows only — per-item records stay byte-identical
        record["locator"] = result.locator
    return record


def _edge_message(verb: str, result: LinkResult) -> str:
    return f"{verb} {result.slug} --{result.predicate}--> {result.target}"


def _get_record(root: Path, view: EntityView) -> dict[str, Any]:
    record: dict[str, Any] = {
        "id": f"{view.type}/{view.slug}",
        "type": view.type,
        "slug": view.slug,
        "path": str(view.path.relative_to(root)),
        "frontmatter": view.meta,
        "body": view.body,
    }
    if view.locator:  # collection rows only — per-item records stay byte-identical
        record["locator"] = view.locator
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
