"""``khub reindex`` — regenerate the OKF ``index.md`` navigation (WPK-005-1, FS-005).

Walks the typed graph (rebuilt from the live Markdown on demand, never the prior
``index.md`` — PRJ-001/REQ-PRJ001-03) and renders an OKF ``index.md``: entities
grouped by type, cross-links along resolved edges, stamped with the OKF version it
conforms to. ``--dry-run`` diffs the new index against the current file and writes
nothing. The graph iteration and group-by-type step are shared with ``viz``
(``core.viz``); only the render target (Markdown vs HTML) differs.
"""

from __future__ import annotations

import difflib
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import networkx as nx

from khub.core.entity import entity_path
from khub.core.graph import build_graph
from khub.core.index import Index, build_index, filter_index, stray_nodes
from khub.core.introspect import load_schema
from khub.core.model import ResolvedSchema

# OKF (Open Knowledge Format) version the generated index conforms to. OKF v0.1 is
# "a git tree of .md concepts with a required type, cross-links, index.md, log.md"
# (design-memo); reindex stamps it so a consumer knows the contract the file meets.
OKF_VERSION = "0.1"

INDEX_NAME = "index.md"


@dataclass(frozen=True)
class ReindexResult:
    """The outcome of a reindex: the derived index, how many entities, and whether written."""

    count: int
    path: Path
    content: str
    diff: str  # unified diff against the current index.md; only populated under --dry-run
    wrote: bool


def reindex(root: Path, *, dry_run: bool = False) -> ReindexResult:
    """Regenerate ``index.md`` from the live graph; ``--dry-run`` diffs and writes nothing."""
    content, count = build_index_doc(root)
    path = root / INDEX_NAME
    if dry_run:
        current = path.read_text() if path.exists() else ""
        diff = "".join(
            difflib.unified_diff(
                current.splitlines(keepends=True),
                content.splitlines(keepends=True),
                fromfile=INDEX_NAME,
                tofile=INDEX_NAME,
            )
        )
        return ReindexResult(count=count, path=path, content=content, diff=diff, wrote=False)
    path.write_text(content)
    return ReindexResult(count=count, path=path, content=content, diff="", wrote=True)


def build_index_doc(root: Path) -> tuple[str, int]:
    """The rendered OKF ``index.md`` and its entity count, derived from the live graph.

    Builds a fresh index from the Markdown tree — the prior ``index.md`` is never read
    as input (derived-not-stored, PRJ-001).
    """
    resolved = load_schema(root)
    scanned = build_index(root, resolved)
    index = filter_index(scanned, stray_nodes(scanned))  # strays are not entities
    graph = build_graph(index)
    return render_index(root, resolved, index, graph), len(index.nodes)


def render_index(
    root: Path, resolved: ResolvedSchema, index: Index, graph: nx.MultiDiGraph
) -> str:
    """Render the OKF index: a version-stamped body grouping entities by type with cross-links."""
    front = f"---\nokf_version: '{OKF_VERSION}'\nentity_count: {len(index.nodes)}\n---\n"
    lines = ["# Index", ""]
    groups = group_by_type(index.nodes, resolved)
    if not groups:
        lines += ["_No entities._", ""]  # a valid, stamped, zero-row index (AC-003 / U06)
    for type_, slugs in groups.items():
        lines.append(f"## {type_}")
        lines.append("")
        for slug in slugs:
            node = (type_, slug)
            link = _entity_link(root, resolved, type_, slug)
            label = _label(index.meta[node], slug)
            xlinks = cross_links(root, resolved, index, graph, node)
            suffix = f" — {', '.join(xlinks)}" if xlinks else ""
            lines.append(f"- [{label}]({link}){suffix}")
        lines.append("")
    return front + "\n".join(lines) + "\n"


def group_by_type(
    nodes: set[tuple[str, str]], resolved: ResolvedSchema
) -> dict[str, list[str]]:
    """Group scanned nodes into one section per type, in schema-declared (stable) order.

    Only types with at least one entity get a section; slugs sort within a type.
    """
    groups: dict[str, list[str]] = {}
    for type_ in resolved.types:
        slugs = sorted(s for (t, s) in nodes if t == type_)
        if slugs:
            groups[type_] = slugs
    return groups


def cross_links(
    root: Path,
    resolved: ResolvedSchema,
    index: Index,
    graph: nx.MultiDiGraph,
    node: tuple[str, str],
) -> list[str]:
    """One ``predicate → [label](link)`` per resolved outbound edge, sorted for determinism.

    The label is the target's ``title`` when present, else its slug (the fallback the
    OKF core ``title`` field is optional everywhere but ``case-study``).
    """
    out: list[str] = []
    for _, tnode, pred in graph.out_edges(node, keys=False, data="predicate"):
        label = _label(index.meta[tnode], tnode[1])
        link = _entity_link(root, resolved, tnode[0], tnode[1])
        out.append(f"{pred} → [{label}]({link})")
    return sorted(out)


def _entity_link(root: Path, resolved: ResolvedSchema, type_: str, slug: str) -> str:
    """The entity's path relative to the workspace root (where ``index.md`` lives).

    A path carrying a space or parenthesis breaks a plain ``(path)`` Markdown link, so
    it is wrapped in angle brackets ``<...>`` (the CommonMark escape); a clean path is
    left as-is so the common case stays unadorned.
    """
    link = str(entity_path(root, resolved.types[type_], slug).relative_to(root))
    if any(ch in link for ch in " ()"):
        return f"<{link}>"
    return link


def _label(meta: dict[str, Any], slug: str) -> str:
    """A human label for a link: the ``title`` if set, else the slug — Markdown-escaped.

    The title is user-authored, so ``]`` or a ``](url)`` sequence would otherwise break
    the row or inject an arbitrary link target; escape the link-text metacharacters.
    """
    title = meta.get("title")
    return _md_escape(str(title)) if title else slug


def _md_escape(text: str) -> str:
    """Backslash-escape the characters that are structural inside Markdown link text."""
    return text.replace("\\", "\\\\").replace("[", "\\[").replace("]", "\\]")
