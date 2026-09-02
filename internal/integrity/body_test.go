package integrity

// The body-content layer through both gates — kb `test_a_body_rule_is_a_gap_
// that_fails_no_gate`, `test_the_body_report_is_single_target_only`,
// `test_lenses_are_filtered_by_frontmatter_and_ordered_by_after`,
// `test_a_when_reads_the_schema_default_of_a_field_nobody_wrote`,
// `test_a_commented_out_heading_is_not_a_heading`, and the per-type
// `template_invalid` rule of kb `check`. A self-contained schema (bare slugs,
// orphan-exempt types) keeps these independent of any preset's id scheme.

import (
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
)

const bodySchema = `
ontology:
  entities:
    note:
      attributes:
        title:  { required: true }
        kind:   { enum: [functional, non-functional, constraint] }
        status: { enum: [proposed, accepted], default: proposed }
      relations:
        affects: { to: any, many: true }
    memo:
      attributes:
        title: { required: true }
policy:
  note: { orphan: true }
  memo: { orphan: true }
storage:
  note: { layout: file, path: notes }
  memo: { layout: file, path: memos, template: false }
`

func bodyWS(t *testing.T, noteTemplate string) string {
	t.Helper()
	root := t.TempDir()
	writeRaw(t, root, ".khub/ontology.yaml", bodySchema)
	writeRaw(t, root, ".khub/config.yaml",
		"name: ws\npreset: fixture\nversion: 0.0.0\nsource:\ndefaults:\n  stale_days: 90\n")
	if noteTemplate != "" {
		writeRaw(t, root, ".khub/templates/note.yaml", noteTemplate)
	}
	return root
}

// createBody is `khub add <type> --id <id> --title <title> --body <body>`.
func createBody(t *testing.T, root, typeName, id, body string, pairs ...string) {
	t.Helper()
	f := fields(pairs...)
	f.Set("title", strings.ToUpper(id[:1])+id[1:])
	_, err := entity.Create(root, typeName, entity.CreateOpts{
		Fields: f, ID: id, Body: body, UseTemplate: true,
	})
	if err != nil {
		t.Fatalf("create %s/%s: %v", typeName, id, err)
	}
}

func bodyRows(errs []FieldError) []string {
	out := []string{}
	for _, e := range errs {
		if e.Field == "body" || e.Field == "template" {
			out = append(out, e.ID()+": "+e.Field+": "+e.Reason)
		}
	}
	return out
}

const ruledTemplate = "sections:\n- heading: Context\n  word_count: {min: 50}\n- heading: Decision\n"

// Shape is an error, content is a gap. A body whose headings are wrong is not
// the document it claims to be; a body whose prose is thin is that document,
// unfinished — and only the first fails the gate.
func TestValidateSplitsErrorsAndGaps(t *testing.T) {
	root := bodyWS(t, ruledTemplate)
	createBody(t, root, "note", "thin", "## Context\n\nToo short.\n\n## Decision\n\nYes.\n")
	createBody(t, root, "note", "shapeless", "no headings at all\n")

	report := mustValidate(t, root, nil, false)
	wantGaps := []string{"note/thin: body: '## Context' 2 words of prose, at least 50 asked for"}
	if got := bodyRows(report.Gaps); strings.Join(got, "|") != strings.Join(wantGaps, "|") {
		t.Errorf("gaps = %v, want %v", got, wantGaps)
	}
	wantErrors := []string{
		"note/shapeless: body: missing or out-of-order section '## Context' " +
			"(template note.yaml requires its headings in order)",
	}
	if got := bodyRows(report.Errors); strings.Join(got, "|") != strings.Join(wantErrors, "|") {
		t.Errorf("errors = %v, want %v", got, wantErrors)
	}
	// The gap alone leaves the gate green.
	one := mustValidate(t, root, ptr("note/thin"), false)
	if !one.OK() || len(one.Gaps) != 1 {
		t.Errorf("a gap failed validate: errors=%v gaps=%v", one.Errors, one.Gaps)
	}
}

// Not even under --strict. Otherwise every rule a template gained would turn a
// green corpus red on upgrade, and nobody would declare one. Reported, though:
// informational is not the same as invisible.
func TestBodyRuleIsAGapThatFailsNoGate(t *testing.T) {
	root := bodyWS(t, ruledTemplate)
	createBody(t, root, "note", "thin", "## Context\n\nToo short.\n\n## Decision\n\nYes.\n")

	for _, strict := range []bool{false, true} {
		report := mustCheck(t, root, strict)
		if !report.Passed() {
			t.Fatalf("strict=%v: a body rule failed check: %+v", strict, report)
		}
		if len(report.Thin) != 1 || report.Thin[0].Type != "note" || report.Thin[0].Slug != "thin" {
			t.Fatalf("strict=%v: thin = %v", strict, report.Thin)
		}
		if want := "'## Context' 2 words of prose, at least 50 asked for"; report.Thin[0].Reason != want {
			t.Errorf("reason = %q, want %q", report.Thin[0].Reason, want)
		}
	}
}

// The metrics and lenses exist for a reviewer reading one document. Computing
// them corpus-wide would put hundreds of section counts in front of someone
// who asked about one file.
func TestBodyReportIsSingleTargetOnly(t *testing.T) {
	root := bodyWS(t, "sections:\n- heading: Context\nlenses:\n- {code: single, instruction: One thing?}\n")
	createBody(t, root, "note", "sized", "## Context\n\none two three\n")

	whole := mustValidate(t, root, nil, false)
	if whole.Body != nil || len(whole.Lenses) != 0 {
		t.Errorf("whole workspace carried a body report: %+v %v", whole.Body, whole.Lenses)
	}
	byType := mustValidate(t, root, ptr("note"), false)
	if byType.Body != nil || len(byType.Lenses) != 0 {
		t.Errorf("a type target carried a body report: %+v %v", byType.Body, byType.Lenses)
	}
	one := mustValidate(t, root, ptr("note/sized"), false)
	if one.Body == nil || one.Body.Words != 3 {
		t.Fatalf("body = %+v, want 3 words", one.Body)
	}
	found := false
	for _, s := range one.Body.Sections {
		if s.Heading == "Context" && s.Words == 3 {
			found = true
		}
	}
	if !found {
		t.Errorf("sections = %v, want Context 3", one.Body.Sections)
	}
	if len(one.Lenses) != 1 || one.Lenses[0].Code != "single" {
		t.Errorf("lenses = %v", one.Lenses)
	}
}

// khub resolves which questions apply and hands them over. It answers none of
// them: the judgement a lens asks for is the part a reader has to be able to
// disagree with.
func TestLensesFilteredByFrontmatterAndOrderedByAfter(t *testing.T) {
	root := bodyWS(t, `sections: []
lenses:
- code: measurable
  when: {kind: [non-functional, constraint]}
  after: single
  instruction: |
    A number, and where it is measured.

    An adjective is not a bound.
- code: single
  name: One statement
  instruction: Exactly one thing?
`)
	createBody(t, root, "note", "nfr", "", "kind", "non-functional")
	createBody(t, root, "note", "fr", "", "kind", "functional")

	got := mustValidate(t, root, ptr("note/nfr"), false).Lenses
	codes := make([]string, 0, len(got))
	for _, l := range got {
		codes = append(codes, l.Code)
	}
	if strings.Join(codes, " ") != "single measurable" {
		t.Fatalf("codes = %v", codes)
	}
	if got[0].Name != "One statement" || got[1].Name != "measurable" {
		t.Errorf("names = %q, %q", got[0].Name, got[1].Name)
	}
	if want := "A number, and where it is measured.\n\nAn adjective is not a bound."; got[1].Instruction != want {
		t.Errorf("instruction = %q", got[1].Instruction)
	}
	fr := mustValidate(t, root, ptr("note/fr"), false).Lenses
	if len(fr) != 1 || fr[0].Code != "single" {
		t.Errorf("functional lenses = %v", fr)
	}
}

// `add` writes no schema defaults beyond `draft` — a value khub chose is not a
// statement anybody made — and a hand-authored file may omit even that, so a
// lens on a defaulted field would otherwise apply to nobody.
func TestWhenReadsSchemaDefaultOfUnwrittenField(t *testing.T) {
	root := bodyWS(t, "sections: []\nlenses:\n"+
		"- {code: published, when: {draft: false}, instruction: Not a draft; hold it to that.}\n"+
		"- {code: open, when: {status: proposed}, instruction: Still open?}\n")
	createBody(t, root, "note", "x", "")
	if raw := readRaw(t, root, "notes/x.md"); strings.Contains(raw, "status:") {
		t.Fatalf("the status default leaked into the file:\n%s", raw)
	}
	// Neither field written at all.
	seed(t, root, "notes/y.md", kv{"type", "note"}, kv{"title", "Y"}, kv{"created", "2026-06-01"})

	for _, id := range []string{"note/x", "note/y"} {
		got := mustValidate(t, root, ptr(id), false).Lenses
		if len(got) != 2 || got[0].Code != "published" || got[1].Code != "open" {
			t.Fatalf("%s: lenses = %v", id, got)
		}
	}
	update(t, root, "note/y", "draft", "true", "status", "accepted")
	if got := mustValidate(t, root, ptr("note/y"), false).Lenses; len(got) != 0 {
		t.Fatalf("after edit: lenses = %v", got)
	}
	update(t, root, "note/x", "draft", "true")
	if got := mustValidate(t, root, ptr("note/x"), false).Lenses; len(got) != 1 || got[0].Code != "open" {
		t.Fatalf("after drafting: lenses = %v", got)
	}
}

// Templates are read once per type, not once per entity: a broken one is one
// finding against the type, not the same finding repeated for every file it
// was supposed to judge — and that type's body checks go quiet.
func TestCheckReportsTemplateInvalidOnceOnTheType(t *testing.T) {
	root := bodyWS(t, "sections: 3\n")
	createBody(t, root, "note", "a", "nothing\n")
	createBody(t, root, "note", "b", "nothing\n")

	report := mustCheck(t, root, false)
	if report.Passed() {
		t.Fatal("a broken template passed check")
	}
	if len(report.TemplateInvalid) != 1 {
		t.Fatalf("template_invalid = %v", report.TemplateInvalid)
	}
	ti := report.TemplateInvalid[0]
	if ti.Type != "note" || ti.Slug != "*" || ti.Field != "template" || ti.ID() != "note/*" {
		t.Errorf("row = %+v", ti)
	}
	if !strings.HasPrefix(ti.Reason, "Template for 'note' (.khub/templates/note.yaml): ") {
		t.Errorf("reason = %q", ti.Reason)
	}
	if len(report.BodyShape) != 0 || len(report.Thin) != 0 {
		t.Errorf("bodies were judged by a contract that does not parse: %v %v", report.BodyShape, report.Thin)
	}
	// validate: the same one row, and the entities' own findings unaffected.
	v := mustValidate(t, root, nil, false)
	if got := bodyRows(v.Errors); len(got) != 1 || !strings.HasPrefix(got[0], "note/*: template: ") {
		t.Errorf("validate rows = %v", got)
	}
}

func TestCheckReportsBodyShapeAsError(t *testing.T) {
	root := bodyWS(t, ruledTemplate)
	createBody(t, root, "note", "fine", "## Context\n\n"+strings.Repeat("word ", 50)+"\n\n## Decision\n\nYes.\n")
	createBody(t, root, "note", "swapped", "## Decision\n\nYes.\n\n## Context\n\nlate.\n")

	report := mustCheck(t, root, false)
	if report.Passed() {
		t.Fatal("a body missing its shape passed check")
	}
	if len(report.BodyShape) != 1 || report.BodyShape[0].ID() != "note/swapped" {
		t.Fatalf("body_shape = %v", report.BodyShape)
	}
	want := "missing or out-of-order section '## Decision' (template note.yaml requires its headings in order)"
	if report.BodyShape[0].Reason != want {
		t.Errorf("reason = %q\n   want %q", report.BodyShape[0].Reason, want)
	}
	// The well-formed body is neither a shape nor a thin finding.
	for _, row := range append(report.BodyShape, report.Thin...) {
		if row.Slug == "fine" {
			t.Errorf("the sound body was reported: %+v", row)
		}
	}
}

// A `##` inside `<!-- -->` is not a heading — a commented-out required heading
// is a missing one.
func TestCommentedOutHeadingIsAShapeError(t *testing.T) {
	root := bodyWS(t, "sections:\n- heading: Context\n- heading: Decision\n")
	createBody(t, root, "note", "hidden", "<!-- ## Context -->\n\nprose\n\n## Decision\n\nYes.\n")

	report := mustCheck(t, root, false)
	if len(report.BodyShape) != 1 || !strings.Contains(report.BodyShape[0].Reason, "'## Context'") {
		t.Fatalf("body_shape = %v", report.BodyShape)
	}
	v := mustValidate(t, root, ptr("note/hidden"), false)
	if v.OK() {
		t.Error("validate passed a body whose required heading is only inside a comment")
	}
}

// Only a per-item md type reads a template. `template: false` opts an md type
// out while its conventional file may stay on disk; a stray template on a type
// that reads none is the stray-template finding, never a body finding.
func TestCheckReadsBodiesOnlyForTemplatedMdTypes(t *testing.T) {
	root := bodyWS(t, "sections:\n- heading: Context\n")
	writeRaw(t, root, ".khub/templates/memo.yaml", "sections:\n- heading: Context\n")
	createBody(t, root, "note", "n", "## Context\n\nok\n")
	createBody(t, root, "memo", "m", "no headings here\n")

	report := mustCheck(t, root, false)
	if !report.Passed() {
		t.Fatalf("check failed: %+v", report)
	}
	if len(report.BodyShape) != 0 || len(report.Thin) != 0 || len(report.TemplateInvalid) != 0 {
		t.Errorf("an opted-out type's body was judged: %v %v %v",
			report.BodyShape, report.Thin, report.TemplateInvalid)
	}
	one := mustValidate(t, root, ptr("memo/m"), false)
	if len(one.Lenses) != 0 {
		t.Errorf("an opted-out type resolved lenses: %v", one.Lenses)
	}
}
