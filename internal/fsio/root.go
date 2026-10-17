package fsio

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
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
		return nil, "", errs.New("unsafe_path", fmt.Sprintf("Path %s is outside workspace %s", filepath.ToSlash(path), filepath.ToSlash(root)))
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", err
	}
	part := ""
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		part = filepath.Join(part, name)
		// Windows reports a file in the path as missing; name it as darwin does.
		info, err := r.Lstat(part)
		err = FileInPath(err, base, filepath.Join(base, part))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			r.Close()
			return nil, "", err
		}
		if err := RefuseLink(info, path); err != nil {
			r.Close()
			return nil, "", err
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
	data, err := r.ReadFile(rel)
	return data, FileInPath(err, root, path)
}

// Stat stats path confined to root; OpenPath's refusals apply.
func Stat(root, path string) (fs.FileInfo, error) {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	info, err := r.Stat(rel)
	return info, FileInPath(err, root, path)
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
		return nil, FileInPath(err, r.Name(), path)
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		// Go uses fdopendir on Darwin and readdirent on Linux; keep CLI errors stable.
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = &os.PathError{Op: "readdir", Path: path, Err: pe.Err}
		}
		return nil, FileInPath(err, r.Name(), path)
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
		err = robust(func() error { return r.RemoveAll(rel) })
	} else {
		err = robust(func() error { return r.Remove(rel) })
	}
	if err != nil {
		return FileInPath(err, root, path)
	}
	if err := syncParent(r, rel); err != nil {
		return errs.New("write_durability_uncertain", fmt.Sprintf("Removed %s, but durability could not be confirmed (%s). Inspect the path before retrying", filepath.ToSlash(path), err))
	}
	return nil
}

// fileInPath is FileInPath's walk, with lstat passed in so every platform can
// test it. A not-exist err becomes NotADirectory, keeping its Op and Path,
// when the nearest existing ancestor of path is a regular file. path itself
// never counts: a file there may have been created after err was taken. The
// walk stops at root, or at a volume or share root, so it never stats outside
// the workspace.
func fileInPath(err error, root, path string, lstat func(string) (fs.FileInfo, error)) error {
	var pe *fs.PathError
	if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &pe) {
		return err
	}
	base, berr := filepath.Abs(root)
	p, perr := filepath.Abs(path)
	if berr != nil || perr != nil {
		return err
	}
	if rel, rerr := filepath.Rel(base, p); rerr != nil || !filepath.IsLocal(rel) {
		return err
	}
	for !samePath(p, base) {
		parent := filepath.Dir(p)
		if parent == p {
			return err
		}
		p = parent
		info, lerr := lstat(p)
		if lerr == nil {
			if info.Mode().IsRegular() {
				return &fs.PathError{Op: pe.Op, Path: pe.Path, Err: NotADirectory}
			}
			return err
		}
		if !errors.Is(lerr, fs.ErrNotExist) {
			return err
		}
	}
	return err
}

// samePath compares paths as the platform does: Windows ignores case.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// The Windows reparse-point attribute bit (winnt.h), named here so the test is
// a pure function every platform can exercise.
const fileAttributeReparsePoint = 0x400 // FILE_ATTRIBUTE_REPARSE_POINT

// reparseAttr reports whether a Windows entry with these attributes is a
// reparse point: a symlink, a junction or mount point, or any other entry a
// filter driver resolves, such as a OneDrive Files On-Demand placeholder.
// Workspace storage holds plain files and directories only, so every reparse
// point is refused, exactly as a symlink is.
func reparseAttr(attrs uint32) bool { return attrs&fileAttributeReparsePoint != 0 }

// IsLink reports whether info is an entry workspace storage refuses: a
// symlink, and on Windows any reparse point.
func IsLink(info fs.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0 || reparse(info)
}

// RefuseLink is the unsafe_path refusal for info at path, or nil when info is
// a plain file or directory. A symlink keeps its message on every platform; a
// Windows reparse point that is not a symlink gets its own, naming the way out.
func RefuseLink(info fs.FileInfo, path string) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return symlinkRefusal(path)
	}
	if reparse(info) {
		return errs.New("unsafe_path", fmt.Sprintf("Reparse points are not allowed in workspace storage: %s. A junction or a cloud placeholder (OneDrive Files On-Demand) is one; keep the workspace outside a synced folder", filepath.ToSlash(path)))
	}
	return nil
}

func symlinkRefusal(path string) error {
	return errs.New("unsafe_path", fmt.Sprintf("Symlinks are not allowed in workspace storage: %s", filepath.ToSlash(path)))
}

// Hide implementation-only temporary names and root-relative syscall paths.
func pathError(err error, path string) error {
	var e *fs.PathError
	if errors.As(err, &e) {
		return &fs.PathError{Op: e.Op, Path: path, Err: e.Err}
	}
	return err
}

// oneDriveVars are the variables OneDrive sets to the folders it syncs.
var oneDriveVars = []string{"OneDrive", "OneDriveConsumer", "OneDriveCommercial"}

// SyncedFolder reports the OneDrive folder root sits in, read through getenv.
// Paths compare as Windows compares them: either separator, any case, and at
// a component boundary, so C:\OneDriveX is not inside C:\OneDrive. It is pure
// so every platform can test it; only the Windows build calls it.
func SyncedFolder(root string, getenv func(string) string) (string, bool) {
	r := windowsClean(root)
	for _, name := range oneDriveVars {
		dir := getenv(name)
		d := windowsClean(dir)
		if d == "" {
			continue
		}
		if strings.EqualFold(r, d) || len(r) > len(d) && r[len(d)] == '/' && strings.EqualFold(r[:len(d)], d) {
			return dir, true
		}
	}
	return "", false
}

// windowsClean is a Windows path in slash form, cleaned, with no trailing
// slash; "" stays "".
func windowsClean(p string) string {
	if p == "" {
		return ""
	}
	return strings.TrimSuffix(pathpkg.Clean(strings.ReplaceAll(p, `\`, "/")), "/")
}

// SkipDirs are vendored or foreign trees khub never scans and never copies:
// integrity's misplaced-file walk and the upgrade candidate copy share it.
var SkipDirs = map[string]bool{
	".git": true, ".kb": true,
	"node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
}
