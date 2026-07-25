"""``khub install-skills`` — copy khub's agent skills into the local agent dirs.

A thin Typer adapter over ``core.skill``. The install is a file copy out of the
package: offline, silent, and safe to re-run. Errors surface as ``LocatedError``
through the shared ``@guard`` boundary, so there is no bespoke exit-code path.

``npx skills add <repo> -s setup`` remains the bootstrap for a machine with no
khub yet, but khub itself never shells out to it.
"""

from __future__ import annotations

import dataclasses

import typer
from rich.console import Console
from rich.table import Table

from khub.cli._render import emit, guard, resolve_root
from khub.core.skill import SkillReport, install_skills


@guard
def install_skills_command(
    ctx: typer.Context,
    target: list[str] = typer.Option(
        None, "--target", help="claude, agents, or opencode (repeatable). Default: all three."
    ),
    skill: list[str] = typer.Option(
        None, "--skill", help="Which skill to install (repeatable). Default: all shipped."
    ),
    global_: bool = typer.Option(
        False, "--global", help="Install into the home directories instead of this workspace."
    ),
    dry_run: bool = typer.Option(
        False, "--dry-run", help="Report what would be written, and write nothing."
    ),
    fmt: str = typer.Option("text", "--format", help="text (Rich table on a TTY) or json."),
) -> None:
    """Install khub's agent skills: khub install-skills [--target agents] [--global]."""
    # --global writes under $HOME and needs no workspace — which is the point: a
    # machine-wide install is exactly what you run before any workspace exists.
    root = None if global_ else resolve_root(ctx)
    report = install_skills(
        root,
        skills=skill or None,
        targets=target or None,
        scope_global=global_,
        dry_run=dry_run,
    )
    emit(_record(report), fmt, lambda: _human(report))


def _record(report: SkillReport) -> dict[str, object]:
    return {
        "scope": report.scope,
        "skills": report.skills,
        "dry_run": report.dry_run,
        "writes": [dataclasses.asdict(w) for w in report.writes],
    }


def _human(report: SkillReport) -> None:
    if not report.writes:
        typer.echo("No skills to install")
        return
    Console().print(_table(report))
    changed = sum(1 for w in report.writes if w.action != "unchanged")
    verb = "would write" if report.dry_run else "wrote"
    typer.echo(f"{verb} {changed} of {len(report.writes)} files ({report.scope} scope)")


def _table(report: SkillReport) -> Table:
    table = Table(title=f"install-skills ({report.scope})")
    table.add_column("path")
    table.add_column("action")
    for write in report.writes:
        table.add_row(write.path, write.action)
    return table
