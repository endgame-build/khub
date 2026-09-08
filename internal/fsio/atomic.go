// Package fsio provides atomic publication and workspace-confined filesystem I/O.
package fsio

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/endgame-build/khub/internal/errs"
)

// AtomicWrite replaces a file outside a workspace (e.g. an explicit export).
func AtomicWrite(path string, data []byte) error {
	return AtomicWriteIn(filepath.Dir(path), path, data)
}
func WriteNew(path string, data []byte) error            { return WriteNewIn(filepath.Dir(path), path, data) }
func AtomicWriteIn(root, path string, data []byte) error { return publish(root, path, data, false) }
func WriteNewIn(root, path string, data []byte) error    { return publish(root, path, data, true) }

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
	defer r.Remove(tmp)
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
				err = r.Rename(tmp, rel)
			} else {
				err = lerr
			}
		}
		if err != nil {
			return err
		}
		if linked {
			if err := r.Remove(tmp); err != nil {
				return publishedError(path, err)
			}
		}
	} else if err = r.Rename(tmp, rel); err != nil {
		return err
	}
	if err := syncParent(r, rel); err != nil {
		return publishedError(path, err)
	}
	return nil
}

func publishedError(path string, err error) error {
	return errs.New("write_durability_uncertain", fmt.Sprintf("Published %s, but durability could not be confirmed (%s). Read the file before retrying", path, err))
}
