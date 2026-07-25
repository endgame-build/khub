"""``khub backfill`` — additive frontmatter/date writes for cutover (WPK-005-2).

A thin Typer adapter over ``core.backfill``. Does not gate: an already-valid tree and
a non-git workspace are both success. The located messages
(``Backfilled dates on N entities``, ``No git history; dates not backfilled``) are the
canonical strings asserted in the spec.
"""

from __future__ import annotations

import typer

from khub.cli._render import guard, resolve_root
from khub.core.backfill import BackfillReport, backfill


@guard
def backfill_command(
    ctx: typer.Context,
    type_: str = typer.Option(
        None, "--type", help="Add missing per-type frontmatter scaffolding for that type."
    ),
    dry_run: bool = typer.Option(
        False, "--dry-run", help="List the entities and fields that would change; write nothing."
    ),
) -> None:
    """Backfill missing dates and frontmatter: khub backfill [--type T] [--dry-run]."""
    root = resolve_root(ctx)
    report = backfill(root, type_, dry_run=dry_run)
    _emit(report)


def _emit(report: BackfillReport) -> None:
    """A write command, so it prints text — no JSON contract (unlike the read commands)."""
    if report.dry_run:
        for id_, fields in _by_entity(report):
            typer.echo(f"{id_}: {', '.join(fields)}")
        if not report.changes:
            typer.echo("No changes")
        _emit_skipped(report)  # a preview that omits the skip is not a preview
        return
    if not report.git_available:
        typer.echo("No git history; dates not backfilled")
    else:
        typer.echo(f"Backfilled dates on {report.dated_entities} entities")
    # A --type run writes scaffolding even with no git; surface it so the "not
    # backfilled" line above is never the whole story when files were in fact changed.
    if report.scaffolded_entities:
        typer.echo(f"Scaffolded frontmatter on {report.scaffolded_entities} entities")
    _emit_skipped(report)


def _emit_skipped(report: BackfillReport) -> None:
    """The collection-skip line — printed by both the real run and the dry-run."""
    if report.skipped_collections:
        typer.echo(
            f"Skipped collection types ({', '.join(report.skipped_collections)}): "
            "row-level git dates land with row-diff attribution"
        )


def _by_entity(report: BackfillReport) -> list[tuple[str, list[str]]]:
    """Changes grouped per entity id, preserving first-seen order — for the dry-run list."""
    grouped: dict[str, list[str]] = {}
    for c in report.changes:
        grouped.setdefault(c.id, []).append(c.field)
    return list(grouped.items())
