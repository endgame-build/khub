package omap

import (
	"reflect"
	"testing"
)

// Key order is contract: Set appends new keys, keeps the position of existing
// ones, and Delete closes the gap without disturbing the rest.
func TestOrderIsInsertionThenStable(t *testing.T) {
	m := New()
	m.Set("b", 1)
	m.Set("a", 2)
	m.Set("c", 3)
	m.Set("a", 20) // existing key: value moves, position does not
	if got, want := m.Keys(), []string{"b", "a", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if v, ok := m.Get("a"); !ok || v != 20 {
		t.Fatalf("Get(a) = %v, %v", v, ok)
	}
	if m.Len() != 3 {
		t.Fatalf("Len = %d", m.Len())
	}

	m.Delete("a")
	m.Delete("missing") // no-op, never a panic
	if got, want := m.Keys(), []string{"b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys after Delete = %v, want %v", got, want)
	}
	if _, ok := m.Get("a"); ok || m.Len() != 2 {
		t.Fatalf("a still present after Delete")
	}

	m.Set("a", 3) // re-added keys go to the end, not back to their old slot
	if got, want := m.Keys(), []string{"b", "c", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys after re-add = %v, want %v", got, want)
	}
}

// Keys aliases the backing slice (dependencies.md names this as a known edge):
// a caller that needs a private copy must take one. Pinned so a change to
// either side is a deliberate one.
func TestKeysAliasesTheBackingSlice(t *testing.T) {
	m := New()
	m.Set("x", nil)
	m.Set("y", nil)
	keys := m.Keys()
	keys[0] = "mutated"
	if got := m.Keys()[0]; got != "mutated" {
		t.Fatalf("Keys() returned a copy (%q); the contract is an alias", got)
	}
}

func TestZeroValueGetOnEmptyMap(t *testing.T) {
	m := New()
	if v, ok := m.Get("nothing"); ok || v != nil {
		t.Fatalf("Get on empty = %v, %v", v, ok)
	}
	if len(m.Keys()) != 0 || m.Len() != 0 {
		t.Fatalf("empty map has keys")
	}
}
