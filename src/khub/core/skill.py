"""Install the khub agent skill via ``npx skills`` (``khub install-skills``).

Shells out to Vercel's skills.sh CLI (``npx skills``, the agent-agnostic skill
installer) to drop the khub skill into whichever coding agent is present —
Claude Code, Cursor, Codex, and ~70 others — and to write a ``skills-lock.json``.

Never raises: a missing ``npx``, an exec failure, or a non-zero exit comes back
as an outcome. This ran as a tail of ``khub init`` until 0.9.0, where that
tolerance was the point (a scaffold must not hard-depend on Node). As its own
command the tolerance stays here, and the CLI adapter turns a ``failed`` outcome
into exit 1 — the layer that knows the install was asked for is the layer that
decides it is fatal.

The clone runs over the inherited stdin so an interactive user can answer an ssh
host-key or passphrase prompt. In CI, configure ssh non-interactively
(``BatchMode=yes``) so a missing key fails fast instead of blocking on the prompt.
"""

from __future__ import annotations

import shutil
import subprocess
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

# The private repo is the skill source; the git URL form installs over SSH — the
# same access ``uv tool install`` already needs — where the ``owner/repo``
# shorthand would need a token.
SKILLS_SOURCE = "git@github.com:endgame-build/khub.git"

# npx drops the skill into per-machine agent directories; keep them out of git.
# The committable artifact is ``skills-lock.json``, which pins the skill versions.
_SKILL_ARTIFACT_DIRS = (".claude/skills/", ".agents/skills/")


@dataclass(frozen=True)
class SkillOutcome:
    """What happened when installing the agent skill."""

    action: str  # "installed" | "skipped-no-npx" | "failed"
    command: list[str]


def skill_command(agents: Sequence[str] | None = None) -> list[str]:
    """The ``npx skills add`` argv. Public so ``--dry-run`` can print exactly what would run."""
    cmd = [
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
    for agent in agents or ():
        cmd += ["--agent", agent]
    return cmd


def install_skill(
    root: Path, *, agents: Sequence[str] | None = None, quiet: bool = False
) -> SkillOutcome:
    """Install the khub skill into the agent(s) detected under ``root``. Never raises.

    Runs ``npx skills add`` with ``cwd=root`` so the skill and its
    ``skills-lock.json`` land in the workspace, then gitignores the per-machine
    skill directories. ``agents`` narrows the install to named coding agents
    (``--agent``, repeatable); ``None`` keeps npx's auto-detect. ``quiet``
    captures npx's output instead of inheriting the terminal, for machine-readable
    callers whose stdout must stay clean.
    """
    from khub.core.workspace import _append_gitignore

    cmd = skill_command(agents)
    if shutil.which("npx") is None:
        return SkillOutcome(action="skipped-no-npx", command=cmd)
    try:
        completed = subprocess.run(cmd, cwd=root, capture_output=quiet, text=quiet)
    except OSError:
        # npx resolved on PATH but exec still failed (Windows .cmd, stale/broken entry).
        return SkillOutcome(action="failed", command=cmd)
    if completed.returncode != 0:
        return SkillOutcome(action="failed", command=cmd)
    gitignore = root / ".gitignore"
    for pattern in _SKILL_ARTIFACT_DIRS:
        _append_gitignore(gitignore, pattern)
    return SkillOutcome(action="installed", command=cmd)
