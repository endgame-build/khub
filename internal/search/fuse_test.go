package search

// Reciprocal rank fusion over several query groups (SearchGroups, fuse):
// the sum, the tie-break, independence from group order, and that one
// distinct group is exactly Search.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

// rrf sums 1/(rrfK + r) over ranks in the order given, as fuse adds them
// (a constant expression would fold exactly and differ by an ulp).
func rrf(ranks ...int) float64 {
	sum := 0.0
	for _, r := range ranks {
		sum += 1 / float64(rrfK+r)
	}
	return sum
}

func hit(type_, slug string, score float64) Hit {
	return Hit{Type: type_, Slug: slug, Title: slug, Score: score, Snippet: slug + " via " + type_}
}

// A hit at rank r adds 1/(60 + r); the sum is exact (never rounded, so a
// real difference is never a tie) and Groups counts the rankings that held
// the entity.
func TestFuseSumsReciprocalRanks(t *testing.T) {
	a := []Hit{hit("c", "one", -3), hit("c", "two", -2), hit("c", "three", -1)}
	b := []Hit{hit("c", "three", -5), hit("c", "one", -1)}
	got := fuse([][]Hit{a, b})
	want := map[string]struct {
		fused  float64
		groups int
	}{
		"one":   {rrf(1, 2), 2},
		"three": {rrf(3, 1), 2},
		"two":   {rrf(2), 1},
	}
	if len(got) != len(want) {
		t.Fatalf("fused %d hits, want %d: %+v", len(got), len(want), got)
	}
	for _, h := range got {
		w := want[h.Slug]
		if h.Fused != w.fused || h.Groups != w.groups {
			t.Errorf("%s: fused %v groups %d, want %v %d", h.Slug, h.Fused, h.Groups, w.fused, w.groups)
		}
	}
	if s := hitSlugs(got); !reflect.DeepEqual(s, []string{"one", "three", "two"}) {
		t.Errorf("order %v, want best fused first", s)
	}
	if rrfK != 60 {
		t.Errorf("rrfK = %d, want the usual 60", rrfK)
	}
}

// Equal fused scores break on type, then slug, whatever the rankings' own
// order was.
func TestFuseTieBreaksOnTypeSlug(t *testing.T) {
	a := []Hit{hit("z", "b", -9), hit("a", "z", -1)}
	b := []Hit{hit("a", "b", -9), hit("z", "a", -1)}
	got := fuse([][]Hit{a, b})
	var ids []string
	for _, h := range got {
		ids = append(ids, h.Type+"/"+h.Slug)
	}
	if want := []string{"a/b", "z/b", "a/z", "z/a"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("order %v, want %v", ids, want)
	}
}

// The record a fused hit carries is the one from the group where it ranks
// best — never the lower BM25, which a longer query inflates — with the
// earlier ranking winning a tie; and the fusion does not depend on group
// order.
func TestFuseOrderIndependentAndKeepsBestRankedRecord(t *testing.T) {
	// "two" is rank 2 in a with the better BM25, rank 1 in b.
	a := []Hit{hit("c", "one", -9), {Type: "c", Slug: "two", Score: -8, Snippet: "from a"}}
	b := []Hit{{Type: "c", Slug: "two", Score: -1, Snippet: "from b"}, hit("c", "three", -1)}
	ab, ba := fuse([][]Hit{a, b}), fuse([][]Hit{b, a})
	for i := range ab {
		ab[i].group, ba[i].group = 0, 0 // the ranking index is the one thing that may differ
	}
	if !reflect.DeepEqual(ab, ba) {
		t.Errorf("fuse(a, b) != fuse(b, a):\n%+v\n%+v", ab, ba)
	}
	for _, h := range ab {
		if h.Slug == "two" && (h.Score != -1 || h.Snippet != "from b" || h.rank != 1) {
			t.Errorf("two carries %+v, want the rank-1 record from b", h)
		}
	}
	// Rank 1 in both: the earlier ranking's record stays.
	tie := fuse([][]Hit{{{Type: "c", Slug: "x", Snippet: "first"}}, {{Type: "c", Slug: "x", Snippet: "second"}}})
	if tie[0].Snippet != "first" || tie[0].Groups != 2 {
		t.Errorf("tie carries %+v, want the first ranking's record", tie[0])
	}
	if got := fuse(nil); len(got) != 0 || got == nil {
		t.Errorf("fuse(nil) = %#v, want an empty, non-nil list", got)
	}
}

// One distinct group is Search (plus Groups), bytes and all; a repeated
// group counts once, judged on its words rather than its bytes; the order
// of the groups never shapes the result.
func TestSearchGroupsSingleIsSearch(t *testing.T) {
	ws := sws(t)
	single, err := Search(ws, "modernization", Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	single.Groups = 1
	for _, texts := range [][]string{
		{"modernization"}, {"modernization", "modernization"}, {"modernization", " modernization "},
	} {
		got, err := SearchGroups(ws, texts, Options{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, single) {
			t.Errorf("SearchGroups(%q) differs from Search:\n%+v\n%+v", texts, got, single)
		}
	}
	plain, err := Search(ws, "Modernization  estate", Options{Limit: 20, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	plain.Groups = 1
	for _, texts := range [][]string{
		{"Modernization  estate", "modernization estate"}, {"Modernization  estate", "((("}, {"Modernization  estate", "modernization, ESTATE!"},
	} {
		got, err := SearchGroups(ws, texts, Options{Limit: 20, Plain: true})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, plain) {
			t.Errorf("plain SearchGroups(%q) differs from Search:\n%+v\n%+v", texts, got, plain)
		}
	}
	if none, err := SearchGroups(ws, []string{"(((", "!!!"}, Options{Limit: 20, Plain: true}); err != nil || none.Groups != 0 || none.Total != 0 {
		t.Errorf("two wordless plain groups: %+v, %v; want no groups and no hits", none, err)
	}
	for _, tc := range []struct{ a, b string }{{"a  b", "a b"}, {"a b", " a b "}} {
		if groupKey(tc.a, false) != groupKey(tc.b, false) {
			t.Errorf("raw %q and %q are not one group", tc.a, tc.b)
		}
	}
	if groupKey("b a", false) == groupKey("a b", false) || groupKey("b a", true) == groupKey("a b", true) {
		t.Error("word order should make two groups")
	}
	ab, err := SearchGroups(ws, []string{"modernization", "mainframe"}, Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	ba, err := SearchGroups(ws, []string{"mainframe", "modernization"}, Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ab, ba) {
		t.Errorf("group order changed the result:\n%+v\n%+v", ab, ba)
	}
}

// The fused set is the union of the groups' matches; a hit in both groups
// outranks one in a single group; the limit cuts the fused list and Total
// counts the whole of it.
func TestSearchGroupsFusesAndLimitsAfter(t *testing.T) {
	ws := sws(t)
	// "modernization" hits op-modern and sparse; "mainframe" hits acme only;
	// "invoicing" hits sparse only, so sparse is in two groups.
	res, err := SearchGroups(ws, []string{"modernization", "mainframe", "invoicing"}, Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hitSlugs(res.Hits), []string{"sparse", "acme", "op-modern"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fused order %v, want %v", got, want)
	}
	if res.Total != 3 || res.Searched != 4 {
		t.Errorf("Total %d Searched %d, want 3 and 4", res.Total, res.Searched)
	}
	sparse := res.Hits[0]
	if sparse.Groups != 2 || sparse.Fused != rrf(1, 2) || res.Groups != 3 {
		t.Errorf("sparse: groups %d fused %v (result groups %d)", sparse.Groups, sparse.Fused, res.Groups)
	}
	if res.Hits[1].Groups != 1 || res.Hits[1].Fused != rrf(1) {
		t.Errorf("acme: groups %d fused %v", res.Hits[1].Groups, res.Hits[1].Fused)
	}

	cut, err := SearchGroups(ws, []string{"modernization", "mainframe", "invoicing"}, Options{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := hitSlugs(cut.Hits); !reflect.DeepEqual(got, []string{"sparse"}) || cut.Total != 3 {
		t.Errorf("limit 1: hits %v total %d, want [sparse] and 3", got, cut.Total)
	}
	none, err := SearchGroups(ws, []string{"modernization", "mainframe"}, Options{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Hits) != 0 || none.Total != 3 {
		t.Errorf("limit 0: %d hits total %d, want none and 3", len(none.Hits), none.Total)
	}
}

// --plain applies per group: each group's words are its own terms, and a
// fused hit's match shares judge it against the group where it ranks best.
func TestSearchGroupsPlainPerGroup(t *testing.T) {
	ws := sws(t)
	// sparse is rank 1 for "invoicing travel" (both words, only there) and
	// rank 2 for "modernization", so its shares come from the first group.
	res, err := SearchGroups(ws, []string{"modernization", "invoicing travel"}, Options{Limit: 20, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hitSlugs(res.Hits), []string{"sparse", "op-modern"}; !reflect.DeepEqual(got, want) {
		t.Errorf("plain fused order %v, want %v", got, want)
	}
	if h := res.Hits[0]; h.Groups != 2 || h.Match != 1 || h.TitleMatch != 0 || h.Snippet == "" {
		t.Errorf("sparse: %+v, want match 1 from the group it leads", h)
	}
	if h := res.Hits[1]; h.Groups != 1 || h.Match != 1 {
		t.Errorf("op-modern: %+v", h)
	}
	if res.Capped {
		t.Error("Capped set without a long group")
	}
	// A type filter and an unknown type behave as in Search.
	if _, err := SearchGroups(ws, []string{"a", "b"}, Options{Type: ptr("nope"), Limit: 20}); err == nil {
		t.Error("unknown type accepted")
	}
}

// A malformed group is the same located refusal Search raises, naming that
// group's text.
func TestSearchGroupsMalformedGroup(t *testing.T) {
	ws := sws(t)
	_, err := SearchGroups(ws, []string{"modernization", "((("}, Options{Limit: 20})
	var loc *errs.Located
	if !errors.As(err, &loc) || loc.Code != "bad_search_query" || loc.Target != "(((" {
		t.Fatalf("err = %v, want bad_search_query on '((('", err)
	}
}
