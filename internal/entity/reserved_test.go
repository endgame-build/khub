package entity

import (
	"os"
	"path/filepath"
	"testing"
)

// requireAbsent fails if relpath exists under ws: a refusal writes nothing.
func requireAbsent(t *testing.T, ws, relpath string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(ws, filepath.FromSlash(relpath))); err == nil {
		t.Fatalf("%s was written", relpath)
	}
}

// A minted slug that is a device name refuses and names --id as the way out.
func TestMintedReservedSlugRefuses(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Create(ws, "client", CreateOpts{Fields: fields("name", "CON"), UseTemplate: true})
	e := requireCode(t, err, "invalid_slug")
	requireMessageContains(t, e,
		"Slug 'con' in client is a reserved device name on Windows; pass --id <slug> to name this one differently")
	requireAbsent(t, ws, "clients/con.md")
}

// A folder layout names a directory with the slug; that is refused too.
func TestFolderReservedSlugRefuses(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Create(ws, "project", CreateOpts{Fields: fields("name", "Aux"), UseTemplate: true})
	_ = requireCode(t, err, "invalid_slug")
	requireAbsent(t, ws, "projects/aux")
}

// An explicit --id is checked after slugifying, and its message asks for another.
func TestExplicitReservedIDRefuses(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Fine"), ID: "LPT3", UseTemplate: true})
	e := requireCode(t, err, "invalid_slug")
	requireMessageContains(t, e, "Slug 'lpt3' in client is a reserved device name on Windows; choose another --id")
	requireAbsent(t, ws, "clients/lpt3.md")
}

// The test is on the final stem: a prefix or a date makes the name ordinary.
func TestPrefixedReservedBaseIsFine(t *testing.T) {
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := newWS(t, "build-hub")
	res, err := Create(ws, "requirement", CreateOpts{Fields: fields("title", "Con", "kind", "functional"), UseTemplate: true})
	requireNoError(t, err)
	if res.Slug != "req-con" {
		t.Fatalf("slug = %q", res.Slug)
	}
	res, err = Create(ws, "adr", CreateOpts{Fields: fields("title", "Nul"), UseTemplate: true})
	requireNoError(t, err)
	if res.Slug != "ad-2026-01-15-nul" {
		t.Fatalf("slug = %q", res.Slug)
	}
}

// A collection row is not a file, so its slug is never a device name.
func TestCollectionRowMayUseReservedSlug(t *testing.T) {
	ws := collectionWS(t)
	res, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/nul", "status", "active"), ID: "nul", UseTemplate: true})
	requireNoError(t, err)
	if res.Slug != "nul" || res.Locator != "repo.jsonl#nul" {
		t.Fatalf("slug = %q, locator = %q", res.Slug, res.Locator)
	}
}
