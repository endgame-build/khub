"""``khub wire`` — link the workspace into agent context files (CLAUDE.md / AGENTS.md). Thin wiring."""

from __future__ import annotations

import typer

from khub.cli._render import resolve_root
from khub.core.errors import LocatedError
from khub.core.wire import wire


def _resolve_target(target: str | None) -> tuple[bool, bool]:
    """Map ``--target`` to ``(claude, agents)``. ``None`` → neither (update existing files)."""
    if target is None:
        return False, False
    mapping = {"claude": (True, False), "agents": (False, True), "both": (True, True)}
    if target not in mapping:
        raise LocatedError.bad_target(target)
    return mapping[target]


def wire_command(
    ctx: typer.Context,
    target: str | None = typer.Option(
        None,
        "--target",
        help="Create and wire a specific file: claude, agents, or both. "
        "Omit to update the agent files that already exist.",
    ),
    dry_run: bool = typer.Option(False, "--dry-run", help="Print the block(s); write nothing."),
) -> None:
    """Wire the workspace into agent context files (CLAUDE.md gets a ``@.khub/schema.yaml``
    import; AGENTS.md gets a schema pointer). Bare ``wire`` updates whichever already exist."""
    try:
        root = resolve_root(ctx)
        claude, agents = _resolve_target(target)
        result = wire(root, claude=claude, agents=agents, dry_run=dry_run)
    except LocatedError as err:
        typer.echo(err.message, err=True)
        raise typer.Exit(1) from None

    if not result.outcomes:
        typer.echo(
            "No CLAUDE.md or AGENTS.md to wire. Pass --target claude|agents|both to create one.",
            err=True,
        )
        return
    if dry_run:
        typer.echo(result.preview)
        return
    for outcome in result.outcomes:
        typer.echo(f"{outcome.action} {outcome.path.name}")
