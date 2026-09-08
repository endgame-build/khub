package graph

// Replaces the nx.simple_cycles call from integrity.py:734 with bounded SCC
// witnesses. Walks retain their ordered adjacency.

import (
	"sort"

	"gonum.org/v1/gonum/graph/multi"
	"gonum.org/v1/gonum/graph/topo"

	"github.com/endgame-build/khub/internal/index"
)

// rotateToMin preserves traversal direction while pinning the starting node.
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

// Cycles returns one deterministic directed cycle per cyclic component.
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
	// Sorting successors makes the witness independent of gonum's map order.
	succ := map[index.Node][]index.Node{}
	for _, e := range edges {
		succ[e.From] = append(succ[e.From], e.To)
	}
	for _, next := range succ {
		index.SortNodes(next)
	}
	var out [][]index.Node
	for _, component := range topo.TarjanSCC(dg) {
		members := map[index.Node]bool{}
		var start index.Node
		for i, gn := range component {
			n := nodes[gn.ID()]
			members[n] = true
			if i == 0 || n.Less(start) {
				start = n
			}
		}
		// A deterministic walk inside an SCC must eventually revisit a node.
		// Its repeated suffix is a real directed cycle, not the SCC's node list.
		positions := map[index.Node]int{}
		var path []index.Node
		for cur := start; ; {
			if at, seen := positions[cur]; seen {
				out = append(out, rotateToMin(path[at:]))
				break
			}
			positions[cur] = len(path)
			path = append(path, cur)
			found := false
			for _, next := range succ[cur] {
				if members[next] {
					cur, found = next, true
					break
				}
			}
			if !found { // singleton without a self-loop
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return cycleLess(out[i], out[j]) })
	return out
}
