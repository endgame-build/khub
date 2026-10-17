package fsio

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/errs"
	"golang.org/x/sys/windows"
)

func TestWindowsConstantsMatchTheSystem(t *testing.T) {
	if errorAccessDenied != windows.ERROR_ACCESS_DENIED ||
		errorSharingViolation != windows.ERROR_SHARING_VIOLATION ||
		errorLockViolation != windows.ERROR_LOCK_VIOLATION {
		t.Fatal("sharing-violation errno constants drifted from x/sys/windows")
	}
	if fileAttributeReparsePoint != syscall.FILE_ATTRIBUTE_REPARSE_POINT {
		t.Fatal("file attribute constants drifted from syscall")
	}
}

// A junction needs no privilege, unlike a symlink, so it is the link an
// unprivileged Windows user can plant in a workspace.
func TestJunctionComponentIsRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v: %s", err, out)
	}
	err := AtomicWriteIn(root, filepath.Join(link, "file"), []byte("bad"))
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "unsafe_path" {
		t.Fatalf("err = %v, want unsafe_path", err)
	}
	// Go reports a junction as ModeSymlink only under GODEBUG=winsymlink=0,
	// so either refusal is the right one; what matters is that it refuses.
	at := filepath.ToSlash(filepath.Join(link, "file"))
	if !strings.HasPrefix(located.Message, "Reparse points are not allowed in workspace storage: "+at) &&
		located.Message != "Symlinks are not allowed in workspace storage: "+at {
		t.Errorf("message = %q, want a link refusal for %s", located.Message, at)
	}
	if _, err := os.Stat(filepath.Join(outside, "file")); err == nil {
		t.Fatal("write escaped through the junction")
	}
}

// os.Open shares read and write but not delete, so a reader holding the
// target blocks a replacing rename until it closes.
func TestAtomicWriteWaitsOutAnOpenReader(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data")
	if err := os.WriteFile(path, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		f.Close()
	}()
	if err := AtomicWriteIn(root, path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, path); got != "new" {
		t.Fatalf("content = %q", got)
	}
}

// A regular file read as a directory carries NotADirectory, which unwraps to
// ERROR_DIRECTORY, the code the CLI prints as ENOTDIR. Go's own report is
// syscall.ENOTDIR, which on Windows is ERROR_PATH_NOT_FOUND and would print as
// a missing file.
func TestReadDirOfAFileIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadDir(root, path)
	var pe *os.PathError
	if !errors.As(err, &pe) || pe.Op != "readdir" || !errors.Is(pe.Err, NotADirectory) || !errors.Is(err, windows.ERROR_DIRECTORY) {
		t.Fatalf("ReadDir error = %#v, want readdir NotADirectory", err)
	}
	if pe.Error() != "readdir "+path+": not a directory" {
		t.Errorf("message = %q", pe.Error())
	}
}

// Stat below a regular file names the file in the way, as darwin does.
func TestStatBelowAFileIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Stat(root, filepath.Join(root, "file", "child"))
	var pe *os.PathError
	if !errors.As(err, &pe) || pe.Op != "statat" || !errors.Is(pe.Err, NotADirectory) {
		t.Fatalf("Stat error = %#v, want statat NotADirectory", err)
	}
}
