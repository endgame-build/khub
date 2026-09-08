// Library-level assertions for Upgrade, ported from kb's tests/test_kb.py
// (test_upgrade_* and test_no_schema_*) against khub's three-layer workspace.
package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// notePresetGrown is the note preset one release later: a second type, and
// the version bumped with it.
const notePresetGrownOntology = `
version: "10.0.0"
ontology:
  entities:
    note:
      attributes:
        body: { type: text }
    memo:
      attributes:
        body: { type: text }
`

const notePresetGrownStorage = `
storage:
  note: { layout: file, path: notes }
  memo: { layout: file, path: memos }
`

// growPreset rewrites the note preset at src as its next release.
func growPreset(t *testing.T, src string) {
	t.Helper()
	writeFile(t, filepath.Join(src, "note", "ontology.yaml"), notePresetGrownOntology)
	writeFile(t, filepath.Join(src, "note", "storage.yaml"), notePresetGrownStorage)
}

func mustUpgrade(t *testing.T, root string, opt UpgradeOptions) *UpgradeResult {
	t.Helper()
	res, err := Upgrade(root, opt)
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	return res
}

func bakFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(root, ".khub"), func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".bak") {
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUpgradeRefusesWithoutAPreset(t *testing.T) {
	// find_root falls back to the cwd in kb; here the CLI resolves the
	// workspace, so the library guard is the config that records no preset.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t)})
	writeFile(t, filepath.Join(ws, ".khub", "config.yaml"), "name: ws\nversion: 9.9.9\n")

	_, err := Upgrade(ws, UpgradeOptions{})
	l := located(t, err)
	if l.Code != "no_preset" {
		t.Errorf("code = %s", l.Code)
	}
	if l.Message != "Workspace "+ws+" records no preset in .khub/config.yaml; run khub init <preset> first" {
		t.Errorf("message = %q", l.Message)
	}
}

func TestUpgradeOnAFreshInitReportsNothing(t *testing.T) {
	// init and upgrade write layer files through the same layerBytes, so an
	// upgrade straight after a scaffold finds nothing to replace — and an
	// untouched file collects no .bak on every upgrade.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})

	res := mustUpgrade(t, ws, UpgradeOptions{})
	if len(res.Config) != 0 || len(res.SingletonsCreated) != 0 || len(res.SchemaDrift) != 0 {
		t.Errorf("result = %+v", res)
	}
	if res.VersionFrom != res.VersionTo || res.VersionFrom == "" {
		t.Errorf("versions = %q -> %q", res.VersionFrom, res.VersionTo)
	}
	if !strings.Contains(readFile(t, filepath.Join(ws, ".khub", "config.yaml")), "version: "+res.VersionTo+"\n") {
		t.Error("config.yaml does not carry the version the result reports")
	}
	if res.Preset != "build-hub" || res.Path != ws {
		t.Errorf("preset/path = %q/%q", res.Preset, res.Path)
	}
	if baks := bakFiles(t, ws); len(baks) != 0 {
		t.Errorf("a fresh upgrade left backups: %v", baks)
	}
}

func TestUpgradeRestampsARetiredPresetName(t *testing.T) {
	// A workspace scaffolded as build-lite before 0.6.0 records that name.
	// upgrade resolves it through the alias instead of dying with
	// unknown_preset, and config.yaml comes out saying build-hub.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	config := filepath.Join(ws, ".khub", "config.yaml")
	writeFile(t, config, strings.Replace(readFile(t, config), "preset: build-hub\n", "preset: build-lite\n", 1))
	if !strings.Contains(readFile(t, config), "preset: build-lite\n") {
		t.Fatal("the fixture did not take")
	}

	res := mustUpgrade(t, ws, UpgradeOptions{})
	if res.Preset != "build-hub" {
		t.Errorf("preset = %q", res.Preset)
	}
	got := readFile(t, config)
	if !strings.Contains(got, "preset: build-hub\n") || strings.Contains(got, "build-lite") {
		t.Errorf("config.yaml was not restamped:\n%s", got)
	}
	if !strings.Contains(got, "version: "+res.VersionTo+"\n") {
		t.Error("config.yaml does not carry the version the result reports")
	}
	// The name is the only thing that moved: the layer files already match
	// what build-hub ships, so nothing is replaced and nothing is backed up.
	if len(res.Config) != 0 {
		t.Errorf("config = %+v", res.Config)
	}
	if baks := bakFiles(t, ws); len(baks) != 0 {
		t.Errorf("backups = %v", baks)
	}
}

func TestUpgradeResultSlicesAreNeverNil(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t)})
	res := mustUpgrade(t, ws, UpgradeOptions{})
	if res.Config == nil || res.SingletonsCreated == nil || res.SchemaDrift == nil {
		t.Errorf("nil slice in %+v", res)
	}
}

func TestUpgradeCopiesAnEditedSchemaAsideBeforeReplacingIt(t *testing.T) {
	// Overwriting is the point; doing it silently and unrecoverably to
	// someone's own ontology is not.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	storage := filepath.Join(ws, ".khub", "storage.yaml")
	shipped := readFile(t, storage)
	writeFile(t, storage, shipped+"\n# mine\n")

	res := mustUpgrade(t, ws, UpgradeOptions{})
	want := []ConfigChange{{Name: ".khub/storage.yaml", Action: "replaced", Backup: ".khub/storage.yaml.bak"}}
	if !reflect.DeepEqual(res.Config, want) {
		t.Errorf("config = %+v, want %+v", res.Config, want)
	}
	if !strings.Contains(readFile(t, storage+".bak"), "# mine") {
		t.Error("the edit is not in the .bak")
	}
	if readFile(t, storage) != shipped {
		t.Error("storage.yaml was not restored to what khub ships")
	}
}

func TestUpgradeCopiesAnEditedTemplateAsideBeforeReplacingIt(t *testing.T) {
	// Templates are workspace-owned the moment init writes them, so they get
	// the same treatment the layer files get: replaced, but never silently.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	tpl := filepath.Join(ws, ".khub", "templates", "adr.yaml")
	shipped := readFile(t, tpl)
	writeFile(t, tpl, shipped+"\n# mine\n")

	res := mustUpgrade(t, ws, UpgradeOptions{})
	want := []ConfigChange{{Name: ".khub/templates/adr.yaml", Action: "replaced",
		Backup: ".khub/templates/adr.yaml.bak"}}
	if !reflect.DeepEqual(res.Config, want) {
		t.Errorf("config = %+v, want %+v", res.Config, want)
	}
	if !strings.Contains(readFile(t, tpl+".bak"), "# mine") {
		t.Error("the edit is not in the .bak")
	}
	if readFile(t, tpl) != shipped {
		t.Error("the template was not restored")
	}
}

func TestUpgradeInstallsATemplateTheWorkspaceNeverHad(t *testing.T) {
	// A workspace created before the preset shipped templates has none, and
	// nothing about that is an edit to preserve — created, not backed up.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	if err := os.RemoveAll(filepath.Join(ws, ".khub", "templates")); err != nil {
		t.Fatal(err)
	}

	res := mustUpgrade(t, ws, UpgradeOptions{})
	if !exists(filepath.Join(ws, ".khub", "templates", "adr.yaml")) {
		t.Error("adr.yaml was not created")
	}
	for _, c := range res.Config {
		if c.Action != "created" || c.Backup != "" || !strings.HasPrefix(c.Name, ".khub/templates/") {
			t.Errorf("unexpected change %+v", c)
		}
	}
	if len(res.Config) == 0 {
		t.Error("no template creation reported")
	}
	if baks := bakFiles(t, ws); len(baks) != 0 {
		t.Errorf("a created template left backups: %v", baks)
	}
}

func TestUpgradeReplacesTheSchemaSoAShippedTypeCanArrive(t *testing.T) {
	// The reason upgrade exists as its own verb: provision reads the
	// workspace's own layer files, so leaving old ones in place means a type
	// shipped in a new release can never reach an existing workspace.
	src := presetSource(t)
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: src})
	growPreset(t, src)

	res := mustUpgrade(t, ws, UpgradeOptions{})
	if res.VersionFrom != "9.9.9" || res.VersionTo != "10.0.0" {
		t.Errorf("versions = %q -> %q", res.VersionFrom, res.VersionTo)
	}
	ontology := readFile(t, filepath.Join(ws, ".khub", "ontology.yaml"))
	if !strings.HasPrefix(ontology, "# khub-preset: note@10.0.0\n") || !strings.Contains(ontology, "memo:") {
		t.Errorf("the shipped ontology did not land:\n%s", ontology)
	}
	if !isDir(filepath.Join(ws, "memos")) {
		t.Error("the new type was not scaffolded")
	}
	if !strings.Contains(readFile(t, filepath.Join(ws, ".khub", "config.yaml")), "version: 10.0.0\n") {
		t.Error("config.yaml was not restamped")
	}
	// Both files whose bodies moved are backed up; policy.yaml (unchanged
	// body, bumped header) is replaced without one.
	want := []ConfigChange{
		{Name: ".khub/ontology.yaml", Action: "replaced", Backup: ".khub/ontology.yaml.bak"},
		{Name: ".khub/policy.yaml", Action: "replaced"},
		{Name: ".khub/storage.yaml", Action: "replaced", Backup: ".khub/storage.yaml.bak"},
	}
	if !reflect.DeepEqual(res.Config, want) {
		t.Errorf("config = %+v, want %+v", res.Config, want)
	}
	if baks := bakFiles(t, ws); !reflect.DeepEqual(baks, []string{".khub/ontology.yaml.bak", ".khub/storage.yaml.bak"}) {
		t.Errorf("backups = %v", baks)
	}
}

func TestHeaderOnlyDifferenceReplacesWithoutABackup(t *testing.T) {
	// A version bump that changes no layer body has no edit to preserve: the
	// header moves, the file is reported replaced, and no .bak is written.
	src := presetSource(t)
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: src})
	writeFile(t, filepath.Join(src, "note", "ontology.yaml"),
		strings.Replace(notePresetOntology, `"9.9.9"`, `"9.9.10"`, 1))

	res := mustUpgrade(t, ws, UpgradeOptions{})
	want := []ConfigChange{
		{Name: ".khub/ontology.yaml", Action: "replaced"},
		{Name: ".khub/policy.yaml", Action: "replaced"},
		{Name: ".khub/storage.yaml", Action: "replaced"},
	}
	if !reflect.DeepEqual(res.Config, want) {
		t.Errorf("config = %+v, want %+v", res.Config, want)
	}
	if baks := bakFiles(t, ws); len(baks) != 0 {
		t.Errorf("a header-only change left backups: %v", baks)
	}
	if res.VersionTo != "9.9.10" {
		t.Errorf("version_to = %q", res.VersionTo)
	}
}

func TestRestampTouchesOnlyTheVersion(t *testing.T) {
	// The workspace may have edited stale_days and left a comment beside it;
	// both survive the restamp.
	src := presetSource(t)
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: src})
	config := filepath.Join(ws, ".khub", "config.yaml")
	writeFile(t, config, strings.Replace(readFile(t, config),
		"stale_days: 90", "stale_days: 30 # a quarter is too long here", 1))
	growPreset(t, src)

	mustUpgrade(t, ws, UpgradeOptions{})
	got := readFile(t, config)
	for _, want := range []string{"version: 10.0.0\n", "stale_days: 30 # a quarter is too long here\n", "preset: note\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, got)
		}
	}
}

func TestNoSchemaKeepsTheWorkspaceOntologyAndNamesTheGap(t *testing.T) {
	src := presetSource(t)
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: src})
	before := readFile(t, filepath.Join(ws, ".khub", "ontology.yaml"))
	growPreset(t, src)

	res := mustUpgrade(t, ws, UpgradeOptions{NoSchema: true})
	if !reflect.DeepEqual(res.SchemaDrift, []string{"memo"}) {
		t.Errorf("schema_drift = %v", res.SchemaDrift)
	}
	if len(res.Config) != 0 {
		t.Errorf("config = %+v under --no-schema", res.Config)
	}
	if readFile(t, filepath.Join(ws, ".khub", "ontology.yaml")) != before {
		t.Error("ontology.yaml moved under --no-schema")
	}
	if exists(filepath.Join(ws, "memos")) {
		t.Error("a type the workspace ontology lacks was scaffolded")
	}
	// The provenance stamp describes the layer files, which were kept.
	if res.VersionTo != "9.9.9" || res.VersionFrom != "9.9.9" {
		t.Errorf("versions = %q -> %q", res.VersionFrom, res.VersionTo)
	}
	if !strings.Contains(readFile(t, filepath.Join(ws, ".khub", "config.yaml")), "version: 9.9.9\n") {
		t.Error("config.yaml was restamped under --no-schema")
	}
}

func TestUpgradeScaffoldsATypeTheWorkspaceOntologyGained(t *testing.T) {
	// Why upgrade shares provision with init rather than only copying files:
	// under --no-schema the workspace keeps its own ontology, and a type
	// added to it still needs its directory.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t)})
	ontology := filepath.Join(ws, ".khub", "ontology.yaml")
	writeFile(t, ontology, readFile(t, ontology)+"    memo:\n      attributes:\n        body: { type: text }\n")
	storage := filepath.Join(ws, ".khub", "storage.yaml")
	writeFile(t, storage, readFile(t, storage)+"  memo: { layout: file, path: memos }\n")

	res := mustUpgrade(t, ws, UpgradeOptions{NoSchema: true})
	if !isDir(filepath.Join(ws, "memos")) {
		t.Error("the gained type was not scaffolded")
	}
	if !strings.Contains(readFile(t, ontology), "memo:") {
		t.Error("the workspace ontology was replaced")
	}
	if len(res.SchemaDrift) != 0 {
		t.Errorf("schema_drift = %v: the workspace has MORE than shipped, not less", res.SchemaDrift)
	}
}

func TestUpgradeCreatesAMissingSingleton(t *testing.T) {
	// Creations only, the rule init follows: a deleted singleton comes back,
	// an existing one is never touched.
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "build-hub", ws, InitOptions{})
	prd := filepath.Join(ws, "knowledge", "prd.md")
	arc42 := filepath.Join(ws, "knowledge", "arc42.md")
	if err := os.Remove(prd); err != nil {
		t.Fatal(err)
	}
	writeFile(t, arc42, "---\ntype: arc42\ntitle: Ours\n---\nhand-written\n")

	res := mustUpgrade(t, ws, UpgradeOptions{})
	if !reflect.DeepEqual(res.SingletonsCreated, []string{"prd"}) {
		t.Errorf("singletons_created = %v", res.SingletonsCreated)
	}
	if !exists(prd) {
		t.Error("prd.md was not recreated")
	}
	if readFile(t, arc42) != "---\ntype: arc42\ntitle: Ours\n---\nhand-written\n" {
		t.Error("an existing singleton was touched")
	}
}

func TestUpgradeReadsTheRecordedPresetSource(t *testing.T) {
	// A workspace scaffolded with --preset-source upgrades from that same
	// tree, not from the packaged presets (which know no `note`).
	src := presetSource(t)
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: src})
	growPreset(t, src)
	res := mustUpgrade(t, ws, UpgradeOptions{})
	if res.VersionTo != "10.0.0" {
		t.Errorf("version_to = %q", res.VersionTo)
	}
}

func TestPresetSourceResolvesRelativeToTheWorkspace(t *testing.T) {
	ws := t.TempDir()
	cases := []struct{ config, want string }{
		{"preset: note\nsource: null\n", ""},
		{"preset: note\n", ""},
		{"preset: note\nsource: presets\n", filepath.Join(ws, "presets")},
		{"preset: note\nsource: /abs/presets\n", "/abs/presets"},
	}
	for _, tc := range cases {
		writeFile(t, filepath.Join(ws, ".khub", "config.yaml"), tc.config)
		got, err := PresetSource(ws)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("PresetSource(%q) = %q, want %q", tc.config, got, tc.want)
		}
	}
}

func TestEntityHashesSkipsTheRootIndex(t *testing.T) {
	// index.md at the root is a projection the CLI writes as an init tail; a
	// force re-init must not read it as a corpus seeded over. A nested
	// index.md is an ordinary entity-suffixed file.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.md"), "---\nokf_version: '0.1'\n---\n# Index\n")
	writeFile(t, filepath.Join(dir, "notes", "index.md"), "---\ntype: note\n---\n")
	got, err := entityHashes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("hashes = %v", got)
	}
	if _, ok := got["notes/index.md"]; !ok {
		t.Errorf("hashes = %v", got)
	}
}
