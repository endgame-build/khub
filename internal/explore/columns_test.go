package explore

import (
	"testing"

	"github.com/endgame-build/khub/internal/introspect"
)

func TestColumnsComponent(t *testing.T) {
	resolved, err := introspect.LoadSchema(initWS(t, "build-hub"))
	if err != nil {
		t.Fatal(err)
	}
	cols := Columns(resolved, "component")
	want := []string{
		// declared outgoing, in declaration order
		"out:system", "out:repo", "out:consumes",
		// base outgoing
		"out:related", "out:references", "out:depends_on",
		// incoming, by source type then predicate, in schema order
		"in:use-case.served_by", "in:requirement.realized_in", "in:adr.affects", "in:api.provider",
		// collapsed incoming base relations
		"in:*.related", "in:*.references", "in:*.depends_on",
	}
	if got := keys(cols); !eq(got, want) {
		t.Fatalf("component columns:\n got %v\nwant %v", got, want)
	}
	byKey := map[string]Column{}
	for _, c := range cols {
		byKey[c.Key] = c
	}
	if c := byKey["in:use-case.served_by"]; c.Label != "serves" || c.Direction != DirIn ||
		c.Predicate != "served_by" || !eq(c.PeerTypes, []string{"use-case"}) || c.Base {
		t.Errorf("served_by incoming column = %+v", c)
	}
	if c := byKey["out:consumes"]; c.Label != "consumes" || !eq(c.PeerTypes, []string{"api"}) {
		t.Errorf("consumes column = %+v", c)
	}
	if c := byKey["in:adr.affects"]; c.Label != "adr.affects" || c.Base {
		t.Errorf("an `any` relation from another type is its own column: %+v", c)
	}
	for _, k := range []string{"out:related", "out:references", "out:depends_on", "in:*.related"} {
		if !byKey[k].Base {
			t.Errorf("%s must be flagged base", k)
		}
	}
	if c := byKey["out:related"]; !eq(c.PeerTypes, []string{"any"}) {
		t.Errorf("an any-target relation peers with any: %+v", c)
	}
}

// A capability's incoming relations declare no inverse, so each is named by
// its source type and predicate, as `khub get --edges` names them.
func TestColumnsCapabilityIncomingWithoutInverse(t *testing.T) {
	resolved, err := introspect.LoadSchema(initWS(t, "build-hub"))
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, c := range Columns(resolved, "capability") {
		labels[c.Key] = c.Label
	}
	for key, want := range map[string]string{
		"in:use-case.capability":      "use-case.capability",
		"in:requirement.capabilities": "requirement.capabilities",
	} {
		if labels[key] != want {
			t.Errorf("%s label = %q, want %q", key, labels[key], want)
		}
	}
}

// A type relating to itself has both halves: adr supersedes adr is an outgoing
// column and, named by its declared inverse, an incoming one: a type relating to itself is counted both ways.
func TestColumnsSelfRelation(t *testing.T) {
	resolved, err := introspect.LoadSchema(initWS(t, "build-hub"))
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Column{}
	for _, c := range Columns(resolved, "adr") {
		byKey[c.Key] = c
	}
	if _, ok := byKey["out:supersedes"]; !ok {
		t.Fatal("missing out:supersedes")
	}
	in, ok := byKey["in:adr.supersedes"]
	if !ok || in.Label != "superseded" {
		t.Fatalf("in:adr.supersedes = %+v (present %v)", in, ok)
	}
	if _, ok := byKey["in:adr.affects"]; !ok {
		t.Fatal("adr's own `affects: any` points back at adr too")
	}
}

// A union relation lands on every type it names (firm-ops meeting.engagement).
func TestColumnsUnionTarget(t *testing.T) {
	resolved, err := introspect.LoadSchema(initWS(t, "firm-ops"))
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"opportunity", "project", "partnership"} {
		found := false
		for _, c := range Columns(resolved, typ) {
			if c.Key == "in:meeting.engagement" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has no in:meeting.engagement column", typ)
		}
	}
}

func TestColumnsUnknownType(t *testing.T) {
	resolved, err := introspect.LoadSchema(initWS(t, "build-hub"))
	if err != nil {
		t.Fatal(err)
	}
	if cols := Columns(resolved, "nope"); cols != nil {
		t.Fatalf("unknown type has no columns, got %v", keys(cols))
	}
}
