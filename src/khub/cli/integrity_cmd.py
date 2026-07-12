"""``khub validate`` / ``khub check`` — the v1 integrity gate (WPK-004-1).

Thin Typer adapters over ``core.integrity``. Both follow the read-command output
contract: JSON on a pipe or under ``--format json``, a human view on a TTY. Unlike
the other reads these *gate* — a non-empty error set exits non-zero. The located
success lines (``Validated N entities; 0 errors``, ``Graph check passed``) are the
canonical examples asserted in the spec.
"""

from __future__ import annotations

from typing import Any

import typer

from khub.cli._render import emit, guard, resolve_root
from khub.core.integrity import CheckReport, ValidateReport, check, validate


@guard
def validate_command(
    ctx: typer.Context,
    target: str = typer.Argument(None, metavar="TARGET", help="A type or type/slug; default: all."),
    strict: bool = typer.Option(False, "--strict", help="Close the schema: reject undeclared keys."),
    fix: bool = typer.Option(False, "--fix", help="Backfill a missing `updated` from git (v1 scope)."),
    fmt: str = typer.Option("text", "--format", help="text (Rich on a TTY) or json."),
) -> None:
    """Validate entities: khub validate [TARGET] [--strict] [--fix]."""
    root = resolve_root(ctx)
    report = validate(root, target, strict=strict, fix=fix)
    _emit_validate(report, fmt)
    if not report.ok:
        raise typer.Exit(1)


def _emit_validate(report: ValidateReport, fmt: str) -> None:
    payload = {
        "count": report.count,
        "errors": [
            {"id": e.id, "type": e.type, "slug": e.slug, "field": e.field, "reason": e.reason}
            for e in report.errors
        ],
        "fixed": report.fixed,
    }
    emit(payload, fmt, lambda: _validate_human(report))


def _validate_human(report: ValidateReport) -> None:
    if report.ok:
        typer.echo(f"Validated {report.count} entities; 0 errors")
        return
    for e in report.errors:
        typer.echo(f"{e.id}: {e.field}: {e.reason}")
    typer.echo(f"Validated {report.count} entities; {len(report.errors)} errors")


@guard
def check_command(
    ctx: typer.Context,
    strict: bool = typer.Option(
        False, "--strict", help="Fail the gate on orphans too (default: informational)."
    ),
    fmt: str = typer.Option("text", "--format", help="text (Rich on a TTY) or json."),
) -> None:
    """Check the active graph: completeness, orphans, dangling edges, strays, cycles."""
    root = resolve_root(ctx)
    report = check(root, strict=strict)
    _emit_check(report, fmt)
    if not report.passed:
        raise typer.Exit(1)


def _emit_check(report: CheckReport, fmt: str) -> None:
    emit(_check_payload(report), fmt, lambda: _check_human(report))


def _check_human(report: CheckReport) -> None:
    if report.passed:
        for o in report.orphans:
            typer.echo(f"orphan {o} (informational)")
        typer.echo("Graph check passed")
        return
    for inc in report.incomplete:
        gaps = ", ".join(inc.missing_fields + inc.missing_relations)
        typer.echo(f"active-but-incomplete {inc.id}: missing {gaps}")
    for d in report.dangling:
        typer.echo(f"dangling edge {d.id}: {d.predicate} -> '{d.target}' does not resolve")
    for o in report.orphans:
        typer.echo(f"orphan {o}")
    for s in report.strays:
        typer.echo(f"stray file {s}")
    # getattr-guarded so this works before/after core adds CheckReport.malformed.
    for m in getattr(report, "malformed", []):
        typer.echo(f"malformed file {m}")
    if report.suppressed_dangling:
        typer.echo(
            f"({report.suppressed_dangling} dangling edges suppressed pending the "
            "malformed collection fix)"
        )
    for cycle in report.cycles:
        typer.echo(f"cycle {' -> '.join(cycle)}")


def _check_payload(report: CheckReport) -> dict[str, Any]:
    return {
        "passed": report.passed,
        "incomplete": [
            {
                "id": i.id,
                "type": i.type,
                "slug": i.slug,
                "missing_fields": i.missing_fields,
                "missing_relations": i.missing_relations,
            }
            for i in report.incomplete
        ],
        "orphans": report.orphans,
        "dangling": [
            {"id": d.id, "type": d.type, "slug": d.slug, "predicate": d.predicate, "target": d.target}
            for d in report.dangling
        ],
        "strays": report.strays,
        # getattr-guarded so this works before/after core adds CheckReport.malformed.
        "malformed": getattr(report, "malformed", []),
        "cycles": report.cycles,
        "suppressed_dangling": report.suppressed_dangling,
    }
