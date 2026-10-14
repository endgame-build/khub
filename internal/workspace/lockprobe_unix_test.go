//go:build unix

package workspace

import (
	"errors"
	"os"
	"syscall"
)

// tryLock asks for the exclusive lock on f without waiting. flock is held per
// open file description, so a fresh descriptor is refused with EWOULDBLOCK
// exactly when another one holds the lock, in this process or another. On
// success f holds the lock and the caller releases it.
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func releaseLock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
