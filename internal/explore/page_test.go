package explore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/omap"
)

func groupsOf(t *testing.T, page *omap.Map) []*omap.Map {
	t.Helper()
	var out []*omap.Map
	for _, g := range get(t, page, "neighbours").([]any) {
		out = append(out, g.(*omap.Map))
	}
	return out
}

func TestPageShape(t *testing.T) {
	page, err := Page(buildHubWS(t), "component/cmp-ledger", now)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"markers", "body_tree", "neighbours"}; !eq(page.Keys(), want) {
		t.Fatalf("page keys %v, want %v", page.Keys(), want)
	}
	var gkeys []string
	for _, g := range groupsOf(t, page) {
		if want := []string{"key", "label", "direction", "base", "items"}; !eq(g.Keys(), want) {
			t.Fatalf("group keys %v", g.Keys())
		}
		gkeys = append(gkeys, get(t, g, "key").(string))
	}
	// EdgeGroup order is column order; empty groups are left out.
	want := []string{"out:system", "in:use-case.served_by", "in:requirement.realized_in", "in:adr.affects", "in:api.provider"}
	if !eq(gkeys, want) {
		t.Fatalf("group order %v, want %v", gkeys, want)
	}
	served := groupsOf(t, page)[1]
	if get(t, served, "label") != "serves" || get(t, served, "direction") != DirIn {
		t.Fatalf("served group = %v", served)
	}
	item := get(t, served, "items").([]any)[0].(*omap.Map)
	if want := []string{"id", "resolved", "type", "title", "markers", "summary"}; !eq(item.Keys(), want) {
		t.Fatalf("neighbour keys %v", item.Keys())
	}
	if get(t, item, "id") != "use-case/uc-pay" || get(t, item, "title") != "Pay an invoice" {
		t.Fatalf("neighbour = %v", item)
	}
	if got := get(t, item, "summary"); got != "The clerk pays an invoice. It settles the same day." {
		t.Fatalf("summary %q", got)
	}
}

// A capability's description is its summary as a neighbour.
func TestPageNeighbourSummaryPrefersDescription(t *testing.T) {
	page, err := Page(buildHubWS(t), "use-case/uc-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groupsOf(t, page) {
		if get(t, g, "key") != "out:capability" {
			continue
		}
		it := get(t, g, "items").([]any)[0].(*omap.Map)
		if get(t, it, "summary") != "Moving money between accounts." {
			t.Fatalf("capability summary %v", get(t, it, "summary"))
		}
		return
	}
	t.Fatal("no capability group")
}

// A stored target that names nothing is shown as unresolved, not hidden.
func TestPageUnresolvedTarget(t *testing.T) {
	root := buildHubWS(t)
	if _, err := entity.Delete(root, "cmp-gateway", true, false); err != nil {
		t.Fatal(err)
	}
	page, err := Page(root, "use-case/uc-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groupsOf(t, page) {
		if get(t, g, "key") != "out:served_by" {
			continue
		}
		var dangling *omap.Map
		for _, it := range get(t, g, "items").([]any) {
			m := it.(*omap.Map)
			if get(t, m, "resolved") == false {
				dangling = m
			}
		}
		if dangling == nil {
			t.Fatalf("no unresolved item in %v", g)
		}
		if _, has := dangling.Get("markers"); has {
			t.Fatal("an unresolved neighbour carries no markers")
		}
		if !strings.Contains(get(t, dangling, "id").(string), "cmp-gateway") {
			t.Fatalf("unresolved id = %v", get(t, dangling, "id"))
		}
		return
	}
	t.Fatal("no served_by group")
}

// The union of the groups is what `khub get --edges` reports, both
// sides in canonical ids — get names an inbound target type/slug, and a
// stored forward target by what was written.
func TestPageAgreesWithGetEdges(t *testing.T) {
	root := buildHubWS(t)
	for _, id := range []string{
		"component/cmp-ledger", "capability/cap-pay", "adr/ad-2026-01-05-first",
		"adr/ad-2026-02-05-second", "use-case/uc-pay", "requirement/req-settle",
	} {
		page, err := Page(root, id, now)
		if err != nil {
			t.Fatal(err)
		}
		ours := map[string]bool{}
		for _, g := range groupsOf(t, page) {
			for _, it := range get(t, g, "items").([]any) {
				ours[get(t, g, "direction").(string)+" "+get(t, it.(*omap.Map), "id").(string)] = true
			}
		}
		view, err := entity.Get(root, id, true)
		if err != nil {
			t.Fatal(err)
		}
		theirs := map[string]bool{}
		for _, e := range view.Edges {
			dir := DirOut
			if e.Derived {
				dir = DirIn
			}
			for _, rid := range e.ResolvedTargets {
				theirs[dir+" "+rid] = true
			}
			if len(e.ResolvedTargets) == 0 {
				theirs[dir+" "+e.Target] = true
			}
		}
		if len(ours) != len(theirs) {
			t.Errorf("%s: page %v, get --edges %v", id, ours, theirs)
			continue
		}
		for k := range theirs {
			if !ours[k] {
				t.Errorf("%s: get --edges has %s, the page does not", id, k)
			}
		}
	}
}

func TestPageBodyTreeResolvesEntityLinks(t *testing.T) {
	root := buildHubWS(t)
	p := filepath.Join(root, "knowledge", "use-cases", "uc-refund.md")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(raw, []byte("\nSee [the ledger](../components/cmp-ledger.md).\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	page, err := Page(root, "use-case/uc-refund", now)
	if err != nil {
		t.Fatal(err)
	}
	as := find(get(t, page, "body_tree").([]any), "a")
	if len(as) != 1 || str(as[0], "entity") != "component/cmp-ledger" {
		t.Fatalf("anchors = %v", as)
	}
}

func TestPageUnknownID(t *testing.T) {
	if _, err := Page(buildHubWS(t), "nope", now); err == nil {
		t.Fatal("want a lookup error")
	}
}

// A qualified id resolves to exactly that node: an alias that happens to
// answer to the same name is not a stand-in for it.
func TestPageQualifiedIDNeverFallsBackToAnAlias(t *testing.T) {
	root := buildHubWS(t)
	if _, err := entity.Update(root, "cap-pay", entity.UpdateOpts{Fields: pairs("aliases", "money")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Page(root, "money", now); err != nil {
		t.Fatalf("a bare alias still resolves: %v", err)
	}
	if _, err := Page(root, "capability/money", now); err == nil {
		t.Fatal("a qualified id must name the node itself, not an alias of another")
	}
}

// Nor is a case variant: on a case-sensitive filesystem `client/Acme` can be
// a stray beside the real `client/acme`, and resolution's casefold fallback
// must not hand the page the real one's body and connections.
func TestPageQualifiedIDIsExact(t *testing.T) {
	root := buildHubWS(t)
	if _, err := Page(root, "capability/cap-pay", now); err != nil {
		t.Fatalf("the exact id resolves: %v", err)
	}
	if _, err := Page(root, "capability/CAP-PAY", now); err == nil {
		t.Fatal("a case variant of a qualified id must not stand in for it")
	}
}
