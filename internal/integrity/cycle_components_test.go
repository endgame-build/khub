package integrity

import (
	"reflect"
	"testing"
)

// a<->b plus a->a and b->b is one strongly connected component, so `check`
// reports one witness for it, not three cycles. The self-loops are not hidden
// for good: unlink a<->b and each becomes its own component with its own
// witness.
func TestCheckCycleComponentIncludesSelfReferencesOnce(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "clients/a.md", kv{"type", "client"}, kv{"name", "A"}, kv{"depends_on", []any{"a", "b"}})
	seed(t, root, "clients/b.md", kv{"type", "client"}, kv{"name", "B"}, kv{"depends_on", []any{"a", "b"}})
	report := mustCheck(t, root, false)
	want := [][]string{{"client/a"}}
	if !reflect.DeepEqual(report.Cycles, want) {
		t.Fatalf("got %v, want %v", report.Cycles, want)
	}
	if report.Passed() {
		t.Fatal("cyclic graph passed")
	}
}
