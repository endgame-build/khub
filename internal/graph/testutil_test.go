package graph

// Shared fixtures for the graph tests, mirroring tests/conftest.py's fresh_ws
// and seed. init_workspace lives in internal/workspace and is not depended on
// here: the flatten (core base + preset entities) is reproduced directly so the
// walk tests stay independent of the init port.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
)

// kv is one ordered frontmatter field — Python's **meta kwargs keep insertion
// order, and so does the emitted YAML.
type kv struct {
	K string
	V any
}

func date(iso string) canon.Date { return canon.Date{ISO: iso} }

// freshWS scaffolds a firm-ops workspace from the authored presets.
func freshWS(t *testing.T) string {
	t.Helper()
	return wsFromPreset(t, "firm-ops")
}

func wsFromPreset(t *testing.T, preset string) string {
	t.Helper()
	dir := filepath.Join("..", "..", "presets", preset)
	root := t.TempDir()
	khub := filepath.Join(root, ".khub")
	mkdirAll(t, khub)
	for _, layer := range []string{"ontology.yaml", "policy.yaml", "storage.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, layer))
		if err != nil {
			continue // policy/storage are optional layers
		}
		writeFile(t, filepath.Join(khub, layer), string(data))
	}
	writeConfig(t, khub, preset, "0.0.0")
	return root
}

// wsFromSchema scaffolds a workspace from an inline preset document (the
// HISTORY_PRESET idiom in tests/test_impact_history.py).
func wsFromSchema(t *testing.T, presetYAML string) string {
	t.Helper()
	root := t.TempDir()
	khub := filepath.Join(root, ".khub")
	mkdirAll(t, khub)
	// The doc carries its layer blocks directly (one file, three blocks); the
	// base arrives embedded via LoadSchema, exactly as in a live workspace.
	writeFile(t, filepath.Join(khub, "ontology.yaml"), presetYAML)
	writeConfig(t, khub, "fixture", "0.0.0")
	return root
}

func writeConfig(t *testing.T, khub, preset string, version any) {
	t.Helper()
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
}

// seed writes one md entity file — the seed fixture, md branch.
func seed(t *testing.T, root, relpath string, fields ...kv) {
	t.Helper()
	meta := omap.New()
	for _, f := range fields {
		meta.Set(f.K, f.V)
	}
	text, err := canon.DumpWide(meta)
	if err != nil {
		t.Fatalf("dump %s: %v", relpath, err)
	}
	p := filepath.Join(root, filepath.FromSlash(relpath))
	mkdirAll(t, filepath.Dir(p))
	writeFile(t, p, "---\n"+text+"---\n")
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

// buildIdx is the tests' _index helper: scan without stray filtering, exactly
// like the Python unit tests.
func buildIdx(t *testing.T, root string) *index.Index {
	t.Helper()
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	return idx
}

func ptr[T any](v T) *T { return &v }
