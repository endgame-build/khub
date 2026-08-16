package integrity

// Test scaffolding for the two gates, mirroring tests/conftest.py's fresh_ws /
// ws_for / seed fixtures and tests/test_integrity.py's seed_clean. Clean trees
// go through entity.Create (so referential integrity holds); breaks are written
// directly so the gate sees them.

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/workspace"
)

type kv struct {
	K string
	V any
}

func freshWS(t *testing.T) string { return wsFor(t, "firm-ops") }

func wsFor(t *testing.T, preset string) string {
	t.Helper()
	root := t.TempDir()
	if _, err := workspace.Init(preset, root, workspace.InitOptions{}); err != nil {
		t.Fatalf("init %s: %v", preset, err)
	}
	return root
}

// initFrom is init_workspace(preset, root, preset_source=dir) — a schema
// authored by the test rather than shipped.
func initFrom(t *testing.T, root, preset, presetSource string) {
	t.Helper()
	if _, err := workspace.Init(preset, root, workspace.InitOptions{PresetSource: presetSource}); err != nil {
		t.Fatalf("init %s from %s: %v", preset, presetSource, err)
	}
}

// fields builds the raw --field map entity.Create takes.
func fields(pairs ...string) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i], pairs[i+1])
	}
	return m
}

// create is entity.create(root, type_, fields, id_=id) with Python's
// use_template=True default.
func create(t *testing.T, root, typeName, id string, pairs ...string) {
	t.Helper()
	mustCreate(t, root, typeName, id, false, pairs...)
}

func createDraft(t *testing.T, root, typeName, id string, pairs ...string) {
	t.Helper()
	mustCreate(t, root, typeName, id, true, pairs...)
}

func mustCreate(t *testing.T, root, typeName, id string, draft bool, pairs ...string) {
	t.Helper()
	_, err := entity.Create(root, typeName, entity.CreateOpts{
		Fields: fields(pairs...), ID: id, Draft: draft, UseTemplate: true,
	})
	if err != nil {
		t.Fatalf("create %s/%s: %v", typeName, id, err)
	}
}

func link(t *testing.T, root, id, predicate, target string) {
	t.Helper()
	if _, err := entity.Link(root, id, predicate, target); err != nil {
		t.Fatalf("link %s %s %s: %v", id, predicate, target, err)
	}
}

func update(t *testing.T, root, id string, pairs ...string) {
	t.Helper()
	if _, err := entity.Update(root, id, entity.UpdateOpts{Fields: fields(pairs...)}); err != nil {
		t.Fatalf("update %s: %v", id, err)
	}
}

// seed writes an md entity's frontmatter directly (conftest.py's seed fixture).
func seed(t *testing.T, root, relpath string, f ...kv) {
	t.Helper()
	meta := omap.New()
	for _, x := range f {
		meta.Set(x.K, x.V)
	}
	text, err := canon.DumpWide(meta)
	if err != nil {
		t.Fatalf("dump %s: %v", relpath, err)
	}
	writeRaw(t, root, relpath, "---\n"+text+"---\n")
}

func writeRaw(t *testing.T, root, relpath, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(relpath))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, root, relpath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(relpath)), 0o755); err != nil {
		t.Fatal(err)
	}
}

// malformedMD is test_scan_hardening.py's MALFORMED_MD: an unterminated quote
// plus an unclosed flow sequence.
const malformedMD = "---\ntype: client\nname: \"unterminated\nbad: [1, 2\n---\nbody\n"

// malformedWS is test_scan_hardening.py's malformed_ws: a sound client beside a
// malformed client file inside the same layout.
func malformedWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	seed(t, root, "clients/real.md", kv{"type", "client"}, kv{"name", "Real"},
		kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})
	writeRaw(t, root, "clients/broken.md", malformedMD)
	return root
}

func readRaw(t *testing.T, root, relpath string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relpath)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// seedClean is test_integrity.py's seed_clean: every required field present,
// every relation resolving.
func seedClean(t *testing.T, root string) {
	t.Helper()
	create(t, root, "person", "noor", "name", "Noor", "role", "partner", "mood", "good")
	create(t, root, "client", "initech", "name", "Initech")
	create(t, root, "opportunity", "initech-deal",
		"name", "Initech Deal", "stage", "prospect", "client", "initech", "owner", "noor")
	create(t, root, "project", "initech-pov",
		"client", "initech", "owner", "noor", "active", "true")
	create(t, root, "meeting", "kickoff",
		"date", "2026-06-01T10:00:00", "call_type", "client", "source", "recording",
		"engagement", "initech-pov")
}

// cleanWS is test_integrity.py's clean_ws: a sound firm-ops tree plus a
// frontmatter-less reference doc outside every layout.
func cleanWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	seedClean(t, root)
	writeRaw(t, root, "identity/mission.md", "# Mission\n\nNo frontmatter here.\n")
	return root
}

func mustValidate(t *testing.T, root string, target *string, strict bool) *ValidateReport {
	t.Helper()
	report, err := Validate(root, target, strict)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	return report
}

func mustCheck(t *testing.T, root string, strict bool) *CheckReport {
	t.Helper()
	report, err := Check(root, strict)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return report
}

// errorKeys renders the (id, field) pairs the Python rows assert on.
func errorKeys(report *ValidateReport) map[[2]string]string {
	out := map[[2]string]string{}
	for _, e := range report.Errors {
		out[[2]string{e.ID(), e.Field}] = e.Reason
	}
	return out
}

func incompleteByID(report *CheckReport) map[string]Incomplete {
	out := map[string]Incomplete{}
	for _, i := range report.Incomplete {
		out[i.ID()] = i
	}
	return out
}

func containsID(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func anySuffix(ss []string, suffix string) bool {
	for _, s := range ss {
		if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

func cycleSet(cycle []string) []string {
	out := append([]string{}, cycle...)
	sort.Strings(out)
	return out
}

func locatedCode(err error) string {
	var l *errs.Located
	if asLocated(err, &l) {
		return l.Code
	}
	return ""
}

func ptr[T any](v T) *T { return &v }
