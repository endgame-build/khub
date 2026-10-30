package extract

// Scaffold: the resolved schema rendered as the contract an extractor fills
// in. One walk over ResolvedSchema — every attribute with its kind, enum,
// pattern, default and required flag; every stored relation with its targets
// and cardinality; the body sections a templated type expects — plus the
// envelope the extractor returns and the rules that go with it. The derived
// inverses are left out: an extractor fills stored edges, and the base
// attributes create writes itself are listed as supplied rather than asked
// for.

import (
	"sort"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/workspace"
)

// Rules are the instruction block every scaffold carries, schema-generic:
// what to extract, what to leave out, and what never to invent.
var Rules = []string{
	"Return one JSON document in the envelope shape and nothing else.",
	"Use only the types listed, spelled exactly; put each extracted record in entities with its type.",
	"Fill a field only when the source states it. Leave a field out rather than guess: never invent a value, never write an empty string.",
	"An enum field takes exactly one of its listed values; a pattern field must match its pattern; a many relation takes a list, a single one takes one target.",
	"A relation target is the id or a known alias of an entity the workspace already holds, or the name the source uses. khub resolves it and reports what it cannot; never invent an id.",
	"Never set the fields listed as supplied; khub writes them.",
	"Quote the passage a record came from in source. Give confidence from 0 to 1 when a value is uncertain; an uncertain record is captured as a draft.",
	"A type that lists body sections takes its prose as Markdown under those ## headings, in that order.",
}

// Scaffold renders the extraction contract for the workspace at root: the
// provenance, the envelope block and one entry per type, in declaration order.
// only narrows the types to those named; a name the schema does not declare
// refuses with the declared ones (`unknown_type`).
func Scaffold(root string, resolved *schema.ResolvedSchema, prov *omap.Map, only []string) (*omap.Map, error) {
	names := resolved.Types.Keys()
	if len(only) > 0 {
		names = []string{}
		for _, name := range only {
			if !resolved.Types.Has(name) {
				preset := "the"
				if p, ok := prov.Get("preset"); ok {
					if s, isStr := p.(string); isStr && s != "" {
						preset = s
					}
				}
				known := append([]string{}, resolved.Types.Keys()...)
				sort.Strings(known)
				return nil, errs.UnknownType(name, preset, known)
			}
			if !contains(names, name) {
				names = append(names, name)
			}
		}
	}
	out := omap.New()
	out.Set("provenance", prov)
	out.Set("envelope", envelopeBlock())
	supplied := map[string]bool{}
	for _, name := range entity.WriteSupplied() {
		supplied[name] = true
	}
	types := []any{}
	for _, name := range names {
		rtype, _ := resolved.Types.Get(name)
		types = append(types, typeContract(root, rtype, supplied))
	}
	out.Set("types", types)
	return out, nil
}

// envelopeBlock documents what the extractor returns: the shape with a
// placeholder in every slot, the fields khub supplies, and the rules.
func envelopeBlock() *omap.Map {
	entry := omap.New()
	entry.Set("type", "<type>")
	fields := omap.New()
	fields.Set("<field>", "<value>")
	entry.Set("fields", fields)
	relations := omap.New()
	relations.Set("<predicate>", []any{"<target>"})
	entry.Set("relations", relations)
	entry.Set("body", "<markdown>")
	entry.Set("source", "<the passage this came from>")
	entry.Set("confidence", 1.0)
	shape := omap.New()
	shape.Set("entities", []any{entry})

	block := omap.New()
	block.Set("shape", shape)
	block.Set("supplied", toAnyList(entity.WriteSupplied()))
	block.Set("rules", toAnyList(Rules))
	return block
}

func typeContract(root string, rtype *schema.ResolvedType, supplied map[string]bool) *omap.Map {
	t := omap.New()
	t.Set("name", rtype.Name)
	t.Set("when", nullable(rtype.When))
	t.Set("id_shape", nullIfEmpty(rtype.IDShape()))
	fields := []any{}
	for _, name := range rtype.Attributes.Keys() {
		if supplied[name] {
			continue
		}
		attr, _ := rtype.Attributes.Get(name)
		f := omap.New()
		f.Set("name", attr.Name)
		f.Set("kind", attr.BaseType)
		f.Set("required", attr.Required)
		if len(attr.Enum) > 0 {
			f.Set("enum", toAnyList(attr.Enum))
		} else {
			f.Set("enum", nil)
		}
		f.Set("pattern", nullable(attr.Pattern))
		f.Set("default", attr.Default)
		fields = append(fields, f)
	}
	t.Set("fields", fields)
	relations := []any{}
	for _, predicate := range rtype.Relations.Keys() {
		rel, _ := rtype.Relations.Get(predicate)
		r := omap.New()
		r.Set("name", rel.Predicate)
		r.Set("to", toAnyList(rel.Targets))
		r.Set("many", rel.Many)
		r.Set("required", rel.Required)
		// Policy path_refs: a value naming no entity may name a workspace
		// file; an extractor that does not know this cannot cite a document.
		r.Set("path_refs", rel.ByPath)
		relations = append(relations, r)
	}
	t.Set("relations", relations)
	t.Set("body", bodyContract(root, rtype))
	return t
}

// bodyContract is the template a type's body follows: its stem and sections,
// `sections: null` when the template does not parse (validate reports why),
// and null when the type reads no template or has none.
func bodyContract(root string, rtype *schema.ResolvedType) any {
	stem := rtype.TemplateName()
	if !rtype.ReadsTemplate() || stem == "" {
		return nil
	}
	body := omap.New()
	body.Set("template", stem)
	tpl, err := template.LoadTemplate(root, stem, rtype.FieldNames())
	if err != nil {
		body.Set("sections", nil)
		return body
	}
	if tpl == nil {
		return nil
	}
	sections := []any{}
	for i := range tpl.Sections {
		s := &tpl.Sections[i]
		sec := omap.New()
		sec.Set("heading", s.Heading)
		sec.Set("optional", s.Optional)
		sec.Set("hint", nullIfEmpty(s.Hint))
		sec.Set("rules", sectionRules(s))
		sections = append(sections, sec)
	}
	body.Set("sections", sections)
	return body
}

// sectionRules renders a section's body-content rules — what validate
// reports as a gap after the write, so the extractor can satisfy them before
// it: `{word_count: {min, max}, required_text, forbidden_text, code_blocks:
// [{lang, min, max}]}`, each key only when the section declares it; null
// when it declares none.
func sectionRules(s *template.Section) any {
	if !s.HasRules() {
		return nil
	}
	rules := omap.New()
	if s.MinWords != nil || s.MaxWords != nil {
		wc := omap.New()
		wc.Set("min", intPtr(s.MinWords))
		wc.Set("max", intPtr(s.MaxWords))
		rules.Set("word_count", wc)
	}
	if len(s.RequiredText) > 0 {
		rules.Set("required_text", textRules(s.RequiredText))
	}
	if len(s.ForbiddenText) > 0 {
		rules.Set("forbidden_text", textRules(s.ForbiddenText))
	}
	if len(s.CodeBlocks) > 0 {
		blocks := []any{}
		for _, c := range s.CodeBlocks {
			b := omap.New()
			b.Set("lang", nullIfEmpty(c.Lang))
			b.Set("min", intPtr(c.Min))
			b.Set("max", intPtr(c.Max))
			blocks = append(blocks, b)
		}
		rules.Set("code_blocks", blocks)
	}
	return rules
}

// textRules renders text rules as a complaint names them: 'literal' or /regex/.
func textRules(rules []template.TextRule) []any {
	out := []any{}
	for _, r := range rules {
		out = append(out, r.String())
	}
	return out
}

func intPtr(p *int) any {
	if p == nil {
		return nil
	}
	return int64(*p)
}

// workspaceProvenance is the preset and version the workspace records.
func workspaceProvenance(root string) (*omap.Map, error) { return workspace.Provenance(root) }

func nullable(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
