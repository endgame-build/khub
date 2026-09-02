// The golden-tree test lives in an external test package on purpose: it runs
// the same tail the CLI runs after Init — reindex.Reindex, which `khub init`
// calls through indexTail (internal/cli/init.go) — and reindex imports entity,
// which imports workspace. From inside package workspace that is an import
// cycle; from here it is not.
package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/reindex"
	"github.com/endgame-build/khub/internal/workspace"
)

func TestIndexNamePinnedToReindex(t *testing.T) {
	// workspace spells index.md itself because importing reindex would cycle
	// (reindex → entity → workspace); this is what keeps the two copies equal.
	if workspace.IndexName != reindex.IndexName {
		t.Fatalf("workspace.indexName = %q, reindex.IndexName = %q", workspace.IndexName, reindex.IndexName)
	}
}

func TestGoldenInitTrees(t *testing.T) {
	// The recorded manifests pin every byte `khub init` writes: the library's
	// scaffold plus the index.md the CLI's tail writes. Each case runs in a
	// directory named "ws" with the target given as "." — the recorder's shape.
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	for _, tc := range []struct {
		name    string
		preset  string
		fixture string
		opt     workspace.InitOptions
	}{
		{"build-lite", "build-lite", "init-build-lite-tree", workspace.InitOptions{}},
		{"firm-ops", "firm-ops", "init-firm-ops-tree", workspace.InitOptions{}},
		{"named", "build-lite", "init-named", workspace.InitOptions{Name: "Custom Name"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := filepath.Join(t.TempDir(), "ws")
			if err := os.MkdirAll(ws, 0o777); err != nil {
				t.Fatal(err)
			}
			workspace.InDir(t, ws, func() {
				res, err := workspace.Init(tc.preset, ".", tc.opt)
				if err != nil {
					t.Fatalf("Init(%s): %v", tc.preset, err)
				}
				if res.Path != "." {
					t.Errorf("path = %q", res.Path)
				}
				if res.Version != workspace.PresetVersion(tc.preset) {
					t.Errorf("version = %q", res.Version)
				}
				if _, err := reindex.Reindex(res.Path, false); err != nil {
					t.Fatalf("Reindex: %v", err)
				}
			})
			workspace.AssertManifest(t, ws, tc.fixture)
		})
	}
}
