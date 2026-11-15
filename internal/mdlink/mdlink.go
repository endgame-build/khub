// Package mdlink renders and reads markdown link destinations. One home for the
// CommonMark angle-bracket escape, shared by every surface that emits a
// file link (reindex's index.md, wire's managed block), so a path that one
// surface renders correctly cannot break in another — and the reader of
// them: RelativeLinks finds the relative destinations a body links to, for
// validate's dead-link gaps.
package mdlink

import "strings"

// LinkDest returns p as a markdown link destination: a path carrying a space
// or parenthesis — legal in a storage path, fatal in a bare destination —
// is wrapped in angle brackets (the CommonMark escape); a clean path stays
// unadorned so the common case reads plainly.
func LinkDest(p string) string {
	if strings.ContainsAny(p, " ()") {
		return "<" + p + ">"
	}
	return p
}
