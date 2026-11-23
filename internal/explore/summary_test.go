package explore

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/index"
)

func TestSummaryRule(t *testing.T) {
	long := strings.Repeat("word ", 60) // 300 characters
	cases := []struct {
		name, desc, body, want string
	}{
		{"description wins", "Short and sweet.", "# H\n\nBody text.", "Short and sweet."},
		{"blank description falls through", "   ", "Body text.", "Body text."},
		{"heading skipped", "", "# Title\n\nFirst para\nwraps here.\n\nSecond.", "First para wraps here."},
		{"list and code before the paragraph are skipped", "", "- a\n- b\n\n```\ncode\n```\n\nThe paragraph.", "The paragraph."},
		{"markup reduced to text", "", "Uses **bold**, `code` and [a link](https://x.test).", "Uses bold, code and a link."},
		{"whitespace collapsed", "", "a\t  b\n   c", "a b c"},
		{"empty body", "", "", ""},
		{"only a heading", "", "# Lonely heading\n", ""},
		{"raw html kept as text", "", "Hi <b>there</b>.", "Hi <b>there</b>."},
	}
	for _, c := range cases {
		meta := pairs("description", c.desc)
		if got := Summary(meta, c.body); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	got := Summary(nil, long)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) > summaryLimit+1 {
		t.Fatalf("long body: %q (%d runes)", got, len([]rune(got)))
	}
	if strings.Contains(got, "wor…") {
		t.Fatalf("cut mid-word: %q", got)
	}
}

// The same body summarizes the same way whatever the storage: md frontmatter
// plus prose, a json file's reserved body field, a jsonl collection row's.
func TestSummaryAcrossFormats(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".khub", "config.yaml"),
		"name: ws\npreset: fixture\nversion: 0.0.0\nsource:\ndefaults:\n  stale_days: 90\n")
	writeFile(t, filepath.Join(root, ".khub", "ontology.yaml"), `ontology:
  entities:
    note:
      attributes:
        title: {}
    card:
      attributes:
        title: {}
    row:
      attributes:
        title: {}
storage:
  note: { layout: file, path: notes, format: md }
  card: { layout: file, path: cards, format: json }
  row: { layout: collection, path: rows.jsonl, format: jsonl }
`)
	body := "## Heading\n\nThe first real paragraph.\n"
	for _, typ := range []string{"note", "card", "row"} {
		if _, err := entity.Create(root, typ, entity.CreateOpts{
			Fields: pairs("title", typ), ID: typ + "-one", Body: body,
		}); err != nil {
			t.Fatalf("create %s: %v", typ, err)
		}
	}
	snap, err := load(root, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"note", "card", "row"} {
		n := index.Node{Type: typ, Slug: typ + "-one"}
		if got := Summary(snap.idx.Meta[n], snap.idx.Body[n]); got != "The first real paragraph." {
			t.Errorf("%s: summary %q", typ, got)
		}
	}
}
