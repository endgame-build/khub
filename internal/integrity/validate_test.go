package integrity

// Ports the library-level `validate` rows of tests/test_integrity.py
// (TS-INT-001) and tests/test_scan_hardening.py. CLI-level assertions are left
// to the golden fixtures.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TS-INT-001-01 / U03 / U05: a clean tree validates; undeclared extensions pass;
// frontmatter-less reference markdown is skipped, not counted.
func TestValidateCleanTree(t *testing.T) {
	root := cleanWS(t)
	report := mustValidate(t, root, nil, false)
	if !report.OK() {
		t.Fatalf("clean tree reported %v", report.Errors)
	}
	if report.Count != 5 {
		t.Errorf("count = %d, want 5 (mission.md is not an entity)", report.Count)
	}
	// person/noor carries an undeclared `mood`, left on disk untouched.
	if !strings.Contains(readRaw(t, root, "identity/team/noor.md"), "mood: good") {
		t.Error("the undeclared extension did not survive the write")
	}
}

// errorWS is test_integrity.py's error_ws: an off-enum stage and an unresolved
// owner relation.
func errorWS(t *testing.T) string {
	t.Helper()
	root := cleanWS(t)
	seed(t, root, "opportunities/bad-stage/_index.md",
		kv{"type", "opportunity"}, kv{"stage", "vibing"}, kv{"client", "initech"},
		kv{"owner", "noor"}, kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})
	seed(t, root, "projects/ghost-owner/_index.md",
		kv{"type", "project"}, kv{"client", "initech"}, kv{"owner", "nobody"},
		kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})
	return root
}

// TS-INT-001-U01 / U02 / U06: an off-enum value and an unresolved relation are
// BOTH reported — the gate collects, it does not stop at the first.
func TestValidateCollectsEveryError(t *testing.T) {
	errs := errorKeys(mustValidate(t, errorWS(t), nil, false))
	cases := []struct {
		id, field, wants string
	}{
		{"opportunity/bad-stage", "stage", "vibing"},
		{"project/ghost-owner", "owner", "nobody"},
	}
	for _, tc := range cases {
		reason, ok := errs[[2]string{tc.id, tc.field}]
		if !ok {
			t.Fatalf("no error for (%s, %s); got %v", tc.id, tc.field, errs)
		}
		if !strings.Contains(reason, tc.wants) {
			t.Errorf("(%s, %s) reason %q does not name %q", tc.id, tc.field, reason, tc.wants)
		}
	}
}

// The conservative scalar-type check: a clear mismatch is flagged, a non-finite
// number is not a number, and a date with trailing junk is not a date.
func TestValidateScalarTypeChecks(t *testing.T) {
	cases := []struct {
		name         string
		field, value string
		wantField    string
	}{
		{"word where a number belongs", "confidence", "high", "confidence"},
		{"non-finite number", "confidence", "inf", "confidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := freshWS(t)
			create(t, root, "person", "w", "name", "W", "role", "consultant")
			seed(t, root, "fragments/f.md",
				kv{"type", "fragment"}, kv{"stage", "raw"}, kv{"owner", "w"},
				kv{tc.field, tc.value}, kv{"created", "2026-06-01"})
			if _, ok := errorKeys(mustValidate(t, root, nil, false))[[2]string{"fragment/f", tc.wantField}]; !ok {
				t.Fatalf("no error on (fragment/f, %s)", tc.wantField)
			}
		})
	}

	t.Run("malformed date", func(t *testing.T) {
		root := freshWS(t)
		seed(t, root, "clients/c.md", kv{"type", "client"}, kv{"name", "C"},
			kv{"created", "2026-01-01 not a date"}, kv{"updated", "2026-01-02"})
		if _, ok := errorKeys(mustValidate(t, root, nil, false))[[2]string{"client/c", "created"}]; !ok {
			t.Fatal("a date with trailing junk was accepted")
		}
	})
}

// TS-INT-001-U04 (REQ-INT001-03): --strict rejects an undeclared key the open
// schema passes.
func TestValidateStrictRejectsUndeclared(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "clients/initech.md", kv{"type", "client"}, kv{"name", "Initech"},
		kv{"vibe", "high"}, kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})

	if open := mustValidate(t, root, nil, false); !open.OK() {
		t.Fatalf("the open schema rejected an extension: %v", open.Errors)
	}
	strict := mustValidate(t, root, nil, true)
	if strict.OK() {
		t.Fatal("--strict accepted the undeclared key")
	}
	found := false
	for _, e := range strict.Errors {
		if e.Field == "vibe" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no error on the undeclared `vibe`: %v", strict.Errors)
	}
}

// Review #4: a stray (wrong internal type) is not counted or validated as its
// layout type.
func TestValidateSkipsStrays(t *testing.T) {
	root := cleanWS(t)
	writeRaw(t, root, "clients/boss.md", "---\ntype: person\nname: Boss\n---\n")
	report := mustValidate(t, root, nil, false)
	if report.Count != 5 {
		t.Errorf("count = %d, want 5 (the stray is excluded)", report.Count)
	}
	for _, e := range report.Errors {
		if e.Slug == "boss" {
			t.Fatalf("the stray was validated: %v", e)
		}
	}
}

// Review #15: a relation value of integer 0 is checked, not skipped by
// truthiness — it resolves to nothing, so it is a referential error.
func TestValidateRelationValueZeroIsChecked(t *testing.T) {
	root := freshWS(t)
	create(t, root, "person", "w", "name", "W", "role", "consultant")
	seed(t, root, "fragments/f.md", kv{"type", "fragment"}, kv{"stage", "raw"},
		kv{"owner", "w"}, kv{"depends_on", []any{int64(0)}}, kv{"created", "2026-01-01"})
	if _, ok := errorKeys(mustValidate(t, root, nil, false))[[2]string{"fragment/f", "depends_on"}]; !ok {
		t.Fatal("a depends_on of 0 was skipped by truthiness")
	}
}

// khub's rule: null means absent (passes validate, `check` reports it); an
// empty string is malformed. The two gates must not disagree about the same byte.
func TestValidateEmptyStringVersusAbsent(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "clients/blank.md",
		kv{"type", "client"}, kv{"name", ""}, kv{"created", "2026-01-01"})
	seed(t, root, "clients/absent.md", kv{"type", "client"}, kv{"created", "2026-01-01"})

	errs := errorKeys(mustValidate(t, root, nil, false))
	if _, ok := errs[[2]string{"client/blank", "name"}]; !ok {
		t.Error("an empty-string name passed validate")
	}
	if _, ok := errs[[2]string{"client/absent", "name"}]; ok {
		t.Error("an absent name was reported by validate (it is check's finding)")
	}

	incomplete := incompleteByID(mustCheck(t, root, false))
	for _, id := range []string{"client/blank", "client/absent"} {
		if _, ok := incomplete[id]; !ok {
			t.Errorf("check did not report %s incomplete", id)
		}
	}
}

// Fix #4 (scan hardening): a stored bare slug resolving to two nodes is
// reported ambiguous.
func TestValidateFlagsAmbiguousStoredBareSlug(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "clients/dup.md", kv{"type", "client"}, kv{"name", "Dup Co"},
		kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})
	seed(t, root, "identity/team/dup.md", kv{"type", "person"}, kv{"name", "Dup Person"},
		kv{"role", "consultant"}, kv{"created", "2026-06-01"})
	seed(t, root, "clients/refs.md", kv{"type", "client"}, kv{"name", "Refs"},
		kv{"depends_on", []any{"dup"}}, kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})

	reason, ok := errorKeys(mustValidate(t, root, nil, false))[[2]string{"client/refs", "depends_on"}]
	if !ok {
		t.Fatal("no error on the ambiguous bare slug")
	}
	if !strings.Contains(reason, "ambiguous") {
		t.Errorf("reason %q does not say ambiguous", reason)
	}
}

// Fix #3 (scan hardening): a target selecting nothing is a typo, EXCEPT a bare
// target naming a declared type.
func TestValidateTargetSelection(t *testing.T) {
	root := freshWS(t)

	t.Run("declared type with zero entities is ok", func(t *testing.T) {
		report := mustValidate(t, root, ptr("meeting"), false)
		if !report.OK() || report.Count != 0 {
			t.Fatalf("count = %d, errors = %v", report.Count, report.Errors)
		}
	})
	for _, target := range []string{"nonsense", "client/ghost"} {
		t.Run(target, func(t *testing.T) {
			_, err := Validate(root, ptr(target), false)
			if err == nil {
				t.Fatal("a no-match target returned a false-clean pass")
			}
			if code := locatedCode(err); code != "validate_target" {
				t.Fatalf("code = %q, want validate_target", code)
			}
		})
	}
}

// Fix #1 (scan hardening): a malformed file becomes a `frontmatter` error, never
// a raised parse error.
func TestValidateReportsMalformedAsFrontmatterError(t *testing.T) {
	root := malformedWS(t)
	report := mustValidate(t, root, nil, false)
	if report.OK() {
		t.Fatal("a malformed file passed validate")
	}
	found := false
	for _, e := range report.Errors {
		if e.Field == "frontmatter" && e.Type == "client" && e.Slug == "broken" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no frontmatter error located to client/broken: %v", report.Errors)
	}
}

// HQ-port regression: a directory matching the layout glob is skipped — not an
// entity, not malformed.
func TestDirectoryNamedMDIsInvisibleToValidate(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "clients/real.md", kv{"type", "client"}, kv{"name", "Real"},
		kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})
	mkdir(t, root, "clients/weird.md")
	mkdir(t, root, "opportunities/ghost/_index.md")

	report := mustValidate(t, root, nil, false)
	if !report.OK() {
		t.Fatalf("a directory named *.md produced errors: %v", report.Errors)
	}
	if report.Count != 1 {
		t.Errorf("count = %d, want 1", report.Count)
	}
}

// --- body-structure contract -------------------------------------------------

// An md type with a workspace template requires the template's headings, in
// order; a body missing one is a `body` finding on that entity.
func TestValidateBodyStructureContract(t *testing.T) {
	root := wsFor(t, "build-lite")
	create(t, root, "adr", "ad-2026-01-15-x", "title", "X", "status", "proposed")
	// Replace the templated body with prose that satisfies no heading.
	path := "knowledge/decisions/ad-2026-01-15-x.md"
	front, _, _ := strings.Cut(readRaw(t, root, path), "\n---\n")
	writeRaw(t, root, path, front+"\n---\n\nno headings at all\n")

	report := mustValidate(t, root, nil, false)
	found := false
	for _, e := range report.Errors {
		if e.Type == "adr" && e.Slug == "ad-2026-01-15-x" && e.Field == "body" {
			found = true
			if !strings.Contains(e.Reason, "missing or out-of-order section") {
				t.Errorf("reason = %q", e.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("no body-structure finding: %v", report.Errors)
	}
}

// A broken template is reported once, on the type, and does not abort the run.
func TestValidateBrokenTemplateIsOneFindingOnTheType(t *testing.T) {
	root := wsFor(t, "build-lite")
	writeRaw(t, root, ".khub/templates/adr.yaml", "sections: 3\n")
	report := mustValidate(t, root, nil, false)
	found := false
	for _, e := range report.Errors {
		if e.Type == "adr" && e.Slug == "*" && e.Field == "template" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no template finding: %v", report.Errors)
	}
}

// The target selector governs template findings too: checking one type must not
// fail on an unrelated type's broken template.
func TestValidateTemplateFindingHonoursTarget(t *testing.T) {
	root := wsFor(t, "build-lite")
	writeRaw(t, root, ".khub/templates/adr.yaml", "sections: 3\n")
	create(t, root, "component", "cmp-api", "title", "API", "kind", "service")

	report := mustValidate(t, root, ptr("component"), false)
	for _, e := range report.Errors {
		if e.Type == "adr" {
			t.Fatalf("an unrelated type's template broke a scoped run: %v", e)
		}
	}
}

// --- the id gate -------------------------------------------------------------

// Three arms, at most one finding per slug: the prefix the type mints (and,
// for the by-value form, the one its deciding attribute chose), the date when
// `id_date`, and the retired `NNN-` ordinal — unless the title itself starts
// with those digits. kb `test_id_gate_*`.
func TestIdGateThreeArms(t *testing.T) {
	root := wsFor(t, "build-lite")
	adr := func(slug, title string) {
		seed(t, root, "knowledge/decisions/"+slug+".md", kv{"type", "adr"}, kv{"title", title},
			kv{"status", "proposed"}, kv{"created", "2026-01-15"})
	}
	cmp := func(slug, title string) {
		seed(t, root, "knowledge/components/"+slug+".md", kv{"type", "component"}, kv{"title", title},
			kv{"kind", "service"}, kv{"created", "2026-01-15"})
	}
	adr("ad-2026-01-15-good", "Good")
	adr("ad-001-old", "Old") // the date arm fires first: an old ordinal id carries no date
	adr("ad-2026-01-15-001-old", "Old")
	adr("ad-undated", "Undated")
	adr("no-prefix", "No prefix")
	cmp("cmp-api", "API")
	cmp("cmp-001-api", "API")
	cmp("cmp-404-handling", "404 handling")
	// A date-shaped opening on an undated type is legal when the title mints it.
	cmp("cmp-2026-01-15-dated", "2026 01 15 dated")

	got := errorKeys(mustValidate(t, root, nil, false))
	want := map[string]string{
		"adr/ad-001-old": "slug carries no date — this type mints ad-YYYY-MM-DD-slug",
		"adr/ad-2026-01-15-001-old": "slug uses the retired '001-' ordinal scheme; this type mints " +
			"ad-YYYY-MM-DD-slug — `git mv` the file to drop the ordinal, then fix references to it",
		"adr/ad-undated": "slug carries no date — this type mints ad-YYYY-MM-DD-slug",
		"adr/no-prefix":  "slug does not start with a prefix this type mints (ad-YYYY-MM-DD-slug)",
		"component/cmp-001-api": "slug uses the retired '001-' ordinal scheme; this type mints " +
			"cmp-slug — `git mv` the file to drop the ordinal, then fix references to it",
	}
	for id, reason := range want {
		if got[[2]string{id, "id"}] != reason {
			t.Errorf("%s: id finding = %q, want %q", id, got[[2]string{id, "id"}], reason)
		}
	}
	for _, id := range []string{"adr/ad-2026-01-15-good", "component/cmp-api",
		"component/cmp-404-handling", "component/cmp-2026-01-15-dated"} {
		if reason, bad := got[[2]string{id, "id"}]; bad {
			t.Errorf("%s: unexpected id finding %q", id, reason)
		}
	}
	// One finding per slug, never two: the arms stop at the first.
	count := 0
	for k := range got {
		if k[1] == "id" {
			count++
		}
	}
	if count != len(want) {
		t.Errorf("%d id findings, want %d: %v", count, len(want), got)
	}
}

// The by-value form: the prefix must be the one the deciding attribute chose,
// and a bare `NNN-slug` no longer passes as "minted before kind was set".
func TestIdGateByValuePrefix(t *testing.T) {
	root := t.TempDir()
	initFrom(t, root, "bykind", writePresetDir(t, "bykind", `
version: "0.1.0"
ontology:
  entities:
    requirement:
      attributes:
        title: { required: true }
        kind:  { enum: [functional, constraint], required: true }
storage:
  requirement:
    layout: file
    path: reqs
    id_prefix: { by: kind, map: { functional: fr, constraint: cst } }
`))
	req := func(slug, kind string) {
		f := []kv{{"type", "requirement"}, {"title", "X"}, {"created", "2026-01-15"}}
		if kind != "" {
			f = append(f, kv{"kind", kind})
		}
		seed(t, root, "reqs/"+slug+".md", f...)
	}
	req("fr-x", "functional")
	req("fr-y", "constraint")
	req("001-z", "functional")
	req("cst-unset", "")

	got := errorKeys(mustValidate(t, root, nil, false))
	if r := got[[2]string{"requirement/fr-x", "id"}]; r != "" {
		t.Errorf("fr-x: unexpected %q", r)
	}
	want := "slug says 'fr-' but the schema mints 'cst-' for kind 'constraint'"
	if r := got[[2]string{"requirement/fr-y", "id"}]; r != want {
		t.Errorf("fr-y: %q, want %q", r, want)
	}
	want = "slug does not start with a prefix this type mints (fr|cst-slug)"
	if r := got[[2]string{"requirement/001-z", "id"}]; r != want {
		t.Errorf("001-z: %q, want %q", r, want)
	}
	// The deciding attribute is unset: no prefix can be right, and `check`
	// names the cause as an incomplete entity — validate stays quiet.
	if r, bad := got[[2]string{"requirement/cst-unset", "id"}]; bad {
		t.Errorf("cst-unset: unexpected %q", r)
	}
}

// writePresetDir lays down a preset directory (init's --preset-source shape)
// from one document carrying the layer blocks, splitting it into the three
// layer files init expects, and returns the directory holding it.
func writePresetDir(t *testing.T, name, doc string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	ontology, storage, _ := strings.Cut(doc, "storage:\n")
	files := map[string]string{"ontology.yaml": ontology, "policy.yaml": "policy: {}\n"}
	if storage != "" {
		files["storage.yaml"] = "storage:\n" + storage
	}
	for file, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name, file), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
