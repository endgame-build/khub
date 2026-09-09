package entity

// Port of the library-level assertions in tests/test_entity_create.py
// (TS-ENT-001 — Create an Entity). The CLI-driven cases in that file are
// covered by the recorded parity fixtures instead.

import (
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
)

// prereqs are the targets every create scenario can relate to.
func prereqs(t *testing.T, ws string) {
	t.Helper()
	seed(t, ws, "clients/initech.md", kv("type", "client", "name", "Initech"))
	seed(t, ws, "identity/team/noor.md", kv("type", "person", "name", "Noor", "role", "partner"))
	seed(t, ws, "projects/initech-pov/_index.md",
		kv("type", "project", "client", "initech", "owner", "noor"))
	seed(t, ws, "partnerships/northwind/_index.md",
		kv("type", "partnership", "partner", "Northwind", "owner", "noor"))
}

func TestSlugifyNormalizes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Acme Corp", "acme-corp"},
		{"  Hello, World!  ", "hello-world"},
		{"acme", "acme"},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TS-ENT-001-U01: id wins, else name; a type with neither refuses. The
// type-name fallback went with the ordinal — without one it minted a single
// id per type.
func TestSlugMintedFromIDOrName(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)

	byID, err := Create(ws, "client", CreateOpts{
		Fields: fields("name", "Acme Corp"), ID: "explicit", UseTemplate: true})
	requireNoError(t, err)
	if byID.Slug != "explicit" {
		t.Fatalf("explicit id: got %q", byID.Slug)
	}
	byName, err := Create(ws, "client", CreateOpts{
		Fields: fields("name", "Beta Corp"), UseTemplate: true})
	requireNoError(t, err)
	if byName.Slug != "beta-corp" {
		t.Fatalf("minted from name: got %q", byName.Slug)
	}
	before := mdFiles(t, ws)
	_, err = Create(ws, "opportunity", CreateOpts{
		Fields:      fields("client", "initech", "owner", "noor", "stage", "prospect"),
		UseTemplate: true})
	e := requireCode(t, err, "no_slug_source")
	requireMessageContains(t, e, "Type 'opportunity' has no name or title to mint an id from; "+
		"pass --name or --title, or name it with --id <slug>")
	if !equalStrings(mdFiles(t, ws), before) {
		t.Fatal("the refused create wrote a file")
	}
}

// TS-ENT-001-U02: enum and pattern are validated on create.
func TestFieldValidationEnumAndPattern(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)

	_, err := Create(ws, "opportunity", CreateOpts{
		Fields:      fields("client", "initech", "owner", "noor", "stage", "banana"),
		UseTemplate: true})
	e := requireCode(t, err, "enum_violation")
	requireMessageContains(t, e,
		"'banana' is not a valid stage (prospect, proposal-sent, won, signed, lost)")

	_, err = Create(ws, "opportunity", CreateOpts{
		Fields: fields("client", "initech", "owner", "noor", "stage", "prospect",
			"crm_id", "abc"),
		UseTemplate: true})
	_ = requireCode(t, err, "pattern_violation")
}

// A bad number raises a located error, not a bare ValueError; inf/nan rejected.
func TestNumberFieldRejectsNonNumericAndNonFinite(t *testing.T) {
	ws := newWS(t, "firm-ops")
	for _, bad := range []string{"abc", "inf", "nan", "1e999"} {
		_, err := Create(ws, "fragment", CreateOpts{
			Fields: fields("stage", "raw", "confidence", bad), UseTemplate: true})
		_ = requireCode(t, err, "number_violation")
	}
	ok, err := Create(ws, "fragment", CreateOpts{
		Fields: fields("title", "Sure", "stage", "raw", "confidence", "0.8"), UseTemplate: true})
	requireNoError(t, err)
	if got := metaValue(t, ok.Path, "confidence"); got != 0.8 {
		t.Fatalf("confidence = %#v, want 0.8", got)
	}
}

// An int-shaped number stays an int; a huge one keeps its literal.
func TestNumberFieldKeepsIntShape(t *testing.T) {
	ws := newWS(t, "firm-ops")
	res, err := Create(ws, "fragment", CreateOpts{
		Fields: fields("title", "Twelve", "stage", "raw", "confidence", "12"), UseTemplate: true})
	requireNoError(t, err)
	if got := metaValue(t, res.Path, "confidence"); got != int64(12) {
		t.Fatalf("confidence = %#v, want int64(12)", got)
	}
	huge := "123456789012345678901234567890"
	big, err := Create(ws, "fragment", CreateOpts{
		Fields: fields("title", "Huge", "stage", "raw", "confidence", huge), UseTemplate: true})
	requireNoError(t, err)
	if got := metaValue(t, big.Path, "confidence"); got != (canon.BigInt{Literal: huge}) {
		t.Fatalf("confidence = %#v, want BigInt(%s)", got, huge)
	}
}

// TS-ENT-001-U03: draft is the manual flag — default false even when a required
// field is missing.
func TestDraftIsManual(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)

	active, err := Create(ws, "opportunity", CreateOpts{
		Fields: fields("name", "Active deal", "stage", "prospect"), UseTemplate: true})
	requireNoError(t, err)
	if active.Draft {
		t.Fatal("a missing required field must not draft the entity")
	}
	if got := metaValue(t, active.Path, "stage"); got != "prospect" {
		t.Fatalf("stage = %#v", got)
	}
	if got := metaValue(t, active.Path, "draft"); got != false {
		t.Fatalf("draft = %#v, want false", got)
	}

	drafted, err := Create(ws, "opportunity", CreateOpts{
		Fields:      fields("name", "Drafted deal", "client", "initech", "owner", "noor", "stage", "prospect"),
		Draft:       true,
		UseTemplate: true})
	requireNoError(t, err)
	if !drafted.Draft {
		t.Fatal("--draft must mark the entity unpublished")
	}
	if got := metaValue(t, drafted.Path, "draft"); got != true {
		t.Fatalf("draft = %#v, want true", got)
	}
}

// TS-ENT-001-U04: an unresolvable relation hard-fails and writes nothing.
func TestReferentialIntegrityHardFailWritesNoFile(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)
	before := mdFiles(t, ws)

	_, err := Create(ws, "opportunity", CreateOpts{
		Fields:      fields("client", "ghost-co", "owner", "noor", "stage", "prospect"),
		UseTemplate: true})
	e := requireCode(t, err, "referential_integrity")
	requireMessageContains(t, e, "No client 'ghost-co' to satisfy relation 'client'")
	if !equalStrings(mdFiles(t, ws), before) {
		t.Fatal("a refused create wrote a file")
	}
}

// TS-ENT-001-U05: --strict rejects an undeclared field; otherwise it is preserved.
func TestStrictFilter(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)

	_, err := Create(ws, "opportunity", CreateOpts{
		Fields: fields("client", "initech", "owner", "noor", "stage", "prospect",
			"vibe", "high"),
		Strict: true, UseTemplate: true})
	e := requireCode(t, err, "strict_unknown_field")
	requireMessageContains(t, e, "Unknown field 'vibe' rejected under --strict")

	loose, err := Create(ws, "opportunity", CreateOpts{
		Fields: fields("name", "Loose deal", "client", "initech", "owner", "noor", "stage", "prospect",
			"vibe", "high"),
		UseTemplate: true})
	requireNoError(t, err)
	if got := metaValue(t, loose.Path, "vibe"); got != "high" {
		t.Fatalf("vibe = %#v", got)
	}
}

// A source that slugifies to empty is refused, never written to a hidden path.
func TestEmptySlugIsRejected(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)
	_, err := Create(ws, "client", CreateOpts{Fields: fields("name", "!!!"), UseTemplate: true})
	e := requireCode(t, err, "invalid_slug")
	requireMessageContains(t, e, "Cannot mint a slug from '!!!'")
}

// TS-ENT-001-U06: a within-type minted collision refuses — minting reads no
// siblings, so the same name mints the same slug, and nothing is suffixed.
func TestSameNameRefuses(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)
	first, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Acme"), UseTemplate: true})
	requireNoError(t, err)
	if first.Slug != "acme" {
		t.Fatalf("slug = %q, want acme", first.Slug)
	}
	before := mdFiles(t, ws)
	_, err = Create(ws, "client", CreateOpts{Fields: fields("name", "Acme"), UseTemplate: true})
	e := requireCode(t, err, "slug_taken")
	requireMessageContains(t, e,
		"Slug 'acme' already exists in client; pass --id <slug> to name this one differently")
	if !equalStrings(mdFiles(t, ws), before) {
		t.Fatal("the refused create wrote a file")
	}
}

// TS-ENT-001-U07: folder vs flat path; engagement stored as an explicit edge.
func TestLayoutResolutionAndEngagementEdge(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)

	opp, err := Create(ws, "opportunity", CreateOpts{
		Fields:      fields("name", "Deal", "client", "initech", "owner", "noor", "stage", "prospect"),
		UseTemplate: true})
	requireNoError(t, err)
	want := filepath.Join(ws, "opportunities", opp.Slug, "_index.md")
	if opp.Path != want {
		t.Fatalf("folder layout path = %q, want %q", opp.Path, want)
	}

	meeting, err := Create(ws, "meeting", CreateOpts{
		Fields: fields("title", "Kickoff", "engagement", "initech-pov", "call_type", "client",
			"source", "recording", "date", "2026-06-19"),
		UseTemplate: true})
	requireNoError(t, err)
	wantFlat := filepath.Join(ws, "meetings", meeting.Slug+".md")
	if meeting.Path != wantFlat {
		t.Fatalf("file layout path = %q, want %q", meeting.Path, wantFlat)
	}
	if got := metaValue(t, meeting.Path, "engagement"); got != "initech-pov" {
		t.Fatalf("engagement = %#v", got)
	}
	// A datetime-declared field fed an ISO date stores as a plain date scalar.
	if got := metaValue(t, meeting.Path, "date"); got != (canon.Date{ISO: "2026-06-19"}) {
		t.Fatalf("date = %#v, want an unquoted date", got)
	}
}

// The frontmatter key canon: type, created, updated, draft, schema-declared
// attributes in DECLARATION order, then relations, then extras in input order.
func TestFrontmatterKeyOrderIsTheSchemaCanon(t *testing.T) {
	ws := newWS(t, "firm-ops")
	prereqs(t, ws)
	res, err := Create(ws, "opportunity", CreateOpts{
		// Input order deliberately disagrees with declaration order.
		Fields: fields("owner", "noor", "zeta", "z", "stage", "prospect",
			"client", "initech", "name", "Deal", "alpha", "a"),
		UseTemplate: true})
	requireNoError(t, err)
	got := readMeta(t, res.Path).Keys()
	want := []string{
		"type", "created", "updated", "draft",
		"name", "stage", // declaration order on the type
		"client", "owner", // relation declaration order
		"zeta", "alpha", // extras keep CLI input order
	}
	if !equalStrings(got, want) {
		t.Fatalf("key order = %v, want %v", got, want)
	}
}
