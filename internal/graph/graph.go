// Package graph ports src/khub/core/graph.py — the derived MultiDiGraph
// projection over the entity index plus the three read-only walks (neighbors,
// impact, history).
//
// The graph is rebuilt per call from the live tree and never written back
// (QRY-SHARED-001). Walk output order is a byte-visible contract, so this file
// keeps its OWN insertion-ordered adjacency rather than leaning on any library's
// iteration order: it reproduces networkx MultiDiGraph exactly — parallel edges
// between one pair survive under different predicates, and adjacency yields in
// edge-insertion order. gonum appears only in cycles.go, where enumeration (not
// ordering) is the job.
package graph

import (
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// Edge is one stored forward edge: (u, v, predicate). The predicate is the
// relation field name that carried the target.
type Edge struct {
	From, To  index.Node
	Predicate string
}

// adjacency is one node's neighbour list: networkx's _succ[n] / _pred[n]. order
// holds neighbours by first-edge-added; keys holds each neighbour's parallel
// predicates in key order (networkx shares one keydict between _succ[u][v] and
// _pred[v][u], so the two sequences are identical by construction).
type adjacency struct {
	order []index.Node
	keys  map[index.Node][]string
}

func newAdjacency() *adjacency {
	return &adjacency{keys: map[index.Node][]string{}}
}

// Graph is the nx.MultiDiGraph projection: nodes in scan order, edges in
// insertion order, both directions materialized.
type Graph struct {
	nodes   []index.Node
	nodeSet map[index.Node]bool
	succ    map[index.Node]*adjacency
	pred    map[index.Node]*adjacency
}

func newGraph() *Graph {
	return &Graph{
		nodeSet: map[index.Node]bool{},
		succ:    map[index.Node]*adjacency{},
		pred:    map[index.Node]*adjacency{},
	}
}

func (g *Graph) addNode(n index.Node) {
	if g.nodeSet[n] {
		return
	}
	g.nodeSet[n] = true
	g.nodes = append(g.nodes, n)
	g.succ[n] = newAdjacency()
	g.pred[n] = newAdjacency()
}

func (g *Graph) addEdge(u, v index.Node, predicate string) {
	g.addNode(u)
	g.addNode(v)
	s := g.succ[u]
	if _, ok := s.keys[v]; !ok {
		s.order = append(s.order, v)
	}
	s.keys[v] = append(s.keys[v], predicate)
	p := g.pred[v]
	if _, ok := p.keys[u]; !ok {
		p.order = append(p.order, u)
	}
	p.keys[u] = append(p.keys[u], predicate)
}

// BuildGraph projects the index into a directed multigraph of resolved forward
// edges — build_graph.
//
// A MultiDiGraph keeps parallel edges between the same pair under different
// predicates, which a plain DiGraph would collapse. Self-edges are skipped: a
// self-reference connects nothing new (mirrors the orphan rule in core.project).
func BuildGraph(idx *index.Index) *Graph {
	g := newGraph()
	for _, node := range idx.Order {
		g.addNode(node)
	}
	for _, node := range idx.Order {
		meta := idx.Meta[node]
		rtype, ok := idx.Resolved.Types.Get(node.Type)
		if !ok {
			continue
		}
		for _, predicate := range rtype.Relations.Keys() {
			rel, _ := rtype.Relations.Get(predicate)
			raw, _ := meta.Get(predicate)
			if !values.Truthy(raw) {
				continue
			}
			for _, target := range targetList(raw) {
				for _, tnode := range resolveTargets(rel, target, idx) {
					if tnode != node {
						g.addEdge(node, tnode, predicate)
					}
				}
			}
		}
	}
	return g
}

// resolveTargets wraps index.ResolveTarget and imposes a deterministic order on
// the multi-node case. Python iterates a set here, so CPython hash order decides
// edge-insertion order when one value resolves to several nodes; that is not
// reproducible, so this sorts by the (type, slug) tuple compare instead. Single-
// node resolution — the common case — is untouched.
func resolveTargets(rel *schema.ResolvedRelation, target string, idx *index.Index) []index.Node {
	set := idx.ResolveTarget(rel, target)
	if len(set) == 0 {
		return nil
	}
	out := make([]index.Node, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	if len(out) > 1 {
		index.SortNodes(out)
	}
	return out
}

// Nodes returns every node in scan order — list(G.nodes).
func (g *Graph) Nodes() []index.Node { return g.nodes }

// Has reports whether the node is in the graph.
func (g *Graph) Has(n index.Node) bool { return g.nodeSet[n] }

// Edges returns every edge in networkx's G.edges() nesting: node-insertion order
// outer, that node's successor-insertion order next, key order innermost.
func (g *Graph) Edges() []Edge {
	var out []Edge
	for _, n := range g.nodes {
		out = append(out, g.OutEdges(n)...)
	}
	return out
}

// OutEdges is G.out_edges(n, keys=False, data="predicate").
func (g *Graph) OutEdges(n index.Node) []Edge {
	a := g.succ[n]
	if a == nil {
		return nil
	}
	var out []Edge
	for _, v := range a.order {
		for _, p := range a.keys[v] {
			out = append(out, Edge{From: n, To: v, Predicate: p})
		}
	}
	return out
}

// InEdges is G.in_edges(n, keys=False, data="predicate").
func (g *Graph) InEdges(n index.Node) []Edge {
	a := g.pred[n]
	if a == nil {
		return nil
	}
	var out []Edge
	for _, u := range a.order {
		for _, p := range a.keys[u] {
			out = append(out, Edge{From: u, To: n, Predicate: p})
		}
	}
	return out
}

// OutDegree is G.out_degree(n): parallel edges count separately.
func (g *Graph) OutDegree(n index.Node) int { return degree(g.succ[n]) }

// InDegree is G.in_degree(n).
func (g *Graph) InDegree(n index.Node) int { return degree(g.pred[n]) }

func degree(a *adjacency) int {
	if a == nil {
		return 0
	}
	total := 0
	for _, ps := range a.keys {
		total += len(ps)
	}
	return total
}

// PredicateEdges is the edge set of _predicate_digraph: only `predicate` edges,
// collapsed to a simple DiGraph (one entry per (u, v)), first-occurrence order.
func (g *Graph) PredicateEdges(predicate string) []Edge {
	var out []Edge
	seen := map[[2]index.Node]bool{}
	for _, e := range g.Edges() {
		if e.Predicate != predicate {
			continue
		}
		key := [2]index.Node{e.From, e.To}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

// targetList is Python's `value if isinstance(value, list) else [value]`,
// followed by the str(target) the resolve call applies.
func targetList(v any) []string {
	if list, ok := v.([]any); ok {
		out := make([]string, 0, len(list))
		for _, item := range list {
			out = append(out, values.Str(item))
		}
		return out
	}
	return []string{values.Str(v)}
}
