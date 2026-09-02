package entity

// Port of the library-level assertions in tests/test_entity_edit.py
// (TS-ENT-003 — Edit an Entity).

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
)

func editPrereqs(t *testing.T, ws string) {
	t.Helper()
	seed(t, ws, "clients/initech.md", kv("type", "client", "name", "Initech"))
	seed(t, ws, "identity/team/noor.md", kv("type", "person", "name", "Noor", "role", "partner"))
}

// deal seeds the opportunity every edit scenario mutates.
func deal(t *testing.T, ws string, over ...any) string {
	t.Helper()
	meta := kv(
		"type", "opportunity",
		"created", "2026-01-01",
		"updated", "2026-01-01",
		"draft", false,
		"stage", "prospect",
		"client", "initech",
		"owner", "noor",
	)
	for i := 0; i+1 < len(over); i += 2 {
		meta.Set(over[i].(string), over[i+1])
	}
	seed(t, ws, "opportunities/initech-deal/_index.md", meta)
	return filepath.Join(ws, "opportunities", "initech-deal", "_index.md")
}

// changedKeys names the frontmatter keys whose lines moved between two renders.
func changedKeys(before, after string) []string {
	set := map[string]bool{}
	bl := strings.Split(before, "\n")
	al := strings.Split(after, "\n")
	n := len(bl)
	if len(al) < n {
		n = len(al)
	}
	for i := 0; i < n; i++ {
		if bl[i] == al[i] {
			continue
		}
		line := al[i]
		if line == "" {
			line = bl[i]
		}
		set[strings.TrimSpace(strings.SplitN(line, ":", 2)[0])] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TS-ENT-003-U01: an out-of-enum edit raises and writes nothing.
func TestEnumRevalidationLeavesFileUnchanged(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws)
	before := readFile(t, path)

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("stage", "banana")})
	requireCode(t, err, "enum_violation")
	if readFile(t, path) != before {
		t.Fatal("a refused edit rewrote the file")
	}
}

// TS-ENT-003-U02: a successful edit bumps updated to today.
func TestUpdatedIsBumped(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws)
	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("stage", "won")})
	requireNoError(t, err)
	if got := metaValue(t, path, "updated"); got != (canon.Date{ISO: today().ISO}) {
		t.Fatalf("updated = %#v, want %s", got, today().ISO)
	}
}

// commentedDeal is the hand-authored, comment-bearing frontmatter the
// minimal-diff assertions edit.
const commentedDeal = "---\n" +
	"type: opportunity\n" +
	"created: 2026-01-01\n" +
	"updated: 2026-01-01\n" +
	"draft: false\n" +
	"stage: prospect  # current pipeline stage\n" +
	"client: initech\n" +
	"owner: noor\n" +
	"---\n"

// TS-ENT-003-U03 (key-order half): a round-trip edit confines the diff to the
// keys it touched and leaves every other line byte-identical.
func TestMinimalDiffPreservesOrder(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := filepath.Join(ws, "opportunities", "initech-deal", "_index.md")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, commentedDeal)

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("stage", "won")})
	requireNoError(t, err)
	after := readFile(t, path)
	if got := changedKeys(commentedDeal, after); !equalStrings(got, []string{"stage", "updated"}) {
		t.Fatalf("changed keys = %v, want [stage updated]", got)
	}
	if got := readMeta(t, path).Keys(); !equalStrings(got,
		[]string{"type", "created", "updated", "draft", "stage", "client", "owner"}) {
		t.Fatalf("key order moved: %v", got)
	}
}

// TS-ENT-003-U03 (comment half): ruamel's round-trip keeps an inline comment
// across an edit, and so does canon's splice write path (canon/splice.go).
// Python re-seats the comment at its recorded column, so the shortened value
// pads out to it.
func TestMinimalDiffPreservesComments(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := filepath.Join(ws, "opportunities", "initech-deal", "_index.md")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, commentedDeal)

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("stage", "won")})
	requireNoError(t, err)
	if after := readFile(t, path); !strings.Contains(after, "# current pipeline stage") {
		t.Fatalf("the comment vanished:\n%s", after)
	}
}

// TS-ENT-003-U03: a create-authored file edits to a 2-line diff and emits no
// YAML anchors.
func TestCreateThenEditIsMinimalDiff(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	res, err := Create(ws, "opportunity", CreateOpts{
		Fields:      fields("name", "Deal", "client", "initech", "owner", "noor", "stage", "prospect"),
		UseTemplate: true})
	requireNoError(t, err)
	before := readFile(t, res.Path)
	if strings.Contains(before, "&id") || strings.Contains(before, "*id") {
		t.Fatalf("created/updated aliased:\n%s", before)
	}
	_, err = Update(ws, res.Slug, UpdateOpts{Fields: fields("stage", "won")})
	requireNoError(t, err)
	changed := changedKeys(before, readFile(t, res.Path))
	if !contains(changed, "stage") {
		t.Fatalf("stage did not change: %v", changed)
	}
	for _, k := range changed {
		if k != "stage" && k != "updated" {
			t.Fatalf("unexpected changed key %q (all: %v)", k, changed)
		}
	}
}

// TS-ENT-003-U04: a field edit never flips draft — no auto-promote.
func TestFieldEditDoesNotTouchDraft(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws, "draft", true)
	res, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("stage", "won")})
	requireNoError(t, err)
	if !res.Draft {
		t.Fatal("UpdateResult.Draft flipped")
	}
	if got := metaValue(t, path, "draft"); got != true {
		t.Fatalf("draft = %#v, want true", got)
	}
}

// TS-ENT-003-U04: `edit <id> draft true|false` sets the flag by hand.
func TestEditTogglesDraft(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws, "draft", false)

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("draft", "true")})
	requireNoError(t, err)
	if got := metaValue(t, path, "draft"); got != true {
		t.Fatalf("draft = %#v after true", got)
	}
	_, err = Update(ws, "initech-deal", UpdateOpts{Fields: fields("draft", "false")})
	requireNoError(t, err)
	if got := metaValue(t, path, "draft"); got != false {
		t.Fatalf("draft = %#v after false", got)
	}
}

// TS-ENT-003-U05: --strict rejects an undeclared field; otherwise it is preserved.
func TestStrictEditorRejectsUnknown(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws)

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("vibe", "high"), Strict: true})
	requireCode(t, err, "strict_unknown_field")

	_, err = Update(ws, "initech-deal", UpdateOpts{Fields: fields("vibe", "high")})
	requireNoError(t, err)
	if got := metaValue(t, path, "vibe"); got != "high" {
		t.Fatalf("vibe = %#v", got)
	}
}

// `edit --body` replaces the prose and normalizes its trailing newline; an
// omitted body leaves the prose byte-for-byte.
func TestEditBodyReplacesAndKeeps(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws)
	writeFile(t, path, strings.TrimSuffix(readFile(t, path), "")+"original prose\n")

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("stage", "won")})
	requireNoError(t, err)
	if !strings.HasSuffix(readFile(t, path), "original prose\n") {
		t.Fatal("a metadata-only edit moved the body")
	}
	replacement := "new prose"
	_, err = Update(ws, "initech-deal", UpdateOpts{Fields: fields(), Body: &replacement})
	requireNoError(t, err)
	if !strings.HasSuffix(readFile(t, path), "new prose\n") {
		t.Fatalf("body not replaced:\n%s", readFile(t, path))
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
