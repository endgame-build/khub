"""Shared adapter seams — WPK-001-2.

Every command routes through the same two seams:

- :func:`guard` — the single error boundary. A :class:`LocatedError` raised
  anywhere in a command body renders as its message on stderr and exits 1 —
  never a traceback, never a per-command catch block.
- :func:`emit` — the single output dispatch. One JSON document under
  ``--format json`` or on any pipe (the field-parity contract the agent reads);
  the command's human view on a TTY.
"""

from __future__ import annotations

import functools
import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, ParamSpec, TypeVar

import typer
from rich.console import Console

from khub.core.errors import LocatedError
from khub.core.locate import find_workspace

_P = ParamSpec("_P")
_R = TypeVar("_R")


@dataclass(frozen=True)
class CliState:
    """Global CLI state stashed on ``ctx.obj`` by the root callback.

    Carries the ``--workspace`` override and nothing else. It also carried an
    ``agent`` flag until 0.9.0, when khub stopped prompting: with no prompts there
    is no human/agent mode to switch between.
    """

    workspace: Path | None


def guard(fn: Callable[_P, _R]) -> Callable[_P, _R]:
    """Decorate a command function with the LocatedError → stderr + exit 1 boundary."""

    @functools.wraps(fn)
    def wrapper(*args: _P.args, **kwargs: _P.kwargs) -> _R:
        try:
            return fn(*args, **kwargs)
        except LocatedError as err:
            typer.echo(err.message, err=True)
            raise typer.Exit(1) from None

    return wrapper


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

    The output gate (``want_json``) keys off this, so a pipe or a redirect gets
    machine output. It also gated prompting until 0.9.0; only the output side remains.
    """
    return Console().is_terminal


def want_json(fmt: str) -> bool:
    """JSON is the output when ``--format json`` is set or the stream is not a TTY."""
    return fmt == "json" or not is_tty()


def emit(data: Any, fmt: str, human: Callable[[], None]) -> None:
    """Print ``data`` as one JSON document (json format or non-TTY) or run the human view."""
    if want_json(fmt):
        typer.echo(json.dumps(data, default=str))
    else:
        human()
