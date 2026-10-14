//go:build unix

package fsio

import (
	"os"
	"syscall"
)

// lockFile blocks until f holds the exclusive advisory lock.
func lockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }

func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
