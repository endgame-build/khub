// Library-level assertions ported from tests/test_singletons_templates.py
// (template loading, the heading contract, fenced-heading exclusion) plus
// kb 0.14.0's tests/test_kb.py template rows (the section-rule vocabulary,
// BROKEN_TEMPLATES, hint rendering). The fence-scanner table pins the
// CommonMark pairing rules kb `_fence_spans` states; the body-scanning rows
// (comments, offsets, rules) live in body_test.go.
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
	// CommonMark pairing (kb _fence_spans): a closer repeats the opener's
	// character at least as many times with no info string, indent up to
	// three spaces, an unterminated fence runs to the end.
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"plain", "## A\n\n## B\n", []string{"A", "B"}},
		{"fenced hidden", "## Intro\n\n```md\n## Config\n```\n", []string{"Intro"}},
		{"unclosed fence runs to EOF", "```\n## Hidden\n", []string{}},
		{"bad closer is not a close", "```\n## H1\n```x\n## H2\n", []string{}},
		{"mixed markers", "```\ncode\n~~~\nmore\n```\n## After\n", []string{"After"}},
		{"later fence after unclosed", "```\ntext\n~~~\n## In\n~~~\n## Out\n", []string{}},
		{"three-space indented close closes", "```\n## H\n   ```\n## Tail\n", []string{"Tail"}},
		{"four-tick opener ignores a three-tick line", "````\n## H\n```\n## Tail\n", []string{}},
		{"close with trailing ws", "```\n## H\n```   \n## Tail\n", []string{"Tail"}},
		{"longer closer closes", "```\n## H\n````\n## Tail\n", []string{"Tail"}},
		{"tilde fence", "~~~\n## H\n~~~\n## Tail\n", []string{"Tail"}},
		{"empty fence adjacent", "## A\n```\n```\n## B\n", []string{"A", "B"}},
		{"numbering stripped only with a separator", "## 1. Vision\n## 2.1 Goals\n## 3) X\n## 10.2.3 Deep\n",
			[]string{"Vision", "2.1 Goals", "X", "10.2.3 Deep"}},
		{"h3 and no-space excluded", "### Nope\n##No\n##  Spaced  \n", []string{"Spaced"}},
		{"crlf", "## X\r\n```\r\n## Y\r\n```\r\n## Z\r\n", []string{"X", "Z"}},
		{"fence at start", "```\nx\n```\n## A", []string{"A"}},
		{"close without trailing newline", "```\nx\n``` ", []string{}},
		{"commented heading", "## A\n<!-- ## B -->\n## C\n", []string{"A", "C"}},
		{"four-space indent is not a heading", "    ## A\n   ## B\n", []string{"B"}},
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

// --- render ----------------------------------------------------------------------

// kb test_a_hint_becomes_a_comment_and_text_is_literal: one file is both
// scaffold and contract — `hint` is guidance the author replaces, `text` is
// content they keep, and the top-level hint leads the body.
func TestHintBecomesCommentAndTextIsLiteral(t *testing.T) {
	sections := []Section{
		{Heading: "Context", Hint: "the forces"},
		{Heading: "Decision", Text: "Nothing yet.\n"},
		{Heading: "Consequences"},
	}
	tpl := &BodyTemplate{Type: "adr", Sections: sections}
	want := "## Context\n\n<!-- the forces -->\n\n## Decision\n\nNothing yet.\n\n## Consequences\n"
	if got := tpl.Render(); got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
	led := &BodyTemplate{Type: "adr", Hint: "one decision", Sections: sections}
	if got := led.Render(); got != "<!-- one decision -->\n\n"+want {
		t.Errorf("Render() with a top-level hint = %q", got)
	}
	// Optional sections are scaffolded like any other: declaring one optional
	// says it may be absent from a finished body, not that an author should
	// have to remember it exists.
	opt := &BodyTemplate{Type: "adr", Sections: []Section{{Heading: "Context"}, {Heading: "Alternatives", Optional: true}}}
	if got := opt.Render(); got != "## Context\n\n## Alternatives\n" {
		t.Errorf("Render() with an optional section = %q", got)
	}
	if got := opt.RequiredHeadings(); !reflect.DeepEqual(got, []string{"Context"}) {
		t.Errorf("RequiredHeadings = %v", got)
	}
}

// kb test_a_top_level_hint_is_how_a_type_with_no_headings_still_guides: a
// type that is one statement or one paragraph would turn every heading into
// a body_shape finding waiting to happen; the hint is how its template still
// says what belongs in the body.
func TestTopLevelHintRendersForHeadinglessTemplate(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "adr", "hint: one sentence, present tense\nsections: []\n")

	tpl, err := LoadTemplate(root, "adr", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Hint != "one sentence, present tense" {
		t.Errorf("Hint = %q", tpl.Hint)
	}
	if got := tpl.Render(); got != "<!-- one sentence, present tense -->\n" {
		t.Errorf("Render() = %q", got)
	}
	if len(tpl.RequiredHeadings()) != 0 {
		t.Errorf("RequiredHeadings = %v, want none", tpl.RequiredHeadings())
	}
	// Its own scaffold satisfies it: no heading missing, no rule fired.
	if missing, ok := MissingHeading(tpl, tpl.Render()); ok {
		t.Errorf("MissingHeading = %q", missing)
	}
	if got := BodyRules(tpl, tpl.Render()); len(got) != 0 {
		t.Errorf("BodyRules = %v", got)
	}
}

// --- load_template ---------------------------------------------------------------

func TestLoadTemplateRendersScaffold(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "prd", prdTemplate)

	tpl, err := LoadTemplate(root, "prd", nil)
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
	tpl, err := LoadTemplate(t.TempDir(), "ghost", nil)
	if err != nil || tpl != nil {
		t.Fatalf("LoadTemplate = (%v, %v), want (nil, nil)", tpl, err)
	}
}

func TestTemplateReservedKeysRejected(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "note", "sections:\n  - heading: Summary\n    repeat: true\n")

	_, err := LoadTemplate(root, "note", nil)
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

	tpl, err := LoadTemplate(root, "prd", nil)
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

	_, err := LoadTemplate(root, "adr", nil)
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
			_, err := LoadTemplate(root, "x", nil)
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

	_, err := LoadTemplate(root, "note", nil)
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
			"unknown top-level key(s): budget, zz"},
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
			_, err := LoadTemplate(root, "x", nil)
			located := mustLocated(t, err, "template_invalid")
			if !strings.Contains(located.Message, tc.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", located.Message, tc.wantMsg)
			}
		})
	}
}

// kb BROKEN_TEMPLATES, the section/bound/rule/hint/title rows (the lens rows
// are lenses_test.go's). Every one is a typo that would otherwise be
// silently ignored, leaving an author to believe a rule was in force. The
// wrapper is khub's pinned `Template for '<stem>' (...)`; the <why> is kb's.
func TestBrokenTemplates(t *testing.T) {
	cases := []struct {
		name    string
		content string
		why     string
	}{
		{"reserved key", "sections:\n- heading: X\n  repeat: true\n",
			"sections[0] uses reserved key(s) repeat — planned for a later version, not supported yet"},
		// Only keys with a planned meaning are reserved; `images`, `lists`
		// and `tables` never had one and are ordinary unknown keys.
		{"unreserved key is unknown", "sections:\n- heading: X\n  tables: 1\n",
			"sections[0] unknown key(s): tables"},
		{"unknown entry key", "sections:\n- heading: X\n  hnit: typo\n",
			"sections[0] unknown key(s): hnit"},
		{"no sections key", "title: Nope\n",
			"'sections' is required (use `sections: []` for no body contract)"},
		{"sections is not a list", "sections: 7\n", "'sections' must be a list"},
		{"entry without a heading", "sections:\n- hint: orphaned\n",
			"sections[0] needs a non-empty string 'heading'"},
		{"unknown top-level key", "sections: []\ncolour: blue\n",
			"unknown top-level key(s): colour"},
		{"top level is not a mapping", "- just\n- a list\n",
			"top level must be a mapping with a 'sections' list"},
		{"section hint is not text", "sections:\n- heading: X\n  hint: 7\n",
			"sections[0] 'hint' must be text, got int"},
		{"section text is not text", "sections:\n- heading: X\n  text: [a]\n",
			"sections[0] 'text' must be text, got list"},
		{"optional is not a bool", "sections:\n- heading: X\n  optional: yes please\n",
			"sections[0] 'optional' must be true or false"},
		{"word_count is not a mapping", "sections:\n- heading: X\n  word_count: 40\n",
			"sections[0] 'word_count' must be a mapping with 'min' and/or 'max'"},
		{"word_count unknown key", "sections:\n- heading: X\n  word_count: {mni: 4}\n",
			"sections[0] 'word_count' unknown key(s): mni"},
		{"word_count max below min", "sections:\n- heading: X\n  word_count: {min: 9, max: 2}\n",
			"sections[0] 'word_count' 'max' is below 'min'"},
		{"word_count min is a bool", "sections:\n- heading: X\n  word_count: {min: true}\n",
			"sections[0] 'word_count' 'min' must be a non-negative whole number"},
		{"word_count min is negative", "sections:\n- heading: X\n  word_count: {min: -1}\n",
			"sections[0] 'word_count' 'min' must be a non-negative whole number"},
		{"required_text is not a list", "sections:\n- heading: X\n  required_text: nope\n",
			"sections[0] 'required_text' must be a list of literals or {pattern: …}"},
		{"required_text entry is neither", "sections:\n- heading: X\n  required_text: [{p: x}]\n",
			"sections[0] 'required_text' entries are a literal or {pattern: …}"},
		{"required_text empty literal", "sections:\n- heading: X\n  required_text: ['  ']\n",
			"sections[0] 'required_text' has an empty literal"},
		{"forbidden_text pattern is not text", "sections:\n- heading: X\n  forbidden_text: [{pattern: 3}]\n",
			"sections[0] 'forbidden_text' 'pattern' must be a non-empty string"},
		{"forbidden_text pattern will not compile", "sections:\n- heading: X\n  forbidden_text: [{pattern: '('}]\n",
			"sections[0] 'forbidden_text' pattern '(' does not compile: "},
		{"code_blocks is not a list", "sections:\n- heading: X\n  code_blocks: 1\n",
			"sections[0] 'code_blocks' must be a list of {lang, min, max} mappings"},
		{"code_blocks entry is not a mapping", "sections:\n- heading: X\n  code_blocks: [mermaid]\n",
			"sections[0] 'code_blocks'[0] must be a mapping with 'lang', 'min' or 'max'"},
		{"code_blocks unknown key", "sections:\n- heading: X\n  code_blocks: [{language: py}]\n",
			"sections[0] 'code_blocks'[0] unknown key(s): language"},
		{"code_blocks lang is blank", "sections:\n- heading: X\n  code_blocks: [{lang: ' ', min: 1}]\n",
			"sections[0] 'code_blocks'[0] 'lang' must be a non-empty string"},
		{"code_blocks max below min", "sections:\n- heading: X\n  code_blocks: [{min: 3, max: 1}]\n",
			"sections[0] 'code_blocks'[0] 'max' is below 'min'"},
		{"code_blocks rule with no bound at all", "sections:\n- heading: X\n  code_blocks: [{lang: mermaid}]\n",
			"sections[0] 'code_blocks'[0] needs 'min' or 'max'"},
		{"title is not text", "title: 7\nsections: []\n", "'title' must be text, got int"},
		{"hint is not text", "hint: [a, b]\nsections: []\n", "'hint' must be text, got list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTemplate(t, root, "adr", tc.content)
			_, err := LoadTemplate(root, "adr", nil)
			located := mustLocated(t, err, "template_invalid")
			want := "Template for 'adr' (.khub/templates/adr.yaml): " + tc.why
			if !strings.HasPrefix(located.Message, want) {
				t.Errorf("message = %q, want prefix %q", located.Message, want)
			}
		})
	}
}

// The vocabulary that parses: every rule key lands where kb puts it.
func TestSectionRulesParse(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "adr", `title: Decision
hint: one decision per record
sections:
- heading: Context
  optional: true
  word_count: {min: 40, max: 400}
  required_text: ["we chose", {pattern: 'because|since'}]
  forbidden_text: [{pattern: 'robust|scalable'}]
- heading: Diagram
  word_count: {max: 0}
  code_blocks: [{lang: ' Mermaid ', min: 1}, {max: 2}]
`)
	tpl, err := LoadTemplate(root, "adr", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Title != "Decision" || tpl.Hint != "one decision per record" {
		t.Errorf("Title, Hint = %q, %q", tpl.Title, tpl.Hint)
	}
	ctx := tpl.Sections[0]
	if !ctx.Optional || *ctx.MinWords != 40 || *ctx.MaxWords != 400 {
		t.Errorf("Context = %+v", ctx)
	}
	if len(ctx.RequiredText) != 2 || ctx.RequiredText[0].Regex != nil || ctx.RequiredText[1].Regex == nil {
		t.Errorf("RequiredText = %+v", ctx.RequiredText)
	}
	if got := ctx.RequiredText[0].String(); got != "'we chose'" {
		t.Errorf("literal String() = %q", got)
	}
	if got := ctx.ForbiddenText[0].String(); got != "/robust|scalable/" {
		t.Errorf("pattern String() = %q", got)
	}
	dia := tpl.Sections[1]
	if dia.Optional || dia.MinWords != nil || *dia.MaxWords != 0 {
		t.Errorf("Diagram = %+v", dia)
	}
	if len(dia.CodeBlocks) != 2 || dia.CodeBlocks[0].Lang != "Mermaid" || *dia.CodeBlocks[0].Min != 1 ||
		dia.CodeBlocks[0].Max != nil || dia.CodeBlocks[1].Lang != "" || dia.CodeBlocks[1].Min != nil ||
		*dia.CodeBlocks[1].Max != 2 {
		t.Errorf("CodeBlocks = %+v", dia.CodeBlocks)
	}
	if got := dia.CodeBlocks[0].String(); got != "Mermaid block(s)" {
		t.Errorf("CodeRule String() = %q", got)
	}
	if got := dia.CodeBlocks[1].String(); got != "code block(s)" {
		t.Errorf("bare CodeRule String() = %q", got)
	}
	if !ctx.HasRules() || !dia.HasRules() {
		t.Error("HasRules should be true for both")
	}
	if got := tpl.RequiredHeadings(); !reflect.DeepEqual(got, []string{"Diagram"}) {
		t.Errorf("RequiredHeadings = %v", got)
	}
}

func TestPaddedHeadingAndBareText(t *testing.T) {
	// Heading whitespace strips; text without a trailing newline gains one.
	// Render output captured from Python.
	root := t.TempDir()
	writeTemplate(t, root, "x", "sections:\n  - heading: '  Padded  '\n    text: 'no newline'\n")

	tpl, err := LoadTemplate(root, "x", nil)
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
