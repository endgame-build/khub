package canon

import (
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

// A collection row whose value the emitter folds onto several lines still
// splices: adding it, folding a short one, unfolding a long one and editing a
// neighbour all touch only that pair's lines, and every comment survives.
// goccy drops the space the emitter writes before each fold, which once made
// any folded document unspliceable (restoreFoldSpaces).
func TestFoldedRowValueSplices(t *testing.T) {
	head := "# top comment\na:\n  created: 2026-10-01\n  title: A   # keep me\nb:\n  created: 2026-10-01\n  title: B\n"
	long := strings.TrimSpace(strings.Repeat("word ", 32))
	folded := "  description: word word word word word word word word word word word word word \n" +
		"    word word word word word word word word word word word word word word word \n" +
		"    word word word word\n"
	cases := []struct {
		name, start, field, value, want string
	}{
		{"add a folding value", head, "description", long, head + folded},
		{"fold a short value", head + "  description: short\n", "description", long, head + folded},
		{"unfold a long value", head + folded, "description", "short", head + "  description: short\n"},
		{"edit beside a folded value", head + folded, "title", "Bee", strings.Replace(head, "title: B\n", "title: Bee\n", 1) + folded},
	}
	for _, c := range cases {
		rows, err := LoadCollection(c.start, "yaml")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := rows.Get("b")
		b.(*omap.Map).Set(c.field, c.value)
		got, err := SpliceCollection(c.start, rows, "yaml")
		if err != nil || got != c.want {
			t.Errorf("%s: err=%v\n got %q\nwant %q", c.name, err, got, c.want)
		}
	}
}
