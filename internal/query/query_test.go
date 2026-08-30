package query

// Ports the library-level rows of tests/test_query.py (TS-QRY-001): AND
// semantics, --has/--missing gap finding, the unknown-field filter error,
// empty-result success, orphan/stale annotation and filters, draft scoping, and
// the derived-inverse predicate handling added in 0.11.0.

import (
	"sort"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/errs"
)

var now = time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)

// qws is the query fixture: opportunities across stages, a project gap, an
// orphan, a stale entity, and a tagged client.
func qws(t *testing.T) string {
	t.Helper()
	ws := freshWS(t)
	recent := []kv{{"created", date("2026-06-01")}, {"updated", date("2026-06-01")}}
	seed(t, ws, "clients/initech.md", append([]kv{{"type", "client"}, {"name", "Initech"}}, recent...)...)
	seed(t, ws, "identity/team/noor.md",
		kv{"type", "person"}, kv{"name", "Noor"}, kv{"role", "partner"}, kv{"created", date("2026-06-01")})
	for _, op := range [][2]string{
		{"op-prospect", "prospect"}, {"op-proposal", "proposal-sent"}, {"op-won", "won"},
	} {
		seed(t, ws, "opportunities/"+op[0]+"/_index.md", append([]kv{
			{"type", "opportunity"}, {"stage", op[1]}, {"client", "initech"}, {"owner", "noor"},
		}, recent...)...)
	}
	// one active project whose owner resolves (with a bool attr + a many-valued
	// relation for the coercion tests); one draft project missing owner (the gap)
	seed(t, ws, "projects/has-owner/_index.md", append([]kv{
		{"type", "project"}, {"client", "initech"}, {"owner", "noor"},
		{"active", true}, {"team", []any{"noor"}},
	}, recent...)...)
	seed(t, ws, "projects/no-owner/_index.md", append([]kv{
		{"type", "project"}, {"client", "initech"}, {"draft", true},
	}, recent...)...)
	// an orphan (no edges) and a stale (old, but edged) client for the flag filters
	seed(t, ws, "clients/orphan-client.md", append([]kv{{"type", "client"}, {"name", "Orphan"}}, recent...)...)
	seed(t, ws, "clients/stale-client.md",
		kv{"type", "client"}, kv{"name", "Stale"}, kv{"depends_on", []any{"initech"}},
		kv{"created", date("2000-01-01")}, kv{"updated", date("2000-01-01")})
	// a tagged (non-orphan) client — the positive control for the tag test
	seed(t, ws, "clients/tagged.md", append([]kv{
		{"type", "client"}, {"name", "Tagged"}, {"tags", []any{"vip", "active"}},
		{"depends_on", []any{"initech"}},
	}, recent...)...)
	return ws
}

func mustQuery(t *testing.T, root string, f Filters) []Match {
	t.Helper()
	got, err := Query(root, f, now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return got
}

func slugs(ms []Match) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Slug)
	}
	return out
}

func sortedSlugs(ms []Match) []string {
	out := slugs(ms)
	sort.Strings(out)
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

// TS-QRY-001-U01 (QRY-001): every filter ANDs — type + field discriminates.
func TestQueryAndsFilters(t *testing.T) {
	ws := qws(t)
	got := mustQuery(t, ws, Filters{Type: ptr("opportunity"), Fields: fields("stage", "prospect")})
	if !eq(slugs(got), []string{"op-prospect"}) {
		t.Fatalf("got %v", slugs(got))
	}
	// A second, non-matching field rules it out (AND, not OR).
	none := mustQuery(t, ws, Filters{
		Type: ptr("opportunity"), Fields: fields("stage", "prospect", "source", "event"),
	})
	if len(none) != 0 {
		t.Fatalf("AND, not OR; got %v", slugs(none))
	}
}

// QRY-001: filters read frontmatter/edges only — a body-only string never matches.
func TestQueryNeverMatchesBodyProse(t *testing.T) {
	ws := qws(t)
	seedRaw(t, ws, "clients/bodyword.md",
		"---\ntype: client\nname: Plain\ncreated: 2026-06-01\nupdated: 2026-06-01\n"+
			"tags: []\n---\nhiddenword lives only in the body\n")
	got := mustQuery(t, ws, Filters{Type: ptr("client"), Tag: ptr("hiddenword")})
	if len(got) != 0 {
		t.Fatalf("body prose must never match a filter; got %v", slugs(got))
	}
}

// TS-QRY-001-U02 (REQ-QRY001-02): --missing/--has resolve the edge.
func TestQueryMissingAndHas(t *testing.T) {
	ws := qws(t)
	missing := mustQuery(t, ws, Filters{Type: ptr("project"), Missing: ptr("owner")})
	if !eq(slugs(missing), []string{"no-owner"}) {
		t.Fatalf("got %v", slugs(missing))
	}
	has := mustQuery(t, ws, Filters{Type: ptr("project"), Has: ptr("owner")})
	if !eq(slugs(has), []string{"has-owner"}) {
		t.Fatalf("got %v", slugs(has))
	}
}

// TS-QRY-001-U03 (REQ-QRY001-03): an undeclared field is a located filter error.
func TestQueryUnknownFieldRaises(t *testing.T) {
	_, err := Query(qws(t), Filters{Type: ptr("opportunity"), Fields: fields("vibe", "high")}, now)
	located, ok := err.(*errs.Located)
	if !ok || located.Code != "filter_error" {
		t.Fatalf("want filter_error, got %#v", err)
	}
	if located.Target != "vibe" || located.Type != "opportunity" {
		t.Fatalf("want the field and type located, got %+v", located)
	}
	if located.Message != "No field 'vibe' on type 'opportunity'" {
		t.Fatalf("message drift: %q", located.Message)
	}
}

// QRY-001: a mistyped --has/--missing predicate errors, not a silent empty set.
func TestQueryUnknownPredicateRaises(t *testing.T) {
	_, err := Query(qws(t), Filters{Type: ptr("project"), Missing: ptr("ownre")}, now)
	located, ok := err.(*errs.Located)
	if !ok || located.Code != "filter_error" || located.Target != "ownre" {
		t.Fatalf("want filter_error on 'ownre', got %#v", err)
	}
}

// An unknown --type is the shared unknown_type error, naming the preset.
func TestQueryUnknownTypeRaises(t *testing.T) {
	_, err := Query(qws(t), Filters{Type: ptr("zzz")}, now)
	located, ok := err.(*errs.Located)
	if !ok || located.Code != "unknown_type" {
		t.Fatalf("want unknown_type, got %#v", err)
	}
	if located.Message != "No type 'zzz' in the firm-ops schema. Known types: "+
		"case-study, client, fragment, meeting, opportunity, partnership, person, project, transcript" {
		t.Fatalf("message drift: %q", located.Message)
	}
}

// QRY-001: --tag matches list membership exactly — never a substring.
func TestQueryTagMatchesMembership(t *testing.T) {
	ws := qws(t)
	got := mustQuery(t, ws, Filters{Type: ptr("client"), Tag: ptr("vip")})
	if !eq(slugs(got), []string{"tagged"}) {
		t.Fatalf("got %v", slugs(got))
	}
	if prefix := mustQuery(t, ws, Filters{Type: ptr("client"), Tag: ptr("vi")}); len(prefix) != 0 {
		t.Fatalf("a tag prefix must not substring-match; got %v", slugs(prefix))
	}
}

// A many-valued relation matches on membership; a bool matches case-insensitively.
func TestQueryMatchesListAndBoolFields(t *testing.T) {
	ws := qws(t)
	team := mustQuery(t, ws, Filters{Type: ptr("project"), Fields: fields("team", "noor")})
	if !eq(slugs(team), []string{"has-owner"}) {
		t.Fatalf("got %v", slugs(team))
	}
	active := mustQuery(t, ws, Filters{Type: ptr("project"), Fields: fields("active", "true")})
	if !eq(slugs(active), []string{"has-owner"}) {
		t.Fatalf("got %v", slugs(active))
	}
	// YAML True renders "True"; the compare lowercases both sides.
	upper := mustQuery(t, ws, Filters{Type: ptr("project"), Fields: fields("active", "TRUE")})
	if !eq(slugs(upper), []string{"has-owner"}) {
		t.Fatalf("got %v", slugs(upper))
	}
}

// TS-QRY-001-U05 (QRY-009): every match carries orphan/stale; the flags filter.
func TestQueryCarriesAndFiltersFlags(t *testing.T) {
	ws := qws(t)
	every := mustQuery(t, ws, Filters{Type: ptr("client")})
	if len(every) == 0 {
		t.Fatal("want clients")
	}
	orphans := mustQuery(t, ws, Filters{Orphan: true})
	if !eq(sortedSlugs(orphans), []string{"orphan-client"}) {
		t.Fatalf("got %v", sortedSlugs(orphans))
	}
	for _, m := range orphans {
		if !m.Orphan {
			t.Fatalf("orphan filter must only return orphans: %+v", m)
		}
	}
	stales := mustQuery(t, ws, Filters{Stale: true})
	found := false
	for _, m := range stales {
		if !m.Stale {
			t.Fatalf("stale filter must only return stale rows: %+v", m)
		}
		found = found || m.Slug == "stale-client"
	}
	if !found {
		t.Fatalf("want stale-client, got %v", slugs(stales))
	}
}

// TS-QRY-001-U06 (QRY-002): no match returns an empty collection, never raising.
func TestQueryEmptyIsSuccess(t *testing.T) {
	got := mustQuery(t, qws(t), Filters{Type: ptr("opportunity"), Fields: fields("stage", "lost")})
	if got == nil || len(got) != 0 {
		t.Fatalf("want an empty non-nil slice, got %#v", got)
	}
}

// TS-QRY-001-U07 (QRY-SHARED-003): default includes drafts; --active/--draft narrow.
func TestQueryDraftScoping(t *testing.T) {
	ws := qws(t)
	if got := sortedSlugs(mustQuery(t, ws, Filters{Type: ptr("project")})); !eq(got, []string{"has-owner", "no-owner"}) {
		t.Fatalf("got %v", got)
	}
	if got := sortedSlugs(mustQuery(t, ws, Filters{Type: ptr("project"), ActiveOnly: true})); !eq(got, []string{"has-owner"}) {
		t.Fatalf("got %v", got)
	}
	if got := sortedSlugs(mustQuery(t, ws, Filters{Type: ptr("project"), DraftOnly: true})); !eq(got, []string{"no-owner"}) {
		t.Fatalf("got %v", got)
	}
}

// QRY-SHARED-003: --missing surfaces incompleteness even on a draft entity.
func TestQueryMissingSurfacesGapRegardlessOfDraft(t *testing.T) {
	got := mustQuery(t, qws(t), Filters{Type: ptr("project"), Missing: ptr("owner")})
	if !eq(slugs(got), []string{"no-owner"}) {
		t.Fatalf("got %v", slugs(got))
	}
}

// --limit caps the returned set, after ordering.
func TestQueryLimit(t *testing.T) {
	got := mustQuery(t, qws(t), Filters{Type: ptr("opportunity"), Limit: ptr(2)})
	if len(got) != 2 {
		t.Fatalf("want 2, got %v", slugs(got))
	}
}

// Results are ordered by the (type, slug) tuple compare, never a joined string.
func TestQueryOrderIsTupleCompare(t *testing.T) {
	got := mustQuery(t, qws(t), Filters{})
	var ids []string
	for _, m := range got {
		ids = append(ids, m.Type+"/"+m.Slug)
	}
	want := []string{
		"client/orphan-client", "client/stale-client", "client/initech", "client/tagged",
		"opportunity/op-proposal", "opportunity/op-prospect", "opportunity/op-won",
		"person/noor", "project/has-owner", "project/no-owner",
	}
	if !eq(ids, want) {
		t.Fatalf("want %v, got %v", want, ids)
	}
}

// Title falls back title -> name -> slug.
func TestQueryTitleFallback(t *testing.T) {
	ws := qws(t)
	byslug := map[string]string{}
	for _, m := range mustQuery(t, ws, Filters{}) {
		byslug[m.Slug] = m.Title
	}
	if byslug["initech"] != "Initech" { // `name`, no `title`
		t.Fatalf("want the name, got %q", byslug["initech"])
	}
	if byslug["op-prospect"] != "op-prospect" { // neither — the slug
		t.Fatalf("want the slug, got %q", byslug["op-prospect"])
	}
}

// --- derived inverse predicates as filters (0.11.0) ---------------------------

const inversePreset = `
version: "0.1.0"
ontology:
  entities:
    adr:
      attributes:
        status: { enum: [proposed, accepted, rejected] }
      relations:
        supersedes: { to: adr, inverse: superseded, acyclic: true }
    repo: {}
storage:
  adr:  { layout: file, path: adrs }
  repo: { layout: file, path: repos }
`

func inverseWS(t *testing.T) string {
	t.Helper()
	ws := wsFromSchema(t, inversePreset)
	seed(t, ws, "adrs/first.md", kv{"type", "adr"}, kv{"title", "First"}, kv{"status", "rejected"})
	seed(t, ws, "adrs/second.md",
		kv{"type", "adr"}, kv{"title", "Second"}, kv{"status", "accepted"}, kv{"supersedes", "first"})
	seed(t, ws, "adrs/live.md", kv{"type", "adr"}, kv{"title", "Live"}, kv{"status", "accepted"})
	return ws
}

// A declared inverse is never stored, so it is answered from the inbound side.
func TestHasAndMissingAcceptADeclaredInverse(t *testing.T) {
	ws := inverseWS(t)
	superseded := mustQuery(t, ws, Filters{Type: ptr("adr"), Has: ptr("superseded")})
	if !eq(slugs(superseded), []string{"first"}) {
		t.Fatalf("got %v", slugs(superseded))
	}
	current := sortedSlugs(mustQuery(t, ws, Filters{Type: ptr("adr"), Missing: ptr("superseded")}))
	if !eq(current, []string{"live", "second"}) {
		t.Fatalf("got %v", current)
	}
}

// Accepting inverses must not turn a typo into a silent empty result.
func TestUnknownPredicateIsStillRejected(t *testing.T) {
	_, err := Query(inverseWS(t), Filters{Type: ptr("adr"), Has: ptr("bogus")}, now)
	if _, ok := err.(*errs.Located); !ok {
		t.Fatalf("want a located error, got %#v", err)
	}
}

// Checking the inverse branch first meant that the moment ANY type declared
// `inverse: <name>`, `<name>` stopped being answerable as a forward predicate —
// and depends_on is in the base block of every type in every preset.
func TestStoredForwardEdgeIsNeverShadowedByAnInverse(t *testing.T) {
	ws := wsFromSchema(t, `
version: "0.1.0"
ontology:
  entities:
    domain:
      relations:
        blocks: { to: any, many: true, inverse: depends_on }
storage:
  domain: { layout: file, path: domains }
`)
	seed(t, ws, "domains/a.md", kv{"type", "domain"}, kv{"title", "A"}, kv{"depends_on", []any{"b"}})
	seed(t, ws, "domains/b.md", kv{"type", "domain"}, kv{"title", "B"})
	has := mustQuery(t, ws, Filters{Type: ptr("domain"), Has: ptr("depends_on")})
	if !eq(slugs(has), []string{"a"}) { // a's own stored edge
		t.Fatalf("got %v", slugs(has))
	}
	missing := mustQuery(t, ws, Filters{Type: ptr("domain"), Missing: ptr("depends_on")})
	if !eq(slugs(missing), []string{"b"}) {
		t.Fatalf("got %v", slugs(missing))
	}
}

// `superseded` is an inverse of adr `supersedes`; `repo` declares none, so
// filtering it there is a typo that used to match the whole type.
func TestAnInverseIsRejectedOnATypeThatCannotCarryIt(t *testing.T) {
	ws := inverseWS(t)
	seed(t, ws, "repos/r1.md", kv{"type", "repo"}, kv{"title", "R1"})
	_, err := Query(ws, Filters{Type: ptr("repo"), Missing: ptr("superseded")}, now)
	if _, ok := err.(*errs.Located); !ok {
		t.Fatalf("want a located error, got %#v", err)
	}
}

// Without --type, --missing skips a type that could never carry the name at all.
func TestMissingSkipsTypesThatCannotCarryTheName(t *testing.T) {
	ws := inverseWS(t)
	seed(t, ws, "repos/r1.md", kv{"type", "repo"}, kv{"title", "R1"})
	got := sortedSlugs(mustQuery(t, ws, Filters{Missing: ptr("superseded")}))
	if !eq(got, []string{"live", "second"}) { // repo/r1 declares nothing to be missing
		t.Fatalf("got %v", got)
	}
}

// An attribute has no edge, so --has/--missing read presence off frontmatter:
// absent, null, or empty counts as a gap.
func TestHasAndMissingOverAnAttribute(t *testing.T) {
	ws := inverseWS(t)
	seed(t, ws, "adrs/blank.md", kv{"type", "adr"}, kv{"title", "Blank"}, kv{"status", ""})
	seed(t, ws, "adrs/nullish.md", kv{"type", "adr"}, kv{"title", "Nullish"}, kv{"status", nil})
	has := sortedSlugs(mustQuery(t, ws, Filters{Type: ptr("adr"), Has: ptr("status")}))
	if !eq(has, []string{"first", "live", "second"}) {
		t.Fatalf("got %v", has)
	}
	missing := sortedSlugs(mustQuery(t, ws, Filters{Type: ptr("adr"), Missing: ptr("status")}))
	if !eq(missing, []string{"blank", "nullish"}) {
		t.Fatalf("got %v", missing)
	}
}
