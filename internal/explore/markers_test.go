package explore

import (
	"testing"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/query"
)

// Every marker equals what `khub query` and `khub check` report for the same
// entity: one definition each, read through the functions those verbs call.
func TestMarkersAgreeWithQueryAndCheck(t *testing.T) {
	root := buildHubWS(t)
	snap, err := load(root, now)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := query.Query(root, query.Filters{}, now)
	if err != nil {
		t.Fatal(err)
	}
	report, err := integrity.Check(root, false, now)
	if err != nil {
		t.Fatal(err)
	}
	incomplete := map[string]bool{}
	for _, i := range report.Incomplete {
		incomplete[i.ID()] = true
	}
	if len(matches) != len(snap.idx.Order) {
		t.Fatalf("query sees %d entities, the snapshot %d", len(matches), len(snap.idx.Order))
	}
	for _, m := range matches {
		node := index.Node{Type: m.Type, Slug: m.Slug}
		got := snap.markers(node)
		if got.Draft != m.Draft || got.Stale != m.Stale || got.Orphan != m.Orphan || got.Trust != m.Trust {
			t.Errorf("%s: markers %+v, query %+v", node.ID(), got, m)
		}
		if got.Incomplete != incomplete[node.ID()] {
			t.Errorf("%s: incomplete %v, check %v", node.ID(), got.Incomplete, incomplete[node.ID()])
		}
		if snap.title(node) != m.Title {
			t.Errorf("%s: title %q, query %q", node.ID(), snap.title(node), m.Title)
		}
	}
}

// The fixture holds one of each marker, so the agreement above is not vacuous.
func TestMarkersFixtureCoversEveryFlag(t *testing.T) {
	snap, err := load(buildHubWS(t), now)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(Markers) bool{
		"capability/cap-ideas": func(m Markers) bool { return m.Draft && !m.Incomplete },
		"system/sys-old":       func(m Markers) bool { return m.Stale },
		"actor/act-lonely":     func(m Markers) bool { return m.Orphan },
		"use-case/uc-half":     func(m Markers) bool { return m.Incomplete && !m.Draft },
	}
	for id, ok := range cases {
		typ, slug := splitID(id)
		m := snap.markers(index.Node{Type: typ, Slug: slug})
		if !ok(m) {
			t.Errorf("%s markers = %+v", id, m)
		}
	}
}

func splitID(id string) (string, string) {
	for i := 0; i < len(id); i++ {
		if id[i] == '/' {
			return id[:i], id[i+1:]
		}
	}
	return "", id
}
