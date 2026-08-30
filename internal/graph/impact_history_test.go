package graph

// Ports the library-level rows of tests/test_impact_history.py (TS-QRY-003 /
// TS-QRY-004): the depth-marked transitive closure with its reverse and cycle
// cases, and the supersession chain with its derived inverse — plus the
// _require_predicate gate the walk_* entries add and the cycle enumeration
// integrity consumes.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
)

// historyPreset mirrors tests/test_impact_history.py HISTORY_PRESET — firm-ops
// declares no supersession, so history needs its own preset.
const historyPreset = `
version: "0.1.0"
ontology:
  entities:
    decision:
      relations:
        supersedes: { to: decision, inverse: superseded_by }
storage:
  decision: { layout: file, path: decisions }
`

// impactWS: a depends_on chain, a leaf, and a cycle — all on the universal edge.
func impactWS(t *testing.T) string {
	t.Helper()
	ws := freshWS(t)
	recent := []kv{{"created", date("2026-06-01")}, {"updated", date("2026-06-01")}}
	chain := []struct{ slug, name, dep string }{
		{"node-a", "A", "node-b"},
		{"node-b", "B", "node-c"},
		{"node-c", "C", ""},
		{"leaf-node", "Leaf", ""},
		{"node-x", "X", "node-y"},
		{"node-y", "Y", "node-z"},
		{"node-z", "Z", "node-x"},
	}
	for _, c := range chain {
		fields := []kv{{"type", "client"}, {"name", c.name}}
		if c.dep != "" {
			fields = append(fields, kv{"depends_on", []any{c.dep}})
		}
		seed(t, ws, "clients/"+c.slug+".md", append(fields, recent...)...)
	}
	return ws
}

// historyWS: a four-link supersedes chain over the fixture preset.
func historyWS(t *testing.T) string {
	t.Helper()
	ws := wsFromSchema(t, historyPreset)
	for _, pair := range [][2]string{
		{"decision-0012", "decision-0008"},
		{"decision-0008", "decision-0005"},
		{"decision-0005", "decision-0001"},
	} {
		seed(t, ws, "decisions/"+pair[0]+".md", kv{"type", "decision"}, kv{"supersedes", pair[1]})
	}
	seed(t, ws, "decisions/decision-0001.md", kv{"type", "decision"})
	return ws
}

func mustImpact(t *testing.T, root, id, predicate string, reverse bool) []ImpactNode {
	t.Helper()
	got, err := Impact(buildIdx(t, root), id, predicate, reverse)
	if err != nil {
		t.Fatalf("Impact(%s): %v", id, err)
	}
	return got
}

func beyondSource(ns []ImpactNode) []string {
	var out []string
	for _, n := range ns {
		if n.Depth > 0 {
			out = append(out, n.Slug)
		}
	}
	return slugSet(out)
}

// TS-QRY-003-U01/U04 (QRY-005): forward closure over the default depends_on.
func TestImpactDescendantsDefaultPredicate(t *testing.T) {
	got := mustImpact(t, impactWS(t), "node-a", "depends_on", false)
	if want := []string{"node-b", "node-c"}; !eqSlices(beyondSource(got), want) {
		t.Fatalf("want %v, got %+v", want, got)
	}
	if got[0].Slug != "node-a" || got[0].Depth != 0 {
		t.Fatalf("source must lead at depth 0, got %+v", got[0])
	}
}

// TS-QRY-003-U02 (REQ-QRY003-02): --reverse walks ancestors, not descendants.
func TestImpactAncestorsOnReverse(t *testing.T) {
	got := mustImpact(t, impactWS(t), "node-c", "depends_on", true)
	if want := []string{"node-a", "node-b"}; !eqSlices(beyondSource(got), want) {
		t.Fatalf("want %v, got %+v", want, got)
	}
}

// TS-QRY-003-U03 (QRY-006): a cycle terminates and each node appears once.
func TestImpactCycleSafe(t *testing.T) {
	got := mustImpact(t, impactWS(t), "node-x", "depends_on", false)
	var slugs []string
	for _, n := range got {
		slugs = append(slugs, n.Slug)
	}
	sorted := append([]string(nil), slugs...)
	sort.Strings(sorted)
	if want := []string{"node-x", "node-y", "node-z"}; !eqSlices(sorted, want) {
		t.Fatalf("want %v, got %v", want, sorted)
	}
	if len(slugs) != len(slugSet(slugs)) {
		t.Fatalf("each node once; got %v", slugs)
	}
}

// TS-QRY-003-U05 (REQ-QRY003-04): each node carries its depth from the source.
func TestImpactTreeDepth(t *testing.T) {
	got := mustImpact(t, impactWS(t), "node-a", "depends_on", false)
	depth := map[string]int{}
	for _, n := range got {
		depth[n.Slug] = n.Depth
	}
	want := map[string]int{"node-a": 0, "node-b": 1, "node-c": 2}
	if len(depth) != len(want) {
		t.Fatalf("want %v, got %v", want, depth)
	}
	for k, v := range want {
		if depth[k] != v {
			t.Fatalf("want %v, got %v", want, depth)
		}
	}
}

// TS-QRY-003-U06 (REQ-QRY003-02): --predicate retargets the closure.
func TestImpactPredicateOverride(t *testing.T) {
	got := mustImpact(t, impactWS(t), "node-a", "related", false)
	if len(beyondSource(got)) != 0 {
		t.Fatalf("no `related` edges to walk; got %+v", got)
	}
}

// The closure is ordered by (depth, (type, slug)) — a TUPLE compare, so a type
// boundary always outranks a slug comparison at the same depth.
func TestImpactOrderIsDepthThenTupleCompare(t *testing.T) {
	ws := wsFromSchema(t, `
version: "0.1.0"
ontology:
  entities:
    alpha: {}
    zeta: {}
storage:
  alpha: { layout: file, path: alpha }
  zeta:  { layout: file, path: zeta }
`)
	// One source fanning out to (zeta, aaa) and (alpha, zzz) at the same depth:
	// a joined-string compare would put "alpha/zzz" first either way, but the
	// tuple compare is what pins it — and the slug order inside a type is what a
	// joined compare would break at depth 2.
	seed(t, ws, "alpha/src.md", kv{"type", "alpha"}, kv{"depends_on", []any{"zeta/aaa", "alpha/zzz"}})
	seed(t, ws, "zeta/aaa.md", kv{"type", "zeta"})
	seed(t, ws, "alpha/zzz.md", kv{"type", "alpha"})
	got := mustImpact(t, ws, "alpha/src", "depends_on", false)
	var ids []string
	for _, n := range got {
		ids = append(ids, n.Type+"/"+n.Slug)
	}
	want := []string{"alpha/src", "alpha/zzz", "zeta/aaa"}
	if !eqSlices(ids, want) {
		t.Fatalf("want %v, got %v", want, ids)
	}
}

// --- history -----------------------------------------------------------------

func mustHistory(t *testing.T, root, id, predicate string, limit *int) []HistoryLink {
	t.Helper()
	got, err := History(buildIdx(t, root), id, predicate, limit)
	if err != nil {
		t.Fatalf("History(%s): %v", id, err)
	}
	return got
}

func historySlugs(ls []HistoryLink) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Slug)
	}
	return out
}

// TS-QRY-004-U01/U04 (QRY-007): follow the default supersedes chain in order.
func TestHistoryChainOrder(t *testing.T) {
	got := mustHistory(t, historyWS(t), "decision-0012", "supersedes", nil)
	want := []string{"decision-0012", "decision-0008", "decision-0005", "decision-0001"}
	if !eqSlices(historySlugs(got), want) {
		t.Fatalf("want %v, got %v", want, historySlugs(got))
	}
}

// TS-QRY-004-U02 (REQ-QRY004-02): --limit caps to the N most recent links.
func TestHistoryLimit(t *testing.T) {
	got := mustHistory(t, historyWS(t), "decision-0012", "supersedes", ptr(2))
	if want := []string{"decision-0012", "decision-0008"}; !eqSlices(historySlugs(got), want) {
		t.Fatalf("want %v, got %v", want, historySlugs(got))
	}
}

// TS-QRY-004-U03 (QRY-SHARED-002): superseded_by is derived, never persisted.
func TestHistoryDerivedInverseNotStored(t *testing.T) {
	ws := historyWS(t)
	got := mustHistory(t, ws, "decision-0012", "supersedes", nil)
	by := map[string]string{}
	for _, l := range got {
		by[l.Slug] = l.SupersededBy
	}
	if by["decision-0008"] != "decision/decision-0012" {
		t.Fatalf("want the derived inverse, got %q", by["decision-0008"])
	}
	if by["decision-0012"] != "" {
		t.Fatalf("the head supersedes nothing above it, got %q", by["decision-0012"])
	}
	raw, err := os.ReadFile(filepath.Join(ws, "decisions", "decision-0008.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "superseded_by") {
		t.Fatal("superseded_by must never be persisted")
	}
}

// TS-QRY-004-U06 (QRY-008): history reads the graph chain, not git.
func TestHistoryReadsGraphNotGit(t *testing.T) {
	ws := historyWS(t)
	if _, err := os.Stat(filepath.Join(ws, ".git")); err == nil {
		t.Fatal("fixture must not be a git repo")
	}
	got := historySlugs(mustHistory(t, ws, "decision-0012", "supersedes", nil))
	if !eqSlices(got[:2], []string{"decision-0012", "decision-0008"}) {
		t.Fatalf("got %v", got)
	}
}

// A chain root returns only itself (the CLI's "No supersession history" line).
func TestHistoryChainRootReturnsSelf(t *testing.T) {
	got := mustHistory(t, historyWS(t), "decision-0001", "supersedes", nil)
	if !eqSlices(historySlugs(got), []string{"decision-0001"}) {
		t.Fatalf("got %v", historySlugs(got))
	}
}

// TS-QRY-004-U05 (REQ-QRY004-03): an unresolvable id raises a lookup error.
func TestHistoryLookupError(t *testing.T) {
	_, err := History(buildIdx(t, historyWS(t)), "ghost", "supersedes", nil)
	located, ok := err.(*errs.Located)
	if !ok || located.Code != "lookup_error" {
		t.Fatalf("want lookup_error, got %#v", err)
	}
}

// A cyclic supersedes chain terminates on the visited guard.
func TestHistoryCyclicChainTerminates(t *testing.T) {
	ws := wsFromSchema(t, historyPreset)
	for _, pair := range [][2]string{{"d1", "d2"}, {"d2", "d3"}, {"d3", "d1"}} {
		seed(t, ws, "decisions/"+pair[0]+".md", kv{"type", "decision"}, kv{"supersedes", pair[1]})
	}
	got := historySlugs(mustHistory(t, ws, "d1", "supersedes", nil))
	if !eqSlices(got, []string{"d1", "d2", "d3"}) {
		t.Fatalf("got %v", got)
	}
}

// --- walk entries / _require_predicate ---------------------------------------

// walk_history's default `supersedes` is absent on firm-ops — a located error,
// not a silent one-node chain.
func TestWalkHistoryUnknownPredicate(t *testing.T) {
	ws := nws(t)
	_, err := WalkHistory(ws, "initech", "supersedes", nil)
	located, ok := err.(*errs.Located)
	if !ok || located.Code != "unknown_predicate" {
		t.Fatalf("want unknown_predicate, got %#v", err)
	}
	if located.Relation != "supersedes" {
		t.Fatalf("want the predicate located, got %q", located.Relation)
	}
	if !strings.HasPrefix(located.Message, "No predicate 'supersedes' in the schema. Declared predicates: ") {
		t.Fatalf("message drift: %q", located.Message)
	}
	// The declared list is sorted, comma-space joined.
	tail := strings.TrimPrefix(located.Message, "No predicate 'supersedes' in the schema. Declared predicates: ")
	parts := strings.Split(tail, ", ")
	if !sort.StringsAreSorted(parts) {
		t.Fatalf("declared predicates must be sorted: %v", parts)
	}
	if !containsStr(parts, "depends_on") || !containsStr(parts, "client") {
		t.Fatalf("want the universal and firm-ops edges listed: %v", parts)
	}
}

// A named --predicate no type declares fails; nil (no filter) stays valid.
func TestWalkNeighborsPredicateGate(t *testing.T) {
	ws := nws(t)
	if _, err := WalkNeighbors(ws, "initech-pov", ptr("nope"), DirectionBoth, 1); err == nil {
		t.Fatal("want unknown_predicate")
	} else if located, ok := err.(*errs.Located); !ok || located.Code != "unknown_predicate" {
		t.Fatalf("got %#v", err)
	}
	if _, err := WalkNeighbors(ws, "initech-pov", nil, DirectionBoth, 1); err != nil {
		t.Fatalf("nil predicate must stay valid: %v", err)
	}
}

// walk_impact's default depends_on resolves on firm-ops (it is a base relation).
func TestWalkImpactDefaultPredicate(t *testing.T) {
	got, err := WalkImpact(dependsWS(t), "dep-a", "depends_on", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"dep-b", "dep-c", "dep-d"}; !eqSlices(beyondSource(got), want) {
		t.Fatalf("want %v, got %+v", want, got)
	}
}

// --- graph shape / cycles -----------------------------------------------------

// build_graph skips self-edges: a self-reference connects nothing new.
func TestBuildGraphSkipsSelfEdges(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "identity/team/solo.md",
		kv{"type", "person"}, kv{"name", "Solo"}, kv{"role", "manager"},
		kv{"created", date("2026-06-01")}, kv{"related", []any{"solo"}})
	g := BuildGraph(buildIdx(t, ws))
	n := index.Node{Type: "person", Slug: "solo"}
	if g.OutDegree(n) != 0 || g.InDegree(n) != 0 {
		t.Fatalf("self-edge must be skipped: out=%d in=%d", g.OutDegree(n), g.InDegree(n))
	}
}

// Nodes() is scan order (schema declaration order x sorted glob), never sorted.
func TestGraphNodesKeepScanOrder(t *testing.T) {
	idx := buildIdx(t, impactWS(t))
	g := BuildGraph(idx)
	if len(g.Nodes()) != len(idx.Order) {
		t.Fatalf("want %d nodes, got %d", len(idx.Order), len(g.Nodes()))
	}
	for i, n := range g.Nodes() {
		if n != idx.Order[i] {
			t.Fatalf("node %d: want %v, got %v", i, idx.Order[i], n)
		}
	}
}

// Edges() nests node order outer, adjacency order inner — viz reads it unsorted.
func TestGraphEdgesNesting(t *testing.T) {
	g := BuildGraph(buildIdx(t, parallelWS(t)))
	var got []string
	for _, e := range g.Edges() {
		got = append(got, e.From.ID()+" -"+e.Predicate+"-> "+e.To.ID())
	}
	want := []string{
		"client/pa -related-> client/pb",
		"client/pa -depends_on-> client/pb",
		"client/pb -related-> client/pa",
	}
	if !eqSlices(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// PredicateEdges collapses the multigraph to _predicate_digraph's simple DiGraph.
func TestPredicateEdgesDedup(t *testing.T) {
	ws := freshWS(t)
	recent := []kv{{"created", date("2026-06-01")}, {"updated", date("2026-06-01")}}
	// The same target twice: MultiDiGraph keeps two edges, DiGraph keeps one.
	seed(t, ws, "clients/dup-src.md", append([]kv{
		{"type", "client"}, {"name", "Dup"}, {"depends_on", []any{"dup-dst", "dup-dst"}},
	}, recent...)...)
	seed(t, ws, "clients/dup-dst.md", append([]kv{{"type", "client"}, {"name", "Dst"}}, recent...)...)
	g := BuildGraph(buildIdx(t, ws))
	src := index.Node{Type: "client", Slug: "dup-src"}
	if got := g.OutDegree(src); got != 2 {
		t.Fatalf("multigraph keeps parallel edges: want 2, got %d", got)
	}
	if got := len(g.PredicateEdges("depends_on")); got != 1 {
		t.Fatalf("the predicate digraph collapses them: want 1, got %d", got)
	}
}

// Cycles enumerates the elementary cycles integrity reports.
func TestCyclesOverPredicate(t *testing.T) {
	g := BuildGraph(buildIdx(t, impactWS(t)))
	cycles := Cycles(g, "depends_on")
	if len(cycles) != 1 {
		t.Fatalf("want one cycle, got %v", cycles)
	}
	var ids []string
	for _, n := range cycles[0] {
		ids = append(ids, n.ID())
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	want := []string{"client/node-x", "client/node-y", "client/node-z"}
	if !eqSlices(sorted, want) {
		t.Fatalf("want %v, got %v (unrotated %v)", want, sorted, ids)
	}
	if len(Cycles(g, "related")) != 0 {
		t.Fatal("no `related` cycles exist")
	}
}
