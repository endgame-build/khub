"""Git-derived reads — ``khub stale`` and ``khub log`` (WPK-004-2, FS-004).

Two reads round out the integrity loop, both derived from git, never from
hand-kept state:

- ``stale``: entities whose effective date is past a threshold (default: the
  workspace's ``stale_days``), oldest first. The effective date is the ``updated``
  field when present, else the last-commit date read from ``git log`` (read, never
  written — INT-008). A non-git workspace falls back to ``updated`` alone.
- ``log``: ``git log`` rendered at ontology altitude — each commit's changed files
  mapped to entity ids and the relation predicates touched, never raw file paths.
  ``log <id>`` filters to one entity. A no-git workspace is a no-op success.

The ``git log`` date helper is shared with ``backfill`` (FS-005) and with
``validate --fix``. Every git call is read-only.
"""

from __future__ import annotations

import subprocess
from dataclasses import dataclass, field
from datetime import date
from pathlib import Path
from typing import Any

from khub.core import formats
from khub.core.entity import entity_path, resolve_id
from khub.core.index import build_index, filter_index, stray_nodes
from khub.core.introspect import load_schema
from khub.core.project import effective_date, stale_days

# ASCII control bytes as field/record separators in the git format string: they
# never appear in a commit hash or ISO date, so parsing stays unambiguous even
# when a path or date would otherwise collide with a printable delimiter.
_REC = "\x1e"  # record separator: one per commit
_FLD = "\x1f"  # field separator: hash | date within the commit header

# INT-007 previously kept a private 30-day default here, distinct from the workspace
# `stale_days`. The divergence was inert — the CLI always passed the configured
# `stale_days`, so the 30-day literal surfaced only to direct library callers. Unified:
# a `None` threshold now resolves to the one source, `project.stale_days(root)`.


# --- git helpers (read-only) -------------------------------------------------


def _git(root: Path, *args: str) -> subprocess.CompletedProcess[str]:
    """Run ``git -C root <args>`` capturing text output (never raises on failure)."""
    return subprocess.run(  # noqa: S603,S607 — fixed argv, no shell
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
    ``stale``/``log`` never needed, so it extends the shared helper here. ``git log``
    lists newest-first, so the oldest commit is the last line; committer date (``%cd``)
    matches ``last_commit_date`` and ``log``.

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
        # Only reach for git when `updated` is absent — avoids a subprocess per dated entity.
        git_date = None
        if git_ok and meta.get("updated") is None:
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


# --- log ---------------------------------------------------------------------


@dataclass(frozen=True)
class LogEntry:
    """One commit's change to one entity, at ontology altitude (no file path)."""

    commit: str  # short hash
    type: str
    slug: str
    date: str  # committer ISO datetime
    relations: list[str] = field(default_factory=list)  # predicates touched


def log(
    root: Path,
    id_: str | None = None,
    *,
    limit: int | None = None,
    since: str | None = None,
) -> list[LogEntry] | None:
    """Git history mapped to entities and the relations each commit touched.

    Returns ``None`` when the workspace has no git history (the caller reports the
    no-op success). With ``id_``, only commits touching that entity are returned;
    ``limit`` caps the commit count and ``since`` bounds the window. The relations
    a commit touched are computed by diffing the file's frontmatter against its
    parent — an attribute-only edit yields an empty relation list, the entity still
    named.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    valid = filter_index(index, stray_nodes(index))  # strays are not entities
    # Resolve the id BEFORE the git check, so a bad/ambiguous id raises the located
    # lookup error whether or not the workspace has git history (a non-git `log
    # ghost` must fail the same way it does in a repo, not report "no history").
    target = resolve_id(valid, id_) if id_ else None
    if not has_git_history(root):
        return None

    path_to_node = {
        str(entity_path(root, resolved.types[t], s).relative_to(root)): (t, s)
        for t, s in valid.nodes
    }
    target_rel = (
        str(entity_path(root, resolved.types[target[0]], target[1]).relative_to(root))
        if target
        else None
    )

    args = ["log", f"--format={_REC}%h{_FLD}%cI", "--name-only"]
    if limit is not None:
        args.append(f"--max-count={limit}")
    if since is not None:
        args.append(f"--since={since}")
    if target_rel is not None:
        args += ["--", target_rel]
    out = _git(root, *args).stdout

    entries: list[LogEntry] = []
    for chunk in out.split(_REC):
        if not chunk.strip():
            continue
        lines = chunk.splitlines()
        commit, when = lines[0].split(_FLD)
        files = [ln for ln in lines[1:] if ln.strip()]
        for relpath in files:
            node = path_to_node.get(relpath)
            if node is None or (target is not None and node != target):
                continue
            rtype = resolved.types[node[0]]
            entries.append(
                LogEntry(
                    commit=commit,
                    type=node[0],
                    slug=node[1],
                    date=when,
                    relations=_relations_changed(root, commit, relpath, rtype),
                )
            )
    return _since_exact(entries, since)


def _since_exact(entries: list[LogEntry], since: str | None) -> list[LogEntry]:
    """Inclusive ``--since`` to the day: git's ``--since`` is approximate, so re-filter.

    Only applied when ``since`` is an ISO date; a relative spec (``2 weeks ago``) is
    left to git's coarse filter.
    """
    if since is None:
        return entries
    try:
        floor = date.fromisoformat(since)
    except ValueError:
        return entries
    return [e for e in entries if date.fromisoformat(e.date[:10]) >= floor]


def _relations_changed(root: Path, commit: str, relpath: str, rtype: Any) -> list[str]:
    """The relation predicates whose value changed in ``commit`` versus its parent."""
    new = _frontmatter_at(root, commit, relpath)
    old = _frontmatter_at(root, f"{commit}^", relpath)  # empty when the file was added
    return [pred for pred in rtype.relations if _rel_value(new.get(pred)) != _rel_value(old.get(pred))]


def _rel_value(value: Any) -> Any:
    """A relation value normalized for change comparison: a many-valued list compares
    on membership, not order — a pure reorder is not a change."""
    return sorted(map(str, value)) if isinstance(value, list) else value


def _frontmatter_at(root: Path, rev: str, relpath: str) -> dict[str, Any]:
    """The metadata of ``relpath`` at ``rev``; empty if absent or unparseable."""
    res = _git(root, "show", f"{rev}:{relpath}")
    if res.returncode != 0:
        return {}
    try:
        return formats.parse(res.stdout, formats.fmt_of(relpath))[0]
    except Exception:  # noqa: BLE001 — a malformed blob is "no relations changed"
        return {}
