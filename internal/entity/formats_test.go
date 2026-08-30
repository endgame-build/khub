package entity

// Per-format write assertions: the non-md per-item formats carry prose in the
// reserved `body` key, and a datetime-typed value takes the separator its
// serializer uses in Python (ruamel isoformat(' '), json.dumps isoformat()).
// Byte-checked against khub 0.18.0.

import (
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

// addNonMDTypes declares a json and a yaml per-item type on a firm-ops
// workspace, each with a date and a datetime attribute, editing the ontology
// and storage layers the way an engagement extends its schema.
func addNonMDTypes(t *testing.T, ws string) {
	t.Helper()
	op := filepath.Join(ws, ".khub", "ontology.yaml")
	ontDoc := loadYAML(t, op)
	ontAny, _ := ontDoc.Get("ontology")
	entitiesAny, _ := ontAny.(*omap.Map).Get("entities")
	entities := entitiesAny.(*omap.Map)
	stp := filepath.Join(ws, ".khub", "storage.yaml")
	stDoc := loadYAML(t, stp)
	storageAny, _ := stDoc.Get("storage")
	storage := storageAny.(*omap.Map)

	for _, spec := range []struct{ name, format, path string }{
		{"jnote", "json", "jnotes"},
		{"ynote", "yaml", "ynotes"},
	} {
		attrs := omap.New()
		attrs.Set("when", kv("type", "datetime"))
		attrs.Set("day", kv("type", "date"))
		decl := omap.New()
		decl.Set("attributes", attrs)
		entities.Set(spec.name, decl)

		stDecl := omap.New()
		stDecl.Set("layout", "file")
		stDecl.Set("format", spec.format)
		stDecl.Set("path", spec.path)
		storage.Set(spec.name, stDecl)
	}
	text, err := canon.DumpWide(ontDoc)
	requireNoError(t, err)
	writeFile(t, op, text)
	text, err = canon.DumpWide(stDoc)
	requireNoError(t, err)
	writeFile(t, stp, text)
}

func TestJSONEntityBytes(t *testing.T) {
	ws := newWS(t, "firm-ops")
	addNonMDTypes(t, ws)
	res, err := Create(ws, "jnote", CreateOpts{
		Fields: fields("when", "2026-06-19T10:00", "day", "2026-06-19"),
		ID:     "j1", Body: "prose", UseTemplate: true})
	requireNoError(t, err)

	want := "{\n" +
		"  \"type\": \"jnote\",\n" +
		"  \"created\": \"" + today().ISO + "\",\n" +
		"  \"updated\": \"" + today().ISO + "\",\n" +
		"  \"draft\": false,\n" +
		"  \"when\": \"2026-06-19T10:00:00\",\n" +
		"  \"day\": \"2026-06-19\",\n" +
		"  \"body\": \"prose\"\n" +
		"}\n"
	if got := readFile(t, res.Path); got != want {
		t.Fatalf("json entity bytes:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLEntityBytes(t *testing.T) {
	ws := newWS(t, "firm-ops")
	addNonMDTypes(t, ws)
	res, err := Create(ws, "ynote", CreateOpts{
		Fields: fields("when", "2026-06-19T10:00", "day", "2026-06-19"),
		ID:     "y1", Body: "prose", UseTemplate: true})
	requireNoError(t, err)

	want := "type: ynote\n" +
		"created: " + today().ISO + "\n" +
		"updated: " + today().ISO + "\n" +
		"draft: false\n" +
		"when: 2026-06-19 10:00:00\n" +
		"day: 2026-06-19\n" +
		"body: prose\n"
	if got := readFile(t, res.Path); got != want {
		t.Fatalf("yaml entity bytes:\n%s\nwant:\n%s", got, want)
	}
}

// A non-md per-item type reserves `body` for prose, exactly as a collection
// does.
func TestBodyFieldReservedOnJSONEntity(t *testing.T) {
	ws := newWS(t, "firm-ops")
	addNonMDTypes(t, ws)
	_, err := Create(ws, "jnote", CreateOpts{
		Fields: fields("body", "prose"), ID: "j2", UseTemplate: true})
	e := requireCode(t, err, "body_field_reserved")
	requireMessageContains(t, e,
		"'body' is reserved on a json entity; pass --body/--body-file for prose")
}

// An edit round-trips a non-md entity: the reserved key is popped on read and
// placed back on write, and an empty body writes no key.
func TestNonMDEditRoundTrip(t *testing.T) {
	ws := newWS(t, "firm-ops")
	addNonMDTypes(t, ws)
	res, err := Create(ws, "ynote", CreateOpts{
		Fields: fields("day", "2026-06-19"), ID: "y1", Body: "prose", UseTemplate: true})
	requireNoError(t, err)

	_, err = Update(ws, "y1", UpdateOpts{Fields: fields("day", "2026-07-01")})
	requireNoError(t, err)
	if got := readFile(t, res.Path); got != "type: ynote\n"+
		"created: "+today().ISO+"\n"+
		"updated: "+today().ISO+"\n"+
		"draft: false\n"+
		"day: 2026-07-01\n"+
		"body: prose\n" {
		t.Fatalf("after edit:\n%s", got)
	}
	cleared := ""
	_, err = Update(ws, "y1", UpdateOpts{Fields: fields(), Body: &cleared})
	requireNoError(t, err)
	if hasKey(t, res.Path, "body") {
		t.Fatalf("an empty body wrote the reserved key:\n%s", readFile(t, res.Path))
	}
}
