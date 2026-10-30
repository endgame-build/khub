package integrity

// Policy `waive`: a waived finding leaves its bucket, stays visible under
// Waived with its reason, and stops counting against Passed; a waiver that
// matches nothing is stale and fails the gate.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// waivedKeys is "<finding> <id>: <reason>" per Waived row, in report order.
func waivedKeys(report *CheckReport) []string {
	out := []string{}
	for _, w := range report.Waived {
		out = append(out, w.Finding+" "+w.ID()+": "+w.Reason)
	}
	return out
}

func TestWaiverMovesFindingsOutOfTheGateAndKeepsThemVisible(t *testing.T) {
	root := gapsWS(t)
	// gapsWS leaves a stray file, which no waiver can cover; drop it so the
	// verdict below turns on the waivers alone.
	if err := os.Remove(filepath.Join(root, "clients", "notes.md")); err != nil {
		t.Fatal(err)
	}
	before := mustCheck(t, root, true)
	if before.Passed() {
		t.Fatalf("the unwaived workspace passed --strict: %+v", before)
	}
	writeRaw(t, root, ".khub/policy.yaml", `policy:
  person:
    waive:
      orphan-person:
        orphans: Alumni record; nothing of ours points at it any more.
  project:
    waive:
      initech-pov:
        dangling: Initech is being re-imported from the CRM this week.
  opportunity:
    waive:
      initech-deal:
        dangling: Same re-import.
`)
	report := mustCheck(t, root, true)
	if len(report.Orphans) != 0 || len(report.Dangling) != 0 {
		t.Fatalf("waived findings stayed in their buckets: orphans=%v dangling=%v",
			report.Orphans, report.Dangling)
	}
	if len(report.StaleWaivers) != 0 {
		t.Fatalf("stale = %+v", report.StaleWaivers)
	}
	if !report.Passed() {
		t.Fatalf("every finding is waived, yet --strict failed: %+v", report)
	}
	want := []string{
		"orphans person/orphan-person: Alumni record; nothing of ours points at it any more.",
		"dangling opportunity/initech-deal: Same re-import.",
		"dangling project/initech-pov: Initech is being re-imported from the CRM this week.",
	}
	got := waivedKeys(report)
	// The dangling rows follow the check's own order; compare as written.
	if len(got) != len(want) {
		t.Fatalf("waived = %v, want %v", got, want)
	}
	for _, w := range want {
		if !containsID(got, w) {
			t.Errorf("waived = %v, missing %q", got, w)
		}
	}
	// The detail is the finding itself, as its bucket would have held it.
	for _, w := range report.Waived {
		if w.Finding == "dangling" {
			d, ok := w.Detail.(Dangling)
			if !ok || d.Target != "initech" || d.ID() != w.ID() {
				t.Errorf("dangling detail = %#v", w.Detail)
			}
		}
	}
}

func TestWaiverThatMatchesNothingIsStaleAndFails(t *testing.T) {
	root := cleanWS(t)
	writeRaw(t, root, ".khub/policy.yaml", `policy:
  person:
    waive:
      noor:
        orphans: Noor is wired in, so this matches nothing.
      ghost:
        incomplete: No such person.
`)
	report := mustCheck(t, root, false)
	if report.Passed() {
		t.Fatal("stale waivers passed the gate")
	}
	got := []string{}
	for _, s := range report.StaleWaivers {
		got = append(got, s.Finding+" "+s.ID())
	}
	if want := []string{"orphans person/noor", "incomplete person/ghost"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stale = %v, want %v (authored order)", got, want)
	}
	if len(report.Waived) != 0 {
		t.Errorf("waived = %v", report.Waived)
	}
}

// An orphan is an orphan whether or not --strict counts it, so its waiver
// matches in both modes and is never stale for want of the flag.
func TestOrphanWaiverMatchesWithoutStrict(t *testing.T) {
	root := cleanWS(t)
	create(t, root, "client", "dormant-co", "name", "Dormant Co")
	writeRaw(t, root, ".khub/policy.yaml",
		"policy:\n  client:\n    waive:\n      dormant-co:\n        orphans: Dormant; engagements archived.\n")
	for _, strict := range []bool{false, true} {
		report := mustCheck(t, root, strict)
		if !report.Passed() || len(report.Orphans) != 0 || len(report.Waived) != 1 {
			t.Errorf("strict=%v: passed=%v orphans=%v waived=%v",
				strict, report.Passed(), report.Orphans, report.Waived)
		}
	}
}

// A waiver covers one bucket: waiving an entity's orphan finding leaves its
// incompleteness reported and failing.
func TestWaiverCoversOnlyItsBucket(t *testing.T) {
	root := cleanWS(t)
	create(t, root, "project", "loose", "client", "initech")
	writeRaw(t, root, ".khub/policy.yaml",
		"policy:\n  project:\n    waive:\n      loose:\n        orphans: unused\n")
	report := mustCheck(t, root, false)
	if _, ok := incompleteByID(report)["project/loose"]; !ok {
		t.Fatalf("incomplete = %v", report.Incomplete)
	}
	if report.Passed() {
		t.Error("an unwaived incomplete entity passed")
	}
	// project/loose links its client, so it is no orphan: the waiver is stale.
	if len(report.StaleWaivers) != 1 {
		t.Errorf("stale = %+v", report.StaleWaivers)
	}
}

// Body findings waive per entity too: one waiver covers every row of its
// bucket on that entity, and a waived body_shape no longer fails the gate.
func TestBodyFindingsWaive(t *testing.T) {
	root := bodyWS(t, ruledTemplate)
	writeRaw(t, root, ".khub/ontology.yaml", strings.Replace(bodySchema, "  note: { orphan: true }\n",
		"  note:\n    orphan: true\n    waive:\n"+
			"      swapped: { body_shape: Imported verbatim from the old wiki., thin: Same import. }\n"+
			"      thin: { thin: Stub kept on purpose. }\n", 1))
	createBody(t, root, "note", "thin", "## Context\n\nToo short.\n\n## Decision\n\nYes.\n")
	createBody(t, root, "note", "swapped", "## Decision\n\nYes.\n\n## Context\n\nlate.\n")

	report := mustCheck(t, root, true)
	if len(report.BodyShape) != 0 || len(report.Thin) != 0 || len(report.StaleWaivers) != 0 {
		t.Fatalf("body_shape=%v thin=%v stale=%v", report.BodyShape, report.Thin, report.StaleWaivers)
	}
	if !report.Passed() {
		t.Fatalf("waived body findings failed the gate: %+v", report)
	}
	want := []string{
		"body_shape note/swapped: Imported verbatim from the old wiki.",
		"thin note/swapped: Same import.",
		"thin note/thin: Stub kept on purpose.",
	}
	if got := waivedKeys(report); !reflect.DeepEqual(got, want) {
		t.Errorf("waived = %v, want %v", got, want)
	}
}

// Drafting an entity must not move the gate. Drafts are exempt from
// required-completeness, so an `incomplete` waiver on a draft matches nothing
// — and is dormant, not stale, until the entity is published again.
func TestIncompleteWaiverIsDormantWhileTheEntityIsADraft(t *testing.T) {
	root := cleanWS(t)
	create(t, root, "project", "loose", "client", "initech")
	writeRaw(t, root, ".khub/policy.yaml",
		"policy:\n  project:\n    waive:\n      loose:\n        incomplete: Owner is being hired.\n")
	published := mustCheck(t, root, false)
	if !published.Passed() || len(published.Waived) != 1 {
		t.Fatalf("published: passed=%v waived=%v stale=%v", published.Passed(), published.Waived, published.StaleWaivers)
	}
	update(t, root, "project/loose", "draft", "true")
	drafted := mustCheck(t, root, false)
	if !drafted.Passed() || len(drafted.StaleWaivers) != 0 || len(drafted.Waived) != 0 {
		t.Fatalf("drafted: passed=%v waived=%v stale=%v", drafted.Passed(), drafted.Waived, drafted.StaleWaivers)
	}
	// Dormancy is for the draft's own completeness only: a waiver naming an
	// entity that does not exist is stale whatever the bucket.
	writeRaw(t, root, ".khub/policy.yaml",
		"policy:\n  project:\n    waive:\n      nobody:\n        incomplete: Gone.\n")
	if r := mustCheck(t, root, false); r.Passed() || len(r.StaleWaivers) != 1 {
		t.Fatalf("missing entity: passed=%v stale=%v", r.Passed(), r.StaleWaivers)
	}
}

// An orphans waiver on an `orphan: true` type can never match; the stale row
// says so rather than "matches no finding".
func TestOrphanWaiverOnExemptTypeSaysWhy(t *testing.T) {
	root := wsFor(t, "build-hub")
	writeRaw(t, root, ".khub/policy.yaml",
		"policy:\n  prd: { required: true, orphan: true }\n  arc42:\n    orphan: true\n    waive:\n      arc42: { orphans: Narrative root. }\n")
	report := mustCheck(t, root, false)
	if len(report.StaleWaivers) != 1 || report.StaleWaivers[0].Cause != StaleOrphanExempt {
		t.Fatalf("stale = %+v", report.StaleWaivers)
	}
	if report.Passed() {
		t.Error("a stale waiver passed the gate")
	}
}
