package entity

// Port of the minting half of tests/test_id_prefix.py — enumerated ids
// `<prefix>-NNN-<slug>`, or `NNN-<slug>` where no prefix is declared. The
// schema-side assertions in that file belong to internal/schema.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiteralPrefixNumbersFromOne(t *testing.T) {
	ws := newWS(t, "build-lite")
	first, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Use Postgres", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	second, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Drop Redis", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	if first.Slug != "ad-001-use-postgres" || second.Slug != "ad-002-drop-redis" {
		t.Fatalf("slugs = %q, %q", first.Slug, second.Slug)
	}
}

// fr- and cst- count independently: the number reads as "the Nth constraint".
func TestByValuePrefixKeepsOneSequencePerPrefix(t *testing.T) {
	ws := newWS(t, "build-lite")
	mint := func(title, kind string) string {
		t.Helper()
		res, err := Create(ws, "requirement", CreateOpts{
			Fields: fields("title", title, "kind", kind), UseTemplate: true})
		requireNoError(t, err)
		return res.Slug
	}
	got := []string{
		mint("Pay by card", "functional"),
		mint("Settle in 2s", "constraint"),
		mint("Refund in 30d", "functional"),
		mint("VAT applies", "business-rule"),
	}
	want := []string{
		"fr-001-pay-by-card", "cst-001-settle-in-2s",
		"fr-002-refund-in-30d", "br-001-vat-applies",
	}
	if !equalStrings(got, want) {
		t.Fatalf("slugs = %v, want %v", got, want)
	}
}

// firm-ops declares no prefix, so its ids are plain NNN-slug.
func TestNoDeclaredPrefixStillNumbers(t *testing.T) {
	ws := newWS(t, "firm-ops")
	first, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Acme Corp"), UseTemplate: true})
	requireNoError(t, err)
	second, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Globex"), UseTemplate: true})
	requireNoError(t, err)
	if first.Slug != "001-acme-corp" || second.Slug != "002-globex" {
		t.Fatalf("slugs = %q, %q", first.Slug, second.Slug)
	}
}

// 045 pads; past 999 the number grows rather than wrapping, and removing the
// tail reuses the ordinal (it is one past the highest in use).
func TestOrdinalIsPaddedToThreeAndKeepsCounting(t *testing.T) {
	ws := newWS(t, "build-lite")
	decisions := filepath.Join(ws, "knowledge", "decisions")
	mkdirAll(t, decisions)
	for _, slug := range []string{"ad-044-a", "ad-999-b"} {
		writeFile(t, filepath.Join(decisions, slug+".md"),
			"---\ntype: adr\ntitle: "+slug+"\nstatus: proposed\ncreated: 2026-01-01\n---\n")
	}
	next, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Next", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	if next.Slug != "ad-1000-next" {
		t.Fatalf("slug = %q, want ad-1000-next", next.Slug)
	}

	if err := os.Remove(filepath.Join(decisions, "ad-999-b.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(decisions, "ad-1000-next.md")); err != nil {
		t.Fatal(err)
	}
	after, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "After", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	if after.Slug != "ad-045-after" {
		t.Fatalf("slug = %q, want ad-045-after", after.Slug)
	}
}

// The caller named it; khub does not renumber or re-prefix an explicit --id,
// and the next minted one continues from it.
func TestExplicitIDBypassesTheScheme(t *testing.T) {
	ws := newWS(t, "build-lite")
	res, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Hand named", "status", "proposed"),
		ID:     "ad-050-hand", UseTemplate: true})
	requireNoError(t, err)
	if res.Slug != "ad-050-hand" {
		t.Fatalf("explicit slug = %q", res.Slug)
	}
	next, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Next", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	if next.Slug != "ad-051-next" {
		t.Fatalf("next slug = %q", next.Slug)
	}
}

// `kind` is required, but capture is never blocked — so minting cannot be either.
func TestMissingByValueFallsBackToBareNumber(t *testing.T) {
	ws := newWS(t, "build-lite")
	res, err := Create(ws, "requirement", CreateOpts{
		Fields: fields("title", "No kind yet"), UseTemplate: true})
	requireNoError(t, err)
	if res.Slug != "001-no-kind-yet" {
		t.Fatalf("slug = %q, want 001-no-kind-yet", res.Slug)
	}
}
