package explore

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/endgame-build/khub/internal/omap"
)

// The block tree is how an entity body reaches the page: a JSON tree of a
// dozen node kinds that the page (ui/blocks.js) builds element by
// element through textContent. The page never parses Markdown and never
// receives markup, so a body cannot inject any — the build gate that bans the
// markup sinks stays satisfiable, and the "content cannot run" guarantee lives
// at the same choke point as every other string on the page.
//
// Kinds: p, h, ul, ol, li, pre, quote, hr, text, em, strong, code, a, br.

// md is the CommonMark parser: no extensions, no HTML rendering — only the
// AST is used.
var md = goldmark.New()

// schemeRE matches a URL scheme, as CommonMark and browsers read one.
var schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:`)

// linkResolver turns a relative link destination into the id of the entity it
// names, when it names one.
type linkResolver func(dest string) (string, bool)

// BodyTree parses body into the block tree with no entity resolution: every
// relative link becomes plain text. Used where no index is at hand.
func BodyTree(body string) []any { return bodyTree(body, nil) }

func bodyTree(body string, resolve linkResolver) []any {
	if strings.TrimSpace(body) == "" {
		return []any{}
	}
	src := []byte(body)
	doc := md.Parser().Parse(text.NewReader(src))
	w := &treeWriter{src: src, resolve: resolve}
	return w.blocks(doc)
}

type treeWriter struct {
	src     []byte
	resolve linkResolver
}

func block(kind string) *omap.Map {
	m := omap.New()
	m.Set("k", kind)
	return m
}

func textBlock(kind, s string) *omap.Map {
	m := block(kind)
	m.Set("text", s)
	return m
}

func withChildren(m *omap.Map, children []any) *omap.Map {
	m.Set("c", children)
	return m
}

// blocks renders the block-level children of n.
func (w *treeWriter) blocks(n ast.Node) []any {
	out := []any{}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if b := w.block(c); b != nil {
			out = append(out, b)
		}
	}
	return out
}

func (w *treeWriter) block(n ast.Node) *omap.Map {
	switch v := n.(type) {
	case *ast.Paragraph:
		return withChildren(block("p"), w.inlines(v))
	case *ast.TextBlock:
		// A tight list item's text: no paragraph of its own.
		return withChildren(block("p"), w.inlines(v))
	case *ast.Heading:
		h := block("h")
		h.Set("level", v.Level)
		return withChildren(h, w.inlines(v))
	case *ast.List:
		kind := "ul"
		if v.IsOrdered() {
			kind = "ol"
		}
		return withChildren(block(kind), w.blocks(v))
	case *ast.ListItem:
		return withChildren(block("li"), w.blocks(v))
	case *ast.Blockquote:
		return withChildren(block("quote"), w.blocks(v))
	case *ast.ThematicBreak:
		return block("hr")
	case *ast.FencedCodeBlock:
		return textBlock("pre", w.lines(v.Lines()))
	case *ast.CodeBlock:
		return textBlock("pre", w.lines(v.Lines()))
	case *ast.HTMLBlock:
		// Raw HTML is shown, never rendered: its source arrives as text.
		raw := w.lines(v.Lines())
		if v.HasClosure() {
			raw += string(v.ClosureLine.Value(w.src))
		}
		return withChildren(block("p"), []any{textBlock("text", strings.TrimRight(raw, "\n"))})
	case *ast.LinkReferenceDefinition:
		return nil // consumed by the links that use it
	default:
		// An unknown block keeps its children rather than vanishing.
		if n.HasChildren() {
			if n.FirstChild().Type() == ast.TypeInline {
				return withChildren(block("p"), w.inlines(n))
			}
			return withChildren(block("quote"), w.blocks(n))
		}
		return nil
	}
}

func (w *treeWriter) lines(segs *text.Segments) string {
	var b bytes.Buffer
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		b.Write(seg.Value(w.src))
	}
	return b.String()
}

// inlines renders the inline children of n, merging adjacent text runs.
func (w *treeWriter) inlines(n ast.Node) []any {
	var out []any
	push := func(m *omap.Map) {
		if k, _ := m.Get("k"); k == "text" && len(out) > 0 {
			if prev, ok := out[len(out)-1].(*omap.Map); ok {
				if pk, _ := prev.Get("k"); pk == "text" {
					a, _ := prev.Get("text")
					b, _ := m.Get("text")
					prev.Set("text", a.(string)+b.(string))
					return
				}
			}
		}
		out = append(out, m)
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.inline(c, push)
	}
	if out == nil {
		return []any{}
	}
	return out
}

func (w *treeWriter) inline(n ast.Node, push func(*omap.Map)) {
	switch v := n.(type) {
	case *ast.Text:
		push(textBlock("text", string(v.Value(w.src))))
		switch {
		case v.HardLineBreak():
			push(block("br"))
		case v.SoftLineBreak():
			push(textBlock("text", " "))
		}
	case *ast.String:
		push(textBlock("text", string(v.Value)))
	case *ast.CodeSpan:
		push(textBlock("code", w.plain(v)))
	case *ast.Emphasis:
		kind := "em"
		if v.Level >= 2 {
			kind = "strong"
		}
		push(withChildren(block(kind), w.inlines(v)))
	case *ast.Link:
		push(withChildren(w.anchor(string(v.Destination)), w.inlines(v)))
	case *ast.AutoLink:
		a := w.anchor(string(v.URL(w.src)))
		push(withChildren(a, []any{textBlock("text", string(v.Label(w.src)))}))
	case *ast.Image:
		// Never fetched: the page loads nothing from anywhere. The alt text
		// stands in, linked when the destination is one a link may carry.
		alt := w.plain(v)
		if alt == "" {
			alt = "image"
		}
		push(withChildren(w.anchor(string(v.Destination)), []any{textBlock("text", "["+alt+"]")}))
	case *ast.RawHTML:
		var b bytes.Buffer
		for i := 0; i < v.Segments.Len(); i++ {
			seg := v.Segments.At(i)
			b.Write(seg.Value(w.src))
		}
		push(textBlock("text", b.String()))
	default:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			w.inline(c, push)
		}
	}
}

// anchor builds an `a` block. href survives only for http, https and mailto;
// a relative destination that names a workspace entity becomes `entity`;
// anything else (javascript:, data:, an absolute or in-page path) is dropped,
// and the page renders the link's text without a link.
func (w *treeWriter) anchor(dest string) *omap.Map {
	a := block("a")
	dest = strings.TrimSpace(dest)
	href, entity := "", ""
	if m := schemeRE.FindString(dest); m != "" {
		switch strings.ToLower(m) {
		case "http:", "https:", "mailto:":
			href = dest
		}
	} else if w.resolve != nil && dest != "" && !strings.HasPrefix(dest, "/") && !strings.HasPrefix(dest, "#") {
		if id, ok := w.resolve(dest); ok {
			entity = id
		}
	}
	a.Set("href", href)
	if entity != "" {
		a.Set("entity", entity)
	}
	return a
}

// plain is the text content of an inline subtree, with no markup.
func (w *treeWriter) plain(n ast.Node) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch v := c.(type) {
			case *ast.Text:
				b.Write(v.Value(w.src))
				if v.SoftLineBreak() || v.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(v.Value)
			case *ast.AutoLink:
				b.Write(v.Label(w.src))
			case *ast.RawHTML:
				for i := 0; i < v.Segments.Len(); i++ {
					seg := v.Segments.At(i)
					b.Write(seg.Value(w.src))
				}
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}
