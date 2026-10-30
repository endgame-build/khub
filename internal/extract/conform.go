package extract

// Conform: the extractor's envelope against the resolved schema and the live
// workspace, without a write. Per entity it answers exactly what `add` then
// `validate` would — the same field validator (integrity.FieldFindings), the
// same relation resolution (entity.ResolveWriteValue: exact slug, then alias,
// never a guess), the same body-shape finding — and, split from those the way
// `check` splits them from validate, which required fields the record would
// leave empty. A gap never fails the gate: capture is never blocked, and an
// incomplete record lands as active-but-incomplete, or as a draft.

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/values"
)

// Unresolved is one relation target the workspace holds no entity for, with
// the refusal `add` would raise for it.
type Unresolved struct {
	Predicate string
	Value     string
	Reason    string
}

// EntityReport is the verdict on one envelope entry. OK is false when Errors
// or Unresolved is non-empty — the two things the write path refuses. Gaps
// are the required fields and relations the record would leave empty: reported,
// never gating. ID is the qualified id add would write the entry under, ""
// when it cannot be minted (then Errors says why, under field `id`).
// Confidence echoes the entry's own number, nil when it gave none.
type EntityReport struct {
	Index      int
	Type       string
	ID         string
	OK         bool
	Errors     []integrity.FieldError
	Gaps       []string
	Unresolved []Unresolved
	Confidence any
}

// Report is the conformance report: every entity, how many conform, and the
// score — conformant over entities, to three decimals; 1 for an empty
// envelope, which has nothing to reject.
type Report struct {
	Entities   []EntityReport
	Conformant int
	Score      float64
}

// OK reports whether every entity conforms.
func (r *Report) OK() bool { return r.Conformant == len(r.Entities) }

// ScoreText renders the score the way the text report prints it.
func (r *Report) ScoreText() string { return strconv.FormatFloat(r.Score, 'f', 3, 64) }

// Conform judges env against the workspace at root. An entity naming a type
// the schema does not declare refuses the whole call (`unknown_type`): the
// contract was not read, so no score would mean anything. Nothing is written
// and no lock is taken.
func Conform(root string, env *Envelope) (*Report, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	for _, ent := range env.Entities {
		if !resolved.Types.Has(ent.Type) {
			preset, perr := presetName(root)
			if perr != nil {
				return nil, perr
			}
			known := append([]string{}, resolved.Types.Keys()...)
			sort.Strings(known)
			return nil, errs.UnknownType(ent.Type, preset, known)
		}
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	idx = index.Filter(idx, index.StrayNodes(idx))

	templates := newTemplateCache(root)
	supplied := map[string]bool{}
	for _, name := range entity.WriteSupplied() {
		supplied[name] = true
	}
	report := &Report{Entities: make([]EntityReport, 0, len(env.Entities))}
	minted := &mintedIDs{first: map[index.Node]int{}}
	for i, ent := range env.Entities {
		rtype, _ := resolved.Types.Get(ent.Type)
		er := conformEntity(i, ent, rtype, idx, templates, supplied, minted)
		if er.OK {
			report.Conformant++
		}
		report.Entities = append(report.Entities, er)
	}
	report.Score = score(report.Conformant, len(report.Entities))
	return report, nil
}

// score is conformant/entities rounded to three decimals, 1 when there are
// no entities. Rounded through strconv so the JSON number and the text agree.
func score(conformant, total int) float64 {
	if total == 0 {
		return 1
	}
	rounded, err := strconv.ParseFloat(
		strconv.FormatFloat(float64(conformant)/float64(total), 'f', 3, 64), 64)
	if err != nil {
		return math.NaN() // unreachable: a formatted float parses
	}
	return rounded
}

// mintedIDs is the (type, slug) set the envelope mints as it is read, so a
// second entry minting an id an earlier one already claimed is reported
// against that entry — a property of the envelope alone, decided by the same
// minting add runs — and an unresolved target that names an earlier entry's
// would-be id can say so by index. ids is in entry order, a nil for an entry
// that minted nothing.
type mintedIDs struct {
	first map[index.Node]int
	ids   []*index.Node
}

func conformEntity(
	i int, ent Extracted, rtype *schema.ResolvedType, idx *index.Index,
	templates *templateCache, supplied map[string]bool, minted *mintedIDs,
) EntityReport {
	er := EntityReport{
		Index: i, Type: ent.Type,
		Errors: []integrity.FieldError{}, Gaps: []string{}, Unresolved: []Unresolved{},
		Confidence: ent.Confidence,
	}
	fail := func(field, reason string) {
		er.Errors = append(er.Errors, integrity.FieldError{Type: ent.Type, Field: field, Reason: reason})
	}

	// The record as add would assemble it: the supplied head, then the
	// attributes as given, then every relation with its resolved targets.
	meta := omap.New()
	meta.Set("type", ent.Type)
	relations := omap.New() // predicate -> []string, fields first, then relations
	for _, key := range ent.Fields.Keys() {
		value, _ := ent.Fields.Get(key)
		switch {
		case supplied[key]:
			// The contract lists these as khub's own; on add `draft` is a
			// flag and `created` would date the id, so an extractor setting
			// one is a contract fault, not a value to judge.
			fail(key, fmt.Sprintf("'%s' is supplied by khub on write; leave it out of fields", key))
		case key == "body" && rtype.Storage.Fmt != "md":
			// The reserved prose key of a json/yaml entity (create refuses it
			// as body_field_reserved); the envelope has its own body key.
			fail("body", fmt.Sprintf("'body' is reserved on a %s entity; put the prose in the envelope's body",
				rtype.Storage.Fmt))
		default:
			if _, isRel := rtype.Relations.Get(key); isRel {
				// A declared predicate among the fields is the same statement
				// `--<predicate>` makes on add: a relation.
				targets, ok := targetList(value)
				if !ok {
					fail(key, "relation value must be a string or a list of strings")
					continue
				}
				relations.Set(key, append(listOf(relations, key), targets...))
				continue
			}
			if _, isAttr := rtype.Attributes.Get(key); isAttr {
				// partition stores a blank declared attribute as null — absent,
				// so a required one is a gap — never as the '' validate rejects.
				if s, isStr := value.(string); isStr && strings.TrimSpace(s) == "" {
					value = nil
				}
			}
			meta.Set(key, value)
		}
	}
	for _, predicate := range ent.Relations.Keys() {
		raw, _ := ent.Relations.Get(predicate)
		relations.Set(predicate, append(listOf(relations, predicate), raw.([]string)...))
	}
	for _, predicate := range relations.Keys() {
		rel, ok := rtype.Relations.Get(predicate)
		if !ok {
			fail(predicate, errs.IllegalPredicate(predicate, ent.Type).Message)
			continue
		}
		resolved := []string{}
		for _, target := range listOf(relations, predicate) {
			value, err := entity.ResolveWriteValue(idx, rel, predicate, target)
			if err != nil {
				reason := reasonOf(err)
				// Targets resolve against the workspace only. One that names
				// an earlier entry's would-be id is still unresolved — adding
				// that entry first is what resolves it — and the reason says
				// which entry, read off the same minting, never guessed.
				if j, node, ok := minted.names(rel, target); ok {
					reason = fmt.Sprintf("%s; envelope entry #%d mints %s, add it first", reason, j, node.ID())
				}
				er.Unresolved = append(er.Unresolved, Unresolved{predicate, target, reason})
				continue
			}
			resolved = append(resolved, value)
		}
		resolved = entity.UniqueTargets(idx, rel, resolved)
		switch {
		case len(resolved) == 0:
			// Nothing resolved: the record would carry no edge here, which the
			// gap sweep below reports when the relation is required.
		case !rel.Many && len(resolved) == 1:
			meta.Set(predicate, resolved[0])
		default:
			meta.Set(predicate, toAnyList(resolved)) // FieldFindings reports a surplus on a single-valued relation
		}
	}

	er.Errors = append(er.Errors, integrity.FieldFindings(rtype, meta, idx, false)...)

	// The id add would write under, with every refusal it raises before the
	// write — the one place a missing required field does block capture (a
	// type's slug source), so it is an error here, not a gap. Across the
	// envelope one (type, slug) is minted once: the write path refuses the
	// second as slug_taken, and here the finding names the entry that got
	// there first.
	var mintedNode *index.Node
	if slug, err := entity.PlanSlug(rtype, meta, idx); err != nil {
		fail("id", reasonOf(err))
	} else {
		node := index.Node{Type: ent.Type, Slug: slug}
		er.ID = node.ID()
		if j, dup := minted.first[node]; dup {
			fail("id", fmt.Sprintf("envelope entry #%d mints %s as well; name one of them differently", j, node.ID()))
		} else {
			minted.first[node] = i
			mintedNode = &node
		}
	}
	minted.ids = append(minted.ids, mintedNode)

	// The body against the type's template, as validate judges the written
	// file. No body means add seeds the scaffold, which passes by construction.
	if ent.Body != nil && rtype.ReadsTemplate() && rtype.TemplateName() != "" {
		stem := rtype.TemplateName()
		tpl, err := templates.load(stem, rtype)
		switch {
		case err != nil:
			fail("template", reasonOf(err))
		case tpl != nil && len(tpl.Sections) > 0:
			if reason, found := integrity.BodyShapeReason(tpl, stem, *ent.Body); found {
				fail("body", reason)
			}
		}
	}

	// Required-completeness, as check would report the written record: a
	// required attribute add does not supply, or a required relation, empty.
	for _, name := range rtype.Attributes.Keys() {
		attr, _ := rtype.Attributes.Get(name)
		if attr.Required && !supplied[name] && !values.Present(metaGet(meta, name)) {
			er.Gaps = append(er.Gaps, name)
		}
	}
	for _, predicate := range rtype.Relations.Keys() {
		rel, _ := rtype.Relations.Get(predicate)
		if rel.Required && !values.Present(metaGet(meta, predicate)) {
			er.Gaps = append(er.Gaps, predicate)
		}
	}
	er.OK = len(er.Errors) == 0 && len(er.Unresolved) == 0
	return er
}

// names is the earliest entry whose would-be id the target spells — as a
// bare slug or a qualified id, case-folded the way lookup folds — among the
// types rel accepts. A later entry naming an earlier one is the only
// self-reference that can resolve once the adds run in envelope order.
func (m *mintedIDs) names(rel *schema.ResolvedRelation, target string) (int, index.Node, bool) {
	typeName, slug := "", target
	if before, after, qualified := strings.Cut(target, "/"); qualified {
		typeName, slug = before, after
	}
	want := index.Casefold(slug)
	for j, node := range m.ids {
		if node == nil {
			continue
		}
		if typeName != "" && index.Casefold(typeName) != index.Casefold(node.Type) {
			continue
		}
		if rel.Kind != schema.KindAny && !contains(rel.Targets, node.Type) {
			continue
		}
		if index.Casefold(node.Slug) == want {
			return j, *node, true
		}
	}
	return 0, index.Node{}, false
}

// templateCache loads each type's template once per conform run, keeping the
// load error so every entity of a broken type reports it.
type templateCache struct {
	root   string
	loaded map[string]*template.BodyTemplate
	failed map[string]error
}

func newTemplateCache(root string) *templateCache {
	return &templateCache{root: root, loaded: map[string]*template.BodyTemplate{}, failed: map[string]error{}}
}

func (c *templateCache) load(stem string, rtype *schema.ResolvedType) (*template.BodyTemplate, error) {
	if err, failed := c.failed[rtype.Name]; failed {
		return nil, err
	}
	if tpl, ok := c.loaded[rtype.Name]; ok {
		return tpl, nil
	}
	tpl, err := template.LoadTemplate(c.root, stem, rtype.FieldNames())
	if err != nil {
		c.failed[rtype.Name] = err
		return nil, err
	}
	c.loaded[rtype.Name] = tpl
	return tpl, nil
}

// reasonOf is the located message, else the error's text.
func reasonOf(err error) string {
	var located *errs.Located
	if errors.As(err, &located) && located.Message != "" {
		return located.Message
	}
	return err.Error()
}

// presetName is the workspace's preset for the unknown-type message, "the"
// when the config records none — what `schema show` prints.
func presetName(root string) (string, error) {
	prov, err := workspaceProvenance(root)
	if err != nil {
		return "", err
	}
	if p, ok := prov.Get("preset"); ok {
		if s, isStr := p.(string); isStr && strings.TrimSpace(s) != "" {
			return s, nil
		}
	}
	return "the", nil
}

func listOf(m *omap.Map, key string) []string {
	v, ok := m.Get(key)
	if !ok {
		return nil
	}
	return v.([]string)
}

func metaGet(meta *omap.Map, key string) any {
	v, _ := meta.Get(key)
	return v
}

func toAnyList(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
