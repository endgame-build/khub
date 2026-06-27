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

from khub.core.index import resolve_target, scan_type
from khub.core.introspect import load_schema
from khub.core.model import ResolvedType
from khub.core.resolve import load_yaml


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


def project(root: Path, *, stale_days: int, now: date) -> Projection:
    """Scan the entity tree and derive counts and health flags.

    Entity identity is ``(type, slug)``: two entities of different types may
    share a slug without colliding. An edge connects two *distinct* nodes, so a
    self-reference does not rescue an otherwise-isolated entity from orphanhood.
    OKF conformance requires typed/union relation targets to resolve to a node of
    a declared target type; universal (``to: any``) edges may point anywhere.
    """
    resolved = load_schema(root)
    counts: dict[str, int] = {}
    draft = active = stale = 0
    all_have_type = True
    broken_ref = False

    # Pass 1: scan every entity, indexing nodes by (type, slug) and by slug.
    scanned: list[tuple[str, str, dict[str, Any], ResolvedType]] = []
    nodes: set[tuple[str, str]] = set()
    types_by_slug: dict[str, set[str]] = {}
    for tname, rtype in resolved.types.items():
        entities = scan_type(root, rtype)
        counts[tname] = len(entities)
        for slug, meta in entities:
            nodes.add((tname, slug))
            types_by_slug.setdefault(slug, set()).add(tname)
            scanned.append((tname, slug, meta, rtype))

    # Pass 2: resolve edges, count drafts, derive staleness.
    has_out: set[tuple[str, str]] = set()
    has_in: set[tuple[str, str]] = set()
    for tname, slug, meta, rtype in scanned:
        node = (tname, slug)
        if not meta.get("type"):
            all_have_type = False
        if meta.get("draft"):
            draft += 1
        else:
            active += 1
        if is_stale(meta, now=now, stale_days=stale_days):
            stale += 1
        for predicate, rel in rtype.relations.items():
            value = meta.get(predicate)
            if not value:
                continue
            for target in value if isinstance(value, list) else [value]:
                resolved_to = resolve_target(rel, str(target), nodes, types_by_slug)
                others = resolved_to - {node}
                if others:
                    has_out.add(node)
                    has_in |= others
                if rel.kind != "any" and not resolved_to:
                    broken_ref = True

    orphan = sum(1 for n in nodes if n not in has_out and n not in has_in)
    return Projection(
        counts=counts,
        total=sum(counts.values()),
        draft=draft,
        active=active,
        orphan=orphan,
        stale=stale,
        okf_conformant=all_have_type and not broken_ref,
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


def is_stale(meta: dict[str, Any], *, now: date, stale_days: int) -> bool:
    raw = meta.get("updated")
    if raw is None:
        raw = meta.get("created")
    if raw is None:
        return False  # no timestamp to judge against
    ts = _as_date(raw)
    # A present-but-unparseable date is surfaced as stale, not silently dropped.
    return ts is None or (now - ts).days > stale_days


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
