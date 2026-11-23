package okfimport_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/okfimport"
	"github.com/endgame-build/khub/internal/workspace"
)

// A workspace with an md type that declares a relation, a type whose name
// slugifies from an OKF type string, and a yaml collection.
const ontology = `
version: "0.1.0"
ontology:
  entities:
    note:
      relations:
        cites: { to: any, many: true }
        lead: { to: note }
    case-study: {}
    vendor: {}
    decision:
      attributes:
        title: {}
    part:
      attributes:
        kind: { enum: [bolt, nut] }
`

const storage = `
storage:
  note:       { layout: file, path: notes }
  case-study: { layout: file, path: studies }
  vendor:     { layout: collection, format: yaml, path: vendors.yaml }
  decision:   { layout: file, path: decisions, id_prefix: ad, id_date: true }
  part:       { layout: file, path: parts, id_prefix: { by: kind, map: { bolt: bl, nut: nt } } }
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

func run(t *testing.T, root, bundle string, opt okfimport.Options) *okfimport.Result {
	t.Helper()
	opt.Actor = "khub"
	res, err := okfimport.Import(root, bundle, opt)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return res
}

func reasons(res *okfimport.Result) map[string]string {
	out := map[string]string{}
	for _, s := range res.Skipped {
		out[s.Concept] = s.Reason
	}
	return out
}

func imported(res *okfimport.Result) []string {
	var out []string
	for _, c := range res.Imported {
		out = append(out, c.Concept+" -> "+c.ID)
	}
	return out
}

// Frontmatter lands as written: unknown keys, nested values and their order
// survive, and only `type` (to the khub type, the OKF one kept as
// `okf_type`), `status` (forced to draft) and `verified` (quarantined as
// `okf_verified`, renamed in place) change. A concept with a `generated`
// keeps it; one without is stamped.
func TestImportKeepsFrontmatterAsWritten(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	src := "---\ntype: Case Study  # theirs\ntitle: Acme rollout\nzeta: 1\nalpha: {x: [1, 2]}\n" +
		"mid: kept\nstatus: stable\nverified: {by: human:a, at: 2026-01-01T00:00:00Z}\n" +
		"generated: {by: tool/1, at: 2026-01-01T00:00:00Z}\n---\n\nBody stays.\n"
	write(t, bundle, "studies/acme-rollout.md", src)
	write(t, bundle, "notes/plain.md", "---\ntype: note\n---\n")
	res := run(t, root, bundle, okfimport.Options{})

	want := "---\ntype: case-study  # theirs\ntitle: Acme rollout\nzeta: 1\nalpha: {x: [1, 2]}\n" +
		"mid: kept\nstatus: draft\nokf_verified: {by: human:a, at: 2026-01-01T00:00:00Z}\n" +
		"generated: {by: tool/1, at: 2026-01-01T00:00:00Z}\nokf_type: Case Study\n---\n\nBody stays.\n"
	if got := read(t, root, "studies/acme-rollout.md"); got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
	if !strings.Contains(read(t, root, "notes/plain.md"), "generated:\n  by: khub\n") {
		t.Fatalf("a concept without generated was not stamped:\n%s", read(t, root, "notes/plain.md"))
	}
	if c := res.Imported[1]; c.OKFType != "Case Study" || c.StatusWas != "stable" || !c.VerifiedQuarantined ||
		res.Imported[0].StatusWas != "" || res.Imported[0].VerifiedQuarantined {
		t.Fatalf("imported %+v", res.Imported)
	}
}

// An OKF type lands in the type the map names, else a declared type of the
// same name or slug; anything else is skipped, never invented.
func TestImportMapsTypes(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "a.md", "---\ntype: Metric\n---\n")
	write(t, bundle, "b.md", "---\ntype: Case Study\n---\n")
	write(t, bundle, "c.md", "---\ntype: Playbook\n---\n")
	write(t, bundle, "d.md", "---\ntype: Vendor\n---\n")
	mapFile := filepath.Join(t.TempDir(), "map.yaml")
	write(t, filepath.Dir(mapFile), "map.yaml", "Metric: note\n")
	res := run(t, root, bundle, okfimport.Options{TypeMap: mapFile})
	if got, want := imported(res), []string{"a -> note/a", "b -> case-study/b", "d -> vendor/d"}; !slices.Equal(got, want) {
		t.Fatalf("imported %v", got)
	}
	if got := reasons(res); !reflect.DeepEqual(got, map[string]string{"c": "unmapped_type"}) {
		t.Fatalf("skipped %v", got)
	}
	if !strings.Contains(read(t, root, "vendors.yaml"), "d:\n") {
		t.Fatal("a collection type did not get its row")
	}
}

// The slug is the file stem: a taken slug, a second concept on the same slug
// and an unusable stem are skipped, never suffixed or merged. A folder
// entity exported as <slug>/_index.md keeps its folder's name.
func TestImportSlugConflicts(t *testing.T) {
	root := newWS(t)
	write(t, root, "notes/taken.md", "---\ntype: note\n---\n")
	bundle := t.TempDir()
	write(t, bundle, "taken.md", "---\ntype: note\n---\n")
	write(t, bundle, "x/twice.md", "---\ntype: note\n---\n")
	write(t, bundle, "y/twice.md", "---\ntype: note\n---\n")
	write(t, bundle, "!!!.md", "---\ntype: note\n---\n")
	write(t, bundle, "folder-entity/_index.md", "---\ntype: note\n---\n")
	res := run(t, root, bundle, okfimport.Options{})
	if got, want := reasons(res), map[string]string{
		"taken": "slug_taken", "y/twice": "slug_taken", "!!!": "no_slug_source",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skipped %v", got)
	}
	if got, want := imported(res), []string{"folder-entity/_index -> note/folder-entity", "x/twice -> note/twice"}; !slices.Equal(got, want) {
		t.Fatalf("imported %v", got)
	}
}

// A body link or a sources resource that names another imported concept —
// bundle-absolute, relative or with an anchor — becomes a `references` edge;
// broken and external links are counted and left in the body. An image, a
// directory or an index is not a concept link.
func TestImportResolvesLinks(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	body := "See [b](/notes/b.md), [c](../other/c.md#part), [b again](b.md),\n" +
		"[gone](missing.md), [site](https://x.example), ![img](pic.md), [dir](sub/), [idx](index.md).\n"
	write(t, bundle, "notes/a.md", "---\ntype: note\nsources:\n- resource: /notes/b.md\n"+
		"- resource: dashboards/rev\n---\n"+body)
	write(t, bundle, "notes/b.md", "---\ntype: note\n---\n")
	write(t, bundle, "other/c.md", "---\ntype: note\n---\n")
	res := run(t, root, bundle, okfimport.Options{})
	if res.Links != (okfimport.Links{Resolved: 4, Broken: 1, External: 1}) || res.ReferencesAdded != 2 {
		t.Fatalf("links %+v, references %d", res.Links, res.ReferencesAdded)
	}
	got := read(t, root, "notes/a.md")
	if !strings.Contains(got, "references:\n- b\n- c\n") || !strings.Contains(got, body) {
		t.Fatalf("a:\n%s", got)
	}
}

// export's `# Related` section reads back into its declared relations and is
// stripped; a target the frontmatter already names, spelled differently, is
// not added twice. A section with a predicate the type does not declare is
// the producer's own prose: it stays, and its links read as ordinary links.
func TestImportReadsBackRelated(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "notes/a.md", "---\ntype: note\ncites:\n- note/b\n---\nBody.\n\n# Related\n\n"+
		"- cites: [B](/notes/b.md)\n- cites: [C](/notes/c.md)\n- cites: ghost\n")
	write(t, bundle, "notes/b.md", "---\ntype: note\n---\n")
	write(t, bundle, "notes/c.md", "---\ntype: note\n---\nTheirs.\n\n# Related\n\n- owner: [B](/notes/b.md)\n")
	res := run(t, root, bundle, okfimport.Options{})
	if got := read(t, root, "notes/a.md"); !strings.Contains(got, "cites:\n- note/b\n- c\n") ||
		strings.Contains(got, "# Related") || !strings.Contains(got, "---\nBody.\n") {
		t.Fatalf("a:\n%s", got)
	}
	if got := read(t, root, "notes/c.md"); !strings.Contains(got, "# Related\n\n- owner: [B](/notes/b.md)\n") ||
		!strings.Contains(got, "references:\n- b\n") {
		t.Fatalf("c:\n%s", got)
	}
	if res.Links.Broken != 1 {
		t.Fatalf("links %+v", res.Links)
	}
}

// A relation value nothing answers to is moved to `okf_unresolved` and
// reported: import never writes a dangling edge.
func TestImportQuarantinesUnresolvedRelations(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "notes/a.md", "---\ntype: note\ncites:\n- b\n- ghost\n- also-gone\n---\n")
	write(t, bundle, "notes/b.md", "---\ntype: note\n---\n")
	res := run(t, root, bundle, okfimport.Options{})
	got := read(t, root, "notes/a.md")
	if !strings.Contains(got, "cites:\n- b\n") || !strings.Contains(got, "okf_unresolved:\n  cites:\n  - ghost\n  - also-gone\n") {
		t.Fatalf("a:\n%s", got)
	}
	want := []okfimport.Unresolved{{Concept: "notes/a", Field: "cites", Value: "ghost"},
		{Concept: "notes/a", Field: "cites", Value: "also-gone"}}
	if !reflect.DeepEqual(res.Unresolved, want) {
		t.Fatalf("unresolved %+v", res.Unresolved)
	}
}

// The id follows the type's scheme, minted as add mints one around the stem,
// with the stem kept as an alias so the old name still resolves; a stem that
// already follows the scheme is kept. Links resolve to the minted id. A
// by-value prefix whose deciding attribute is unset cannot mint: skipped.
func TestImportMintsTheTypesIDs(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "decisions/use-postgres.md", "---\ntype: decision\ntitle: Use Postgres\ncreated: 2026-01-15\n---\n")
	write(t, bundle, "decisions/ad-2026-02-01-keep.md", "---\ntype: decision\n---\n")
	write(t, bundle, "parts/hex.md", "---\ntype: part\nkind: nut\n---\n")
	write(t, bundle, "parts/mystery.md", "---\ntype: part\n---\n")
	write(t, bundle, "notes/n.md", "---\ntype: note\n---\nSee [the call](../decisions/use-postgres.md).\n")
	res := run(t, root, bundle, okfimport.Options{})
	if got, want := imported(res), []string{
		"decisions/ad-2026-02-01-keep -> decision/ad-2026-02-01-keep",
		"decisions/use-postgres -> decision/ad-2026-01-15-use-postgres",
		"notes/n -> note/n",
		"parts/hex -> part/nt-hex",
	}; !slices.Equal(got, want) {
		t.Fatalf("imported %v", got)
	}
	if got := reasons(res); !reflect.DeepEqual(got, map[string]string{"parts/mystery": "id_prefix_undecided"}) {
		t.Fatalf("skipped %v", got)
	}
	if !strings.Contains(read(t, root, "decisions/ad-2026-01-15-use-postgres.md"), "aliases:\n- use-postgres\n") {
		t.Fatal("the stem was not kept as an alias")
	}
	if !strings.Contains(read(t, root, "notes/n.md"), "references:\n- ad-2026-01-15-use-postgres\n") {
		t.Fatalf("n:\n%s", read(t, root, "notes/n.md"))
	}
	if res.Imported[1].Alias != "use-postgres" || res.Imported[0].Alias != "" {
		t.Fatalf("aliases %+v", res.Imported)
	}
}

// A document that is not a concept is skipped with its reason; reserved
// files, hidden directories and links are never read.
func TestImportSkipsWhatIsNotAConcept(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "plain.md", "# no frontmatter\n")
	write(t, bundle, "bad.md", "---\ntype: [unclosed\n---\n")
	write(t, bundle, "untyped.md", "---\ntitle: T\n---\n")
	write(t, bundle, "index.md", "---\nokf_version: '0.2'\n---\n")
	write(t, bundle, "sub/log.md", "# log\n")
	write(t, bundle, ".hidden/h.md", "---\ntype: note\n---\n")
	secret := filepath.Join(t.TempDir(), "secret.md")
	write(t, filepath.Dir(secret), "secret.md", "---\ntype: note\n---\nsecret\n")
	if err := os.Symlink(secret, filepath.Join(bundle, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(secret), filepath.Join(bundle, "linkdir")); err != nil {
		t.Fatal(err)
	}
	res := run(t, root, bundle, okfimport.Options{})
	if got, want := reasons(res), map[string]string{
		"plain": "no_frontmatter", "bad": "malformed_frontmatter", "untyped": "missing_type", "link": "symlink",
		"linkdir": "symlink",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skipped %v", got)
	}
	if len(res.Imported) != 0 {
		t.Fatalf("imported %v", res.Imported)
	}
}

// A dry run plans the same import and writes nothing.
func TestImportDryRunWritesNothing(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "a.md", "---\ntype: note\n---\n")
	res := run(t, root, bundle, okfimport.Options{DryRun: true})
	if len(res.Imported) != 1 || res.Imported[0].Path != "notes/a.md" {
		t.Fatalf("imported %+v", res.Imported)
	}
	if _, err := os.Stat(filepath.Join(root, "notes", "a.md")); !os.IsNotExist(err) {
		t.Fatal("a dry run wrote the entity")
	}
}

// The run is refused whole when the bundle is missing, overlaps the
// workspace, or the type map is not a mapping of strings.
func TestImportRefusals(t *testing.T) {
	root := newWS(t)
	code := func(bundle string, opt okfimport.Options) string {
		_, err := okfimport.Import(root, bundle, opt)
		var l *errs.Located
		if !errors.As(err, &l) {
			t.Fatalf("want a located error for %s, got %v", bundle, err)
		}
		return l.Code
	}
	if c := code(filepath.Join(root, "nope"), okfimport.Options{}); c != "import_bundle_unreadable" {
		t.Fatal(c)
	}
	write(t, root, "inner/a.md", "---\ntype: note\n---\n")
	if c := code(filepath.Join(root, "inner"), okfimport.Options{}); c != "import_bundle_overlaps" {
		t.Fatal(c)
	}
	if c := code(filepath.Dir(root), okfimport.Options{}); c != "import_bundle_overlaps" {
		t.Fatal(c)
	}
	bad := filepath.Join(t.TempDir(), "map.yaml")
	write(t, filepath.Dir(bad), "map.yaml", "- not a mapping\n")
	if c := code(t.TempDir(), okfimport.Options{TypeMap: bad}); c != "import_type_map_invalid" {
		t.Fatal(c)
	}
	undeclared := filepath.Join(t.TempDir(), "map.yaml")
	write(t, filepath.Dir(undeclared), "map.yaml", "Metric: nothing-declared\n")
	if c := code(t.TempDir(), okfimport.Options{TypeMap: undeclared}); c != "import_type_map_invalid" {
		t.Fatal(c)
	}
}

// A percent-encoded link names the decoded path; a bundle root reached
// through a symlink is read like any other.
func TestImportDecodesLinksAndFollowsTheRootLink(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "notes/a.md", "---\ntype: note\n---\nSee [the other](my%20notes.md?view=1#top).\n")
	write(t, bundle, "notes/my notes.md", "---\ntype: note\n---\n")
	link := filepath.Join(t.TempDir(), "bundle-link")
	if err := os.Symlink(bundle, link); err != nil {
		t.Fatal(err)
	}
	res := run(t, root, link, okfimport.Options{})
	if len(res.Imported) != 2 || res.Links.Resolved != 1 || res.Links.Broken != 0 {
		t.Fatalf("imported %v, links %+v", imported(res), res.Links)
	}
	if !strings.Contains(read(t, root, "notes/a.md"), "references:\n- my-notes\n") {
		t.Fatalf("a:\n%s", read(t, root, "notes/a.md"))
	}
}

// An import that collides names the bundle, not an --id import has none of.
func TestImportSlugTakenNamesTheBundle(t *testing.T) {
	root := newWS(t)
	write(t, root, "notes/taken.md", "---\ntype: note\n---\n")
	bundle := t.TempDir()
	write(t, bundle, "taken.md", "---\ntype: note\n---\n")
	res := run(t, root, bundle, okfimport.Options{})
	if len(res.Skipped) != 1 || strings.Contains(res.Skipped[0].Message, "--id") ||
		!strings.Contains(res.Skipped[0].Message, "rename the concept's file in the bundle") {
		t.Fatalf("skipped %+v", res.Skipped)
	}
}

// The plan runs to a fixed point. b is refused (two leads on a single-valued
// relation), so it never lands, and a's value naming it is quarantined like
// any other unresolved value instead of refusing a.
func TestImportPlansToAFixedPoint(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "notes/a.md", "---\ntype: note\ncites:\n- b\n---\n")
	write(t, bundle, "notes/b.md", "---\ntype: note\nlead:\n- x\n- y\n---\n")
	write(t, bundle, "notes/x.md", "---\ntype: note\n---\n")
	write(t, bundle, "notes/y.md", "---\ntype: note\n---\n")
	res := run(t, root, bundle, okfimport.Options{})
	if got := reasons(res); !reflect.DeepEqual(got, map[string]string{"notes/b": "cardinality_violation"}) {
		t.Fatalf("skipped %v", got)
	}
	if got, want := imported(res), []string{"notes/a -> note/a", "notes/x -> note/x", "notes/y -> note/y"}; !slices.Equal(got, want) {
		t.Fatalf("imported %v", got)
	}
	if got := read(t, root, "notes/a.md"); strings.Contains(got, "\ncites:") || !strings.Contains(got, "okf_unresolved:\n  cites:\n  - b\n") {
		t.Fatalf("a:\n%s", got)
	}
	if want := []okfimport.Unresolved{{Concept: "notes/a", Field: "cites", Value: "b"}}; !reflect.DeepEqual(res.Unresolved, want) {
		t.Fatalf("unresolved %+v", res.Unresolved)
	}
}

// A write that fails once the plan is final fails the run with its OS error
// and undoes every file it wrote: the new files are removed and the collection it added a row
// to is restored, so no edge points at a concept that never landed.
func TestImportWritesAllOrNone(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the user cannot write")
	}
	root := newWS(t)
	write(t, root, "vendors.yaml", "# vendors\nglobex:\n  name: Globex\n")
	write(t, root, "notes/existing.md", "---\ntype: note\n---\n")
	bundle := t.TempDir()
	write(t, bundle, "a/acme.md", "---\ntype: vendor\n---\n")
	write(t, bundle, "b/ad-2026-01-15-go.md", "---\ntype: decision\ncreated: 2026-01-15\n---\n")
	write(t, bundle, "c/n.md", "---\ntype: note\ncites:\n- acme\n---\n")
	before := snapshot(t, root)
	if err := os.Chmod(filepath.Join(root, "notes"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "notes"), 0o755) })
	_, err := okfimport.Import(root, bundle, okfimport.Options{Actor: "khub"})
	var pe *os.PathError
	if !errors.As(err, &pe) || !errors.Is(err, os.ErrPermission) || !strings.HasSuffix(filepath.ToSlash(pe.Path), "notes/n.md") {
		t.Fatalf("got %v", err)
	}
	if after := snapshot(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("the workspace changed:\nbefore %v\nafter  %v", before, after)
	}
}

// snapshot is every file of the workspace outside .khub/generated, by path.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == filepath.Join(".khub", "generated") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		b, err := os.ReadFile(p)
		out[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A stem another entity already answers to, by slug or alias, in the
// workspace or the batch, is not added to `aliases`: a lookup by it would be
// ambiguous. It is reported as dropped.
func TestImportDropsAStemAliasThatIsTaken(t *testing.T) {
	root := newWS(t)
	write(t, root, "notes/use-postgres.md", "---\ntype: note\n---\n")
	bundle := t.TempDir()
	write(t, bundle, "decisions/use-postgres.md", "---\ntype: decision\ncreated: 2026-01-15\n---\n")
	write(t, bundle, "decisions/use-redis.md", "---\ntype: decision\ncreated: 2026-01-15\n---\n")
	write(t, bundle, "decisions/use-kafka.md", "---\ntype: decision\ncreated: 2026-01-15\n---\n")
	write(t, bundle, "notes/cache.md", "---\ntype: note\naliases:\n- use-redis\n---\n")
	res := run(t, root, bundle, okfimport.Options{})
	got := map[string][2]string{}
	for _, c := range res.Imported {
		got[c.Concept] = [2]string{c.Alias, c.AliasDropped}
	}
	want := map[string][2]string{
		"decisions/use-kafka":    {"use-kafka", ""},
		"decisions/use-postgres": {"", "use-postgres"},
		"decisions/use-redis":    {"", "use-redis"},
		"notes/cache":            {"", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aliases %v", got)
	}
	if strings.Contains(read(t, root, "decisions/ad-2026-01-15-use-postgres.md"), "aliases") {
		t.Fatal("a taken stem was added to aliases")
	}
}

// Every key import writes is refused the same way when the concept already
// carries it: `okf_verified` beside `verified`, `okf_type` when the type maps
// elsewhere, and `okf_unresolved` when a value is unresolved. A key import
// does not need to write stays as written.
func TestImportRefusesAQuarantineKeyItWouldOverwrite(t *testing.T) {
	root := newWS(t)
	bundle := t.TempDir()
	write(t, bundle, "v.md", "---\ntype: note\nverified: {by: human:x, at: 2026-01-15T00:00:00Z}\nokf_verified: mine\n---\n")
	write(t, bundle, "t.md", "---\ntype: Case Study\nokf_type: mine\n---\n")
	write(t, bundle, "u.md", "---\ntype: note\ncites: [ghost]\nokf_unresolved: mine\n---\n")
	write(t, bundle, "kept.md", "---\ntype: note\nokf_unresolved: mine\nokf_type: mine\n---\n")
	res := run(t, root, bundle, okfimport.Options{})
	if got, want := reasons(res), map[string]string{
		"v": "quarantine_taken", "t": "quarantine_taken", "u": "quarantine_taken",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skipped %v", got)
	}
	for _, s := range res.Skipped {
		key := map[string]string{"v": "okf_verified", "t": "okf_type", "u": "okf_unresolved"}[s.Concept]
		if !strings.Contains(s.Message, "`"+key+"`") {
			t.Errorf("%s: %s", s.Concept, s.Message)
		}
	}
	if got := read(t, root, "notes/kept.md"); !strings.Contains(got, "okf_unresolved: mine\nokf_type: mine\n") {
		t.Fatalf("kept:\n%s", got)
	}
}
