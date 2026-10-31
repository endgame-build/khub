package export_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/export"
	"github.com/endgame-build/khub/internal/workspace"
)

// A preset holding every storage cell export reads: md files with relations,
// a json and a yaml per-item type, and a yaml collection.
const ontology = `
version: "0.1.0"
ontology:
  entities:
    note:
      relations:
        cites: { to: [note, card], many: true }
    card: {}
    sheet: {}
    vendor: {}
    crate: {}
`

const storage = `
storage:
  note:   { layout: file, path: notes }
  card:   { layout: file, path: cards, format: json }
  sheet:  { layout: file, path: sheets, format: yaml }
  vendor: { layout: collection, format: yaml, path: data/vendors.yaml }
  crate:  { layout: file, path: data/vendors }
`

func newWS(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	write(t, src, "cells/ontology.yaml", ontology)
	write(t, src, "cells/storage.yaml", storage)
	root := t.TempDir()
	if _, err := workspace.Init("cells", root, workspace.InitOptions{PresetSource: src}); err != nil {
		t.Fatalf("init: %v", err)
	}
	return root
}

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func run(t *testing.T, root, target string, opt export.Options) *export.Result {
	t.Helper()
	res, err := export.Export(root, target, opt)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return res
}

func code(t *testing.T, err error) string {
	t.Helper()
	var l *errs.Located
	if !errors.As(err, &l) {
		t.Fatalf("want a located error, got %v", err)
	}
	return l.Code
}

const long = "a description long enough that the round-trip emitter would fold it past " +
	"eighty columns, which a foreign frontmatter parser has been seen to lose"

func seedCells(t *testing.T, root string) {
	t.Helper()
	write(t, root, "notes/a.md", "---\ntype: note\ntitle: A  # kept\nstatus: draft\n"+
		"verified:\n- {by: human:a, at: 2026-01-01T00:00:00Z}\ncites:\n- c1\n- ghost\n- b\n---\nBody of A.\n")
	write(t, root, "notes/b.md", "---\ntype: note\ntitle: B\ndescription: Second\n  note.\n---\n")
	write(t, root, "cards/c1.json", `{"type": "card", "title": "C1", "x_extra": 1, "body": "Card body."}`+"\n")
	write(t, root, "sheets/s1.yaml", "type: sheet\ntitle: S1\ndescription: "+long+"\n")
	write(t, root, "data/vendors.yaml", "# vendors\nacme:\n  type: vendor\n  name: Acme Co\n  body: Row prose.\n")
}

// Each entity is one concept at its workspace path: md as written, other
// formats materialized with safe-profile frontmatter, rows under the
// collection's stem. Relations become a `# Related` section.
func TestExportWritesOneConceptPerEntity(t *testing.T) {
	root := newWS(t)
	seedCells(t, root)
	out := filepath.Join(t.TempDir(), "bundle")
	res := run(t, root, out, export.Options{})

	wantConcepts := []string{"notes/a.md", "notes/b.md", "cards/c1.md", "sheets/s1.md", "data/vendors/acme.md"}
	if !slices.Equal(res.Concepts, wantConcepts) {
		t.Fatalf("concepts %v", res.Concepts)
	}
	if got, want := read(t, out, "notes/a.md"), read(t, root, "notes/a.md")+
		"\n# Related\n\n- cites: [C1](/cards/c1.md)\n- cites: ghost\n- cites: [B](/notes/b.md)\n"; got != want {
		t.Fatalf("note a\n got: %q\nwant: %q", got, want)
	}
	if got, want := read(t, out, "cards/c1.md"),
		"---\ntype: card\ntitle: C1\nx_extra: 1\n---\n\nCard body.\n"; got != want {
		t.Fatalf("card\n got: %q\nwant: %q", got, want)
	}
	if got, want := read(t, out, "sheets/s1.md"),
		"---\ntype: sheet\ntitle: S1\ndescription: "+long+"\n---\n"; got != want {
		t.Fatalf("sheet (safe profile keeps the long line)\n got: %q\nwant: %q", got, want)
	}
	if got, want := read(t, out, "data/vendors/acme.md"),
		"---\ntype: vendor\nname: Acme Co\n---\n\nRow prose.\n"; got != want {
		t.Fatalf("row\n got: %q\nwant: %q", got, want)
	}
}

// Every directory gets an index; only the root one has frontmatter, and it
// holds okf_version alone. Entries carry the concept's description on one line.
func TestExportWritesSection8Indexes(t *testing.T) {
	root := newWS(t)
	seedCells(t, root)
	out := filepath.Join(t.TempDir(), "bundle")
	res := run(t, root, out, export.Options{})
	if want := []string{"index.md", "cards/index.md", "data/index.md", "data/vendors/index.md",
		"notes/index.md", "sheets/index.md"}; !slices.Equal(res.Indexes, want) {
		t.Fatalf("indexes %v", res.Indexes)
	}
	if got, want := read(t, out, "index.md"), "---\nokf_version: '0.2'\n---\n# Subdirectories\n\n"+
		"* [cards](cards/index.md)\n* [data](data/index.md)\n* [notes](notes/index.md)\n"+
		"* [sheets](sheets/index.md)\n"; got != want {
		t.Fatalf("root index\n got: %q\nwant: %q", got, want)
	}
	if got, want := read(t, out, "notes/index.md"),
		"# note\n\n* [A](a.md)\n* [B](b.md) - Second note.\n"; got != want {
		t.Fatalf("notes index\n got: %q\nwant: %q", got, want)
	}
	if got, want := read(t, out, "data/index.md"), "# Subdirectories\n\n* [vendors](vendors/index.md)\n"; got != want {
		t.Fatalf("data index\n got: %q\nwant: %q", got, want)
	}
}

// Only entities are exported: .khub/, the agent files, the workspace's own
// index.md, reference docs and ignored paths stay out.
func TestExportHoldsEntitiesOnly(t *testing.T) {
	root := newWS(t)
	write(t, root, "notes/a.md", "---\ntype: note\n---\n")
	write(t, root, "CLAUDE.md", "# agents\n")
	write(t, root, "notes/README.md", "no frontmatter, a reference doc\n")
	write(t, root, "notes/skip/x.md", "---\ntype: note\n---\n")
	write(t, root, ".khub/ignore", "notes/skip/\n")
	out := filepath.Join(t.TempDir(), "bundle")
	run(t, root, out, export.Options{})
	var got []string
	if err := filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(out, p)
			got = append(got, filepath.ToSlash(rel))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{".khub-export", "index.md", "notes/a.md", "notes/index.md"}; !slices.Equal(got, want) {
		t.Fatalf("bundle holds %v", got)
	}
}

// The target must be outside the workspace, and empty unless forced; --force
// replaces only the files the bundle writes. A dry run writes nothing.
func TestExportTargetRules(t *testing.T) {
	root := newWS(t)
	seedCells(t, root)

	for _, target := range []string{filepath.Join(root, "out"), filepath.Dir(root)} {
		if _, err := export.Export(root, target, export.Options{}); code(t, err) != "export_target_overlaps" {
			t.Fatalf("overlap %s: %v", target, err)
		}
	}
	if _, statErr := os.Stat(filepath.Join(root, "out")); !os.IsNotExist(statErr) {
		t.Fatal("a refused export created its target")
	}
	file := filepath.Join(t.TempDir(), "f")
	write(t, filepath.Dir(file), "f", "x")
	if _, err := export.Export(root, file, export.Options{}); code(t, err) != "export_target_not_directory" {
		t.Fatalf("file target: %v", err)
	}

	out := filepath.Join(t.TempDir(), "bundle")
	dry := run(t, root, out, export.Options{DryRun: true})
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) || len(dry.Concepts) != 5 {
		t.Fatalf("dry run wrote or planned wrong: %v", dry.Concepts)
	}

	other := filepath.Join(t.TempDir(), "other")
	write(t, other, "mine.txt", "keep me\n")
	if _, err := export.Export(root, other, export.Options{}); code(t, err) != "export_target_not_empty" {
		t.Fatalf("not empty: %v", err)
	}
	if _, err := export.Export(root, other, export.Options{Force: true}); code(t, err) != "export_target_not_bundle" {
		t.Fatalf("not a bundle: %v", err)
	}
	if read(t, other, "mine.txt") != "keep me\n" {
		t.Fatal("a refused export touched a non-bundle directory")
	}

	run(t, root, out, export.Options{})
	write(t, out, "notes/a.md", "stale\n")
	run(t, root, out, export.Options{Force: true})
	if strings.HasPrefix(read(t, out, "notes/a.md"), "stale") {
		t.Fatal("--force kept a stale concept of the previous bundle")
	}
}

// --force replaces only a previous export that holds nothing else. Another
// workspace — whose reindexed index.md also declares okf_version — is not one;
// nor is a bundle that has since gained a .git/ or a file of its own. Each is
// refused with nothing touched.
func TestExportForceNeverDeletesWhatItDidNotWrite(t *testing.T) {
	root := newWS(t)
	seedCells(t, root)

	other := newWS(t)
	write(t, other, "index.md", "---\nokf_version: '0.2'\n---\n# Index\n")
	write(t, other, "notes/keep.md", "---\ntype: note\n---\n")
	if _, err := export.Export(root, other, export.Options{Force: true}); code(t, err) != "export_target_not_bundle" {
		t.Fatalf("workspace as target: %v", err)
	}
	if read(t, other, "notes/keep.md") != "---\ntype: note\n---\n" {
		t.Fatal("a refused export touched the other workspace")
	}

	for name, extra := range map[string]string{"git": ".git/HEAD", "user file": "notes/mine.md"} {
		out := filepath.Join(t.TempDir(), "bundle")
		run(t, root, out, export.Options{})
		write(t, out, extra, "mine\n")
		if _, err := export.Export(root, out, export.Options{Force: true}); code(t, err) != "export_target_unsafe" {
			t.Fatalf("%s: %v", name, err)
		}
		if read(t, out, extra) != "mine\n" {
			t.Fatalf("%s: a refused export deleted %s", name, extra)
		}
	}
}

// A scan with an unparseable file would export an incomplete bundle.
func TestExportRefusesAMalformedWorkspace(t *testing.T) {
	root := newWS(t)
	write(t, root, "cards/bad.json", "{not json")
	if _, err := export.Export(root, filepath.Join(t.TempDir(), "b"), export.Options{}); code(t, err) != "malformed_projection" {
		t.Fatalf("got %v", err)
	}
}

// Two entities whose concepts would land on one path refuse the export: a
// collection row materializes under the collection's stem, where a file type
// may already keep the same name.
func TestExportRefusesAConceptPathCollision(t *testing.T) {
	root := newWS(t)
	write(t, root, "data/vendors.yaml", "acme:\n  type: vendor\n")
	write(t, root, "data/vendors/acme.md", "---\ntype: crate\n---\n")
	if _, err := export.Export(root, filepath.Join(t.TempDir(), "b"), export.Options{}); code(t, err) != "export_path_collision" {
		t.Fatalf("got %v", err)
	}
}

// A target that is a link is replaced where it points: the bundle behind it
// is the new one, and the link stays a link.
func TestExportForceReplacesTheBundleBehindALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := newWS(t)
	seedCells(t, root)
	base := t.TempDir()
	real := filepath.Join(base, "bundle")
	run(t, root, real, export.Options{})
	write(t, real, "cards/c1.md", "stale\n")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	run(t, root, link, export.Options{Force: true})
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v", err)
	}
	if read(t, real, "cards/c1.md") == "stale\n" {
		t.Fatal("the bundle behind the link is still the old one")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(base, ".*")); len(leftovers) != 0 {
		t.Fatalf("left behind %v", leftovers)
	}
}

// Once the new bundle is swapped in, an old one that will not delete is
// reported in CleanupLeft; the export itself succeeded.
func TestExportReportsAnOldBundleItCouldNotRemove(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory whose entries the user cannot delete")
	}
	root := newWS(t)
	seedCells(t, root)
	out := filepath.Join(t.TempDir(), "bundle")
	run(t, root, out, export.Options{})
	locked := filepath.Join(out, "cards")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	res := run(t, root, out, export.Options{Force: true})
	if len(res.CleanupLeft) != 1 || !strings.Contains(res.CleanupLeft[0], ".bundle.khub-old-") {
		t.Fatalf("cleanup_left %v", res.CleanupLeft)
	}
	left := filepath.FromSlash(res.CleanupLeft[0])
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(left, "cards"), 0o755) })
	if _, err := os.Stat(filepath.Join(out, ".khub-export")); err != nil {
		t.Fatalf("the new bundle is not in place: %v", err)
	}
	if info, err := os.Stat(filepath.Join(out, "cards")); err != nil || info.Mode().Perm() == 0o555 {
		t.Fatalf("the target still holds the old bundle: %v", err)
	}
}
