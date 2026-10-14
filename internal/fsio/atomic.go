// Package fsio provides atomic publication and workspace-confined filesystem I/O.
package fsio

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/endgame-build/khub/internal/errs"
)

// AtomicWrite replaces a file outside a workspace (e.g. an explicit export).
func AtomicWrite(path string, data []byte) error {
	return AtomicWriteIn(filepath.Dir(path), path, data)
}

// WriteNew creates a file outside a workspace and refuses to replace one.
func WriteNew(path string, data []byte) error { return WriteNewIn(filepath.Dir(path), path, data) }

// AtomicWriteIn replaces path, confined to root, through a unique sibling
// temporary: readers see the old bytes or the new, never a torn file.
func AtomicWriteIn(root, path string, data []byte) error { return publish(root, path, data, false) }

// WriteNewIn is AtomicWriteIn for a path that must not exist yet.
func WriteNewIn(root, path string, data []byte) error { return publish(root, path, data, true) }

// linkFile is the exclusive-publish primitive; tests swap it to simulate a
// filesystem without hard links.
var linkFile = func(r *os.Root, oldname, newname string) error { return r.Link(oldname, newname) }

func publish(root, path string, data []byte, exclusive bool) (err error) {
	defer func() { err = pathError(err, path) }()
	r, rel, err := OpenPath(root, path)
	if err != nil {
		return err
	}
	defer r.Close()
	mode := fs.FileMode(0o666)
	info, err := r.Lstat(rel)
	if err == nil {
		if exclusive {
			return &fs.PathError{Op: "create", Path: path, Err: fs.ErrExist}
		}
		if !info.Mode().IsRegular() {
			return &fs.PathError{Op: "write", Path: path, Err: fs.ErrInvalid}
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	existed := err == nil
	tmp := filepath.Join(filepath.Dir(rel), ".khub-write-"+rand.Text())
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	// Best-effort cleanup: after a successful rename there is nothing at tmp
	// to remove, and on the failure paths the error already returned wins.
	defer func() { _ = robust(func() error { return r.Remove(tmp) }) }()
	// Chmod the descriptor, never a path that could have been swapped.
	if existed {
		if err := f.Chmod(mode); err != nil {
			f.Close()
			return err
		}
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if exclusive {
		// A hard link publishes the completed file without replacing a winner.
		linked := true
		err = linkFile(r, tmp, rel)
		if errors.Is(err, fs.ErrExist) {
			// Never leak the temporary name through a LinkError.
			err = &fs.PathError{Op: "create", Path: path, Err: fs.ErrExist}
		} else if err != nil {
			// No hard links (exFAT, some SMB and FUSE mounts): re-check the
			// target, then rename. The window is the Lstat-to-rename gap only.
			linked = false
			if _, lerr := r.Lstat(rel); lerr == nil {
				err = &fs.PathError{Op: "create", Path: path, Err: fs.ErrExist}
			} else if errors.Is(lerr, fs.ErrNotExist) {
				err = robust(func() error { return r.Rename(tmp, rel) })
			} else {
				err = lerr
			}
		}
		if err != nil {
			return err
		}
		if linked {
			if err := robust(func() error { return r.Remove(tmp) }); err != nil {
				return publishedError(path, err)
			}
		}
	} else if err = robust(func() error { return r.Rename(tmp, rel) }); err != nil {
		return err
	}
	if err := syncParent(r, rel); err != nil {
		return publishedError(path, err)
	}
	return nil
}

func publishedError(path string, err error) error {
	return errs.New("write_durability_uncertain", fmt.Sprintf("Published %s, but durability could not be confirmed (%s). Read the file before retrying", filepath.ToSlash(path), err))
}

// Windows error codes (winerror.h) raised when another process holds a file
// open without FILE_SHARE_DELETE: a concurrent khub read (os.Open does not
// share delete), an antivirus scan, the search indexer. Each clears once that
// handle closes. Named here so the classifier is a pure function every
// platform can exercise; only the Windows build wires it in.
const (
	errorAccessDenied     syscall.Errno = 5  // ERROR_ACCESS_DENIED
	errorSharingViolation syscall.Errno = 32 // ERROR_SHARING_VIOLATION
	errorLockViolation    syscall.Errno = 33 // ERROR_LOCK_VIOLATION
)

// sharingViolation reports whether err is a Windows error that waiting can
// clear, after cmd/internal/robustio. ERROR_ACCESS_DENIED is also a genuine
// permission refusal; that costs one retry budget before the same error
// surfaces.
func sharingViolation(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case errorAccessDenied, errorSharingViolation, errorLockViolation:
		return true
	}
	return false
}

// retryBudget caps the total sleep one call may spend waiting out a transient
// error, as cmd/internal/robustio does.
const retryBudget = 2 * time.Second

// retry runs op until it succeeds, fails with an error transient rejects, or
// the sleep budget is spent. It returns op's last error untouched, so a caller
// sees the same error kind it would have without retries.
func retry(op func() error, transient func(error) bool, sleep func(time.Duration), budget time.Duration) error {
	delay := time.Millisecond
	var slept time.Duration
	for {
		err := op()
		if err == nil || !transient(err) || slept >= budget {
			return err
		}
		d := min(delay, budget-slept)
		sleep(d)
		slept += d
		delay *= 2
	}
}
