// kb test_every_shipped_template_parses + test_a_shipped_template_never_fails
// _its_own_scaffold, over every embedded preset: `add` must not hand back a
// document that is already failing. An external test package so it can
// scaffold a real workspace (workspace imports template).
package template_test

import (
	"testing"

	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/presets"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/workspace"
)

func TestEveryShippedTemplateParsesAndItsScaffoldBreaksNoRule(t *testing.T) {
	names := presets.Known(presets.Embedded())
	if len(names) == 0 {
		t.Fatal("khub ships no presets at all")
	}
	for _, preset := range names {
		t.Run(preset, func(t *testing.T) {
			ws := t.TempDir()
			if _, err := workspace.Init(preset, ws, workspace.InitOptions{}); err != nil {
				t.Fatal(err)
			}
			resolved, err := introspect.LoadSchema(ws)
			if err != nil {
				t.Fatal(err)
			}
			seen := 0
			for _, name := range resolved.Types.Keys() {
				rtype, _ := resolved.Types.Get(name)
				stem := rtype.TemplateName()
				if !rtype.ReadsTemplate() || stem == "" {
					continue
				}
				// With the type's fields, so a lens `when` naming a field the
				// type does not declare is caught here rather than by silently
				// applying to nobody.
				tpl, err := template.LoadTemplate(ws, stem, rtype.FieldNames())
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				if tpl == nil {
					continue
				}
				seen++
				// A template carrying no contract, no scaffold and no lens is
				// a file to delete.
				if len(tpl.Sections) == 0 && tpl.Hint == "" && len(tpl.Lenses) == 0 {
					t.Errorf("%s: template %s.yaml declares nothing", name, stem)
				}
				scaffold := tpl.Render()
				if missing, ok := template.MissingHeading(tpl, scaffold); ok {
					t.Errorf("%s: scaffold is missing its own heading %q", name, missing)
				}
				if got := template.BodyRules(tpl, scaffold); len(got) > 0 {
					t.Errorf("%s: scaffold breaks its own rules: %+v", name, got)
				}
			}
			if seen == 0 {
				t.Logf("%s ships no templates", preset)
			}
		})
	}
}
