package trust

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

func noteType(t *testing.T) *schema.ResolvedType {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ontology.yaml")
	doc := "ontology:\n  entities:\n    note:\n      attributes:\n        title: {}\n        tags: { type: list }\n" +
		"        verified: { type: list, items: { type: object, fields: { by: {}, at: {}, rev: {} } } }\n" +
		"        generated: { type: object }\n"
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := schema.ResolveWith(nil, []string{p})
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := s.Types.Get("note")
	return rt
}

func event(by, at, rev string) *omap.Map {
	m := omap.New()
	m.Set("by", by)
	m.Set("at", canon.DateTime{ISO: at})
	if rev != "" {
		m.Set("rev", rev)
	}
	return m
}

func TestTierFromLiveEvents(t *testing.T) {
	rt := noteType(t)
	meta := omap.New()
	meta.Set("title", "N")
	rev := Rev(rt, meta, "Body.\n")
	if Tier(rt, meta, "Body.") != Unverified {
		t.Fatal("no events must read unverified")
	}

	meta.Set("verified", []any{event("process:ci", "2026-01-01T00:00:00Z", rev)})
	if got := Tier(rt, meta, "Body."); got != Machine {
		t.Errorf("a live process event = %s, want machine", got)
	}
	// A bare mapping is one event; a human: prefix wins.
	meta.Set("verified", event("human:ilya", "2026-01-01T00:00:00Z", rev))
	if got := Tier(rt, meta, "Body."); got != Human {
		t.Errorf("a live human event = %s, want human", got)
	}
	// Bookkeeping moves nothing; content supersedes.
	meta.Set("tags", []any{"x"})
	if got := Tier(rt, meta, "Body."); got != Human {
		t.Errorf("a tag edit superseded the review: %s", got)
	}
	if got := Tier(rt, meta, "Changed."); got != Unverified {
		t.Errorf("a body edit left the review live: %s", got)
	}
	if got := Superseded(rt, meta, "Changed."); len(got) != 1 || got[0] != 0 {
		t.Errorf("Superseded = %v", got)
	}

	// An event without rev holds always (OKF §5.3), even when the entity was
	// written after it.
	meta.Set("verified", []any{event("human:ext", "2026-01-02T00:00:00Z", "")})
	g := omap.New()
	g.Set("at", canon.DateTime{ISO: "2026-01-03T00:00:00Z"})
	meta.Set("generated", g)
	if got := Tier(rt, meta, "Changed."); got != Human {
		t.Errorf("a rev-less event older than the last write = %s, want human", got)
	}
}

// An event counts only with a conventional actor and an OKF datetime `at`
// (a T and an explicit offset, either letter in either case).
func TestEventsFailingTheRulesNeverCount(t *testing.T) {
	rt := noteType(t)
	meta := omap.New()
	meta.Set("title", "N")
	cases := []struct {
		name string
		e    *omap.Map
		want string
	}{
		{"offsetless at", event("human:a", "2026-11-01T00:00:00", ""), Unverified},
		{"space for T", event("human:a", "2026-11-01 00:00:00Z", ""), Unverified},
		{"date only", event("human:a", "2026-11-01", ""), Unverified},
		{"empty human id", event("human:", "2026-11-01T00:00:00Z", ""), Unverified},
		{"lowercase t and z", event("human:a", "2026-11-01t00:00:00z", ""), Human},
		{"offset", event("process:ci", "2026-11-01T00:00:00+01:00", ""), Machine},
	}
	for _, c := range cases {
		meta.Set("verified", c.e)
		if got := Tier(rt, meta, "Body."); got != c.want {
			t.Errorf("%s: tier %s, want %s", c.name, got, c.want)
		}
	}
	noAt := omap.New()
	noAt.Set("by", "human:a")
	meta.Set("verified", noAt)
	if got := Tier(rt, meta, "Body."); got != Unverified {
		t.Errorf("an event without at: tier %s", got)
	}
}
