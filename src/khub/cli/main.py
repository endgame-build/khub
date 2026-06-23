"""khub CLI entry point — a thin Typer app over ``khub.core`` (design-memo layer 5)."""

from __future__ import annotations

import typer

from khub.cli.compile_cmd import compile_command

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


app.command(name="compile")(compile_command)


def main() -> None:
    """Console-script entry point (``khub``)."""
    app()


if __name__ == "__main__":
    main()
