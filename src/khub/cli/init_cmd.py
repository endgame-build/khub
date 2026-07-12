"""``khub init`` — operator trigger for ``core.init_workspace`` (WPK-001-1). Thin wiring.

After scaffolding, ``init`` wires the workspace into the selected agent context files
(``CLAUDE.md`` / ``AGENTS.md``, both by default) and installs the khub agent skill
(agnostically, via ``npx skills``), so a fresh workspace is agent-ready in one command.
Both tails are best-effort and independently skippable (``--no-wire`` / ``--no-skill``);
neither unwinds a successful scaffold.
"""

from __future__ import annotations

import dataclasses
import json
from pathlib import Path

import typer

from khub.cli import interact
from khub.cli._render import guard
from khub.core.errors import LocatedError
from khub.core.skill import SKILLS_SOURCE, SkillOutcome

# A short convenience list for the wizard's agent multiselect; empty picks (or headless)
# fall back to npx's own auto-detect. Not exhaustive — skills.sh supports ~70 agents.
_COMMON_AGENTS = ["claude-code", "cursor", "codex", "gemini-cli", "opencode"]


def _wire_targets(picks: list[str] | None) -> tuple[bool, bool]:
    """Which agent files ``init`` seeds, as ``(claude, agents)``. Explicit picks map
    ``claude-code`` → CLAUDE.md and any other agent → AGENTS.md; no selection (None/empty)
    seeds both."""
    if not picks:
        return True, True
    return ("claude-code" in picks, any(a != "claude-code" for a in picks))


@guard
def init_command(
    ctx: typer.Context,
    preset: str | None = typer.Argument(None, help="Named preset to seed from (e.g. firm-ops)."),
    path: Path | None = typer.Argument(None, help="Target directory (default: .)."),
    preset_source: Path = typer.Option(
        None, "--preset-source", help="Where to resolve the preset if not packaged with khub."
    ),
    name: str = typer.Option(None, "--name", help="Workspace name (default: the target dir name)."),
    force: bool = typer.Option(False, "--force", help="Scaffold into a non-empty target."),
    no_wire: bool = typer.Option(
        False, "--no-wire", help="Skip wiring the schema into the agent files."
    ),
    no_skill: bool = typer.Option(
        False, "--no-skill", help="Skip installing the agent skill via npx skills."
    ),
    fmt: str = typer.Option(
        "text", "--format", help="text confirmation (default); json emits resolved provenance."
    ),
) -> None:
    """Scaffold a workspace from a preset, then wire it and install the agent skill.

    On a TTY, a missing preset/path launches a wizard and the wire/skill tails are
    confirmed; an agent (``--agent``), a pipe, or ``--format json`` never prompts.
    """
    from khub.core.skill import install_skill
    from khub.core.wire import wire
    from khub.core.workspace import (  # heavy (compile path); imported lazily
        init_workspace,
        known_presets,
    )

    prompter = interact.make_prompter(ctx.obj, fmt)

    # Resolve the inputs: a provided flag wins; else prompt on a TTY; else the headless default.
    if preset is None:
        if prompter is None:
            typer.echo("Missing argument 'PRESET' (e.g. firm-ops).", err=True)
            raise typer.Exit(2)
        preset = prompter.select("Preset", choices=sorted(known_presets()))
    if path is None:
        path = prompter.path("Directory", default=".") if prompter is not None else Path(".")

    # Tail choices: a --no-* flag forces skip; else confirm on a TTY; else default on.
    do_wire = not no_wire and (prompter.confirm("Wire the schema into your agent files?") if prompter else True)
    do_skill = not no_skill and (prompter.confirm("Install the khub agent skill?") if prompter else True)
    agents = None
    if (do_wire or do_skill) and prompter is not None:
        picks = prompter.checkbox(
            "Set up which agents? (none = both files / auto-detect)", _COMMON_AGENTS
        )
        agents = picks or None

    result = init_workspace(preset, path, preset_source=preset_source, name=name, force=force)

    # Best-effort tails: a wire/skill hiccup does not unwind the scaffold above.
    wire_result = None
    wire_error = None
    if do_wire:
        want_claude, want_agents = _wire_targets(agents)
        try:
            wire_result = wire(result.path, claude=want_claude, agents=want_agents)
        except LocatedError as err:
            wire_error = err.message
        except OSError as err:  # e.g. a read-only agent file; report, don't unwind
            wire_error = str(err)
    # In json mode capture npx output so it never precedes the JSON document.
    skill_outcome = (
        install_skill(result.path, agents=agents, quiet=(fmt == "json")) if do_skill else None
    )

    if fmt == "json":
        payload = dataclasses.asdict(result)
        if wire_result is not None:
            payload["wire"] = [dataclasses.asdict(o) for o in wire_result.outcomes]
        if wire_error is not None:
            payload["wire_error"] = wire_error
        if skill_outcome is not None:
            payload["skill"] = dataclasses.asdict(skill_outcome)
        typer.echo(json.dumps(payload, default=str))
        return

    if result.seeded_over_corpus:
        typer.echo(
            f"Initialized {result.preset} workspace; "
            f"{result.entity_files_modified} entity files modified"
        )
    else:
        typer.echo(f"Initialized {result.preset} workspace at {result.path}")
    if wire_result is not None:
        for outcome in wire_result.outcomes:
            typer.echo(f"{outcome.action} {outcome.path.name}")
    if wire_error is not None:
        typer.echo(f"wire skipped: {wire_error}", err=True)
    if skill_outcome is not None:
        # A failure is an error stream; installed/skipped are informational (stdout).
        typer.echo(_skill_line(skill_outcome), err=(skill_outcome.action == "failed"))


def _skill_line(outcome: SkillOutcome) -> str:
    """A human line for the skill-install outcome."""
    if outcome.action == "installed":
        return "installed khub agent skill (npx skills)"
    if outcome.action == "skipped-no-npx":
        return "skill install skipped: npx not found (install Node, or run `npx skills add …`)"
    return f"skill install failed: run `npx skills add {SKILLS_SOURCE} -s khub -s setup` by hand"
