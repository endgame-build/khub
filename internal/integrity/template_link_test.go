package integrity

// The declared-template contract, end to end: a `template:` name in storage
// resolves the file, two types may share one, `template: false` opts out with
// the file present (and keeps claiming its conventional stem), and `check`
// reports both halves of the renamed-template hole — the unclaimed file
// (stray_templates) and the declared name pointing at nothing
// (missing_templates). Capture is never blocked: a broken template link is a
// gate finding, never a schema load error.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/introspect"
)

const templateLinkSchema = `
ontology:
  entities:
    pdr:
      attributes:
        title: { required: true }
    adr:
      attributes:
        title: { required: true }
    memo:
      attributes:
        title: { required: true }
storage:
  pdr:  { layout: file, path: pdrs, template: decision }
  adr:  { layout: file, path: adrs, template: decision }
  memo: { layout: file, path: memos, template: false }
`

const decisionTemplate = `sections:
  - heading: Context
  - heading: Decision
`

func templateLinkWS(t *testing.T, withTemplate bool) string {
	t.Helper()
	root := t.TempDir()
	khub := filepath.Join(root, ".khub")
	if err := os.MkdirAll(filepath.Join(khub, "templates"), 0o777); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, root, ".khub/ontology.yaml", templateLinkSchema)
	writeRaw(t, root, ".khub/config.yaml", "name: ws\npreset: fixture\nversion: 0.0.0\nsource:\ndefaults:\n  stale_days: 90\n")
	if withTemplate {
		writeRaw(t, root, ".khub/templates/decision.yaml", decisionTemplate)
	}
	return root
}

func TestDeclaredTemplateSharedAndEnforced(t *testing.T) {
	root := templateLinkWS(t, true)
	// Both declaring types are held to the shared contract; the opted-out one
	// is not, even though nothing named for it exists either.
	writeRaw(t, root, "pdrs/a.md", "---\ntype: pdr\ntitle: A\ncreated: 2026-01-01\n---\n\n## Context\n")
	writeRaw(t, root, "memos/m.md", "---\ntype: memo\ntitle: M\ncreated: 2026-01-01\n---\n\nfreeform\n")
	report, err := Validate(root, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	var hit bool
	for _, e := range report.Errors {
		if e.Type == "pdr" && strings.Contains(e.Reason, "Decision") {
			hit = true
		}
		if e.Type == "memo" {
			t.Errorf("memo (template: false) got a body finding: %+v", e)
		}
	}
	if !hit {
		t.Error("pdr was not held to the shared 'decision' template")
	}
}

func TestDeclaredTemplateMissingIsACheckFinding(t *testing.T) {
	// A declared name pointing at nothing must not fail schema load — that
	// would take `add` (capture) down with it. The schema resolves; `check`
	// reports the broken link, naming both declaring types and the file.
	root := templateLinkWS(t, false)
	if _, err := introspect.LoadSchema(root); err != nil {
		t.Fatalf("LoadSchema must pass with a declared template missing: %v", err)
	}
	report := mustCheck(t, root, false)
	want := []string{
		"adr: .khub/templates/decision.yaml",
		"pdr: .khub/templates/decision.yaml",
	}
	if len(report.MissingTemplates) != 2 ||
		report.MissingTemplates[0] != want[0] || report.MissingTemplates[1] != want[1] {
		t.Fatalf("missing_templates = %v, want %v", report.MissingTemplates, want)
	}
	if report.Passed() {
		t.Error("a declared-but-missing template must fail the gate")
	}
}

func TestStrayTemplateIsACheckFinding(t *testing.T) {
	root := templateLinkWS(t, true)
	// A file no type claims: the renamed-template case.
	writeRaw(t, root, ".khub/templates/dessision.yaml", decisionTemplate)
	report := mustCheck(t, root, false)
	want := []string{".khub/templates/dessision.yaml"}
	if len(report.StrayTemplates) != 1 || report.StrayTemplates[0] != want[0] {
		t.Fatalf("stray_templates = %v, want %v", report.StrayTemplates, want)
	}
	if report.Passed() {
		t.Error("a stray template must fail the gate")
	}

	// The conventionally named file for an undeclared type is claimed — and so
	// is the shared declared name.
	if err := os.Remove(filepath.Join(root, ".khub", "templates", "dessision.yaml")); err != nil {
		t.Fatal(err)
	}
	report = mustCheck(t, root, false)
	if len(report.StrayTemplates) != 0 {
		t.Fatalf("claimed templates reported stray: %v", report.StrayTemplates)
	}

	// An opted-out type (`template: false`) claims its conventional stem: the
	// deliberately unused file is the documented way to keep a template around,
	// so it must not fail the gate the opt-out exists to satisfy.
	writeRaw(t, root, ".khub/templates/memo.yaml", decisionTemplate)
	report = mustCheck(t, root, false)
	if len(report.StrayTemplates) != 0 {
		t.Fatalf("the opted-out type's conventional stem reported stray: %v", report.StrayTemplates)
	}
	if len(report.MissingTemplates) != 0 {
		t.Fatalf("missing_templates = %v, want none", report.MissingTemplates)
	}
}

// A type that reads no body template must not claim a stem. Before
// ReadsTemplate gated the claiming side, every declared type claimed its
// conventional name whatever its layout or format, so a file named for a
// collection type was shielded from the stray sweep while no verb would ever
// consult it — the renamed-template hole, reopened from the claiming side.
const templateClaimSchema = `
ontology:
  entities:
    prd:
      attributes:
        title: { required: true }
    row: {}
    card: {}
storage:
  prd:  { layout: file, path: prds }
  row:  { layout: collection, format: yaml, path: registry/rows.yaml }
  card: { layout: file, format: json, path: cards }
`

func TestTypesThatReadNoTemplateClaimNoStem(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".khub", "templates"), 0o777); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, root, ".khub/ontology.yaml", templateClaimSchema)
	writeRaw(t, root, ".khub/config.yaml", "name: ws\npreset: fixture\nversion: 0.0.0\nsource:\ndefaults:\n  stale_days: 90\n")

	// prd is per-item md — it reads a template, so its conventional stem is
	// claimed and the file is not a stray.
	writeRaw(t, root, ".khub/templates/prd.yaml", decisionTemplate)
	report := mustCheck(t, root, false)
	if len(report.StrayTemplates) != 0 {
		t.Fatalf("the md type's conventional stem reported stray: %v", report.StrayTemplates)
	}

	// row is a collection and card is non-md json. Neither reads a body
	// template, so a file named for either is unclaimed — and must be reported.
	writeRaw(t, root, ".khub/templates/row.yaml", decisionTemplate)
	writeRaw(t, root, ".khub/templates/card.yaml", decisionTemplate)
	report = mustCheck(t, root, false)
	want := []string{".khub/templates/card.yaml", ".khub/templates/row.yaml"}
	if len(report.StrayTemplates) != 2 ||
		report.StrayTemplates[0] != want[0] || report.StrayTemplates[1] != want[1] {
		t.Fatalf("stray_templates = %v, want %v", report.StrayTemplates, want)
	}
	if report.Passed() {
		t.Error("a template file no type can read must fail the gate")
	}
}
