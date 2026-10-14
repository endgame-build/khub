package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestFileURIOfADrivePath(t *testing.T) {
	got := fileURI(`C:\Work\x\viz.html`)
	if got != "file:///C:/Work/x/viz.html" {
		t.Errorf("fileURI = %q", got)
	}
	if rel := fileURI("viz.html"); !strings.HasPrefix(rel, "file:///") || !filepath.IsAbs(filepath.FromSlash(strings.TrimPrefix(rel, "file:///"))) {
		t.Errorf("fileURI of a relative path = %q", rel)
	}
}

// khub prints every path with '/', so a native path in an OS error reads as
// the darwin fixture's after the runner swaps its prefix.
func TestOSErrorPrintsSlashPaths(t *testing.T) {
	pe := &os.PathError{Op: "open", Path: `C:\ws\knowledge\requirements\req-t.md`, Err: windows.ERROR_ACCESS_DENIED}
	want := "PermissionError: [Errno 13] Permission denied: 'C:/ws/knowledge/requirements/req-t.md'"
	if got := osErrorMessage(pe); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// On Windows the os_error envelope prints the line a Mac prints.
func TestWindowsOSErrorReadsAsDarwin(t *testing.T) {
	for errno, want := range map[windows.Errno]string{
		windows.ERROR_PATH_NOT_FOUND:    "FileNotFoundError: [Errno 2] No such file or directory: 'x'",
		windows.ERROR_ACCESS_DENIED:     "PermissionError: [Errno 13] Permission denied: 'x'",
		windows.ERROR_SHARING_VIOLATION: "PermissionError: [Errno 13] Permission denied: 'x'",
		windows.ERROR_ALREADY_EXISTS:    "FileExistsError: [Errno 17] File exists: 'x'",
		windows.ERROR_DISK_FULL:         "OSError: [Errno 28] No space left on device: 'x'",
	} {
		pe := &os.PathError{Op: "open", Path: "x", Err: errno}
		if got := osErrorMessage(fmt.Errorf("wrapped: %w", pe)); got != want {
			t.Errorf("%v: got %q, want %q", errno, got, want)
		}
	}
	for _, errno := range []windows.Errno{windows.ERROR_DIRECTORY, windows.ERROR_FILENAME_EXCED_RANGE} {
		if !isPathConstructionBug(&os.PathError{Op: "open", Path: "x", Err: errno}) {
			t.Errorf("%v is not a path-construction bug", errno)
		}
	}
	// A file where a directory belongs: what fsio.ReadDirIn reports on Windows
	// reads exactly as the darwin fixture (cli-contract/internal-path-error).
	readdir := &os.PathError{Op: "readdir", Path: `C:\ws\knowledge\requirements`, Err: windows.ERROR_DIRECTORY}
	if !isPathConstructionBug(readdir) {
		t.Error("readdir ERROR_DIRECTORY is not a path-construction bug")
	}
	if got, want := pathBugMessage(readdir), "Cannot readdir 'C:/ws/knowledge/requirements': Not a directory."; !strings.HasPrefix(got, want) {
		t.Errorf("pathBugMessage = %q, want prefix %q", got, want)
	}
	for _, errno := range []windows.Errno{windows.ERROR_BROKEN_PIPE, windows.ERROR_NO_DATA} {
		if !isEPIPE(&os.PathError{Op: "write", Path: "/dev/stdout", Err: errno}) {
			t.Errorf("%v is not a broken pipe", errno)
		}
	}
}
