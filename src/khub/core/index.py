"""Graph index — the shared entity scan behind status, query, and the write verbs.

A snapshot of the entity tree keyed by ``(type, slug)``: the nodes that exist, the
types each bare slug maps to (for id resolution), and each node's frontmatter. The
projection (``khub status``) and the authoring verbs (FS-002) both derive from this
one scan instead of re-walking the tree per call.

# ponytail: full in-memory scan, fine for a v1 firm corpus. When FS-003/FS-004 need
# incremental queries, lift to a persistent index (networkx is already a dependency).
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any

import frontmatter

from khub.core.model import ResolvedRelation, ResolvedSchema, ResolvedType


@dataclass(frozen=True)
class Index:
    """A scanned view of the entity tree: nodes, slug→types, and per-node metadata."""

    resolved: ResolvedSchema
    nodes: set[tuple[str, str]]
    types_by_slug: dict[str, set[str]]
    meta: dict[tuple[str, str], dict[str, Any]]


def build_index(root: Path, resolved: ResolvedSchema) -> Index:
    """Scan every declared type into an :class:`Index` keyed by ``(type, slug)``."""
    nodes: set[tuple[str, str]] = set()
    types_by_slug: dict[str, set[str]] = {}
    meta: dict[tuple[str, str], dict[str, Any]] = {}
    for tname, rtype in resolved.types.items():
        for slug, m in scan_type(root, rtype):
            node = (tname, slug)
            nodes.add(node)
            types_by_slug.setdefault(slug, set()).add(tname)
            meta[node] = m
    return Index(resolved=resolved, nodes=nodes, types_by_slug=types_by_slug, meta=meta)


def scan_type(root: Path, rtype: ResolvedType) -> list[tuple[str, dict[str, Any]]]:
    """The ``(slug, frontmatter)`` pairs stored for one type, by layout."""
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


def resolve_target(
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
