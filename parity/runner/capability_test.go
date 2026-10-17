package main

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"testing"
)

func TestUnknownRequirementIsAnError(t *testing.T) {
	if _, err := unmetRequirement([]string{"no-such-capability"}); err == nil {
		t.Fatal("an unknown requirement must fail the case, not pass silently")
	}
}

func TestNoRequirementsIsRunnable(t *testing.T) {
	why, err := unmetRequirement(nil)
	if why != "" || err != nil {
		t.Fatalf("why=%q err=%v", why, err)
	}
}

// Only a create that succeeds under the denial is a skip. A permission
// refusal means the denial holds; anything else is the probe breaking.
func TestProbeOutcome(t *testing.T) {
	for _, tc := range []struct {
		name     string
		create   error
		wantSkip bool
		wantErr  bool
	}{
		{"create succeeded", nil, true, false},
		{"permission denied", &fs.PathError{Op: "openat", Path: "probe", Err: syscall.EACCES}, false, false},
		{"bare ErrPermission", os.ErrPermission, false, false},
		{"exists", &fs.PathError{Op: "openat", Path: "probe", Err: fs.ErrExist}, false, true},
		{"other", errors.New("disk on fire"), false, true},
	} {
		skip, err := probeOutcome(tc.create)
		if (skip != "") != tc.wantSkip || (err != nil) != tc.wantErr {
			t.Errorf("%s: skip=%q err=%v", tc.name, skip, err)
		}
	}
}

func TestSkipErrorIsMatchedThroughAWrap(t *testing.T) {
	var s *skipError
	if !errors.As(errors.Join(&skipError{reason: "x"}), &s) || s.reason != "x" {
		t.Fatal("main must recognise a skip through errors.As")
	}
}
