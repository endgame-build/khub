package entity

// Port of the minting half of tests/test_id_prefix.py — prefixed and dated ids
// `<prefix>-<YYYY-MM-DD>-<slug>`, each part optional per type, minted as a pure
// function of schema + frontmatter + title. The schema-side assertions in that
// file belong to internal/schema.

import (
	"os"
	"path/filepath"
	"testing"
)

// byKindPreset declares the by-value prefix form no shipped preset uses any
// more (requirement collapsed to a literal `req`), so the arm stays tested.
const byKindPreset = `
version: "0.1.0"
ontology:
  entities:
    requirement:
      attributes:
        title: { required: true }
        kind:  { enum: [functional, constraint, business-rule], required: true }
storage:
  requirement:
    layout: file
    path: reqs
    id_prefix: { by: kind, map: { functional: fr, constraint: cst, business-rule: br } }
`

func byKindWS(t *testing.T) string {
	t.Helper()
	return newWSFrom(t, "bykind", writePreset(t, "bykind", byKindPreset, nil))
}

// A dated type carries the day it was minted on; the clock is pinned the same
// way the parity recorder pins it.
func TestLiteralPrefixAndDate(t *testing.T) {
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := newWS(t, "build-hub")
	first, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Use Postgres", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	second, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Drop Redis", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	if first.Slug != "ad-2026-01-15-use-postgres" || second.Slug != "ad-2026-01-15-drop-redis" {
		t.Fatalf("slugs = %q, %q", first.Slug, second.Slug)
	}
}

// An undated prefixed type is the prefix plus the slugified title, nothing else.
func TestLiteralPrefixWithoutDate(t *testing.T) {
	ws := newWS(t, "build-hub")
	got := []string{}
	for _, tc := range []struct{ typ, title string }{
		{"requirement", "Pay by card"}, {"component", "Public API"}, {"repo", "Acme API"},
	} {
		extra := []string{"title", tc.title}
		switch tc.typ {
		case "requirement":
			extra = append(extra, "kind", "functional")
		case "component":
			extra = append(extra, "kind", "service")
		case "repo":
			extra = append(extra, "repo", "acme/api", "status", "active")
		}
		res, err := Create(ws, tc.typ, CreateOpts{Fields: fields(extra...), UseTemplate: true})
		requireNoError(t, err)
		got = append(got, res.Slug)
	}
	want := []string{"req-pay-by-card", "cmp-public-api", "rp-acme-api"}
	if !equalStrings(got, want) {
		t.Fatalf("slugs = %v, want %v", got, want)
	}
}

// The by-value form picks the prefix from the deciding attribute.
func TestByValuePrefixMintsPerKind(t *testing.T) {
	ws := byKindWS(t)
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
		mint("VAT applies", "business-rule"),
	}
	want := []string{"fr-pay-by-card", "cst-settle-in-2s", "br-vat-applies"}
	if !equalStrings(got, want) {
		t.Fatalf("slugs = %v, want %v", got, want)
	}
}

// firm-ops declares no prefix and no date, so its ids are bare slugs.
func TestNoDeclaredPrefixMintsBareSlug(t *testing.T) {
	ws := newWS(t, "firm-ops")
	first, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Acme Corp"), UseTemplate: true})
	requireNoError(t, err)
	second, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Globex"), UseTemplate: true})
	requireNoError(t, err)
	if first.Slug != "acme-corp" || second.Slug != "globex" {
		t.Fatalf("slugs = %q, %q", first.Slug, second.Slug)
	}
}

// Minting reads no siblings: files already in the directory, ordinal-shaped or
// not, change nothing about the next id. This is the whole point of dropping
// the ordinal — the read-modify-write that raced across branches is gone.
func TestMintingReadsNoSiblings(t *testing.T) {
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := newWS(t, "build-hub")
	decisions := filepath.Join(ws, "knowledge", "decisions")
	mkdirAll(t, decisions)
	for _, slug := range []string{"ad-044-a", "ad-999-b", "ad-2026-01-14-next"} {
		writeFile(t, filepath.Join(decisions, slug+".md"),
			"---\ntype: adr\ntitle: "+slug+"\nstatus: proposed\ncreated: 2026-01-01\n---\n")
	}
	next, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Next", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	if next.Slug != "ad-2026-01-15-next" {
		t.Fatalf("slug = %q, want ad-2026-01-15-next", next.Slug)
	}
}

// The caller named it; khub does not re-prefix or date an explicit --id, and
// the next minted one is unaffected by it.
func TestExplicitIDBypassesTheScheme(t *testing.T) {
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := newWS(t, "build-hub")
	res, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Hand named", "status", "proposed"),
		ID:     "hand", UseTemplate: true})
	requireNoError(t, err)
	if res.Slug != "hand" {
		t.Fatalf("explicit slug = %q", res.Slug)
	}
	next, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Next", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	if next.Slug != "ad-2026-01-15-next" {
		t.Fatalf("next slug = %q", next.Slug)
	}
}

// The same title mints the same id, which is refused — naming --id as the way
// out — and nothing is written. Two branches each adding "X" now collide on
// one filename instead of numbering past each other.
func TestSameTitleRefusesAndNamesID(t *testing.T) {
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := newWS(t, "build-hub")
	_, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Use Postgres", "status", "proposed"), UseTemplate: true})
	requireNoError(t, err)
	before := mdFiles(t, ws)

	_, err = Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Use Postgres", "status", "accepted"), UseTemplate: true})
	e := requireCode(t, err, "slug_taken")
	requireMessageContains(t, e,
		"Slug 'ad-2026-01-15-use-postgres' already exists in adr; pass --id <slug> to name this one differently")
	if !equalStrings(mdFiles(t, ws), before) {
		t.Fatal("the refused create wrote a file")
	}
	if _, statErr := os.Stat(filepath.Join(ws, "knowledge", "decisions",
		"ad-2026-01-15-use-postgres-2.md")); statErr == nil {
		t.Fatal("the refused create auto-suffixed")
	}

	named, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Use Postgres", "status", "accepted"),
		ID:     "use-postgres-again", UseTemplate: true})
	requireNoError(t, err)
	if named.Slug != "use-postgres-again" {
		t.Fatalf("slug = %q", named.Slug)
	}
}

// The id is minted after the frontmatter is assembled, so a --created override
// dates it — an imported decision keeps the day it was really made.
func TestCreatedOverrideDatesTheId(t *testing.T) {
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := newWS(t, "build-hub")
	res, err := Create(ws, "adr", CreateOpts{
		Fields:      fields("title", "Imported", "status", "accepted", "created", "2025-03-01"),
		UseTemplate: true})
	requireNoError(t, err)
	if res.Slug != "ad-2025-03-01-imported" {
		t.Fatalf("slug = %q, want ad-2025-03-01-imported", res.Slug)
	}
}

// A by-value prefix whose deciding attribute is unset has nothing to mint
// from: the bare `NNN-slug` that used to stand in is gone, so it refuses and
// names the flag.
func TestMissingByValueRefusesToMint(t *testing.T) {
	ws := byKindWS(t)
	before := mdFiles(t, ws)
	_, err := Create(ws, "requirement", CreateOpts{
		Fields: fields("title", "No kind yet"), UseTemplate: true})
	e := requireCode(t, err, "id_prefix_undecided")
	requireMessageContains(t, e,
		"Type 'requirement' needs --kind <functional|constraint|business-rule> to mint an id; "+
			"pass it, or name the entity with --id <slug>")
	if !equalStrings(mdFiles(t, ws), before) {
		t.Fatal("the refused create wrote a file")
	}

	// --id sidesteps the mint entirely; the gate reports the shape later.
	named, err := Create(ws, "requirement", CreateOpts{
		Fields: fields("title", "No kind yet"), ID: "no-kind-yet", UseTemplate: true})
	requireNoError(t, err)
	if named.Slug != "no-kind-yet" {
		t.Fatalf("slug = %q", named.Slug)
	}
}
