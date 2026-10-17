package fsio

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/endgame-build/khub/internal/errs"
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

// notADirectory is NotADirectory on Windows. It prints darwin's text and
// unwraps to ERROR_DIRECTORY, the Win32 code the CLI prints as ENOTDIR.
type notADirectory struct{}

func (notADirectory) Error() string { return "not a directory" }
func (notADirectory) Unwrap() error { return windows.ERROR_DIRECTORY }

// NotADirectory is the error for a regular file where a directory belongs.
var NotADirectory error = notADirectory{}

// FileInPath names a regular file where a directory belongs. Windows reports
// that case as ERROR_PATH_NOT_FOUND, which reads as missing, so the nearest
// existing ancestor inside root decides (see fileInPath).
func FileInPath(err error, root, path string) error {
	return fileInPath(err, root, path, os.Lstat)
}

// RefuseSyncedFolder refuses a workspace inside a OneDrive folder up front,
// before a placeholder turns up mid-write as a reparse point.
func RefuseSyncedFolder(root string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if dir, ok := SyncedFolder(abs, os.Getenv); ok {
		return errs.New("unsafe_path", fmt.Sprintf("Synced folders are not allowed for workspace storage: %s is inside %s. OneDrive makes its files cloud placeholders (OneDrive Files On-Demand), which are reparse points; keep the workspace outside a synced folder", filepath.ToSlash(abs), filepath.ToSlash(dir)))
	}
	return nil
}
