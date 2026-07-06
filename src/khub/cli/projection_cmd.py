"""``khub reindex`` / ``khub viz`` — the derived projection writers (WPK-005-1).

Thin Typer adapters over ``core.reindex`` and ``core.viz``. Neither gates: both are
no-op-safe reads that write a derived, disposable artifact (an empty workspace is a
success). The located messages (``Reindexed N entities into index.md``,
``Reindexed 0 entities``, ``Wrote viz.html (N nodes, M edges)``) are the canonical
strings asserted in the spec.
"""

from __future__ import annotations

import webbrowser

import typer

from khub.cli._render import resolve_root
from khub.core.errors import LocatedError
from khub.core.reindex import reindex
from khub.core.viz import DEFAULT_OUT, viz


def reindex_command(
    ctx: typer.Context,
    dry_run: bool = typer.Option(
        False, "--dry-run", help="Print the diff against the current index.md and write nothing."
    ),
) -> None:
    """Regenerate the OKF index.md from the graph: khub reindex [--dry-run]."""
    try:
        root = resolve_root(ctx)
        result = reindex(root, dry_run=dry_run)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if dry_run:
        # An empty diff means the index already matches; say so rather than print nothing.
        typer.echo(result.diff, nl=False) if result.diff else typer.echo("index.md is up to date")
        return
    if result.count:
        typer.echo(f"Reindexed {result.count} entities into index.md")
    else:
        typer.echo("Reindexed 0 entities")


def viz_command(
    ctx: typer.Context,
    out: str = typer.Option(DEFAULT_OUT, "--out", help="Output path for the HTML (default viz.html)."),
    open_: bool = typer.Option(False, "--open", help="Open the written file in the default browser."),
    type_: str = typer.Option(None, "--type", help="Render only that type and its incident edges."),
) -> None:
    """Render the typed graph to a self-contained Cytoscape HTML: khub viz [--out F] [--open] [--type T]."""
    try:
        root = resolve_root(ctx)
        result = viz(root, out=out, type_filter=type_)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    typer.echo(f"Wrote {out} ({result.nodes} nodes, {result.edges} edges)")
    if open_:
        # A file:// URI, not a bare path — webbrowser.open expects a URL, and a
        # scheme-less path is treated as relative/invalid by several openers.
        webbrowser.open(result.path.as_uri())
