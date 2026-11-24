package explore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/project"
	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/savedquery"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/search"
	"github.com/endgame-build/khub/internal/workspace"
)

// reviewedWS is buildHubWS with one review that holds and one whose content
// changed after it.
func reviewedWS(t *testing.T) string {
	t.Helper()
	root := buildHubWS(t)
	for _, id := range []string{"capability/cap-pay", "capability/cap-report"} {
		if _, err := entity.Verify(root, id, "human:ana", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "knowledge", "capabilities", "cap-report.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(raw)+"\nEdited after the review.\n")
	return root
}

// Every number on the overview is the length of the list it leads to, and its
// totals are the ones `khub status` prints.
func TestOverviewAgreesWithRowsAndStatus(t *testing.T) {
	for preset, root := range map[string]string{"build-hub": reviewedWS(t), "firm-ops": firmOpsWS(t)} {
		out, err := Overview(root, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := canon.EncodeCLI(out); err != nil {
			t.Fatal(err)
		}
		total := map[string]int{}
		tiers := map[string]int{}
		rowsOf := map[string][]any{}
		for _, ty := range get(t, out, "types").([]any) {
			m := ty.(*omap.Map)
			name := get(t, m, "name").(string)
			set, err := Rows(root, name, now)
			if err != nil {
				t.Fatal(err)
			}
			rows := get(t, set, "rows").([]any)
			rowsOf[name] = rows
			if get(t, m, "count") != len(rows) {
				t.Errorf("%s %s count = %v, rows %d", preset, name, get(t, m, "count"), len(rows))
			}
			total["all"] += len(rows)
			for _, marker := range markerNames {
				n := 0
				for _, r := range rows {
					if get(t, r.(*omap.Map), marker) == true {
						n++
					}
				}
				if got := get(t, get(t, m, "markers").(*omap.Map), marker); got != n {
					t.Errorf("%s %s %s = %v, rows say %d", preset, name, marker, got, n)
				}
				total[marker] += n
			}
			for _, tier := range get(t, m, "trust").(*omap.Map).Keys() {
				tiers[tier] += get(t, get(t, m, "trust").(*omap.Map), tier).(int)
			}
		}
		days, err := workspace.StaleDays(root)
		if err != nil {
			t.Fatal(err)
		}
		status, err := project.Project(root, days, now)
		if err != nil {
			t.Fatal(err)
		}
		if total["all"] != status.Total || total["draft"] != status.Draft || total["orphan"] != status.Orphan ||
			total["expired"] != status.Expired {
			t.Errorf("%s totals %v, status %+v", preset, total, status)
		}
		for _, tier := range status.Trust.Keys() {
			if n, _ := status.Trust.Get(tier); n != tiers[tier] {
				t.Errorf("%s %s = %d, status %v", preset, tier, tiers[tier], n)
			}
		}
		for _, rel := range get(t, out, "relations").([]any) {
			m := rel.(*omap.Map)
			from, predicate := get(t, m, "from").(string), get(t, m, "predicate").(string)
			links, have, reach := 0, 0, 0
			for _, r := range rowsOf[from] {
				n := get(t, get(t, r.(*omap.Map), "counts").(*omap.Map), DirOut+":"+predicate).(int)
				links += n
				if n > 0 {
					have++
				}
			}
			for _, to := range get(t, m, "to").([]any) {
				reach += get(t, to.(*omap.Map), "links").(int)
			}
			if get(t, m, "links") != links || get(t, m, "have") != have || reach != links {
				t.Errorf("%s %s.%s = %v links, %v have, %d reach; rows say %d, %d", preset, from, predicate,
					get(t, m, "links"), get(t, m, "have"), reach, links, have)
			}
		}
	}
}

// A review that still holds is no finding; one the content has moved past is.
func TestSupersededMarksTheNewestReview(t *testing.T) {
	root := reviewedWS(t)
	set, err := Rows(root, "capability", now)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"capability/cap-pay": false, "capability/cap-report": true, "capability/cap-ideas": false} {
		if got := get(t, rowByID(t, set, id), "superseded"); got != want {
			t.Errorf("%s superseded = %v, want %v", id, got, want)
		}
	}
	page, err := Page(root, "capability/cap-report", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := get(t, page, "reasons").(*omap.Map).Get("superseded"); !ok {
		t.Errorf("reasons = %v", get(t, page, "reasons"))
	}
	// Reviewed again as it now is: the old review is answered.
	if _, err := entity.Verify(root, "capability/cap-report", "process:ci", "", nil); err != nil {
		t.Fatal(err)
	}
	set, err = Rows(root, "capability", now)
	if err != nil {
		t.Fatal(err)
	}
	if get(t, rowByID(t, set, "capability/cap-report"), "superseded") != false {
		t.Error("a newer review that holds does not answer the older one")
	}
}

// A marker that is set has a reason, and one that is not has none.
func TestReasonsMatchMarkers(t *testing.T) {
	root := reviewedWS(t)
	for _, id := range []string{"use-case/uc-half", "actor/act-lonely", "system/sys-old", "capability/cap-ideas", "component/cmp-ledger"} {
		page, err := Page(root, id, now)
		if err != nil {
			t.Fatal(err)
		}
		markers, reasons := get(t, page, "markers").(*omap.Map), get(t, page, "reasons").(*omap.Map)
		for _, name := range markerNames {
			if name == "stale" {
				continue // stale is age or expiry; expiry is its own reason
			}
			_, has := reasons.Get(name)
			if set := get(t, markers, name) == true; set != has {
				t.Errorf("%s %s set %v, reason %v", id, name, set, has)
			}
		}
	}
	page, err := Page(root, "use-case/uc-half", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := get(t, get(t, page, "reasons").(*omap.Map), "incomplete"); !strings.Contains(fmt.Sprint(got), "trigger") {
		t.Errorf("incomplete reason = %v", got)
	}
}

// Findings are check's: the same verdict, the same number per bucket.
func TestFindingsAgreeWithCheck(t *testing.T) {
	root := reviewedWS(t)
	seen := 0
	for _, broken := range []bool{false, true} {
		// What the command line finds and the core cannot.
		shadows := func(*schema.ResolvedSchema) []integrity.FlagShadow { return nil }
		if broken {
			shadows = func(*schema.ResolvedSchema) []integrity.FlagShadow {
				return []integrity.FlagShadow{{Type: "component", Field: "dry-run", Commands: []string{"add"}}}
			}
			writeFile(t, filepath.Join(root, "knowledge", "components", "cmp-loop-a.md"), "---\ntype: component\ntitle: A\nkind: service\ndepends_on: [cmp-loop-b]\n---\n")
			writeFile(t, filepath.Join(root, "knowledge", "components", "cmp-loop-b.md"), "---\ntype: component\ntitle: B\nkind: service\ndepends_on: [cmp-loop-a, cmp-nowhere]\naliases: [twin]\n---\n")
			writeFile(t, filepath.Join(root, "knowledge", "components", "cmp-loop-c.md"), "---\ntype: component\ntitle: C\nkind: service\naliases: [twin]\n---\n")
			writeFile(t, filepath.Join(root, ".khub", "templates", "nobody.yaml"), "sections: []\n")
			writeFile(t, filepath.Join(root, "knowledge", "components", "cmp-broken.md"), "---\ntitle: [\n---\n")
			writeFile(t, filepath.Join(root, "knowledge", "components", "cmp-stray.md"), "---\ntype: actor\ntitle: Stray\n---\n")
		}
		out, err := Findings(root, shadows, now)
		if err != nil {
			t.Fatal(err)
		}
		report, err := integrity.Check(root, false, now)
		if err != nil {
			t.Fatal(err)
		}
		report.FlagShadowed = shadows(nil)
		if get(t, out, "passed") != report.Passed() {
			t.Errorf("passed = %v, check says %v", get(t, out, "passed"), report.Passed())
		}
		got := map[string]int{}
		for _, b := range get(t, out, "buckets").([]any) {
			m := b.(*omap.Map)
			got[get(t, m, "bucket").(string)] = get(t, m, "total").(int)
		}
		for bucket, n := range map[string]int{
			"malformed": len(report.Malformed), "incomplete": len(report.Incomplete), "dangling": len(report.Dangling),
			"body_shape": len(report.BodyShape), "cycles": len(report.Cycles), "strays": len(report.Strays),
			"misplaced": len(report.Misplaced), "alias_conflicts": len(report.AliasConflicts),
			"missing_singletons": len(report.MissingSingletons), "draft_required_singletons": len(report.DraftRequiredSingletons),
			"stray_templates": len(report.StrayTemplates), "missing_templates": len(report.MissingTemplates),
			"template_invalid": len(report.TemplateInvalid), "flag_shadowed": len(report.FlagShadowed),
			"stale_waivers": len(report.StaleWaivers),
		} {
			if broken && n > 0 {
				seen++
			}
			if got[bucket] != n {
				t.Errorf("%s = %d, check says %d", bucket, got[bucket], n)
			}
		}
		// What fails the gate is listed, and nothing else is.
		if (len(got) == 0) != report.Passed() {
			t.Errorf("buckets %v, passed %v", got, report.Passed())
		}
	}
	if seen < 6 {
		t.Errorf("only %d buckets had a finding to compare", seen)
	}
}

// A file that does not parse carries what the parser says, on one line.
func TestFindingsSayWhyAFileDoesNotParse(t *testing.T) {
	root := buildHubWS(t)
	dir := filepath.Join(root, "knowledge", "components")
	writeFile(t, filepath.Join(dir, "cmp-a.md"), "---\ntype: component\ntitle: [unclosed\n---\n")
	writeFile(t, filepath.Join(dir, "cmp-b.md"), "---\ntitle: 1\ntitle: "+strings.Repeat("x", 600)+"\n---\n")
	out, err := Findings(root, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	b := get(t, out, "buckets").([]any)[0].(*omap.Map)
	if get(t, b, "bucket") != "malformed" || get(t, b, "total") != 2 {
		t.Fatalf("bucket %v", b)
	}
	for i, want := range []string{"[3:8]", "already defined"} {
		reason := get(t, get(t, b, "items").([]any)[i].(*omap.Map), "reason").(string)
		if !strings.Contains(reason, want) || strings.Contains(reason, "\n") || len([]rune(reason)) > 240 {
			t.Errorf("reason %q", reason)
		}
	}
}

// superseded is check's superseded_verifications, of the newest review.
func TestSupersededAgreesWithCheck(t *testing.T) {
	root := reviewedWS(t)
	report, err := integrity.Check(root, false, now)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, v := range report.Superseded {
		want[v.Type+"/"+v.Slug] = true // one review each here, so the newest
	}
	got := 0
	for _, typ := range []string{"capability", "component"} {
		set, err := Rows(root, typ, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range get(t, set, "rows").([]any) {
			m := r.(*omap.Map)
			id, on := get(t, m, "id").(string), get(t, m, "superseded") == true
			if on != want[id] {
				t.Errorf("%s superseded = %v, check says %v", id, on, want[id])
			}
			if on {
				got++
			}
		}
	}
	if got == 0 || got != len(want) {
		t.Errorf("%d superseded of check's %d", got, len(want))
	}
}

// A row's date is the one recent and stale judge it by.
func TestRowsDatesAgreeWithRecent(t *testing.T) {
	root := buildHubWS(t)
	report, err := gitlog.Recent(root, 3650, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	dates := map[string]string{}
	for _, e := range report.Entries {
		dates[e.Type+"/"+e.Slug] = e.EffectiveDate.Format("2006-01-02")
	}
	set, err := Rows(root, "component", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range get(t, set, "rows").([]any) {
		m := r.(*omap.Map)
		if id := get(t, m, "id").(string); get(t, m, "updated") != dates[id] {
			t.Errorf("%s updated %v, recent says %s", id, get(t, m, "updated"), dates[id])
		}
	}
}

func TestRecentAgreesWithRecent(t *testing.T) {
	root := buildHubWS(t)
	out, err := Recent(root, 7, RecentLimit, now)
	if err != nil {
		t.Fatal(err)
	}
	report, err := gitlog.Recent(root, 7, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	rows := get(t, out, "rows").([]any)
	if get(t, out, "total") != report.Total || len(rows) != min(report.Total, RecentLimit) {
		t.Fatalf("%v rows of %v, recent has %d", len(rows), get(t, out, "total"), report.Total)
	}
	for i, r := range rows {
		m := r.(*omap.Map)
		e := report.Entries[i]
		if get(t, m, "id") != e.Type+"/"+e.Slug {
			t.Errorf("row %d = %v, recent has %s/%s", i, get(t, m, "id"), e.Type, e.Slug)
		}
		// init scaffolds the singletons, and a scaffold is nobody's writing.
		scaffold := e.Type == "prd" || e.Type == "arc42"
		if (get(t, m, "by") == "khub") == scaffold || get(t, m, "new") != true {
			t.Errorf("%v by %v, new %v", get(t, m, "id"), get(t, m, "by"), get(t, m, "new"))
		}
	}
}

func TestFindNamesFirstThenSearches(t *testing.T) {
	root := buildHubWS(t)
	ids := func(q string) []string {
		out, err := Find(root, q, now)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		var got []string
		for _, h := range get(t, out, "hits").([]any) {
			got = append(got, get(t, h.(*omap.Map), "id").(string))
		}
		if total := get(t, out, "total").(int); total < len(got) {
			t.Errorf("%q total %d under %d hits", q, total, len(got))
		}
		return got
	}
	if got := ids("cmp-ledger"); len(got) == 0 || got[0] != "component/cmp-ledger" {
		t.Errorf("a slug finds %v", got)
	}
	if got := ids("COMPONENT/CMP-LEDGER"); len(got) == 0 || got[0] != "component/cmp-ledger" {
		t.Errorf("an id in capitals finds %v", got)
	}
	// Full text keeps the CLI's order.
	res, err := search.Search(root, "ledger", search.Options{Limit: findLimit, Plain: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	got := ids("ledger")
	if len(got) != len(res.Hits) {
		t.Fatalf("%v, search has %d hits", got, len(res.Hits))
	}
	for i, h := range res.Hits {
		if got[i] != h.Type+"/"+h.Slug {
			t.Errorf("hit %d = %s, search has %s/%s", i, got[i], h.Type, h.Slug)
		}
	}
	// A near miss is a suggestion, not a name: search decides what it finds.
	near, err := search.Search(root, "cmp-ledgr", search.Options{Limit: findLimit, Plain: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids("cmp-ledgr"); len(got) != len(near.Hits) {
		t.Errorf("a near miss finds %v, search has %d hits", got, len(near.Hits))
	}
	// An alias is a name.
	writeFile(t, filepath.Join(root, "knowledge", "components", "cmp-known.md"), "---\ntype: component\ntitle: Known\nkind: service\naliases: [also-known]\n---\n")
	if got := ids("also-known"); len(got) == 0 || got[0] != "component/cmp-known" {
		t.Errorf("an alias finds %v", got)
	}
	// Whatever is typed is words, never query syntax, and nothing is no error.
	for _, q := range []string{"", "   ", `"`, "AND", "a*b(", "no-such-thing-anywhere"} {
		ids(q)
	}
}

func TestSavedQueriesRunAsKhubRunDoes(t *testing.T) {
	root := buildHubWS(t)
	typ := "component"
	if _, err := savedquery.Save(root, "parts", savedquery.Definition(query.Filters{Type: &typ}), false); err != nil {
		t.Fatal(err)
	}
	list, err := SavedQueries(root, now)
	if err != nil {
		t.Fatal(err)
	}
	q := get(t, list, "queries").([]any)[0].(*omap.Map)
	_, matched, err := query.QueryCounted(root, query.Filters{Type: &typ}, now)
	if err != nil {
		t.Fatal(err)
	}
	if get(t, q, "name") != "parts" || get(t, q, "count") != matched {
		t.Errorf("listed %v, query matches %d", q, matched)
	}
	out, err := SavedQuery(root, "parts", now)
	if err != nil {
		t.Fatal(err)
	}
	if rows := get(t, out, "rows").([]any); len(rows) != 3 || get(t, rows[0].(*omap.Map), "id") != "component/cmp-gateway" {
		t.Errorf("rows %v", rows)
	}
	if _, err := SavedQuery(root, "nope", now); err == nil {
		t.Error("an unknown name is not refused")
	}
	// A query the schema has moved past reports why instead of failing the list.
	writeFile(t, filepath.Join(root, ".khub", "queries.yaml"), "gone:\n  type: vanished\n")
	list, err = SavedQueries(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if e := get(t, get(t, list, "queries").([]any)[0].(*omap.Map), "error"); e == nil {
		t.Error("an outdated query carries no reason")
	}
}
