package graph

import (
	"fmt"
	"testing"

	"github.com/endgame-build/khub/internal/index"
)

func groupLines(gs []PredicateGroup) []string {
	var out []string
	for _, g := range gs {
		dir := "in"
		if g.Out {
			dir = "out"
		}
		ids := ""
		for i, n := range g.Nodes {
			if i > 0 {
				ids += ","
			}
			ids += n.ID()
		}
		out = append(out, fmt.Sprintf("%s %s.%s [%s]", dir, g.SourceType, g.Predicate, ids))
	}
	return out
}

// Outgoing groups first, then incoming, each keyed by the storing type and the
// predicate; a parallel pair under two predicates is two groups, not one.
func TestPredicateGroupsDirectionAndOrder(t *testing.T) {
	g := BuildGraph(buildIdx(t, parallelWS(t)))
	got := groupLines(g.PredicateGroups(index.Node{Type: "client", Slug: "pa"}))
	want := []string{
		"out client.related [client/pb]",
		"out client.depends_on [client/pb]",
		"in client.related [client/pb]",
	}
	if !eqSlices(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// A repeated or differently spelt target is one neighbour, and a self link is
// none: len(Nodes) is the distinct count query --count reads.
func TestPredicateGroupsDistinctAndNoSelf(t *testing.T) {
	ws := freshWS(t)
	recent := []kv{{"created", date("2026-06-01")}, {"updated", date("2026-06-01")}}
	seed(t, ws, "clients/src.md", append([]kv{
		{"type", "client"}, {"name", "Src"},
		{"depends_on", []any{"dst", "client/dst", "src"}},
	}, recent...)...)
	seed(t, ws, "clients/dst.md", append([]kv{{"type", "client"}, {"name", "Dst"}}, recent...)...)
	g := BuildGraph(buildIdx(t, ws))
	got := groupLines(g.PredicateGroups(index.Node{Type: "client", Slug: "src"}))
	want := []string{"out client.depends_on [client/dst]"}
	if !eqSlices(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	got = groupLines(g.PredicateGroups(index.Node{Type: "client", Slug: "dst"}))
	want = []string{"in client.depends_on [client/src]"}
	if !eqSlices(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	if gs := g.PredicateGroups(index.Node{Type: "client", Slug: "nobody"}); len(gs) != 0 {
		t.Fatalf("an unknown node has no groups, got %v", groupLines(gs))
	}
}
