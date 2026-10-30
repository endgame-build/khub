package canon

import (
	"errors"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

func entry(pairs ...any) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i].(string), pairs[i+1])
	}
	return m
}

const storageDoc = "# khub-preset: build-hub@0.6.0\n" +
	"# Where each type's bytes land.\n" +
	"storage:\n" +
	"  prd:          { layout: singleton, path: knowledge/prd.md }\n" +
	"  capability:   { layout: file, path: knowledge/capabilities, id_prefix: cap } # prose\n" +
	"  adr:\n" +
	"    layout: file\n" +
	"    path: knowledge/decisions   # inline\n" +
	"    id_prefix: ad\n" +
	"    id_date: true\n" +
	"  repo:         { layout: file, path: knowledge/repos, id_prefix: rp }\n"

func mustSplice(t *testing.T, src string, path []string, e *omap.Map) string {
	t.Helper()
	out, err := SpliceEntry([]byte(src), path, e, Mode12)
	if err != nil {
		t.Fatalf("SpliceEntry: %v", err)
	}
	return string(out)
}

// A flow entry whose values stay scalars is rewritten in place on its own
// line; the header, the comments and every other entry survive byte for byte.
func TestSpliceEntryKeepsFlowStyleAndEveryOtherByte(t *testing.T) {
	got := mustSplice(t, storageDoc, []string{"storage", "repo"},
		entry("layout", "collection", "path", "knowledge/repos.jsonl", "id_prefix", "rp"))
	want := strings.Replace(storageDoc,
		"  repo:         { layout: file, path: knowledge/repos, id_prefix: rp }\n",
		"  repo: { layout: collection, path: knowledge/repos.jsonl, id_prefix: rp }\n", 1)
	if got != want {
		t.Fatalf("spliced:\n%s\nwant:\n%s", got, want)
	}
}

// A block entry is re-emitted as a block at its own indent, the entries
// around it untouched, including the one carrying an inline comment above.
func TestSpliceEntryRewritesABlockEntry(t *testing.T) {
	got := mustSplice(t, storageDoc, []string{"storage", "adr"},
		entry("layout", "folder", "path", "knowledge/decisions", "format", "json", "id_prefix", "ad", "id_date", true))
	want := strings.Replace(storageDoc,
		"  adr:\n    layout: file\n    path: knowledge/decisions   # inline\n    id_prefix: ad\n    id_date: true\n",
		"  adr:\n    layout: folder\n    path: knowledge/decisions\n    format: json\n    id_prefix: ad\n    id_date: true\n", 1)
	if got != want {
		t.Fatalf("spliced:\n%s\nwant:\n%s", got, want)
	}
}

// A flow entry that gains a nested value is re-emitted in block style; a
// key the parent lacks is appended after its last entry; a document with no
// storage block gets one appended.
func TestSpliceEntryFallsToBlockAppendsAndCreates(t *testing.T) {
	got := mustSplice(t, storageDoc, []string{"storage", "capability"},
		entry("layout", "file", "path", []any{"work/{slug}.md"}))
	if !strings.Contains(got, "  capability:\n    layout: file\n    path:\n    - work/{slug}.md\n  adr:\n") {
		t.Fatalf("nested value not re-emitted as a block:\n%s", got)
	}
	// The entry's own trailing comment goes with its line; every other comment stays.
	if !strings.HasPrefix(got, "# khub-preset: build-hub@0.6.0\n# Where") || !strings.Contains(got, "# inline") || strings.Contains(got, "# prose") {
		t.Fatalf("comments:\n%s", got)
	}

	got = mustSplice(t, storageDoc, []string{"storage", "client"}, entry("layout", "file", "path", "clients"))
	if !strings.HasSuffix(got, "  repo:         { layout: file, path: knowledge/repos, id_prefix: rp }\n  client:\n    layout: file\n    path: clients\n") {
		t.Fatalf("missing key not appended after the last entry:\n%s", got)
	}

	got = mustSplice(t, "# header\npolicy: {}\n", []string{"storage", "client"}, entry("layout", "collection", "path", "clients.yaml"))
	if got != "# header\npolicy: {}\nstorage:\n  client:\n    layout: collection\n    path: clients.yaml\n" {
		t.Fatalf("missing storage block not appended:\n%s", got)
	}

	got = mustSplice(t, "", []string{"storage", "client"}, entry("layout", "file", "path", "clients"))
	if got != "storage:\n  client:\n    layout: file\n    path: clients\n" {
		t.Fatalf("empty document:\n%q", got)
	}
}

// A parent that is a flow mapping, or an empty one, has no lines of its own
// to splice; the caller re-emits.
func TestSpliceEntryRefusesWhatItCannotAddress(t *testing.T) {
	for _, src := range []string{
		"storage: { repo: { layout: file } }\n",
		"storage:\n",
		"storage: []\n",
	} {
		_, err := SpliceEntry([]byte(src), []string{"storage", "repo"}, entry("layout", "folder"), Mode12)
		if !errors.Is(err, ErrNoSplice) {
			t.Fatalf("%q: err = %v, want ErrNoSplice", src, err)
		}
	}
}

// The spliced text always loads back as the whole document with just the one
// entry replaced — quoting included.
func TestSpliceEntryRoundTripsExoticScalars(t *testing.T) {
	src := "storage:\n  repo: { layout: file, path: repos }\n  other: { layout: file, path: '017' }\n"
	got := mustSplice(t, src, []string{"storage", "repo"}, entry("layout", "file", "path", "yes: no", "format", "json"))
	have, _, err := LoadDocMap(got, Mode12)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := have.Get("storage")
	repo, _ := st.(*omap.Map).Get("repo")
	if p, _ := repo.(*omap.Map).Get("path"); p != "yes: no" {
		t.Fatalf("path = %v in\n%s", p, got)
	}
	if !strings.Contains(got, "  other: { layout: file, path: '017' }\n") {
		t.Fatalf("sibling changed:\n%s", got)
	}
}
