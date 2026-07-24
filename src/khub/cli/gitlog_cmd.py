"""``khub stale`` — the git-derived read (WPK-004-2).

A thin Typer adapter over ``core.gitlog``. It does not gate: ``stale`` reports a
(possibly empty) set. It follows the read-command contract — JSON on a pipe or
under ``--format json``, a Rich table on a TTY. The no-git notice
(``No git history; using updated field only``) is the canonical string asserted
in the spec.
"""

from __future__ import annotations

from datetime import date
from typing import Any

import typer
from rich.console import Console
from rich.table import Table

from khub.cli._render import emit, guard, resolve_root
from khub.core.gitlog import StaleEntry, StaleReport, stale
from khub.core.project import stale_days


@guard
def stale_command(
    ctx: typer.Context,
    days: int | None = typer.Option(
        None, "--days", help="Staleness threshold in days; default: the workspace's stale_days."
    ),
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY) or json."),
) -> None:
    """List entities past the `updated` threshold, oldest first: khub stale [--days N]."""
    root = resolve_root(ctx)
    # --days omitted → the workspace's configured stale_days is the threshold.
    threshold = days if days is not None else stale_days(root)
    report = stale(root, days=threshold, now=date.today())

    # The machine view carries the fallback in each entry's `source` field;
    # the human view gets the notice (JSON stays a clean payload).
    records = [_stale_record(e) for e in report.entries]
    emit(records, fmt, lambda: _stale_human(report, records))


def _stale_human(report: StaleReport, records: list[dict[str, Any]]) -> None:
    if not report.git_available:
        typer.echo("No git history; using updated field only")
    if not records:
        typer.echo("No stale entities")
    else:
        Console().print(_stale_table(records))


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
