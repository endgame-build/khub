package entity

import "testing"

// A minted slug spells Latin accents out, so a title arriving composed (NFC)
// or decomposed (NFD) mints one slug; other scripts collapse as before.
func TestSlugifySpellsLatinAccents(t *testing.T) {
	for in, want := range map[string]string{
		"Café":             "cafe",
		"Café":            "cafe", // NFD
		"Résumé Über":      "resume-uber",
		"Straße Ærø Œuvre": "strasse-aero-oeuvre",
		"Łódź Þing Ðoor":   "lodz-thing-door",
		"Naïve façade №5":  "naive-facade-5",
		"Привет мир":       "",
		"plain ascii 2026": "plain-ascii-2026",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
