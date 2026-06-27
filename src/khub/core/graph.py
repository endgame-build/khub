"""networkx graph projection and the read-only walks — FS-003 (WPK-003-2/3).

Builds a ``MultiDiGraph`` over the entity :class:`Index` — nodes keyed by
``(type, slug)``, each stored forward edge carrying its predicate — and walks it
three ways:

- ``neighbors``: one-hop (or bounded ``--depth``) adjacency. Outbound edges are
  the entity's own stored relations; inbound edges are computed from the stored
  forward edge on the *other* node and surfaced as derived inverses (never read
  from disk).
- ``impact``: the transitive forward closure over one predicate (default
  ``depends_on``) — descendants, or ancestors under ``reverse``. BFS over the
  predicate subgraph, so it is cycle-safe and visits each node once.
- ``history``: a self-referential supersession chain (default ``supersedes``)
  followed back in order, carrying the derived ``superseded_by`` direction.

Every walk is read-only over a graph rebuilt from the live Markdown on demand;
nothing is written back (QRY-SHARED-001).
"""

from __future__ import annotations

from collections.abc import Iterator
from dataclasses import dataclass
from typing import cast

import networkx as nx

from khub.core.entity import resolve_id
from khub.core.index import Index, resolve_target


@dataclass(frozen=True)
class Neighbor:
    """One adjacent node, labelled by the predicate and direction that reached it."""

    type: str
    slug: str
    predicate: str
    direction: str  # "out" (stored) | "in" (derived inverse)
    derived: bool
    depth: int


@dataclass(frozen=True)
class ImpactNode:
    """A node in the transitive closure, with its depth from the source."""

    type: str
    slug: str
    depth: int


@dataclass(frozen=True)
class HistoryLink:
    """One record in a supersession chain; ``superseded_by`` is the derived inverse."""

    type: str
    slug: str
    superseded_by: str | None  # qualified type/slug of the record superseding this one


def build_graph(index: Index) -> nx.MultiDiGraph:
    """Project the index into a directed multigraph of resolved forward edges.

    A ``MultiDiGraph`` keeps parallel edges between the same pair under different
    predicates (e.g. ``related`` and ``references`` to one target), which a plain
    ``DiGraph`` would collapse. Self-edges are skipped — a self-reference connects
    nothing new (mirrors the orphan rule in ``core.project``).
    """
    g: nx.MultiDiGraph = nx.MultiDiGraph()
    for node, meta in index.meta.items():
        g.add_node(node, meta=meta)
    for node, meta in index.meta.items():
        type_, _ = node
        for predicate, rel in index.resolved.types[type_].relations.items():
            value = meta.get(predicate)
            if not value:
                continue
            for target in value if isinstance(value, list) else [value]:
                for tnode in resolve_target(rel, str(target), index.nodes, index.types_by_slug):
                    if tnode != node:
                        g.add_edge(node, tnode, predicate=predicate)
    return g


def neighbors(
    index: Index,
    id_: str,
    *,
    predicate: str | None = None,
    direction: str = "both",
    depth: int = 1,
) -> list[Neighbor]:
    """Adjacent nodes within ``depth`` hops of ``id_`` in the chosen direction(s).

    Inbound neighbours (``direction`` in/both) are derived inverses: computed from
    the forward edge stored on the other node, never persisted on the source.

    A pair joined by two predicates (or by both an inbound and an outbound edge) is
    two distinct adjacencies, so dedup is keyed on the full ``(node, predicate,
    direction)`` triple — a plain node-level set would collapse parallel edges the
    MultiDiGraph deliberately keeps. A separate node-level ``visited`` set bounds
    multi-hop frontier expansion, which also makes the walk cycle-safe.
    """
    g = build_graph(index)
    src = resolve_id(index, id_)
    visited = {src}  # bounds frontier expansion (and the source is never its own neighbour)
    emitted: set[tuple[tuple[str, str], str, str]] = set()
    frontier = [src]
    found: list[Neighbor] = []
    for d in range(1, depth + 1):
        nxt: list[tuple[str, str]] = []
        for node in frontier:
            for nbr, pred, dir_ in _adjacent(g, node, direction, predicate):
                if nbr == src or (nbr, pred, dir_) in emitted:
                    continue
                emitted.add((nbr, pred, dir_))
                found.append(
                    Neighbor(
                        type=nbr[0],
                        slug=nbr[1],
                        predicate=pred,
                        direction=dir_,
                        derived=dir_ == "in",
                        depth=d,
                    )
                )
                if nbr not in visited:
                    visited.add(nbr)
                    nxt.append(nbr)
        frontier = nxt
    return found


def _adjacent(
    g: nx.MultiDiGraph, node: tuple[str, str], direction: str, predicate: str | None
) -> Iterator[tuple[tuple[str, str], str, str]]:
    """Yield ``(neighbour, predicate, direction)`` for one node, filtered."""
    if direction in ("out", "both"):
        for _, v, pred in g.out_edges(node, keys=False, data="predicate"):
            if predicate is None or pred == predicate:
                yield v, pred, "out"
    if direction in ("in", "both"):
        for u, _, pred in g.in_edges(node, keys=False, data="predicate"):
            if predicate is None or pred == predicate:
                yield u, pred, "in"


def impact(
    index: Index, id_: str, *, predicate: str = "depends_on", reverse: bool = False
) -> list[ImpactNode]:
    """The transitive closure over ``predicate`` from ``id_``, depth-marked.

    Descendants by default; ancestors under ``reverse``. The walk is a BFS over
    the single-predicate subgraph (``networkx.single_source_shortest_path_length``)
    — cycle-safe, each node visited once. The source is included at depth 0, so a
    leaf returns ``[source]`` (the caller reports "no downstream impact").
    """
    g = build_graph(index)
    src = resolve_id(index, id_)
    sub = _predicate_digraph(g, predicate)
    walk: nx.DiGraph = sub.reverse(copy=False) if reverse else sub
    depths = nx.single_source_shortest_path_length(walk, src)
    ordered = sorted(depths.items(), key=lambda kv: (kv[1], kv[0]))
    return [ImpactNode(type=n[0], slug=n[1], depth=d) for n, d in ordered]


def _predicate_digraph(g: nx.MultiDiGraph, predicate: str) -> nx.DiGraph:
    """A simple DiGraph of just the ``predicate`` edges (all nodes retained)."""
    sub: nx.DiGraph = nx.DiGraph()
    sub.add_nodes_from(g.nodes())
    for u, v, pred in g.edges(keys=False, data="predicate"):
        if pred == predicate:
            sub.add_edge(u, v)
    return sub


def history(
    index: Index, id_: str, *, predicate: str = "supersedes", limit: int | None = None
) -> list[HistoryLink]:
    """Follow the self-referential ``predicate`` chain from ``id_`` back in order.

    Returns the source followed by each prior record, newest first. Each prior
    record carries its derived ``superseded_by`` (the newer record that supersedes
    it) — computed from chain order, never stored. A visited set guards a cyclic
    chain; ``limit`` caps to the N most recent links.
    """
    g = build_graph(index)
    src = resolve_id(index, id_)
    chain: list[tuple[str, str]] = [src]
    seen = {src}
    cur = src
    while True:
        nextn = _first_successor(g, cur, predicate)
        if nextn is None or nextn in seen:
            break
        seen.add(nextn)
        chain.append(nextn)
        cur = nextn
    if limit is not None:
        chain = chain[:limit]
    links: list[HistoryLink] = []
    for i, node in enumerate(chain):
        prev = chain[i - 1] if i > 0 else None
        superseded_by = f"{prev[0]}/{prev[1]}" if prev else None
        links.append(HistoryLink(type=node[0], slug=node[1], superseded_by=superseded_by))
    return links


def _first_successor(
    g: nx.MultiDiGraph, node: tuple[str, str], predicate: str
) -> tuple[str, str] | None:
    """The first node reached from ``node`` via a ``predicate`` edge, else None.

    # ponytail: history assumes a single-valued chain predicate (``supersedes`` is
    # `to: decision`, single), so following the first edge is the whole chain. A
    # many-valued predicate would branch — lift to a tree/DAG walk if one appears.
    """
    for _, v, pred in g.out_edges(node, keys=False, data="predicate"):
        if pred == predicate:
            return cast("tuple[str, str]", v)
    return None
