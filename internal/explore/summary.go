package explore

import (
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/values"
)

// summaryLimit caps a summary, in characters, before the ellipsis.
const summaryLimit = 200

// Summary is the one-line summary a neighbour carries: the description
// attribute when set, otherwise the first paragraph of the body that is not a
// heading, as plain text with whitespace collapsed, cut at summaryLimit
// characters on a word boundary with "…"; otherwise "".
//
// The body is the Markdown body for an md entity and the reserved `body` field
// for a json, yaml or collection one — the index holds either the same way, so
// the rule does not branch on storage.
func Summary(meta *omap.Map, body string) string {
	if meta != nil {
		if d, ok := meta.Get("description"); ok && values.Truthy(d) {
			if s := clip(collapse(values.Str(d))); s != "" {
				return s
			}
		}
	}
	return clip(collapse(firstParagraph(body)))
}

// firstParagraph is the plain text of the body's first top-level paragraph.
// A heading, a list, code or a quote before it is skipped, not summarized.
func firstParagraph(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	src := []byte(body)
	doc := md.Parser().Parse(text.NewReader(src))
	w := &treeWriter{src: src}
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		if p, ok := c.(*ast.Paragraph); ok {
			if s := w.plain(p); strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

// collapse folds every run of whitespace to one space and trims the ends.
func collapse(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// clip cuts s to summaryLimit characters, backing up to the last word boundary
// when one is near enough to keep most of the text.
func clip(s string) string {
	r := []rune(s)
	if len(r) <= summaryLimit {
		return s
	}
	cut := summaryLimit
	for i := summaryLimit; i > summaryLimit/2; i-- {
		if unicode.IsSpace(r[i]) {
			cut = i
			break
		}
	}
	return strings.TrimRightFunc(string(r[:cut]), unicode.IsSpace) + "…"
}
