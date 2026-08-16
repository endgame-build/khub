package index

// Python str.casefold() — full Unicode case folding (ß → ss), which
// strings.ToLower does NOT do (go-port-plan R6). Used for slug matching in
// canonical_slug and resolve_target only; slugify's lower step stays ToLower.

import "golang.org/x/text/cases"

var folder = cases.Fold()

func casefold(s string) string { return folder.String(s) }
