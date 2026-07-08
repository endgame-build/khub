"""``khub wire`` — link a khub workspace into CLAUDE.md (and optionally AGENTS.md).

Injects an idempotent, marker-delimited block that does two things:

1. Links the schema into the agent's context with a Claude Code ``@.khub/schema.yaml``
   import, so an agent reasons in the workspace's ontology even if it never runs
   the khub CLI. The block also names the active preset and the declared types.
2. Documents the khub command surface, for when the CLI is available.

Schema-generic: the block is built from the workspace at call time (the active
preset from ``.khub/config.yaml`` and the declared types from the resolved
schema), with no per-type code path. Re-running replaces the block in place, so
the write stays a minimal, idempotent diff.
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
    """The block written and the per-file outcomes."""

    block: str
    outcomes: list[WireOutcome]


def build_block(preset: str, version: str, types: list[str]) -> str:
    """The managed CLAUDE.md block (markers included, no trailing newline).

    The ``@.khub/schema.yaml`` line is a Claude Code import: resolved relative to
    the CLAUDE.md it sits in, it loads the ontology into context every session.
    It stays on its own line and outside any code fence so the import fires.
    """
    stamp = f"{preset}@{version}" if version else (preset or "custom")
    type_list = ", ".join(f"`{t}`" for t in types) if types else "none declared yet"
    return "\n".join(
        [
            BEGIN,
            "## khub workspace",
            "",
            "This repository is a [khub](https://github.com/endgame-build/knowledge-hub) "
            "workspace: its domain is modeled as typed entities and typed relations, and the "
            "schema is the contract. Reason in that model.",
            "",
            "The ontology is imported below, so it loads into context even without running the "
            "khub CLI:",
            "",
            "@.khub/schema.yaml",
            "",
            f"Schema file: [`.khub/schema.yaml`](.khub/schema.yaml). Preset: `{stamp}`. "
            f"Entity types: {type_list}.",
            "",
            "When khub is installed, prefer it for typed reads and writes over grepping files:",
            "",
            "- Introspect: `khub schema`, `khub schema show <type>`, `khub status`.",
            "- Read: `khub query --type <t>`, `khub get <id> --edges`, `khub neighbors <id>`, "
            "`khub impact <id>`, `khub history <id>`, `khub search <text>`.",
            "- Write: `khub add <type> --<field> <v>`, `khub edit <id> <field> <v>`, "
            "`khub link <id> <pred> <target>`, `khub unlink`, `khub remove <id>`. Capture is "
            "never blocked; `--draft` marks an entity unpublished.",
            "- Every read takes `--format json` for machine-readable output.",
            "",
            "Not installed? Run `/khub:setup`, or "
            "`uv tool install git+ssh://git@github.com/endgame-build/knowledge-hub`.",
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


def wire(root: Path, *, agents: bool = False, dry_run: bool = False) -> WireResult:
    """Wire the workspace at ``root`` into CLAUDE.md (and AGENTS.md if ``agents``).

    Reads the active preset and declared types, builds the block, and writes each
    target unless the content is unchanged (or ``dry_run`` is set). Idempotent.
    """
    prov = provenance(root)
    resolved = load_schema(root)
    block = build_block(prov["preset"], prov["version"], types_list(resolved))

    targets = [root / "CLAUDE.md"]
    if agents:
        targets.append(root / "AGENTS.md")

    outcomes: list[WireOutcome] = []
    for path in targets:
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
    return WireResult(block=block, outcomes=outcomes)
