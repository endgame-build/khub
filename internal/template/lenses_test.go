// The lens registry — kb's lens tests (`test_lenses_are_filtered_by_frontmatter_
// and_ordered_by_after`, `test_after_moves_only_what_the_edit_meant_to_move`,
// the lens rows of BROKEN_TEMPLATES) at the library level: loading, ordering,
// refusal wording, and the typed scalar comparison `when` rests on.
package template

import (
	"reflect"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

func lensCodes(lenses []Lens) []string {
	out := make([]string, 0, len(lenses))
	for _, l := range lenses {
		out = append(out, l.Code)
	}
	return out
}

func viewCodes(views []LensView) []string {
	out := make([]string, 0, len(views))
	for _, v := range views {
		out = append(out, v.Code)
	}
	return out
}

func front(pairs ...any) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i].(string), pairs[i+1])
	}
	return m
}

// A stable topological sort, one lens per round: `a`, `b after a`, `c` stays
// `a b c` — draining the whole ready set each round would put `c` before `b`,
// and nothing asked for `c` to move. An `after` that points forward pulls the
// followed lens ahead of its follower.
func TestLensesOrderedByAfterStableKahn(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "x", "sections: []\nlenses:\n"+
		"- {code: a, instruction: first}\n"+
		"- {code: b, instruction: second, after: a}\n"+
		"- {code: c, instruction: third}\n")
	tpl, err := LoadTemplate(root, "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := lensCodes(tpl.Lenses); strings.Join(got, " ") != "a b c" {
		t.Fatalf("order = %v, want a b c", got)
	}

	writeTemplate(t, root, "y", "sections: []\nlenses:\n"+
		"- code: measurable\n  after: single\n  instruction: |\n"+
		"    A number, and where it is measured.\n\n    An adjective is not a bound.\n"+
		"- code: single\n  name: One statement\n  instruction: Exactly one thing?\n")
	tpl, err = LoadTemplate(root, "y", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := lensCodes(tpl.Lenses); strings.Join(got, " ") != "single measurable" {
		t.Fatalf("order = %v, want single measurable", got)
	}
	if tpl.Lenses[0].Name != "One statement" || tpl.Lenses[1].Name != "measurable" {
		t.Errorf("names = %q, %q", tpl.Lenses[0].Name, tpl.Lenses[1].Name)
	}
	// A `|` block keeps its paragraphs; only the trailing newline goes.
	if want := "A number, and where it is measured.\n\nAn adjective is not a bound."; tpl.Lenses[1].Instruction != want {
		t.Errorf("instruction = %q, want %q", tpl.Lenses[1].Instruction, want)
	}
}

func TestLensCycleAndUnknownAfterAreRefused(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "x", "sections: []\nlenses:\n"+
		"- {code: a, instruction: b, after: c}\n- {code: c, instruction: d, after: a}\n")
	_, err := LoadTemplate(root, "x", nil)
	located := mustLocated(t, err, "template_invalid")
	if !strings.HasSuffix(located.Message, ": lens 'after' edges form a cycle: a, c") {
		t.Errorf("message = %q", located.Message)
	}

	writeTemplate(t, root, "x", "sections: []\nlenses:\n- {code: a, instruction: b, after: ghost}\n")
	_, err = LoadTemplate(root, "x", nil)
	located = mustLocated(t, err, "template_invalid")
	if !strings.HasSuffix(located.Message, ": lens 'a' follows 'ghost', which this template does not declare") {
		t.Errorf("message = %q", located.Message)
	}
}

// `when: {stat: proposed}` on a type that declares no `stat` reads nil,
// matches nobody, and disables the lens for the life of the template with
// nothing to report it — so it is refused up front, against the type's
// declared fields. The field set is always consulted: nil and empty alike are
// a type declaring nothing, and refuse every `when` field.
func TestLensWhenReadsUndeclaredFieldIsRefused(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "x", "sections: []\nlenses:\n- {code: a, instruction: b, when: {stat: proposed}}\n")
	want := ": lenses[0] 'when' reads 'stat', which this type does not declare"
	for _, tc := range []struct {
		name   string
		fields []string
	}{
		{"a schema without the field", []string{"title", "status", "kind"}},
		{"a schema declaring nothing", []string{}},
		{"nil", nil},
	} {
		_, err := LoadTemplate(root, "x", tc.fields)
		located := mustLocated(t, err, "template_invalid")
		if !strings.HasSuffix(located.Message, want) {
			t.Errorf("%s: message = %q", tc.name, located.Message)
		}
	}
	tpl, err := LoadTemplate(root, "x", []string{"stat"})
	if err != nil || len(tpl.Lenses) != 1 {
		t.Fatalf("a declared field must be accepted: %v %v", tpl, err)
	}
}

// `when` values and frontmatter values meet typed: a bool with a bool, a
// number with a number whatever its Go type, a date with a date or with the
// string spelling its ISO form, a string exactly. A field neither written nor
// defaulted is nil, which only `null` equals. A list or mapping in the
// frontmatter equals no scalar. The declared divergence from kb: a number
// against the text of its digits does not match.
func TestLensAppliesComparesTypedScalars(t *testing.T) {
	cases := []struct {
		name   string
		when   []WhenClause
		front  *omap.Map
		wantOK bool
	}{
		{"bool vs bool", []WhenClause{{"draft", []any{false}}}, front("draft", false), true},
		{"bool mismatch", []WhenClause{{"draft", []any{false}}}, front("draft", true), false},
		// A quoted "false" is text on disk and stays text: it is not the bool.
		{"quoted text is not the bool", []WhenClause{{"draft", []any{false}}}, front("draft", "false"), false},
		{"missing is nil", []WhenClause{{"kind", []any{"functional"}}}, front(), false},
		{"missing is not the bool", []WhenClause{{"draft", []any{false}}}, front(), false},
		{"null matches null", []WhenClause{{"kind", []any{nil}}}, front("kind", nil), true},
		{"null is not a value", []WhenClause{{"kind", []any{nil}}}, front("kind", "x"), false},
		{"int64 vs float64", []WhenClause{{"n", []any{int64(12)}}}, front("n", 12.0), true},
		{"schema-default int vs int64", []WhenClause{{"n", []any{int64(12)}}}, front("n", 12), true},
		{"float vs float", []WhenClause{{"n", []any{0.5}}}, front("n", 0.5), true},
		{"number mismatch", []WhenClause{{"n", []any{int64(12)}}}, front("n", 12.5), false},
		{"big int vs big int", []WhenClause{{"n", []any{canon.BigInt{Literal: "123456789012345678901"}}}},
			front("n", canon.BigInt{Literal: "123456789012345678901"}), true},
		{"big int vs its float", []WhenClause{{"n", []any{canon.BigInt{Literal: "1" + strings.Repeat("0", 20)}}}},
			front("n", 1e20), true},
		// kb matched `12` against the quoted text "12"; typed comparison does not.
		{"int vs numeric text", []WhenClause{{"n", []any{int64(3)}}}, front("n", "3"), false},
		{"date vs date", []WhenClause{{"created", []any{canon.Date{ISO: "2026-01-15"}}}},
			front("created", canon.Date{ISO: "2026-01-15"}), true},
		{"date vs iso text", []WhenClause{{"created", []any{canon.Date{ISO: "2026-01-15"}}}},
			front("created", "2026-01-15"), true},
		{"iso text vs date", []WhenClause{{"created", []any{"2026-01-15"}}},
			front("created", canon.Date{ISO: "2026-01-15"}), true},
		{"datetime vs iso text", []WhenClause{{"at", []any{canon.DateTime{ISO: "2026-01-15T10:00:00"}}}},
			front("at", "2026-01-15T10:00:00"), true},
		{"date mismatch", []WhenClause{{"created", []any{canon.Date{ISO: "2026-01-15"}}}},
			front("created", canon.Date{ISO: "2026-01-16"}), false},
		{"string exact", []WhenClause{{"kind", []any{"constraint"}}}, front("kind", "constraint"), true},
		{"string is case-sensitive", []WhenClause{{"kind", []any{"constraint"}}}, front("kind", "Constraint"), false},
		{"any of several", []WhenClause{{"kind", []any{"non-functional", "constraint"}}}, front("kind", "constraint"), true},
		{"every clause must hold", []WhenClause{{"kind", []any{"constraint"}}, {"draft", []any{false}}},
			front("kind", "constraint", "draft", true), false},
		{"a list equals no scalar", []WhenClause{{"tags", []any{"a"}}}, front("tags", []any{"a"}), false},
		{"a mapping equals no scalar", []WhenClause{{"meta", []any{"a"}}}, front("meta", front("k", "a")), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lens := &Lens{Code: "l", When: tc.when}
			if got := lens.Applies(tc.front); got != tc.wantOK {
				t.Errorf("Applies = %v, want %v", got, tc.wantOK)
			}
		})
	}
}

// The loader hands lensWhen typed values and they are kept as such: a
// template's `when: {draft: false}` holds the bool, `{n: 12}` the int64,
// `{on: 2026-01-15}` the date — never the text.
func TestLensWhenKeepsLoadedValuesTyped(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "x", "sections: []\nlenses:\n"+
		"- {code: a, instruction: b, when: {draft: false, n: [12, 1.5], on: 2026-01-15, kind: constraint}}\n")
	tpl, err := LoadTemplate(root, "x", []string{"draft", "n", "on", "kind"})
	if err != nil {
		t.Fatal(err)
	}
	want := []WhenClause{
		{"draft", []any{false}},
		{"kind", []any{"constraint"}},
		{"n", []any{int64(12), 1.5}},
		{"on", []any{canon.Date{ISO: "2026-01-15"}}},
	}
	if got := tpl.Lenses[0].When; !reflect.DeepEqual(got, want) {
		t.Errorf("When = %#v\n  want %#v", got, want)
	}
}

// ApplicableLenses filters in template (after-resolved) order and renders the
// section as "" when a lens is not pinned to a heading.
func TestApplicableLensesFilterAndOrder(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "x", "sections:\n- heading: Context\nlenses:\n"+
		"- {code: measurable, instruction: m, when: {kind: [non-functional, constraint]}, after: single}\n"+
		"- {code: single, name: One statement, instruction: s, section: Context}\n")
	tpl, err := LoadTemplate(root, "x", []string{"kind"})
	if err != nil {
		t.Fatal(err)
	}
	got := tpl.ApplicableLenses(front("kind", "non-functional"))
	if strings.Join(viewCodes(got), " ") != "single measurable" {
		t.Fatalf("codes = %v", viewCodes(got))
	}
	if got[0].Section != "Context" || got[1].Section != "" {
		t.Errorf("sections = %q, %q", got[0].Section, got[1].Section)
	}
	if got := tpl.ApplicableLenses(front("kind", "functional")); strings.Join(viewCodes(got), " ") != "single" {
		t.Fatalf("codes = %v", viewCodes(got))
	}
}

// kb's BROKEN_TEMPLATES, lens rows: every one is a typo that would otherwise be
// silently ignored, leaving an author to believe a lens was in force.
func TestBrokenLensTemplates(t *testing.T) {
	cases := []struct{ name, yaml, why string }{
		{"lenses is not a list", "sections: []\nlenses: nope\n", "'lenses' must be a list"},
		{"lens without a code", "sections: []\nlenses:\n- instruction: look\n",
			"lenses[0] needs a non-empty string 'code'"},
		{"lens without an instruction", "sections: []\nlenses:\n- code: one\n",
			"lenses[0] needs a non-empty string 'instruction'"},
		{"lens is not a mapping", "sections: []\nlenses:\n- just text\n",
			"lenses[0] must be a mapping with 'code' and 'instruction'"},
		{"lens unknown key", "sections: []\nlenses:\n- {code: a, instruction: b, colour: red}\n",
			"lenses[0] unknown key(s): colour"},
		{"lens code repeated", "sections: []\nlenses:\n- {code: a, instruction: b}\n- {code: a, instruction: c}\n",
			"lenses[1] repeats the lens code 'a'"},
		{"lens name is not text", "sections: []\nlenses:\n- {code: a, instruction: b, name: 7}\n",
			"lenses[0] 'name' must be text"},
		{"lens section is not text", "sections: []\nlenses:\n- {code: a, instruction: b, section: 7}\n",
			"lenses[0] 'section' must be text"},
		{"lens follows a code nothing declares", "sections: []\nlenses:\n- {code: a, instruction: b, after: ghost}\n",
			"lens 'a' follows 'ghost', which this template does not declare"},
		{"lens after is not a code", "sections: []\nlenses:\n- {code: a, instruction: b, after: [7]}\n",
			"lenses[0] 'after' must name lens codes"},
		{"lens after edges form a cycle",
			"sections: []\nlenses:\n- {code: a, instruction: b, after: c}\n- {code: c, instruction: d, after: a}\n",
			"lens 'after' edges form a cycle: a, c"},
		{"lens names a section nothing declares", "sections: []\nlenses:\n- {code: a, instruction: b, section: Ghost}\n",
			"lenses[0] 'section' names 'Ghost', which this template does not declare"},
		{"lens when is empty", "sections: []\nlenses:\n- {code: a, instruction: b, when: {}}\n",
			"lenses[0] 'when' must be a non-empty field: value mapping"},
		{"lens when is not a mapping", "sections: []\nlenses:\n- {code: a, instruction: b, when: kind}\n",
			"lenses[0] 'when' must be a non-empty field: value mapping"},
		{"lens when lists no value", "sections: []\nlenses:\n- {code: a, instruction: b, when: {kind: []}}\n",
			"lenses[0] 'when.kind' lists no value"},
		{"lens when reads a field nothing declares",
			"sections: []\nlenses:\n- {code: a, instruction: b, when: {stat: proposed}}\n",
			"lenses[0] 'when' reads 'stat', which this type does not declare"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTemplate(t, root, "x", tc.yaml)
			_, err := LoadTemplate(root, "x", []string{"title", "kind"})
			located := mustLocated(t, err, "template_invalid")
			want := "Template for 'x' (.khub/templates/x.yaml): " + tc.why
			if located.Message != want {
				t.Errorf("message = %q\n   want %q", located.Message, want)
			}
		})
	}
}

// A lens registry on a template with no sections is the way a type with no
// heading contract still gets review questions; a null registry is none.
func TestLensesAbsentOrNull(t *testing.T) {
	root := t.TempDir()
	writeTemplate(t, root, "x", "sections: []\n")
	tpl, err := LoadTemplate(root, "x", nil)
	if err != nil || len(tpl.Lenses) != 0 {
		t.Fatalf("absent: %v %v", tpl, err)
	}
	writeTemplate(t, root, "x", "sections: []\nlenses:\n")
	tpl, err = LoadTemplate(root, "x", nil)
	if err != nil || len(tpl.Lenses) != 0 {
		t.Fatalf("null: %v %v", tpl, err)
	}
}
