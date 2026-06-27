"""``khub query`` — filter entities by frontmatter and derived edges (WPK-003-1).

A thin Typer adapter over ``core.query``. Like ``add``/``edit`` it runs with
``ignore_unknown_options`` so arbitrary ``--<field> value`` filters parse out of
the extra args (the known flags — ``--type``, ``--tag``, ``--has``, ``--missing``,
``--orphan``, ``--stale``, ``--draft``/``--active``, ``--limit``, ``--format`` —
still bind). Rich table on a TTY, JSON otherwise, bare ids under ``--format ids``.
"""

from __future__ import annotations

import json
from datetime import date
from pathlib import Path
from typing import Any

import typer
from rich.console import Console
from rich.table import Table

from khub.cli._render import want_json
from khub.cli.entity_cmd import DYNAMIC_FIELDS, parse_fields
from khub.core.errors import LocatedError
from khub.core.locate import find_workspace
from khub.core.query import Match, QueryFilters, query

__all__ = ["query_command", "DYNAMIC_FIELDS"]


def query_command(
    ctx: typer.Context,
    type_: str = typer.Option(None, "--type", help="Restrict to one entity type."),
    tag: str = typer.Option(None, "--tag", help="Keep entities carrying this tag."),
    has: str = typer.Option(None, "--has", help="Keep entities with a resolvable edge for the predicate."),
    missing: str = typer.Option(None, "--missing", help="Keep entities lacking a resolvable edge (gap finder)."),
    orphan: bool = typer.Option(False, "--orphan", help="Keep only orphan (edge-less) entities."),
    stale: bool = typer.Option(False, "--stale", help="Keep only stale entities."),
    draft: bool = typer.Option(False, "--draft", help="Isolate drafts."),
    active: bool = typer.Option(False, "--active", help="Exclude drafts."),
    limit: int = typer.Option(None, "--limit", help="Cap the returned set."),
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY), json, or ids."),
) -> None:
    """Filter entities: khub query --type opportunity --stage prospect --format json."""
    filters = QueryFilters(
        type=type_,
        fields=parse_fields(ctx.args),
        tag=tag,
        has=has,
        missing=missing,
        orphan=orphan,
        stale=stale,
        draft_only=draft,
        active_only=active,
        limit=limit,
    )
    try:
        root = find_workspace(Path.cwd())
        matches = query(root, filters, now=date.today())
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None
    _emit(matches, fmt)


def _emit(matches: list[Match], fmt: str) -> None:
    if fmt == "ids":  # bare ids for piping — independent of TTY detection
        for m in matches:
            typer.echo(m.slug)
        return
    records = [_record(m) for m in matches]
    if want_json(fmt):
        typer.echo(json.dumps(records))
    elif not matches:
        typer.echo("No entities match")
    else:
        Console().print(_table(records))


def _record(m: Match) -> dict[str, Any]:
    return {"id": m.slug, "type": m.type, "draft": m.draft, "orphan": m.orphan, "stale": m.stale}


def _table(records: list[dict[str, Any]]) -> Table:
    table = Table(title="query")
    for col in ("id", "type", "draft", "orphan", "stale"):
        table.add_column(col)
    for r in records:
        table.add_row(r["id"], r["type"], str(r["draft"]), str(r["orphan"]), str(r["stale"]))
    return table
