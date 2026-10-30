package mdlink

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

var (
	// A link reference definition line: `[label]: dest`, indented up to three
	// spaces, then the destination (`<…>` or a run of non-space bytes) and the
	// rest of the line. A label opening with `^` is a footnote (`[^1]: text`),
	// whose first word is prose, not a destination.
	refDefRE = regexp.MustCompile(`^ {0,3}\[([^\]\n^][^\]\n]*)\]:[ \t]*(<[^>\n]*>|\S+)(.*)$`)
	// What may follow a definition's destination: nothing, or a title in
	// double quotes, single quotes or parentheses. Anything else means the
	// line is prose that happens to open with `[x]:` (`[Note]: see below.`).
	refTailRE = regexp.MustCompile(`^(?:[ \t]+(?:"[^"]*"|'[^']*'|\([^)]*\)))?[ \t]*$`)
	// A URI scheme (RFC 3986): `https:`, `mailto:`, `file:`. A Windows drive
	// (`C:`) matches too, and is no relative link either.
	schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	// A list item's opening: a bullet or an ordered marker, then a space.
	listItemRE = regexp.MustCompile(`^ {0,3}(?:[-+*]|\d{1,9}[.)])(?:[ \t]|$)`)
	// An ATX heading line. A definition may follow one directly: a heading is
	// not a paragraph a definition would be continuing.
	headingRE = regexp.MustCompile(`^ {0,3}#{1,6}(?:[ \t]|$)`)
)

// Link is one relative link destination found in a body: Dest is the
// destination exactly as written, Path the file part a reader follows —
// fragment and query dropped, backslash escapes and percent-escapes decoded,
// `/`-separated and lexically cleaned (`./a.md` and `sub/../a.md` are both
// `a.md`). A backslash that escapes nothing stays an ordinary character, on
// every platform.
type Link struct {
	Dest string
	Path string
}

// RelativeLinks lists the relative file links of a Markdown text, one per
// distinct Path (the first destination that names it), in order of first
// appearance: the destinations of inline links and images
// (`[t](dest)`, `![a](dest)`) and of reference definitions (`[l]: dest`).
//
// The caller passes the text with fenced code and HTML comments already
// blanked; indented code blocks and code spans are blanked here. A
// destination with a scheme, one starting with `/` (root-relative: relative
// to what a renderer serves, not to anything khub can see), and a bare
// `#fragment` are not relative file links and are left out.
//
// Deliberately smaller than CommonMark: a destination on the line after
// `](`, and a definition inside a blockquote or a list item, are not read.
func RelativeLinks(text string) []Link {
	text = maskCodeSpans(maskIndentedCode(text))
	hits := inlineHits(text)
	var defs []inlineHit
	offset := 0
	prevOpen := true // start of text, a blank line, a heading or another definition
	for _, line := range strings.Split(text, "\n") {
		blank := strings.TrimSpace(line) == ""
		isDef := false
		if !blank && prevOpen {
			if m := refDefRE.FindStringSubmatch(line); m != nil && refTailRE.MatchString(m[3]) {
				defs = append(defs, inlineHit{offset, m[2]})
				isDef = true
			}
		}
		prevOpen = blank || isDef || headingRE.MatchString(line)
		offset += len(line) + 1
	}
	// Merge: both lists are already in offset order.
	merged := make([]inlineHit, 0, len(hits)+len(defs))
	i, j := 0, 0
	for i < len(hits) || j < len(defs) {
		if j == len(defs) || (i < len(hits) && hits[i].at < defs[j].at) {
			merged = append(merged, hits[i])
			i++
		} else {
			merged = append(merged, defs[j])
			j++
		}
	}
	var out []Link
	seen := map[string]bool{}
	for _, h := range merged {
		dest := h.dest
		if strings.HasPrefix(dest, "<") && strings.HasSuffix(dest, ">") {
			dest = dest[1 : len(dest)-1]
		}
		l, ok := relative(dest)
		if !ok || seen[l.Path] {
			continue
		}
		seen[l.Path] = true
		out = append(out, l)
	}
	return out
}

// inlineHit is one destination and the offset it was found at (the `]` of
// an inline link, the line start of a definition), for ordering.
type inlineHit struct {
	at   int
	dest string
}

// inlineHits finds `[text](dest)` and `![alt](dest)`: a `]` that closes an
// unescaped `[` and is followed by `(`. Brackets escaped by an odd run of
// backslashes are text, and a blank line closes every open bracket — link
// text never spans paragraphs.
func inlineHits(text string) []inlineHit {
	var hits []inlineHit
	var open []int
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			if rest := text[i+1:]; strings.TrimLeft(firstLine(rest), " \t\r") == "" {
				open = open[:0]
			}
		case '[':
			if !escaped(text, i) {
				open = append(open, i)
			}
		case ']':
			if escaped(text, i) || len(open) == 0 {
				continue
			}
			open = open[:len(open)-1]
			if i+1 < len(text) && text[i+1] == '(' {
				if dest, ok := inlineDest(text[i+2:]); ok {
					hits = append(hits, inlineHit{i, dest})
				}
			}
		}
	}
	return hits
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// escaped reports whether the byte at i is preceded by an odd run of
// backslashes.
func escaped(s string, i int) bool {
	n := 0
	for k := i - 1; k >= 0 && s[k] == '\\'; k-- {
		n++
	}
	return n%2 == 1
}

// inlineDest reads an inline link destination from the text just past `](`:
// `<…>` up to its `>`, or a run up to whitespace or the `)` that balances the
// opening one. ok is false for an empty or unterminated destination, and for
// one that would continue on the next line.
func inlineDest(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t")
	if strings.HasPrefix(s, "<") {
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case '\n', '<':
				return "", false
			case '>':
				return s[:i+1], i > 1
			}
		}
		return "", false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s):
			i++
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 {
				return s[:i], i > 0
			}
			depth--
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			return s[:i], i > 0
		}
	}
	return "", false
}

// relative reports whether dest is a relative file link and, if so, the path
// a reader follows.
func relative(dest string) (Link, bool) {
	if dest == "" || strings.HasPrefix(dest, "/") || schemeRE.MatchString(dest) {
		return Link{}, false
	}
	p := dest
	if i := strings.IndexAny(p, "#?"); i >= 0 {
		p = p[:i]
	}
	if p == "" {
		return Link{}, false // `#section`: a link within this document
	}
	p = unescapePunct(p)
	if decoded, err := url.PathUnescape(p); err == nil {
		p = decoded
	}
	return Link{Dest: dest, Path: path.Clean(p)}, true
}

// unescapePunct drops the backslash of a CommonMark backslash escape — one
// before ASCII punctuation — so `esc\(p\).md` names `esc(p).md`. Any other
// backslash is a literal character.
func unescapePunct(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isASCIIPunct(c byte) bool {
	return (c >= '!' && c <= '/') || (c >= ':' && c <= '@') ||
		(c >= '[' && c <= '`') || (c >= '{' && c <= '~')
}

// maskIndentedCode blanks an indented code block: a line indented four
// spaces (or a tab) that follows a blank line or another such line, outside
// a list. Inside a list the same indent is usually the item's own
// continuation, so it is read as prose. Length-preserving, like every mask.
func maskIndentedCode(text string) string {
	lines := strings.Split(text, "\n")
	prevBlank, inCode, inList := true, false, false
	for i, line := range lines {
		blank := strings.TrimSpace(line) == ""
		indented := strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
		switch {
		case blank:
			// a blank line neither opens nor closes a block on its own
		case indented && (inCode || prevBlank) && !inList:
			inCode = true
			lines[i] = strings.Repeat(" ", len(line))
		case indented:
			inCode = false
		default:
			inCode = false
			inList = listItemRE.MatchString(line) || (inList && !prevBlank)
		}
		prevBlank = blank
	}
	return strings.Join(lines, "\n")
}

// maskCodeSpans blanks every inline code span (a backtick run, then the
// first later run of the same length), keeping byte length: a link written
// inside one is an example, not a link. An unmatched run is literal text.
func maskCodeSpans(text string) string {
	buf := []byte(text)
	for i := 0; i < len(buf); {
		if buf[i] != '`' {
			i++
			continue
		}
		n := runLen(buf, i)
		closeAt := -1
		for j := i + n; j < len(buf); {
			if buf[j] != '`' {
				j++
				continue
			}
			m := runLen(buf, j)
			if m == n {
				closeAt = j
				break
			}
			j += m
		}
		if closeAt < 0 {
			i += n
			continue
		}
		for k := i; k < closeAt+n; k++ {
			if buf[k] != '\n' {
				buf[k] = ' '
			}
		}
		i = closeAt + n
	}
	return string(buf)
}

func runLen(buf []byte, at int) int {
	n := 0
	for at+n < len(buf) && buf[at+n] == '`' {
		n++
	}
	return n
}
