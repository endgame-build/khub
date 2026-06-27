"""``khub neighbors`` / ``impact`` / ``history`` — the graph walks (WPK-003-2/3).

Thin Typer adapters over ``core.graph``. Every read command follows the same
output contract (docs/cli.md): a human view on a TTY, JSON on a pipe or under
``--format json``. ``neighbors`` and ``history`` render a Rich table as the human
view; ``impact``'s human view is a depth-marked text tree (indentation marks depth
from the source), still emitting JSON on a pipe so the agent contract holds.
``--format tree`` forces the tree even on a pipe. An empty walk is a success with
a friendly line; an unresolvable id surfaces the shared located lookup error.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import typer
from rich.console import Console
from rich.table import Table

from khub.cli._render import want_json
from khub.core.errors import LocatedError
from khub.core.graph import HistoryLink, ImpactNode, Neighbor, history, impact, neighbors
from khub.core.index import build_index
from khub.core.introspect import load_schema
from khub.core.locate import find_workspace


def neighbors_command(
    id_: str = typer.Argument(..., metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    predicate: str = typer.Option(None, "--predicate", help="Restrict adjacency to one predicate."),
    in_: bool = typer.Option(False, "--in", help="Inbound edges only (incl. derived inverses)."),
    out_: bool = typer.Option(False, "--out", help="Outbound (stored) edges only."),
    both: bool = typer.Option(False, "--both", help="Both directions (the default)."),
    depth: int = typer.Option(1, "--depth", help="Bounded multi-hop adjacency over all predicates."),
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY) or json."),
) -> None:
    """Walk one-hop neighbors: khub neighbors initech-pov [--predicate client --in]."""
    direction = "in" if in_ else "out" if out_ else "both"
    try:
        root = find_workspace(Path.cwd())
        index = build_index(root, load_schema(root))
        result = neighbors(index, id_, predicate=predicate, direction=direction, depth=depth)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    records = [_neighbor_record(n) for n in result]
    if want_json(fmt):
        typer.echo(json.dumps(records))
    elif not result:
        typer.echo("No neighbors")
    else:
        Console().print(_neighbor_table(records))


def impact_command(
    id_: str = typer.Argument(..., metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    predicate: str = typer.Option("depends_on", "--predicate", help="The edge to walk the closure over."),
    reverse: bool = typer.Option(False, "--reverse", help="Walk ancestors (what reaches this node)."),
    fmt: str = typer.Option("text", "--format", help="tree (depth-marked, the TTY default) or json."),
) -> None:
    """Compute blast radius: khub impact node-a [--reverse] [--predicate <p>]."""
    try:
        root = find_workspace(Path.cwd())
        index = build_index(root, load_schema(root))
        result = impact(index, id_, predicate=predicate, reverse=reverse)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    # JSON on an explicit request or any pipe (agent contract); the depth tree is
    # the human view on a TTY, and `--format tree` forces it even on a pipe.
    if fmt == "json" or (fmt != "tree" and not Console().is_terminal):
        typer.echo(json.dumps([_impact_record(n) for n in result]))
        return
    if len(result) <= 1:  # only the source — nothing reachable on the predicate
        typer.echo("No downstream impact")
        return
    for n in result:
        typer.echo("  " * n.depth + f"{n.type}/{n.slug}")


def history_command(
    id_: str = typer.Argument(..., metavar="ID", help="A bare slug, or type/slug on ambiguity."),
    predicate: str = typer.Option("supersedes", "--predicate", help="The self-referential edge to follow."),
    limit: int = typer.Option(None, "--limit", help="Cap to the N most recent links."),
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY) or json."),
) -> None:
    """Trace supersession lineage: khub history decision-0012 [--limit 3]."""
    try:
        root = find_workspace(Path.cwd())
        index = build_index(root, load_schema(root))
        result = history(index, id_, predicate=predicate, limit=limit)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    records = [_history_record(link) for link in result]
    if want_json(fmt):
        typer.echo(json.dumps(records))
    elif len(result) <= 1:  # supersedes nothing — only the source record
        typer.echo("No supersession history")
    else:
        Console().print(_history_table(records))


# --- records / tables --------------------------------------------------------


def _neighbor_record(n: Neighbor) -> dict[str, Any]:
    return {
        "id": f"{n.type}/{n.slug}",
        "type": n.type,
        "slug": n.slug,
        "predicate": n.predicate,
        "direction": n.direction,
        "derived": n.derived,
        "depth": n.depth,
    }


def _neighbor_table(records: list[dict[str, Any]]) -> Table:
    table = Table(title="neighbors")
    for col in ("id", "predicate", "direction", "depth"):
        table.add_column(col)
    for r in records:
        table.add_row(r["id"], r["predicate"], r["direction"], str(r["depth"]))
    return table


def _impact_record(n: ImpactNode) -> dict[str, Any]:
    return {"id": f"{n.type}/{n.slug}", "type": n.type, "slug": n.slug, "depth": n.depth}


def _history_record(link: HistoryLink) -> dict[str, Any]:
    return {
        "id": f"{link.type}/{link.slug}",
        "type": link.type,
        "slug": link.slug,
        "superseded_by": link.superseded_by,
    }


def _history_table(records: list[dict[str, Any]]) -> Table:
    table = Table(title="history")
    for col in ("id", "superseded_by"):
        table.add_column(col)
    for r in records:
        table.add_row(r["id"], r["superseded_by"] or "—")
    return table
