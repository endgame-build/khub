package main

// capability.go: what a case may require of the machine it runs on.
//
// Some cases set up a state the operating system enforces for an ordinary
// user but not for every account. err-os-error denies writes on a directory
// (chmod 500; on Windows an icacls deny ACE) and expects khub's write to be
// refused. Root ignores the mode bits, and on Windows an administrator whose
// token holds the backup and restore privileges ignores the deny ACE:
// os.Root opens with FILE_OPEN_FOR_BACKUP_INTENT, which GitHub's Windows
// runner honours. On such an account the case cannot reproduce its own
// setup, which says nothing about khub, so the runner skips it, loudly,
// instead of failing it.

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// capWriteDenial is `requires: [write-denial]`: denying writes on a directory
// stops this process from creating a file in it.
const capWriteDenial = "write-denial"

// skipError is a case the machine cannot run; main reports it as SKIP.
type skipError struct{ reason string }

func (e *skipError) Error() string { return e.reason }

var writeDenial struct {
	once sync.Once
	ok   bool
	why  string
}

// unmetRequirement returns why this machine cannot run a case that requires
// caps, or "" when it can.
func unmetRequirement(caps []string) (string, error) {
	for _, c := range caps {
		switch c {
		case capWriteDenial:
			writeDenial.once.Do(func() { writeDenial.ok, writeDenial.why = probeWriteDenial() })
			if !writeDenial.ok {
				return writeDenial.why, nil
			}
		default:
			return "", fmt.Errorf("unknown requirement %q (known: %s)", c, capWriteDenial)
		}
	}
	return "", nil
}

// probeWriteDenial denies writes on a scratch directory exactly as the cases
// do, then creates a file through os.Root, as khub does. If the create
// succeeds, the deny does not bind this account.
func probeWriteDenial() (bool, string) {
	dir, err := os.MkdirTemp("", "parity-probe-*")
	if err != nil {
		return false, "cannot create a probe directory: " + err.Error()
	}
	defer os.RemoveAll(dir)
	undo, err := denyWrites(dir)
	if err != nil {
		return false, "cannot deny writes on a probe directory: " + err.Error()
	}
	defer undo()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false, "cannot open the probe directory: " + err.Error()
	}
	defer root.Close()
	f, err := root.OpenFile("probe", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		f.Close()
		_ = root.Remove("probe")
		return false, "this account ignores a write denial on a directory (" + denialBypass + "), so the case cannot set up the state it tests"
	}
	if errors.Is(err, os.ErrPermission) {
		return true, ""
	}
	return false, "the probe write failed for another reason: " + err.Error()
}
