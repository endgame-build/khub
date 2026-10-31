package index

import "testing"

// Lookup folds full Unicode case (ß → ss, not just lower) and composes first,
// so a decomposed name (macOS NFD) matches its composed spelling (NFC).
func TestCasefoldFoldsCaseAndComposition(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"Straße", "STRASSE"},
		{"Café", "café"}, // NFC é vs e + combining acute
		{"ÅNGSTRÖM", "ångström"},
	} {
		if casefold(c.a) != casefold(c.b) {
			t.Errorf("casefold(%q) = %q, casefold(%q) = %q", c.a, casefold(c.a), c.b, casefold(c.b))
		}
	}
}
