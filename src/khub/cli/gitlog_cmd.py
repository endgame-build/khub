"""``khub stale`` / ``khub log`` — the git-derived reads (WPK-004-2).

Thin Typer adapters over ``core.gitlog``. Neither gates: ``stale`` reports an
(possibly empty) set, ``log`` is a no-op success even with no history. Both follow
the read-command contract — JSON on a pipe or under ``--format json``, a human view
on a TTY (``stale`` renders a Rich table; ``log`` a Rich table). The no-git notices
(``No git history; using updated field only``, ``No git history available``) are the
canonical strings asserted in the spec.
"""

from __future__ import annotations

import json
from datetime import date
from typing import Any

import typer
from rich.console import Console
from rich.table import Table

from khub.cli._render import resolve_root, want_json
from khub.core.errors import LocatedError
from khub.core.gitlog import LogEntry, StaleEntry, log, stale
from khub.core.project import stale_days


def stale_command(
    ctx: typer.Context,
    days: int | None = typer.Option(
        None, "--days", help="Staleness threshold in days; default: the workspace's stale_days."
    ),
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY) or json."),
) -> None:
    """List entities past the `updated` threshold, oldest first: khub stale [--days N]."""
    try:
        root = resolve_root(ctx)
        # --days omitted → the workspace's configured stale_days is the threshold.
        threshold = days if days is not None else stale_days(root)
        report = stale(root, days=threshold, now=date.today())
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    records = [_stale_record(e) for e in report.entries]
    if want_json(fmt):
        # The machine view carries the fallback in each entry's `source` field;
        # the human view gets the notice (JSON stays a clean payload).
        typer.echo(json.dumps(records))
        return
    if not report.git_available:
        typer.echo("No git history; using updated field only")
    if not report.entries:
        typer.echo("No stale entities")
    else:
        Console().print(_stale_table(records))


def log_command(
    ctx: typer.Context,
    id_: str = typer.Argument(None, metavar="ID", help="A bare slug or type/slug; default: all."),
    limit: int = typer.Option(None, "--limit", help="Cap the number of rendered commits."),
    since: str = typer.Option(None, "--since", help="Only commits on/after this date."),
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY) or json."),
) -> None:
    """Render git history at ontology altitude: khub log [ID] [--limit N] [--since DATE]."""
    try:
        root = resolve_root(ctx)
        entries = log(root, id_, limit=limit, since=since)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if entries is None:  # no git history is a reported no-op success (exit 0)
        # JSON stays valid on a pipe: an empty payload flagged git_available=false,
        # never the bare human notice (which is not parseable JSON).
        if want_json(fmt):
            typer.echo(json.dumps({"entries": [], "git_available": False}))
        else:
            typer.echo("No git history available")
        return

    records = [_log_record(e) for e in entries]
    if want_json(fmt):
        # One JSON shape in every state: {"entries": [...], "git_available": bool} —
        # a consumer never has to branch between an object and a bare array.
        typer.echo(json.dumps({"entries": records, "git_available": True}))
    elif not entries:
        typer.echo("No matching history")
    else:
        Console().print(_log_table(records))


# --- records / tables --------------------------------------------------------


def _stale_record(e: StaleEntry) -> dict[str, Any]:
    return {
        "id": f"{e.type}/{e.slug}",
        "type": e.type,
        "slug": e.slug,
        "effective_date": e.effective_date.isoformat(),
        "age": e.age,
        "source": e.source,
    }


def _stale_table(records: list[dict[str, Any]]) -> Table:
    table = Table(title="stale")
    for col in ("id", "effective_date", "age", "source"):
        table.add_column(col)
    for r in records:
        table.add_row(r["id"], r["effective_date"], str(r["age"]), r["source"])
    return table


def _log_record(e: LogEntry) -> dict[str, Any]:
    return {
        "commit": e.commit,
        "id": f"{e.type}/{e.slug}",
        "type": e.type,
        "slug": e.slug,
        "relations": e.relations,
        "date": e.date,
    }


def _log_table(records: list[dict[str, Any]]) -> Table:
    table = Table(title="log")
    for col in ("commit", "id", "relations", "date"):
        table.add_column(col)
    for r in records:
        table.add_row(r["commit"], r["id"], ", ".join(r["relations"]) or "—", r["date"])
    return table
