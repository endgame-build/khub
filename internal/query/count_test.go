package query

// Cardinality filters (#58): the parser, the comparison, and the promise that
// a count reads the same graph --has/--missing/--orphan read.

import (
	"errors"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

func TestParseComparison(t *testing.T) {
	good := map[string]Comparison{
		"0": {"=", 0}, "=3": {"=", 3}, "!=1": {"!=", 1}, "<2": {"<", 2},
		"<=2": {"<=", 2}, ">5": {">", 5}, ">=5": {">=", 5}, "007": {"=", 7},
	}
	for expr, want := range good {
		got, ok := ParseComparison(expr)
		if !ok || got != want {
			t.Errorf("ParseComparison(%q) = %v, %v; want %v", expr, got, ok, want)
		}
	}
	for _, expr := range []string{"", ">=", "-1", ">=-1", "+1", "1.5", " 1", "=>1", "==1", "many", "99999999999999999999"} {
		if _, ok := ParseComparison(expr); ok {
			t.Errorf("ParseComparison(%q) accepted", expr)
		}
	}
}

func TestParseCount(t *testing.T) {
	got, ok := ParseCount("depends_on>=2")
	if !ok || got != (Count{"depends_on", Comparison{">=", 2}}) {
		t.Fatalf("got %v, %v", got, ok)
	}
	for _, expr := range []string{"depends_on", "depends_on2", ">=2", "depends_on>=", "depends_on>=x"} {
		if _, ok := ParseCount(expr); ok {
			t.Errorf("ParseCount(%q) accepted", expr)
		}
	}
}

func TestComparisonHolds(t *testing.T) {
	cases := []struct {
		c    Comparison
		n    int
		want bool
	}{
		{Comparison{"=", 2}, 2, true}, {Comparison{"=", 2}, 3, false},
		{Comparison{"!=", 2}, 3, true}, {Comparison{"!=", 2}, 2, false},
		{Comparison{"<", 2}, 1, true}, {Comparison{"<", 2}, 2, false},
		{Comparison{"<=", 2}, 2, true}, {Comparison{"<=", 2}, 3, false},
		{Comparison{">", 2}, 3, true}, {Comparison{">", 2}, 2, false},
		{Comparison{">=", 2}, 2, true}, {Comparison{">=", 2}, 1, false},
	}
	for _, c := range cases {
		if got := c.c.Holds(c.n); got != c.want {
			t.Errorf("%v.Holds(%d) = %v", c.c, c.n, got)
		}
	}
}

// countWS: first is superseded twice (by second and third), live by none.
func countWS(t *testing.T) string {
	t.Helper()
	ws := inverseWS(t) // second supersedes first; live stands alone
	seed(t, ws, "adrs/third.md", kv{"type", "adr"}, kv{"title", "Third"}, kv{"supersedes", "first"})
	seed(t, ws, "repos/r1.md", kv{"type", "repo"}, kv{"title", "R1"})
	return ws
}

func cnt(name, op string, n int) []Count { return []Count{{name, Comparison{op, n}}} }

// A declared inverse is counted from the inbound side, like --has answers it.
func TestCountOverAnInverse(t *testing.T) {
	ws := countWS(t)
	got := slugs(mustQuery(t, ws, Filters{Counts: cnt("superseded", ">=", 2)}))
	if !eq(got, []string{"first"}) {
		t.Fatalf("got %v", got)
	}
}

// `>=1` is --has and `=0` is --missing, including the rule that a type which
// cannot carry the name is no zero.
func TestCountAgreesWithHasAndMissing(t *testing.T) {
	ws := countWS(t)
	for _, name := range []string{"supersedes", "superseded"} {
		has := sortedSlugs(mustQuery(t, ws, Filters{Has: ptr(name)}))
		atLeastOne := sortedSlugs(mustQuery(t, ws, Filters{Counts: cnt(name, ">=", 1)}))
		if !eq(has, atLeastOne) {
			t.Errorf("%s: --has %v, --count >=1 %v", name, has, atLeastOne)
		}
		missing := sortedSlugs(mustQuery(t, ws, Filters{Missing: ptr(name)}))
		zero := sortedSlugs(mustQuery(t, ws, Filters{Counts: cnt(name, "=", 0)}))
		if !eq(missing, zero) {
			t.Errorf("%s: --missing %v, --count =0 %v", name, missing, zero)
		}
	}
}

// Every bound ANDs, so two make a range.
func TestCountsAnd(t *testing.T) {
	ws := countWS(t)
	got := sortedSlugs(mustQuery(t, ws, Filters{Type: ptr("adr"),
		InCount: []Comparison{{">=", 1}}, OutCount: []Comparison{{"=", 0}}}))
	if !eq(got, []string{"first"}) {
		t.Fatalf("got %v", got)
	}
	got = sortedSlugs(mustQuery(t, ws, Filters{Type: ptr("adr"),
		InCount: []Comparison{{">=", 1}, {"<", 2}}}))
	if !eq(got, []string{}) { // first has two
		t.Fatalf("got %v", got)
	}
}

// Zero in and zero out is the edge-less set --orphan reports.
func TestInOutZeroIsOrphan(t *testing.T) {
	ws := countWS(t)
	orphans := sortedSlugs(mustQuery(t, ws, Filters{Orphan: true}))
	edgeless := sortedSlugs(mustQuery(t, ws, Filters{
		InCount: []Comparison{{"=", 0}}, OutCount: []Comparison{{"=", 0}}}))
	if !eq(orphans, edgeless) {
		t.Fatalf("--orphan %v, counts %v", orphans, edgeless)
	}
}

// An attribute has no edges, and a typo must not read as a zero.
func TestCountRefusesWhatHasNoEdges(t *testing.T) {
	ws := countWS(t)
	for _, f := range []Filters{
		{Type: ptr("adr"), Counts: cnt("status", "=", 0)},
		{Counts: cnt("status", "=", 0)},
		{Type: ptr("repo"), Counts: cnt("superseded", "=", 0)},
		{Counts: cnt("bogus", ">=", 1)},
	} {
		_, err := Query(ws, f, now)
		var located *errs.Located
		if !errors.As(err, &located) || located.Code != "filter_error" {
			t.Errorf("%+v: want filter_error, got %#v", f, err)
		}
	}
}

// A relation that is also another relation's declared inverse reaches the same
// neighbour twice, once each way; the count is of neighbours, not edges.
func TestCountIsOfDistinctNeighbours(t *testing.T) {
	ws := wsFromSchema(t, `
version: "0.1.0"
ontology:
  entities:
    node:
      relations:
        parent:   { to: node, inverse: children }
        children: { to: node, many: true }
storage:
  node: { layout: file, path: nodes }
`)
	seed(t, ws, "nodes/a.md", kv{"type", "node"}, kv{"title", "A"}, kv{"children", []any{"b", "c"}})
	seed(t, ws, "nodes/b.md", kv{"type", "node"}, kv{"title", "B"}, kv{"parent", "a"})
	seed(t, ws, "nodes/c.md", kv{"type", "node"}, kv{"title", "C"})
	seed(t, ws, "nodes/d.md", kv{"type", "node"}, kv{"title", "D"}, kv{"parent", "a"})
	got := slugs(mustQuery(t, ws, Filters{Type: ptr("node"), Counts: cnt("children", "=", 3)}))
	if !eq(got, []string{"a"}) { // b, c, d: b reached both ways counts once
		t.Fatalf("got %v", got)
	}
}

func TestDeclaresField(t *testing.T) {
	ws := countWS(t)
	for name, want := range map[string]bool{"status": true, "supersedes": true, "count": false} {
		if got := DeclaresField(ws, name); got != want {
			t.Errorf("DeclaresField(%q) = %v", name, got)
		}
	}
	if DeclaresField(t.TempDir(), "status") {
		t.Error("a directory with no schema declares nothing")
	}
}
