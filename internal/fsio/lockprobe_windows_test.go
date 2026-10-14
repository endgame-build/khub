package fsio

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLock asks for the exclusive lock on f without waiting, over the same
// range lockFile takes: a disjoint range would always be granted. LockFileEx
// locks are held per handle, so a fresh handle is refused with
// ERROR_LOCK_VIOLATION exactly when another one holds the lock, in this
// process or another. On success f holds the lock and the caller releases it.
func tryLock(f *os.File) (bool, error) {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, lockRange, lockRange, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func releaseLock(f *os.File) error { return unlockFile(f) }
