package fsio

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// syncParent is a no-op on Windows. FlushFileBuffers needs a handle opened for
// writing, and a directory opened through os is read-only, so a directory
// Sync fails with ERROR_ACCESS_DENIED and every write would report
// write_durability_uncertain. NTFS journals metadata, so a completed rename or
// create survives a crash without a directory flush; the file's own bytes are
// still flushed by f.Sync before publication.
func syncParent(*os.Root, string) error { return nil }

// robust retries op while another process holds the target open: Windows
// refuses a rename over, or a delete of, a file open without FILE_SHARE_DELETE
// until that handle closes.
func robust(op func() error) error {
	return retry(op, sharingViolation, time.Sleep, retryBudget)
}

// reparse reports any reparse point. Since Go 1.23 os gives a junction and a
// cloud placeholder ModeIrregular rather than ModeSymlink, so the attribute is
// what tells.
func reparse(info fs.FileInfo) bool {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && reparseAttr(d.FileAttributes)
}

// RefuseLinkEntry is RefuseLink for a directory entry. The entry's type misses
// a junction or a placeholder, so its attributes are read too.
func RefuseLinkEntry(d fs.DirEntry, path string) error {
	if d.Type()&os.ModeSymlink != 0 {
		return symlinkRefusal(path)
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	return RefuseLink(info, path)
}

// NotADirectory is the errno for a regular file opened as a directory:
// ERROR_DIRECTORY, which the CLI prints as ENOTDIR (see readdirErrno for why
// syscall.ENOTDIR is the wrong code here).
const NotADirectory = windows.ERROR_DIRECTORY

// readdirErrno names a regular file read as a directory with the Win32 code
// that means it. Go's Windows readdir reports that case as
// &PathError{Op: "readdir", Err: syscall.ENOTDIR} (os/dir_windows.go), but
// syscall.ENOTDIR is ERROR_PATH_NOT_FOUND on Windows — the code a missing
// path raises too, which the CLI prints as ENOENT. ERROR_DIRECTORY ("The
// directory name is invalid") is the code the CLI prints as ENOTDIR, so a file
// sitting where a directory belongs reads as internal_path_error, as it does
// on darwin and Linux. Any other readdir failure carries Go's op
// "GetFileInformationByHandleEx" and its own code, and passes through.
func readdirErrno(pe *os.PathError) error {
	if pe.Op == "readdir" && errors.Is(pe.Err, syscall.ENOTDIR) {
		return windows.ERROR_DIRECTORY
	}
	return pe.Err
}
