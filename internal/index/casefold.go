package index

// Python str.casefold() — full Unicode case folding (ß → ss), which
// strings.ToLower does NOT do. Used for slug matching in
// canonical_slug and resolve_target only; slugify's lower step stays ToLower.

import "golang.org/x/text/cases"

var folder = cases.Fold()

func casefold(s string) string { return folder.String(s) }

// Casefold folds s the way the index matches slugs and aliases, for callers
// that must group names exactly as lookup does.
func Casefold(s string) string { return casefold(s) }
