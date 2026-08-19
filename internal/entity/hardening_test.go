package entity

// Port of tests/test_entity_hardening.py — one focused test per non-trivial
// write-gate fix: scalar-many corruption, the type discriminator lock, slug
// length cap, explicit-id collision refusal, comma-list on a single-valued
// relation, date/bool write gates, BOM/blank-lead editability, title slug
// fallback, self-link and ambiguous-target refusal, backdating, and the
// LinkResult Changed contract.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
)

func people(t *testing.T, ws string) {
	t.Helper()
	seed(t, ws, "identity/team/ann.md", kv("type", "person", "name", "Ann", "role", "consultant"))
	seed(t, ws, "identity/team/bob.md", kv("type", "person", "name", "Bob", "role", "engineer"))
}

// fix 1: a many-relation stored as a scalar gains a value as a list, never
// char-split.
func TestScalarManyLinkNoCharSplit(t *testing.T) {
	ws := newWS(t, "firm-ops")
	people(t, ws)
	seed(t, ws, "fragments/note.md",
		kv("type", "fragment", "stage", "raw", "owner", "ann", "related", "ann"))

	res, err := Link(ws, "note", "related", "bob")
	requireNoError(t, err)
	if !res.Changed {
		t.Fatal("link reported no change")
	}
	got := metaValue(t, filepath.Join(ws, "fragments", "note.md"), "related")
	list, ok := got.([]any)
	if !ok || len(list) != 2 || list[0] != "ann" || list[1] != "bob" {
		t.Fatalf("related = %#v, want [ann bob]", got)
	}
}

// fix 1: unlinking the lone value of a scalar-stored many-relation removes it
// cleanly — no character survivors.
func TestScalarManyUnlinkClean(t *testing.T) {
	ws := newWS(t, "firm-ops")
	people(t, ws)
	seed(t, ws, "fragments/note.md",
		kv("type", "fragment", "stage", "raw", "owner", "ann", "related", "ann"))

	res, err := Unlink(ws, "note", "related", "ann")
	requireNoError(t, err)
	if !res.Changed {
		t.Fatal("unlink reported no change")
	}
	if hasKey(t, filepath.Join(ws, "fragments", "note.md"), "related") {
		t.Fatal("the key survived")
	}
}

// fix 2: a user `type` field disagreeing with the layout type is refused,
// writing nothing.
func TestUserTypeFieldRejected(t *testing.T) {
	ws := newWS(t, "firm-ops")
	before := mdFiles(t, ws)
	_, err := Create(ws, "client", CreateOpts{
		Fields: fields("name", "Widgets", "type", "person"), UseTemplate: true})
	e := requireCode(t, err, "type_field_forbidden")
	requireMessageContains(t, e,
		"The 'type' field is set by the command (client); it cannot be overridden")
	if !equalStrings(mdFiles(t, ws), before) {
		t.Fatal("a refused create wrote a file")
	}
}

// fix 3: a ~300-char --id raises a located error, not an OSError at write.
func TestOverlongIDRejectedCleanly(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Create(ws, "client", CreateOpts{
		Fields: fields("name", "Widgets"), ID: strings.Repeat("a", 300), UseTemplate: true})
	e := requireCode(t, err, "invalid_slug")
	requireMessageContains(t, e, "exceeds 100 characters")
}

// fix 5: a comma-list on a single-valued relation refuses instead of dropping
// its tail.
func TestCommaOnSingleRelationRaises(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "clients/initech.md", kv("type", "client", "name", "Initech"))
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	seed(t, ws, "identity/team/noor.md", kv("type", "person", "name", "Noor", "role", "partner"))
	before := mdFiles(t, ws)

	_, err := Create(ws, "opportunity", CreateOpts{
		Fields:      fields("client", "initech,acme", "owner", "noor", "stage", "prospect"),
		UseTemplate: true})
	requireCode(t, err, "cardinality_violation")
	if !equalStrings(mdFiles(t, ws), before) {
		t.Fatal("a refused create wrote a file")
	}
}

// fix 6: a within-type explicit --id collision refuses (no silent auto-suffix).
func TestExplicitIDCollisionRaises(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Acme"), ID: "acme", UseTemplate: true})
	requireNoError(t, err)
	_, err = Create(ws, "client", CreateOpts{Fields: fields("name", "Acme"), ID: "acme", UseTemplate: true})
	e := requireCode(t, err, "slug_taken")
	requireMessageContains(t, e, "Slug 'acme' is already taken in client; choose another --id")
	if _, statErr := os.Stat(filepath.Join(ws, "clients", "acme-2.md")); statErr == nil {
		t.Fatal("the refused create auto-suffixed")
	}
}

// fix 7: an impossible date is refused at write time (mirrors the number gate).
func TestBadDateRejected(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws)
	before := readFile(t, path)

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("created", "2026-13-45")})
	e := requireCode(t, err, "date_violation")
	requireMessageContains(t, e, "'2026-13-45' is not a valid date for created")
	if readFile(t, path) != before {
		t.Fatal("a refused edit rewrote the file")
	}
}

// fix 7: a valid ISO date passes the write gate and stores unquoted.
func TestGoodDateAccepted(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws)
	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("created", "2026-02-02")})
	requireNoError(t, err)
	if got := metaValue(t, path, "created"); got != (canon.Date{ISO: "2026-02-02"}) {
		t.Fatalf("created = %#v", got)
	}
	if !strings.Contains(readFile(t, path), "created: 2026-02-02\n") {
		t.Fatalf("the date serialized quoted:\n%s", readFile(t, path))
	}
}

// fix 8: a non-BOOLISH bool value is refused, not silently coerced to False.
func TestBadBoolRejected(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws)
	before := readFile(t, path)

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("draft", "banana")})
	e := requireCode(t, err, "bool_violation")
	requireMessageContains(t, e,
		"'banana' is not a valid boolean (true/false, yes/no, 1/0, on/off)")
	if readFile(t, path) != before {
		t.Fatal("a refused edit rewrote the file")
	}
}

// fix 8: a BOOLISH string parses to the right bool (yes → True).
func TestBoolishValueAccepted(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws, "draft", false)
	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("draft", "yes")})
	requireNoError(t, err)
	if got := metaValue(t, path, "draft"); got != true {
		t.Fatalf("draft = %#v, want true", got)
	}
}

// fix 9: a BOM or leading blank lines no longer make an otherwise-valid file
// un-editable; a genuinely missing fence still fails strictly.
func TestSplitFrontmatterTolerance(t *testing.T) {
	yamlBOM, bodyBOM, err := canon.SplitFrontmatter(
		"\uFEFF---\ntype: fragment\nstage: raw\n---\nnote\n")
	requireNoError(t, err)
	if !strings.Contains(yamlBOM, "type: fragment") || strings.TrimSpace(bodyBOM) != "note" {
		t.Fatalf("BOM split = %q / %q", yamlBOM, bodyBOM)
	}
	yamlBlank, _, err := canon.SplitFrontmatter("\n\n---\ntype: fragment\nstage: raw\n---\nnote\n")
	requireNoError(t, err)
	if !strings.Contains(yamlBlank, "type: fragment") {
		t.Fatalf("blank-lead split = %q", yamlBlank)
	}
	if _, _, err := canon.SplitFrontmatter("no fence here\n"); err == nil {
		t.Fatal("a missing fence was accepted")
	}
}

// fix 9: a BOM-prefixed file is readable AND editable end-to-end.
func TestBOMEntityIsEditable(t *testing.T) {
	ws := newWS(t, "firm-ops")
	people(t, ws)
	path := filepath.Join(ws, "fragments", "bomfrag.md")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, "\uFEFF---\ntype: fragment\nstage: raw\nowner: ann\n---\nnote\n")

	_, err := Update(ws, "bomfrag", UpdateOpts{Fields: fields("stage", "mature")})
	requireNoError(t, err)
	if got := metaValue(t, path, "stage"); got != "mature" {
		t.Fatalf("stage = %#v", got)
	}
}

// fix 10: a no-name type mints from title, else from the type name.
func TestSlugFromTitleThenType(t *testing.T) {
	ws := newWS(t, "firm-ops")
	people(t, ws)
	titled, err := Create(ws, "fragment", CreateOpts{
		Fields: fields("stage", "raw", "owner", "ann", "title", "My Note"), UseTemplate: true})
	requireNoError(t, err)
	if titled.Slug != "001-my-note" {
		t.Fatalf("title slug = %q", titled.Slug)
	}
	bare, err := Create(ws, "fragment", CreateOpts{
		Fields: fields("stage", "raw", "owner", "ann"), UseTemplate: true})
	requireNoError(t, err)
	if bare.Slug != "002-fragment" {
		t.Fatalf("type slug = %q", bare.Slug)
	}
}

// fix 11: linking an entity to itself is refused — a self-edge connects nothing.
func TestSelfLinkRejected(t *testing.T) {
	ws := newWS(t, "firm-ops")
	people(t, ws)
	_, err := Link(ws, "ann", "related", "ann")
	e := requireCode(t, err, "self_link")
	requireMessageContains(t, e, "Cannot link 'ann' to itself via 'related'")
}

// fix 12: a bare target resolving to >1 node is refused; a qualified type/slug
// works.
func TestAmbiguousBareTargetRaises(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	seed(t, ws, "partnerships/acme/_index.md", kv("type", "partnership", "partner", "Acme"))
	seed(t, ws, "identity/team/pat.md", kv("type", "person", "name", "Pat", "role", "partner"))

	_, err := Link(ws, "pat", "related", "acme")
	e := requireCode(t, err, "ambiguity_error")
	requireMessageContains(t, e,
		"Slug 'acme' is ambiguous: client/acme, partnership/acme. Qualify as type/slug")

	res, err := Link(ws, "pat", "related", "client/acme")
	requireNoError(t, err)
	if !res.Changed {
		t.Fatal("the qualified link reported no change")
	}
}

// fix 13: an explicit `updated` in the edit wins; a plain edit still bumps to
// today.
func TestBackdatedUpdatedPreserved(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws)

	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("updated", "2026-06-01")})
	requireNoError(t, err)
	if got := metaValue(t, path, "updated"); got != (canon.Date{ISO: "2026-06-01"}) {
		t.Fatalf("updated = %#v, want the backdated value", got)
	}
	_, err = Update(ws, "initech-deal", UpdateOpts{Fields: fields("stage", "won")})
	requireNoError(t, err)
	if got := metaValue(t, path, "updated"); got != (canon.Date{ISO: today().ISO}) {
		t.Fatalf("updated = %#v, want today", got)
	}
}

// fix 14: link/unlink report Changed, and a no-op leaves the file
// byte-for-byte intact.
func TestLinkChangedFlagAndNoSpuriousRewrite(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "identity/team/noor.md", kv("type", "person", "name", "Noor", "role", "partner"))
	seed(t, ws, "partnerships/northwind/_index.md",
		kv("type", "partnership", "partner", "Northwind", "owner", "noor"))
	path := project(t, ws)

	first, err := Link(ws, "initech-pov", "partner", "northwind")
	requireNoError(t, err)
	if !first.Changed {
		t.Fatal("the first link reported no change")
	}
	afterLink := readFile(t, path)
	again, err := Link(ws, "initech-pov", "partner", "northwind")
	requireNoError(t, err)
	if again.Changed || readFile(t, path) != afterLink {
		t.Fatal("a repeat link rewrote the file")
	}

	removed, err := Unlink(ws, "initech-pov", "partner", "northwind")
	requireNoError(t, err)
	if !removed.Changed {
		t.Fatal("unlink reported no change")
	}
	afterUnlink := readFile(t, path)
	noop, err := Unlink(ws, "initech-pov", "partner", "northwind")
	requireNoError(t, err)
	if noop.Changed || readFile(t, path) != afterUnlink {
		t.Fatal("a repeat unlink rewrote the file")
	}
}

// review round: a blank relation value is a located error on create AND update,
// not an index panic.
func TestEmptyRelationValueRejected(t *testing.T) {
	ws := newWS(t, "firm-ops")
	people(t, ws)
	seed(t, ws, "clients/initech.md", kv("type", "client", "name", "Initech"))

	_, err := Create(ws, "project", CreateOpts{
		Fields: fields("client", "", "owner", "noor"), ID: "p-empty", UseTemplate: true})
	e := requireCode(t, err, "empty_relation_value")
	requireMessageContains(t, e,
		"Empty value for relation 'client'; use unlink to remove an edge")

	seed(t, ws, "projects/p1/_index.md",
		kv("type", "project", "client", "initech", "owner", "noor"))
	_, err = Update(ws, "p1", UpdateOpts{Fields: fields("client", "")})
	requireCode(t, err, "empty_relation_value")
}

// review round: `edit X <relation> X` refuses the self-edge exactly like link.
func TestEditSelfLinkRejected(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "identity/team/noor.md", kv("type", "person", "name", "Noor", "role", "partner"))
	_, err := Update(ws, "noor", UpdateOpts{Fields: fields("related", "noor")})
	requireCode(t, err, "self_link")
}

// review round: UpdateResult.Draft parses a hand-authored draft: 'false' as
// active (AsBool, not truthiness).
func TestUpdateResultDraftReadsBoolish(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "identity/team/ann.md",
		kv("type", "person", "name", "Ann", "role", "partner", "draft", "false"))
	res, err := Update(ws, "ann", UpdateOpts{Fields: fields("name", "Ann B")})
	requireNoError(t, err)
	if res.Draft {
		t.Fatal("a string 'false' draft reported as a draft")
	}
}

// `--field ""` clears an attribute to null rather than storing the empty
// string khub's own gate calls malformed.
func TestBlankAttributeClearsToNull(t *testing.T) {
	ws := newWS(t, "firm-ops")
	editPrereqs(t, ws)
	path := deal(t, ws, "name", "Deal")
	_, err := Update(ws, "initech-deal", UpdateOpts{Fields: fields("name", "")})
	requireNoError(t, err)
	if got := metaValue(t, path, "name"); got != nil {
		t.Fatalf("name = %#v, want null", got)
	}
}

func TestPatternMatchTimeoutFires(t *testing.T) {
	// Schema `pattern`s are author-supplied and regexp2 backtracks with no
	// linear-time guarantee: without fullMatch's MatchTimeout this exact
	// pattern/input pair runs for centuries, hanging the write verb. The
	// guard is the error; if a future edit drops the timeout, this test
	// hangs until `go test`'s own deadline kills the run — loudly.
	_, err := fullMatch(`(a+)+$`, strings.Repeat("a", 36)+"b")
	if err == nil {
		t.Fatal("catastrophic pattern returned no error; MatchTimeout is not set")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err = %v, want match timeout", err)
	}
}
