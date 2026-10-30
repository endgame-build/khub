// The dry-run halves of MkdirAll, publish and Remove: every refusal khub
// itself makes before the filesystem is touched, and no write.

package fsio

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"
)

// CheckMkdirAll is MkdirAll without the mkdir, failing with the error
// MkdirAll would return: OpenPath's refusals, then MkdirAll's own walk, whose
// Stat names a file standing where a middle directory must go. A file at the
// last component passes that walk and fails in os.Root.MkdirAll, which
// reports it as mkdirat EEXIST on the root-relative path (every platform:
// Windows maps the NT collision status to the same syscall.EEXIST).
func CheckMkdirAll(root, path string) error {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	parts := strings.Split(rel, string(filepath.Separator))
	part, prev := "", ""
	for i, name := range parts {
		prev, part = part, filepath.Join(part, name)
		info, err := r.Stat(part)
		if errors.Is(err, fs.ErrNotExist) {
			// MkdirAll creates the rest. Windows reports a path through a
			// file as missing (unix fails the Stat with ENOTDIR first), and
			// os.Root.MkdirAll then fails opening that file as a directory.
			if i > 0 {
				if above, serr := r.Stat(prev); serr == nil && !above.IsDir() {
					return &fs.PathError{Op: "openat", Path: prev, Err: syscall.ENOTDIR}
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
		if i == len(parts)-1 && !info.IsDir() {
			return &fs.PathError{Op: "mkdirat", Path: rel, Err: syscall.EEXIST}
		}
	}
	return nil
}

// CheckPublish is AtomicWriteIn (exclusive false) or WriteNewIn (exclusive
// true) up to the point publish creates its temporary: OpenPath's refusals,
// an existing file where a new one must go, and a non-regular file where one
// is replaced.
func CheckPublish(root, path string, exclusive bool) (err error) {
	defer func() { err = pathError(err, path) }()
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	info, err := r.Lstat(rel)
	switch {
	case err == nil && exclusive:
		return &fs.PathError{Op: "create", Path: path, Err: fs.ErrExist}
	case err == nil && !info.Mode().IsRegular():
		return &fs.PathError{Op: "write", Path: path, Err: fs.ErrInvalid}
	case err == nil, errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return err
}

// CheckRemove is Remove without the removal: OpenPath's refusals, and a path
// that is not there.
func CheckRemove(root, path string) error {
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := r.Lstat(rel); err != nil {
		return FileInPath(err, root, path)
	}
	return nil
}
