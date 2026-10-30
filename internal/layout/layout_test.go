package layout_test

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/layout"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/workspace"
)

// cell is one layout × format storage cell.
type cell struct{ layout, format string }

func (c cell) String() string { return c.layout + "/" + c.format }

// cells is every cell a migration may land in, both directions.
var cells = []cell{
	{"file", "md"}, {"file", "json"}, {"file", "yaml"},
	{"folder", "md"}, {"folder", "json"}, {"folder", "yaml"},
	{"collection", "json"}, {"collection", "jsonl"}, {"collection", "yaml"},
}

// sources are the preset-cells types, one per cell the corpus preset declares.
var sources = map[string]cell{
	"note":    {"file", "md"},
	"card":    {"file", "json"},
	"sheet":   {"file", "yaml"},
	"dossier": {"folder", "md"},
	"repo":    {"collection", "jsonl"},
	"vendor":  {"collection", "yaml"},
	"account": {"collection", "json"},
}

func fields(pairs ...string) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i], pairs[i+1])
	}
	return m
}

func add(t *testing.T, root, typeName string, opts entity.CreateOpts) {
	t.Helper()
	if _, err := entity.Create(root, typeName, opts); err != nil {
		t.Fatalf("add %s: %v", typeName, err)
	}
}

// newWS scaffolds the parity corpus preset (every storage cell) and seeds two
// entities per type: one plain, one carrying a body, a date, a number and a
// list, so each renderer meets every scalar kind.
func TestMain(m *testing.M) {
	// The clock seam every write verb reads; set once so the subtests may run
	// in parallel (t.Setenv forbids t.Parallel).
	os.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	os.Exit(m.Run())
}

func newWS(t *testing.T) string {
	t.Helper()
	baseOnce.Do(func() { baseWS, baseErr = seedWS() })
	if baseErr != nil {
		t.Fatalf("seed: %v", baseErr)
	}
	root := filepath.Join(t.TempDir(), "ws")
	if err := copyTree(baseWS, root); err != nil {
		t.Fatal(err)
	}
	return root
}

var (
	baseOnce sync.Once
	baseWS   string
	baseErr  error
)

// seedWS scaffolds the base workspace once; every test copies it.
func seedWS() (string, error) {
	dir, err := os.MkdirTemp("", "khub-layout-")
	if err != nil {
		return "", err
	}
	root := filepath.Join(dir, "ws")
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	mod := wd
	for {
		if _, err := os.Stat(filepath.Join(mod, "go.mod")); err == nil {
			break
		}
		mod = filepath.Dir(mod)
	}
	src := filepath.Join(mod, "parity", "corpus")
	if _, err := workspace.Init("preset-cells", root, workspace.InitOptions{PresetSource: src}); err != nil {
		return "", err
	}
	// note first: every rich entity relates to note/plain-note.
	for _, name := range []string{"note", "card", "sheet", "dossier", "repo", "vendor", "account"} {
		if _, err := entity.Create(root, name, entity.CreateOpts{Fields: fields("title", "Plain "+name), UseTemplate: true}); err != nil {
			return "", err
		}
		if _, err := entity.Create(root, name, entity.CreateOpts{
			Fields: fields("title", "Rich "+name, "tags", "a,b", "due", "2026-02-01", "amount", "3.5", "related", "note/plain-note"),
			Body:   "Prose with — dashes.\n\n## And a heading\n",
			ID:     "rich-" + name,
		}); err != nil {
			return "", err
		}
	}
	return root, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0o777)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, 0o644)
	})
}

func located(t *testing.T, err error) *errs.Located {
	t.Helper()
	var l *errs.Located
	if !errors.As(err, &l) {
		t.Fatalf("not a located error: %v", err)
	}
	return l
}

func refuses(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got success", code)
	}
	if l := located(t, err); l.Code != code {
		t.Fatalf("code = %s (%s), want %s", l.Code, l.Message, code)
	}
}

// tree snapshots every file under root but the generated directory.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".khub/generated" {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// view is every entity of typeName as the scan reads it: meta in the CLI
// dialect (dates as ISO strings, so json and yaml compare alike) and body.
func view(t *testing.T, root, typeName string) map[string]string {
	t.Helper()
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := index.BuildWithBodies(root, resolved)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, n := range idx.Order {
		if n.Type != typeName {
			continue
		}
		// A row's `type` is supplied by the scan, after its own keys; a file
		// opens with it. Compare the values, not that position.
		meta := omap.New()
		meta.Set("type", n.Type)
		for _, k := range idx.Meta[n].Keys() {
			v, _ := idx.Meta[n].Get(k)
			meta.Set(k, v)
		}
		doc, err := canon.EncodeCLI(meta)
		if err != nil {
			t.Fatal(err)
		}
		// The scan strips an md body's surrounding whitespace and keeps a
		// row's verbatim; the bytes are compared elsewhere.
		out[n.Slug] = doc + "\n" + strings.TrimSpace(idx.Body[n])
	}
	return out
}

func migrate(t *testing.T, root, typeName string, target layout.Target) *layout.Plan {
	t.Helper()
	p, err := layout.Migrate(root, typeName, target, layout.Options{Execute: true})
	if err != nil {
		t.Fatalf("migrate %s to %+v: %v", typeName, target, err)
	}
	return p
}

func storageOf(t *testing.T, root, typeName string) (string, string, string) {
	t.Helper()
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := resolved.Types.Get(typeName)
	return rt.Storage.Layout, rt.Storage.Fmt, rt.StorageRelpath()
}

// Every source cell migrates to every other cell and back, the scan reads
// the same entities throughout, and the return trip reproduces the original
// files byte for byte.
func TestEveryCellRoundTrips(t *testing.T) {
	for name, from := range sources {
		for _, to := range cells {
			if to == from {
				continue
			}
			t.Run(name+"->"+to.String(), func(t *testing.T) {
				t.Parallel()
				root := newWS(t)
				before := tree(t, root)
				want := view(t, root, name)
				_, _, origPath := storageOf(t, root, name)

				p := migrate(t, root, name, layout.Target{Layout: to.layout, Format: to.format})
				if p.Entities != 2 || !p.Executed || !p.StorageYAML {
					t.Fatalf("plan: %+v", p)
				}
				if l, f, _ := storageOf(t, root, name); l != to.layout || f != to.format {
					t.Fatalf("storage after = %s/%s", l, f)
				}
				if got := view(t, root, name); !equalViews(got, want) {
					t.Fatalf("entities changed on the way to %s:\n%v\nwant\n%v", to, got, want)
				}
				for _, rel := range p.Remove {
					if _, exists := tree(t, root)[rel]; exists {
						t.Fatalf("%s still there after the migration", rel)
					}
				}
				// Everything outside the type is untouched but storage.yaml and index.md.
				after := tree(t, root)
				for rel, data := range before {
					if strings.HasPrefix(rel, origPath+"/") || rel == origPath || rel == ".khub/storage.yaml" || rel == "index.md" {
						continue
					}
					if after[rel] != data {
						t.Fatalf("%s changed by a migration of %s", rel, name)
					}
				}

				migrate(t, root, name, layout.Target{Layout: from.layout, Format: from.format})
				if l, f, p := storageOf(t, root, name); l != from.layout || f != from.format || p != origPath {
					t.Fatalf("storage after return = %s/%s at %s, want %s at %s", l, f, p, from, origPath)
				}
				back := tree(t, root)
				for rel, data := range before {
					if rel == ".khub/storage.yaml" || rel == "index.md" {
						continue
					}
					if back[rel] != data {
						t.Fatalf("%s is not byte-identical after %s -> %s -> %s:\n%s\nwant\n%s", rel, from, to, from, back[rel], data)
					}
				}
				for rel := range back {
					if _, had := before[rel]; !had && rel != "index.md" {
						t.Fatalf("%s left behind after the round trip", rel)
					}
				}
				if got := view(t, root, name); !equalViews(got, want) {
					t.Fatalf("entities changed on the way back:\n%v\nwant\n%v", got, want)
				}
			})
		}
	}
}

func equalViews(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// A dry run computes the same plan and touches nothing — not even the lock.
func TestDryRunWritesNothing(t *testing.T) {
	root := newWS(t)
	before := tree(t, root)
	p, err := layout.Migrate(root, "note", layout.Target{Layout: "collection", Format: "yaml"}, layout.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Executed || p.Entities != 2 || p.To.Path != "notes.yaml" || len(p.Create) != 1 || len(p.Remove) != 2 {
		t.Fatalf("plan: %+v", p)
	}
	if got := tree(t, root); !equalViews(got, before) {
		t.Fatal("dry run changed the workspace")
	}
	if journals, _ := filepath.Glob(filepath.Join(root, ".khub", "generated", "migrate-*")); len(journals) != 0 {
		t.Fatalf("dry run left a journal: %v", journals)
	}
}

// The derived destination keeps the type where it is; --path moves it, and
// a path is cleaned to what the schema stores.
func TestDerivedAndExplicitPaths(t *testing.T) {
	root := newWS(t)
	for _, tc := range []struct {
		typeName string
		target   layout.Target
		path     string
		format   bool // storage.yaml carries a format key
	}{
		{"note", layout.Target{Layout: "collection", Format: "jsonl"}, "notes.jsonl", false},
		{"repo", layout.Target{Layout: "folder", Format: "yaml"}, "repos", true},
		{"account", layout.Target{Format: "yaml"}, "accounts.yaml", false},
		{"card", layout.Target{Path: "./data/cards/"}, "data/cards", true},
		{"vendor", layout.Target{Layout: "collection", Path: "data/vendors.yaml"}, "data/vendors.yaml", false},
	} {
		p, err := layout.Migrate(root, tc.typeName, tc.target, layout.Options{})
		if err != nil {
			t.Fatalf("%s: %v", tc.typeName, err)
		}
		if p.To.Path != tc.path {
			t.Fatalf("%s: path = %s, want %s", tc.typeName, p.To.Path, tc.path)
		}
	}
	refuses(t, layoutErr(root, "vendor", layout.Target{Layout: "collection", Path: "data/vendors.dat"}), "migrate_target")
	migrate(t, root, "vendor", layout.Target{Layout: "collection", Path: "data/vendors.yaml"})
	text, err := os.ReadFile(filepath.Join(root, ".khub", "storage.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "  vendor:\n    layout: collection\n    path: data/vendors.yaml\n  account:") {
		t.Fatalf("storage.yaml: a collection's path carries its format:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "vendors.yaml")); err != nil {
		t.Fatal(err)
	}
}

// storage.yaml keeps its header, its comments and every other entry; a type
// the file never named gets an entry appended.
func TestStorageYAMLKeepsEveryOtherByte(t *testing.T) {
	root := newWS(t)
	storagePath := filepath.Join(root, ".khub", "storage.yaml")
	authored := "# khub-preset: preset-cells@0.1.0\n" +
		"# hand-kept\n" +
		"storage:\n" +
		"  note:    { layout: file, path: notes }   # the default cell\n" +
		"  card:    { layout: file, format: json, path: cards }\n" +
		"  sheet:   { layout: file, format: yaml, path: sheets }\n" +
		"  dossier: { layout: folder, path: dossiers }\n" +
		"  charter: { layout: singleton, path: charter.md }\n" +
		"  repo:    { layout: collection, format: jsonl, path: repos.jsonl }\n" +
		"  vendor:  { layout: collection, format: yaml, path: vendors.yaml }\n" +
		"  account: { layout: collection, format: json, path: accounts.json }\n"
	if err := os.WriteFile(storagePath, []byte(authored), 0o644); err != nil {
		t.Fatal(err)
	}
	migrate(t, root, "card", layout.Target{Layout: "collection", Format: "yaml"})
	got, _ := os.ReadFile(storagePath)
	want := strings.Replace(authored,
		"  card:    { layout: file, format: json, path: cards }\n",
		"  card: { layout: collection, path: cards.yaml }\n", 1)
	if string(got) != want {
		t.Fatalf("storage.yaml:\n%s\nwant:\n%s", got, want)
	}

	// A type with no storage entry (the file/<type> default) gets one appended.
	if err := os.WriteFile(storagePath, []byte("# header\nstorage:\n  charter: { layout: singleton, path: charter.md }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Under that file every other type is file/md at <type>/; move the md
	// notes there first so the scan finds them.
	for _, rel := range []string{"notes/plain-note.md", "notes/rich-note.md"} {
		data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err := os.MkdirAll(filepath.Join(root, "note"), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "note", filepath.Base(rel)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	migrate(t, root, "note", layout.Target{Layout: "collection", Format: "jsonl"})
	got, _ = os.ReadFile(storagePath)
	if string(got) != "# header\nstorage:\n  charter: { layout: singleton, path: charter.md }\n  note:\n    layout: collection\n    path: note.jsonl\n" {
		t.Fatalf("storage.yaml:\n%s", got)
	}
}

func TestRefusals(t *testing.T) {
	root := newWS(t)
	try := func(typeName string, target layout.Target) error {
		_, err := layout.Migrate(root, typeName, target, layout.Options{})
		return err
	}
	refuses(t, try("nothing", layout.Target{Layout: "folder"}), "unknown_type")
	refuses(t, try("charter", layout.Target{Format: "json"}), "migrate_singleton")
	refuses(t, try("note", layout.Target{}), "migrate_unchanged")
	refuses(t, try("note", layout.Target{Layout: "file", Format: "md", Path: "notes"}), "migrate_unchanged")
	refuses(t, try("note", layout.Target{Layout: "heap"}), "migrate_target")
	refuses(t, try("note", layout.Target{Format: "toml"}), "migrate_target")
	refuses(t, try("note", layout.Target{Layout: "collection"}), "migrate_target")
	refuses(t, try("note", layout.Target{Format: "jsonl"}), "migrate_target")
	refuses(t, try("note", layout.Target{Layout: "collection", Format: "json", Path: "work/{slug}.json"}), "migrate_target")
	refuses(t, try("note", layout.Target{Layout: "collection", Format: "json", Path: "../escape.json"}), "migrate_target")
	refuses(t, try("note", layout.Target{Path: "cards"}), "invalid_schema") // overlaps card's storage

	// An existing destination that the plan does not produce.
	if err := os.WriteFile(filepath.Join(root, "notes.jsonl"), []byte(`{"slug": "other", "title": "x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, try("note", layout.Target{Layout: "collection", Format: "jsonl"}), "migrate_target_exists")
	// An empty collection file at the destination is taken over.
	if err := os.WriteFile(filepath.Join(root, "notes.jsonl"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := layout.Migrate(root, "note", layout.Target{Layout: "collection", Format: "jsonl"}, layout.Options{})
	if err != nil || len(p.Rewrite) != 1 || len(p.Create) != 0 {
		t.Fatalf("empty destination: %+v %v", p, err)
	}

	// A stray in the type's storage, then a malformed file.
	stray := filepath.Join(root, "notes", "stray.md")
	if err := os.WriteFile(stray, []byte("---\ntype: card\ntitle: no\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, try("note", layout.Target{Format: "json"}), "migrate_unclean")
	if err := os.WriteFile(stray, []byte("---\ntitle: [broken\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, try("note", layout.Target{Format: "json"}), "migrate_unclean")
	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}

	// A value the destination cannot carry.
	add(t, root, "note", entity.CreateOpts{Fields: fields("title", "Bodied", "body", "x"), UseTemplate: true})
	refuses(t, try("note", layout.Target{Format: "json"}), "migrate_lossy")
	refuses(t, try("note", layout.Target{Layout: "collection", Format: "yaml"}), "migrate_lossy")
	if _, err := entity.Delete(root, "note/bodied", true); err != nil {
		t.Fatal(err)
	}
	add(t, root, "note", entity.CreateOpts{Fields: fields("title", "Slugged", "slug", "other"), UseTemplate: true})
	refuses(t, try("note", layout.Target{Layout: "collection", Format: "yaml"}), "migrate_lossy")
	if _, err := entity.Delete(root, "note/slugged", true); err != nil {
		t.Fatal(err)
	}
	add(t, root, "repo", entity.CreateOpts{Fields: fields("title", "Con"), ID: "con"})
	refuses(t, try("repo", layout.Target{Layout: "file", Format: "md"}), "migrate_lossy")

	// An ignored destination.
	if err := os.WriteFile(filepath.Join(root, ".khub", "ignore"), []byte("archive/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, try("note", layout.Target{Path: "archive/notes"}), "migrate_target")
}

// A move that would leave a path reference dangling is refused: the
// candidate's `validate` reports an error the live workspace does not.
func TestRefusesAMoveThatBreaksAPathReference(t *testing.T) {
	root := newWS(t)
	policy := filepath.Join(root, ".khub", "policy.yaml")
	if err := os.WriteFile(policy, []byte("policy:\n  charter: { orphan: true }\n  note: { path_refs: true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := entity.Update(root, "note/plain-note", entity.UpdateOpts{Fields: fields("references", "cards/plain-card.json")}); err != nil {
		t.Fatal(err)
	}
	err := layoutErr(root, "card", layout.Target{Format: "yaml"})
	refuses(t, err, "migrate_invalid")
	if !strings.Contains(err.Error(), "note/plain-note: references:") {
		t.Fatalf("message does not name the new error: %v", err)
	}
	// The same move on a type nobody references by path is fine.
	migrate(t, root, "sheet", layout.Target{Format: "json"})
}

func layoutErr(root, typeName string, target layout.Target) error {
	_, err := layout.Migrate(root, typeName, target, layout.Options{})
	return err
}

// A templated source migrates to a collection, or to a plain directory the
// caller names; the template itself is never rewritten.
func TestTemplatedSource(t *testing.T) {
	root := newWS(t)
	storagePath := filepath.Join(root, ".khub", "storage.yaml")
	text, _ := os.ReadFile(storagePath)
	authored := strings.Replace(string(text), "    path: notes\n", "    path: work/{slug}/{slug}.md\n", 1)
	if authored == string(text) {
		t.Fatalf("storage.yaml shape:\n%s", text)
	}
	if err := os.WriteFile(storagePath, []byte(authored), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"plain-note", "rich-note"} {
		data, _ := os.ReadFile(filepath.Join(root, "notes", slug+".md"))
		if err := os.MkdirAll(filepath.Join(root, "work", slug), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "work", slug, slug+".md"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refuses(t, layoutErr(root, "note", layout.Target{Format: "json"}), "migrate_target")
	want := view(t, root, "note")
	p := migrate(t, root, "note", layout.Target{Layout: "collection", Format: "yaml"})
	if p.To.Path != "work.yaml" || !strings.Contains(p.From.Path, "{slug}") {
		t.Fatalf("plan: %+v", p)
	}
	if got := view(t, root, "note"); !equalViews(got, want) {
		t.Fatalf("entities changed:\n%v\nwant\n%v", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "work")); !os.IsNotExist(err) {
		t.Fatalf("emptied template tree not removed: %v", err)
	}
	migrate(t, root, "note", layout.Target{Layout: "file", Format: "md", Path: "notes"})
	if got := view(t, root, "note"); !equalViews(got, want) {
		t.Fatal("entities changed on the way to a plain directory")
	}
}

// A hand-authored md file that carries a comment survives a trip through a
// collection with its values intact; the comment itself is the one thing
// the collection cannot carry.
func TestCommentBearingSourceKeepsItsValues(t *testing.T) {
	root := newWS(t)
	hand := filepath.Join(root, "notes", "hand.md")
	if err := os.WriteFile(hand, []byte("---\n# kept?\ntype: note\ntitle: Hand\ncreated: 2026-01-02\ndraft: false\n---\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := view(t, root, "note")
	migrate(t, root, "note", layout.Target{Layout: "collection", Format: "json"})
	migrate(t, root, "note", layout.Target{Layout: "file", Format: "md"})
	if got := view(t, root, "note"); !equalViews(got, want) {
		t.Fatalf("values changed:\n%v\nwant\n%v", got, want)
	}
	back, _ := os.ReadFile(hand)
	if string(back) != "---\ntype: note\ntitle: Hand\ncreated: 2026-01-02\ndraft: false\n---\nBody.\n" {
		t.Fatalf("hand.md after the round trip:\n%s", back)
	}
}

func TestShowListsEveryType(t *testing.T) {
	root := newWS(t)
	rows, err := layout.Show(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Type+"="+r.Layout+"/"+r.Format+"@"+r.Path)
	}
	sort.Strings(names)
	want := []string{
		"account=collection/json@accounts.json", "card=file/json@cards", "charter=singleton/md@charter.md",
		"dossier=folder/md@dossiers", "note=file/md@notes", "repo=collection/jsonl@repos.jsonl",
		"sheet=file/yaml@sheets", "vendor=collection/yaml@vendors.yaml",
	}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("rows = %v", names)
	}
	if _, err := schema.ParseTemplate("work/{slug}.md", "md"); err != nil {
		t.Fatal(err)
	}
}
