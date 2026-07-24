"""``khub install-skills`` — install the agent skill via ``npx skills``.

A thin Typer adapter over ``core.skill``. This ran as a tail of ``khub init``
until 0.9.0, which made scaffolding a workspace depend on Node being present and
on SSH access to the skill repo. Split out: ``init`` scaffolds and wires, then
names this command; installing is its own explicit step.

That split changes the failure contract. Inside ``init`` an install failure was
best-effort — the scaffold had to survive it. Asked for directly, anything short
of an install is the command's whole outcome, so it reports on stderr and exits 1.
That covers a missing ``npx`` too: as an init tail "skipped, no npx" was a fine
exit 0, but a CI step running this command on a Node-less image must not report
success with no skill installed.
"""

from __future__ import annotations

import dataclasses

import typer

from khub.cli._render import emit, guard, resolve_root, want_json
from khub.core.skill import SKILLS_SOURCE, SkillOutcome, install_skill, skill_command


@guard
def install_skills_command(
    ctx: typer.Context,
    agent: list[str] = typer.Option(
        None,
        "--agent",
        help="Install for this coding agent (repeatable). Omitted: npx auto-detects.",
    ),
    dry_run: bool = typer.Option(
        False, "--dry-run", help="Print the npx command that would run, and run nothing."
    ),
    fmt: str = typer.Option("text", "--format", help="text or json (emits the outcome)."),
) -> None:
    """Install the khub agent skill: khub install-skills [--agent claude-code] [--dry-run]."""
    root = resolve_root(ctx)
    agents = agent or None

    if dry_run:
        cmd = skill_command(agents)
        emit(
            {"action": "dry-run", "command": cmd},
            fmt,
            lambda: typer.echo(" ".join(cmd)),
        )
        return

    # Capture npx's own output whenever this run emits a machine document — which is
    # `--format json` OR any non-TTY (a pipe, a redirect, CI), the same gate `emit`
    # uses. Keying on `fmt` alone let npx's progress print ahead of the JSON on a
    # pipe, leaving stdout unparseable.
    outcome = install_skill(root, agents=agents, quiet=want_json(fmt))
    installed = outcome.action == "installed"
    emit(dataclasses.asdict(outcome), fmt, lambda: typer.echo(skill_line(outcome), err=not installed))
    if not installed:
        # npx's diagnostics were captured away from stdout; surface them on stderr so a
        # failed install is debuggable instead of just an action name.
        if outcome.detail:
            typer.echo(outcome.detail, err=True)
        raise typer.Exit(1)


def skill_line(outcome: SkillOutcome) -> str:
    """A human line for the install outcome — shared with the ``init`` hint text."""
    if outcome.action == "installed":
        return "installed khub agent skill (npx skills)"
    if outcome.action == "skipped-no-npx":
        return "skill install skipped: npx not found (install Node, then re-run khub install-skills)"
    return f"skill install failed: run `npx skills add {SKILLS_SOURCE} -s khub -s setup` by hand"
