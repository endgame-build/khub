"""Graph projection — WPK-001-2.

A minimal, derived-at-runtime view of the entity tree behind ``khub status``:
per-type counts, the draft/active split, orphan (zero edges in or out), stale
(older than ``stale_days``), and the OKF-conformance flag. Counts are never
stored (WS-006); orphan and stale are core projection properties meant to be
computed identically for status, query, and check (WS-008).

# ponytail: full in-memory scan, fine for a v1 firm corpus. When FS-003/FS-004
# need incremental queries, lift this to a persistent index (networkx is already
# a dependency for that fuller graph) — the Projection shape can stay.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import date, datetime
from pathlib import Path
from typing import Any

import frontmatter

from khub.core.introspect import load_schema
from khub.core.model import ResolvedRelation, ResolvedType


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
        entities = _scan_type(root, rtype)
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
        if _is_stale(meta, now=now, stale_days=stale_days):
            stale += 1
        for predicate, rel in rtype.relations.items():
            value = meta.get(predicate)
            if not value:
                continue
            for target in value if isinstance(value, list) else [value]:
                resolved_to = _resolve_target(rel, str(target), nodes, types_by_slug)
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


def _resolve_target(
    rel: ResolvedRelation,
    target: str,
    nodes: set[tuple[str, str]],
    types_by_slug: dict[str, set[str]],
) -> set[tuple[str, str]]:
    """The nodes a relation value resolves to: any-type for universal edges,
    a declared target type otherwise."""
    if rel.kind == "any":
        return {(t, target) for t in types_by_slug.get(target, ())}
    return {(t, target) for t in rel.targets if (t, target) in nodes}


def _is_stale(meta: dict[str, Any], *, now: date, stale_days: int) -> bool:
    raw = meta.get("updated")
    if raw is None:
        raw = meta.get("created")
    if raw is None:
        return False  # no timestamp to judge against
    ts = _as_date(raw)
    # A present-but-unparseable date is surfaced as stale, not silently dropped.
    return ts is None or (now - ts).days > stale_days


def _scan_type(root: Path, rtype: ResolvedType) -> list[tuple[str, dict[str, Any]]]:
    if not rtype.storage.path:
        return []
    base = root / rtype.storage.path
    if not base.exists():
        return []
    out: list[tuple[str, dict[str, Any]]] = []
    if rtype.storage.layout == "folder":
        for idx in sorted(base.glob("*/_index.md")):
            out.append((idx.parent.name, frontmatter.load(str(idx)).metadata))
    else:
        for f in sorted(base.glob("*.md")):
            if f.name == "_index.md":
                continue
            out.append((f.stem, frontmatter.load(str(f)).metadata))
    return out


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
