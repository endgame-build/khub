"""Shared output helper — WPK-001-2.

`khub schema` and `khub status` render a Rich table on a TTY and machine-readable
JSON otherwise (or whenever ``--format json`` is set). The JSON branch is the
field-parity contract the agent reads; the table is the human view.
"""

from __future__ import annotations

import json
from typing import Any, Callable

import typer
from rich.console import Console
from rich.table import Table


def want_json(fmt: str) -> bool:
    """JSON is the output when ``--format json`` is set or the stream is not a TTY."""
    return fmt == "json" or not Console().is_terminal


def emit(data: Any, fmt: str, build_table: Callable[[Any], Table]) -> None:
    """Print ``data`` as JSON (json format or non-TTY) or a Rich table (TTY)."""
    if want_json(fmt):
        typer.echo(json.dumps(data, default=str))
    else:
        Console().print(build_table(data))
