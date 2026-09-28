package index_test

// The scan reads and parses entries concurrently. These tests pin what that
// must not change: listing order, the malformed set, and which error wins.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/workspace"
)

const scanOntology = `
version: "0.1.0"
ontology:
  entities:
    note: {}
    doc: {}
    rec: {}
    pack: {}
`

const scanStorage = `
storage:
  note: { layout: file, path: notes, format: md }
  doc: { layout: file, path: docs, format: json }
  rec: { layout: file, path: recs, format: yaml }
  pack: { layout: folder, path: packs }
`

func scanWS(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	write(t, src, "scan/ontology.yaml", scanOntology)
	write(t, src, "scan/storage.yaml", scanStorage)
	root := t.TempDir()
	if _, err := workspace.Init("scan", root, workspace.InitOptions{PresetSource: src}); err != nil {
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

func build(t *testing.T, root string) (*index.Index, error) {
	t.Helper()
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	return index.Build(root, resolved)
}

// Every fifth file of each type is malformed, so good and bad entries
// interleave across the worker pool.
func TestScanOrderAndMalformedAreDeterministic(t *testing.T) {
	root := scanWS(t)
	const perType = 80
	var wantOrder []index.Node
	var wantMalformed []string
	for i := range perType {
		slug := fmt.Sprintf("e%03d", i)
		bad := i%5 == 0
		files := []struct{ typ, rel, good, broken string }{
			{"note", "notes/" + slug + ".md", "---\ntype: note\ntitle: N " + slug + "\n---\nbody\n", "---\nkey: [unclosed\n---\n"},
			{"doc", "docs/" + slug + ".json", `{"type": "doc", "title": "D ` + slug + `"}`, `{"type": `},
			{"rec", "recs/" + slug + ".yaml", "type: rec\ntitle: R " + slug + "\n", "key: [unclosed\n"},
			{"pack", "packs/" + slug + "/_index.md", "---\ntype: pack\ntitle: P " + slug + "\n---\n", "---\nkey: [unclosed\n---\n"},
		}
		for _, f := range files {
			if bad {
				write(t, root, f.rel, f.broken)
				wantMalformed = append(wantMalformed, f.rel)
			} else {
				write(t, root, f.rel, f.good)
			}
		}
	}
	for _, typ := range []string{"note", "doc", "rec", "pack"} {
		for i := range perType {
			if i%5 != 0 {
				wantOrder = append(wantOrder, index.Node{Type: typ, Slug: fmt.Sprintf("e%03d", i)})
			}
		}
	}
	sort.Strings(wantMalformed)

	first, err := build(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Order, wantOrder) {
		t.Fatalf("order differs from type declaration order x sorted listing")
	}
	if !reflect.DeepEqual(first.Malformed, wantMalformed) {
		t.Fatalf("malformed = %v, want %v", first.Malformed, wantMalformed)
	}
	for range 50 {
		idx, err := build(t, root)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(idx.Order, first.Order) || !reflect.DeepEqual(idx.Malformed, first.Malformed) {
			t.Fatal("a rebuild changed Order or Malformed")
		}
		for _, n := range idx.Order {
			if !reflect.DeepEqual(idx.Meta[n], first.Meta[n]) {
				t.Fatalf("a rebuild changed the frontmatter of %s", n.ID())
			}
		}
	}
}

// With two symlinks in one type directory, the scan names the first in
// listing order, as a serial scan stopping at it would.
func TestScanReportsTheFirstErrorInListingOrder(t *testing.T) {
	root := scanWS(t)
	for i := range 40 {
		write(t, root, fmt.Sprintf("notes/e%03d.md", i), "---\ntype: note\n---\n")
	}
	target := filepath.Join(root, "notes", "e000.md")
	for _, name := range []string{"e010-link.md", "e030-link.md"} {
		if err := os.Symlink(target, filepath.Join(root, "notes", name)); err != nil {
			t.Fatal(err)
		}
	}
	for range 20 {
		_, err := build(t, root)
		if err == nil || !strings.Contains(err.Error(), "e010-link.md") {
			t.Fatalf("err = %v, want the e010-link.md symlink refusal", err)
		}
	}
}

// Only BuildWithBodies keeps bodies, and Filter carries them through.
func TestBodiesAreKeptOnlyWhenAskedFor(t *testing.T) {
	root := scanWS(t)
	write(t, root, "notes/a.md", "---\ntype: note\n---\nalpha body\n")
	write(t, root, "notes/stray.md", "---\ntype: doc\n---\nstray body\n")
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := index.Build(root, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Body != nil {
		t.Fatal("Build kept bodies")
	}
	full, err := index.BuildWithBodies(root, resolved)
	if err != nil {
		t.Fatal(err)
	}
	a := index.Node{Type: "note", Slug: "a"}
	kept := index.Filter(full, index.StrayNodes(full))
	if kept.Body[a] != "alpha body" {
		t.Fatalf("body = %q", kept.Body[a])
	}
	if _, has := kept.Body[index.Node{Type: "note", Slug: "stray"}]; has {
		t.Fatal("Filter kept a dropped node's body")
	}
}
