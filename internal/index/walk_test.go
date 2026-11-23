package index

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/endgame-build/khub/internal/schema"
)

// WalkTree reaches every regular file outside hidden directories, vendored
// trees, .khub/ignore paths and linked directories.
func TestWalkTreeBoundaries(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"a.md", ".hidden.md", "docs/b.md", "docs/c.json",
		".khub/x.md", ".git/y.md", "node_modules/z.md", "examples/e.md", "outside/o.md",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "linked")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	ignore, err := schema.ParseIgnore("examples/\noutside/\n")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := WalkTree(root, ignore, func(path string) error {
		rel, _ := filepath.Rel(root, path)
		got = append(got, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{".hidden.md", "a.md", "docs/b.md", "docs/c.json"}
	if !slices.Equal(got, want) {
		t.Fatalf("walked %v, want %v", got, want)
	}
}
