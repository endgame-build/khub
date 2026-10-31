package index

import "testing"

// casefold is Python's str.casefold(), not strings.ToLower: full Unicode
// folding, so ß matches SS and a dotted capital I matches its lowercase
// form. Slug and alias lookup go through it; slugify's own lower step does
// not, which is why "ß İstanbul Café" mints req-i-stanbul-caf and still
// resolves by any casing of that id. This file pins the fold itself.
func TestCasefoldIsFullUnicodeFolding(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Straße", "strasse"},
		{"STRASSE", "strasse"},
		{"İstanbul", "i̇stanbul"},
		{"CAFÉ", "café"},
		{"req-Pay-By-Card", "req-pay-by-card"},
		{"", ""},
	}
	for _, c := range cases {
		if got := casefold(c.in); got != c.want {
			t.Errorf("casefold(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := Casefold(c.in); got != casefold(c.in) {
			t.Errorf("Casefold(%q) = %q, differs from the unexported fold", c.in, got)
		}
	}
	// Folding is idempotent and two spellings that fold the same compare
	// equal after one pass — the property lookup relies on.
	for _, s := range []string{"Straße", "STRASSE", "straſse"} {
		if casefold(casefold(s)) != casefold(s) {
			t.Errorf("casefold is not idempotent on %q", s)
		}
	}
	// What it does NOT do: a decomposed é (e + U+0301) and a composed é are
	// different strings to the fold. Normalisation is nobody's job yet
	// (.claude/rules/testing.md, "Unicode filesystems"); this pins the
	// current contract so a change to it is deliberate.
	if casefold("Café") == casefold("Café") {
		t.Error("casefold now normalises NFD to NFC; update smoke.sh's unicode block and this test")
	}
}
