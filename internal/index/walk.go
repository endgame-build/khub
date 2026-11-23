package index

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/schema"
)

// WalkTree calls fn with the path of every regular file below root, in name
// order. It leaves out hidden directories (.khub, .git, .claude),
// fsio.SkipDirs, `.khub/ignore` paths and linked directories (a symlink, or a
// Windows junction or other reparse point). It is the one definition of the
// workspace tree that check's misplaced walk and status's OKF conformance
// share.
func WalkTree(root string, ignore *schema.IgnoreList, fn func(path string) error) error {
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := fsio.ReadDir(root, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if rel, err := filepath.Rel(root, path); err == nil && ignore.Ignored(rel, e.IsDir()) {
				continue
			}
			if e.Type().IsRegular() {
				if err := fn(path); err != nil {
					return err
				}
				continue
			}
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || fsio.SkipDirs[e.Name()] {
				continue
			}
			if fi, err := os.Lstat(path); err == nil && fsio.IsLink(fi) {
				continue
			}
			if err := walk(path); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}
