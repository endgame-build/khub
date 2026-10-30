// The migrate lock test lives in an external test package for the reason
// golden_test.go does: layout imports workspace, so from inside the package
// the import would cycle.
package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/layout"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/workspace"
)

// A migration publishes under the workspace lock and runs its tails inside
// it: the hook sees the moved files and a held lock, and the lock is released
// once Migrate returns.
func TestMigrateRunsTailsUnderTheWorkspaceLock(t *testing.T) {
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	root := filepath.Join(t.TempDir(), "ws")
	if _, err := workspace.Init("build-hub", root, workspace.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	fields := omap.New()
	fields.Set("title", "Api")
	if _, err := entity.Create(root, "repo", entity.CreateOpts{Fields: fields, UseTemplate: true}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err := layout.Migrate(root, "repo", layout.Target{Layout: "collection", Format: "jsonl"}, layout.Options{
		Execute: true,
		Tails: func(got string) {
			calls++
			if got != root {
				t.Fatalf("hook root = %q, want %q", got, root)
			}
			if !workspace.LockHeld(t, root) {
				t.Fatal("tails ran without the workspace lock")
			}
			if _, err := os.Stat(filepath.Join(root, "knowledge", "repos.jsonl")); err != nil {
				t.Fatalf("tails ran before the collection was published: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("tails ran %d times, want once", calls)
	}
	if workspace.LockHeld(t, root) {
		t.Fatal("lock still held after Migrate returned")
	}
	// A dry run holds no lock and runs no tails.
	if _, err := layout.Migrate(root, "repo", layout.Target{Layout: "file", Format: "md"}, layout.Options{Tails: func(string) {
		t.Fatal("Tails ran on a dry run")
	}}); err != nil {
		t.Fatal(err)
	}
}
