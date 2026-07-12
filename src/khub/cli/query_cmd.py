"""``khub query`` — filter entities by frontmatter and derived edges (WPK-003-1).

A thin Typer adapter over ``core.query``. Like ``add``/``edit`` it runs with
``ignore_unknown_options`` so arbitrary ``--<field> value`` filters parse out of
the extra args (the known flags — ``--type``, ``--tag``, ``--has``, ``--missing``,
``--orphan``, ``--stale``, ``--draft``/``--active``, ``--limit``, ``--format`` —
still bind). Rich table on a TTY, JSON otherwise, bare ids under ``--format ids``.
"""

from __future__ import annotations

from datetime import date
from typing import Any

import typer
from rich.console import Console
from rich.table import Table

from khub.cli import interact
from khub.cli._render import emit, guard, resolve_root
from khub.cli.entity_cmd import DYNAMIC_FIELDS, parse_fields
from khub.core.introspect import load_schema, types_list
from khub.core.query import Match, QueryFilters, query

__all__ = ["query_command", "DYNAMIC_FIELDS"]


@guard
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
    prompter = interact.make_prompter(ctx.obj, fmt)
    fields = parse_fields(ctx.args)
    root = resolve_root(ctx)
    # A bare interactive query offers a type filter; (all) keeps the full set.
    no_filters = not any(
        [type_, tag, has, missing, orphan, stale, draft, active, limit, fields]
    )
    if prompter is not None and no_filters:
        choice = prompter.select(
            "Filter by type", choices=["(all)", *sorted(types_list(load_schema(root)))]
        )
        if choice != "(all)":
            type_ = choice
    filters = QueryFilters(
        type=type_,
        fields=fields,
        tag=tag,
        has=has,
        missing=missing,
        orphan=orphan,
        stale=stale,
        draft_only=draft,
        active_only=active,
        limit=limit,
    )
    matches = query(root, filters, now=date.today())
    _emit(matches, fmt)


def _emit(matches: list[Match], fmt: str) -> None:
    if fmt == "ids":  # bare ids for piping — independent of TTY detection
        for m in matches:
            typer.echo(m.slug)
        return
    records = [_record(m) for m in matches]
    emit(records, fmt, lambda: _human(records))


def _human(records: list[dict[str, Any]]) -> None:
    if not records:
        typer.echo("No entities match")
    else:
        Console().print(_table(records))


def _record(m: Match) -> dict[str, Any]:
    return {
        "id": f"{m.type}/{m.slug}",
        "type": m.type,
        "slug": m.slug,
        "draft": m.draft,
        "orphan": m.orphan,
        "stale": m.stale,
    }


def _table(records: list[dict[str, Any]]) -> Table:
    table = Table(title="query")
    for col in ("id", "type", "draft", "orphan", "stale"):
        table.add_column(col)
    for r in records:
        table.add_row(r["id"], r["type"], str(r["draft"]), str(r["orphan"]), str(r["stale"]))
    return table
