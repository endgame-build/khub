package entity

// Port of the library-level assertions in tests/test_collections.py
// (TS-COL-001 — Single-File Collections). The document-shape cases in that file
// belong to internal/canon; these cover the locked read-modify-write path the
// write verbs drive.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

// addRepoType adds a `repo` collection type plus a project→repo edge, editing
// the workspace's ontology and storage layers the way an engagement extends
// its schema.
func addRepoType(t *testing.T, ws, format, path string) {
	t.Helper()
	op := filepath.Join(ws, ".khub", "ontology.yaml")
	ontDoc := loadYAML(t, op)
	ontAny, _ := ontDoc.Get("ontology")
	entitiesAny, _ := ontAny.(*omap.Map).Get("entities")
	entities := entitiesAny.(*omap.Map)

	attrs := omap.New()
	attrs.Set("repo", kv("type", "text", "required", true))
	attrs.Set("status", kv("enum", []any{"active", "archived"}))
	rels := omap.New()
	rels.Set("project", kv("to", "project"))

	decl := omap.New()
	decl.Set("attributes", attrs)
	decl.Set("relations", rels)
	entities.Set("repo", decl)

	projectAny, _ := entities.Get("project")
	projectRels, _ := projectAny.(*omap.Map).Get("relations")
	projectRels.(*omap.Map).Set("code", kv("to", "repo"))

	text, err := canon.DumpWide(ontDoc)
	requireNoError(t, err)
	writeFile(t, op, text)

	stp := filepath.Join(ws, ".khub", "storage.yaml")
	stDoc := loadYAML(t, stp)
	storageAny, _ := stDoc.Get("storage")
	storage := storageAny.(*omap.Map)
	stDecl := omap.New()
	stDecl.Set("layout", "collection")
	stDecl.Set("format", format)
	if path != "" {
		stDecl.Set("path", path)
	}
	storage.Set("repo", stDecl)
	text, err = canon.DumpWide(stDoc)
	requireNoError(t, err)
	writeFile(t, stp, text)
}

func collectionWS(t *testing.T) string {
	t.Helper()
	ws := newWS(t, "firm-ops")
	addRepoType(t, ws, "jsonl", "")
	return ws
}

// jsonlRows maps each line of a jsonl collection by its slug, so a sibling
// row's bytes can be compared across a write.
func jsonlRows(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(readFile(t, path), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		slug := strings.SplitN(strings.SplitN(line, `"slug": "`, 2)[1], `"`, 2)[0]
		out[slug] = line
	}
	return out
}

// add creates the file, the record carries the row locator, and the stored row
// omits `type` while carrying its prose in the reserved body key.
func TestCollectionCreateEmitsLocator(t *testing.T) {
	ws := collectionWS(t)
	seed(t, ws, "projects/demo/_index.md",
		kv("type", "project", "created", "2026-06-01", "updated", "2026-06-01"))
	file := filepath.Join(ws, "repo.jsonl")
	if _, err := os.Stat(file); err == nil {
		t.Fatal("the collection file existed before the first add")
	}

	res, err := Create(ws, "repo", CreateOpts{
		Fields:      fields("repo", "endgame-build/acme", "status", "active", "project", "demo"),
		ID:          "acme",
		Body:        "row prose",
		UseTemplate: true})
	requireNoError(t, err)
	if got := relSlash(ws, res.Path); got != "repo.jsonl" {
		t.Fatalf("path = %q", got)
	}
	if res.Locator != "repo.jsonl#acme" {
		t.Fatalf("locator = %q", res.Locator)
	}

	rows, err := canon.LoadCollection(readFile(t, file), "jsonl")
	requireNoError(t, err)
	rowAny, _ := rows.Get("acme")
	row := rowAny.(*omap.Map)
	if body, _ := row.Get("body"); body != "row prose" {
		t.Fatalf("stored body = %#v", body)
	}
	if _, hasType := row.Get("type"); hasType {
		t.Fatal("the row stored a redundant `type` key")
	}
}

// get on a row returns the locator, the body, a type-bearing frontmatter, and a
// raw that is the ROW, never the file.
func TestCollectionGetReturnsTheRow(t *testing.T) {
	ws := collectionWS(t)
	_, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/acme"), ID: "acme",
		Body: "row prose", UseTemplate: true})
	requireNoError(t, err)
	_, err = Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/beta"), ID: "beta", UseTemplate: true})
	requireNoError(t, err)

	view, err := Get(ws, "acme", false)
	requireNoError(t, err)
	if view.Locator != "repo.jsonl#acme" {
		t.Fatalf("locator = %q", view.Locator)
	}
	if view.Body != "row prose" {
		t.Fatalf("body = %q", view.Body)
	}
	if got, _ := view.Meta.Get("type"); got != "repo" {
		t.Fatalf("frontmatter type = %#v", got)
	}
	if _, has := view.Meta.Get("body"); has {
		t.Fatal("the reserved body key reached the frontmatter")
	}
	if !strings.HasPrefix(view.Raw, `{"slug": "acme"`) {
		t.Fatalf("raw = %q", view.Raw)
	}
	if strings.Contains(strings.TrimSpace(view.Raw), "\n") {
		t.Fatalf("raw carried more than the row: %q", view.Raw)
	}
}

// a row mints from its title like any entity; a repeated title and an explicit
// --id collision both refuse — nothing is suffixed, the in-lock read gates it.
func TestCollectionMintAndCollision(t *testing.T) {
	ws := collectionWS(t)
	_, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/acme"), ID: "acme", UseTemplate: true})
	requireNoError(t, err)

	minted, err := Create(ws, "repo", CreateOpts{
		Fields: fields("title", "X", "repo", "endgame-build/x", "status", "active"), UseTemplate: true})
	requireNoError(t, err)
	if minted.Slug != "x" {
		t.Fatalf("minted slug = %q", minted.Slug)
	}
	_, err = Create(ws, "repo", CreateOpts{
		Fields: fields("title", "X", "repo", "endgame-build/x2"), UseTemplate: true})
	e := requireCode(t, err, "slug_taken")
	requireMessageContains(t, e,
		"Slug 'x' already exists in repo; pass --id <slug> to name this one differently")
	_, err = Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/y"), ID: "acme", UseTemplate: true})
	e = requireCode(t, err, "slug_taken")
	requireMessageContains(t, e, "already taken")
	_, err = Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/z"), UseTemplate: true})
	requireCode(t, err, "no_slug_source")
}

// editing one row leaves the sibling row's line byte-identical; a no-op unlink
// leaves the whole file untouched.
func TestCollectionEditIsRowLocal(t *testing.T) {
	ws := collectionWS(t)
	seed(t, ws, "projects/demo/_index.md",
		kv("type", "project", "created", "2026-06-01", "updated", "2026-06-01"))
	file := filepath.Join(ws, "repo.jsonl")
	_, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/acme", "status", "active"),
		ID:     "acme", UseTemplate: true})
	requireNoError(t, err)
	_, err = Create(ws, "repo", CreateOpts{
		Fields: fields("title", "X", "repo", "endgame-build/x", "status", "active"), UseTemplate: true})
	requireNoError(t, err)

	before := jsonlRows(t, file)
	res, err := Update(ws, "acme", UpdateOpts{Fields: fields("status", "archived")})
	requireNoError(t, err)
	if res.Locator != "repo.jsonl#acme" {
		t.Fatalf("update locator = %q", res.Locator)
	}
	after := jsonlRows(t, file)
	if after["x"] != before["x"] {
		t.Fatalf("the sibling row moved:\n%s\n%s", before["x"], after["x"])
	}
	if after["acme"] == before["acme"] {
		t.Fatal("the edited row did not change")
	}

	textBefore := readFile(t, file)
	noop, err := Unlink(ws, "acme", "related", "demo")
	requireNoError(t, err)
	if noop.Changed {
		t.Fatal("the no-op unlink reported a change")
	}
	if readFile(t, file) != textBefore {
		t.Fatal("the no-op unlink rewrote the file")
	}
}

// remove refuses while an inbound edge resolves; --force removes the row only
// and the file survives with its other rows.
func TestCollectionRemoveRowOnly(t *testing.T) {
	ws := collectionWS(t)
	seed(t, ws, "projects/demo/_index.md",
		kv("type", "project", "created", "2026-06-01", "updated", "2026-06-01"))
	file := filepath.Join(ws, "repo.jsonl")
	_, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/acme"), ID: "acme", UseTemplate: true})
	requireNoError(t, err)
	_, err = Create(ws, "repo", CreateOpts{
		Fields: fields("title", "X", "repo", "endgame-build/x"), UseTemplate: true})
	requireNoError(t, err)

	_, err = Link(ws, "demo", "code", "repo/acme")
	requireNoError(t, err)

	refused, err := Delete(ws, "repo/acme", false)
	requireNoError(t, err)
	if refused.Removed || len(refused.Inbound) != 1 {
		t.Fatalf("removed=%v inbound=%d", refused.Removed, len(refused.Inbound))
	}
	removed, err := Delete(ws, "repo/acme", true)
	requireNoError(t, err)
	if !removed.Removed {
		t.Fatal("--force did not remove the row")
	}
	rows, err := canon.LoadCollection(readFile(t, file), "jsonl")
	requireNoError(t, err)
	if _, gone := rows.Get("acme"); gone {
		t.Fatal("the row survived")
	}
	if _, kept := rows.Get("x"); !kept {
		t.Fatal("the file lost its other row")
	}
}

// a bad row makes the WHOLE file malformed, and every write refuses.
func TestMalformedCollectionRefusesWrites(t *testing.T) {
	ws := collectionWS(t)
	file := filepath.Join(ws, "repo.jsonl")
	writeFile(t, file, "{\"slug\": \"ok\", \"repo\": \"endgame-build/ok\"}\nnot json\n")
	before := readFile(t, file)

	_, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/x"), ID: "x", UseTemplate: true})
	e := requireCode(t, err, "malformed_entity")
	requireMessageContains(t, e, "Refusing to write repo.jsonl: cannot round-trip it")
	if readFile(t, file) != before {
		t.Fatal("a refused write touched the file")
	}
}

// every collection mutation serializes on the gitignored sidecar lock.
func TestCollectionWriteTakesTheSidecarLock(t *testing.T) {
	ws := collectionWS(t)
	_, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "endgame-build/acme"), ID: "acme", UseTemplate: true})
	requireNoError(t, err)
	lock := filepath.Join(ws, ".khub", "generated", "locks", "workspace.lock")
	if _, statErr := os.Stat(lock); statErr != nil {
		t.Fatalf("no lock sidecar at %s: %v", lock, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(ws, "repo.jsonl.tmp")); statErr == nil {
		t.Fatal("the atomic swap left its temp sibling behind")
	}
}

// a yaml collection at a declared path addresses its rows by the same locator
// rule.
func TestYAMLCollectionLocatorFollowsThePath(t *testing.T) {
	ws := newWS(t, "firm-ops")
	addRepoType(t, ws, "yaml", "data/repos.yaml")
	res, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "e/a"), ID: "svc-a", UseTemplate: true})
	requireNoError(t, err)
	if res.Locator != "data/repos.yaml#svc-a" {
		t.Fatalf("locator = %q", res.Locator)
	}
	if _, statErr := os.Stat(filepath.Join(ws, "data", "repos.yaml")); statErr != nil {
		t.Fatalf("the collection file did not land at its declared path: %v", statErr)
	}
}

// `body` is reserved on a non-md type: written as a field it would be clobbered
// by the next render.
func TestBodyFieldReservedOnCollections(t *testing.T) {
	ws := collectionWS(t)
	_, err := Create(ws, "repo", CreateOpts{
		Fields: fields("repo", "e/a", "body", "prose"), ID: "svc-a", UseTemplate: true})
	e := requireCode(t, err, "body_field_reserved")
	requireMessageContains(t, e, "'body' is reserved on a jsonl entity")
}
