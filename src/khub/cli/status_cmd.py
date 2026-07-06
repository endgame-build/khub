"""``khub status`` — report workspace status (WPK-001-2). Thin wiring.

Per-type counts, the draft/active split, orphan/stale counts, and the OKF flag,
all derived from the graph projection. Rich table on a TTY, JSON otherwise; an
empty workspace reports the initialized-but-empty line.
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
from khub.core.project import project, stale_days


def status_command(
    ctx: typer.Context,
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY) or json."),
) -> None:
    """Summarize the workspace: counts, draft/active, orphan/stale, OKF conformance."""
    try:
        root = resolve_root(ctx)
        # Inside the guard: a corrupt schema.yaml raises a located schema_error
        # here, which must render as a clean line, not a traceback.
        proj = project(root, stale_days=stale_days(root), now=date.today())
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    data: dict[str, Any] = {
        "counts": proj.counts,
        "total": proj.total,
        "draft": proj.draft,
        "active": proj.active,
        "orphan": proj.orphan,
        "stale": proj.stale,
        "okf_conformant": proj.okf_conformant,
    }
    # getattr-guarded so output is unchanged until core adds stray/malformed counts.
    for extra in ("stray", "malformed"):
        val = getattr(proj, extra, None)
        if val is not None:
            data[extra] = val

    # JSON on a pipe or when asked (the agent contract); a Rich table on a TTY,
    # with the friendly empty-state line for the operator.
    if want_json(fmt):
        typer.echo(json.dumps(data, default=str))
    elif proj.total == 0:
        typer.echo("Workspace initialized; no entities yet")
    else:
        Console().print(_status_table(data))


def _status_table(data: dict[str, Any]) -> Table:
    table = Table(title="status")
    table.add_column("type")
    table.add_column("count", justify="right")
    for tname, count in data["counts"].items():
        table.add_row(tname, str(count))
    table.add_section()
    table.add_row("draft / active", f"{data['draft']} / {data['active']}")
    table.add_row("orphan", str(data["orphan"]))
    table.add_row("stale", str(data["stale"]))
    for extra in ("stray", "malformed"):
        if extra in data:
            table.add_row(extra, str(data[extra]))
    table.add_row("OKF-conformant", "yes" if data["okf_conformant"] else "no")
    return table
