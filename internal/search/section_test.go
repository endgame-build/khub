package search

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// headingPath reads ATX headings only, skips fenced code, and pops deeper
// headings when a shallower one opens.
func TestHeadingPath(t *testing.T) {
	text := strings.Join([]string{
		"Preamble.",            // 0
		"# Design",             // 1
		"## Write path ##",     // 2
		"~~~~",                 // 3
		"# not a heading",      // 4
		"~~~",                  // 5 too short to close
		"~~~~~",                // 6 closes
		"### Locking",          // 7
		"    # indented code",  // 8
		"#nospace",             // 9
		"## Read path",         // 10
		"####### seven hashes", // 11
		"   ###   Deep   #",    // 12
		"Tail.",                // 13
	}, "\n")
	lineStart := func(n int) int {
		pos := 0
		for i := 0; i < n; i++ {
			pos += strings.IndexByte(text[pos:], '\n') + 1
		}
		return pos
	}
	cases := []struct {
		line int
		want []string
	}{
		{0, nil},
		{1, []string{"Design"}},
		{4, []string{"Design", "Write path"}},
		{6, []string{"Design", "Write path"}},
		{9, []string{"Design", "Write path", "Locking"}},
		{11, []string{"Design", "Read path"}},
		{13, []string{"Design", "Read path", "Deep"}},
	}
	for _, c := range cases {
		if got := headingPath(text, lineStart(c.line)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("line %d: got %q, want %q", c.line, got, c.want)
		}
	}
}

// The section follows the snippet: a body hit names its heading path, a
// title hit, a frontmatter value and text before the first heading name
// none.
func TestSearchSection(t *testing.T) {
	root := freshWS(t)
	seedRaw(t, root, "clients/store.md",
		"---\ntype: client\nname: Store\nsummary: zebra\ncreated: 2026-06-01\n---\n"+
			"Intro.\n\n# Design\n\n## Locking\n\nWe flock the lockfile.\n")
	for _, c := range []struct {
		text string
		want []string
	}{
		{"lockfile", []string{"Design", "Locking"}},
		{"store", nil},
		{"intro", nil},
		{"zebra", nil},
	} {
		res, err := Search(root, c.text, Options{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Hits) != 1 || !reflect.DeepEqual(res.Hits[0].Section, c.want) {
			t.Errorf("%s: got %+v, want section %q", c.text, res.Hits, c.want)
		}
	}
}

// A phrase longer than the snippet window comes back unmarked; the section
// is then the snippet's start, inside the phrase. A control character the
// body holds is never taken for a mark, and the snippet keeps it. A bare
// "#" closes the headings above it and names no section.
func TestSearchSectionEdges(t *testing.T) {
	root := freshWS(t)
	words := make([]string, 15)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", i+1)
	}
	phrase := strings.Join(words, " ")
	seedRaw(t, root, "clients/long.md",
		"---\ntype: client\nname: Long\ncreated: 2026-06-01\n---\n"+
			"# Alpha\n\na b c d e f g h i j k l m n o p q r s t u v w x y z\n\n# Beta\n\n"+phrase+"\n")
	seedRaw(t, root, "clients/ctrl.md",
		"---\ntype: client\nname: Ctrl\ncreated: 2026-06-01\n---\n"+
			"# Gamma\n\nraw \x01 and \x02 bytes\n\n## Delta\n\nthe quokka lives here\n")
	seedRaw(t, root, "clients/bare.md",
		"---\ntype: client\nname: Bare\ncreated: 2026-06-01\n---\n"+
			"# Epsilon\n\n#\n\nthe wombat lives here\n")
	for _, c := range []struct {
		text, slug string
		want       []string
	}{
		// The snippet opens inside the phrase ("…w2 … w13…") and marks nothing.
		{`"` + phrase + `"`, "long", []string{"Beta"}},
		{"quokka", "ctrl", []string{"Gamma", "Delta"}},
		{"bytes", "ctrl", []string{"Gamma"}},
		{"wombat", "bare", nil},
	} {
		res, err := Search(root, c.text, Options{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Hits) != 1 || res.Hits[0].Slug != c.slug ||
			!reflect.DeepEqual(res.Hits[0].Section, c.want) {
			t.Errorf("%s: got %+v, want %s with section %q", c.text, res.Hits, c.slug, c.want)
		}
	}
	res, err := Search(root, "bytes", Options{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if want := "raw \x01 and \x02 bytes"; !strings.Contains(res.Hits[0].Snippet, want) {
		t.Errorf("snippet lost the body's control bytes: %q", res.Hits[0].Snippet)
	}
}

// pickMarks skips every byte the corpus holds and never picks whitespace.
func TestPickMarks(t *testing.T) {
	var seen [256]bool
	seen[1], seen[3] = true, true
	if got := pickMarks(&seen); got != (marks{"\x02", "\x04"}) {
		t.Errorf("got %q", got)
	}
	for c := 1; c < 0x20; c++ {
		seen[c] = true
	}
	seen[0x1f] = false
	if got := pickMarks(&seen); got != (marks{}) {
		t.Errorf("one free byte: got %q, want no marks", got)
	}
}
