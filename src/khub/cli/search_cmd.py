"""``khub search`` — full-text over title and body (FS-003 fast-follow, FTS5).

A thin Typer adapter over ``core.search``. Raw FTS5 MATCH syntax passes through
(``"quoted phrases"``, ``OR``, ``NEAR``, ``prefix*``). Rich table on a TTY, JSON
otherwise, bare slugs under ``--format ids``.
"""

from __future__ import annotations

from typing import Any

import typer
from rich.console import Console
from rich.table import Table

from khub.cli._render import emit, guard, resolve_root
from khub.core.search import SearchHit, search

__all__ = ["search_command"]


@guard
def search_command(
    ctx: typer.Context,
    text: str = typer.Argument(..., help='FTS5 MATCH text: terms, "phrases", OR, NEAR, prefix*.'),
    type_: str = typer.Option(None, "--type", help="Restrict to one entity type."),
    limit: int = typer.Option(20, "--limit", help="Cap the returned set."),
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY), json, or ids."),
) -> None:
    """Full-text search: khub search modernization --type transcript --format json."""
    root = resolve_root(ctx)
    hits = search(root, text, type_=type_, limit=limit)
    _emit(hits, fmt)


def _emit(hits: list[SearchHit], fmt: str) -> None:
    if fmt == "ids":  # bare slugs for piping — independent of TTY detection
        for h in hits:
            typer.echo(h.slug)
        return
    records = [_record(h) for h in hits]
    emit(records, fmt, lambda: _human(records))


def _human(records: list[dict[str, Any]]) -> None:
    if not records:
        typer.echo("No entities match")
    else:
        Console().print(_table(records))


def _record(h: SearchHit) -> dict[str, Any]:
    record = {
        "id": f"{h.type}/{h.slug}",
        "type": h.type,
        "slug": h.slug,
        "title": h.title,
        "score": h.score,
        "snippet": h.snippet,
        "path": h.path,
    }
    if h.locator:  # collection rows only — per-item records stay byte-identical
        record["locator"] = h.locator
    return record


def _table(records: list[dict[str, Any]]) -> Table:
    table = Table(title="search")
    for col in ("id", "title", "snippet"):
        table.add_column(col)
    for r in records:
        table.add_row(r["id"], r["title"], r["snippet"])
    return table
