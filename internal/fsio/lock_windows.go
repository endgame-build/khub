package fsio

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockRange covers the whole file and beyond: Windows locks byte ranges, and a
// range past EOF is legal, so the lock holds whatever the file's length.
const lockRange = ^uint32(0)

// lockFile blocks until f holds the exclusive lock. LockFileEx is the Windows
// counterpart of flock(LOCK_EX): held per handle, and released by the system
// when the handle closes or the process dies, so a crashed writer cannot
// strand the workspace. That release may lag, so Locked unlocks explicitly
// before it closes. Unlike flock the lock is mandatory: no other handle may
// read or write the locked bytes. The lock file carries no data, so nothing
// reads them. f is opened synchronously, so a zero Overlapped means offset 0
// and the call waits for the lock.
func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, lockRange, lockRange, new(windows.Overlapped))
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, lockRange, lockRange, new(windows.Overlapped))
}
