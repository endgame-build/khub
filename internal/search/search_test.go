package search

// Ports the library-level rows of tests/test_search.py (TS-SRCH-001):
// body-prose matching (the sanctioned exception to QRY-001), title matching,
// BM25 ranking, --type narrowing, --limit, empty-result success, the malformed
// MATCH located error, and crash containment on a malformed entity file. The
// projection is in-memory per invocation — there is no cache file to assert on,
// only results.

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/query"
)

// sws is the search fixture: distinctive prose in file- and folder-layout bodies.
func sws(t *testing.T) string {
	t.Helper()
	ws := freshWS(t)
	seed(t, ws, "clients/initech.md",
		kv{"type", "client"}, kv{"name", "Initech"}, kv{"created", date("2026-06-01")})
	// file layout with a title and a body-only word ("mainframe" lives nowhere else)
	seedRaw(t, ws, "clients/acme.md",
		"---\ntype: client\nname: Acme\ntitle: Acme Corp\ncreated: 2026-06-01\n---\n"+
			"Legacy assessment of the mainframe estate before any rewrite.\n")
	// folder layout, term-dense for the ranking test
	seedRaw(t, ws, "opportunities/op-modern/_index.md",
		"---\ntype: opportunity\nstage: prospect\ncreated: 2026-06-01\n---\n"+
			"Modernization, modernization, modernization.\n")
	// mentions the same term once, in a longer body (should rank below op-modern)
	seedRaw(t, ws, "clients/sparse.md",
		"---\ntype: client\nname: Sparse\ncreated: 2026-06-01\n---\n"+
			"A long engagement note that mentions modernization exactly once while "+
			"otherwise talking about invoicing, staffing, travel, and scheduling.\n")
	// a malformed file (unterminated frontmatter) must not brick the search
	seedRaw(t, ws, "clients/broken.md", "---\ntype: client\nunclosed: [\n")
	return ws
}

func mustSearch(t *testing.T, root, text string, type_ *string, limit int) []Hit {
	t.Helper()
	got, err := Search(root, text, Options{Type: type_, Limit: limit})
	if err != nil {
		t.Fatalf("Search(%q): %v", text, err)
	}
	return got.Hits
}

func hitSlugs(hs []Hit) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.Slug)
	}
	return out
}

func eq(a, b []string) bool {
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

// A body-only word finds its entity — the sanctioned exception to QRY-001.
func TestSearchMatchesBodyProse(t *testing.T) {
	hits := mustSearch(t, sws(t), "mainframe", nil, 20)
	if len(hits) != 1 || hits[0].Type != "client" || hits[0].Slug != "acme" {
		t.Fatalf("got %+v", hits)
	}
	if hits[0].Title != "Acme Corp" {
		t.Fatalf("title: %q", hits[0].Title)
	}
	if hits[0].Path != "clients/acme.md" {
		t.Fatalf("path: %q", hits[0].Path)
	}
	if !strings.Contains(hits[0].Snippet, "mainframe") {
		t.Fatalf("snippet: %q", hits[0].Snippet)
	}
	if hits[0].Locator != "" {
		t.Fatalf("a per-item record carries no locator, got %q", hits[0].Locator)
	}
}

// A title-only word matches; the title column is indexed alongside the body.
func TestSearchMatchesTitle(t *testing.T) {
	if got := hitSlugs(mustSearch(t, sws(t), "corp", nil, 20)); !eq(got, []string{"acme"}) {
		t.Fatalf("got %v", got)
	}
}

// BM25 puts the term-dense folder-layout body above the one-mention body.
func TestSearchRanksDenserMatchFirst(t *testing.T) {
	hits := mustSearch(t, sws(t), "modernization", nil, 20)
	if got := hitSlugs(hits); !eq(got, []string{"op-modern", "sparse"}) {
		t.Fatalf("got %v", got)
	}
	if hits[0].Score > hits[1].Score { // lower BM25 = better
		t.Fatalf("scores: %v %v", hits[0].Score, hits[1].Score)
	}
}

// --type narrows the hits; an unknown type is the same located error as query.
func TestSearchTypeFilter(t *testing.T) {
	ws := sws(t)
	if got := hitSlugs(mustSearch(t, ws, "modernization", ptr("client"), 20)); !eq(got, []string{"sparse"}) {
		t.Fatalf("got %v", got)
	}
	_, err := Search(ws, "modernization", Options{Type: ptr("zzz"), Limit: 20})
	var located *errs.Located
	ok := errors.As(err, &located)
	if !ok || located.Code != "unknown_type" {
		t.Fatalf("want unknown_type, got %#v", err)
	}
	if !strings.HasPrefix(located.Message, "No type 'zzz' in the firm-ops schema. Known types: ") {
		t.Fatalf("message drift: %q", located.Message)
	}
}

func TestSearchLimit(t *testing.T) {
	ws := sws(t)
	if got := mustSearch(t, ws, "modernization", nil, 1); len(got) != 1 {
		t.Fatalf("got %v", hitSlugs(got))
	}
	// a zero/negative cap returns nothing — SQLite's "LIMIT -1 = unbounded" never leaks
	if got := mustSearch(t, ws, "modernization", nil, 0); len(got) != 0 {
		t.Fatalf("limit 0: got %v", hitSlugs(got))
	}
	if got := mustSearch(t, ws, "modernization", nil, -1); len(got) != 0 {
		t.Fatalf("limit -1: got %v", hitSlugs(got))
	}
}

// No match returns an empty collection, never raising (QRY-002).
func TestSearchEmptyIsSuccess(t *testing.T) {
	got := mustSearch(t, sws(t), "chrysanthemum", nil, 20)
	if got == nil || len(got) != 0 {
		t.Fatalf("want an empty non-nil slice, got %#v", got)
	}
}

// A malformed FTS5 expression raises a located error, not a driver error.
func TestSearchBadMatchSyntaxIsLocated(t *testing.T) {
	_, err := Search(sws(t), `mainframe AND "`, Options{Limit: 20})
	var located *errs.Located
	ok := errors.As(err, &located)
	if !ok || located.Code != "bad_search_query" {
		t.Fatalf("want bad_search_query, got %#v", err)
	}
	if !strings.HasPrefix(located.Message, `Invalid search query 'mainframe AND "' (`) ||
		!strings.HasSuffix(located.Message, `). Quote phrases: '"exact phrase"'`) {
		t.Fatalf("message drift: %q", located.Message)
	}
	if located.Target != `mainframe AND "` {
		t.Fatalf("want the raw text located, got %q", located.Target)
	}
}

// The embedded detail must read like CPython's str(OperationalError) — bare
// sqlite3_errmsg(), with neither the driver's "sqlite3: " prefix nor its
// result-code text. These four are the messages CPython 3.53.x emits verbatim.
func TestSearchBadQueryDetailMatchesCPython(t *testing.T) {
	ws := sws(t)
	for _, tc := range []struct{ q, detail string }{
		{`mainframe AND "`, "unterminated string"},
		{`"unclosed`, "unterminated string"},
		{`NEAR(a`, `fts5: syntax error near ""`},
		{`a OR OR b`, `fts5: syntax error near "OR"`},
	} {
		_, err := Search(ws, tc.q, Options{Limit: 20})
		var located *errs.Located
		ok := errors.As(err, &located)
		if !ok {
			t.Fatalf("%q: want a located error, got %#v", tc.q, err)
		}
		want := "Invalid search query '" + tc.q + "' (" + tc.detail + "). Quote phrases: '\"exact phrase\"'"
		if located.Message != want {
			t.Fatalf("%q:\n want %q\n  got %q", tc.q, want, located.Message)
		}
	}
}

// The malformed clients/broken.md is not an entity: skipped, never indexed.
func TestSearchSurvivesMalformedFile(t *testing.T) {
	for _, h := range mustSearch(t, sws(t), "unclosed OR client OR mainframe", nil, 20) {
		if h.Slug == "broken" {
			t.Fatal("a malformed file must never be indexed")
		}
	}
}

// Raw FTS5 MATCH syntax passes through: phrases, OR, prefix*, NEAR.
func TestSearchPassesRawMatchSyntax(t *testing.T) {
	ws := sws(t)
	if got := hitSlugs(mustSearch(t, ws, `"mainframe estate"`, nil, 20)); !eq(got, []string{"acme"}) {
		t.Fatalf("phrase: %v", got)
	}
	if got := hitSlugs(mustSearch(t, ws, "mainfr*", nil, 20)); !eq(got, []string{"acme"}) {
		t.Fatalf("prefix: %v", got)
	}
	or := hitSlugs(mustSearch(t, ws, "mainframe OR chrysanthemum", nil, 20))
	if !eq(or, []string{"acme"}) {
		t.Fatalf("OR: %v", or)
	}
	near := hitSlugs(mustSearch(t, ws, "NEAR(mainframe estate, 2)", nil, 20))
	if !eq(near, []string{"acme"}) {
		t.Fatalf("NEAR: %v", near)
	}
}

// fts_body folds scalar string attributes in, so an entity is findable by a
// frontmatter VALUE — but type/title/name are excluded (they are noise, or
// already in the title column).
func TestSearchIndexesScalarAttributeValues(t *testing.T) {
	ws := freshWS(t)
	seedRaw(t, ws, "clients/valued.md",
		"---\ntype: client\nname: Valued\nwebsite: https://octopus.example\ncreated: 2026-06-01\n---\n")
	if got := hitSlugs(mustSearch(t, ws, "octopus", nil, 20)); !eq(got, []string{"valued"}) {
		t.Fatalf("an attribute value must be searchable, got %v", got)
	}
	// `name` populates the title column, not the body — searching it still works.
	if got := hitSlugs(mustSearch(t, ws, "Valued", nil, 20)); !eq(got, []string{"valued"}) {
		t.Fatalf("got %v", got)
	}
}

// A collection row carries locator (path#slug) and its path is the shared file.
func TestSearchCollectionRowCarriesLocator(t *testing.T) {
	ws := wsFromSchema(t, `
version: "0.1.0"
ontology:
  entities:
    widget: {}
storage:
  widget: { layout: collection, format: yaml, path: inventory/widgets.yaml }
`)
	seedRaw(t, ws, "inventory/widgets.yaml",
		"w1:\n  type: widget\n  title: First Widget\n  body: a chrysanthemum lives here\n")
	hits := mustSearch(t, ws, "chrysanthemum", nil, 20)
	if len(hits) != 1 {
		t.Fatalf("got %+v", hits)
	}
	if hits[0].Path != "inventory/widgets.yaml" {
		t.Fatalf("path: %q", hits[0].Path)
	}
	if hits[0].Locator != "inventory/widgets.yaml#w1" {
		t.Fatalf("locator: %q", hits[0].Locator)
	}
	if hits[0].Title != "First Widget" {
		t.Fatalf("title: %q", hits[0].Title)
	}
}

// Rows are inserted in sorted-node order, so a bm25 tie breaks on that rowid.
func TestSearchTiesBreakOnInsertionOrder(t *testing.T) {
	ws := freshWS(t)
	for _, slug := range []string{"zeta", "alpha", "mid"} {
		seedRaw(t, ws, "clients/"+slug+".md",
			"---\ntype: client\nname: "+slug+"\ncreated: 2026-06-01\n---\nidentical tiebreaker prose here.\n")
	}
	got := hitSlugs(mustSearch(t, ws, "tiebreaker", nil, 20))
	if !eq(got, []string{"alpha", "mid", "zeta"}) {
		t.Fatalf("ties must follow sorted-node insertion order, got %v", got)
	}
}

// Two different judgements about the same body (kb
// `test_the_search_index_drops_hints_and_keeps_code`). A hint is the template's
// words, so indexing it made every fresh entity a strong hit for its own
// scaffold. A fenced block is the author's — a mermaid diagram names the
// components and a bash block names the command, and finding those is what
// `khub search` is for.
func TestSearchDropsHintCommentsAndKeepsCode(t *testing.T) {
	ws := freshWS(t)
	seedRaw(t, ws, "clients/scaffolded.md",
		"---\ntype: client\nname: Scaffolded\ncreated: 2026-06-01\n---\n"+
			"<!-- what it is responsible for -->\n\n```bash\ndocker compose up\n```\n")
	if got := hitSlugs(mustSearch(t, ws, "docker", nil, 20)); !eq(got, []string{"scaffolded"}) {
		t.Fatalf("a fenced block is not indexed: %v", got)
	}
	if got := hitSlugs(mustSearch(t, ws, "responsible", nil, 20)); len(got) != 0 {
		t.Fatalf("a hint comment is indexed: %v", got)
	}
}

// --plain quotes every distinct word, prefix-matches words of three runes and
// more, and ORs them, so punctuation never reaches FTS5 as syntax.
func TestPlainMatch(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"encrypt customer data", `"encrypt"* OR "customer"* OR "data"*`},
		{"login rate-limit?", `"login"* OR "rate"* OR "limit"*`},
		{"a to", `"a" OR "to"`},
		{"Rate rate RATE", `"Rate"*`},
		{"OR NEAR AND", `"OR" OR "NEAR"* OR "AND"*`},
		{`col:val "quoted" x* ^y -z`, `"col"* OR "val"* OR "quoted"* OR "x" OR "y" OR "z"`},
		{"café café", `"café"* OR "cafe` + "́" + `"*`},
		{"हिन्दी", `"हिन्दी"*`},
		{"((( ))) -- ^ *", ""},
		{"", ""},
	} {
		if got := plainJoined(tc.in); got != tc.want {
			t.Errorf("plainTerms(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	words := make([]string, 100)
	for i := range words {
		words[i] = fmt.Sprintf("w%03d", i)
	}
	if terms, capped := plainTerms(strings.Join(words, " ")); len(terms) != MaxPlainTerms || !capped {
		t.Errorf("a 100-word query kept %d terms (capped %v), want %d", len(terms), capped, MaxPlainTerms)
	}
}

func plainJoined(text string) string {
	terms, _ := plainTerms(text)
	return strings.Join(terms, " OR ")
}

// No plain input is a syntax error, whatever punctuation it carries.
func TestPlainNeverRaisesASyntaxError(t *testing.T) {
	ws := sws(t)
	for _, q := range []string{`"`, `*`, `(((`, `)`, `^x`, `-x`, `a:b`, `NEAR(`, `x AND`, `'`, `{a b}`, `+`, "́", "  "} {
		if _, err := Search(ws, q, Options{Limit: 20, Plain: true}); err != nil {
			t.Errorf("plain %q: %v", q, err)
		}
	}
}

// Hits carry the verdicts query reports and per-predicate edge counts:
// out-edges by predicate, in-edges by source type and predicate.
func TestSearchHitsCarryFlagsAndEdgeCounts(t *testing.T) {
	ws := wsFromPreset(t, "build-hub")
	seedRaw(t, ws, "knowledge/components/cmp-api.md",
		"---\ntype: component\ntitle: Gateway API\nupdated: 2025-01-01\n---\n")
	seedRaw(t, ws, "knowledge/requirements/req-a.md",
		"---\ntype: requirement\ntitle: Gateway auth\nrealized_in: [cmp-api]\n---\n")
	seedRaw(t, ws, "knowledge/requirements/req-b.md",
		"---\ntype: requirement\ntitle: Gateway limits\ndraft: true\nrealized_in: [cmp-api]\n---\n")
	seedRaw(t, ws, "knowledge/requirements/req-lone.md",
		"---\ntype: requirement\ntitle: Gateway lone\n---\n")
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	res, err := Search(ws, "gateway", Options{Limit: 20, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	bySlug := map[string]Hit{}
	for _, h := range res.Hits {
		bySlug[h.Slug] = h
	}
	api, a, b, lone := bySlug["cmp-api"], bySlug["req-a"], bySlug["req-b"], bySlug["req-lone"]
	if !api.Stale || a.Stale {
		t.Errorf("stale: cmp-api %v (updated a year ago), req-a %v (undated)", api.Stale, a.Stale)
	}
	if !b.Draft || a.Draft {
		t.Errorf("draft: req-b %v, req-a %v", b.Draft, a.Draft)
	}
	if !lone.Orphan || a.Orphan || api.Orphan {
		t.Errorf("orphan: req-lone %v, req-a %v, cmp-api %v", lone.Orphan, a.Orphan, api.Orphan)
	}
	if want := []EdgeCount{{"requirement.realized_in", 2}}; !reflect.DeepEqual(api.In, want) || api.Out != nil {
		t.Errorf("cmp-api edges: in %v out %v", api.In, api.Out)
	}
	if want := []EdgeCount{{"realized_in", 1}}; !reflect.DeepEqual(a.Out, want) || a.In != nil {
		t.Errorf("req-a edges: out %v in %v", a.Out, a.In)
	}
	if lone.Out != nil || lone.In != nil {
		t.Errorf("req-lone edges: out %v in %v", lone.Out, lone.In)
	}
}

// search and query judge draft, orphan and stale the same way, for every hit.
func TestSearchFlagsAgreeWithQuery(t *testing.T) {
	ws := relevanceWS(t)
	seedRaw(t, ws, "knowledge/components/cmp-stale-probe.md",
		"---\ntype: component\ntitle: Probe\ndraft: true\nupdated: 2024-01-01\n---\nprobe\n")
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	matches, err := query.Query(ws, query.Filters{}, now)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]query.Match{}
	for _, m := range matches {
		want[m.Type+"/"+m.Slug] = m
	}
	res, err := Search(ws, "probe OR the OR a", Options{Limit: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	hits := res.Hits
	if len(hits) < 10 {
		t.Fatalf("only %d hits; the comparison needs a spread", len(hits))
	}
	for _, h := range hits {
		m := want[h.Type+"/"+h.Slug]
		if h.Draft != m.Draft || h.Orphan != m.Orphan || h.Stale != m.Stale {
			t.Errorf("%s/%s: search (draft %v orphan %v stale %v), query (draft %v orphan %v stale %v)",
				h.Type, h.Slug, h.Draft, h.Orphan, h.Stale, m.Draft, m.Orphan, m.Stale)
		}
	}
}

// A result reports every match before the limit, how many entities were
// searched, and the workspace's malformed files. Plain hits carry match
// shares: a hit holding every word scores 1, and a word missing from the
// title lowers title_match below match.
func TestSearchResultTotalsAndShares(t *testing.T) {
	ws := relevanceWS(t)
	res, err := Search(ws, "encrypt customer sensitive payload", Options{Limit: 2, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 || res.Total != 10 || res.Searched != 41 || res.Malformed != 0 || res.Capped {
		t.Fatalf("hits %d total %d searched %d malformed %d capped %v",
			len(res.Hits), res.Total, res.Searched, res.Malformed, res.Capped)
	}
	top, next := res.Hits[0], res.Hits[1]
	if top.Match != 1 || !(top.TitleMatch > 0 && top.TitleMatch < 1) {
		t.Errorf("top hit %s: match %v title_match %v", top.Slug, top.Match, top.TitleMatch)
	}
	if next.Match >= top.Match {
		t.Errorf("second hit %s matches %v, top %v", next.Slug, next.Match, top.Match)
	}

	raw, err := Search(ws, "encryption", Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range raw.Hits {
		if h.Match != 0 || h.TitleMatch != 0 {
			t.Errorf("raw hit %s carries shares", h.Slug)
		}
	}

	words := make([]string, 100)
	for i := range words {
		words[i] = fmt.Sprintf("w%03d", i)
	}
	long, err := Search(ws, strings.Join(words, " "), Options{Limit: 20, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if !long.Capped {
		t.Error("a 100-word plain query is not reported capped")
	}

	seedRaw(t, ws, "knowledge/components/cmp-broken.md", "---\nkey: [unclosed\n---\n")
	broken, err := Search(ws, "kiosk", Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if broken.Malformed != 1 {
		t.Errorf("malformed = %d, want 1", broken.Malformed)
	}
}

// Match shares cover every hit when the hits outnumber one rowid batch.
func TestPlainSharesSpanRowidBatches(t *testing.T) {
	ws := freshWS(t)
	const n = 2*rowidBatch + 100
	for i := range n {
		seedRaw(t, ws, fmt.Sprintf("clients/c%04d.md", i), "---\ntype: client\nname: Walrus\n---\n")
	}
	res, err := Search(ws, "walrus", Options{Limit: 5000, Plain: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != n {
		t.Fatalf("hits = %d, want %d", len(res.Hits), n)
	}
	for _, h := range res.Hits {
		if h.Match != 1 || h.TitleMatch != 1 {
			t.Fatalf("%s: match %v title_match %v, want 1 and 1", h.Slug, h.Match, h.TitleMatch)
		}
	}
}
