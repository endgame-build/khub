"""Git-derived reads — ``khub stale`` (WPK-004-2, FS-004).

``stale`` reports entities whose effective date is past a threshold (default: the
workspace's ``stale_days``), oldest first. The effective date is the ``updated``
field when present, else the last-commit date read from ``git log`` (read, never
written — INT-008). A non-git workspace falls back to ``updated`` alone.

``khub log`` lived here until 0.9.0, rendering git history at ontology altitude.
It was removed: ``khub history`` answers the graph-side question and ``git log --
<path>`` answers the rest, neither of which needed the per-row commit attribution
that made up most of this module.

The ``git log`` date helpers are shared with ``backfill`` (FS-005). Every git call
is read-only.
"""

from __future__ import annotations

import subprocess
from dataclasses import dataclass
from datetime import date
from pathlib import Path

from khub.core.entity import entity_path
from khub.core.index import build_index, filter_index, stray_nodes
from khub.core.introspect import load_schema
from khub.core.project import effective_date, stale_days

# INT-007 previously kept a private 30-day default here, distinct from the workspace
# `stale_days`. The divergence was inert — the CLI always passed the configured
# `stale_days`, so the 30-day literal surfaced only to direct library callers. Unified:
# a `None` threshold now resolves to the one source, `project.stale_days(root)`.


# --- git helpers (read-only) -------------------------------------------------


def _git(root: Path, *args: str) -> subprocess.CompletedProcess[str]:
    """Run ``git -C root <args>`` capturing text output (never raises on failure)."""
    return subprocess.run(
        ["git", "-C", str(root), *args],
        capture_output=True,
        text=True,
        check=False,
    )


def has_git_history(root: Path) -> bool:
    """Whether ``root`` is a git repo with at least one commit.

    A repo with no commits (``HEAD`` unborn) reads the same as a non-repo here:
    neither has history to derive a date or a change list from.
    """
    return _git(root, "rev-parse", "--verify", "HEAD").returncode == 0


def last_commit_date(root: Path, relpath: str) -> date | None:
    """The committer date of the last commit touching ``relpath``, or None if untracked.

    Committer date (not author date) so a single commit reads the same here and in
    ``log`` (which renders ``%cI``) — a rebased/amended commit otherwise shows two
    different dates across the two commands.
    """
    res = _git(root, "log", "-1", "--format=%cd", "--date=short", "--", relpath)
    out = res.stdout.strip()
    if res.returncode != 0 or not out:
        return None
    return date.fromisoformat(out)


def first_commit_date(root: Path, relpath: str) -> date | None:
    """The committer date of the *first* commit touching ``relpath``, or None if untracked.

    ``backfill`` (FS-005) derives ``created`` from the first commit — the read
    ``stale`` never needed, so it extends the shared helper here. ``git log`` lists
    newest-first, so the oldest commit is the last line; committer date (``%cd``)
    matches ``last_commit_date``.

    # ponytail: lines[-1] is the oldest commit on a linear history (the common cutover
    # case). A merge with non-monotonic committer dates could reorder the tail; lift to
    # `git log --diff-filter=A` (the add commit) if a merge-heavy history mis-dates `created`.
    """
    res = _git(root, "log", "--format=%cd", "--date=short", "--", relpath)
    lines = res.stdout.strip().splitlines()
    if res.returncode != 0 or not lines:
        return None
    return date.fromisoformat(lines[-1])


# --- stale -------------------------------------------------------------------


@dataclass(frozen=True)
class StaleEntry:
    """One entity past the threshold, with the date it was judged against."""

    type: str
    slug: str
    effective_date: date
    age: int  # days since the effective date; drives the oldest-first sort
    source: str  # "updated" | "git log"


@dataclass(frozen=True)
class StaleReport:
    """The stale set plus whether git was available to backfill missing dates."""

    entries: list[StaleEntry]
    git_available: bool


def stale(root: Path, *, days: int | None = None, now: date) -> StaleReport:
    """Entities whose effective date is more than ``days`` old, oldest first.

    ``days`` defaults to ``None``, which resolves to the workspace's ``stale_days`` —
    the single threshold source shared with ``status``/``query``. The effective date
    is ``updated`` when present, else the git last-commit date (read-only). Entities
    with neither are skipped (no date to judge). Sort is by age descending.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    valid = filter_index(index, stray_nodes(index))  # strays are not entities
    git_ok = has_git_history(root)
    threshold = stale_days(root) if days is None else days

    entries: list[StaleEntry] = []
    for type_, slug in sorted(valid.nodes):
        meta = valid.meta[(type_, slug)]
        # Only reach for git when `updated` is absent — avoids a subprocess per dated
        # entity. A collection row never takes the file's commit date: any row's edit
        # bumps it, so attributing it would make every other row read never-stale — a
        # dated lie, not an approximation. An undated row is skipped instead.
        git_date = None
        if (
            git_ok
            and meta.get("updated") is None
            and resolved.types[type_].storage.layout != "collection"
        ):
            path = entity_path(root, resolved.types[type_], slug)
            git_date = last_commit_date(root, str(path.relative_to(root)))
        eff, source = effective_date(meta, git_date=git_date)
        if eff is None:
            continue
        age = (now - eff).days
        if age > threshold:
            entries.append(StaleEntry(type_, slug, eff, age, source))
    entries.sort(key=lambda e: (e.age, e.type, e.slug), reverse=True)
    return StaleReport(entries=entries, git_available=git_ok)
