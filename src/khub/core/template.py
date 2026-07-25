"""Body templates — TPL-001.

A template is a per-type YAML file at ``.khub/templates/<type>.yaml`` (flattened
from the preset's ``templates/`` dir at init, workspace-owned thereafter — the
same two-sided relationship as ``schema.yaml``). Its presence activates two
behaviours for an md type: ``add``/``init`` seed new bodies from it, and
``validate`` requires its section headings in every instance body as an ordered
subsequence (prefix-match, numbering stripped, extra headings allowed).

Shape::

    title: Product requirements   # optional; init-created singleton title
    sections:
      - heading: Vision           # required
        hint: one paragraph       # optional -> rendered as an HTML comment
      - heading: Non-goals
        text: |                   # optional literal pre-filled markdown
          Nothing here yet.

Entry keys are deliberately document-schema-aligned; ``optional``, ``repeat``,
``pattern``, and budget keys are reserved for later and rejected today with a
clear error rather than silently ignored.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path

from khub.core.errors import LocatedError
from khub.core.resolve import load_yaml

TEMPLATES_DIR = ".khub/templates"

_ENTRY_KEYS = {"heading", "hint", "text"}
_RESERVED_KEYS = {"optional", "repeat", "pattern", "min_tokens", "max_tokens", "budget"}
_TOP_KEYS = {"title", "sections"}

# Leading list numbering on a heading ("1.", "3)", "2.1"), stripped before matching.
_NUMBERING = re.compile(r"^\d+([.)]\d*)*[.)]?\s+")
_H2 = re.compile(r"^##\s+(.*)$", re.MULTILINE)
# Fenced code blocks — a `## heading` inside one is content, not structure, and
# must never satisfy a required section.
_FENCE = re.compile(r"^(```|~~~).*?^\1[^\S\n]*$", re.MULTILINE | re.DOTALL)


@dataclass(frozen=True)
class Section:
    """One template entry: the required heading plus its scaffold content."""

    heading: str
    hint: str | None = None
    text: str | None = None


@dataclass(frozen=True)
class BodyTemplate:
    """A parsed body template: scaffold source and section contract in one."""

    type: str
    title: str | None = None
    sections: tuple[Section, ...] = field(default_factory=tuple)

    def render(self) -> str:
        """The scaffolded body: each heading, then its hint comment / text."""
        parts: list[str] = []
        for s in self.sections:
            parts.append(f"## {s.heading}\n")
            if s.hint:
                parts.append(f"<!-- {s.hint} -->\n")
            if s.text:
                parts.append(s.text.rstrip("\n") + "\n")
        return "\n".join(parts)

    @property
    def required_headings(self) -> tuple[str, ...]:
        return tuple(s.heading for s in self.sections)


def template_path(root: Path, type_: str) -> Path:
    return root / TEMPLATES_DIR / f"{type_}.yaml"


def load_template(root: Path, type_: str) -> BodyTemplate | None:
    """The type's template, or None when there is no body contract.

    None means either no template file, or a file declaring ``sections: []`` — the
    explicit way to say "this type has a template (for `add` to seed a title) but
    requires no headings". A MISSING ``sections`` key is still an error: the file
    exists and declares nothing coherent, which is a typo, not an intent.
    """
    path = template_path(root, type_)
    if not path.is_file():
        return None
    data = load_yaml(path)
    if not isinstance(data, dict):
        raise _invalid(type_, "top level must be a mapping with a 'sections' list")
    unknown = set(data) - _TOP_KEYS
    if unknown:
        raise _invalid(type_, f"unknown top-level keys: {', '.join(sorted(unknown))}")
    if "sections" not in data:
        raise _invalid(type_, "'sections' is required (use `sections: []` for no body contract)")
    raw_sections = data["sections"]
    if not isinstance(raw_sections, list):
        raise _invalid(type_, "'sections' must be a list")
    if not raw_sections:
        # Declared, deliberately empty: no headings required — but still a template,
        # so `add` seeds the title and `init` still creates the singleton. Returning
        # None here read as "no template at all" to both callers.
        return BodyTemplate(type=type_, title=data.get("title"), sections=())
    sections: list[Section] = []
    for i, entry in enumerate(raw_sections):
        if not isinstance(entry, dict):
            raise _invalid(type_, f"sections[{i}] must be a mapping with a 'heading'")
        reserved = set(entry) & _RESERVED_KEYS
        if reserved:
            raise _invalid(
                type_,
                f"sections[{i}] uses reserved key(s) {', '.join(sorted(reserved))} — "
                "planned for a later version, not supported yet",
            )
        unknown = set(entry) - _ENTRY_KEYS
        if unknown:
            raise _invalid(type_, f"sections[{i}] unknown key(s): {', '.join(sorted(unknown))}")
        heading = entry.get("heading")
        if not heading or not isinstance(heading, str):
            raise _invalid(type_, f"sections[{i}] needs a non-empty string 'heading'")
        sections.append(
            Section(heading=heading.strip(), hint=entry.get("hint"), text=entry.get("text"))
        )
    title = data.get("title")
    return BodyTemplate(type=type_, title=title, sections=tuple(sections))


def body_h2s(body: str) -> list[str]:
    """The body's H2 headings, in order, numbering stripped; fenced code ignored."""
    return [_NUMBERING.sub("", h.strip()) for h in _H2.findall(_FENCE.sub("", body))]


def missing_heading(template: BodyTemplate, body: str) -> str | None:
    """The first template heading not found in order in ``body``, or None.

    The contract is an ordered subsequence: every template heading must appear,
    in template order, as a prefix of some body H2 (numbering stripped); extra
    body headings are allowed anywhere.
    """
    found = body_h2s(body)
    pos = 0
    for required in template.required_headings:
        while pos < len(found) and not found[pos].startswith(required):
            pos += 1
        if pos == len(found):
            return required
        pos += 1
    return None


def _invalid(type_: str, why: str) -> LocatedError:
    return LocatedError(
        code="template_invalid",
        message=f"Template for '{type_}' ({TEMPLATES_DIR}/{type_}.yaml): {why}",
    )
