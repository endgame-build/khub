"""``khub backfill`` — bring an imported tree to a validatable state (WPK-005-2, FS-005).

The write path for cutover: read first- and last-commit dates from ``git log`` per
file and write ``created``/``updated`` where missing, and with ``--type <t>`` add the
missing per-type required scaffolding for entities of that type. Every write is
additive and minimal — only where a value is absent (never overwrites — PRJ-003), and
through the ``ruamel.yaml`` round-trip so authored values, key order, and inline
comments survive (PRJ-SHARED-002). ``--dry-run`` lists the changes and writes nothing;
a workspace with no git history skips date backfill and reports it.

Shares the ``git log`` date helper with ``stale``/``log`` (WPK-004-2) and extends it
with the first-commit read those reads never needed (``core.gitlog.first_commit_date``).
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any

from khub.core.entity import _read_doc, _write_doc, entity_path
from khub.core.errors import LocatedError
from khub.core.gitlog import first_commit_date, has_git_history, last_commit_date
from khub.core.index import build_index, filter_index, stray_nodes
from khub.core.integrity import _present
from khub.core.introspect import load_schema
from khub.core.locate import provenance
from khub.core.model import ResolvedType

# Placeholder written for a scaffolded required key (--type): a *null* value, not an
# empty string. Null is the only value that both keeps the field "missing" to `check`
# (so the gap is still surfaced — backfill scaffolds presence, never a fabricated value)
# AND passes `validate` (whose _attr_error short-circuits on a None value). An empty
# string would satisfy neither: `validate` rejects '' as a malformed enum/date/number,
# so scaffolding with '' turns a validate-clean tree into a validate-failing one — the
# opposite of the "bring an imported tree to a validatable state" goal.
_SCAFFOLD = None


@dataclass(frozen=True)
class BackfillChange:
    """One additive write: which entity, which field, the value, and where it came from."""

    type: str
    slug: str
    field: str
    value: str
    source: str  # "git log" (dates) | "type schema" (scaffolding)

    @property
    def id(self) -> str:
        return f"{self.type}/{self.slug}"


@dataclass(frozen=True)
class BackfillReport:
    """The set of changes plus whether git was available and whether anything was written."""

    changes: list[BackfillChange]
    git_available: bool
    dry_run: bool

    @property
    def dated_entities(self) -> int:
        """Distinct entities that received a git-sourced date — the ``N`` in the message."""
        return len({(c.type, c.slug) for c in self.changes if c.source == "git log"})

    @property
    def scaffolded_entities(self) -> int:
        """Distinct entities that received ``--type`` frontmatter scaffolding."""
        return len({(c.type, c.slug) for c in self.changes if c.source == "type schema"})


def backfill(root: Path, type_: str | None = None, *, dry_run: bool = False) -> BackfillReport:
    """Write missing dates (from git) and, with ``type_``, missing per-type scaffolding.

    Additive and missing-only: a field is written only when absent, so an authored
    value, key order, and comments are never disturbed (PRJ-003 / PRJ-SHARED-002).
    """
    resolved = load_schema(root)
    if type_ is not None and type_ not in resolved.types:
        # A misspelled --type must fail loudly, not scaffold nothing and report success.
        raise LocatedError.unknown_type(
            type_, provenance(root).get("preset", ""), sorted(resolved.types)
        )
    scanned = build_index(root, resolved)
    index = filter_index(scanned, stray_nodes(scanned))  # strays are not entities
    git_ok = has_git_history(root)

    changes: list[BackfillChange] = []
    for node in sorted(index.nodes):
        t, slug = node
        rtype = resolved.types[t]
        meta = index.meta[node]
        path = entity_path(root, rtype, slug)
        additions = _additions(root, path, rtype, meta, type_ == t, git_ok)
        if not additions:
            continue
        for field, (value, source) in additions.items():
            changes.append(BackfillChange(t, slug, field, "" if value is None else str(value), source))
        if not dry_run:
            _apply(path, {f: v for f, (v, _) in additions.items()})
    return BackfillReport(changes=changes, git_available=git_ok, dry_run=dry_run)


def _additions(
    root: Path, path: Path, rtype: ResolvedType, meta: dict[str, Any], scaffold: bool, git_ok: bool
) -> dict[str, tuple[Any, str]]:
    """The ``field -> (value, source)`` map of absent fields to write for one entity.

    Dates come first from git; ``--type`` scaffolding then covers any required key still
    absent (including ``created``/``updated`` when git could not supply them).
    """
    adds: dict[str, tuple[Any, str]] = {}
    if git_ok:
        relpath = str(path.relative_to(root))
        if meta.get("created") is None:
            d = first_commit_date(root, relpath)
            if d is not None:
                adds["created"] = (d, "git log")
        if meta.get("updated") is None:
            d = last_commit_date(root, relpath)
            if d is not None:
                adds["updated"] = (d, "git log")
    if scaffold:
        for key in _missing_required(rtype, meta):
            if key not in adds:
                adds[key] = (_SCAFFOLD, "type schema")
    return adds


def _missing_required(rtype: ResolvedType, meta: dict[str, Any]) -> list[str]:
    """The required attributes and relations absent on an entity — the scaffolding keys.

    Uses the same required-field knowledge ``validate``/``check`` use (FS-004).
    """
    keys = [a.name for a in rtype.attributes.values() if a.required and not _present(meta.get(a.name))]
    keys += [p for p, rel in rtype.relations.items() if rel.required and not _present(meta.get(p))]
    return keys


def _apply(path: Path, additions: dict[str, Any]) -> None:
    """Round-trip-write the absent fields, preserving key order and comments (ruamel).

    Only ever called with fields confirmed absent, so an authored value is never
    overwritten (PRJ-003).
    """
    cmap, body = _read_doc(path)
    for field, value in additions.items():
        cmap[field] = value
    _write_doc(path, cmap, body)
