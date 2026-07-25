"""Install khub's agent skills by copying them out of the package.

khub authors its skills in ``skills/`` at the repo root and ships them as package
data. Installing one is a file copy into whichever skill directories the local
agents read — no Node, no network, no clone, and nothing that can fail on a
machine without egress.

Until 0.10.0 this shelled out to ``npx skills add <private repo>``, because
``plugin/skills/`` sat outside the wheel and genuinely was not on disk at
runtime. That made a two-file copy depend on Node, network, and SSH access to a
private repo. ``npx skills add`` still works against the repo — root ``skills/``
is a container skills.sh discovers without a manifest — but it is a bootstrap
route for machines that have no khub yet, not something khub itself invokes.

Installed skills are managed copies: a re-install re-syncs them to the packaged
content, so edits belong in ``skills/``, not in an installed copy.
"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

from khub.core.errors import LocatedError

# Skills live at the repo root (the container `npx skills add` matches without a
# manifest) and reach the wheel through hatch's force-include, which lands them
# beside `presets/` and `assets/`. A dev checkout has no packaged copy, hence the
# repo fallback: src/khub/core/skill.py -> parents[3] is the repo root.
_PACKAGED_SKILLS = Path(__file__).resolve().parent.parent / "skills"
_REPO_SKILLS = Path(__file__).resolve().parents[3] / "skills"

# Where each agent family reads project-local skills, and the home equivalent.
# opencode reads all three project dirs; the copies are byte-identical, so a name
# collision resolves to the same skill rather than one shadowing another.
TARGETS: dict[str, tuple[str, str]] = {
    "claude": (".claude/skills", ".claude/skills"),
    "agents": (".agents/skills", ".agents/skills"),
    "opencode": (".opencode/skills", ".config/opencode/skills"),
}


def skills_dir() -> Path:
    """The packaged skills directory, or the repo's in a dev checkout."""
    for candidate in (_PACKAGED_SKILLS, _REPO_SKILLS):
        if candidate.is_dir():
            return candidate
    raise LocatedError(
        code="skills_missing",
        message=(
            f"No skills directory found at {_PACKAGED_SKILLS} or {_REPO_SKILLS}; "
            "the khub install is incomplete (skills are packaged from the repo's skills/)"
        ),
    )


def available_skills() -> list[str]:
    """Every skill khub ships, by directory name."""
    return sorted(p.parent.name for p in skills_dir().glob("*/SKILL.md"))


@dataclass(frozen=True)
class SkillWrite:
    """One destination file and what happened to it."""

    path: str  # relative to the workspace root, or absolute under --global
    action: str  # "created" | "updated" | "unchanged"


@dataclass(frozen=True)
class SkillReport:
    """The outcome of one install: where it wrote, and what it wrote."""

    scope: str  # "project" | "global"
    skills: list[str]
    writes: list[SkillWrite]
    dry_run: bool = False


def install_skills(
    root: Path,
    *,
    skills: Sequence[str] | None = None,
    targets: Sequence[str] | None = None,
    scope_global: bool = False,
    dry_run: bool = False,
) -> SkillReport:
    """Copy the named skills into the target directories under ``root`` (or ``$HOME``).

    Every file is compared before it is written, so a re-install reports
    ``unchanged`` and leaves mtimes alone — the command is safe to run from a
    session hook. ``dry_run`` reports the same writes without touching disk.
    Project scope also gitignores the skill directories: they are reproducible
    from the installed khub, so committing them would be committing a copy.
    """
    from khub.core.workspace import _append_gitignore

    source = skills_dir()
    wanted = list(skills) if skills else available_skills()
    _reject_unknown(wanted, available_skills(), "skill")
    chosen = list(targets) if targets else list(TARGETS)
    _reject_unknown(chosen, list(TARGETS), "target")

    base = Path.home() if scope_global else root
    writes: list[SkillWrite] = []
    for target in chosen:
        project_dir, home_dir = TARGETS[target]
        dest_root = base / (home_dir if scope_global else project_dir)
        for name in wanted:
            writes.extend(_sync_skill(source / name, dest_root / name, base, dry_run))
        if not scope_global and not dry_run:
            # Project scope only: a home directory is not a git repo.
            _append_gitignore(root / ".gitignore", f"{project_dir}/")

    return SkillReport(
        scope="global" if scope_global else "project",
        skills=wanted,
        writes=writes,
        dry_run=dry_run,
    )


def _sync_skill(src: Path, dest: Path, base: Path, dry_run: bool) -> list[SkillWrite]:
    """Copy one skill directory, reporting an action per file."""
    writes: list[SkillWrite] = []
    for item in sorted(p for p in src.rglob("*") if p.is_file()):
        target = dest / item.relative_to(src)
        payload = item.read_bytes()
        if not target.exists():
            action = "created"
        elif target.read_bytes() == payload:
            action = "unchanged"
        else:
            action = "updated"
        if not dry_run and action != "unchanged":
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(payload)
        writes.append(SkillWrite(path=_display(target, base), action=action))
    return writes


def _display(path: Path, base: Path) -> str:
    """Workspace-relative where possible; absolute under --global."""
    try:
        return str(path.relative_to(base))
    except ValueError:
        return str(path)


def _reject_unknown(given: Sequence[str], known: Sequence[str], kind: str) -> None:
    unknown = [g for g in given if g not in known]
    if unknown:
        raise LocatedError(
            code=f"unknown_{kind}",
            message=(
                f"Unknown {kind}: {', '.join(sorted(unknown))}. "
                f"Known {kind}s: {', '.join(known)}"
            ),
        )
