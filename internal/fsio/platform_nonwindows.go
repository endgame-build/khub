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

// NotADirectory is the error for a regular file where a directory belongs.
var NotADirectory error = syscall.ENOTDIR

// FileInPath is the identity here: the kernel already says ENOTDIR for a
// regular file where a directory belongs, so no stat is paid.
func FileInPath(err error, _, _ string) error { return err }

// RefuseSyncedFolder is Windows-only: OneDrive placeholders are its concern.
func RefuseSyncedFolder(string) error { return nil }
