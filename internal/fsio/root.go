package fsio

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
)

// OpenPath confines an operation to root and refuses existing symlink components.
// os.Root also prevents escape if another process swaps a component after Lstat.
// The workspace root itself may be reached through a symlink (e.g. macOS /tmp).
func OpenPath(root, path string) (*os.Root, string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return nil, "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	rel, err := filepath.Rel(base, absolute)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, "", errs.New("unsafe_path", fmt.Sprintf("Path %s is outside workspace %s", path, root))
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", err
	}
	part := ""
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		part = filepath.Join(part, name)
		info, err := r.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			r.Close()
			return nil, "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			r.Close()
			return nil, "", errs.New("unsafe_path", fmt.Sprintf("Symlinks are not allowed in workspace storage: %s", path))
		}
	}
	return r, rel, nil
}

// ReadFile reads path confined to root; OpenPath's refusals apply.
func ReadFile(root, path string) ([]byte, error) {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.ReadFile(rel)
}

// Stat stats path confined to root; OpenPath's refusals apply.
func Stat(root, path string) (fs.FileInfo, error) {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Stat(rel)
}

// ReadDir lists path confined to root; OpenPath's refusals apply.
func ReadDir(root, path string) ([]os.DirEntry, error) {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return ReadDirIn(r, rel, path)
}

// ReadDirIn is ReadDir over a Root the caller already holds; path is the
// workspace path the entries were asked for, used only to shape the error.
func ReadDirIn(r *os.Root, rel, path string) ([]os.DirEntry, error) {
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		// Go uses fdopendir on Darwin and readdirent on Linux; keep CLI errors stable.
		var pe *os.PathError
		if errors.As(err, &pe) {
			return nil, &os.PathError{Op: "readdir", Path: path, Err: pe.Err}
		}
		return nil, err
	}
	// os.File.ReadDir is directory order; callers rely on os.ReadDir's sort.
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

// MkdirAll syncs each new directory entry, including recovery-journal parents.
func MkdirAll(root, path string) error {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	var created []string
	part := ""
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		part = filepath.Join(part, name)
		if _, err := r.Stat(part); errors.Is(err, fs.ErrNotExist) {
			created = append(created, part)
		} else if err != nil {
			return err
		}
	}
	if err := r.MkdirAll(rel, 0o777); err != nil {
		return err
	}
	for i := len(created) - 1; i >= 0; i-- {
		if err := syncParent(r, created[i]); err != nil {
			return publishedError(path, err)
		}
	}
	return nil
}

// Remove deletes path confined to root, recursively when asked.
func Remove(root, path string, recursive bool) error {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	if recursive {
		err = r.RemoveAll(rel)
	} else {
		err = r.Remove(rel)
	}
	if err != nil {
		return err
	}
	if err := syncParent(r, rel); err != nil {
		return errs.New("write_durability_uncertain", fmt.Sprintf("Removed %s, but durability could not be confirmed (%s). Inspect the path before retrying", path, err))
	}
	return nil
}
func syncParent(r *os.Root, rel string) error {
	dir, err := r.Open(filepath.Dir(rel))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Hide implementation-only temporary names and root-relative syscall paths.
func pathError(err error, path string) error {
	var e *fs.PathError
	if errors.As(err, &e) {
		return &fs.PathError{Op: e.Op, Path: path, Err: e.Err}
	}
	return err
}

// SkipDirs are vendored or foreign trees khub never scans and never copies:
// integrity's misplaced-file walk and the upgrade candidate copy share it.
var SkipDirs = map[string]bool{
	".git": true, ".kb": true,
	"node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
}
