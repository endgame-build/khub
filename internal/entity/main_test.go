package entity

import (
	"os"
	"testing"
	"time"
)

// TestMain pins the inputs of a `generated` stamp, as the parity runner does
// (KHUB_ACTOR, KHUB_PARITY_NOW, TZ=UTC), so byte assertions hold under any
// shell and zone (Claude Code sets CLAUDECODE).
func TestMain(m *testing.M) {
	os.Setenv("KHUB_ACTOR", "khub")
	os.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	time.Local = time.UTC
	os.Exit(m.Run())
}

// stamped is the `generated` block a write under TestMain's pins appends, at
// the mapping's indent.
func stamped(indent string) string {
	return indent + "generated:\n" + indent + "  by: khub\n" + indent + "  at: 2026-01-15T00:00:00Z\n"
}
