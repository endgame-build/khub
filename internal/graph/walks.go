package graph

// Ports the three read-only walks of src/khub/core/graph.py: neighbors, impact,
// history, plus their root-taking walk_* entries and _require_predicate.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/schema"
)

// Direction literals for Neighbors.
const (
	DirectionOut  = "out"
	DirectionIn   = "in"
	DirectionBoth = "both"
)

// Neighbor is one adjacent node, labelled by the predicate and direction that
// reached it.
type Neighbor struct {
	Type      string
	Slug      string
	Predicate string
	Direction string // "out" (stored) | "in" (derived inverse)
	Derived   bool
	Depth     int
}

// ImpactNode is a node in the transitive closure, with its depth from the source.
type ImpactNode struct {
	Type  string
	Slug  string
	Depth int
}

// HistoryLink is one record in a supersession chain. SupersededBy is the derived
// inverse — the qualified id of the record superseding this one, "" for the head
// (Python None).
type HistoryLink struct {
	Type         string
	Slug         string
	SupersededBy string
}

// Neighbors returns the adjacent nodes within depth hops of id_ in the chosen
// direction(s) — the neighbors() port.
//
// Inbound neighbours are derived inverses: computed from the forward edge stored
// on the other node, never persisted on the source.
//
// A pair joined by two predicates (or by both an inbound and an outbound edge) is
// two distinct adjacencies, so dedup is keyed on the full (node, predicate,
// direction) triple — a plain node-level set would collapse the parallel edges
// the multigraph deliberately keeps. A separate node-level visited set bounds
// multi-hop frontier expansion, which also makes the walk cycle-safe.
func Neighbors(idx *index.Index, id string, predicate *string, direction string, depth int) ([]Neighbor, error) {
	g := BuildGraph(idx)
	src, err := entity.ResolveID(idx, id)
	if err != nil {
		return nil, err
	}
	// bounds frontier expansion (and the source is never its own neighbour)
	visited := map[index.Node]bool{src: true}
	// dedup on the whole (node, predicate, direction) triple — adjacentHit IS
	// that triple, so it doubles as the key.
	emitted := map[adjacentHit]bool{}
	frontier := []index.Node{src}
	found := []Neighbor{}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []index.Node
		for _, node := range frontier {
			for _, a := range adjacent(g, node, direction, predicate) {
				if a.node == src || emitted[a] {
					continue
				}
				emitted[a] = true
				found = append(found, Neighbor{
					Type:      a.node.Type,
					Slug:      a.node.Slug,
					Predicate: a.pred,
					Direction: a.dir,
					Derived:   a.dir == DirectionIn,
					Depth:     d + 1,
				})
				if !visited[a.node] {
					visited[a.node] = true
					next = append(next, a.node)
				}
			}
		}
		frontier = next
	}
	return found, nil
}

type adjacentHit struct {
	node index.Node
	pred string
	dir  string
}

// adjacent yields (neighbour, predicate, direction) for one node, filtered —
// _adjacent. Out-edges come first, exactly like the Python generator.
func adjacent(g *Graph, node index.Node, direction string, predicate *string) []adjacentHit {
	var out []adjacentHit
	if direction == DirectionOut || direction == DirectionBoth {
		for _, e := range g.OutEdges(node) {
			if predicate == nil || e.Predicate == *predicate {
				out = append(out, adjacentHit{e.To, e.Predicate, DirectionOut})
			}
		}
	}
	if direction == DirectionIn || direction == DirectionBoth {
		for _, e := range g.InEdges(node) {
			if predicate == nil || e.Predicate == *predicate {
				out = append(out, adjacentHit{e.From, e.Predicate, DirectionIn})
			}
		}
	}
	return out
}

// Impact returns the transitive closure over predicate from id_, depth-marked —
// the impact() port.
//
// Descendants by default; ancestors under reverse. A BFS over the single-
// predicate subgraph: cycle-safe, each node visited once. The source is included
// at depth 0, so a leaf returns [source] (the caller reports "no downstream
// impact"). The result is sorted by (depth, (type, slug)) — a TUPLE compare, never
// a joined-string one.
func Impact(idx *index.Index, id string, predicate string, reverse bool) ([]ImpactNode, error) {
	g := BuildGraph(idx)
	src, err := entity.ResolveID(idx, id)
	if err != nil {
		return nil, err
	}
	succ := predicateAdjacency(g, predicate, reverse)
	depths := shortestPathLengths(succ, src)

	nodes := make([]index.Node, 0, len(depths))
	for n := range depths {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if depths[nodes[i]] != depths[nodes[j]] {
			return depths[nodes[i]] < depths[nodes[j]]
		}
		return nodes[i].Less(nodes[j])
	})
	out := make([]ImpactNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, ImpactNode{Type: n.Type, Slug: n.Slug, Depth: depths[n]})
	}
	return out, nil
}

// predicateAdjacency is _predicate_digraph collapsed to a successor map, with
// the reverse view folded in (sub.reverse(copy=False)).
func predicateAdjacency(g *Graph, predicate string, reverse bool) map[index.Node][]index.Node {
	succ := map[index.Node][]index.Node{}
	for _, e := range g.PredicateEdges(predicate) {
		u, v := e.From, e.To
		if reverse {
			u, v = v, u
		}
		succ[u] = append(succ[u], v)
	}
	return succ
}

// shortestPathLengths is nx.single_source_shortest_path_length: BFS depths for
// every node reachable from src, src itself at 0.
func shortestPathLengths(succ map[index.Node][]index.Node, src index.Node) map[index.Node]int {
	depths := map[index.Node]int{src: 0}
	frontier := []index.Node{src}
	for d := 1; len(frontier) > 0; d++ {
		var next []index.Node
		for _, n := range frontier {
			for _, v := range succ[n] {
				if _, seen := depths[v]; seen {
					continue
				}
				depths[v] = d
				next = append(next, v)
			}
		}
		frontier = next
	}
	return depths
}

// History follows the self-referential predicate chain from id_ back in order —
// the history() port.
//
// Returns the source followed by each prior record, newest first. Each prior
// record carries its derived SupersededBy (the newer record that supersedes it),
// computed from chain order and never stored. A visited set guards a cyclic
// chain; limit (nil = unbounded) caps to the N most recent links.
func History(idx *index.Index, id string, predicate string, limit *int) ([]HistoryLink, error) {
	g := BuildGraph(idx)
	src, err := entity.ResolveID(idx, id)
	if err != nil {
		return nil, err
	}
	chain := []index.Node{src}
	seen := map[index.Node]bool{src: true}
	cur := src
	for {
		next, ok := firstSuccessor(g, cur, predicate)
		if !ok || seen[next] {
			break
		}
		seen[next] = true
		chain = append(chain, next)
		cur = next
	}
	if limit != nil {
		chain = pySlice(chain, *limit)
	}
	links := make([]HistoryLink, 0, len(chain))
	for i, node := range chain {
		supersededBy := ""
		if i > 0 {
			supersededBy = chain[i-1].ID()
		}
		links = append(links, HistoryLink{Type: node.Type, Slug: node.Slug, SupersededBy: supersededBy})
	}
	return links, nil
}

// pySlice is Python's chain[:n] — a negative n counts from the end, and any
// out-of-range bound clamps instead of panicking.
func pySlice(chain []index.Node, n int) []index.Node {
	if n < 0 {
		n += len(chain)
		if n < 0 {
			n = 0
		}
	}
	if n > len(chain) {
		n = len(chain)
	}
	return chain[:n]
}

// firstSuccessor is _first_successor: the first node reached from node via a
// predicate edge.
//
// history assumes a single-valued chain predicate (supersedes is `to: decision`,
// single), so following the first edge is the whole chain. A many-valued
// predicate would branch — lift to a tree/DAG walk if one appears.
func firstSuccessor(g *Graph, node index.Node, predicate string) (index.Node, bool) {
	for _, e := range g.OutEdges(node) {
		if e.Predicate == predicate {
			return e.To, true
		}
	}
	return index.Node{}, false
}

// --- root-taking entries -----------------------------------------------------

// WalkNeighbors is walk_neighbors: load, index, and walk in one call. A named
// predicate no type declares is a located error; nil (no filter) stays valid.
func WalkNeighbors(root, id string, predicate *string, direction string, depth int) ([]Neighbor, error) {
	resolved, idx, err := walkIndex(root)
	if err != nil {
		return nil, err
	}
	if predicate != nil {
		if err := requirePredicate(resolved, *predicate); err != nil {
			return nil, err
		}
	}
	return Neighbors(idx, id, predicate, direction, depth)
}

// WalkImpact is walk_impact. An explicit or default predicate no type declares is
// a located error, so impact on a preset without that edge fails cleanly instead
// of returning a lone source node.
func WalkImpact(root, id string, predicate string, reverse bool) ([]ImpactNode, error) {
	resolved, idx, err := walkIndex(root)
	if err != nil {
		return nil, err
	}
	if err := requirePredicate(resolved, predicate); err != nil {
		return nil, err
	}
	return Impact(idx, id, predicate, reverse)
}

// WalkHistory is walk_history. The default `supersedes` predicate is absent on a
// preset (e.g. firm-ops) that declares no supersession — a located error, not a
// silent one-node chain.
func WalkHistory(root, id string, predicate string, limit *int) ([]HistoryLink, error) {
	resolved, idx, err := walkIndex(root)
	if err != nil {
		return nil, err
	}
	if err := requirePredicate(resolved, predicate); err != nil {
		return nil, err
	}
	return History(idx, id, predicate, limit)
}

// walkIndex is _walk_index plus the schema load the walk_* entries share: the
// index the walks see has strays filtered, like every other verb.
func walkIndex(root string) (*schema.ResolvedSchema, *index.Index, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, nil, err
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, nil, err
	}
	return resolved, index.Filter(scanned, index.StrayNodes(scanned)), nil
}

// requirePredicate is _require_predicate: raise unless some type in the schema
// declares predicate (universal edges included).
//
// core/errors.py builds this LocatedError inline with no factory, so the code and
// message are assembled here rather than in internal/errs.
func requirePredicate(resolved *schema.ResolvedSchema, predicate string) error {
	declared := map[string]bool{}
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		for _, p := range rtype.Relations.Keys() {
			declared[p] = true
		}
	}
	if declared[predicate] {
		return nil
	}
	names := make([]string, 0, len(declared))
	for p := range declared {
		names = append(names, p)
	}
	sort.Strings(names)
	e := errs.New("unknown_predicate", fmt.Sprintf(
		"No predicate '%s' in the schema. Declared predicates: %s",
		predicate, strings.Join(names, ", ")))
	e.Relation = predicate
	return e
}
