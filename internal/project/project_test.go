package project

// Ports tests/test_project.py (TS-WS-003-U01..U04) plus the library-level rows
// tests/test_status.py asserts through the CLI: per-type counts in declaration
// order, the draft/active split, orphan (zero edges in or out), stale
// derivation, and the OKF flag. Counts are derived from the scanned tree, never
// stored (WS-006/WS-008).

import (
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

var now = time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)

// seeded: a referenced/stale client, two people (one orphan draft), and one
// opportunity carrying edges to the client and a person.
func seeded(t *testing.T) string {
	t.Helper()
	ws := freshWS(t)
	seed(t, ws, "clients/acme.md", kv{"type", "client"}, kv{"name", "Acme"},
		kv{"created", date("2025-01-01")}, kv{"updated", date("2000-01-01")}) // referenced + stale
	seed(t, ws, "identity/team/noor.md", kv{"type", "person"}, kv{"name", "Noor"},
		kv{"role", "manager"}, kv{"created", date("2026-06-01")}) // referenced
	seed(t, ws, "identity/team/nobody.md", kv{"type", "person"}, kv{"name", "Nobody"},
		kv{"role", "consultant"}, kv{"draft", true}, kv{"created", date("2026-06-01")}) // orphan + draft
	seed(t, ws, "opportunities/acme-pov/_index.md", kv{"type", "opportunity"},
		kv{"stage", "prospect"}, kv{"created", date("2026-06-20")}, kv{"updated", date("2026-06-20")},
		kv{"client", "acme"}, kv{"owner", "noor"}) // has edges
	return ws
}

func mustProject(t *testing.T, root string, staleDays int) *Projection {
	t.Helper()
	p, err := Project(root, staleDays, now)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	return p
}

// TS-WS-003-U01: per-type counts (incl. zeros) and the total.
func TestCountsAndTotal(t *testing.T) {
	p := mustProject(t, seeded(t), 90)
	if count(t, p, "client") != 1 || count(t, p, "person") != 2 || count(t, p, "opportunity") != 1 {
		t.Fatalf("counts: %v", p.Counts.Keys())
	}
	if count(t, p, "meeting") != 0 { // empty types still reported
		t.Fatal("empty types must still be reported")
	}
	if p.Total != 4 {
		t.Fatalf("total: %d", p.Total)
	}
}

// Counts iterate in SCHEMA DECLARATION order — the status JSON emits them verbatim.
func TestCountsKeepDeclarationOrder(t *testing.T) {
	p := mustProject(t, seeded(t), 90)
	want := []string{
		"opportunity", "project", "meeting", "transcript", "fragment",
		"case-study", "partnership", "person", "client",
	}
	got := p.Counts.Keys()
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

// TS-WS-003-U02: draft vs active counts.
func TestDraftActiveSplit(t *testing.T) {
	p := mustProject(t, seeded(t), 90)
	if p.Draft != 1 || p.Active != 3 {
		t.Fatalf("draft=%d active=%d", p.Draft, p.Active)
	}
}

// TS-WS-003-U03 (WS-008): orphan = no relations in or out (only `nobody`).
func TestOrphanIsZeroEdges(t *testing.T) {
	if p := mustProject(t, seeded(t), 90); p.Orphan != 1 {
		t.Fatalf("orphan=%d", p.Orphan)
	}
}

// TS-WS-003-U03: the year-2000 client is the only stale entity.
func TestStaleFromUpdated(t *testing.T) {
	if p := mustProject(t, seeded(t), 90); p.Stale != 1 {
		t.Fatalf("stale=%d", p.Stale)
	}
}

// TS-WS-003-U04 (WS-007): types present and refs resolve → OKF-conformant.
func TestOKFConformantWhenRefsResolve(t *testing.T) {
	if p := mustProject(t, seeded(t), 90); !p.OKFConformant {
		t.Fatal("want OKF-conformant")
	}
}

// TS-WS-003-U04: a dangling relation target breaks OKF conformance.
func TestOKFBrokenWhenRefDangles(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "identity/team/noor.md", kv{"type", "person"}, kv{"name", "Noor"},
		kv{"role", "manager"}, kv{"created", date("2026-06-01")})
	seed(t, ws, "opportunities/ghosted/_index.md", kv{"type", "opportunity"},
		kv{"stage", "prospect"}, kv{"created", date("2026-06-20")}, kv{"updated", date("2026-06-20")},
		kv{"client", "missing-client"}, kv{"owner", "noor"}) // client target does not exist
	if p := mustProject(t, ws, 90); p.OKFConformant {
		t.Fatal("a dangling typed edge must break OKF")
	}
}

// TS-WS-003-02: a freshly initialized workspace projects to all zeros.
func TestEmptyWorkspaceZeroes(t *testing.T) {
	p := mustProject(t, freshWS(t), 90)
	if p.Total != 0 {
		t.Fatalf("total=%d", p.Total)
	}
	for _, k := range p.Counts.Keys() {
		if count(t, p, k) != 0 {
			t.Fatalf("%s is non-zero", k)
		}
	}
}

// Identity is (type, slug): a client and a person sharing a slug are two nodes.
func TestCrossTypeSlugCollisionKeepsNodesDistinct(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "clients/dup.md", kv{"type", "client"}, kv{"name", "Dup Co"},
		kv{"created", date("2026-06-01")}, kv{"updated", date("2026-06-01")})
	seed(t, ws, "identity/team/dup.md", kv{"type", "person"}, kv{"name", "Dup Person"},
		kv{"role", "manager"}, kv{"created", date("2026-06-01")})
	p := mustProject(t, ws, 90)
	if p.Total != 2 || p.Orphan != 2 { // slug-only keying would collapse to 1
		t.Fatalf("total=%d orphan=%d", p.Total, p.Orphan)
	}
}

// A typed `client` edge pointing at a person slug does not resolve → OKF false.
func TestTypedEdgeToWrongTypeBreaksOKF(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "identity/team/noor.md", kv{"type", "person"}, kv{"name", "Noor"},
		kv{"role", "manager"}, kv{"created", date("2026-06-01")})
	seed(t, ws, "opportunities/op/_index.md", kv{"type", "opportunity"}, kv{"stage", "prospect"},
		kv{"created", date("2026-06-01")}, kv{"updated", date("2026-06-01")},
		kv{"client", "noor"}, kv{"owner", "noor"})
	if p := mustProject(t, ws, 90); p.OKFConformant {
		t.Fatal("(client, noor) is not a node; only (person, noor) is")
	}
}

// A `references` (to: any) edge may point at a URL without breaking OKF.
func TestUniversalAnyEdgeToNonEntityKeepsOKF(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "clients/acme.md", kv{"type", "client"}, kv{"name", "Acme"},
		kv{"created", date("2026-06-01")}, kv{"updated", date("2026-06-01")},
		kv{"references", []any{"https://example.com/spec"}})
	if p := mustProject(t, ws, 90); !p.OKFConformant {
		t.Fatal("a universal edge may point anywhere")
	}
}

// An entity whose only edge points at itself has no real link → still orphan.
func TestSelfReferenceStaysOrphan(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "identity/team/solo.md", kv{"type", "person"}, kv{"name", "Solo"},
		kv{"role", "manager"}, kv{"created", date("2026-06-01")}, kv{"related", []any{"solo"}})
	if p := mustProject(t, ws, 90); p.Orphan != 1 {
		t.Fatalf("orphan=%d", p.Orphan)
	}
}

// A present-but-unparseable date is surfaced as stale, not silently dropped.
func TestMalformedDateCountsAsStale(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "clients/acme.md", kv{"type", "client"}, kv{"name", "Acme"},
		kv{"created", date("2026-06-01")}, kv{"updated", "not-a-date"})
	if p := mustProject(t, ws, 90); p.Stale != 1 {
		t.Fatalf("stale=%d", p.Stale)
	}
}

// A stray (internal `type` mismatching its layout) leaves the entity counts and
// breaks OKF conformance.
func TestStraysAreCountedSeparatelyAndBreakOKF(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "clients/acme.md", kv{"type", "client"}, kv{"name", "Acme"},
		kv{"created", date("2026-06-01")}, kv{"updated", date("2026-06-01")})
	seed(t, ws, "clients/impostor.md", kv{"type", "person"}, kv{"name", "Impostor"},
		kv{"created", date("2026-06-01")})
	p := mustProject(t, ws, 90)
	if p.Stray != 1 || p.Total != 1 || p.OKFConformant {
		t.Fatalf("stray=%d total=%d okf=%v", p.Stray, p.Total, p.OKFConformant)
	}
}

// A malformed file leaves the counts, is reported on its own, and breaks OKF.
// The fence must CLOSE for the YAML to be parsed at all: an unterminated fence
// is python-frontmatter's ({}, text), which makes the file a stray instead (see
// TestUnterminatedFenceIsAStrayNotMalformed).
func TestMalformedFilesAreCountedSeparatelyAndBreakOKF(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "clients/acme.md", kv{"type", "client"}, kv{"name", "Acme"},
		kv{"created", date("2026-06-01")}, kv{"updated", date("2026-06-01")})
	seedRaw(t, ws, "clients/broken.md", "---\ntype: client\nunclosed: [\n---\n")
	p := mustProject(t, ws, 90)
	if p.Malformed != 1 || p.Total != 1 || p.OKFConformant {
		t.Fatalf("malformed=%d total=%d okf=%v", p.Malformed, p.Total, p.OKFConformant)
	}
}

// `---\ntype: client\nunclosed: [\n` (no closing fence) parses as ({}, text) in
// python-frontmatter, so its `type` does not match its layout: a STRAY, not a
// malformed entity. The two totals must not be conflated.
func TestUnterminatedFenceIsAStrayNotMalformed(t *testing.T) {
	ws := freshWS(t)
	seed(t, ws, "clients/acme.md", kv{"type", "client"}, kv{"name", "Acme"},
		kv{"created", date("2026-06-01")}, kv{"updated", date("2026-06-01")})
	seedRaw(t, ws, "clients/broken.md", "---\ntype: client\nunclosed: [\n")
	p := mustProject(t, ws, 90)
	if p.Stray != 1 || p.Malformed != 0 || p.Total != 1 || p.OKFConformant {
		t.Fatalf("stray=%d malformed=%d total=%d okf=%v",
			p.Stray, p.Malformed, p.Total, p.OKFConformant)
	}
}

// A type declaring `orphan: true` is exempt from the orphan sweep.
func TestOrphanTrueTypeIsExempt(t *testing.T) {
	ws := wsFromSchema(t, `
version: "0.1.0"
ontology:
  entities:
    note: {}
    thing: {}
policy:
  note: { orphan: true }
storage:
  note:  { layout: file, path: notes }
  thing: { layout: file, path: things }
`)
	seed(t, ws, "notes/n1.md", kv{"type", "note"}, kv{"title", "N"})
	seed(t, ws, "things/t1.md", kv{"type", "thing"}, kv{"title", "T"})
	if p := mustProject(t, ws, 90); p.Orphan != 1 {
		t.Fatalf("only thing/t1 counts as an orphan; got %d", p.Orphan)
	}
}

// --- effective_date / is_stale -------------------------------------------------

func TestEffectiveDatePrecedence(t *testing.T) {
	git := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)

	// updated wins over everything.
	d, src := EffectiveDate(meta(kv{"updated", date("2026-06-01")}, kv{"created", date("2020-01-01")}), &git)
	if src != "updated" || d == nil || d.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("updated must win: %v %q", d, src)
	}
	// `updated: null` falls through to git.
	d, src = EffectiveDate(meta(kv{"updated", nil}, kv{"created", date("2020-01-01")}), &git)
	if src != "git log" || d == nil || !d.Equal(git) {
		t.Fatalf("null updated falls through to git: %v %q", d, src)
	}
	// no git supplied → created.
	d, src = EffectiveDate(meta(kv{"updated", nil}, kv{"created", date("2020-01-01")}), nil)
	if src != "created" || d == nil || d.Format("2006-01-02") != "2020-01-01" {
		t.Fatalf("want created: %v %q", d, src)
	}
	// nothing at all.
	if d, src = EffectiveDate(meta(kv{"title", "x"}), nil); d != nil || src != "none" {
		t.Fatalf("want (nil, none): %v %q", d, src)
	}
	// present-but-unparseable keeps its source and yields no date — it does NOT
	// fall through to git.
	if d, src = EffectiveDate(meta(kv{"updated", ""}), &git); d != nil || src != "updated" {
		t.Fatalf("want (nil, updated): %v %q", d, src)
	}
}

func TestEffectiveDateParsesDatetimeAndBasicForms(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"2026-06-20T10:00:00", "2026-06-20"},
		{"2026-06-20 10:00:00+02:00", "2026-06-20"},
		{"20260620", "2026-06-20"},
		{"2026-W03-1", "2026-01-12"},
	} {
		d, _ := EffectiveDate(meta(kv{"updated", tc.in}), nil)
		if d == nil || d.Format("2006-01-02") != tc.want {
			t.Fatalf("%q: want %s, got %v", tc.in, tc.want, d)
		}
	}
	if d, _ := EffectiveDate(meta(kv{"updated", "not-a-date"}), nil); d != nil {
		t.Fatalf("unparseable must yield nil, got %v", d)
	}
}

func TestIsStale(t *testing.T) {
	// no timestamp to judge against
	if IsStale(nil, meta(kv{"title", "x"}), now, 90, nil) {
		t.Fatal("source==none is never stale")
	}
	// exactly at the threshold is not stale ( > , not >= )
	edge := now.AddDate(0, 0, -90).Format("2006-01-02")
	if IsStale(nil, meta(kv{"updated", edge}), now, 90, nil) {
		t.Fatal("exactly stale_days old is not yet stale")
	}
	past := now.AddDate(0, 0, -91).Format("2006-01-02")
	if !IsStale(nil, meta(kv{"updated", past}), now, 90, nil) {
		t.Fatal("one day past the threshold is stale")
	}
	// unparseable is surfaced as stale
	if !IsStale(nil, meta(kv{"updated", "nope"}), now, 90, nil) {
		t.Fatal("an unparseable date is stale, not silently dropped")
	}
	// a future date is not stale (negative delta)
	future := now.AddDate(0, 0, 5).Format("2006-01-02")
	if IsStale(nil, meta(kv{"updated", future}), now, 90, nil) {
		t.Fatal("a future date is not stale")
	}
}

func TestIsExpired(t *testing.T) {
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	for _, tc := range []struct {
		name string
		m    *omap.Map
		want bool
	}{
		{"absent never expires", meta(kv{"title", "x"}), false},
		{"null never expires", meta(kv{"stale_after", nil}), false},
		{"tomorrow has not expired", meta(kv{"stale_after", tomorrow}), false},
		{"today has expired (>=)", meta(kv{"stale_after", today}), true},
		{"yesterday has expired", meta(kv{"stale_after", yesterday}), true},
		{"a time of day does not move the day", meta(kv{"stale_after", today + "T23:59:00"}), true},
		{"a date object", meta(kv{"stale_after", date(yesterday)}), true},
		{"unparseable is surfaced", meta(kv{"stale_after", "someday"}), true},
		{"a one-digit month and day still read", meta(kv{"stale_after", canon.DateTime{ISO: "2099-1-5 10:00:00"}}), false},
		{"a one-digit timestamp in the past", meta(kv{"stale_after", canon.DateTime{ISO: "2026-1-5 10:00:00"}}), true},
		{"an offset does not move the day", meta(kv{"stale_after", canon.DateTime{ISO: tomorrow + " 01:00:00+14:00"}}), false},
		{"a day that does not exist is surfaced", meta(kv{"stale_after", canon.Date{ISO: "2099-02-30"}}), true},
	} {
		if got := IsExpired(nil, tc.m, now); got != tc.want {
			t.Errorf("%s: IsExpired = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The union is what query/search/`khub stale` report; status keeps the two
// apart. An expiry rescues nothing from the age rule, and a fresh `updated`
// rescues nothing from the expiry.
func TestIsStaleIsTheUnion(t *testing.T) {
	old := now.AddDate(0, 0, -200).Format("2006-01-02")
	fresh := now.Format("2006-01-02")
	past := now.AddDate(0, 0, -1).Format("2006-01-02")
	future := now.AddDate(0, 0, 30).Format("2006-01-02")
	if !IsStale(nil, meta(kv{"updated", fresh}, kv{"stale_after", past}), now, 90, nil) {
		t.Fatal("a fresh entity past its stale_after is stale")
	}
	if !IsStale(nil, meta(kv{"updated", old}, kv{"stale_after", future}), now, 90, nil) {
		t.Fatal("an expiry still ahead does not rescue an untouched entity")
	}
	if IsAged(meta(kv{"updated", fresh}, kv{"stale_after", past}), now, 90, nil) {
		t.Fatal("IsAged ignores stale_after")
	}
}

// A type that redeclares `stale_after` as another scalar type made it its own
// field: it carries no expiry, whatever it holds.
func TestRedeclaredStaleAfterNeverExpires(t *testing.T) {
	ws := wsFromSchema(t, `
version: "0.1.0"
ontology:
  entities:
    note:
      attributes:
        stale_after: { type: text }
    thing: {}
storage:
  note:  { layout: file, path: notes }
  thing: { layout: file, path: things }
`)
	seed(t, ws, "notes/n1.md", kv{"type", "note"}, kv{"title", "N"}, kv{"stale_after", "when the quarter closes"})
	seed(t, ws, "things/t1.md", kv{"type", "thing"}, kv{"title", "T"}, kv{"stale_after", date("2020-01-01")})
	if p := mustProject(t, ws, 90); p.Expired != 1 {
		t.Fatalf("only thing/t1 declares the base expiry; got %d expired", p.Expired)
	}
}
