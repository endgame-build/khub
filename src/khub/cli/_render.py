"""Shared output helper — WPK-001-2.

`khub schema` and `khub status` render a Rich table on a TTY and machine-readable
JSON otherwise (or whenever ``--format json`` is set). The JSON branch is the
field-parity contract the agent reads; the table is the human view.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Callable

import typer
from rich.console import Console
from rich.table import Table

from khub.core.locate import find_workspace


def resolve_root(ctx: typer.Context) -> Path:
    """The workspace root, honoring a ``--workspace/-C`` override on the root context.

    The root callback stores a ``CliState`` (carrying the ``--workspace`` value) on
    ``ctx.obj``; Typer propagates it to every (including nested) subcommand context, so
    commands resolve against the override when given and the working directory otherwise.
    """
    state = ctx.obj
    override = state.workspace if state is not None else None
    return find_workspace(Path(override) if override else Path.cwd())


def is_tty() -> bool:
    """True when stdout is an interactive terminal — the one human/machine signal.

    Both the output gate (``want_json``) and the prompt gate (``interact.can_prompt``)
    key off this, so a pipe or a redirect flips both to machine behavior together.
    """
    return Console().is_terminal


def want_json(fmt: str) -> bool:
    """JSON is the output when ``--format json`` is set or the stream is not a TTY."""
    return fmt == "json" or not is_tty()


def emit(data: Any, fmt: str, build_table: Callable[[Any], Table]) -> None:
    """Print ``data`` as JSON (json format or non-TTY) or a Rich table (TTY)."""
    if want_json(fmt):
        typer.echo(json.dumps(data, default=str))
    else:
        Console().print(build_table(data))
