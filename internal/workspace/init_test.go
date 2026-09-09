// Library-level assertions ported from tests/test_init.py (the
// init_workspace half; the CLI rows are covered by parity fixtures), plus
// byte-level golden checks against parity/cases/init-wire-skills — the
// recorded tree manifests ARE the spec for what init writes.
package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/presets"
)

// A minimal, self-contained preset for the fast paths (tests/test_init.py
// NOTE_PRESET), in the three-layer shape.
const notePresetOntology = `
version: "9.9.9"
ontology:
  entities:
    note:
      attributes:
        body: { type: text }
`

const notePresetStorage = `
storage:
  note: { layout: file, path: notes }
`

// buildHubTemplates is every template build-hub ships, by name — one per md
// type, which is all eleven of them.
var buildHubTemplates = []string{
	"actor.yaml", "adr.yaml", "api.yaml", "arc42.yaml", "capability.yaml",
	"component.yaml", "prd.yaml", "repo.yaml", "requirement.yaml",
	"system.yaml", "use-case.yaml",
}

// presetSource is the preset_source fixture: a directory holding the tiny
// `note` preset in the directory layout.
func presetSource(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "presets")
	writeFile(t, filepath.Join(src, "note", "ontology.yaml"), notePresetOntology)
	writeFile(t, filepath.Join(src, "note", "storage.yaml"), notePresetStorage)
	return src
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o666); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func mustInit(t *testing.T, preset, path string, opt InitOptions) *InitResult {
	t.Helper()
	res, err := Init(preset, path, opt)
	if err != nil {
		t.Fatalf("Init(%s): %v", preset, err)
	}
	return res
}

func located(t *testing.T, err error) *errs.Located {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var l *errs.Located
	ok := errors.As(err, &l)
	if !ok {
		t.Fatalf("err is %T: %v", err, err)
	}
	return l
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// --- TS-WS-001-U02..U05 / U08: scaffold a workspace ------------------------------

func TestFlattenWritesOneSchema(t *testing.T) {
	// TS-WS-001-U02 (WS-001): the preset's three layers land as three files,
	// and the base block does NOT — it stays embedded in the binary.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t)})
	text := readFile(t, filepath.Join(ws, ".khub", "ontology.yaml"))
	if strings.Contains(text, "base:") {
		t.Error("the base block was copied into the workspace")
	}
	if !strings.Contains(text, "    note:\n") {
		t.Error("no preset entity")
	}
	if !strings.Contains(readFile(t, filepath.Join(ws, ".khub", "storage.yaml")), "note:") {
		t.Error("no storage decl")
	}
	if !exists(filepath.Join(ws, ".khub", "policy.yaml")) {
		t.Error("policy.yaml not written (empty layers still land)")
	}
}

func TestProvenanceHeaderStamped(t *testing.T) {
	// TS-WS-001-U03 (WS-009): the schema header carries preset@version.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t)})
	for _, layer := range []string{"ontology.yaml", "policy.yaml", "storage.yaml"} {
		head := strings.SplitN(readFile(t, filepath.Join(ws, ".khub", layer)), "\n", 2)[0]
		if head != "# khub-preset: note@9.9.9" {
			t.Fatalf("%s header = %q", layer, head)
		}
	}
}

func TestConfigCarriesProvenanceAndDefaults(t *testing.T) {
	// TS-WS-001-U04 (WS-009): config.yaml has provenance, name, and defaults.
	// `defaults` carries stale_days only — no `format` default is written.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t), Name: "acme"})
	text := readFile(t, filepath.Join(ws, ".khub", "config.yaml"))
	for _, want := range []string{"name: acme", "preset: note", "version: 9.9.9", "stale_days: 90"} {
		if !strings.Contains(text, want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "format:") {
		t.Errorf("config.yaml writes a format default:\n%s", text)
	}
	prov, err := Provenance(ws)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := prov.Get("preset"); v != "note" {
		t.Errorf("provenance preset = %v", v)
	}
	days, err := StaleDays(ws)
	if err != nil || days != DefaultStaleDays {
		t.Errorf("StaleDays = %d, %v", days, err)
	}
}

func TestNameDefaultsToTargetDir(t *testing.T) {
	// TS-WS-001-U04: name defaults to the target directory name.
	target := filepath.Join(t.TempDir(), "hq")
	res := mustInit(t, "note", target, InitOptions{PresetSource: presetSource(t)})
	if res.Name != "hq" {
		t.Fatalf("name = %q", res.Name)
	}
	if !strings.Contains(readFile(t, filepath.Join(target, ".khub", "config.yaml")), "name: hq") {
		t.Error("config name is not the dir name")
	}
}

func TestNameFallsBackWhenBlank(t *testing.T) {
	// A blank --name falls back to the target dir name, never an empty name.
	ws := filepath.Join(t.TempDir(), "ws")
	res := mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t), Name: ""})
	if res.Name != "ws" {
		t.Fatalf("name = %q", res.Name)
	}
}

func TestGitignoreExcludesGenerated(t *testing.T) {
	// TS-WS-001-U05 (WS-002): .khub/generated/ is gitignored, once.
	ws := filepath.Join(t.TempDir(), "ws")
	src := presetSource(t)
	mustInit(t, "note", ws, InitOptions{PresetSource: src})
	text := readFile(t, filepath.Join(ws, ".gitignore"))
	if text != ".khub/generated/\n" {
		t.Fatalf("gitignore = %q", text)
	}
	// Re-running appends nothing: the line is already there.
	mustInit(t, "note", ws, InitOptions{PresetSource: src, Force: true})
	if got := readFile(t, filepath.Join(ws, ".gitignore")); got != text {
		t.Fatalf("gitignore grew: %q", got)
	}
}

func TestGitignorePreservesTrailingNewlineShape(t *testing.T) {
	// _append_gitignore keeps an existing file's shape: a file with no final
	// newline gets one before the appended line.
	ws := filepath.Join(t.TempDir(), "ws")
	writeFile(t, filepath.Join(ws, ".gitignore"), "*.log")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t), Force: true})
	if got := readFile(t, filepath.Join(ws, ".gitignore")); got != "*.log\n.khub/generated/\n" {
		t.Fatalf("gitignore = %q", got)
	}
}

func TestEntityTreeLaidDown(t *testing.T) {
	// TS-WS-001-U02: one directory per type's storage path.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t)})
	if !isDir(filepath.Join(ws, "notes")) {
		t.Fatal("notes/ was not created")
	}
}

func TestSingletonPathsCreateOnlyTheirParent(t *testing.T) {
	// A singleton's or collection's path names a FILE: init creates the parent
	// directory and never the file itself, and creates nothing when the parent
	// IS the target.
	src := presetSource(t)
	writeFile(t, filepath.Join(src, "cells", "ontology.yaml"), `
version: "1.0.0"
ontology:
  entities:
    charter: {}
    row: {}
`)
	writeFile(t, filepath.Join(src, "cells", "storage.yaml"), `
storage:
  charter: { layout: singleton, path: charter.md }
  row: { layout: collection, format: yaml, path: registry/rows.yaml }
`)
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "cells", ws, InitOptions{PresetSource: src})
	if !isDir(filepath.Join(ws, "registry")) {
		t.Error("collection parent was not created")
	}
	if exists(filepath.Join(ws, "registry", "rows.yaml")) {
		t.Error("collection file was created")
	}
	// charter.md has no template, so no singleton is minted either.
	if exists(filepath.Join(ws, "charter.md")) {
		t.Error("template-less singleton was created")
	}
}

func TestTemplatesAreCopiedIntoTheWorkspace(t *testing.T) {
	// The workspace gets its own editable copy of every preset template, and
	// the directory is created whenever the preset ships one — even empty.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	for _, name := range buildHubTemplates {
		if !exists(filepath.Join(ws, ".khub", "templates", name)) {
			t.Errorf("missing template %s", name)
		}
	}

	src := presetSource(t)
	if err := os.MkdirAll(filepath.Join(src, "note", "templates"), 0o777); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "bare")
	mustInit(t, "note", bare, InitOptions{PresetSource: src})
	if !isDir(filepath.Join(bare, ".khub", "templates")) {
		t.Error("an empty preset templates/ did not produce a workspace copy")
	}

	// A preset with no templates/ at all produces none.
	plain := filepath.Join(t.TempDir(), "plain")
	mustInit(t, "note", plain, InitOptions{PresetSource: presetSource(t)})
	if exists(filepath.Join(plain, ".khub", "templates")) {
		t.Error("a template-less preset produced .khub/templates/")
	}
}

func TestPresetSourceIsRecordedAsProvenance(t *testing.T) {
	// source is str(preset_source) — echoed into both config.yaml and the
	// result — and "" (Python's None) when the packaged presets were used.
	src := presetSource(t)
	ws := filepath.Join(t.TempDir(), "ws")
	res := mustInit(t, "note", ws, InitOptions{PresetSource: src + string(filepath.Separator)})
	if res.Source != src {
		t.Errorf("source = %q, want %q", res.Source, src)
	}
	if !strings.Contains(readFile(t, filepath.Join(ws, ".khub", "config.yaml")), "source: "+src) {
		t.Error("config.yaml does not carry the source")
	}

	packaged := filepath.Join(t.TempDir(), "packaged")
	res = mustInit(t, "firm-ops", packaged, InitOptions{})
	if res.Source != "" {
		t.Errorf("source = %q, want the null marker", res.Source)
	}
	if !strings.Contains(readFile(t, filepath.Join(packaged, ".khub", "config.yaml")), "source: null\n") {
		t.Error("config.yaml does not write a null source")
	}
}

// --- TS-WS-001-U06 / U07: the guards, in order ------------------------------------

func TestUnknownPresetWritesNothing(t *testing.T) {
	// TS-WS-001-U06 (REQ-WS001-03): a bad preset leaves the target empty.
	dir := t.TempDir()
	_, err := Init("bogus", dir, InitOptions{})
	if code := located(t, err).Code; code != "unknown_preset" {
		t.Fatalf("code = %s", code)
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil || len(entries) != 0 {
		t.Fatalf("target is not empty: %v %v", entries, rerr)
	}
}

func TestNonemptyTargetGuarded(t *testing.T) {
	// TS-WS-001-U07 (REQ-WS001-04): a non-empty target needs --force, and the
	// message states that nothing already there is touched.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "stray.txt"), "keep me")
	_, err := Init("note", dir, InitOptions{PresetSource: presetSource(t)})
	l := located(t, err)
	if l.Code != "target_not_empty" {
		t.Fatalf("code = %s", l.Code)
	}
	for _, want := range []string{
		fmt.Sprintf("Target %s is not empty", dir),
		"scaffold alongside the existing files; no file already there is modified",
	} {
		if !strings.Contains(l.Message, want) {
			t.Errorf("message %q lacks %q", l.Message, want)
		}
	}
	if readFile(t, filepath.Join(dir, "stray.txt")) != "keep me" {
		t.Error("stray.txt was touched")
	}
	if exists(filepath.Join(dir, ".khub")) {
		t.Error(".khub was created")
	}
}

func TestEntityLessPresetRejected(t *testing.T) {
	// A preset declaring no entities is rejected, not silently scaffolded
	// empty — and the rejection lands before any write.
	src := presetSource(t)
	writeFile(t, filepath.Join(src, "hollow", "ontology.yaml"),
		"version: \"1.0.0\"\nontology: {entities: {}}\n")
	ws := filepath.Join(t.TempDir(), "ws")
	_, err := Init("hollow", ws, InitOptions{PresetSource: src})
	l := located(t, err)
	if l.Code != "empty_preset" {
		t.Fatalf("code = %s", l.Code)
	}
	if l.Message != "Preset 'hollow' declares no entities" {
		t.Fatalf("message = %q", l.Message)
	}
	if exists(ws) {
		t.Error("the target was created")
	}
}

func TestReinitRefusesADifferentPreset(t *testing.T) {
	// Preserving the schema means scaffolding another preset over it would
	// mint directories and singletons for types the active schema does not
	// declare — orphan files no read verb can see.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "firm-ops", ws, InitOptions{})
	_, err := Init("build-hub", ws, InitOptions{Force: true})
	l := located(t, err)
	if l.Code != "preset_mismatch" {
		t.Fatalf("code = %s", l.Code)
	}
	want := fmt.Sprintf("%s is a 'firm-ops' workspace; refusing to scaffold 'build-hub' over it. "+
		"Its .khub schema files are workspace-owned and would be kept, leaving files for "+
		"types the schema does not declare.", ws)
	if l.Message != want {
		t.Fatalf("message = %q", l.Message)
	}
	if exists(filepath.Join(ws, "knowledge", "prd.md")) {
		t.Error("build-hub's singleton was minted")
	}
	if !strings.Contains(readFile(t, filepath.Join(ws, ".khub", "config.yaml")), "firm-ops") {
		t.Error("config was rewritten")
	}
}

func TestPresetMismatchRunsBeforeTheEmptinessGuard(t *testing.T) {
	// Guard order is contract: a live workspace scaffolded with another preset
	// reports preset_mismatch, never target_not_empty.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	_, err := Init("firm-ops", ws, InitOptions{}) // no --force
	if code := located(t, err).Code; code != "preset_mismatch" {
		t.Fatalf("code = %s", code)
	}
}

func TestRetiredPresetNameInitsAsItsReplacement(t *testing.T) {
	// build-lite became build-hub in 0.6.0; kb's graduation command still
	// says `khub init build-lite`. The alias resolves and the workspace
	// records the canonical name, so nothing downstream ever sees the old one.
	ws := filepath.Join(t.TempDir(), "ws")
	res := mustInit(t, "build-lite", ws, InitOptions{})
	if res.Preset != "build-hub" || res.Version != presetVersion("build-hub") {
		t.Fatalf("preset/version = %q/%q", res.Preset, res.Version)
	}
	config := readFile(t, filepath.Join(ws, ".khub", "config.yaml"))
	if !strings.Contains(config, "preset: build-hub\n") || strings.Contains(config, "build-lite") {
		t.Errorf("config.yaml does not record the canonical name:\n%s", config)
	}
	for _, layer := range []string{"ontology.yaml", "policy.yaml", "storage.yaml"} {
		head := strings.SplitN(readFile(t, filepath.Join(ws, ".khub", layer)), "\n", 2)[0]
		if head != "# khub-preset: build-hub@"+presetVersion("build-hub") {
			t.Errorf("%s header = %q", layer, head)
		}
	}
	// Re-scaffolding under either name is the same preset, not a mismatch.
	if _, err := Init("build-lite", ws, InitOptions{Force: true}); err != nil {
		t.Errorf("re-init under the retired name: %v", err)
	}
}

// --- the cutover guarantee -------------------------------------------------------

func TestForceNeverOverwritesEntityMd(t *testing.T) {
	// TS-WS-001-U08 (REQ-WS001-05, WS-003): force-seed leaves entity .md
	// untouched and reports the corpus.
	dir := t.TempDir()
	entity := filepath.Join(dir, "notes", "first.md")
	writeFile(t, entity, "---\ntype: note\n---\nbody\n")
	before := readFile(t, entity)
	res := mustInit(t, "note", dir, InitOptions{PresetSource: presetSource(t), Force: true})
	if readFile(t, entity) != before {
		t.Error("the entity file changed")
	}
	if res.EntityFilesModified != 0 {
		t.Errorf("entity_files_modified = %d", res.EntityFilesModified)
	}
	if !res.SeededOverCorpus {
		t.Error("seeded_over_corpus is false over a corpus")
	}
	if !isDir(filepath.Join(dir, ".khub")) {
		t.Error(".khub was not created")
	}
}

func TestReinitOverEmptyWorkspaceIsNotACutover(t *testing.T) {
	// Re-running --force on an entity-empty workspace is not a corpus cutover.
	// (firm-ops mints no singletons, so nothing entity-shaped exists yet.)
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "firm-ops", ws, InitOptions{})
	res := mustInit(t, "firm-ops", ws, InitOptions{Force: true})
	if res.SeededOverCorpus {
		t.Error("seeded_over_corpus is true on an entity-empty workspace")
	}
	if res.EntityFilesModified != 0 {
		t.Errorf("entity_files_modified = %d", res.EntityFilesModified)
	}
}

func TestForceSeedToleratesDirectoryNamedMd(t *testing.T) {
	// Corpus-port regression: rglob("*.md") matches directories; init must not
	// crash, and a directory is not an entity file.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "archive", "docs", "data-backup.md"), 0o777); err != nil {
		t.Fatal(err)
	}
	res := mustInit(t, "note", dir, InitOptions{PresetSource: presetSource(t), Force: true})
	if res.EntityFilesModified != 0 {
		t.Errorf("entity_files_modified = %d", res.EntityFilesModified)
	}
	if res.SeededOverCorpus {
		t.Error("a directory named *.md counted as a corpus")
	}
}

func TestEntityHashesIsMeasuredNotAssumed(t *testing.T) {
	// The cutover count is arithmetic over measured hashes: mutate a file
	// between the two sweeps and the same subtraction init runs reports 1.
	// (Python injects the tamper by monkeypatching _append_gitignore; Go has
	// no import-time seam, so the measurement itself is asserted here.)
	dir := t.TempDir()
	entity := filepath.Join(dir, "notes", "a.md")
	writeFile(t, entity, "---\ntype: note\n---\noriginal\n")
	writeFile(t, filepath.Join(dir, "keep.md"), "steady\n")
	before, err := entityHashes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("before = %v", before)
	}
	writeFile(t, entity, "TAMPERED")
	after, err := entityHashes(dir)
	if err != nil {
		t.Fatal(err)
	}
	modified := 0
	for rel, digest := range before {
		if after[rel] != digest {
			modified++
		}
	}
	if modified != 1 {
		t.Fatalf("modified = %d", modified)
	}
}

func TestEntityHashesSkipsVendorAndWorkspaceTrees(t *testing.T) {
	// The sweep covers every entity-capable suffix but never descends into
	// .khub / .git / .venv / node_modules.
	dir := t.TempDir()
	for _, rel := range []string{
		"a.md", "b.json", "c.yaml", "d.jsonl",
		".khub/ontology.yaml", ".git/x.md", ".venv/y.json", "node_modules/p/package.json",
	} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(rel)), "x")
	}
	writeFile(t, filepath.Join(dir, "notes.txt"), "not an entity")
	got, err := entityHashes(dir)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"a.md", "b.json", "c.yaml", "d.jsonl"}) {
		t.Fatalf("keys = %v", keys)
	}
}

// --- re-init preserves workspace-owned files ---------------------------------------

func TestForceReinitPreservesSchemaAndTemplates(t *testing.T) {
	// The engagement owns its layer files outright — editing them IS the
	// override mechanism — and templates are workspace-owned after init.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	schemaPath := filepath.Join(ws, ".khub", "ontology.yaml")
	tplPath := filepath.Join(ws, ".khub", "templates", "prd.yaml")
	writeFile(t, schemaPath, readFile(t, schemaPath)+"\n# LOCAL OVERRIDE\n")
	writeFile(t, tplPath, readFile(t, tplPath)+"\n# LOCAL TEMPLATE EDIT\n")

	res := mustInit(t, "build-hub", ws, InitOptions{Force: true})

	if !strings.Contains(readFile(t, schemaPath), "# LOCAL OVERRIDE") {
		t.Error("ontology.yaml was restored")
	}
	if !strings.Contains(readFile(t, tplPath), "# LOCAL TEMPLATE EDIT") {
		t.Error("the template was restored")
	}
	for _, want := range []string{".khub/ontology.yaml", ".khub/policy.yaml", ".khub/storage.yaml",
		".khub/config.yaml", ".khub/templates/prd.yaml"} {
		if !contains(res.Preserved, want) {
			t.Errorf("preserved lacks %s: %v", want, res.Preserved)
		}
	}
	// Order is contract: the schema layers in write order, config, then
	// templates by name.
	if res.Preserved[0] != ".khub/ontology.yaml" || res.Preserved[1] != ".khub/policy.yaml" ||
		res.Preserved[2] != ".khub/storage.yaml" || res.Preserved[3] != ".khub/config.yaml" {
		t.Errorf("preserved order = %v", res.Preserved)
	}
	if !sort.StringsAreSorted(res.Preserved[4:]) {
		t.Errorf("templates are not sorted: %v", res.Preserved[4:])
	}
}

func TestReinitStillRestoresWhatIsActuallyMissing(t *testing.T) {
	// Preserving must not become "do nothing": a deleted template and a
	// deleted singleton are still recreated — those are creations.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	tpl := filepath.Join(ws, ".khub", "templates", "adr.yaml")
	singleton := filepath.Join(ws, "knowledge", "prd.md")
	if err := os.Remove(tpl); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(singleton); err != nil {
		t.Fatal(err)
	}

	res := mustInit(t, "build-hub", ws, InitOptions{Force: true})

	if !exists(tpl) {
		t.Error("the deleted template was not restored")
	}
	if !contains(res.SingletonsCreated, "prd") {
		t.Errorf("singletons_created = %v", res.SingletonsCreated)
	}
	if contains(res.Preserved, ".khub/templates/adr.yaml") {
		t.Errorf("a restored template was reported preserved: %v", res.Preserved)
	}
}

// --- rollback ----------------------------------------------------------------------

func TestFailedInitLeavesNoPartialKhub(t *testing.T) {
	// A failure mid-scaffold cleans up the partial .khub it created and any
	// singleton it minted; directories and the .gitignore line may remain.
	// The failure is a preset shipping a malformed body template for a
	// singleton — parity's preset-badtemplate, the one route by which
	// template_invalid reaches init.
	src := presetSource(t)
	writeFile(t, filepath.Join(src, "bad", "ontology.yaml"), `
version: "0.1.0"
ontology:
  entities:
    charter:
      attributes:
        title: { required: true }
`)
	writeFile(t, filepath.Join(src, "bad", "policy.yaml"), "policy:\n  charter: { orphan: true }\n")
	writeFile(t, filepath.Join(src, "bad", "storage.yaml"),
		"storage:\n  charter: { layout: singleton, path: charter.md }\n")
	writeFile(t, filepath.Join(src, "bad", "templates", "charter.yaml"),
		"- top level is a list, not a mapping\n")
	ws := filepath.Join(t.TempDir(), "ws")

	_, err := Init("bad", ws, InitOptions{PresetSource: src})
	l := located(t, err)
	if l.Code != "template_invalid" {
		t.Fatalf("code = %s (%s)", l.Code, l.Message)
	}
	if exists(filepath.Join(ws, ".khub")) {
		t.Error("the partial .khub survived")
	}
	if exists(filepath.Join(ws, "charter.md")) {
		t.Error("a singleton survived the rollback")
	}
	if !exists(filepath.Join(ws, ".gitignore")) {
		t.Error(".gitignore was removed (the unwind is best-effort, not atomic)")
	}
}

func TestFailedReinitKeepsAnExistingKhub(t *testing.T) {
	// The unwind removes only the .khub files this run WROTE: a re-init that
	// fails must not delete the workspace's own schema. The failing shape is
	// the mixed-generation one the post-scaffold resolve exists to catch — a
	// preserved ontology beside a freshly written storage layer annotating a
	// type that ontology never declared. Pre-resolve, this init reported
	// success and left a workspace no command could load.
	src := presetSource(t)
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: src})
	schemaPath := filepath.Join(ws, ".khub", "ontology.yaml")
	storagePath := filepath.Join(ws, ".khub", "storage.yaml")
	before := readFile(t, schemaPath)
	if err := os.Remove(storagePath); err != nil { // the workspace dropped its storage layer
		t.Fatal(err)
	}

	// Same preset name, moved on: its storage now annotates a type the
	// workspace's preserved ontology does not declare.
	writeFile(t, filepath.Join(src, "note", "storage.yaml"), notePresetStorage+`  ghost: { layout: file, path: ghosts }
`)

	_, err := Init("note", ws, InitOptions{PresetSource: src, Force: true})
	if err == nil {
		t.Fatal("expected the mixed-generation resolve error")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error %q does not name the undeclared type", err)
	}
	if !exists(schemaPath) || readFile(t, schemaPath) != before {
		t.Error("the pre-existing .khub was destroyed")
	}
	if exists(storagePath) {
		t.Error("the freshly written storage.yaml survived the unwind")
	}
}

func TestInitRejectsAPresetDeclaringABase(t *testing.T) {
	// The resolver forbids an authored ontology.base; flatten applies the same
	// rule at the one place the workspace file gets authored, BEFORE anything
	// is written — copying the block verbatim used to scaffold a workspace no
	// command could load.
	src := presetSource(t)
	writeFile(t, filepath.Join(src, "based", "ontology.yaml"),
		"ontology:\n  base:\n    attributes:\n      type: { type: text }\n  entities:\n    note: {}\n")
	ws := filepath.Join(t.TempDir(), "ws")
	_, err := Init("based", ws, InitOptions{PresetSource: src})
	l := located(t, err)
	if l.Code != "invalid_schema" || !strings.Contains(l.Message, "khub-owned") {
		t.Fatalf("err = %s (%s)", l.Code, l.Message)
	}
	if exists(filepath.Join(ws, ".khub")) {
		t.Error("the rejection must land before any write")
	}
}

func TestInitRejectsAFlatLayerFile(t *testing.T) {
	// A policy.yaml authored without the `policy:` wrapper — per-type entries
	// at the top level, the natural mistake — used to be read as an EMPTY
	// layer: init wrote `policy: {}` and every declared gate vanished with no
	// diagnostic anywhere.
	src := presetSource(t)
	writeFile(t, filepath.Join(src, "note", "policy.yaml"), "note: { orphan: true }\n")
	ws := filepath.Join(t.TempDir(), "ws")
	_, err := Init("note", ws, InitOptions{PresetSource: src})
	l := located(t, err)
	if l.Code != "invalid_schema" || !strings.Contains(l.Message, "'policy:'") {
		t.Fatalf("err = %s (%s)", l.Code, l.Message)
	}
}

// --- golden trees: parity/cases/init-wire-skills -----------------------------------

func TestGoldenRerunPreserves(t *testing.T) {
	// parity/cases/init-wire-skills/init-rerun-preserves step 3: a --force
	// re-init over a workspace holding one entity preserves every
	// workspace-owned file — the three layers, config and the eleven
	// templates — mints no singleton, and modifies nothing.
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(ws, 0o777); err != nil {
		t.Fatal(err)
	}
	inDir(t, ws, func() {
		mustInit(t, "build-hub", ".", InitOptions{})
		// Stand in for `khub add requirement` (the entity package owns that verb).
		writeFile(t, filepath.Join("knowledge", "requirements", "req-keep.md"),
			"---\ntype: requirement\n---\n")

		res := mustInit(t, "build-hub", ".", InitOptions{Force: true})
		if !res.SeededOverCorpus {
			t.Error("seeded_over_corpus is false")
		}
		if res.EntityFilesModified != 0 {
			t.Errorf("entity_files_modified = %d", res.EntityFilesModified)
		}
		if len(res.SingletonsCreated) != 0 {
			t.Errorf("singletons_created = %v", res.SingletonsCreated)
		}
		want := []string{
			".khub/ontology.yaml", ".khub/policy.yaml", ".khub/storage.yaml",
			".khub/config.yaml",
		}
		for _, name := range buildHubTemplates {
			want = append(want, ".khub/templates/"+name)
		}
		if !reflect.DeepEqual(res.Preserved, want) {
			t.Errorf("preserved = %v", res.Preserved)
		}
	})
}

func TestGoldenTemplateInvalidRollback(t *testing.T) {
	// parity/cases/init-wire-skills/err-template-invalid: the rollback leaves
	// exactly one file behind — the .gitignore line.
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(ws, 0o777); err != nil {
		t.Fatal(err)
	}
	corpus := repoPath(t, "parity", "corpus")
	inDir(t, ws, func() {
		_, err := Init("preset-badtemplate", ".", InitOptions{PresetSource: corpus})
		l := located(t, err)
		if l.Code != "template_invalid" {
			t.Fatalf("code = %s", l.Code)
		}
		want := "Template for 'charter' (.khub/templates/charter.yaml): " +
			"top level must be a mapping with a 'sections' list"
		if l.Message != want {
			t.Fatalf("message = %q", l.Message)
		}
	})
	assertManifest(t, ws, "err-template-invalid")
}

func TestGoldenEmptyPresetWritesNothing(t *testing.T) {
	// parity/cases/init-wire-skills/err-empty-preset: an empty tree manifest.
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(ws, 0o777); err != nil {
		t.Fatal(err)
	}
	corpus := repoPath(t, "parity", "corpus")
	inDir(t, ws, func() {
		_, err := Init("preset-empty", ".", InitOptions{PresetSource: corpus})
		if code := located(t, err).Code; code != "empty_preset" {
			t.Fatalf("code = %s", code)
		}
	})
	assertManifest(t, ws, "err-empty-preset")
}

// --- golden helpers ----------------------------------------------------------------

// presetVersion is the `version:` each shipped preset declares; a retired
// name reports the version of the preset it resolves to, and an unknown one
// reports nothing so the comparison fails loudly.
func presetVersion(preset string) string {
	switch presets.Canonical(preset, presets.Embedded()) {
	case "build-hub":
		return "0.6.0"
	case "firm-ops":
		return "0.3.0"
	}
	return ""
}

// inDir runs fn with the process working directory at dir. Init resolves "."
// against it, which is how the recorder drives the CLI.
func inDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := os.Chdir(prev); cerr != nil {
			t.Fatal(cerr)
		}
	}()
	fn()
}

func repoPath(t *testing.T, parts ...string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

// assertManifest compares the scaffolded tree against a recorded parity
// fixture, in the runner's own "sha256 exec-bit relpath" sorted format.
func assertManifest(t *testing.T, root, fixture string) {
	t.Helper()
	wantPath := repoPath(t, "parity", "cases", "init-wire-skills", fixture, "expected", "tree.manifest")
	raw, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	got := treeManifest(t, root)
	if got != string(raw) {
		t.Errorf("tree.manifest mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", fixture, raw, got)
	}
}

func treeManifest(t *testing.T, root string) string {
	t.Helper()
	var rels []string
	byRel := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() {
			return nil //nolint:nilerr // the runner ignores walk errors too
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".git/") || strings.Contains(rel, "/.git/") ||
			strings.Contains(rel, ".khub/generated/") {
			return nil
		}
		payload, _ := os.ReadFile(p)
		mode := "-"
		if info.Mode()&0o111 != 0 {
			mode = "x"
		}
		sum := sha256.Sum256(payload)
		rels = append(rels, rel)
		byRel[rel] = fmt.Sprintf("%s %s %s", hex.EncodeToString(sum[:]), mode, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sort by PATH, matching the runner. Sorting the composed line ordered by
	// hash, so one changed byte reshuffled the whole manifest.
	sort.Strings(rels)
	lines := make([]string, len(rels))
	for i, rel := range rels {
		lines[i] = byRel[rel]
	}
	return strings.Join(lines, "\n") + "\n"
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
