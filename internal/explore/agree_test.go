package explore

import (
	"slices"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/query"
)

// firmOpsWS is a small firm-ops workspace with a union edge and a few owners.
func firmOpsWS(t *testing.T) string {
	t.Helper()
	root := initWS(t, "firm-ops")
	mustCreate(t, root, "person", "ana", false, "title", "Ana", "name", "Ana", "role", "manager")
	mustCreate(t, root, "client", "acme", false, "title", "Acme")
	mustCreate(t, root, "client", "globex", false, "title", "Globex")
	mustCreate(t, root, "opportunity", "acme-deal", false,
		"title", "Acme Deal", "client", "acme", "stage", "prospect")
	mustCreate(t, root, "opportunity", "globex-deal", false,
		"title", "Globex Deal", "client", "globex", "stage", "prospect", "related", "acme-deal")
	return root
}

// For every type of both presets, every count column equals what the
// CLI reports. Outgoing and declared-inverse columns are held against
// `khub query --count '<name>=N'`; an incoming column with no declared inverse
// — which query cannot name — against the inbound edges `khub get --edges`
// reports, ids normalized to canonical form.
func TestCountsAgreeWithTheCLI(t *testing.T) {
	for preset, root := range map[string]string{"build-hub": buildHubWS(t), "firm-ops": firmOpsWS(t)} {
		resolved, err := introspect.LoadSchema(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, typ := range resolved.Types.Keys() {
			set, err := Rows(root, typ, now)
			if err != nil {
				t.Fatalf("%s %s: %v", preset, typ, err)
			}
			checkAgainstQuery(t, root, preset, typ, set)
			checkAgainstGet(t, root, preset, set)
		}
	}
}

func rowCounts(set *omap.Map) map[string]map[string]int {
	out := map[string]map[string]int{}
	rows, _ := set.Get("rows")
	for _, r := range rows.([]any) {
		row := r.(*omap.Map)
		id, _ := row.Get("id")
		counts, _ := row.Get("counts")
		c := map[string]int{}
		for _, k := range counts.(*omap.Map).Keys() {
			v, _ := counts.(*omap.Map).Get(k)
			c[k] = v.(int)
		}
		out[id.(string)] = c
	}
	return out
}

// checkAgainstQuery compares each outgoing column, and each declared-inverse
// incoming column summed over the source types declaring that inverse, with
// the ids `query --count name=N` keeps, for every N the type shows.
func checkAgainstQuery(t *testing.T, root, preset, typ string, set *omap.Map) {
	t.Helper()
	counts := rowCounts(set)
	byName := map[string][]string{} // query name → column keys it sums
	rels, _ := set.Get("relations")
	for _, c := range rels.([]any) {
		col := c.(*omap.Map)
		key, _ := col.Get("key")
		label, _ := col.Get("label")
		dir, _ := col.Get("direction")
		k, l := key.(string), label.(string)
		switch {
		case dir == DirOut:
			byName[strings.TrimPrefix(k, "out:")] = append(byName[strings.TrimPrefix(k, "out:")], k)
		case !strings.Contains(l, "."): // a declared inverse
			byName[l] = append(byName[l], k)
		}
	}
	for name, colKeys := range byName {
		sum := map[string]int{}
		values := map[int]bool{0: true}
		for id, c := range counts {
			for _, k := range colKeys {
				sum[id] += c[k]
			}
			values[sum[id]] = true
		}
		for n := range values {
			tname := typ
			got, err := query.Query(root, query.Filters{
				Type:   &tname,
				Counts: []query.Count{{Name: name, Cmp: query.Comparison{Op: "=", N: n}}},
			}, now)
			if err != nil {
				t.Fatalf("%s %s query %s=%d: %v", preset, typ, name, n, err)
			}
			var queried, ours []string
			for _, m := range got {
				queried = append(queried, m.Type+"/"+m.Slug)
			}
			for id, v := range sum {
				if v == n {
					ours = append(ours, id)
				}
			}
			slices.Sort(queried)
			slices.Sort(ours)
			if !slices.Equal(queried, ours) {
				t.Errorf("%s %s %s=%d: query %v, rows %v", preset, typ, name, n, queried, ours)
			}
		}
	}
}

// checkAgainstGet compares each incoming column without a declared inverse
// (labelled <source type>.<predicate>) with the distinct sources of that
// predicate among the derived edges `get --edges` reports.
func checkAgainstGet(t *testing.T, root, preset string, set *omap.Map) {
	t.Helper()
	rows, _ := set.Get("rows")
	rels, _ := set.Get("relations")
	for _, r := range rows.([]any) {
		row := r.(*omap.Map)
		id, _ := row.Get("id")
		view, err := entity.Get(root, id.(string), true)
		if err != nil {
			t.Fatal(err)
		}
		links, _ := row.Get("links")
		for _, c := range rels.([]any) {
			col := c.(*omap.Map)
			key, _ := col.Get("key")
			label, _ := col.Get("label")
			dir, _ := col.Get("direction")
			k, l := key.(string), label.(string)
			if dir != DirIn || !strings.Contains(l, ".") {
				continue
			}
			want := map[string]bool{}
			for _, e := range view.Edges {
				if !e.Derived {
					continue
				}
				src := strings.SplitN(e.Target, "/", 2)[0]
				pred := e.Predicate
				if strings.HasPrefix(k, "in:*.") {
					// collapsed base column: any source type, this predicate
					if strings.HasSuffix(pred, "."+strings.TrimPrefix(k, "in:*.")) {
						want[e.Target] = true
					}
					continue
				}
				if pred == l && strings.HasPrefix(k, "in:"+src+".") {
					want[e.Target] = true
				}
			}
			got := map[string]bool{}
			v, _ := links.(*omap.Map).Get(k)
			for _, x := range v.([]any) {
				got[x.(string)] = true
			}
			if len(got) != len(want) {
				t.Errorf("%s %v %s: rows %v, get --edges %v", preset, id, k, got, want)
				continue
			}
			for x := range want {
				if !got[x] {
					t.Errorf("%s %v %s: get --edges has %s, rows do not", preset, id, k, x)
				}
			}
		}
	}
}
