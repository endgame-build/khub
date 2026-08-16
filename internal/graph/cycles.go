package graph

// Cycle enumeration over one predicate — the nx.simple_cycles(_predicate_digraph(g, p))
// call integrity.py:734 makes. This is the ONLY place gonum is used: enumeration
// is the job here, ordering is not (the walks own their own adjacency because
// their output order is a byte contract).

import (
	"sort"

	"gonum.org/v1/gonum/graph/multi"
	"gonum.org/v1/gonum/graph/topo"

	"github.com/endgame-build/khub/internal/index"
)

// Cycles returns every elementary cycle over the single-predicate subgraph, as
// node lists — nx.simple_cycles over _predicate_digraph.
//
// Rotation and enumeration order are implementation-defined on both sides
// (go-port-plan R14), so canonicalize before rendering; membership and count are
// exact. gonum appends the entry node again at the end of each cycle where
// networkx does not, so that trailing repeat is stripped here.
//
// build_graph skips self-edges, so the subgraph never holds a self-loop —
// which is also the one shape gonum's DirectedCyclesIn drops (it prunes SCCs
// below two vertices). Single-node cycles are integrity's _self_cycles job,
// derived from frontmatter, not from this graph.
// rotateToMin starts a cycle at its lexicographically smallest node, keeping
// the traversal order intact.
func rotateToMin(cycle []index.Node) []index.Node {
	if len(cycle) < 2 {
		return cycle
	}
	min := 0
	for i, n := range cycle {
		if n.Less(cycle[min]) {
			min = i
		}
	}
	return append(append([]index.Node{}, cycle[min:]...), cycle[:min]...)
}

func cycleLess(a, b []index.Node) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i].Less(b[i])
		}
	}
	return len(a) < len(b)
}

func Cycles(g *Graph, predicate string) [][]index.Node {
	edges := g.PredicateEdges(predicate)
	if len(edges) == 0 {
		return nil
	}
	dg := multi.NewDirectedGraph()
	ids := map[index.Node]int64{}
	nodes := map[int64]index.Node{}
	nodeFor := func(n index.Node) int64 {
		if id, ok := ids[n]; ok {
			return id
		}
		gn := dg.NewNode()
		dg.AddNode(gn)
		ids[n] = gn.ID()
		nodes[gn.ID()] = n
		return gn.ID()
	}
	for _, e := range edges {
		u, v := nodeFor(e.From), nodeFor(e.To)
		dg.SetLine(dg.NewLine(dg.Node(u), dg.Node(v)))
	}
	var out [][]index.Node
	for _, cycle := range topo.DirectedCyclesIn(dg) {
		if len(cycle) < 2 {
			continue
		}
		// gonum closes the walk by repeating the entry node; networkx does not.
		trimmed := cycle[:len(cycle)-1]
		nodesOut := make([]index.Node, 0, len(trimmed))
		for _, gn := range trimmed {
			nodesOut = append(nodesOut, nodes[gn.ID()])
		}
		out = append(out, nodesOut)
	}
	// gonum's DirectedCyclesIn iterates a Go map, so both the order of the
	// cycle list AND each cycle's starting point vary run to run. `check` is a
	// gate whose output is compared byte-for-byte, so it must be reproducible:
	// rotate each cycle to start at its smallest node, then sort the list.
	// Membership and count are unaffected — only the presentation is pinned.
	for i, cycle := range out {
		out[i] = rotateToMin(cycle)
	}
	sort.Slice(out, func(i, j int) bool { return cycleLess(out[i], out[j]) })
	return out
}
