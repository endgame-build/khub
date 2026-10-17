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
// instead of failing it. Only a create that succeeds under the denial is a
// skip; a probe that breaks in any other way fails the case.

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

// writeDenial probes once per run, on the first case that requires it.
var writeDenial = sync.OnceValues(probeWriteDenial)

// unmetRequirement returns why this machine cannot run a case that requires
// caps, or "" when it can. An error means the probe itself broke; main counts
// it as a failure, never as a skip.
func unmetRequirement(caps []string) (string, error) {
	for _, c := range caps {
		switch c {
		case capWriteDenial:
			if why, err := writeDenial(); why != "" || err != nil {
				return why, err
			}
		default:
			return "", fmt.Errorf("unknown requirement %q (known: %s)", c, capWriteDenial)
		}
	}
	return "", nil
}

// probeWriteDenial denies writes on a scratch directory exactly as the cases
// do, then creates a file through os.Root, as khub does.
func probeWriteDenial() (string, error) {
	dir, err := os.MkdirTemp("", "parity-probe-*")
	if err != nil {
		return "", fmt.Errorf("write-denial probe: create a scratch directory: %w", err)
	}
	defer os.RemoveAll(dir)
	undo, err := denyWrites(dir)
	if err != nil {
		return "", fmt.Errorf("write-denial probe: deny writes: %w", err)
	}
	defer undo()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("write-denial probe: open the scratch directory: %w", err)
	}
	defer root.Close()
	f, err := root.OpenFile("probe", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		f.Close()
		_ = root.Remove("probe")
	}
	return probeOutcome(err)
}

// probeOutcome reads the probe's create. Refused for permission: the denial
// holds. Succeeded: this account bypasses it, so the case is a skip. Any
// other failure is the probe breaking, which is an error.
func probeOutcome(createErr error) (skip string, err error) {
	switch {
	case createErr == nil:
		return "this account ignores a write denial on a directory (" + denialBypass + "), so the case cannot set up the state it tests", nil
	case errors.Is(createErr, os.ErrPermission):
		return "", nil
	default:
		return "", fmt.Errorf("write-denial probe: the create failed for a reason other than the denial: %w", createErr)
	}
}
