"""``khub wire`` — link a khub workspace into agent context files (CLAUDE.md / AGENTS.md).

Injects an idempotent, marker-delimited block that does two things:

1. Links the schema into the agent's context. ``CLAUDE.md`` gets a Claude Code
   ``@.khub/schema.yaml`` import (loaded into context every session); ``AGENTS.md``
   (the cross-agent standard, which has no import directive) gets a plain pointer to
   read the schema file. The block also names the active preset and the declared types.
2. Documents the khub command surface, for when the CLI is available.

Bare ``wire`` updates whichever context files already exist and creates none;
``--target`` (claude/agents/both) targets a specific file, creating it if missing.
Schema-generic: the block is built from the workspace at call time (the active preset
from ``.khub/config.yaml`` and the declared types from the resolved schema), with no
per-type code path. Re-running replaces the block in place, so the write stays a
minimal, idempotent diff.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from khub.core.introspect import load_schema, types_list
from khub.core.locate import provenance

BEGIN = "<!-- khub:begin -->"
END = "<!-- khub:end -->"


@dataclass(frozen=True)
class WireOutcome:
    """What happened to one wired file."""

    path: Path
    action: str  # "created" | "updated" | "unchanged"


@dataclass(frozen=True)
class WireResult:
    """A dry-run preview of the target blocks and the per-file outcomes."""

    preview: str
    outcomes: list[WireOutcome]


def build_block(
    preset: str, version: str, types: list[str], *, import_supported: bool = True
) -> str:
    """The managed context-file block (markers included, no trailing newline).

    With ``import_supported`` (CLAUDE.md), the ontology is pulled in by a Claude Code
    ``@.khub/schema.yaml`` import: resolved relative to the file it sits in, it loads the
    ontology into context every session, and stays on its own line outside any code fence
    so the import fires. Without it (AGENTS.md and other agents, which have no import
    directive), the block points the agent at the schema file to read instead.
    """
    stamp = f"{preset}@{version}" if version else (preset or "custom")
    type_list = ", ".join(f"`{t}`" for t in types) if types else "none declared yet"
    if import_supported:
        link = [
            (
                "The ontology is imported below, so it loads into context even without "
                "running the khub CLI:"
            ),
            "",
            "@.khub/schema.yaml",
            "",
        ]
    else:
        link = [
            (
                "The ontology is defined in the schema file below; read it to work in this "
                "model, even without running the khub CLI:"
            ),
            "",
        ]
    return "\n".join(
        [
            BEGIN,
            "## khub workspace",
            "",
            (
                "This repository is a [khub](https://github.com/endgame-build/khub) "
                "workspace: its domain is modeled as typed entities and typed relations, and "
                "the schema is the contract. Reason in that model."
            ),
            "",
            *link,
            (
                f"Schema file: [`.khub/schema.yaml`](.khub/schema.yaml). Preset: `{stamp}`. "
                f"Entity types: {type_list}."
            ),
            "",
            "When khub is installed, prefer it for typed reads and writes over grepping files:",
            "",
            "- Introspect: `khub schema`, `khub schema show <type>`, `khub status`.",
            (
                "- Read: `khub query --type <t>`, `khub get <id> --edges`, "
                "`khub neighbors <id>`, `khub impact <id>`, `khub history <id>`, "
                "`khub search <text>`."
            ),
            (
                "- Write: `khub add <type> --<field> <v>`, `khub edit <id> <field> <v>`, "
                "`khub link <id> <pred> <target>`, `khub unlink`, `khub remove <id>`. "
                "Capture is never blocked; `--draft` marks an entity unpublished."
            ),
            "- Every read takes `--format json` for machine-readable output.",
            "",
            (
                "Not installed? `uv tool install git+ssh://git@github.com/endgame-build/khub`, "
                "then `khub install-skills`."
            ),
            END,
        ]
    )


def _upsert(text: str, block: str) -> str:
    """Return ``text`` with ``block`` inserted, or replacing an existing block.

    Replaces everything between the markers (keeping surrounding content), or
    appends the block when the file has no markers. The result always ends with a
    trailing newline.
    """
    if BEGIN in text and END in text:
        pre = text[: text.index(BEGIN)]
        post = text[text.index(END) + len(END) :]
        merged = pre + block + post
    elif text.strip():
        merged = text.rstrip("\n") + "\n\n" + block
    else:
        merged = block
    return merged if merged.endswith("\n") else merged + "\n"


def wire(
    root: Path, *, claude: bool = False, agents: bool = False, dry_run: bool = False
) -> WireResult:
    """Wire the workspace at ``root`` into agent context files.

    With ``claude``/``agents`` set, those files are the targets and are created if
    missing. With neither set, the targets are whichever of ``CLAUDE.md`` / ``AGENTS.md``
    already exist (updated in place; none created). Writes each target unless the content
    is unchanged (or ``dry_run`` is set). Idempotent.
    """
    prov = provenance(root)
    resolved = load_schema(root)
    types = types_list(resolved)
    claude_block = build_block(prov["preset"], prov["version"], types, import_supported=True)
    agents_block = build_block(prov["preset"], prov["version"], types, import_supported=False)
    candidates = [(root / "CLAUDE.md", claude_block), (root / "AGENTS.md", agents_block)]

    if claude or agents:
        chosen = {"CLAUDE.md": claude, "AGENTS.md": agents}
        targets = [(p, b) for p, b in candidates if chosen[p.name]]
    else:
        targets = [(p, b) for p, b in candidates if p.exists()]

    outcomes: list[WireOutcome] = []
    previews: list[str] = []
    for path, block in targets:
        previews.append(f"# {path.name}\n{block}")
        old = path.read_text(encoding="utf-8") if path.exists() else None
        new = _upsert(old or "", block)
        if old is None:
            action = "created"
        elif new == old:
            action = "unchanged"
        else:
            action = "updated"
        if not dry_run and action != "unchanged":
            path.write_text(new, encoding="utf-8")
        outcomes.append(WireOutcome(path=path, action=action))
    return WireResult(preview="\n\n".join(previews), outcomes=outcomes)
