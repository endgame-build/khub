package graph

// Ports the library-level rows of tests/test_neighbors.py (TS-QRY-002):
// direction and predicate filtering, the derived inbound inverse, bounded depth
// vs the unbounded impact closure, parallel edges, and the located lookup error.

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

// nws is the neighbors fixture: initech with two inbound `client` edges and a
// meeting supplying the inbound derived inverse.
func nws(t *testing.T) string {
	t.Helper()
	ws := freshWS(t)
	recent := []kv{{"created", date("2026-06-01")}, {"updated", date("2026-06-01")}}
	seed(t, ws, "clients/initech.md", append([]kv{{"type", "client"}, {"name", "Initech"}}, recent...)...)
	seed(t, ws, "identity/team/noor.md",
		kv{"type", "person"}, kv{"name", "Noor"}, kv{"role", "partner"}, kv{"created", date("2026-06-01")})
	seed(t, ws, "projects/initech-pov/_index.md",
		append([]kv{{"type", "project"}, {"client", "initech"}, {"owner", "noor"}}, recent...)...)
	seed(t, ws, "opportunities/initech-deal/_index.md",
		append([]kv{{"type", "opportunity"}, {"stage", "prospect"}, {"client", "initech"}, {"owner", "noor"}}, recent...)...)
	seed(t, ws, "meetings/kickoff.md",
		kv{"type", "meeting"}, kv{"engagement", "initech-pov"}, kv{"date", "2026-06-20T10:00:00"},
		kv{"call_type", "client"}, kv{"source", "recording"})
	seed(t, ws, "clients/lonely-client.md", append([]kv{{"type", "client"}, {"name", "Lonely"}}, recent...)...)
	return ws
}

// dependsWS is a four-deep depends_on chain.
func dependsWS(t *testing.T) string {
	t.Helper()
	ws := freshWS(t)
	recent := []kv{{"created", date("2026-06-01")}, {"updated", date("2026-06-01")}}
	for _, pair := range [][2]string{{"dep-a", "dep-b"}, {"dep-b", "dep-c"}, {"dep-c", "dep-d"}} {
		seed(t, ws, "clients/"+pair[0]+".md", append([]kv{
			{"type", "client"}, {"name", pair[0]}, {"depends_on", []any{pair[1]}},
		}, recent...)...)
	}
	seed(t, ws, "clients/dep-d.md", append([]kv{{"type", "client"}, {"name", "dep-d"}}, recent...)...)
	return ws
}

// parallelWS: two clients joined by two predicates, plus a mutual inbound edge.
func parallelWS(t *testing.T) string {
	t.Helper()
	ws := freshWS(t)
	recent := []kv{{"created", date("2026-06-01")}, {"updated", date("2026-06-01")}}
	seed(t, ws, "clients/pa.md", append([]kv{
		{"type", "client"}, {"name", "pa"}, {"related", []any{"pb"}}, {"depends_on", []any{"pb"}},
	}, recent...)...)
	seed(t, ws, "clients/pb.md", append([]kv{
		{"type", "client"}, {"name", "pb"}, {"related", []any{"pa"}},
	}, recent...)...)
	return ws
}

func mustNeighbors(t *testing.T, root, id string, predicate *string, direction string, depth int) []Neighbor {
	t.Helper()
	got, err := Neighbors(buildIdx(t, root), id, predicate, direction, depth)
	if err != nil {
		t.Fatalf("Neighbors(%s): %v", id, err)
	}
	return got
}

// TS-QRY-002-U01 (QRY-004): default is both; in/out narrow direction.
func TestNeighborsDirectionFilter(t *testing.T) {
	ws := nws(t)
	both := mustNeighbors(t, ws, "initech-pov", nil, DirectionBoth, 1)
	var sawOut, sawIn bool
	for _, n := range both {
		sawOut = sawOut || n.Direction == DirectionOut
		sawIn = sawIn || n.Direction == DirectionIn
	}
	if !sawOut || !sawIn {
		t.Fatalf("both: want an out and an in edge, got %+v", both)
	}
	for _, n := range mustNeighbors(t, ws, "initech-pov", nil, DirectionOut, 1) {
		if n.Direction != DirectionOut {
			t.Fatalf("out: %+v", n)
		}
	}
	inOnly := mustNeighbors(t, ws, "initech-pov", nil, DirectionIn, 1)
	if len(inOnly) == 0 {
		t.Fatal("in: want at least one inbound neighbour")
	}
	for _, n := range inOnly {
		if n.Direction != DirectionIn {
			t.Fatalf("in: %+v", n)
		}
	}
}

// TS-QRY-002-U02 (REQ-QRY002-02): --predicate restricts adjacency to one relation.
func TestNeighborsPredicateFilter(t *testing.T) {
	got := mustNeighbors(t, nws(t), "initech-pov", ptr("owner"), DirectionBoth, 1)
	if len(got) != 1 || got[0].Slug != "noor" || got[0].Predicate != "owner" {
		t.Fatalf("want one (noor, owner), got %+v", got)
	}
}

// TS-QRY-002-U03 (QRY-003/SHARED-002): inbound is a derived inverse, never stored.
func TestNeighborsInboundIncludesDerivedInverse(t *testing.T) {
	ws := nws(t)
	got := mustNeighbors(t, ws, "initech-pov", nil, DirectionIn, 1)
	found := false
	for _, n := range got {
		if n.Predicate == "engagement" && n.Derived && n.Slug == "kickoff" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a derived engagement<-kickoff inverse, got %+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(ws, "projects", "initech-pov", "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "engagement") {
		t.Fatal("the inverse must be computed, never persisted on the source")
	}
}

// TS-QRY-002-U04 (QRY-010): --depth is bounded; impact is the unbounded closure.
func TestNeighborsDepthBoundedVsImpactClosure(t *testing.T) {
	ws := dependsWS(t)
	idx := buildIdx(t, ws)
	near, err := Neighbors(idx, "dep-a", ptr("depends_on"), DirectionOut, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := slugSet(neighborSlugs(near)); !eqSlices(got, []string{"dep-b", "dep-c"}) {
		t.Fatalf("depth 2 stops at two hops; got %v", got)
	}
	full, err := Impact(idx, "dep-a", "depends_on", false)
	if err != nil {
		t.Fatal(err)
	}
	var beyond []string
	for _, n := range full {
		if n.Depth > 0 {
			beyond = append(beyond, n.Slug)
		}
	}
	if got := slugSet(beyond); !eqSlices(got, []string{"dep-b", "dep-c", "dep-d"}) {
		t.Fatalf("impact exhausts the predicate; got %v", got)
	}
}

// QRY-003: two predicates joining one pair are two adjacencies, neither collapsed.
func TestNeighborsParallelEdges(t *testing.T) {
	ws := parallelWS(t)
	out := mustNeighbors(t, ws, "pa", nil, DirectionOut, 1)
	var pairs []string
	for _, n := range out {
		pairs = append(pairs, n.Slug+"/"+n.Predicate)
	}
	if got := slugSet(pairs); !eqSlices(got, []string{"pb/depends_on", "pb/related"}) {
		t.Fatalf("want both predicates kept, got %v", got)
	}
	var triples []string
	for _, n := range mustNeighbors(t, ws, "pa", nil, DirectionBoth, 1) {
		triples = append(triples, n.Slug+"/"+n.Predicate+"/"+n.Direction)
	}
	if !containsStr(triples, "pb/related/in") {
		t.Fatalf("the mutual inbound edge must survive, got %v", triples)
	}
}

// TS-QRY-002-U05 (REQ-QRY002-03): an unresolvable id raises a lookup error.
func TestNeighborsLookupError(t *testing.T) {
	_, err := Neighbors(buildIdx(t, nws(t)), "ghost", nil, DirectionBoth, 1)
	var located *errs.Located
	ok := errors.As(err, &located)
	if !ok || located.Code != "lookup_error" {
		t.Fatalf("want lookup_error, got %#v", err)
	}
}

// TS-QRY-002-U06 (REQ-QRY002-01): every neighbour carries a predicate and direction.
func TestNeighborsAdjacencyLabeled(t *testing.T) {
	got := mustNeighbors(t, nws(t), "initech-pov", nil, DirectionBoth, 1)
	if len(got) == 0 {
		t.Fatal("want neighbours")
	}
	for _, n := range got {
		if n.Predicate == "" || (n.Direction != DirectionIn && n.Direction != DirectionOut) {
			t.Fatalf("unlabelled adjacency: %+v", n)
		}
	}
}

// The isolated entity returns an empty (non-nil) set, so the CLI emits `[]`.
func TestNeighborsIsolatedEntity(t *testing.T) {
	got := mustNeighbors(t, nws(t), "lonely-client", nil, DirectionBoth, 1)
	if got == nil || len(got) != 0 {
		t.Fatalf("want an empty non-nil slice, got %#v", got)
	}
}

// The second hop appears only at depth 2 (the CLI's --depth 2 row).
func TestNeighborsDepthTwoReachesSecondHop(t *testing.T) {
	ws := nws(t)
	one := neighborIDs(mustNeighbors(t, ws, "initech", nil, DirectionBoth, 1))
	if containsStr(one, "person/noor") {
		t.Fatalf("noor is two hops out; got %v", one)
	}
	two := neighborIDs(mustNeighbors(t, ws, "initech", nil, DirectionBoth, 2))
	if !containsStr(two, "person/noor") {
		t.Fatalf("want person/noor at depth 2, got %v", two)
	}
}

// The record order is the JSON array order, so it is contract: out-edges before
// in-edges per node, each in adjacency-insertion order, depth by depth.
func TestNeighborsRecordOrder(t *testing.T) {
	got := mustNeighbors(t, nws(t), "initech-pov", nil, DirectionBoth, 1)
	var rows []string
	for _, n := range got {
		rows = append(rows, n.Direction+" "+n.Predicate+" "+n.Type+"/"+n.Slug)
	}
	want := []string{
		"out client client/initech",
		"out owner person/noor",
		"in engagement meeting/kickoff",
	}
	if !eqSlices(rows, want) {
		t.Fatalf("want %v, got %v", want, rows)
	}
}

// Depth is assigned per BFS round, and a node already emitted at depth 1 is not
// re-emitted at depth 2 under the same (node, predicate, direction) triple.
func TestNeighborsDepthMarking(t *testing.T) {
	got := mustNeighbors(t, dependsWS(t), "dep-a", nil, DirectionOut, 3)
	depth := map[string]int{}
	for _, n := range got {
		depth[n.Slug] = n.Depth
	}
	want := map[string]int{"dep-b": 1, "dep-c": 2, "dep-d": 3}
	if len(depth) != len(want) {
		t.Fatalf("want %v, got %v", want, depth)
	}
	for k, v := range want {
		if depth[k] != v {
			t.Fatalf("want %v, got %v", want, depth)
		}
	}
}

// --- helpers -----------------------------------------------------------------

func neighborSlugs(ns []Neighbor) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Slug)
	}
	return out
}

func neighborIDs(ns []Neighbor) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Type+"/"+n.Slug)
	}
	return out
}

func slugSet(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func eqSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
