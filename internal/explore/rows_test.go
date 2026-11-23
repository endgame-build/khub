package explore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

func get(t *testing.T, m *omap.Map, key string) any {
	t.Helper()
	v, ok := m.Get(key)
	if !ok {
		t.Fatalf("missing key %q in %v", key, m.Keys())
	}
	return v
}

func rowByID(t *testing.T, set *omap.Map, id string) *omap.Map {
	t.Helper()
	for _, r := range get(t, set, "rows").([]any) {
		row := r.(*omap.Map)
		if get(t, row, "id") == id {
			return row
		}
	}
	t.Fatalf("no row %s", id)
	return nil
}

func TestRowsShapeAndOrder(t *testing.T) {
	set, err := Rows(buildHubWS(t), "component", now)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"type", "total", "attributes", "relations", "rows"}; !eq(set.Keys(), want) {
		t.Fatalf("RowSet keys %v, want %v", set.Keys(), want)
	}
	if get(t, set, "total") != 3 {
		t.Fatalf("total = %v, want 3 components", get(t, set, "total"))
	}
	// RowSet.attributes: the type's own plain attributes, base bookkeeping out,
	// title out (it is the title column).
	var names []string
	for _, a := range get(t, set, "attributes").([]any) {
		names = append(names, get(t, a.(*omap.Map), "name").(string))
	}
	if want := []string{"kind", "stack", "owner", "lifecycle", "tier"}; !eq(names, want) {
		t.Fatalf("attribute columns %v, want %v", names, want)
	}
	kind := get(t, set, "attributes").([]any)[0].(*omap.Map)
	if enum := get(t, kind, "enum").([]any); len(enum) != 3 {
		t.Fatalf("kind enum = %v", enum)
	}
	// RowSet.rows: index order (scan order), not sorted by the server.
	var ids []string
	for _, r := range get(t, set, "rows").([]any) {
		ids = append(ids, get(t, r.(*omap.Map), "id").(string))
	}
	if want := []string{"component/cmp-gateway", "component/cmp-ledger", "component/cmp-portal"}; !eq(ids, want) {
		t.Fatalf("row order %v, want %v", ids, want)
	}
	row := rowByID(t, set, "component/cmp-ledger")
	want := []string{"id", "slug", "title", "draft", "stale", "orphan", "incomplete", "trust", "attrs", "counts", "links"}
	if !eq(row.Keys(), want) {
		t.Fatalf("Row keys %v, want %v", row.Keys(), want)
	}
	// Row.attrs: present values only.
	attrs := get(t, row, "attrs").(*omap.Map)
	if !eq(attrs.Keys(), []string{"kind", "owner", "tier"}) {
		t.Fatalf("attrs keys %v", attrs.Keys())
	}
	if get(t, attrs, "tier") != "tier-1" {
		t.Fatalf("tier = %v", get(t, attrs, "tier"))
	}
}

func TestRowsCountsAndLinks(t *testing.T) {
	set, err := Rows(buildHubWS(t), "component", now)
	if err != nil {
		t.Fatal(err)
	}
	var colKeys []string
	for _, c := range get(t, set, "relations").([]any) {
		colKeys = append(colKeys, get(t, c.(*omap.Map), "key").(string))
	}
	for _, r := range get(t, set, "rows").([]any) {
		row := r.(*omap.Map)
		counts := get(t, row, "counts").(*omap.Map)
		links := get(t, row, "links").(*omap.Map)
		// Every column key present, in column order, zeros included.
		if !eq(counts.Keys(), colKeys) || !eq(links.Keys(), colKeys) {
			t.Fatalf("%v: counts/links keys %v / %v, want %v", get(t, row, "id"), counts.Keys(), links.Keys(), colKeys)
		}
		for _, k := range colKeys {
			c := get(t, counts, k).(int)
			l := get(t, links, k).([]any)
			if c != len(l) {
				t.Errorf("%v %s: count %d, links %v", get(t, row, "id"), k, c, l)
			}
			for _, id := range l {
				if id == get(t, row, "id") {
					t.Errorf("%v links to itself under %s", id, k)
				}
			}
		}
	}
	ledger := rowByID(t, set, "component/cmp-ledger")
	counts := get(t, ledger, "counts").(*omap.Map)
	for key, want := range map[string]int{
		"out:system":                 1,
		"in:use-case.served_by":      2, // uc-pay and uc-refund
		"in:requirement.realized_in": 1,
		"in:api.provider":            1,
		"in:adr.affects":             1,
		"out:consumes":               0,
		"in:*.related":               0,
	} {
		if got := get(t, counts, key); got != want {
			t.Errorf("cmp-ledger %s = %v, want %d", key, got, want)
		}
	}
	links := get(t, ledger, "links").(*omap.Map)
	served := get(t, links, "in:use-case.served_by").([]any)
	if len(served) != 2 || served[0] != "use-case/uc-pay" || served[1] != "use-case/uc-refund" {
		t.Fatalf("served_by links = %v", served)
	}
}

// The incoming base relation is one column whichever type stores the edge.
func TestRowsCollapsedBaseIncoming(t *testing.T) {
	set, err := Rows(buildHubWS(t), "capability", now)
	if err != nil {
		t.Fatal(err)
	}
	row := rowByID(t, set, "capability/cap-report")
	if got := get(t, get(t, row, "counts").(*omap.Map), "in:*.related"); got != 1 {
		t.Fatalf("cap-report in:*.related = %v, want 1 (req-settle relates to it)", got)
	}
}

func TestRowsEmptyType(t *testing.T) {
	set, err := Rows(buildHubWS(t), "repo", now)
	if err != nil {
		t.Fatal(err)
	}
	if get(t, set, "total") != 0 || len(get(t, set, "rows").([]any)) != 0 {
		t.Fatalf("empty type: %v", set)
	}
}

func TestRowsUnknownType(t *testing.T) {
	_, err := Rows(buildHubWS(t), "nope", now)
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "unknown_type" {
		t.Fatalf("want unknown_type, got %v", err)
	}
}

// The set encodes through the CLI encoder: every list is []any, so nothing
// collapses into a string.
func TestRowsEncode(t *testing.T) {
	set, err := Rows(buildHubWS(t), "use-case", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canon.EncodeCLI(set); err != nil {
		t.Fatal(err)
	}
}

// A file that does not parse would silently shrink every count; the view
// refuses instead, as viz does, and names the file.
func TestRowsRefuseMalformedScan(t *testing.T) {
	root := buildHubWS(t)
	writeFile(t, filepath.Join(root, "knowledge", "components", "cmp-broken.md"), "---\ntype: component\ntitle: [unclosed\n---\n")
	for name, call := range map[string]func() error{
		"rows": func() error { _, err := Rows(root, "component", now); return err },
		"page": func() error { _, err := Page(root, "component/cmp-ledger", now); return err },
	} {
		err := call()
		var located *errs.Located
		if !errors.As(err, &located) || located.Code != "malformed_projection" || !strings.Contains(located.Message, "cmp-broken.md") {
			t.Errorf("%s: want malformed_projection naming the file, got %v", name, err)
		}
	}
}
