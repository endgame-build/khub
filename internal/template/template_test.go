// Library-level assertions ported from tests/test_singletons_templates.py
// (template loading, the heading contract, fenced-heading exclusion) plus a
// fence-scanner table whose expected values were captured from the Python
// implementation (body_h2s over the _FENCE regex) to pin the hand-rolled
// scanner to the backreference regex's exact behavior.
package template

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

const prdTemplate = `title: Product requirements
sections:
  - heading: Vision
    hint: one paragraph
  - heading: Non-goals
  - heading: Success metrics
    text: |
      Nothing measured yet.
`

func writeTemplate(t *testing.T, root, typeName, content string) {
	t.Helper()
	dir := filepath.Join(root, ".khub", "templates")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, typeName+".yaml"), []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func mustLocated(t *testing.T, err error, code string) *errs.Located {
	t.Helper()
	var located *errs.Located
	if !errors.As(err, &located) {
		t.Fatalf("want *errs.Located, got %T (%v)", err, err)
	}
	if located.Code != code {
		t.Fatalf("code = %s, want %s (message %q)", located.Code, code, located.Message)
	}
	return located
}

// --- body_h2s / the fence scanner ------------------------------------------------

func TestBodyH2s(t *testing.T) {
	// Expected values captured from the Python _FENCE/_H2/_NUMBERING pipeline.
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"plain", "## A\n\n## B\n", []string{"A", "B"}},
		{"fenced hidden", "## Intro\n\n```md\n## Config\n```\n", []string{"Intro"}},
		{"unclosed fence keeps content", "```\n## Hidden\n", []string{"Hidden"}},
		{"bad closer is not a close", "```\n## H1\n```x\n## H2\n", []string{"H1", "H2"}},
		{"mixed markers", "```\ncode\n~~~\nmore\n```\n## After\n", []string{"After"}},
		{"later fence after unclosed", "```\ntext\n~~~\n## In\n~~~\n## Out\n", []string{"Out"}},
		{"indented close does not close", "```\n## H\n   ```\n## Tail\n", []string{"H", "Tail"}},
		{"four-tick line opens", "````\n## H\n```\n## Tail\n", []string{"Tail"}},
		{"close with trailing ws", "```\n## H\n```   \n## Tail\n", []string{"Tail"}},
		{"four-tick line never closes", "```\n## H\n````\n## Tail\n", []string{"H", "Tail"}},
		{"tilde fence", "~~~\n## H\n~~~\n## Tail\n", []string{"Tail"}},
		{"empty fence adjacent", "## A\n```\n```\n## B\n", []string{"A", "B"}},
		{"numbering stripped", "## 1. Vision\n## 2.1 Goals\n## 3) X\n## 10.2.3 Deep\n",
			[]string{"Vision", "Goals", "X", "Deep"}},
		{"h3 and no-space excluded", "### Nope\n##No\n##  Spaced  \n", []string{"Spaced"}},
		{"crlf", "## X\r\n```\r\n## Y\r\n```\r\n## Z\r\n", []string{"X", "Z"}},
		{"fence at start", "```\nx\n```\n## A", []string{"A"}},
		{"close without trailing newline", "```\nx\n``` ", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BodyH2s(tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BodyH2s(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// --- missing_heading -------------------------------------------------------------

func TestMissingHeadingSubsequence(t *testing.T) {
	tpl := &BodyTemplate{Type: "t", Sections: []Section{{Heading: "Vision"}, {Heading: "Non-goals"}}}
	cases := []struct {
		body    string
		missing string
		ok      bool // ok=true: a heading is missing
	}{
		{"## Vision\n\n## Non-goals\n", "", false},
		{"## Vision\n\n## Extra\n\n## Non-goals\n", "", false},
		{"## 1. Vision\n\n## 2. Non-goals\n", "", false},   // numbering stripped
		{"## Non-goals\n\n## Vision\n", "Non-goals", true}, // order matters
		{"## Vision\n", "Non-goals", true},
	}
	for _, tc := range cases {
		missing, ok := MissingHeading(tpl, tc.body)
		if missing != tc.missing || ok != tc.ok {
			t.Errorf("MissingHeading(%q) = (%q, %v), want (%q, %v)",
				tc.body, missing, ok, tc.missing, tc.ok)
		}
	}
}

func TestFencedHeadingNeverSatisfiesASection(t *testing.T) {
	// A ## line inside a code fence is content, not structure.
	tpl := &BodyTemplate{Type: "t", Sections: []Section{{Heading: "Config"}}}
	body := "## Intro\n\n```md\n## Config\n```\n"
	if missing, ok := MissingHeading(tpl, body); !ok || missing != "Config" {
		t.Errorf("MissingHeading = (%q, %v), want (Config, true)", missing, ok)
	}
	if missing, ok := MissingHeading(tpl, body+"\n## Config\n"); ok {
		t.Errorf("MissingHeading = (%q, %v), want none", missing, ok)
	}
}

// --- load_template ---------------------------------------------------------------

func TestLoadTemplateRendersScaffold(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "prd", prdTemplate)

	tpl, err := LoadTemplate(root, "prd")
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Title != "Product requirements" {
		t.Errorf("Title = %q", tpl.Title)
	}
	if got := tpl.RequiredHeadings(); !reflect.DeepEqual(got, []string{"Vision", "Non-goals", "Success metrics"}) {
		t.Errorf("RequiredHeadings = %v", got)
	}
	// Exact scaffold text, captured from Python's render().
	want := "## Vision\n\n<!-- one paragraph -->\n\n## Non-goals\n\n## Success metrics\n\nNothing measured yet.\n"
	if got := tpl.Render(); got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestNoTemplateFileMeansNoContract(t *testing.T) {
	tpl, err := LoadTemplate(t.TempDir(), "ghost")
	if err != nil || tpl != nil {
		t.Fatalf("LoadTemplate = (%v, %v), want (nil, nil)", tpl, err)
	}
}

func TestTemplateReservedKeysRejected(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "note", "sections:\n  - heading: Summary\n    optional: true\n")

	_, err := LoadTemplate(root, "note")
	located := mustLocated(t, err, "template_invalid")
	if !strings.Contains(located.Message, "reserved") {
		t.Errorf("message = %q, want it to name the reserved key", located.Message)
	}
}

func TestEmptySectionsMeansNoBodyContract(t *testing.T) {
	// `sections: []` stays a template: add seeds the title, init still creates
	// the singleton. Returning nil here read as "no template at all".
	root := t.TempDir()
	writeTemplate(t, root, "prd", "title: Product requirements\nsections: []\n")

	tpl, err := LoadTemplate(root, "prd")
	if err != nil {
		t.Fatal(err)
	}
	if tpl == nil || len(tpl.Sections) != 0 {
		t.Fatalf("tpl = %+v, want a template with no sections", tpl)
	}
	if tpl.Title != "Product requirements" {
		t.Errorf("Title = %q", tpl.Title)
	}
	if got := tpl.Render(); got != "" {
		t.Errorf("Render() = %q, want empty", got)
	}
}

func TestMissingSectionsKeyIsStillAnError(t *testing.T) {
	// A file that declares nothing coherent is a typo, not an intent.
	root := t.TempDir()
	writeTemplate(t, root, "adr", "title: x\n")

	_, err := LoadTemplate(root, "adr")
	located := mustLocated(t, err, "template_invalid")
	want := "Template for 'adr' (.khub/templates/adr.yaml): 'sections' is required (use `sections: []` for no body contract)"
	if located.Message != want {
		t.Errorf("message = %q, want %q", located.Message, want)
	}
}

func TestTopLevelShapes(t *testing.T) {
	// Falsy documents collapse to {} (-> sections required); truthy
	// non-mappings fail the isinstance gate — Python's `data or {}` chain.
	cases := []struct {
		name    string
		content string
		wantMsg string
	}{
		{"empty file", "", "'sections' is required"},
		{"empty list", "[]\n", "'sections' is required"},
		{"nonempty list", "- a\n- b\n", "top level must be a mapping with a 'sections' list"},
		{"null sections", "sections:\n", "'sections' must be a list"},
		{"scalar sections", "sections: 3\n", "'sections' must be a list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTemplate(t, root, "x", tc.content)
			_, err := LoadTemplate(root, "x")
			located := mustLocated(t, err, "template_invalid")
			if !strings.Contains(located.Message, tc.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", located.Message, tc.wantMsg)
			}
		})
	}
}

func TestBrokenYAMLIsARawParseError(t *testing.T) {
	// validate collects this as a finding; the library surfaces the raw
	// parse error, not a Located one.
	root := t.TempDir()
	writeTemplate(t, root, "note", "- heading: [unclosed\n")

	_, err := LoadTemplate(root, "note")
	if err == nil {
		t.Fatal("want a parse error, got nil")
	}
	var located *errs.Located
	if errors.As(err, &located) {
		t.Errorf("want a raw parse error, got Located %s", located.Code)
	}
}

func TestEntryValidation(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantMsg string
	}{
		{"unknown top-level keys", "title: x\nsections: []\nbudget: 2\nzz: 1\n",
			"unknown top-level keys: budget, zz"},
		{"entry not a mapping", "sections:\n  - just-a-string\n",
			"sections[0] must be a mapping with a 'heading'"},
		{"entry unknown keys", "sections:\n  - heading: A\n    wat: 1\n",
			"sections[0] unknown key(s): wat"},
		{"heading missing", "sections:\n  - hint: x\n",
			"sections[0] needs a non-empty string 'heading'"},
		{"heading empty", "sections:\n  - heading: ''\n",
			"sections[0] needs a non-empty string 'heading'"},
		{"heading not a string", "sections:\n  - heading: 5\n",
			"sections[0] needs a non-empty string 'heading'"},
		{"reserved key placement", "sections:\n  - heading: A\n  - heading: B\n    repeat: true\n",
			"sections[1] uses reserved key(s) repeat — planned for a later version, not supported yet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTemplate(t, root, "x", tc.content)
			_, err := LoadTemplate(root, "x")
			located := mustLocated(t, err, "template_invalid")
			if !strings.Contains(located.Message, tc.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", located.Message, tc.wantMsg)
			}
		})
	}
}

func TestPaddedHeadingAndBareText(t *testing.T) {
	// Heading whitespace strips; text without a trailing newline gains one.
	// Render output captured from Python.
	root := t.TempDir()
	writeTemplate(t, root, "x", "sections:\n  - heading: '  Padded  '\n    text: 'no newline'\n")

	tpl, err := LoadTemplate(root, "x")
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Sections[0].Heading != "Padded" {
		t.Errorf("Heading = %q", tpl.Sections[0].Heading)
	}
	if got := tpl.Render(); got != "## Padded\n\nno newline\n" {
		t.Errorf("Render() = %q", got)
	}
}
