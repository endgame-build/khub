"""``khub wire`` — link the workspace into CLAUDE.md (and AGENTS.md). Thin wiring."""

from __future__ import annotations

import typer

from khub.cli._render import resolve_root
from khub.core.errors import LocatedError
from khub.core.wire import wire


def wire_command(
    ctx: typer.Context,
    agents: bool = typer.Option(False, "--agents", help="Also wire AGENTS.md."),
    dry_run: bool = typer.Option(False, "--dry-run", help="Print the block; write nothing."),
) -> None:
    """Inject a managed khub block (with a ``@.khub/schema.yaml`` import) into CLAUDE.md."""
    try:
        root = resolve_root(ctx)
        result = wire(root, agents=agents, dry_run=dry_run)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if dry_run:
        typer.echo(result.block)
        return
    for outcome in result.outcomes:
        typer.echo(f"{outcome.action} {outcome.path.name}")
