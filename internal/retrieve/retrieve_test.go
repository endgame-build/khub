package retrieve

// The library-level rows behind `khub retrieve`: seeds from a search or from
// ids, the walk's order and per-predicate depth, each cap's trim order, the
// byte cap against the encoder, the reason fields, and the refusals.

import (
	"errors"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/graph"
)

// --- seeds -------------------------------------------------------------------

// A search seeds in rank order, each seed scored and snippeted, and the
// hits past the limit are counted and named.
func TestSearchSeedsRankedAndLimited(t *testing.T) {
	ws := graphWS(t)
	all := mustRetrieve(t, ws, Options{Text: "token", Depth: 0})
	if got := ids(all); len(got) != 3 {
		t.Fatalf("want 3 token hits, got %v", got)
	}
	for _, r := range all.Records {
		if r.Reason.Kind != ReasonSeed || !r.Reason.Scored || r.Reason.Snippet == "" || r.Reason.Depth != 0 {
			t.Fatalf("a search seed carries kind, depth 0, score and snippet: %+v", r.Reason)
		}
	}
	two := mustRetrieve(t, ws, Options{Text: "token", Depth: 0, Limit: ptr(2)})
	if !eqSlices(ids(two), ids(all)[:2]) {
		t.Fatalf("the limit keeps the best-ranked: want %v, got %v", ids(all)[:2], ids(two))
	}
	if two.Omitted.Limit != 1 || !eqSlices(two.Omitted.IDs(), ids(all)[2:]) || !two.Omitted.Truncated() {
		t.Fatalf("the dropped hit is counted and named: %+v", two.Omitted)
	}
	if two.Omitted.Insufficient {
		t.Fatal("a limit is what the caller asked for, never an insufficiency")
	}
	if two.Search == nil || two.Search.Total != 3 || len(two.Search.Hits) != 2 || two.Seeds != 2 {
		t.Fatalf("the search result reports every hit and the seeds kept: %+v", two.Search)
	}
}

// Ids seed in argument order, duplicates folded, every one unless limited.
func TestIDSeedsInArgumentOrder(t *testing.T) {
	ws := graphWS(t)
	res := mustRetrieve(t, ws, Options{IDs: []string{"web", "auth", "web", "note/jwt"}, Depth: 0})
	if want := []string{"part/web", "need/auth", "note/jwt"}; !eqSlices(ids(res), want) {
		t.Fatalf("want %v, got %v", want, ids(res))
	}
	for _, r := range res.Records {
		if r.Reason.Kind != ReasonSeed || r.Reason.Scored {
			t.Fatalf("an id seed has no score: %+v", r.Reason)
		}
	}
	if res.Search != nil {
		t.Fatal("no search ran")
	}
	cut := mustRetrieve(t, ws, Options{IDs: []string{"web", "auth", "jwt"}, Depth: 0, Limit: ptr(1)})
	if !eqSlices(ids(cut), []string{"part/web"}) || cut.Omitted.Limit != 2 || cut.Seeds != 1 ||
		!eqSlices(cut.Omitted.IDs(), []string{"need/auth", "note/jwt"}) {
		t.Fatalf("the limit cuts the ids from the end: %v %+v", ids(cut), cut.Omitted)
	}
}

// An id that does not resolve refuses the whole call, as get does.
func TestUnknownIDRefuses(t *testing.T) {
	_, err := Retrieve(graphWS(t), Options{IDs: []string{"web", "nope"}})
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "lookup_error" {
		t.Fatalf("want lookup_error, got %v", err)
	}
}

// No seed at all is a refusal, not an empty success.
func TestNoSeedRefuses(t *testing.T) {
	_, err := Retrieve(graphWS(t), Options{})
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "no_seed" {
		t.Fatalf("want no_seed, got %v", err)
	}
}

// --- expansion ---------------------------------------------------------------

// The walk is breadth-first in adjacency order: out-edges before in-edges,
// each in insertion order, hop by hop; a node joins once, on first reach,
// and seeds are never re-reached.
func TestExpansionOrderIsBreadthFirstAdjacency(t *testing.T) {
	res := mustRetrieve(t, graphWS(t), Options{IDs: []string{"api"}, Depth: 2})
	want := []string{
		"part/api",  // seed
		"part/db",   // hop 1, out depends_on
		"need/auth", // hop 1, in realized_in
		"part/web",  // hop 1, in depends_on
		"note/jwt",  // hop 1, in affects
	}
	if !eqSlices(ids(res), want) {
		t.Fatalf("want %v, got %v", want, ids(res))
	}
	web := res.Records[3].Reason
	if web.Kind != ReasonExpanded || web.Depth != 1 || web.Predicate != "depends_on" ||
		web.Direction != graph.DirectionIn || web.From != "part/api" {
		t.Fatalf("an expanded record names its first path: %+v", web)
	}
}

// Each predicate is followed to its own depth: depends_on reaches db at hop
// 2 while affects stops after hop 1, and a predicate not named is not walked.
func TestExpandPerPredicateDepth(t *testing.T) {
	ws := graphWS(t)
	res := mustRetrieve(t, ws, Options{IDs: []string{"jwt"}, Depth: 1,
		Expand: []Expansion{{"affects", 1}, {"depends_on", 2}}})
	want := []string{"note/jwt", "part/api", "part/web", "part/db"}
	if !eqSlices(ids(res), want) {
		t.Fatalf("want %v, got %v", want, ids(res))
	}
	if db := res.Records[3].Reason; db.Depth != 2 || db.Predicate != "depends_on" || db.From != "part/api" {
		t.Fatalf("db is reached at hop 2 over depends_on from api: %+v", db)
	}
	only := mustRetrieve(t, ws, Options{IDs: []string{"jwt"}, Expand: []Expansion{{"depends_on", 3}}})
	if !eqSlices(ids(only), []string{"note/jwt"}) {
		t.Fatalf("affects is not named, so nothing is reached: %v", ids(only))
	}
}

// --depth 0 returns the seeds alone; --in and --out restrict the direction.
func TestDepthZeroAndDirection(t *testing.T) {
	ws := graphWS(t)
	if got := ids(mustRetrieve(t, ws, Options{IDs: []string{"api"}, Depth: 0})); !eqSlices(got, []string{"part/api"}) {
		t.Fatalf("depth 0: %v", got)
	}
	out := ids(mustRetrieve(t, ws, Options{IDs: []string{"api"}, Depth: 1, Direction: graph.DirectionOut}))
	if !eqSlices(out, []string{"part/api", "part/db"}) {
		t.Fatalf("--out: %v", out)
	}
	in := ids(mustRetrieve(t, ws, Options{IDs: []string{"api"}, Depth: 1, Direction: graph.DirectionIn}))
	if !eqSlices(in, []string{"part/api", "need/auth", "part/web", "note/jwt"}) {
		t.Fatalf("--in: %v", in)
	}
}

// An unknown predicate refuses before any scan, naming the declared ones.
func TestUnknownExpandPredicateRefuses(t *testing.T) {
	_, err := Retrieve(graphWS(t), Options{IDs: []string{"api"}, Expand: []Expansion{{"nope", 1}}})
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "unknown_predicate" ||
		!strings.Contains(located.Message, "depends_on") {
		t.Fatalf("want unknown_predicate listing the declared names, got %v", err)
	}
}

// --- lifecycle and views -----------------------------------------------------

// Every record carries the title, the flags query reports, the `get` view
// with its edges, and a workspace-relative path.
func TestRecordsCarryViewsAndFlags(t *testing.T) {
	res := mustRetrieve(t, graphWS(t), Options{IDs: []string{"api", "lonely"}, Depth: 0})
	api, lonely := res.Records[0], res.Records[1]
	if api.Title != "API" || api.Orphan || lonely.Title != "Lonely need" || !lonely.Orphan {
		t.Fatalf("titles and orphan flags: %+v %+v", api, lonely)
	}
	// The stored depends_on plus the derived `realizes`; affects and the
	// inbound depends_on declare no inverse, so they are not edges of api.
	if api.View == nil || api.View.Body == "" || len(api.View.Edges) != 2 || api.Path != "parts/api.md" {
		t.Fatalf("the get view with edges and a relative path: %+v", api)
	}
	if e := api.View.Edges[1]; e.Predicate != "realizes" || !e.Derived {
		t.Fatalf("the derived inverse rides along: %+v", api.View.Edges)
	}
}

// --- caps --------------------------------------------------------------------

// MaxEntities trims the deepest first, then the greatest (type, slug) within
// a depth, and the output keeps its order.
func TestMaxEntitiesTrimsPeripheryFirst(t *testing.T) {
	ws := graphWS(t)
	full := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2})
	// auth; api (1); db, web, jwt (2)
	if want := []string{"need/auth", "part/api", "part/db", "part/web", "note/jwt"}; !eqSlices(ids(full), want) {
		t.Fatalf("fixture: want %v, got %v", want, ids(full))
	}
	res := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2, MaxEntities: 3})
	if want := []string{"need/auth", "part/api", "note/jwt"}; !eqSlices(ids(res), want) {
		t.Fatalf("want %v, got %v", want, ids(res))
	}
	if res.Omitted.MaxEntities != 2 || !eqSlices(res.Omitted.IDs(), []string{"part/web", "part/db"}) {
		t.Fatalf("part/web goes before part/db: %+v", res.Omitted)
	}
	if res.Omitted.Insufficient || !res.Omitted.Truncated() {
		t.Fatalf("no seed went: %+v", res.Omitted)
	}
}

// A cap below the seed count trims the least relevant seed first and reports
// insufficiency rather than clipping silently.
func TestCapBelowSeedsIsInsufficient(t *testing.T) {
	res := mustRetrieve(t, graphWS(t), Options{IDs: []string{"web", "auth", "jwt"}, Depth: 1, MaxEntities: 2})
	if want := []string{"part/web", "need/auth"}; !eqSlices(ids(res), want) {
		t.Fatalf("want the first two seeds, got %v", ids(res))
	}
	if named := res.Omitted.IDs(); !res.Omitted.Insufficient || named[len(named)-1] != "note/jwt" {
		t.Fatalf("the last seed is the last to go, and it is an insufficiency: %+v", res.Omitted)
	}
}

// MaxBytes holds the printed document — the encoder's bytes plus the newline
// — under the cap, trims in the same order, and books what went.
func TestMaxBytesHoldsTheDocument(t *testing.T) {
	ws := graphWS(t)
	full := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2})
	fullDoc, err := canon.EncodeCLI(Render(full))
	if err != nil {
		t.Fatal(err)
	}
	// The irreducible floor: every record and every omitted id gone.
	floor := &Result{Omitted: Omitted{MaxBytes: len(full.Records), Insufficient: true}}
	floorDoc, err := canon.EncodeCLI(Render(floor))
	if err != nil {
		t.Fatal(err)
	}
	floorSize := len(floorDoc) + 1
	for _, limit := range []int{len(fullDoc) + 1, len(fullDoc), len(fullDoc) - 200, 900, 500, 200, floorSize, floorSize - 1, 1} {
		res := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2, MaxBytes: limit})
		doc, err := canon.EncodeCLI(Render(res))
		if err != nil {
			t.Fatal(err)
		}
		if limit < floorSize {
			// Below the floor the answer is the floor itself: nothing more can go.
			if len(doc)+1 != floorSize || len(res.Records) != 0 || len(res.Omitted.IDs()) != 0 {
				t.Fatalf("limit %d: want the %d-byte floor, got %d bytes %v", limit, floorSize, len(doc)+1, res.Omitted.IDs())
			}
			continue
		}
		if len(doc)+1 > limit {
			t.Fatalf("limit %d: the document is %d bytes", limit, len(doc)+1)
		}
		if limit == len(fullDoc)+1 && res.Omitted.Truncated() {
			t.Fatalf("limit %d holds the whole document, yet trimmed %+v", limit, res.Omitted)
		}
		if limit <= len(fullDoc) && res.Omitted.MaxBytes == 0 {
			t.Fatalf("limit %d is under the document, yet nothing trimmed", limit)
		}
		if n := len(res.Records) + res.Omitted.MaxBytes; n != len(full.Records) {
			t.Fatalf("limit %d: kept %d + trimmed %d != %d", limit, len(res.Records), res.Omitted.MaxBytes, n)
		}
		// Trimmed in the same order MaxEntities trims: the deepest first.
		if len(res.Records) > 0 && res.Records[0].Node.ID() != "need/auth" {
			t.Fatalf("limit %d: the seed is the last to go, got %v", limit, ids(res))
		}
		if len(res.Records) == 0 && !res.Omitted.Insufficient {
			t.Fatalf("limit %d: everything went, so the seeds did", limit)
		}
	}
}

// --no-body shrinks the bytes the cap counts, so more entities fit.
func TestNoBodyFitsMore(t *testing.T) {
	ws := graphWS(t)
	bare := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2, NoBody: true})
	bareDoc, err := canon.EncodeCLI(Render(bare))
	if err != nil {
		t.Fatal(err)
	}
	// Exactly the bytes the body-less document needs: it fits whole, and the
	// same records with their bodies do not.
	limit := len(bareDoc) + 1
	with := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2, MaxBytes: limit})
	without := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2, MaxBytes: limit, NoBody: true})
	if len(without.Records) != len(bare.Records) || without.Omitted.Truncated() {
		t.Fatalf("the body-less document fits whole: %v %+v", ids(without), without.Omitted)
	}
	if len(with.Records) >= len(without.Records) {
		t.Fatalf("with bodies %d, without %d", len(with.Records), len(without.Records))
	}
	for _, r := range without.Records {
		if _, has := renderRecord(r, without.Body).Get("body"); has {
			t.Fatal("no body under NoBody")
		}
	}
}

// A cap between the floor and the envelope with every omitted id shrinks the
// id list from the tail, so the document still fits and the counts stay.
func TestMaxBytesShrinksOmittedIDs(t *testing.T) {
	ws := graphWS(t)
	gone := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2, MaxBytes: 1})
	floorDoc, err := canon.EncodeCLI(Render(gone))
	if err != nil {
		t.Fatal(err)
	}
	// Room for the floor plus two ids and a little, not for all five.
	limit := len(floorDoc) + 1 + 2*len(`"part/web", `) + 4
	res := mustRetrieve(t, ws, Options{IDs: []string{"auth"}, Depth: 2, MaxBytes: limit})
	doc, err := canon.EncodeCLI(Render(res))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc)+1 > limit || len(res.Records) != 0 || res.Omitted.MaxBytes != 5 {
		t.Fatalf("limit %d: %d bytes, %d records, %+v", limit, len(doc)+1, len(res.Records), res.Omitted)
	}
	named := res.Omitted.IDs()
	if len(named) == 0 || len(named) >= 5 || !eqSlices(named, res.Omitted.MaxBytesIDs[:len(named)]) {
		t.Fatalf("the list shrinks from the tail: %v of %v", named, res.Omitted.MaxBytesIDs)
	}
}

// The omitted id budget goes to what the caps trimmed first — MaxEntities,
// then MaxBytes — and to what Limit dropped only while room remains.
func TestOmittedIDsNameCapTrimsFirst(t *testing.T) {
	ws := wsFromSchema(t, preset)
	for i := 0; i < MaxOmittedIDs+5; i++ {
		seed(t, ws, "parts/p"+string(rune('a'+i/26))+string(rune('a'+i%26))+".md", "token\n",
			kv{"type", "part"}, kv{"title", "Part"}, kv{"depends_on", []any{"hub"}})
	}
	seed(t, ws, "parts/hub.md", "the hub\n", kv{"type", "part"}, kv{"title", "Hub"})
	res := mustRetrieve(t, ws, Options{Text: "token", Depth: 1, Limit: ptr(2), MaxEntities: 2})
	if res.Omitted.Limit != MaxOmittedIDs+3 || res.Omitted.MaxEntities != 1 {
		t.Fatalf("fixture: %+v", res.Omitted)
	}
	named := res.Omitted.IDs()
	if len(named) != MaxOmittedIDs || named[0] != "part/hub" || !eqSlices(named[1:], res.Omitted.LimitIDs[:MaxOmittedIDs-1]) {
		t.Fatalf("the trimmed neighbour is named first, then the dropped hits: %v", named)
	}
}

// The omitted id list is bounded; the counts are not.
func TestOmittedIDsAreBounded(t *testing.T) {
	ws := wsFromSchema(t, preset)
	for i := 0; i < MaxOmittedIDs+10; i++ {
		seed(t, ws, "parts/p"+string(rune('a'+i/26))+string(rune('a'+i%26))+".md", "token\n",
			kv{"type", "part"}, kv{"title", "Part"})
	}
	res := mustRetrieve(t, ws, Options{Text: "token", Depth: 0, Limit: ptr(0)})
	if res.Omitted.Limit != MaxOmittedIDs+10 || len(res.Omitted.IDs()) != MaxOmittedIDs {
		t.Fatalf("want %d counted and %d named, got %+v", MaxOmittedIDs+10, MaxOmittedIDs, res.Omitted)
	}
}

// --- rendering ---------------------------------------------------------------

// The document's key order and the record's are contract.
func TestRenderKeyOrder(t *testing.T) {
	res := mustRetrieve(t, graphWS(t), Options{Text: "token", Depth: 1, Limit: ptr(1)})
	doc := Render(res)
	if want := []string{"count", "entities", "omitted"}; !eqSlices(doc.Keys(), want) {
		t.Fatalf("want %v, got %v", want, doc.Keys())
	}
	want := []string{"id", "type", "slug", "title", "draft", "orphan", "stale", "reason", "path", "frontmatter", "body", "edges"}
	if got := renderRecord(res.Records[0], true).Keys(); !eqSlices(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	seedKeys := renderReason(res.Records[0].Reason).Keys()
	if !eqSlices(seedKeys, []string{"kind", "depth", "score", "snippet"}) {
		t.Fatalf("seed reason keys: %v", seedKeys)
	}
	expandedKeys := renderReason(res.Records[1].Reason).Keys()
	if !eqSlices(expandedKeys, []string{"kind", "depth", "predicate", "direction", "from"}) {
		t.Fatalf("expanded reason keys: %v", expandedKeys)
	}
	omitted, _ := doc.Get("omitted")
	if got := omitted.(interface{ Keys() []string }).Keys(); !eqSlices(got,
		[]string{"limit", "max_entities", "max_bytes", "ids", "truncated", "insufficient"}) {
		t.Fatalf("omitted keys: %v", got)
	}
}
