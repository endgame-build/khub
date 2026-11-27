package explore

import (
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

// A marked list holds what the overview counts, each row carrying the marker.
func TestMarkedAgreesWithOverview(t *testing.T) {
	root := reviewedWS(t)
	overview, err := Overview(root, now)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, name := range MarkedNames {
		want := 0
		for _, ty := range get(t, overview, "types").([]any) {
			group := "markers"
			if name == "unverified" {
				group = "trust"
			}
			want += get(t, get(t, ty.(*omap.Map), group).(*omap.Map), name).(int)
		}
		out, err := Marked(root, name, now)
		if err != nil {
			t.Fatal(err)
		}
		rows := get(t, out, "rows").([]any)
		if get(t, out, "matched") != want || len(rows) != want {
			t.Errorf("%s: %v matched, %d rows, overview says %d", name, get(t, out, "matched"), len(rows), want)
		}
		for _, r := range rows {
			m := get(t, r.(*omap.Map), "markers").(*omap.Map)
			if name == "unverified" && get(t, m, "trust") != "unverified" || name != "unverified" && get(t, m, name) != true {
				t.Errorf("%s lists %v with %v", name, get(t, r.(*omap.Map), "id"), m)
			}
		}
		seen += want
	}
	if seen == 0 {
		t.Error("nothing was marked")
	}
}
