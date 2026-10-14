package main

import (
	"errors"
	"os"
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

// The probe agrees with what this account can actually do: an ordinary user
// is stopped by the denial, root is not. Either way the answer is consistent
// with a direct attempt, and a refusal comes with a reason.
func TestWriteDenialProbeMatchesTheAccount(t *testing.T) {
	ok, why := probeWriteDenial()
	if !ok && why == "" {
		t.Fatal("a skipped capability must say why")
	}
	if ok && why != "" {
		t.Fatalf("a held capability carries no reason, got %q", why)
	}
	if os.Getuid() == 0 && ok {
		t.Fatal("root ignores mode bits, yet the probe says the denial holds")
	}
}

func TestSkipErrorIsMatchedThroughAWrap(t *testing.T) {
	var s *skipError
	if !errors.As(errors.Join(&skipError{reason: "x"}), &s) || s.reason != "x" {
		t.Fatal("main must recognise a skip through errors.As")
	}
}
