"""Graph projection — WPK-001-2.

A minimal, derived-at-runtime view of the entity tree behind ``khub status``:
per-type counts, the draft/active split, orphan (zero edges in or out), stale
(older than ``stale_days``), and the OKF-conformance flag. Counts are never
stored (WS-006); orphan and stale are core projection properties meant to be
computed identically for status, query, and check (WS-008). The entity scan and
edge resolution live in ``core.index``, shared with the authoring verbs (FS-002).
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date, datetime
from pathlib import Path
from typing import Any

from khub.core.index import build_index, filter_index, resolve_target, stray_nodes
from khub.core.introspect import load_schema
from khub.core.resolve import load_yaml
from khub.core.values import as_bool


@dataclass(frozen=True)
class Projection:
    """A derived summary of the workspace's entity tree."""

    counts: dict[str, int]
    total: int
    draft: int
    active: int
    orphan: int
    stale: int
    okf_conformant: bool
    stray: int = 0
    malformed: int = 0


def project(root: Path, *, stale_days: int, now: date) -> Projection:
    """Scan the entity tree and derive counts and health flags.

    Entity identity is ``(type, slug)``: two entities of different types may
    share a slug without colliding. An edge connects two *distinct* nodes, so a
    self-reference does not rescue an otherwise-isolated entity from orphanhood.
    OKF conformance requires typed/union relation targets to resolve to a node of
    a declared target type; universal (``to: any``) edges may point anywhere.

    Strays (a file whose internal ``type`` mismatches its layout) and malformed
    files are excluded from the entity counts — matching the integrity verbs — and
    reported as their own ``stray``/``malformed`` totals instead.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    strays = stray_nodes(index)
    valid = filter_index(index, strays)  # strays are not entities of their layout

    counts: dict[str, int] = {tname: 0 for tname in resolved.types}
    for tname, _slug in valid.nodes:
        counts[tname] += 1

    draft = active = stale = 0
    broken_ref = False
    has_out: set[tuple[str, str]] = set()
    has_in: set[tuple[str, str]] = set()
    for node in valid.nodes:
        tname, _slug = node
        meta = valid.meta[node]
        if as_bool(meta.get("draft", False)):
            draft += 1
        else:
            active += 1
        if is_stale(meta, now=now, stale_days=stale_days):
            stale += 1
        for predicate, rel in resolved.types[tname].relations.items():
            value = meta.get(predicate)
            if not value:
                continue
            for target in value if isinstance(value, list) else [value]:
                resolved_to = resolve_target(rel, str(target), valid.nodes, valid.types_by_slug)
                others = resolved_to - {node}
                if others:
                    has_out.add(node)
                    has_in |= others
                if rel.kind != "any" and not resolved_to:
                    broken_ref = True

    orphan = sum(1 for n in valid.nodes if n not in has_out and n not in has_in)
    return Projection(
        counts=counts,
        total=sum(counts.values()),
        draft=draft,
        active=active,
        orphan=orphan,
        stale=stale,
        # OKF conformance means the tree would export as a valid bundle: every file
        # in a layout is a typed entity (no strays, no malformed) and every
        # typed/union edge resolves.
        okf_conformant=not broken_ref and not strays and not index.malformed,
        stray=len(strays),
        malformed=len(index.malformed),
    )


def stale_days(root: Path) -> int:
    """The workspace's ``stale_days`` threshold, read from ``.khub/config.yaml``.

    Shared by ``status`` and the FS-003 reads so the stale flag means the same
    everywhere. Tolerates a null ``defaults:`` block, a null/blank ``stale_days:``
    value, and a quoted number; falls back to the default for any absent/empty value.
    """
    from khub.core.workspace import DEFAULT_STALE_DAYS

    cfg = load_yaml(root / ".khub" / "config.yaml")
    defaults = cfg.get("defaults") or {}
    raw = defaults.get("stale_days")
    if raw is None or raw == "":
        return DEFAULT_STALE_DAYS
    return int(raw)


def effective_date(meta: dict[str, Any], *, git_date: date | None = None) -> tuple[date | None, str]:
    """The date staleness is judged against, with its provenance.

    Precedence: the ``updated`` field, then a git last-commit date (when supplied),
    then ``created``. The single staleness definition shared by ``status``/``query``
    (which pass no ``git_date``) and ``khub stale`` (which backfills git when
    ``updated`` is absent) — INT-SHARED-004. A present-but-unparseable value yields
    ``(None, <source>)`` so the caller can still see where it came from.
    """
    raw = meta.get("updated")
    if raw is not None:
        return _as_date(raw), "updated"
    if git_date is not None:
        return git_date, "git log"
    raw = meta.get("created")
    if raw is not None:
        return _as_date(raw), "created"
    return None, "none"


def is_stale(
    meta: dict[str, Any], *, now: date, stale_days: int, git_date: date | None = None
) -> bool:
    date_, source = effective_date(meta, git_date=git_date)
    if source == "none":
        return False  # no timestamp to judge against
    # A present-but-unparseable date is surfaced as stale, not silently dropped.
    return date_ is None or (now - date_).days > stale_days


def _as_date(value: Any) -> date | None:
    if value is None:
        return None
    if isinstance(value, datetime):
        return value.date()
    if isinstance(value, date):
        return value
    try:
        return date.fromisoformat(str(value)[:10])
    except ValueError:
        return None
