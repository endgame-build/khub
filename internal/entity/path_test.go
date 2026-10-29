package entity

// get --path: the path forms and refusals the parity case cannot spell. The
// runner hands khub no absolute workspace path, and a symlink fixture would
// need a Windows twin, so absolute paths, a root reached through a symlink,
// a symlink inside the workspace and the collection id cap live here.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realWS is a workspace as the CLI hands it to the core: symlinks resolved.
func realWS(t *testing.T, preset string) string {
	t.Helper()
	ws, err := filepath.EvalSymlinks(newWS(t, preset))
	requireNoError(t, err)
	return ws
}

func getOneByPath(t *testing.T, ws, p string) string {
	t.Helper()
	views, err := GetManyByPath(ws, ws, []string{p}, false)
	requireNoError(t, err)
	return views[0].Type + "/" + views[0].Slug
}

func TestGetByPathAbsolute(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	if got := getOneByPath(t, ws, filepath.Join(ws, "clients", "acme.md")); got != "client/acme" {
		t.Fatalf("got %s", got)
	}
	_, err := GetManyByPath(ws, ws, []string{filepath.Join(filepath.Dir(ws), "elsewhere.md")}, false)
	_ = requireCode(t, err, "path_outside_workspace")
}

// An absolute path may spell the root through a symlink outside the
// workspace, as macOS /tmp spells /private/tmp.
func TestGetByPathRootThroughSymlink(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	link := filepath.Join(t.TempDir(), "ws-link")
	if err := os.Symlink(ws, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	if got := getOneByPath(t, ws, filepath.Join(link, "clients", "acme.md")); got != "client/acme" {
		t.Fatalf("got %s", got)
	}
	// A symlink outside the workspace to a directory inside it is not the
	// root, so the path is outside.
	sub := filepath.Join(t.TempDir(), "clients-link")
	if err := os.Symlink(filepath.Join(ws, "clients"), sub); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	_, err := GetManyByPath(ws, ws, []string{filepath.Join(sub, "acme.md")}, false)
	_ = requireCode(t, err, "path_outside_workspace")
}

// A symlink inside the workspace is refused the way storage refuses one, never
// followed to the entity it points at.
func TestGetByPathRefusesSymlinkInWorkspace(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	if err := os.Symlink("clients", filepath.Join(ws, "cl")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	_, err := GetManyByPath(ws, ws, []string{"cl/acme.md"}, false)
	_ = requireCode(t, err, "unsafe_path")
}

// Any file under a folder entity's directory resolves to that entity, however
// deep; an entity file of another type wins over the folder it sits in.
func TestGetByPathFolderOwnsItsTree(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "partnerships/acme/_index.md", kv("type", "partnership", "partner", "Acme"))
	mkdirAll(t, filepath.Join(ws, "partnerships", "acme", "deck"))
	writeFile(t, filepath.Join(ws, "partnerships", "acme", "deck", "q3.txt"), "slides\n")
	for _, p := range []string{"partnerships/acme", "partnerships/acme/_index.md", "partnerships/acme/deck", "partnerships/acme/deck/q3.txt"} {
		if got := getOneByPath(t, ws, p); got != "partnership/acme" {
			t.Fatalf("%s resolved to %s", p, got)
		}
	}
	_, err := GetManyByPath(ws, ws, []string{"partnerships/acme/deck/missing.txt"}, false)
	_ = requireCode(t, err, "path_not_entity")
}

// A collection file refuses with its ids, capped at ten, and names the calls
// that read one or list them all. An empty collection holds no entity.
func TestGetByPathCollection(t *testing.T) {
	ws, err := filepath.EvalSymlinks(newWSFrom(t, "preset-cells", filepath.Join(repoRoot(t), "parity", "corpus")))
	requireNoError(t, err)
	for i := range 12 {
		_, err := Create(ws, "repo", CreateOpts{Fields: fields("title", fmt.Sprintf("r%02d", i)), UseTemplate: true})
		requireNoError(t, err)
	}
	_, err = GetManyByPath(ws, ws, []string{"repos.jsonl"}, false)
	e := requireCode(t, err, "path_is_collection")
	requireMessageContains(t, e, "'repos.jsonl' is a repo collection holding 12 entities: "+
		"repo/r00, repo/r01, repo/r02, repo/r03, repo/r04, repo/r05, repo/r06, repo/r07, repo/r08, repo/r09 and 2 more. "+
		"Get one by id (khub get <id>) or list them (khub query --type repo)")

	writeFile(t, filepath.Join(ws, "vendors.yaml"), "")
	_, err = GetManyByPath(ws, ws, []string{"vendors.yaml"}, false)
	e = requireCode(t, err, "path_not_entity")
	requireMessageContains(t, e, "the vendor collection holds no entities")
}

func cellsWS(t *testing.T) string {
	t.Helper()
	ws, err := filepath.EvalSymlinks(newWSFrom(t, "preset-cells", filepath.Join(repoRoot(t), "parity", "corpus")))
	requireNoError(t, err)
	return ws
}

// A relative path reads from the caller's directory, as grep printed it. From
// inside a folder entity, `notes/alpha.md` is that folder's file, never the
// workspace's notes/alpha.md.
func TestGetByPathRelativeToBase(t *testing.T) {
	ws := cellsWS(t)
	for _, typ := range []string{"note", "dossier"} {
		title := map[string]string{"note": "Alpha", "dossier": "Gamma"}[typ]
		_, err := Create(ws, typ, CreateOpts{Fields: fields("title", title), UseTemplate: true})
		requireNoError(t, err)
	}
	gamma := filepath.Join(ws, "dossiers", "gamma")
	mkdirAll(t, filepath.Join(gamma, "notes"))
	writeFile(t, filepath.Join(gamma, "notes", "alpha.md"), "scratch\n")

	views, err := GetManyByPath(ws, gamma, []string{"notes/alpha.md", "../../notes/alpha.md", "_index.md", "."}, false)
	requireNoError(t, err)
	var got []string
	for _, v := range views {
		got = append(got, v.Type+"/"+v.Slug)
	}
	if want := []string{"dossier/gamma", "note/alpha", "dossier/gamma", "dossier/gamma"}; !equalStrings(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// A missing file names its workspace path, not the spelling passed.
	_, err = GetManyByPath(ws, gamma, []string{"missing.md"}, false)
	e := requireCode(t, err, "path_not_entity")
	requireMessageContains(t, e, "No entity at 'dossiers/gamma/missing.md': the file does not exist")
	// A base outside the workspace puts a relative path outside it.
	_, err = GetManyByPath(ws, filepath.Dir(ws), []string{"notes/alpha.md"}, false)
	_ = requireCode(t, err, "path_outside_workspace")
}

// A symlink inside the workspace back at the root is refused even when the
// root is spelled through a symlink outside, where only the outermost
// ancestor that is the root counts.
func TestGetByPathRefusesSelfLinkUnderOtherRootSpelling(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	link := filepath.Join(t.TempDir(), "ws-link")
	if err := os.Symlink(ws, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	if err := os.Symlink(".", filepath.Join(ws, "self")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	for _, p := range []string{
		filepath.Join(ws, "self", "clients", "acme.md"),
		filepath.Join(link, "self", "clients", "acme.md"),
	} {
		_, err := GetManyByPath(ws, ws, []string{p}, false)
		_ = requireCode(t, err, "unsafe_path")
	}
}

// Paths match byte for byte as scanned; a file found under another case
// names the scanned spelling. An ignored file says so.
func TestGetByPathSpellingAndIgnore(t *testing.T) {
	ws := realWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	seed(t, ws, "partnerships/acme/_index.md", kv("type", "partnership", "partner", "Acme"))
	// A case-sensitive filesystem finds no such file; a case-insensitive one
	// finds the entity's file under a spelling the scan does not hold.
	for p, scanned := range map[string]string{"Clients/Acme.md": "clients/acme.md", "PARTNERSHIPS/acme/_index.md": "partnerships/acme/_index.md", "PARTNERSHIPS/acme": "partnerships/acme"} {
		_, err := GetManyByPath(ws, ws, []string{p}, false)
		e := requireCode(t, err, "path_not_entity")
		if !strings.Contains(e.Message, "the file does not exist") {
			requireMessageContains(t, e, "paths match byte for byte as scanned; khub scanned this one as '"+scanned+"'")
		}
	}

	writeFile(t, filepath.Join(ws, ".khub", "ignore"), "examples/\n")
	seed(t, ws, "examples/sample.md", kv("type", "client", "name", "Sample"))
	_, err := GetManyByPath(ws, ws, []string{"examples/sample.md"}, false)
	e := requireCode(t, err, "path_not_entity")
	requireMessageContains(t, e, "it is ignored by .khub/ignore")
}

func TestGetByPathCollectionOfOne(t *testing.T) {
	ws := cellsWS(t)
	_, err := Create(ws, "repo", CreateOpts{Fields: fields("title", "solo"), UseTemplate: true})
	requireNoError(t, err)
	_, err = GetManyByPath(ws, ws, []string{"repos.jsonl"}, false)
	e := requireCode(t, err, "path_is_collection")
	requireMessageContains(t, e, "holding 1 entity: repo/solo.")
}
