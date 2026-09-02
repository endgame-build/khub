package entity

// Port of the create-side assertions in tests/test_singletons_templates.py:
// singleton scan/get/add semantics and body seeding at `add`. The template
// loader and validate/check findings in that file belong to internal/template
// and internal/integrity.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const singletonPresetOntology = `
version: "0.1.0"
ontology:
  entities:
    prd:
      attributes:
        title: { required: true }
    note:
      attributes:
        title: { required: true }
`

const singletonPresetStorage = `
storage:
  prd:  { layout: singleton, path: knowledge/prd.md }
  note: { layout: file, path: notes }
`

const singletonPresetPolicy = `
policy:
  prd: { required: true }
`

const prdTemplate = `title: Product requirements
sections:
  - heading: Vision
    hint: one paragraph
  - heading: Non-goals
  - heading: Success metrics
    text: |
      Nothing measured yet.
`

const noteTemplate = `sections:
  - heading: Summary
  - heading: Details
`

// templatedWS is the singleton-bearing fixture workspace with both templates.
func templatedWS(t *testing.T) string {
	t.Helper()
	dir := writePreset(t, "fixture",
		singletonPresetOntology+singletonPresetPolicy+singletonPresetStorage,
		map[string]string{
			"prd.yaml":  prdTemplate,
			"note.yaml": noteTemplate,
		})
	return newWSFrom(t, "fixture", dir)
}

// add on a missing singleton writes the one file, seeded from the template.
func TestAddCreatesMissingSingletonWithTemplate(t *testing.T) {
	ws := templatedWS(t)
	res, err := Create(ws, "prd", CreateOpts{Fields: fields("title", "PRD"), UseTemplate: true})
	requireNoError(t, err)
	if res.Slug != "prd" {
		t.Fatalf("slug = %q, want the type name", res.Slug)
	}
	if res.Locator != "" {
		t.Fatalf("a singleton must carry no row locator, got %q", res.Locator)
	}
	text := readFile(t, filepath.Join(ws, "knowledge", "prd.md"))
	for _, want := range []string{"## Vision", "<!-- one paragraph -->", "## Non-goals",
		"Nothing measured yet."} {
		if !strings.Contains(text, want) {
			t.Fatalf("template section %q missing:\n%s", want, text)
		}
	}
}

// a singleton resolves by its bare type name.
func TestSingletonResolvesByBareTypeName(t *testing.T) {
	ws := templatedWS(t)
	_, err := Create(ws, "prd", CreateOpts{Fields: fields("title", "PRD"), UseTemplate: true})
	requireNoError(t, err)

	view, err := Get(ws, "prd", false)
	requireNoError(t, err)
	if got, _ := view.Meta.Get("type"); got != "prd" {
		t.Fatalf("type = %#v", got)
	}
	if !strings.HasPrefix(view.Body, "## Vision") {
		t.Fatalf("body = %q", view.Body)
	}
}

func TestAddRefusesSecondSingleton(t *testing.T) {
	ws := templatedWS(t)
	_, err := Create(ws, "prd", CreateOpts{Fields: fields("title", "PRD"), UseTemplate: true})
	requireNoError(t, err)

	_, err = Create(ws, "prd", CreateOpts{Fields: fields("title", "Another"), UseTemplate: true})
	e := requireCode(t, err, "singleton_exists")
	requireMessageContains(t, e,
		"Singleton 'prd' already exists at knowledge/prd.md; edit it instead of adding another")
}

func TestAddRefusesForeignSingletonID(t *testing.T) {
	ws := templatedWS(t)
	_, err := Create(ws, "prd", CreateOpts{
		Fields: fields("title", "PRD"), ID: "my-prd", UseTemplate: true})
	e := requireCode(t, err, "singleton_id")
	requireMessageContains(t, e, "'prd' is a singleton — its id is always 'prd' (drop --id)")
	if _, statErr := os.Stat(filepath.Join(ws, "knowledge", "prd.md")); statErr == nil {
		t.Fatal("the refused create wrote the singleton")
	}
}

func TestAddSeedsBodyFromTemplate(t *testing.T) {
	ws := templatedWS(t)
	res, err := Create(ws, "note", CreateOpts{Fields: fields("title", "First"), UseTemplate: true})
	requireNoError(t, err)
	text := readFile(t, res.Path)
	if !strings.Contains(text, "## Summary") || !strings.Contains(text, "## Details") {
		t.Fatalf("template not seeded:\n%s", text)
	}
}

func TestAddExplicitBodyWinsOverTemplate(t *testing.T) {
	ws := templatedWS(t)
	res, err := Create(ws, "note", CreateOpts{
		Fields: fields("title", "Second"), Body: "just prose\n", UseTemplate: true})
	requireNoError(t, err)
	text := readFile(t, res.Path)
	if strings.Contains(text, "## Summary") {
		t.Fatalf("the template overrode --body:\n%s", text)
	}
	if !strings.HasSuffix(text, "just prose\n") {
		t.Fatalf("body = %q", text)
	}
}

// --no-template on a templated type would mint an entity validate rejects, so
// it is refused and nothing is written.
func TestNoTemplateRefusedOnTemplatedType(t *testing.T) {
	ws := templatedWS(t)
	_, err := Create(ws, "note", CreateOpts{Fields: fields("title", "Third"), UseTemplate: false})
	e := requireCode(t, err, "template_required")
	requireMessageContains(t, e, "has a body template")
	if _, statErr := os.Stat(filepath.Join(ws, "notes", "third.md")); statErr == nil {
		t.Fatal("the refused create wrote a file")
	}
}

// Capture is never blocked: a broken template seeds nothing and still writes.
func TestAddNeverBlockedByBrokenTemplate(t *testing.T) {
	ws := templatedWS(t)
	writeFile(t, filepath.Join(ws, ".khub", "templates", "note.yaml"), "sections: 3\n")

	res, err := Create(ws, "note", CreateOpts{
		Fields: fields("title", "Still writes"), UseTemplate: true})
	requireNoError(t, err)
	if _, statErr := os.Stat(res.Path); statErr != nil {
		t.Fatalf("nothing was written: %v", statErr)
	}
	if strings.Contains(readFile(t, res.Path), "## Summary") {
		t.Fatal("a broken template seeded a body")
	}
}

// A type with no template at all is unaffected by --no-template.
func TestNoTemplateAllowedOnUntemplatedType(t *testing.T) {
	ws := templatedWS(t)
	if err := os.Remove(filepath.Join(ws, ".khub", "templates", "note.yaml")); err != nil {
		t.Fatal(err)
	}
	res, err := Create(ws, "note", CreateOpts{
		Fields: fields("title", "Bare"), UseTemplate: false})
	requireNoError(t, err)
	if body := strings.SplitN(readFile(t, res.Path), "---\n", 3)[2]; body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}
