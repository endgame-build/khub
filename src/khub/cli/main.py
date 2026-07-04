"""khub CLI entry point — a thin Typer app over ``khub.core`` (design-memo layer 5)."""

from __future__ import annotations

import typer

from khub.cli.backfill_cmd import backfill_command
from khub.cli.compile_cmd import compile_command
from khub.cli.entity_cmd import (
    DYNAMIC_FIELDS,
    add_command,
    edit_command,
    get_command,
    link_command,
    remove_command,
    unlink_command,
)
from khub.cli.gitlog_cmd import log_command, stale_command
from khub.cli.graph_cmd import history_command, impact_command, neighbors_command
from khub.cli.init_cmd import init_command
from khub.cli.integrity_cmd import check_command, validate_command
from khub.cli.projection_cmd import reindex_command, viz_command
from khub.cli.query_cmd import query_command
from khub.cli.schema_cmd import schema_app
from khub.cli.status_cmd import status_command

app = typer.Typer(
    help="khub — schema-bound context management.",
    no_args_is_help=True,
    add_completion=False,
)


@app.callback()
def _root() -> None:
    """khub — schema-bound context management."""
    # A no-op group callback so subcommands (compile, and future init/schema/...)
    # route as `khub <command>` rather than collapsing to the root.


app.command(name="init")(init_command)
app.add_typer(schema_app, name="schema")
app.command(name="status")(status_command)
app.command(name="compile")(compile_command)
app.command(name="add", context_settings=DYNAMIC_FIELDS)(add_command)
app.command(name="get")(get_command)
app.command(name="edit", context_settings=DYNAMIC_FIELDS)(edit_command)
app.command(name="link")(link_command)
app.command(name="unlink")(unlink_command)
app.command(name="remove")(remove_command)
app.command(name="query", context_settings=DYNAMIC_FIELDS)(query_command)
app.command(name="neighbors")(neighbors_command)
app.command(name="impact")(impact_command)
app.command(name="history")(history_command)
app.command(name="validate")(validate_command)
app.command(name="check")(check_command)
app.command(name="stale")(stale_command)
app.command(name="log")(log_command)
app.command(name="reindex")(reindex_command)
app.command(name="viz")(viz_command)
app.command(name="backfill")(backfill_command)


def main() -> None:
    """Console-script entry point (``khub``)."""
    app()


if __name__ == "__main__":
    main()
