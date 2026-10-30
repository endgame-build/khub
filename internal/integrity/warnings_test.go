package integrity

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/entity"
)

// sample is a non-empty value of t: strings are "x", a slice holds one
// sample, a struct has every field sampled.
func sample(t reflect.Type) reflect.Value {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Slice:
		v.Set(reflect.Append(reflect.MakeSlice(t, 0, 1), sample(t.Elem())))
	case reflect.Struct:
		for i := range t.NumField() {
			v.Field(i).Set(sample(t.Field(i).Type))
		}
	}
	return v
}

// Findings follows Passed for every bucket CheckReport has, derived by
// reflection so a bucket added later is covered without touching this test:
// a report holding one entry in one bucket fails the gate exactly when it
// yields a warning, in both modes.
func TestFindingsFollowPassedEveryBucket(t *testing.T) {
	rt := reflect.TypeFor[CheckReport]()
	buckets := 0
	for i := range rt.NumField() {
		field := rt.Field(i)
		if field.Type.Kind() != reflect.Slice {
			continue
		}
		buckets++
		for _, strict := range []bool{false, true} {
			report := &CheckReport{Strict: strict}
			reflect.ValueOf(report).Elem().Field(i).Set(sample(field.Type))
			if failed, warned := !report.Passed(), len(report.Findings()) > 0; failed != warned {
				t.Errorf("%s (strict=%v): gate failed=%v but warned=%v", field.Name, strict, failed, warned)
			}
		}
	}
	if buckets < 15 {
		t.Fatalf("reflected %d buckets; CheckReport has more", buckets)
	}
}

// Each gating bucket's warning carries that bucket's JSON name as its rule;
// an informational bucket yields none.
func TestFindingsFollowPassed(t *testing.T) {
	gating := map[string]*CheckReport{
		"incomplete":                {Incomplete: []Incomplete{{Type: "project", Slug: "p", MissingRelations: []string{"owner"}}}},
		"orphans":                   {Strict: true, Orphans: []string{"client/c"}},
		"dangling":                  {Dangling: []Dangling{{Type: "project", Slug: "p", Predicate: "client", Target: "gone"}}},
		"strays":                    {Strays: []string{"clients/x.txt"}},
		"stray_templates":           {StrayTemplates: []string{".khub/templates/old.yaml"}},
		"missing_templates":         {MissingTemplates: []string{"project: .khub/templates/gone.yaml"}},
		"template_invalid":          {TemplateInvalid: []FieldError{{Type: "project", Slug: "*", Reason: "bad"}}},
		"body_shape":                {BodyShape: []FieldError{{Type: "project", Slug: "p", Reason: "missing heading"}}},
		"misplaced":                 {Misplaced: []Misplaced{{Path: "lost.md", Type: "client", Expected: "clients/"}}},
		"malformed":                 {Malformed: []string{"clients/broken.md"}},
		"cycles":                    {Cycles: [][]string{{"a/x", "a/y", "a/x"}}},
		"missing_singletons":        {MissingSingletons: []string{"prd"}},
		"draft_required_singletons": {DraftRequiredSingletons: []string{"prd"}, DraftSingletons: []string{"prd"}},
		"alias_conflicts":           {AliasConflicts: []AliasConflict{{Alias: "x", Claimants: []string{"a/x", "b/x"}}}},
	}
	for rule, report := range gating {
		if report.Passed() {
			t.Errorf("%s: the fixture report passes; it should fail the gate", rule)
		}
		got := report.Findings()
		if len(got) != 1 || got[0].Rule != rule {
			t.Errorf("%s: findings = %+v, want one under that rule", rule, got)
		}
	}
	informational := map[string]*CheckReport{
		"orphans outside strict":   {Orphans: []string{"client/c"}},
		"thin":                     {Thin: []FieldError{{Type: "project", Slug: "p", Reason: "short"}}},
		"optional draft singleton": {DraftSingletons: []string{"arc42"}},
		"suppressed dangling":      {SuppressedDangling: 3},
	}
	for name, report := range informational {
		if !report.Passed() {
			t.Errorf("%s: the fixture report fails; it should pass", name)
		}
		if got := report.Findings(); len(got) != 0 {
			t.Errorf("%s: findings = %+v, want none", name, got)
		}
	}
}

// Workspace text cannot forge a second warning line or reach the terminal as
// an escape sequence.
func TestFindingLineEscapesControls(t *testing.T) {
	report := &CheckReport{
		Strays:         []string{"x\nwarning: forged line.md", "e\x1b[31mRED.md"},
		AliasConflicts: []AliasConflict{{Alias: "a\u0085b\u2028c", Claimants: []string{"p/a", "q/a"}}},
	}
	want := []string{
		`x\x0awarning: forged line.md › strays: not a valid entity of the type whose layout holds it`,
		`e\x1b[31mRED.md › strays: not a valid entity of the type whose layout holds it`,
		`a\u0085b\u2028c › alias_conflicts: names more than one entity: p/a, q/a`,
	}
	got := []string{}
	for _, f := range report.Findings() {
		got = append(got, f.Line())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lines =\n%q\nwant\n%q", got, want)
	}
}

func TestFindingLine(t *testing.T) {
	report := &CheckReport{Dangling: []Dangling{{Type: "project", Slug: "p", Predicate: "client", Target: "gone"}}}
	if got, want := report.Findings()[0].Line(), "project/p › dangling: client -> 'gone' does not resolve"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func sessionLines(t *testing.T, root, session string) []string {
	t.Helper()
	found, err := SessionWarnings(root, session)
	if err != nil {
		t.Fatalf("SessionWarnings(%s): %v", session, err)
	}
	lines := []string{}
	for _, f := range found {
		lines = append(lines, f.Line())
	}
	return lines
}

// Each session sees standing debt once; a fixed finding that comes back is
// new again.
func TestSessionWarnings(t *testing.T) {
	root := cleanWS(t)
	if got := sessionLines(t, root, "s1"); len(got) != 0 {
		t.Fatalf("clean workspace warned: %v", got)
	}
	create(t, root, "project", "half-pov", "title", "Half", "client", "initech")
	want := "project/half-pov › incomplete: missing owner"

	if got := sessionLines(t, root, "s1"); len(got) != 1 || got[0] != want {
		t.Fatalf("s1 first sight = %v, want [%s]", got, want)
	}
	if got := sessionLines(t, root, "s1"); len(got) != 0 {
		t.Fatalf("s1 repeated a finding it was shown: %v", got)
	}
	if got := sessionLines(t, root, "s2"); len(got) != 1 || got[0] != want {
		t.Fatalf("s2 did not see the standing debt: %v", got)
	}

	link(t, root, "half-pov", "owner", "noor")
	if got := sessionLines(t, root, "s1"); len(got) != 0 {
		t.Fatalf("a fix warned: %v", got)
	}
	if _, err := entity.Unlink(root, "half-pov", "owner", "noor"); err != nil {
		t.Fatal(err)
	}
	if got := sessionLines(t, root, "s1"); len(got) != 1 || got[0] != want {
		t.Fatalf("a reintroduced finding = %v, want [%s]", got, want)
	}

	// The session state lives under .khub/generated/, which no scan reads.
	if report := mustCheck(t, root, false); len(report.Strays) != 0 || len(report.Misplaced) != 0 {
		t.Fatalf("session state surfaced in check: %+v", report)
	}
}

// A record untouched for longer than the TTL is pruned on the next write; a
// recent one and the writing session's own stay.
func TestSessionRecordsPrune(t *testing.T) {
	root := cleanWS(t)
	sessionLines(t, root, "old")
	sessionLines(t, root, "recent")
	dir := SessionDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("records = %v, %v; want 2", entries, err)
	}
	stale := time.Now().Add(-sessionTTL - time.Hour)
	// Age the first record only; which session it is does not matter.
	if err := os.Chtimes(filepath.Join(dir, entries[0].Name()), stale, stale); err != nil {
		t.Fatal(err)
	}
	kept := entries[1].Name()
	sessionLines(t, root, "now")
	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range after {
		names[e.Name()] = true
	}
	if len(after) != 2 || names[entries[0].Name()] || !names[kept] {
		t.Errorf("after prune: %v; want the stale record gone, %s and the new one kept", names, kept)
	}
}
