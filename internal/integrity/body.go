// The body-content layer shared by both gates (kb `check`'s template loop and
// `_entity_findings`' body block): each templated type's contract loaded once,
// and every instance body held to it. Shape is an error, content is a gap —
// the split is the whole point. A body whose headings are wrong is not the
// document it claims to be; a body whose prose is thin is that document,
// unfinished.

package integrity

import (
	"fmt"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
)

// bodyReport is what one pass over the templated bodies found. The three
// finding slices are in type-declaration then sorted-node order. gaps is the
// one bucket that never gates: a section rule the prose does not satisfy,
// reported as `gaps` by validate and `thin` by check — both kb's names for
// the same finding. templates holds, per type whose template loaded clean,
// the parsed contract (nil for a type that has none) — validate reads it to
// resolve lenses without a second load, and a type whose template did not
// parse is absent from it.
type bodyReport struct {
	templateInvalid []FieldError
	shape           []FieldError
	gaps            []FieldError
	templates       map[string]*template.BodyTemplate
}

// bodyFindings is the body pass of kb `check`: per md type that reads a
// template, load the template ONCE (a broken one is one `template_invalid`
// finding against the type, never the same finding repeated for every file it
// was supposed to judge — and a contract that does not parse cannot judge a
// body, so that type's shape and rule checks go quiet), then read every
// instance body: the first required heading missing or out of order is a
// `body_shape` error, and every section rule the prose does not satisfy is a
// `body_rule` gap. target is validate's selector (nil for the whole workspace).
//
// Loaded per TYPE, not per shared stem: a lens `when` is validated against the
// owning type's fields, so two types sharing one template can disagree about
// whether it is valid.
func bodyFindings(
	root string, resolved *schema.ResolvedSchema, valid *index.Index, target *string,
) bodyReport {
	report := bodyReport{
		templateInvalid: []FieldError{},
		shape:           []FieldError{},
		gaps:            []FieldError{},
		templates:       map[string]*template.BodyTemplate{},
	}
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		if !rtype.ReadsTemplate() {
			continue
		}
		// Honour the target selector: `validate capability/cap` once reported an
		// unrelated type's broken template and exited 1, so an agent checking its
		// own entity got a failure it did not cause and could not act on.
		if !typeInTarget(tname, target) {
			continue
		}
		stem := rtype.TemplateName()
		if stem == "" {
			continue // template: false — explicitly untemplated
		}
		tpl, err := template.LoadTemplate(root, stem, rtype.FieldNames())
		if err != nil {
			report.templateInvalid = append(report.templateInvalid,
				FieldError{tname, "*", "template", errReason(err)})
			continue
		}
		report.templates[tname] = tpl
		if tpl == nil || len(tpl.Sections) == 0 {
			continue // no template, or an explicitly empty contract: any body passes
		}
		for _, node := range sortedNodes(valid) {
			if node.Type != tname || !inTarget(node, target) {
				continue
			}
			body, ok := readBody(entity.EntityPath(root, rtype, node.Slug))
			if !ok {
				// Frontmatter parsed (the node exists) but the full read failed;
				// the scan did NOT flag this file, so stay loud here.
				report.shape = append(report.shape, FieldError{
					tname, node.Slug, "body", "body could not be read for the structure check",
				})
				continue
			}
			if missing, found := template.MissingHeading(tpl, body); found {
				report.shape = append(report.shape, FieldError{
					Type: tname, Slug: node.Slug, Field: "body",
					Reason: fmt.Sprintf(
						"missing or out-of-order section '## %s' "+
							"(template %s.yaml requires its headings in order)", missing, stem),
				})
			}
			for _, c := range template.BodyRules(tpl, body) {
				report.gaps = append(report.gaps, FieldError{
					Type: tname, Slug: node.Slug, Field: "body",
					Reason: fmt.Sprintf("'## %s' %s", c.Heading, c.Reason),
				})
			}
		}
	}
	return report
}

// lensFront is kb `_lens_front`: the entity's frontmatter with schema defaults
// filled in, for a lens `when` to read. `add` writes no defaults — a status
// khub chose for you is not a statement anybody made — so a defaulted field is
// absent from the file, and `when: {draft: false}` would match only the
// entities that happened to spell it out. The default is what the schema says
// the field means when unwritten, so resolving it here is the same read-time
// computation inverse edges get. Defaults first in attribute order, then meta
// overlays in its own order.
func lensFront(rtype *schema.ResolvedType, meta *omap.Map) *omap.Map {
	out := omap.New()
	for _, name := range rtype.Attributes.Keys() {
		attr, _ := rtype.Attributes.Get(name)
		if attr.Default != nil {
			out.Set(name, attr.Default)
		}
	}
	for _, k := range meta.Keys() {
		v, _ := meta.Get(k)
		out.Set(k, v)
	}
	return out
}
