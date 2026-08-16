package search

// Shared fixtures for the search tests, mirroring tests/conftest.py's fresh_ws
// and seed. init_workspace lives in internal/workspace and is not depended on
// here: the flatten (core base + preset entities) is reproduced directly.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

type kv struct {
	K string
	V any
}

func date(iso string) canon.Date { return canon.Date{ISO: iso} }

func freshWS(t *testing.T) string { return wsFromPreset(t, "firm-ops") }

func wsFromPreset(t *testing.T, preset string) string {
	t.Helper()
	dir := filepath.Join("..", "..", "presets")
	core := loadYAMLFile(t, filepath.Join(dir, "core.yaml"))
	spec := loadYAMLFile(t, filepath.Join(dir, preset, "schema.yaml"))
	return writeWS(t, core, spec, preset)
}

func wsFromSchema(t *testing.T, presetYAML string) string {
	t.Helper()
	core := loadYAMLFile(t, filepath.Join("..", "..", "presets", "core.yaml"))
	spec, err := canon.LoadDoc(presetYAML)
	if err != nil {
		t.Fatalf("parse inline preset: %v", err)
	}
	specMap, _ := spec.(*omap.Map)
	if specMap == nil {
		t.Fatal("inline preset is not a mapping")
	}
	return writeWS(t, core, specMap, "fixture")
}

func writeWS(t *testing.T, core, spec *omap.Map, preset string) string {
	t.Helper()
	root := t.TempDir()
	base, _ := core.Get("base")
	entities, _ := spec.Get("entities")
	version, _ := spec.Get("version")
	if version == nil {
		version = "0.0.0"
	}
	merged := omap.New()
	merged.Set("base", base)
	merged.Set("entities", entities)
	text, err := canon.DumpWide(merged)
	if err != nil {
		t.Fatalf("dump schema: %v", err)
	}
	khub := filepath.Join(root, ".khub")
	mkdirAll(t, khub)
	writeFile(t, filepath.Join(khub, "schema.yaml"), text)

	cfg := omap.New()
	cfg.Set("name", "ws")
	cfg.Set("preset", preset)
	cfg.Set("version", version)
	cfg.Set("source", nil)
	defaults := omap.New()
	defaults.Set("stale_days", int64(90))
	cfg.Set("defaults", defaults)
	cfgText, err := canon.DumpWide(cfg)
	if err != nil {
		t.Fatalf("dump config: %v", err)
	}
	writeFile(t, filepath.Join(khub, "config.yaml"), cfgText)
	return root
}

func seed(t *testing.T, root, relpath string, f ...kv) {
	t.Helper()
	m := omap.New()
	for _, x := range f {
		m.Set(x.K, x.V)
	}
	text, err := canon.DumpWide(m)
	if err != nil {
		t.Fatalf("dump %s: %v", relpath, err)
	}
	p := filepath.Join(root, filepath.FromSlash(relpath))
	mkdirAll(t, filepath.Dir(p))
	writeFile(t, p, "---\n"+text+"---\n")
}

func seedRaw(t *testing.T, root, relpath, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(relpath))
	mkdirAll(t, filepath.Dir(p))
	writeFile(t, p, text)
}

func loadYAMLFile(t *testing.T, path string) *omap.Map {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	v, err := canon.LoadDoc(string(raw))
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	m, _ := v.(*omap.Map)
	if m == nil {
		t.Fatalf("%s is not a mapping", path)
	}
	return m
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }
