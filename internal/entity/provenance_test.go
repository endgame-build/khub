package entity

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

func TestActorLadder(t *testing.T) {
	for _, c := range []struct{ explicit, env, claude, want string }{
		{"human:ilya", "process:ci", "1", "human:ilya"},
		{"", "process:ci", "1", "process:ci"},
		{"", "", "1", "agent/claude-code"},
		{"", "", "", "khub"},
	} {
		t.Setenv("KHUB_ACTOR", c.env)
		t.Setenv("CLAUDECODE", c.claude)
		if got := Actor(c.explicit); got != c.want {
			t.Errorf("Actor(%q) with KHUB_ACTOR=%q CLAUDECODE=%q = %q, want %q",
				c.explicit, c.env, c.claude, got, c.want)
		}
	}
}

// add, edit, and a link that changes something stamp; a no-op link does not.
func TestWritesStampGenerated(t *testing.T) {
	ws := newWSFrom(t, "nav", writePreset(t, "nav", navigationPreset, nil))
	res, err := Create(ws, "note", CreateOpts{ID: "a", Fields: fields("title", "A"), Actor: "human:ilya", UseTemplate: true})
	requireNoError(t, err)
	_, err = Create(ws, "note", CreateOpts{ID: "b", Fields: fields("title", "B"), UseTemplate: true})
	requireNoError(t, err)
	by := func() any {
		g, _ := readMeta(t, res.Path).Get("generated")
		v, _ := g.(interface{ Get(string) (any, bool) }).Get("by")
		return v
	}
	if got := by(); got != "human:ilya" {
		t.Fatalf("add stamped %v", got)
	}
	if _, err := Link(ws, "note/a", "related", "note/b", "", false); err != nil {
		t.Fatal(err)
	}
	if got := by(); got != "khub" {
		t.Fatalf("link stamped %v", got)
	}
	before := readFile(t, res.Path)
	if r, err := Link(ws, "note/a", "related", "note/b", "process:ci", false); err != nil || r.Changed {
		t.Fatalf("no-op link: %v %v", r, err)
	}
	if readFile(t, res.Path) != before {
		t.Fatal("a no-op link stamped the file")
	}
	if _, err := Update(ws, "note/a", UpdateOpts{Fields: fields("title", "A2"), Actor: "process:ci"}); err != nil {
		t.Fatal(err)
	}
	if got := by(); got != "process:ci" {
		t.Fatalf("edit stamped %v", got)
	}
}

// verify appends {by, at, rev}, turning a bare mapping into a list; it bumps
// no `updated` and stamps no `generated`. rev ignores a tag edit and moves
// with a body edit.
func TestVerifyAppendsAReviewEvent(t *testing.T) {
	ws := newWSFrom(t, "nav", writePreset(t, "nav", navigationPreset, nil))
	path := filepath.Join(ws, "notes", "a.md")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, "---\ntype: note\ntitle: A\ncreated: 2026-01-01\nupdated: 2026-01-01\n"+
		"verified: {by: human:old, at: 2026-01-01T00:00:00Z}\n---\nBody.\n")
	first, err := Verify(ws, "note/a", "human:ilya", "", nil)
	requireNoError(t, err)
	meta := readMeta(t, path)
	v, _ := meta.Get("verified")
	if list, ok := v.([]any); !ok || len(list) != 2 {
		t.Fatalf("verified = %#v, want the bare mapping plus the new event", v)
	}
	_, stamped := meta.Get("generated")
	if u, _ := meta.Get("updated"); values.Str(u) != "2026-01-01" || stamped {
		t.Fatalf("verify wrote content bookkeeping:\n%s", readFile(t, path))
	}
	if first.At != "2026-01-15T00:00:00Z" || len(first.Rev) != 12 {
		t.Fatalf("event = %+v", first)
	}

	_, err = Update(ws, "note/a", UpdateOpts{Fields: fields("tags", "x")})
	requireNoError(t, err)
	if again, _ := Verify(ws, "note/a", "process:ci", "2026-01-16T09:00:00+02:00", nil); again.Rev != first.Rev {
		t.Errorf("a tag edit moved rev: %s -> %s", first.Rev, again.Rev)
	}
	body := "Changed."
	_, err = Update(ws, "note/a", UpdateOpts{Body: &body})
	requireNoError(t, err)
	if moved, _ := Verify(ws, "note/a", "process:ci", "", nil); moved.Rev == first.Rev {
		t.Error("a body edit left rev unchanged")
	}

	_, err = Verify(ws, "note/a", "human:ilya", "2026-01-16 09:00:00Z", nil)
	_ = requireCode(t, err, "pattern_violation")
}

// A publish that fails leaves the entity as it was: verify --commit is all or
// nothing.
func TestVerifyRestoresTheFileWhenPublishFails(t *testing.T) {
	ws := newWSFrom(t, "nav", writePreset(t, "nav", navigationPreset, nil))
	res, err := Create(ws, "note", CreateOpts{ID: "a", Fields: fields("title", "A"), UseTemplate: true})
	requireNoError(t, err)
	before := readFile(t, res.Path)
	_, err = Verify(ws, "note/a", "human:ilya", "", func(string) error { return errors.New("no signature") })
	if err == nil || err.Error() != "no signature" {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, res.Path) != before {
		t.Fatalf("a failed publish left the event written:\n%s", readFile(t, res.Path))
	}
}

// Keys an event carries beyond {by, at, rev} — the lens verdict keys among
// them — survive every write that rewrites the file: edit, link, verify and
// AppendLensEvents keep each existing event byte for byte.
func TestVerifiedEventExtraKeysSurviveWrites(t *testing.T) {
	ws := newWSFrom(t, "nav", writePreset(t, "nav", navigationPreset, nil))
	_, err := Create(ws, "note", CreateOpts{ID: "b", Fields: fields("title", "B"), UseTemplate: true})
	requireNoError(t, err)
	path := filepath.Join(ws, "notes", "a.md")
	mkdirAll(t, filepath.Dir(path))
	events := "verified:\n" +
		"- by: human:ana\n  at: 2026-01-01T00:00:00Z\n  rev: 0123456789ab\n" +
		"  lens: single\n  verdict: fail\n  q: ba9876543210\n  note: Two rules in one.\n" +
		"- by: process:ci\n  at: 2026-01-02T00:00:00Z\n  custom: {kept: true}\n"
	writeFile(t, path, "---\ntype: note\ntitle: A\ncreated: 2026-01-01\nupdated: 2026-01-01\n"+events+"---\nBody.\n")
	contains := func(step string) {
		t.Helper()
		if text := readFile(t, path); !strings.Contains(text, events) {
			t.Fatalf("after %s the events moved:\n%s", step, text)
		}
	}
	_, err = Update(ws, "note/a", UpdateOpts{Fields: fields("title", "A2")})
	requireNoError(t, err)
	contains("edit")
	body := "Changed."
	_, err = Update(ws, "note/a", UpdateOpts{Body: &body})
	requireNoError(t, err)
	contains("edit --body")
	_, err = Link(ws, "note/a", "related", "note/b", "", false)
	requireNoError(t, err)
	contains("link")
	_, err = Verify(ws, "note/a", "human:ilya", "", nil)
	requireNoError(t, err)
	contains("verify")
	_, err = AppendLensEvents(ws, false, func(_ *schema.ResolvedSchema, idx *index.Index) ([]LensEvent, error) {
		return []LensEvent{{Node: index.Node{Type: "note", Slug: "a"}, Event: kv("by", "human:ana", "lens", "x")}}, nil
	})
	requireNoError(t, err)
	contains("a lens append")
}

// A refused plan writes nothing, and a dry run writes nothing either.
func TestAppendLensEventsIsAllOrNothing(t *testing.T) {
	ws := newWSFrom(t, "nav", writePreset(t, "nav", navigationPreset, nil))
	res, err := Create(ws, "note", CreateOpts{ID: "a", Fields: fields("title", "A"), UseTemplate: true})
	requireNoError(t, err)
	before := readFile(t, res.Path)
	node := index.Node{Type: "note", Slug: "a"}
	_, err = AppendLensEvents(ws, false, func(*schema.ResolvedSchema, *index.Index) ([]LensEvent, error) {
		return nil, errors.New("refused")
	})
	if err == nil || err.Error() != "refused" {
		t.Fatalf("err = %v", err)
	}
	got, err := AppendLensEvents(ws, true, func(*schema.ResolvedSchema, *index.Index) ([]LensEvent, error) {
		return []LensEvent{{Node: node, Event: kv("by", "human:ana", "lens", "x")}}, nil
	})
	requireNoError(t, err)
	if len(got) != 1 || readFile(t, res.Path) != before {
		t.Fatalf("dry run: %v\n%s", got, readFile(t, res.Path))
	}
}
