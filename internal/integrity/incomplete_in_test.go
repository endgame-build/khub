package integrity

import (
	"reflect"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
)

// IncompleteIn over a caller's scan is Check's incomplete bucket exactly —
// same rows, same order, waivers applied — so a reader that needs only
// completeness can skip the second walk of the tree.
func TestIncompleteInMatchesCheck(t *testing.T) {
	root := gapsWS(t)
	for _, policy := range []string{"", "policy:\n  project:\n    waive:\n      initech-pov:\n        incomplete: Being re-imported.\n"} {
		if policy != "" {
			writeRaw(t, root, ".khub/policy.yaml", policy)
		}
		report, err := Check(root, false, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := introspect.LoadSchema(root)
		if err != nil {
			t.Fatal(err)
		}
		scanned, err := index.BuildWithBodies(root, resolved)
		if err != nil {
			t.Fatal(err)
		}
		got := IncompleteIn(resolved, index.Filter(scanned, index.StrayNodes(scanned)))
		if !reflect.DeepEqual(got, report.Incomplete) {
			t.Fatalf("policy %q: IncompleteIn = %+v, check = %+v", policy, got, report.Incomplete)
		}
	}
}
