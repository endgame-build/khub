package index

// Python str.casefold() — full Unicode case folding (ß → ss), which
// strings.ToLower does NOT do. Used for slug matching in
// canonical_slug and resolve_target only; slugify's lower step stays ToLower.

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// casefold composes s (NFC) before folding, so a name decomposed the way a
// macOS file name is (NFD) matches its composed spelling. A fresh Caser per
// call: a Caser keeps state and must not be shared between goroutines.
func casefold(s string) string { return cases.Fold().String(norm.NFC.String(s)) }

// Casefold folds s the way the index matches slugs and aliases, for callers
// that must group names exactly as lookup does.
func Casefold(s string) string { return casefold(s) }
