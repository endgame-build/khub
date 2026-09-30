package introspect

import (
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

func m(kv ...any) *omap.Map {
	out := omap.New()
	for i := 0; i < len(kv); i += 2 {
		out.Set(kv[i].(string), kv[i+1])
	}
	return out
}

func TestDiffSnapshot(t *testing.T) {
	old := m("types", m(
		"a", m("attributes", m(
			"x", m("type", "text", "enum", []any{"p", "q"}, "required", false, "pattern", nil),
			"y", m("type", "text"),
		), "relations", m(
			"r", m("to", []any{"b"}),
		)),
		"gone", m("attributes", m()),
	))
	cur := m("types", m(
		"a", m("attributes", m(
			"x", m("type", "text", "enum", []any{"p"}, "required", true, "pattern", "^p$"),
			"z", m("type", "date"),
		), "relations", m(
			"r", m("to", []any{"b", "c"}),
		)),
		"new", m("attributes", m()),
	))
	got := DiffSnapshot(old, cur)
	want := []struct{ op, path string }{
		{"changed", "types.a.attributes.x.enum"},
		{"changed", "types.a.attributes.x.required"},
		{"changed", "types.a.attributes.x.pattern"},
		{"added", "types.a.attributes.z"},
		{"removed", "types.a.attributes.y"},
		{"changed", "types.a.relations.r.to"},
		{"added", "types.new"},
		{"removed", "types.gone"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d changes, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		c := got[i].(*omap.Map)
		op, _ := c.Get("op")
		path, _ := c.Get("path")
		if op != w.op || path != w.path {
			t.Errorf("change %d = %v %v, want %s %s", i, op, path, w.op, w.path)
		}
	}
}

func TestDiffSnapshotIgnoresKeyOrder(t *testing.T) {
	old := m("types", m("a", m("required", true, "orphan", false), "b", m()))
	cur := m("types", m("b", m(), "a", m("orphan", false, "required", true)))
	if got := DiffSnapshot(old, cur); len(got) != 0 {
		t.Fatalf("reorder reported as change: %v", got)
	}
}
