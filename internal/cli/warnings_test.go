package cli

import (
	"fmt"
	"testing"

	"github.com/endgame-build/khub/internal/integrity"
)

func findingsN(n int) []integrity.Finding {
	out := make([]integrity.Finding, n)
	for i := range out {
		out[i] = integrity.Finding{ID: fmt.Sprintf("person/p%d", i), Rule: "incomplete", Message: "missing role"}
	}
	return out
}

// A long backlog prints warningCap lines and one count of the rest; at the
// cap exactly, no count line.
func TestWarningLinesCap(t *testing.T) {
	if got := warningLines(findingsN(warningCap)); len(got) != warningCap {
		t.Errorf("at the cap: %d lines, want %d", len(got), warningCap)
	}
	got := warningLines(findingsN(warningCap + 380))
	if len(got) != warningCap+1 {
		t.Fatalf("over the cap: %d lines, want %d", len(got), warningCap+1)
	}
	if want := "warning: … and 380 more; run khub check"; got[warningCap] != want {
		t.Errorf("count line = %q, want %q", got[warningCap], want)
	}
	if want := "warning: person/p0 › incomplete: missing role"; got[0] != want {
		t.Errorf("first line = %q, want %q", got[0], want)
	}
}
