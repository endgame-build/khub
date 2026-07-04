"""``khub viz`` — a self-contained Cytoscape visualization of the typed graph (WPK-005-1).

Serializes the ``networkx`` projection to Cytoscape elements (nodes tagged with their
type for by-type coloring, edges labeled with their predicate — PRJ-006) and inlines
the whole Cytoscape library plus the elements into one HTML file that opens with no
network (PRJ-005 / REQ-PRJ003-02). ``--type <t>`` renders only that type and the
edges leaving it, pulling in just those edges' target endpoints (REQ-PRJ003-03).
Shares the graph walk with ``reindex``; only the render target differs.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import networkx as nx

from khub.core.errors import LocatedError
from khub.core.graph import build_graph
from khub.core.index import build_index, filter_index, stray_nodes
from khub.core.introspect import load_schema
from khub.core.locate import provenance

Node = tuple[str, str]
Edge = tuple[Node, Node, str]
Element = dict[str, Any]

_ASSETS = Path(__file__).resolve().parent.parent / "assets"
_CYTOSCAPE_JS = _ASSETS / "cytoscape.min.js"

# A fixed palette cycled over the types present, so node color is stable per run and
# distinct across types (PRJ-006). Colorblind-friendly qualitative set.
_PALETTE = [
    "#4e79a7", "#f28e2b", "#e15759", "#76b7b2", "#59a14f",
    "#edc948", "#b07aa1", "#ff9da7", "#9c755f", "#bab0ac",
]

DEFAULT_OUT = "viz.html"


@dataclass(frozen=True)
class VizResult:
    """The outcome of a viz render: where it landed and how much it drew."""

    path: Path
    nodes: int
    edges: int


def viz(root: Path, *, out: str = DEFAULT_OUT, type_filter: str | None = None) -> VizResult:
    """Render the typed graph to a self-contained Cytoscape HTML at ``out``."""
    resolved = load_schema(root)
    if type_filter is not None and type_filter not in resolved.types:
        # A misspelled --type must fail loudly, not render an empty graph as "success".
        raise LocatedError.unknown_type(
            type_filter, provenance(root).get("preset", ""), sorted(resolved.types)
        )
    scanned = build_index(root, resolved)
    index = filter_index(scanned, stray_nodes(scanned))  # strays are not entities
    graph = build_graph(index)

    nodes, edges = _select(graph, type_filter)
    elements = to_cytoscape(nodes, edges)
    html = render_html(elements, types=sorted({n[0] for n in nodes}))

    out_path = Path(out)
    path = out_path if out_path.is_absolute() else root / out_path
    path.parent.mkdir(parents=True, exist_ok=True)  # honor --out under a new subdirectory
    path.write_text(html)
    return VizResult(path=path, nodes=len(elements["nodes"]), edges=len(elements["edges"]))


def _select(graph: nx.MultiDiGraph, type_filter: str | None) -> tuple[list[Node], list[Edge]]:
    """The nodes and edges to render: the whole graph, or one type and its outgoing edges.

    Under ``--type t`` the kept edges are those *leaving* a ``t`` node; the kept nodes are
    the ``t`` nodes plus the endpoints those edges reach. A node incident only via an
    inbound edge (e.g. a meeting pointing at a project) is not pulled in — the filter is
    on the outbound frontier of the type (TS-PRJ-003-03).
    """
    all_edges: list[Edge] = list(graph.edges(keys=False, data="predicate"))
    if type_filter is None:
        return list(graph.nodes), all_edges
    typed = {n for n in graph.nodes if n[0] == type_filter}
    edges = [(u, v, p) for (u, v, p) in all_edges if u in typed]
    nodes = typed | {v for (u, v, _) in edges}
    return sorted(nodes), edges


def to_cytoscape(nodes: list[Node], edges: list[Edge]) -> dict[str, list[Element]]:
    """Serialize nodes and edges to Cytoscape ``elements`` JSON-able dicts.

    Each node carries its ``type`` (for by-type coloring); each edge carries its
    ``predicate`` as ``label`` (PRJ-006).
    """
    node_els = [
        {"data": {"id": _id(n), "label": n[1], "type": n[0]}} for n in nodes
    ]
    edge_els = [
        {"data": {"source": _id(u), "target": _id(v), "label": p}} for (u, v, p) in edges
    ]
    return {"nodes": node_els, "edges": edge_els}


def render_html(elements: dict[str, list[Element]], *, types: list[str]) -> str:
    """Wrap the elements and the inlined Cytoscape library into one standalone HTML file."""
    lib = _CYTOSCAPE_JS.read_text()
    payload = json.dumps(elements["nodes"] + elements["edges"])
    style = json.dumps(_style(types))
    return _TEMPLATE.format(lib=lib, elements=payload, style=style)


def _style(types: list[str]) -> list[dict[str, Any]]:
    """Cytoscape style: label nodes/edges, arrow edges, and one color per type present."""
    style: list[dict[str, Any]] = [
        {"selector": "node", "style": {
            "label": "data(label)", "font-size": 8, "background-color": "#999",
            "text-valign": "center", "color": "#fff", "width": 18, "height": 18,
        }},
        {"selector": "edge", "style": {
            "label": "data(label)", "font-size": 6, "width": 1, "line-color": "#ccc",
            "curve-style": "bezier", "target-arrow-shape": "triangle",
            "target-arrow-color": "#ccc",
        }},
    ]
    for i, type_ in enumerate(types):
        style.append(
            {"selector": f'node[type="{type_}"]',
             "style": {"background-color": _PALETTE[i % len(_PALETTE)]}}
        )
    return style


def _id(node: tuple[str, str]) -> str:
    return f"{node[0]}/{node[1]}"


_TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>khub viz</title>
<style>html,body{{margin:0;height:100%}}#cy{{width:100%;height:100vh;display:block}}</style>
<script>{lib}</script>
</head>
<body>
<div id="cy"></div>
<script>
var elements = {elements};
var style = {style};
cytoscape({{
  container: document.getElementById('cy'),
  elements: elements,
  style: style,
  layout: {{ name: 'cose' }}
}});
</script>
</body>
</html>
"""
