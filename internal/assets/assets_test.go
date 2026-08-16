package assets

// During dual-ship a second test pinned these bytes against
// src/khub/assets/cytoscape.min.js, because `khub viz` inlined the library from
// both the Go binary and the Python wheel and the two copies could drift. The
// Python copy is gone, so that guard has nothing left to compare and was
// removed with it. The self-containment check below is the one that still has a
// job.

import (
	"strings"
	"testing"
)

// The self-containment guarantee: the library itself must not pull anything
// over the network (PRJ-005 / REQ-PRJ003-02).
func TestCytoscapeIsSubstantialAndOffline(t *testing.T) {
	if len(CytoscapeJS) < 200_000 {
		t.Fatalf("embedded library is only %d bytes", len(CytoscapeJS))
	}
	for _, tag := range []string{`src="http`, `src='http`, `href="http`, `href='http`} {
		if strings.Contains(CytoscapeJS, tag) {
			t.Errorf("the library references an external asset (%q)", tag)
		}
	}
}
