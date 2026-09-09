package entity

// Test scaffolding standing in for tests/conftest.py's fresh_ws / ws_for / seed
// fixtures. The workspace is flattened here the way core/workspace.py
// init_workspace flattens it (core base + preset entities, wide dump, template
// copy) so these tests do not depend on the init port landing first.

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/values"
)

// kv builds an ordered map from alternating key/value pairs.
func kv(pairs ...any) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i].(string), pairs[i+1])
	}
	return m
}

// fields builds the --field map from alternating key/value strings.
func fields(pairs ...string) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i], pairs[i+1])
	}
	return m
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test working directory")
		}
		dir = parent
	}
}

func presetsDir(t *testing.T) string {
	return filepath.Join(repoRoot(t), "presets")
}

// newWS scaffolds a workspace from a shipped preset.
func newWS(t *testing.T, preset string) string {
	t.Helper()
	return newWSFrom(t, preset, presetsDir(t))
}

// newWSFrom scaffolds a workspace from any preset directory (init's
// --preset-source): the layer files are copied verbatim, and the base arrives
// embedded via LoadSchema, exactly as in a live workspace. A fixture preset's
// ontology.yaml may carry all three layer blocks in one document — the merge
// dispatches on top-level key, not filename.
func newWSFrom(t *testing.T, preset, dir string) string {
	t.Helper()
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".khub"))

	version := "0.0.0"
	ont := loadYAML(t, filepath.Join(dir, preset, "ontology.yaml"))
	if v, has := ont.Get("version"); has && v != nil {
		version = scalarText(v)
	}
	for _, layer := range []string{"ontology.yaml", "policy.yaml", "storage.yaml"} {
		data, readErr := os.ReadFile(filepath.Join(dir, preset, layer))
		if readErr != nil {
			continue // policy/storage are optional layers
		}
		writeFile(t, filepath.Join(root, ".khub", layer), string(data))
	}

	defaults := omap.New()
	defaults.Set("stale_days", int64(90))
	cfgText, err := canon.DumpWide(kv(
		"name", filepath.Base(root), "preset", preset, "version", version,
		"source", nil, "defaults", defaults))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".khub", "config.yaml"), cfgText)

	tplSrc := filepath.Join(dir, preset, "templates")
	if entries, derr := os.ReadDir(tplSrc); derr == nil {
		mkdirAll(t, filepath.Join(root, ".khub", "templates"))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			raw, rerr := os.ReadFile(filepath.Join(tplSrc, e.Name()))
			if rerr != nil {
				t.Fatal(rerr)
			}
			writeFile(t, filepath.Join(root, ".khub", "templates", e.Name()), string(raw))
		}
	}
	return root
}

// writePreset lays down a throwaway preset directory (init's --preset-source
// shape) and returns the directory holding it. schemaText is one layered
// document (ontology/policy/storage blocks in any combination) — the merge
// dispatches on top-level key, so one file carrying three blocks reads the
// same as three files.
func writePreset(t *testing.T, name, schemaText string, templates map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, name))
	writeFile(t, filepath.Join(dir, name, "ontology.yaml"), schemaText)
	for file, text := range templates {
		mkdirAll(t, filepath.Join(dir, name, "templates"))
		writeFile(t, filepath.Join(dir, name, "templates", file), text)
	}
	return dir
}

func loadYAML(t *testing.T, path string) *omap.Map {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.LoadDoc(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(*omap.Map)
	if !ok {
		t.Fatalf("%s is not a mapping", path)
	}
	return m
}

func scalarText(v any) string { return values.Str(v) }

// seed writes an entity file under a workspace root, dispatched on the suffix —
// the conftest.py `seed` fixture.
func seed(t *testing.T, root, relpath string, meta *omap.Map) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(relpath))
	mkdirAll(t, filepath.Dir(p))
	switch {
	case strings.HasSuffix(relpath, ".json"):
		text, err := canon.EncodeDisk(meta)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, text)
	case strings.HasSuffix(relpath, ".yaml"):
		text, err := canon.DumpWide(meta)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, text)
	default:
		text, err := canon.DumpWide(meta)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, "---\n"+text+"---\n")
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
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

// readMeta reads a written entity's frontmatter through the same parser the
// scan uses.
func readMeta(t *testing.T, path string) *omap.Map {
	t.Helper()
	meta, _, err := canon.Parse(readFile(t, path), canon.FmtOf(path))
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func metaValue(t *testing.T, path, key string) any {
	t.Helper()
	v, _ := readMeta(t, path).Get(key)
	return v
}

func hasKey(t *testing.T, path, key string) bool {
	t.Helper()
	_, ok := readMeta(t, path).Get(key)
	return ok
}

// mdFiles lists every entity markdown file, workspace-relative and sorted —
// the `_md_files` helper the Python write-nothing assertions use.
func mdFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if info.IsDir() {
			if info.Name() == ".khub" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// requireCode asserts the error is a located error carrying code.
func requireCode(t *testing.T, err error, code string) *errs.Located {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a %s error, got nil", code)
	}
	var located *errs.Located
	if !errors.As(err, &located) {
		t.Fatalf("expected a located error, got %T: %v", err, err)
	}
	if located.Code != code {
		t.Fatalf("expected code %q, got %q (%s)", code, located.Code, located.Message)
	}
	return located
}

func requireMessageContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected %q in %q", substr, err.Error())
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
