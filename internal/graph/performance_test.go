package graph

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/endgame-build/khub/internal/index"
)

func TestNeighborsExtremeDepth(t *testing.T) {
	idx := buildIdx(t, dependsWS(t))
	for _, depth := range []int{math.MinInt, -1, 0, 4, math.MaxInt} {
		got, err := Neighbors(idx, "dep-a", ptr("depends_on"), DirectionOut, depth)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if depth > 0 {
			want = 3
		}
		if len(got) != want {
			t.Fatalf("depth %d: %v", depth, got)
		}
	}
}

func TestCyclesRepresentativeAndDeterministic(t *testing.T) {
	n := func(s string) index.Node { return index.Node{Type: "node", Slug: s} }
	// Sorted SCC membership a,b,c is NOT a directed cycle: a->c->b->a is.
	edges := [][2]string{{"a", "c"}, {"c", "b"}, {"b", "a"}, {"b", "c"}, {"x", "x"}, {"z", "y"}}
	want := [][]index.Node{{n("a"), n("c"), n("b")}, {n("x")}}
	for iteration := range 20 {
		g := newGraph()
		for i := range edges {
			e := edges[(i+iteration)%len(edges)]
			g.addEdge(n(e[0]), n(e[1]), "p")
		}
		g.addEdge(n("a"), n("a"), "other")
		got := Cycles(g, "p")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("cycles = %v, want %v", got, want)
		}
	}
}

func BenchmarkCyclesDense(b *testing.B) {
	for _, size := range []int{50, 200} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			g := newGraph()
			for i := range size {
				for j := range size {
					if i != j {
						g.addEdge(index.Node{Slug: fmt.Sprint(i)}, index.Node{Slug: fmt.Sprint(j)}, "p")
					}
				}
			}
			b.ResetTimer()
			for b.Loop() {
				if len(Cycles(g, "p")) != 1 {
					b.Fatal("missing component")
				}
			}
		})
	}
}
