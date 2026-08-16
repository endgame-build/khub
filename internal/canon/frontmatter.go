package canon

// The ~15-line frontmatter splitter the memo §4 calls for — a port of
// formats._split_frontmatter with its exact error strings, plus the md
// renderer (formats.render md arm). Splitting is the ONLY frontmatter
// dependency khub has; every Go frontmatter library is parse-only.

import (
	"fmt"
	"regexp"
	"strings"
)

// fmBoundary is python-frontmatter's YAMLHandler.FM_BOUNDARY: a run of three
// or more dashes on its own line (trailing whitespace allowed). Note this is
// LOOSER than SplitFrontmatter's strict "---": `----` and `--- ` open a fence.
// The trailing class is Python's \s (space, tab, CR, FF, VT); the newline
// that \s also covers is what `$` anchors against. A bare [ \t] silently
// made every CRLF entity invisible to Go while Python read it fine.
var fmBoundary = regexp.MustCompile(`(?m)^-{3,}[ 	\f\v\r]*$`)

// ParseFrontmatter is the SCAN-altitude md read: a port of python-frontmatter's
// parse(), which khub's formats.parse uses for md. It never fails on a missing
// or unterminated fence — a fenceless file is ({}, text), which is what makes
// a loose .md inside a layout a STRAY rather than a malformed entity. Only a
// fence that split and whose YAML is invalid raises.
//
// Distinct from SplitFrontmatter, the EDIT-altitude split (formats.read_doc),
// which is strict and whose error strings are contract. Python keeps the same
// two altitudes.
func ParseFrontmatter(text string) (yamlText, body string, hasFence bool) {
	// No BOM strip here: python-frontmatter does not tolerate one, so a
	// BOM-prefixed file simply has no fence at position 0 and reads as a
	// stray. SplitFrontmatter (the edit altitude) DOES strip it, because
	// khub's own _split_frontmatter deliberately does.
	work := strings.TrimSpace(text)
	loc := fmBoundary.FindStringIndex(work)
	if loc == nil || loc[0] != 0 {
		return "", work, false // detect_format found no handler
	}
	// FM_BOUNDARY.split(text, 2) must yield 3 parts; fewer is a ValueError
	// python-frontmatter swallows into ({}, text).
	rest := work[loc[1]:]
	second := fmBoundary.FindStringIndex(rest)
	if second == nil {
		return "", work, false
	}
	return rest[:second[0]], strings.TrimSpace(rest[second[1]:]), true
}

// SplitFrontmatter returns (yamlText, body). Tolerates a UTF-8 BOM and
// leading blank lines; the fence itself is strict "---" lines.
func SplitFrontmatter(text string) (string, string, error) {
	work := strings.TrimPrefix(text, "\uFEFF")
	lines := strings.Split(work, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || lines[start] != "---" {
		head := []rune(work)
		if len(head) > 20 {
			head = head[:20]
		}
		return "", "", fmt.Errorf("No frontmatter fence in %s", pyRepr(string(head)))
	}
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "---" {
			return strings.Join(lines[start+1:i], "\n") + "\n", strings.Join(lines[i+1:], "\n"), nil
		}
	}
	return "", "", fmt.Errorf("Unterminated frontmatter")
}

// RenderMD reassembles a markdown document from frontmatter YAML text
// (must end with "\n") and the body: "---\n<yaml>---\n<body>".
func RenderMD(yamlText, body string) string {
	return "---\n" + yamlText + "---\n" + body
}

// pyRepr renders a string the way Python repr does inside khub's error
// messages (single-quoted, \n escapes) — the message text is contract.
func pyRepr(s string) string {
	var b strings.Builder
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
		quote = '"'
	}
	b.WriteByte(quote)
	for _, ch := range s {
		switch ch {
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\\':
			b.WriteString(`\\`)
		case rune(quote):
			b.WriteString(`\` + string(quote))
		default:
			if ch < 0x20 {
				fmt.Fprintf(&b, `\x%02x`, ch)
			} else {
				b.WriteRune(ch)
			}
		}
	}
	b.WriteByte(quote)
	return b.String()
}
