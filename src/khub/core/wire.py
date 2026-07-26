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


def _when_to_record(whens: dict[str, str] | None) -> list[str]:
    """The capture triggers, rendered from each type's schema-declared ``when``.

    The block already said HOW to write. It never said WHEN, and a real-codebase eval
    showed that is where wired agents lose: most off-rails operations were no command at
    all, because a terse ask ("Note it.") read as conversation rather than work. These
    lines are per-domain and come verbatim from the schema, so a new type ships its own
    trigger and no surface code learns a type name.
    """
    if not whens:
        return []
    return [
        "Record as you go — when one of these moments occurs, capture it:",
        "",
        *[f"- `{t}` — {w}" for t, w in whens.items()],
        "",
        (
            "A stated fact about the system is a capture request, whatever the wording. "
            '"note it", "write it down", "log it", "FYI", "heads up", "for the record" — and a '
            "bare statement with no instruction at all — all mean record it. Do that, then say "
            "what you recorded and its id. Answering \"Noted.\" without a record does not "
            "complete the task, and neither does asking which file to write to: entities are "
            "written with `khub add`, never by choosing a path."
        ),
        "",
    ]


def build_block(
    preset: str,
    version: str,
    types: list[str],
    *,
    import_supported: bool = True,
    whens: dict[str, str] | None = None,
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
            *_when_to_record(whens),
            (
                "Every write goes through the CLI — it is the only path that validates against "
                "the schema and resolves relations. If you edit an entity file by hand anyway "
                "(or a human did), run `khub validate` on it immediately: an unvalidated "
                "hand-edit is how a workspace acquires a field no gate will ever report."
            ),
            "",
            "Reading the graph is a khub operation too — query it, do not grep it:",
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
            (
                "- Every read and every write above takes `--format json`. Piped output is "
                "JSON by default; a table is only for a TTY. (`reindex`, `viz`, `backfill` "
                "and `wire` are operator commands and print prose.)"
            ),
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

    With ``claude``/``agents`` set, those files are the targets. With neither set, both
    are — and either that is missing is created, so a repo carrying only one context file
    (or neither) ends up wired for every agent that reads it rather than silently for
    some. Writes each target unless the content is unchanged (or ``dry_run`` is set).
    Idempotent.
    """
    prov = provenance(root)
    resolved = load_schema(root)
    types = types_list(resolved)
    whens = {name: t.when for name, t in resolved.types.items() if t.when}
    claude_block = build_block(
        prov["preset"], prov["version"], types, import_supported=True, whens=whens
    )
    agents_block = build_block(
        prov["preset"], prov["version"], types, import_supported=False, whens=whens
    )
    candidates = [(root / "CLAUDE.md", claude_block), (root / "AGENTS.md", agents_block)]

    if claude or agents:
        chosen = {"CLAUDE.md": claude, "AGENTS.md": agents}
        targets = [(p, b) for p, b in candidates if chosen[p.name]]
    else:
        targets = candidates

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
