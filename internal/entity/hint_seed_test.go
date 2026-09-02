package entity

// The kb 0.14.0 seeding rules at `add`: a template renders whenever it says
// anything (a top-level hint alone is a scaffold), and --no-template is
// refused only where the template requires headings.

import (
	"path/filepath"
	"strings"
	"testing"
)

// kb test_a_top_level_hint_is_how_a_type_with_no_headings_still_guides.
func TestTopLevelHintSeedsAHeadinglessBody(t *testing.T) {
	ws := templatedWS(t)
	writeFile(t, filepath.Join(ws, ".khub", "templates", "note.yaml"),
		"hint: one sentence, present tense\nsections: []\n")
	res, err := Create(ws, "note", CreateOpts{Fields: fields("title", "Guided"), UseTemplate: true})
	requireNoError(t, err)
	body := strings.SplitN(readFile(t, res.Path), "---\n", 3)[2]
	if strings.TrimSpace(body) != "<!-- one sentence, present tense -->" {
		t.Fatalf("body = %q", body)
	}
}

// --no-template opts out of a scaffold nothing requires: optional sections
// and a hint are guidance, not a contract validate would reject.
func TestNoTemplateAllowedWhenNothingIsRequired(t *testing.T) {
	ws := templatedWS(t)
	writeFile(t, filepath.Join(ws, ".khub", "templates", "note.yaml"),
		"hint: guidance\nsections:\n  - heading: Summary\n    optional: true\n")
	res, err := Create(ws, "note", CreateOpts{Fields: fields("title", "Bare"), UseTemplate: false})
	requireNoError(t, err)
	if body := strings.SplitN(readFile(t, res.Path), "---\n", 3)[2]; body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}
