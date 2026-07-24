"""khub CLI entry point — a thin Typer app over ``khub.core`` (design-memo layer 5)."""

from __future__ import annotations

from pathlib import Path

import typer

from khub.cli.backfill_cmd import backfill_command
from khub.cli.entity_cmd import (
    DYNAMIC_FIELDS,
    add_command,
    edit_command,
    get_command,
    link_command,
    remove_command,
    unlink_command,
)
from khub.cli.gitlog_cmd import stale_command
from khub.cli.graph_cmd import history_command, impact_command, neighbors_command
from khub.cli._render import CliState
from khub.cli.init_cmd import init_command
from khub.cli.integrity_cmd import check_command, validate_command
from khub.cli.projection_cmd import reindex_command, viz_command
from khub.cli.query_cmd import query_command
from khub.cli.schema_cmd import schema_app
from khub.cli.search_cmd import search_command
from khub.cli.skill_cmd import install_skills_command
from khub.cli.status_cmd import status_command
from khub.cli.wire_cmd import wire_command

app = typer.Typer(
    help="khub — schema-bound context management.",
    no_args_is_help=True,
    add_completion=False,
)


def _version_callback(value: bool) -> None:
    """Print the khub version and exit (eager, so it short-circuits any command)."""
    if not value:
        return
    from importlib.metadata import PackageNotFoundError
    from importlib.metadata import version as _pkg_version

    try:
        ver = _pkg_version("khub")
    except PackageNotFoundError:
        from khub import __version__ as ver
    typer.echo(ver)
    raise typer.Exit()


@app.callback()
def _root(
    ctx: typer.Context,
    workspace: Path | None = typer.Option(
        None, "-C", "--workspace", help="Operate on this workspace instead of the working directory."
    ),
    version: bool | None = typer.Option(
        None, "--version", callback=_version_callback, is_eager=True, help="Print the khub version and exit."
    ),
) -> None:
    """khub — schema-bound context management."""
    # Stash global state; every command resolves the root via cli._render.resolve_root(),
    # reading ctx.obj (Click propagates it to subcommands). Defaults keep `khub <command>`
    # routing as before.
    ctx.obj = CliState(workspace=workspace)


app.command(name="init")(init_command)
app.add_typer(schema_app, name="schema")
app.command(name="status")(status_command)
app.command(name="add", context_settings=DYNAMIC_FIELDS)(add_command)
app.command(name="get")(get_command)
app.command(name="edit", context_settings=DYNAMIC_FIELDS)(edit_command)
app.command(name="link")(link_command)
app.command(name="unlink")(unlink_command)
app.command(name="remove")(remove_command)
app.command(name="query", context_settings=DYNAMIC_FIELDS)(query_command)
app.command(name="search")(search_command)
app.command(name="neighbors")(neighbors_command)
app.command(name="impact")(impact_command)
app.command(name="history")(history_command)
app.command(name="validate")(validate_command)
app.command(name="check")(check_command)
app.command(name="stale")(stale_command)
app.command(name="reindex")(reindex_command)
app.command(name="viz")(viz_command)
app.command(name="backfill")(backfill_command)
app.command(name="wire")(wire_command)
app.command(name="install-skills")(install_skills_command)


def main() -> None:
    """Console-script entry point (``khub``)."""
    app()


if __name__ == "__main__":
    main()
