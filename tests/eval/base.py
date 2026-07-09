"""Fixture + pre-flight for the wiring eval.

Builds a wired firm-ops workspace (managed CLAUDE.md block + the khub/setup
skills) and an unwired control (block + skill stripped), and guarantees the
`khub` the agents-under-test invoke is the *current repo's* build. Everything is
derived from this repo so the wiring under test is always the latest version.
"""

from __future__ import annotations

import json
import shutil
import subprocess
import tomllib
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SKILLS_SRC = REPO / "plugin" / "skills"


def _run(cmd: list[str], cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(cmd, cwd=cwd, check=check, text=True, capture_output=True)


def repo_version() -> str:
    data = tomllib.loads((REPO / "pyproject.toml").read_text())
    return str(data["project"]["version"])


def preflight() -> str:
    """Force-reinstall khub from this repo and assert the CLI is that version.

    The agents call bare `khub` on PATH; this makes that binary the code under
    test and aborts if a stale/shadowing khub wins. Returns the pinned version.
    """
    _run(["uv", "tool", "install", "--force", str(REPO)])
    which = _run(["which", "khub"]).stdout.strip()
    ver = _run(["khub", "--version"]).stdout.strip()
    want = repo_version()
    if ver != want:
        raise SystemExit(f"khub version mismatch: PATH khub is {ver!r} at {which}, repo is {want!r}")
    return ver


def entity_path_prefixes(ws: Path) -> list[str]:
    """Workspace-relative path prefixes that hold entities (the bypass detector).

    Read from the live schema so it stays schema-generic: every type's storage
    `path` (folder/file layouts) plus collection files.
    """
    out = _run(["khub", "-C", str(ws), "schema", "--format", "json"]).stdout
    schema = json.loads(out)
    prefixes: set[str] = set()
    for t in schema.get("types", []):
        p = t.get("path") or f"{t['name']}s"
        prefixes.add(p.rstrip("/"))
    return sorted(prefixes)


# --- fixture builders ---------------------------------------------------------


def _install_skills(ws: Path) -> None:
    dest = ws / ".claude" / "skills"
    dest.mkdir(parents=True, exist_ok=True)
    for name in ("khub", "setup"):
        shutil.copytree(SKILLS_SRC / name, dest / name, dirs_exist_ok=True)


def _install_instructions_hook(ws: Path) -> None:
    """Best-effort load-proof: log every InstructionsLoaded event to .eval/."""
    settings = ws / ".claude" / "settings.json"
    settings.parent.mkdir(parents=True, exist_ok=True)
    settings.write_text(
        json.dumps(
            {
                "hooks": {
                    "InstructionsLoaded": [
                        {"hooks": [{"type": "command", "command": "mkdir -p .eval && cat >> .eval/instructions.jsonl"}]}
                    ]
                }
            },
            indent=2,
        )
    )


def build_wired(dest: Path) -> Path:
    """A wired, empty firm-ops workspace grown by the agents themselves."""
    if dest.exists():
        shutil.rmtree(dest)
    _run(["khub", "init", "firm-ops", str(dest), "--no-skill"])
    _install_skills(dest)
    _install_instructions_hook(dest)
    return dest


def build_unwired(dest: Path, from_wired: Path) -> Path:
    """The control: same .khub/ + data, but no wired block and no skill."""
    from khub.core.wire import BEGIN, END

    if dest.exists():
        shutil.rmtree(dest)
    shutil.copytree(from_wired, dest)
    shutil.rmtree(dest / ".claude" / "skills", ignore_errors=True)
    claude = dest / "CLAUDE.md"
    if claude.exists():
        text = claude.read_text()
        if BEGIN in text and END in text:
            text = text[: text.index(BEGIN)] + text[text.index(END) + len(END) :]
        claude.write_text(text.strip() + "\n" if text.strip() else "")
    return dest


def fresh_copy(src: Path, dest: Path) -> Path:
    """An isolated copy of a workspace for one agent run."""
    if dest.exists():
        shutil.rmtree(dest)
    shutil.copytree(src, dest)
    return dest


if __name__ == "__main__":  # smoke: build both fixtures and print a summary
    import sys

    work = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("/tmp/khub-eval")
    print("khub pinned to", preflight())
    wired = build_wired(work / "wired")
    unwired = build_unwired(work / "unwired", wired)
    print("wired:", wired, "| block:", "khub:begin" in (wired / "CLAUDE.md").read_text(),
          "| skill:", (wired / ".claude/skills/khub/SKILL.md").exists())
    print("unwired:", unwired, "| block:", "khub:begin" in (unwired / "CLAUDE.md").read_text(),
          "| skill dir:", (unwired / ".claude/skills").exists())
    print("entity path prefixes:", entity_path_prefixes(wired))
