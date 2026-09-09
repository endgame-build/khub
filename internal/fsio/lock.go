// Workspace mutations serialize on a stable advisory lock file.

package fsio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// WorkspaceLockPath is stable: never delete lock files while a writer may run.
func WorkspaceLockPath(root string) string {
	return filepath.Join(root, ".khub", "generated", "locks", "workspace.lock")
}

// Locked serializes the entire read/validate/commit operation, not just publication.
func Locked[T any](root string, fn func() (T, error)) (result T, err error) {
	// ponytail: one workspace writer; split locks only if measured throughput requires it.
	lockPath := WorkspaceLockPath(root)
	var f *os.File
	// First writers race each other on creating the lock directory, and darwin
	// has been seen to report ENOENT for a component another thread created a
	// moment earlier while every directory is present. Re-create and retry a
	// bounded number of times, never sleep; anything but ENOENT returns as is.
	for attempt := 0; ; attempt++ {
		if err = MkdirAll(root, filepath.Dir(lockPath)); err != nil {
			return result, err
		}
		r, rel, oerr := OpenPath(root, lockPath)
		if oerr != nil {
			return result, oerr
		}
		f, err = r.OpenFile(rel, os.O_CREATE|os.O_RDWR, 0o644)
		r.Close() // the lock lives on f's descriptor; the Root only reached it
		if err == nil {
			break
		}
		if attempt == 2 || !errors.Is(err, fs.ErrNotExist) {
			return result, err
		}
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return result, err
	}
	defer func() {
		if e := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err == nil {
			err = e
		}
	}()
	return fn()
}
