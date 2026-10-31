package migrate_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/migrate"
	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/workspace"
)

// A preset that holds every legacy shape: a type whose `decision` enum holds
// the retired type-level status values, types inheriting the base status, a
// type that redeclares status with its own vocabulary, and collections in all
// three formats.
const ontology = `
version: "0.1.0"
ontology:
  entities:
    note: {}
    adr:
      attributes:
        decision: { enum: [proposed, accepted, rejected] }
    svc: {}
    keep:
      attributes:
        status: { enum: [active, retired] }
    old:
      attributes:
        status: { enum: [proposed, active, deprecated] }
    flag:
      attributes:
        draft: { type: bool }
    row: {}
    jrow: {}
    lrow: {}
`

const storage = `
storage:
  note: { layout: file, path: notes }
  adr:  { layout: file, path: decisions }
  svc:  { layout: file, path: svcs, format: yaml }
  keep: { layout: file, path: keeps }
  old:  { layout: file, path: olds }
  flag: { layout: file, path: flags }
  row:  { layout: collection, format: yaml, path: rows.yaml }
  jrow: { layout: collection, format: json, path: jrows.json }
  lrow: { layout: collection, format: jsonl, path: lrows.jsonl }
`

func newWS(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	write(t, src, "legacy/ontology.yaml", ontology)
	write(t, src, "legacy/storage.yaml", storage)
	root := t.TempDir()
	if _, err := workspace.Init("legacy", root, workspace.InitOptions{PresetSource: src}); err != nil {
		t.Fatalf("init: %v", err)
	}
	return root
}

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func run(t *testing.T, root string) *migrate.Result {
	t.Helper()
	res, err := migrate.Run(root, fsio.AtomicWriteIn)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return res
}

func entity(t *testing.T, res *migrate.Result, id string) migrate.Entity {
	t.Helper()
	for _, e := range res.Migrated {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("%s not migrated; got %+v", id, res.Migrated)
	return migrate.Entity{}
}

func TestDraftTrueBecomesStatusDraftAndSourcesMove(t *testing.T) {
	root := newWS(t)
	write(t, root, "notes/a.md", "---\ntype: note\ntitle: A   # hand comment\ndraft: true\n"+
		"sources:\n- other\n- path/to/file.md\n---\nBody.\n")
	res := run(t, root)
	want := "---\ntype: note\ntitle: A   # hand comment\nstatus: draft\n" +
		"references:\n- other\n- path/to/file.md\n---\nBody.\n"
	if got := read(t, root, "notes/a.md"); got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
	e := entity(t, res, "note/a")
	wantChanges := []migrate.Change{
		{Op: migrate.OpRenameKey, Field: "draft", From: "draft", To: "status"},
		{Op: migrate.OpMapValue, Field: "status", From: true, To: "draft"},
		{Op: migrate.OpMoveEntries, Field: "sources", From: []any{"other", "path/to/file.md"}, To: "references"},
	}
	if !reflect.DeepEqual(e.Changes, wantChanges) || e.Reemitted {
		t.Fatalf("got %+v", e)
	}
}

func TestDraftFalseIsDroppedAndMixedSourcesSplit(t *testing.T) {
	root := newWS(t)
	write(t, root, "notes/b.md", "---\n# header\ntype: note\ndraft: false   # retired\n"+
		"sources:  # where from\n- other\n- resource: https://x.example\nreferences:\n- other\n- more\n---\n")
	res := run(t, root)
	// The changed `sources` list has its own lines rewritten and keeps the
	// comment on its key line; `references` already held every bare entry.
	want := "---\n# header\ntype: note\n" +
		"sources:  # where from\n- resource: https://x.example\nreferences:\n- other\n- more\n---\n"
	if got := read(t, root, "notes/b.md"); got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
	if e := entity(t, res, "note/b"); e.Reemitted || len(e.Changes) != 2 {
		t.Fatalf("got %+v", e)
	}
}

func TestStatusMovesToItsFieldOrMapsOntoTheBaseEnum(t *testing.T) {
	root := newWS(t)
	write(t, root, "decisions/x.md", "---\ntype: adr\nstatus: accepted  # the call\ndraft: true\n---\n")
	write(t, root, "decisions/y.md", "---\ntype: adr\nstatus: proposed\n---\n")
	write(t, root, "svcs/s.yaml", "type: svc\nstatus: active  # live\nbody: prose\n")
	write(t, root, "keeps/k.md", "---\ntype: keep\nstatus: active\ndraft: false\n---\n")
	res := run(t, root)

	if got, want := read(t, root, "decisions/x.md"),
		"---\ntype: adr\ndecision: accepted  # the call\nstatus: draft\n---\n"; got != want {
		t.Fatalf("adr x\n got: %q\nwant: %q", got, want)
	}
	// proposed is a decision value, so the decision enum wins over the map.
	if got, want := read(t, root, "decisions/y.md"), "---\ntype: adr\ndecision: proposed\n---\n"; got != want {
		t.Fatalf("adr y\n got: %q\nwant: %q", got, want)
	}
	if got, want := read(t, root, "svcs/s.yaml"), "type: svc\nstatus: stable  # live\nbody: prose\n"; got != want {
		t.Fatalf("svc\n got: %q\nwant: %q", got, want)
	}
	// A redeclared status keeps its own vocabulary; only the draft flag goes.
	if got, want := read(t, root, "keeps/k.md"), "---\ntype: keep\nstatus: active\n---\n"; got != want {
		t.Fatalf("keep\n got: %q\nwant: %q", got, want)
	}
	if e := entity(t, res, "svc/s"); !reflect.DeepEqual(e.Changes,
		[]migrate.Change{{Op: migrate.OpMapValue, Field: "status", From: "active", To: "stable"}}) {
		t.Fatalf("svc changes %+v", e.Changes)
	}
}

func TestConflictLeavesTheEntityUnchanged(t *testing.T) {
	root := newWS(t)
	src := "---\ntype: note\nstatus: stable\ndraft: true\nsources:\n- other\n---\n"
	write(t, root, "notes/c.md", src)
	res := run(t, root)
	if got := read(t, root, "notes/c.md"); got != src {
		t.Fatalf("conflicted file rewritten:\n%s", got)
	}
	want := []migrate.Conflict{{ID: "note/c", Path: "notes/c.md", Field: "draft",
		Reason: "draft is true but status is 'stable'; settle it by hand"}}
	if !reflect.DeepEqual(res.Conflicts, want) || len(res.Migrated) != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestCollections(t *testing.T) {
	root := newWS(t)
	write(t, root, "rows.yaml", "# header\nr1:\n  type: row\n  draft: true   # row flag\n"+
		"# between rows\nr2:\n  type: row\n  status: proposed\n  draft: false\n# trailing\n")
	write(t, root, "jrows.json", "{\n  \"j1\": {\n    \"type\": \"jrow\",\n    \"draft\": true\n  }\n}\n")
	write(t, root, "lrows.jsonl", "{\"slug\": \"l1\", \"type\": \"lrow\", \"status\": \"archived\"}\n")
	res := run(t, root)

	if got, want := read(t, root, "rows.yaml"), "# header\nr1:\n  type: row\n  status: draft  # row flag\n"+
		"# between rows\nr2:\n  type: row\n  status: draft\n# trailing\n"; got != want {
		t.Fatalf("yaml rows\n got: %q\nwant: %q", got, want)
	}
	rows, err := canon.LoadCollection(read(t, root, "jrows.json"), "json")
	if err != nil {
		t.Fatal(err)
	}
	j1, _ := rows.Get("j1")
	if status, _ := j1.(interface{ Get(string) (any, bool) }).Get("status"); status != "draft" {
		t.Fatalf("json row status = %#v", status)
	}
	rows, err = canon.LoadCollection(read(t, root, "lrows.jsonl"), "jsonl")
	if err != nil {
		t.Fatal(err)
	}
	l1, _ := rows.Get("l1")
	if status, _ := l1.(interface{ Get(string) (any, bool) }).Get("status"); status != "deprecated" {
		t.Fatalf("jsonl row status = %#v", status)
	}
	for _, id := range []string{"row/r1", "row/r2", "jrow/j1", "lrow/l1"} {
		// yaml splices; json and jsonl carry no comments to lose.
		if e := entity(t, res, id); e.Reemitted {
			t.Fatalf("%s reported re-emitted", id)
		}
	}
}

// Every op detects its own shape, so a second run finds nothing to do.
func TestSecondRunChangesNothing(t *testing.T) {
	root := newWS(t)
	write(t, root, "notes/a.md", "---\ntype: note\ndraft: true\nsources:\n- other\n---\n")
	write(t, root, "decisions/x.md", "---\ntype: adr\nstatus: accepted\ndraft: false\n---\n")
	write(t, root, "rows.yaml", "r1:\n  type: row\n  status: active\n")
	if first := run(t, root); len(first.Migrated) != 3 {
		t.Fatalf("first run: %+v", first.Migrated)
	}
	before := map[string]string{}
	for _, rel := range []string{"notes/a.md", "decisions/x.md", "rows.yaml"} {
		before[rel] = read(t, root, rel)
	}
	second := run(t, root)
	if len(second.Migrated) != 0 || len(second.Conflicts) != 0 {
		t.Fatalf("second run: %+v", second)
	}
	for rel, text := range before {
		if read(t, root, rel) != text {
			t.Fatalf("%s moved on the second run", rel)
		}
	}
}

// A retired status that maps onto the lifecycle gives way to a true draft
// flag: the entity was unpublished, so it becomes `status: draft`, and the
// changes record the value it dropped.
func TestDraftWinsOverARetiredStatus(t *testing.T) {
	root := newWS(t)
	write(t, root, "svcs/s.yaml", "type: svc\nstatus: active  # live\ndraft: true\n")
	res := run(t, root)
	if got, want := read(t, root, "svcs/s.yaml"), "type: svc\nstatus: draft   # live\n"; got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
	want := []migrate.Change{
		{Op: migrate.OpMapValue, Field: "status", From: "active", To: "draft"},
		{Op: migrate.OpDeleteKey, Field: "draft", From: true},
	}
	if e := entity(t, res, "svc/s"); !reflect.DeepEqual(e.Changes, want) {
		t.Fatalf("got %+v", e.Changes)
	}
	// A lifecycle status already written disagrees with the flag: a conflict.
	write(t, root, "svcs/t.yaml", "type: svc\nstatus: deprecated\ndraft: true\n")
	if res := run(t, root); len(res.Conflicts) != 1 || res.Conflicts[0].ID != "svc/t" {
		t.Fatalf("got %+v", res)
	}
}

// A type whose status still holds a retired enum (old layer files kept by
// --no-schema) cannot take `status: draft`: the entity stays and the conflict
// names the full upgrade. A false flag still goes.
func TestDraftWaitsForAStatusThatAcceptsIt(t *testing.T) {
	root := newWS(t)
	src := "---\ntype: old\ndraft: true\n---\n"
	write(t, root, "olds/o.md", src)
	write(t, root, "olds/p.md", "---\ntype: old\nstatus: active\ndraft: false\n---\n")
	res := run(t, root)
	if got := read(t, root, "olds/o.md"); got != src {
		t.Fatalf("rewritten:\n%s", got)
	}
	if len(res.Conflicts) != 1 || !strings.Contains(res.Conflicts[0].Reason, "without --no-schema") {
		t.Fatalf("conflicts %+v", res.Conflicts)
	}
	if got, want := read(t, root, "olds/p.md"), "---\ntype: old\nstatus: active\n---\n"; got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A type that declares `draft` owns it: never migrated, never the publish flag.
func TestAnOwnDraftFieldIsDomainData(t *testing.T) {
	root := newWS(t)
	src := "---\ntype: flag\ndraft: true\n---\n"
	write(t, root, "flags/f.md", src)
	write(t, root, "notes/n.md", "---\ntype: note\ndraft: true\n---\n")
	got, err := query.Query(root, query.Filters{DraftOnly: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "note" {
		t.Fatalf("drafts = %+v", got)
	}
	if res := run(t, root); len(res.Conflicts) != 0 || read(t, root, "flags/f.md") != src {
		t.Fatalf("own draft migrated: %+v", res)
	}
}

// A file the scan read but the edit-altitude reader refuses is a conflict,
// not a failed upgrade.
func TestUnreadableFileIsAConflict(t *testing.T) {
	root := newWS(t)
	src := "---\ntype: note\ndraft: true\n----\nBody.\n"
	write(t, root, "notes/u.md", src)
	res := run(t, root)
	if len(res.Conflicts) != 1 || res.Conflicts[0].ID != "note/u" ||
		!strings.HasPrefix(res.Conflicts[0].Reason, "unreadable: ") {
		t.Fatalf("got %+v", res.Conflicts)
	}
	if read(t, root, "notes/u.md") != src {
		t.Fatal("unreadable file rewritten")
	}
}

// A date-only `stale_after` (OKF 0.1), unquoted or quoted, becomes midnight
// UTC that day; a datetime already there is left alone, and a second run
// changes nothing.
func TestDateOnlyStaleAfterWidensToADatetime(t *testing.T) {
	root := newWS(t)
	write(t, root, "notes/d.md", "---\ntype: note\nstale_after: 2026-12-31  # review by\n---\n")
	write(t, root, "notes/q.md", "---\ntype: note\nstale_after: '2026-12-31'\n---\n")
	write(t, root, "notes/k.md", "---\ntype: note\nstale_after: 2026-12-31T09:00:00+02:00\n---\n")
	res := run(t, root)
	if got, want := read(t, root, "notes/d.md"),
		"---\ntype: note\nstale_after: 2026-12-31T00:00:00Z # review by\n---\n"; got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
	if got, want := read(t, root, "notes/q.md"),
		"---\ntype: note\nstale_after: 2026-12-31T00:00:00Z\n---\n"; got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
	if e := entity(t, res, "note/d"); !reflect.DeepEqual(e.Changes, []migrate.Change{{Op: migrate.OpMapValue,
		Field: "stale_after", From: canon.Date{ISO: "2026-12-31"}, To: canon.DateTime{ISO: "2026-12-31T00:00:00Z"}}}) {
		t.Fatalf("changes %+v", e.Changes)
	}
	if len(res.Migrated) != 2 {
		t.Fatalf("migrated %+v", res.Migrated)
	}
	if again := run(t, root); len(again.Migrated) != 0 {
		t.Fatalf("second run %+v", again.Migrated)
	}
}
