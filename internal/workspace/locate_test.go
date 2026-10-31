// Library-level assertions for locate.go, ported from the Python suite:
// the no-workspace resolution error (tests/test_status.py
// test_status_no_workspace), provenance values (tests/test_schema.py), and
// the stale_days tolerance chain (tests/test_status.py
// test_status_tolerates_null_and_string_config, exercised here at the
// library layer).
package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

// canonical resolves symlinks the way FindWorkspace resolves its start path
// (macOS t.TempDir lives under the /var -> /private/var symlink).
func canonical(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", p, err)
	}
	return resolved
}

func writeConfig(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".khub"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".khub", "config.yaml"), []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestFindWorkspaceAtRootAndAbove(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".khub"), 0o777); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o777); err != nil {
		t.Fatal(err)
	}
	want := canonical(t, root)

	for _, start := range []string{root, nested} {
		got, err := FindWorkspace(start)
		if err != nil {
			t.Fatalf("FindWorkspace(%s): %v", start, err)
		}
		if got != want {
			t.Errorf("FindWorkspace(%s) = %s, want %s", start, got, want)
		}
	}
}

func TestFindWorkspaceMissIsTheResolutionError(t *testing.T) {
	// TS-WS-003-03: no .khub above the start dir -> the shared no_workspace error.
	_, err := FindWorkspace(t.TempDir())
	var located *errs.Located
	if !errors.As(err, &located) {
		t.Fatalf("want *errs.Located, got %T (%v)", err, err)
	}
	if located.Code != "no_workspace" {
		t.Errorf("code = %s, want no_workspace", located.Code)
	}
	if located.Message != "No .khub workspace found. Run khub init <preset>" {
		t.Errorf("message = %q", located.Message)
	}
}

func TestProvenanceReadsPresetAndVersion(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "name: hq\npreset: firm-ops\nversion: \"0.1.0\"\ndefaults:\n  stale_days: 90\n")

	prov, err := Provenance(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := prov.Keys(); len(got) != 2 || got[0] != "preset" || got[1] != "version" {
		t.Errorf("key order = %v, want [preset version]", got)
	}
	if v, _ := prov.Get("preset"); v != "firm-ops" {
		t.Errorf("preset = %v", v)
	}
	if v, _ := prov.Get("version"); v != "0.1.0" {
		t.Errorf("version = %v", v)
	}
}

func TestProvenanceMissingKeysReadEmpty(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "name: hq\n")

	prov, err := Provenance(root)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := prov.Get("preset"); v != "" {
		t.Errorf("preset = %v, want \"\"", v)
	}
	if v, _ := prov.Get("version"); v != "" {
		t.Errorf("version = %v, want \"\"", v)
	}
}

// A missing config.yaml is the located config_missing refusal, for every
// reader of it.
func TestMissingConfigIsConfigMissing(t *testing.T) {
	root := t.TempDir()
	_, perr := Provenance(root)
	_, serr := StaleDays(root)
	_, xerr := PresetSource(root)
	for _, err := range []error{perr, serr, xerr} {
		var le *errs.Located
		if !errors.As(err, &le) || le.Code != "config_missing" {
			t.Fatalf("want config_missing, got %v", err)
		}
	}
}

func TestStaleDaysDefaultChain(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   int
	}{
		{"plain int", "name: hq\npreset: firm-ops\ndefaults:\n  stale_days: 45\n", 45},
		{"null defaults block", "name: hq\npreset: firm-ops\ndefaults:\n", DefaultStaleDays},
		{"quoted number", "name: hq\npreset: firm-ops\ndefaults:\n  stale_days: \"120\"\n", 120},
		{"null stale_days value", "name: hq\npreset: firm-ops\ndefaults:\n  stale_days:\n", DefaultStaleDays},
		{"no defaults key", "name: hq\npreset: firm-ops\n", DefaultStaleDays},
		{"empty config", "", DefaultStaleDays},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeConfig(t, root, tc.config)
			got, err := StaleDays(root)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("StaleDays = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestStaleDaysRejectsNonNumericString(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "defaults:\n  stale_days: soon\n")
	if _, err := StaleDays(root); err == nil {
		t.Fatal("want error for non-numeric stale_days, got nil")
	}
}
