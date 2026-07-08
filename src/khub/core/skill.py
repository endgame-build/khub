"""Install the khub agent skill via ``npx skills`` (the tail of ``khub init``).

Shells out to Vercel's skills.sh CLI (``npx skills``, the agent-agnostic skill
installer) to drop the khub skill into whichever coding agent is present —
Claude Code, Cursor, Codex, and ~70 others — and to write a ``skills-lock.json``.
Same convention Neon's ``neon init`` uses.

Best-effort by design: if ``npx`` is absent it is skipped, and a non-zero exit is
reported, not raised. Scaffolding a workspace must never hard-depend on Node.
"""

from __future__ import annotations

import shutil
import subprocess
from dataclasses import dataclass
from pathlib import Path

# The private repo is the skill source; the git URL form installs over SSH — the
# same access ``uv tool install`` already needs — where the ``owner/repo``
# shorthand would need a token.
SKILLS_SOURCE = "git@github.com:endgame-build/knowledge-hub.git"


@dataclass(frozen=True)
class SkillOutcome:
    """What happened when installing the agent skill."""

    action: str  # "installed" | "skipped-no-npx" | "failed"
    command: list[str]


def _command() -> list[str]:
    return [
        "npx",
        "-y",
        "skills",
        "add",
        SKILLS_SOURCE,
        "--skill",
        "khub",
        "--skill",
        "setup",
        "--yes",
    ]


def install_skill(root: Path) -> SkillOutcome:
    """Install the khub skill into the agent(s) detected under ``root``.

    Runs ``npx skills add`` with ``cwd=root`` so the skill and its
    ``skills-lock.json`` land in the workspace. Returns an outcome; never raises.
    """
    cmd = _command()
    if shutil.which("npx") is None:
        return SkillOutcome(action="skipped-no-npx", command=cmd)
    completed = subprocess.run(cmd, cwd=root)  # inherit the terminal so npx shows progress
    action = "installed" if completed.returncode == 0 else "failed"
    return SkillOutcome(action=action, command=cmd)
