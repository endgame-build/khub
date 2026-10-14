//go:build !windows

package fsio

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// syncParent makes a new, renamed or removed directory entry durable.
func syncParent(r *os.Root, rel string) error {
	dir, err := r.Open(filepath.Dir(rel))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// robust runs op once: only Windows retries sharing violations.
func robust(op func() error) error { return op() }

// reparse is Windows-only: here a link already reports ModeSymlink.
func reparse(fs.FileInfo) bool { return false }

// RefuseLinkEntry is RefuseLink for a directory entry. Here the entry's type
// alone decides, so no extra stat is paid per entry.
func RefuseLinkEntry(d fs.DirEntry, path string) error {
	if d.Type()&os.ModeSymlink != 0 {
		return symlinkRefusal(path)
	}
	return nil
}

// readdirErrno is the errno a failed directory read reports: here the
// kernel's own, ENOTDIR for a regular file read as a directory.
func readdirErrno(pe *os.PathError) error { return pe.Err }

// NotADirectory is the errno for a regular file opened as a directory.
const NotADirectory = syscall.ENOTDIR
