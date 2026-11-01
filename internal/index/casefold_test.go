package index

import "testing"

// casefold is Python's str.casefold(), not strings.ToLower: full Unicode
// folding, so ß matches SS and a dotted capital I matches its lowercase
// form. It composes first, so a decomposed name (macOS NFD) matches its
// composed spelling (NFC). Slug and alias lookup go through it.
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
	for _, c := range []struct{ a, b string }{
		{"Café", "Cafe\u0301"}, // NFC é vs e + combining acute
		{"ÅNGSTRÖM", "ångström"},
	} {
		if casefold(c.a) != casefold(c.b) {
			t.Errorf("casefold(%q) = %q, casefold(%q) = %q", c.a, casefold(c.a), c.b, casefold(c.b))
		}
	}
}
