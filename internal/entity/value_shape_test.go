package entity

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

const objectPreset = `
ontology:
  entities:
    note:
      attributes:
        generated: { type: object, fields: { by: { required: true } } }
        verified: { type: list, items: { type: object, fields: { by: {} } } }
        scores: { type: list, items: { type: number } }
`

func TestScalarIntoObjectIsRefused(t *testing.T) {
	ws := newWSFrom(t, "obj", writePreset(t, "obj", objectPreset, nil))
	if _, err := Create(ws, "note", CreateOpts{ID: "n", Fields: fields("title", "N", "scores", "1, 2")}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws, "note", "n.md")
	before := readFile(t, path)
	for _, c := range []struct{ field, shape string }{
		{"generated", "an object"},
		{"verified", "a list of objects"},
	} {
		_, err := Update(ws, "note/n", UpdateOpts{Fields: fields(c.field, "x")})
		var le *errs.Located
		if !errors.As(err, &le) || le.Code != "value_shape" {
			t.Fatalf("edit %s x: err = %v, want value_shape", c.field, err)
		}
		want := "Field '" + c.field + "' holds " + c.shape +
			", which a command-line value cannot express; write it in the entity file"
		if le.Message != want {
			t.Errorf("message = %q\nwant      %q", le.Message, want)
		}
	}
	if after := readFile(t, path); after != before {
		t.Errorf("a refused edit rewrote the file:\n%s", after)
	}
	// A list of scalars coerces each element through its item spec.
	if _, err := Update(ws, "note/n", UpdateOpts{Fields: fields("scores", "1, two")}); err == nil {
		t.Error("scores '1, two' accepted; want the number refusal for the element")
	}
}

const navigationPreset = `
ontology:
  entities:
    note: {}
    spec: {}
    dossier: {}
storage:
  note: {layout: file, path: notes}
  spec: {layout: file, path: 'spokes/{slug}.md'}
  dossier: {layout: folder, path: dossiers}
`

// add refuses any slug whose file would be index.md or log.md, plain or
// templated, minted or explicit; a folder's _index.md stays legal.
func TestAddRefusesNavigationFileNames(t *testing.T) {
	ws := newWSFrom(t, "nav", writePreset(t, "nav", navigationPreset, nil))
	for _, c := range []struct {
		typ, id, title, want string
	}{
		{"note", "index", "N", "Slug 'index' in note is reserved: index.md is an OKF navigation file; choose another --id"},
		{"spec", "log", "S", "Slug 'log' in spec is reserved: log.md is an OKF navigation file; choose another --id"},
		{"note", "", "Log", "Slug 'log' in note is reserved: log.md is an OKF navigation file; " +
			"pass --id <slug> to name this one differently"},
	} {
		_, err := Create(ws, c.typ, CreateOpts{ID: c.id, Fields: fields("title", c.title), UseTemplate: true})
		var le *errs.Located
		if !errors.As(err, &le) || le.Code != "invalid_slug" || le.Message != c.want {
			t.Errorf("%s --id %q: err = %v\nwant %q", c.typ, c.id, err, c.want)
		}
	}
	if _, err := Create(ws, "dossier", CreateOpts{ID: "index", Fields: fields("title", "D"), UseTemplate: true}); err != nil {
		t.Errorf("folder slug index refused: %v", err)
	}
}

// An OKF datetime keeps the ISO 8601 text the caller wrote; the space form
// fails its pattern. A plain datetime (no pattern) still normalizes.
func TestOKFDatetimeKeepsItsLiteral(t *testing.T) {
	ws := newWSFrom(t, "nav", writePreset(t, "nav", navigationPreset, nil))
	res, err := Create(ws, "note", CreateOpts{ID: "n", Fields: fields("title", "N"), UseTemplate: true})
	requireNoError(t, err)
	_, err = Update(ws, "note/n", UpdateOpts{Fields: fields("stale_after", "2026-01-01T00:00:00Z")})
	requireNoError(t, err)
	if raw := readFile(t, res.Path); !strings.Contains(raw, "stale_after: 2026-01-01T00:00:00Z\n") {
		t.Fatalf("the literal was not kept:\n%s", raw)
	}
	_, err = Update(ws, "note/n", UpdateOpts{Fields: fields("stale_after", "2026-01-01 00:00:00Z")})
	_ = requireCode(t, err, "pattern_violation")
}
