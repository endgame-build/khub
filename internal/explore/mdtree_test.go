package explore

import (
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

// kinds lists every node kind in a tree, depth first.
func kinds(tree []any) []string {
	var out []string
	var walk func([]any)
	walk = func(nodes []any) {
		for _, n := range nodes {
			m := n.(*omap.Map)
			k, _ := m.Get("k")
			out = append(out, k.(string))
			if c, ok := m.Get("c"); ok {
				walk(c.([]any))
			}
		}
	}
	walk(tree)
	return out
}

// find returns every node of one kind.
func find(tree []any, kind string) []*omap.Map {
	var out []*omap.Map
	var walk func([]any)
	walk = func(nodes []any) {
		for _, n := range nodes {
			m := n.(*omap.Map)
			if k, _ := m.Get("k"); k == kind {
				out = append(out, m)
			}
			if c, ok := m.Get("c"); ok {
				walk(c.([]any))
			}
		}
	}
	walk(tree)
	return out
}

func str(m *omap.Map, key string) string {
	v, _ := m.Get(key)
	s, _ := v.(string)
	return s
}

func TestBodyTreeEveryKind(t *testing.T) {
	body := strings.Join([]string{
		"# One", "", "###### Six", "",
		"Para with *em*, **strong**, `code`, a [link](https://ok.test) and a  ",
		"hard break.", "",
		"- tight", "- list", "",
		"1. ordered", "", "> quoted", "", "---", "",
		"```", "fenced <b>code</b>", "```",
	}, "\n")
	tree := BodyTree(body)
	got := map[string]bool{}
	for _, k := range kinds(tree) {
		got[k] = true
	}
	for _, k := range []string{"p", "h", "ul", "ol", "li", "pre", "quote", "hr", "text", "em", "strong", "code", "a", "br"} {
		if !got[k] {
			t.Errorf("kind %q never produced; got %v", k, kinds(tree))
		}
	}
	hs := find(tree, "h")
	if len(hs) != 2 {
		t.Fatalf("headings: %d", len(hs))
	}
	for i, want := range []int{1, 6} {
		if lv, _ := hs[i].Get("level"); lv != want {
			t.Errorf("heading %d level %v, want %d", i, lv, want)
		}
	}
	if pre := find(tree, "pre"); len(pre) != 1 || str(pre[0], "text") != "fenced <b>code</b>\n" {
		t.Errorf("pre = %v", pre)
	}
	if _, err := canon.EncodeCLI(tree); err != nil {
		t.Fatal(err)
	}
}

// Raw HTML, block and inline, arrives as text — shown, never rendered.
func TestBodyTreeRawHTMLIsText(t *testing.T) {
	tree := BodyTree("<script>alert(1)</script>\n\nHi <img src=x onerror=alert(2)> there.\n")
	var texts []string
	for _, n := range find(tree, "text") {
		texts = append(texts, str(n, "text"))
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "<script>alert(1)</script>") || !strings.Contains(joined, "<img src=x onerror=alert(2)>") {
		t.Fatalf("raw html must survive as text, got %q", joined)
	}
	for _, k := range kinds(tree) {
		switch k {
		case "p", "text":
		default:
			t.Errorf("unexpected kind %q from raw html", k)
		}
	}
}

// Only http, https and mailto survive as href; every other scheme, an
// absolute path and an in-page anchor are dropped to plain text.
func TestBodyTreeLinkAllowList(t *testing.T) {
	cases := map[string]string{
		"https://ok.test/a":   "https://ok.test/a",
		"http://ok.test":      "http://ok.test",
		"mailto:a@b.test":     "mailto:a@b.test",
		"javascript:alert(1)": "",
		"JavaScript:alert(1)": "",
		"data:text/html,x":    "",
		"vbscript:x":          "",
		"/api/graph":          "",
		"//evil.test":         "",
		"#section":            "",
		"other.md":            "", // no resolver: relative links are text
	}
	for dest, want := range cases {
		tree := BodyTree("[x](" + dest + ")")
		as := find(tree, "a")
		if len(as) != 1 {
			t.Fatalf("%s: %d anchors", dest, len(as))
		}
		if got := str(as[0], "href"); got != want {
			t.Errorf("%s: href %q, want %q", dest, got, want)
		}
		if _, has := as[0].Get("entity"); has {
			t.Errorf("%s: no resolver, so no entity", dest)
		}
	}
	// An autolink obeys the same list.
	if as := find(BodyTree("<https://ok.test>"), "a"); len(as) != 1 || str(as[0], "href") != "https://ok.test" {
		t.Errorf("autolink: %v", as)
	}
}

// A relative link naming another entity's file becomes an entity link.
func TestBodyTreeEntityLink(t *testing.T) {
	resolve := func(dest string) (string, bool) {
		if dest == "../components/cmp-ledger.md" {
			return "component/cmp-ledger", true
		}
		return "", false
	}
	tree := bodyTree("See [the ledger](../components/cmp-ledger.md) and [nothing](nope.md).", resolve)
	as := find(tree, "a")
	if len(as) != 2 {
		t.Fatalf("anchors: %d", len(as))
	}
	if str(as[0], "entity") != "component/cmp-ledger" || str(as[0], "href") != "" {
		t.Errorf("entity link = %v", as[0])
	}
	if _, has := as[1].Get("entity"); has {
		t.Errorf("unresolved relative link must not carry an entity: %v", as[1])
	}
}

func TestBodyTreeEmpty(t *testing.T) {
	if tree := BodyTree("  \n"); len(tree) != 0 {
		t.Fatalf("blank body: %v", kinds(tree))
	}
}
