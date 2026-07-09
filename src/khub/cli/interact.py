"""Interactive prompts for human CLI use — questionary wrappers behind an agent gate.

khub is agent-first: a machine passes flags and never sees a prompt. When a human
runs a write verb in a terminal with a value missing, the command fills the gap
through a :class:`Prompter` (questionary under the hood). Two functions carry the
whole contract:

- :func:`can_prompt` — the single decision (a human, on a TTY, not asking for JSON).
- :func:`make_prompter` — the single construction seam; every command obtains its
  prompter here and nowhere else, so a test monkeypatches this one function.

Core verbs never prompt. Interactivity lives entirely at this surface, and a
prompted value flows into the same core call a flag would.
"""

from __future__ import annotations

from collections.abc import Callable, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import typer

from khub.cli._render import is_tty


def _q() -> Any:
    """Import questionary lazily. ``interact`` is imported at CLI startup (for CliState),
    but questionary pulls prompt_toolkit — kept off the path of every agent invocation
    and loaded only when a human actually prompts."""
    import questionary

    return questionary

# Base/auto-managed attributes the wizard never asks for: type and the dates are
# stamped by the core writer; draft is a separate publish confirm.
_RESERVED_FIELDS = frozenset({"type", "created", "updated", "draft"})
_SKIP = "(skip)"


@dataclass(frozen=True)
class CliState:
    """Global CLI state stashed on ``ctx.obj`` by the root callback."""

    workspace: Path | None
    agent: bool


def can_prompt(state: CliState | None, fmt: str) -> bool:
    """Whether to prompt: a human, on a TTY, who did not ask for machine output.

    False for agents (``--agent``), for ``--format json``, and for any non-TTY (pipe,
    redirect, CI) — so a missing value falls back to today's error, never a hang on stdin.
    """
    if state is not None and state.agent:
        return False
    if fmt == "json":
        return False
    return is_tty()


def _ask(question: Any) -> Any:
    """Run a questionary question; a cancel (Ctrl-C / ESC) aborts the command cleanly."""
    answer = question.ask()
    if answer is None:
        raise typer.Abort()
    return answer


class Prompter:
    """Thin questionary wrapper. Constructed only when :func:`can_prompt` is True."""

    def text(self, message: str, *, default: str | None = None) -> str:
        answer: str = _ask(_q().text(message, default=default or ""))
        return answer.strip()

    def select(self, message: str, choices: Sequence[str], *, default: str | None = None) -> str:
        answer: str = _ask(_q().select(message, choices=list(choices), default=default))
        return answer

    def checkbox(self, message: str, choices: Sequence[str]) -> list[str]:
        picks: list[str] = _ask(_q().checkbox(message, choices=list(choices)))
        return picks

    def confirm(self, message: str, *, default: bool = True) -> bool:
        answer: bool = _ask(_q().confirm(message, default=default))
        return answer

    def path(self, message: str, *, default: str | None = None) -> Path:
        # No pre-filled default: questionary would make it editable text, so a typed
        # absolute path lands appended to it. Prompt empty, coerce blank → the default.
        answer: str = _ask(_q().path(message))
        return Path(answer.strip() or default or ".")


def make_prompter(state: CliState | None, fmt: str) -> Prompter | None:
    """The single seam. Returns a :class:`Prompter` when interactive, else ``None``.

    Because every command routes through here, a test monkeypatches this function to a
    fake that both forces the wizard branch (returns non-None under a non-TTY runner)
    and scripts answers.
    """
    return Prompter() if can_prompt(state, fmt) else None


def _label(name: str, required: bool) -> str:
    return f"{name} (required)" if required else name


# A lister maps a relation's target type names to pickable ``type/slug`` ids.
TargetLister = Callable[[Sequence[str]], list[str]]


def _prompt_attr(prompter: Prompter, attr: dict[str, Any]) -> str | None:
    """One scalar/enum field: enum → select, else free text. ``None`` means skipped."""
    label = _label(attr["name"], attr["required"])
    if attr["enum"]:
        choice = prompter.select(label, choices=[_SKIP, *attr["enum"]])
        return None if choice == _SKIP else choice
    return prompter.text(label) or None


def _prompt_relation(prompter: Prompter, rel: dict[str, Any], list_targets: TargetLister) -> str | None:
    """One relation: pick from existing targets (checkbox when ``many``), free-text slug
    when none exist yet. ``None`` means skipped; a many-relation joins picks with commas."""
    label = _label(rel["predicate"], rel["required"])
    targets = list_targets(rel["to"])
    if not targets:
        hint = "/".join(rel["to"]) or "target"
        return prompter.text(f"{label}: no {hint} yet, type a slug or skip") or None
    if rel["many"]:
        picks = prompter.checkbox(label, choices=targets)
        return ",".join(picks) if picks else None
    choice = prompter.select(label, choices=[_SKIP, *targets])
    return None if choice == _SKIP else choice


def editable_names(type_view: dict[str, Any]) -> list[str]:
    """The field and relation names a human may set — the ``edit`` pick-list."""
    attrs = [a["name"] for a in type_view["fields"] if a["name"] not in _RESERVED_FIELDS]
    return attrs + [r["predicate"] for r in type_view["relations"]]


def prompt_field_by_name(
    prompter: Prompter, type_view: dict[str, Any], name: str, list_targets: TargetLister
) -> str | None:
    """Prompt the value for one named field/relation, using its schema spec (``edit``)."""
    for attr in type_view["fields"]:
        if attr["name"] == name:
            return _prompt_attr(prompter, attr)
    for rel in type_view["relations"]:
        if rel["predicate"] == name:
            return _prompt_relation(prompter, rel, list_targets)
    return prompter.text(name) or None


def prompt_entity_fields(
    prompter: Prompter, type_view: dict[str, Any], list_targets: TargetLister
) -> dict[str, str]:
    """Prompt a type's fields and relations from its ``type_view``; return a field dict.

    Enums become selects, relations become picks over existing targets (a free-text
    slug when none exist yet), everything else is free text. Required is marked but
    never forced — an empty answer is skipped, so capture stays unblocked. The result
    is the same ``{field: value}`` mapping the ``--field value`` flags produce, so the
    interactive and headless paths hit the identical core writer.
    """
    fields: dict[str, str] = {}
    for attr in type_view["fields"]:
        if attr["name"] in _RESERVED_FIELDS:
            continue
        value = _prompt_attr(prompter, attr)
        if value is not None:
            fields[attr["name"]] = value
    for rel in type_view["relations"]:
        value = _prompt_relation(prompter, rel, list_targets)
        if value is not None:
            fields[rel["predicate"]] = value
    return fields
