package mdlink

import (
	"reflect"
	"testing"
)

func TestRelativeLinks(t *testing.T) {
	text := "See [a](a.md) and ![img](img/x.png \"title\") and [b](<my notes.md>).\n" +
		"[web](https://example.com/x.md) [mail](mailto:x@y.z) [top](#heading) " +
		"[root](/abs.md) [frag](c.md#part) [q](d.md?x=1) [enc](e%20f.md)\n" +
		"[dup](a.md#again) [same](./a.md) [also](sub/../a.md) [paren](g_(1).md) " +
		"`[code](code.md)` ``[c2](`x`.md)``\n" +
		"[esc](esc\\(p\\).md) [back](..\\prd.md)\n" +
		"\n" +
		"[ref]: ../ref.md \"Ref\"\n" +
		"  [angle]: <h i.md>\n" +
		"[paren title]: j.md (Title)\n" +
		"[^1]: Footnote prose, not a link.\n" +
		"[win](C:/x.md) [empty]() not a link [x] (y.md)\n"
	got := RelativeLinks(text)
	want := []Link{
		{"a.md", "a.md"},
		{"img/x.png", "img/x.png"},
		{"my notes.md", "my notes.md"},
		{"c.md#part", "c.md"},
		{"d.md?x=1", "d.md"},
		{"e%20f.md", "e f.md"},
		{"g_(1).md", "g_(1).md"},
		{`esc\(p\).md`, "esc(p).md"},
		{`..\prd.md`, `..\prd.md`},
		{"../ref.md", "../ref.md"},
		{"h i.md", "h i.md"},
		{"j.md", "j.md"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RelativeLinks =\n%v\nwant\n%v", got, want)
	}
}

// Text that only looks like a link is not one.
func TestRelativeLinksFalsePositives(t *testing.T) {
	for name, text := range map[string]string{
		"prose definition":         "[Note]: see below for details.\n",
		"definition mid-paragraph": "Some prose here\n[ref]: ref.md\n",
		"no opening bracket":       "As in figure 2](missing-fig.md) above.\n",
		"escaped close":            "\\[not\\](x.md)\n",
		"blank line between":       "[open\n\nlater](x.md)\n",
		"indented code":            "Prose.\n\n    [code](code.md)\n",
		"destination next line":    "[t](\nx.md)\n",
		"plain":                    "plain prose, [brackets] and (parens)\n",
	} {
		if got := RelativeLinks(text); len(got) != 0 {
			t.Errorf("%s: RelativeLinks = %v, want none", name, got)
		}
	}
}

func TestRelativeLinksEscapedBracketInText(t *testing.T) {
	got := RelativeLinks("[esc\\](x)](escaped.md)\n")
	want := []Link{{"escaped.md", "escaped.md"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RelativeLinks = %v, want %v", got, want)
	}
}

// A list item's indented continuation is prose, not code.
func TestRelativeLinksListContinuation(t *testing.T) {
	got := RelativeLinks("- item\n\n    continued [l](l.md)\n\n## H\n[d]: d.md\n")
	want := []Link{{"l.md", "l.md"}, {"d.md", "d.md"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RelativeLinks = %v, want %v", got, want)
	}
}
