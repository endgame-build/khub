package integrity

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

func om(pairs ...any) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i].(string), pairs[i+1])
	}
	return m
}

// nestedAttrs resolves one type declaring an object and two lists.
func nestedAttrs(t *testing.T) *schema.ResolvedType {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ontology.yaml")
	doc := `
ontology:
  entities:
    note:
      attributes:
        generated:
          type: object
          fields:
            by: { required: true }
            mode: { enum: [auto, manual] }
        verified:
          type: list
          items:
            type: object
            fields:
              by: { required: true, pattern: '[a-z]+:.+' }
              at: { type: datetime }
        scores: { type: list, items: { type: number } }
        tags: { type: list }
`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := schema.ResolveWith(nil, []string{p})
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := s.Types.Get("note")
	return rt
}

func TestNestedAttrErrors(t *testing.T) {
	note := nestedAttrs(t)
	attr := func(name string) *schema.ResolvedAttribute {
		a, _ := note.Attributes.Get(name)
		return a
	}
	cases := []struct {
		field string
		value any
		want  []string // nil = valid
	}{
		{"generated", om("by", "agent:x", "extra", 1), nil},
		{"generated", "x", []string{"'x' is not a valid object for generated"}},
		{"generated", om("mode", "auto"), []string{"generated.by is required"}},
		{"generated", om("by", "a", "mode", "maybe"), []string{"'maybe' is not a valid generated.mode (auto, manual)"}},
		{"generated", om("by", ""), []string{"generated.by is empty; omit the field or write null, not ''"}},
		// Every nested violation is reported, in declaration order.
		{"generated", om("mode", "maybe"), []string{
			"generated.by is required",
			"'maybe' is not a valid generated.mode (auto, manual)",
		}},
		{"verified", []any{om("by", "human:a")}, nil},
		// A bare mapping on a list of objects is that list's one item.
		{"verified", om("by", "human:a"), nil},
		{"verified", om("at", "2026-01-01"), []string{"verified[0].by is required"}},
		{"verified", []any{om("by", "human:a"), om("by", "nobody")},
			[]string{"'nobody' does not match the pattern for verified[1].by ([a-z]+:.+)"}},
		{"verified", []any{om("at", "soon"), om("by", "nobody")}, []string{
			"verified[0].by is required",
			"'soon' is not a valid datetime for verified[0].at",
			"'nobody' does not match the pattern for verified[1].by ([a-z]+:.+)",
		}},
		{"verified", []any{"human:a"}, []string{"'human:a' is not a valid object for verified[0]"}},
		{"scores", []any{int64(1), "two", "three"}, []string{
			"'two' is not a valid number for scores[1]",
			"'three' is not a valid number for scores[2]",
		}},
		// A plain list keeps today's behaviour: no element is checked.
		{"tags", []any{om("a", 1), "x"}, nil},
	}
	for _, c := range cases {
		if got := attrErrors(attr(c.field), c.value); !slices.Equal(got, c.want) {
			t.Errorf("%s %v:\n got  %q\n want %q", c.field, c.value, got, c.want)
		}
	}
}

// Footnotes and sources[].id pair up both ways; fenced and inline code cite
// nothing, and a footnote on an entity without sources is a gap too.
func TestFootnoteGaps(t *testing.T) {
	src := func(id string) *omap.Map { return om("resource", "https://x/"+id, "id", id) }
	body := "Claim one[^a] and two[^ghost], not `[^b]`.\n\n```\n[^b] in code\n```\n\n[^a]: First.\n"
	got := footnoteGaps("note", "n", om("sources", []any{src("a"), src("b"), "legacy"}), body)
	want := []string{
		"footnote [^ghost] names no sources entry; give one `id: ghost` or drop the footnote",
		"sources[1].id 'b' is cited by no footnote [^b] in the body",
	}
	if len(got) != len(want) {
		t.Fatalf("gaps = %v", got)
	}
	for i, w := range want {
		if got[i].Reason != w || got[i].Field != "sources" {
			t.Errorf("gap %d = %+v, want %q", i, got[i], w)
		}
	}
	if g := footnoteGaps("note", "n", om("title", "T"), body); len(g) != 2 {
		t.Errorf("an entity without sources: gaps = %v, want [^a] and [^ghost]", g)
	}
	if g := footnoteGaps("note", "n", om("title", "T"), "No footnotes, `[^x]` in code."); len(g) != 0 {
		t.Errorf("a body without footnotes got gaps: %v", g)
	}
}

// Two sources sharing an id is a gap: a footnote cannot say which it cites.
func TestDuplicateSourceIDIsAGap(t *testing.T) {
	src := func(id, at string) *omap.Map { return om("resource", "https://x/"+at, "id", id) }
	got := footnoteGaps("note", "n", om("sources", []any{src("a", "1"), src("b", "2"), src("a", "3")}), "One[^a], two[^b].\n")
	want := "sources[2].id 'a' repeats sources[0].id, so a footnote [^a] cannot tell them apart; give each source its own id"
	if len(got) != 1 || got[0].Reason != want {
		t.Fatalf("gaps = %v", got)
	}
}
