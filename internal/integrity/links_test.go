package integrity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A dead relative link is a `body` gap on any md body, templated or not:
// reported, never failing validate. Live links, external URLs, anchors and
// links inside fences, comments and code spans are not findings; a link whose
// spelling differs from the file's only in case is dead on every platform.
func TestDeadLinksAreGaps(t *testing.T) {
	root := bodyWS(t, "")
	writeRaw(t, root, "notes/assets/diagram.png", "png")
	createBody(t, root, "note", "target", "Target.\n")
	createBody(t, root, "note", "source",
		"Live: [t](target.md), [t2](./target.md#top), ![d](assets/diagram.png), [dir](assets).\n"+
			"Dead: [gone](gone.md), [case](Target.md), [up](../memos/missing.md#x).\n"+
			"Again: [gone twice](gone.md).\n"+
			"Skipped: [web](https://example.com/gone.md), [anchor](#here), `[code](code.md)`.\n"+
			"<!-- [c](comment.md) -->\n```\n[f](fenced.md)\n```\n")
	createBody(t, root, "memo", "plain", "[old]: ../notes/old.md\n")

	report := mustValidate(t, root, nil, false)
	if !report.OK() {
		t.Fatalf("dead links failed validate: %v", report.Errors)
	}
	want := []string{
		"memo/plain: body: dead link '../notes/old.md' (no such file)",
		"note/source: body: dead link 'gone.md' (no such file)",
		"note/source: body: dead link 'Target.md' (no such file)",
		"note/source: body: dead link '../memos/missing.md#x' (no such file)",
	}
	if got := bodyRows(report.Gaps); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("gaps =\n%v\nwant\n%v", got, want)
	}

	one := mustValidate(t, root, ptr("memo/plain"), false)
	if got := bodyRows(one.Gaps); len(got) != 1 || got[0] != want[0] {
		t.Errorf("targeted gaps = %v, want [%s]", got, want[0])
	}

	// check does not read links: thin stays the body-rule bucket.
	if c := mustCheck(t, root, false); len(c.Thin) != 0 {
		t.Errorf("check thin = %v, want none", c.Thin)
	}
}

// One gap per dead path: spellings that clean to the same path are one link.
func TestDeadLinkDedupesCleanedPath(t *testing.T) {
	root := bodyWS(t, "")
	createBody(t, root, "note", "dup", "[a](gone.md) [b](./gone.md) [c](sub/../gone.md)\n")
	got := bodyRows(mustValidate(t, root, nil, false).Gaps)
	want := []string{"note/dup: body: dead link 'gone.md' (no such file)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("gaps = %v, want %v", got, want)
	}
}

// Bodies are untrusted: whether a gap appears must say nothing about files
// outside the workspace. A link that resolves above the root, or through a
// symlink that leaves it, is skipped whether or not its target exists; a
// symlink that stays inside the root is followed.
func TestDeadLinksNeverProbeOutsideTheWorkspace(t *testing.T) {
	root := bodyWS(t, "")
	outside := t.TempDir()
	writeRaw(t, outside, "exists.txt", "x")
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("notes", filepath.Join(root, "in")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	up, err := filepath.Rel(filepath.Join(root, "notes"), outside)
	if err != nil {
		t.Fatal(err)
	}
	up = filepath.ToSlash(up)
	createBody(t, root, "note", "target", "Target.\n")
	createBody(t, root, "note", "probe",
		"[e]("+up+"/exists.txt) [n]("+up+"/nope.txt) "+
			"[se](../out/exists.txt) [sn](../out/nope.txt) [readme](../../README.md) "+
			"[il](../in/target.md) [id](../in/gone.md)\n")
	got := bodyRows(mustValidate(t, root, nil, false).Gaps)
	want := []string{"note/probe: body: dead link '../in/gone.md' (no such file)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("gaps = %v, want %v", got, want)
	}
}
