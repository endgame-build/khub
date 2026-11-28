// The lens review: the state of every applicable lens of an entity, read from
// the lens verdicts `khub review --record` appends to its `verified` list.
//
// khub never answers a lens. It computes which lenses apply (exactly as
// `validate <type/slug>` lists them), what the current content and question
// are, and whether the last recorded verdict for each still holds; an agent,
// a person or a script answers the prompt. `check --require-lenses` gates on
// the result.

package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/trust"
)

// The lens states.
const (
	LensPass       = "pass"
	LensFail       = "fail"
	LensStale      = "stale"
	LensUnreviewed = "unreviewed"
)

// The verdicts a lens event may carry.
const (
	VerdictPass = "pass"
	VerdictFail = "fail"
)

// LensQ is a lens question's hash: the first 12 hex digits of a SHA-256 over
// the instruction text exactly as `validate` and `review` print it (the
// template's string with leading and trailing newlines trimmed). A verdict
// records it, so an edit of the question makes the verdict stale.
func LensQ(instruction string) string {
	sum := sha256.Sum256([]byte(instruction))
	return hex.EncodeToString(sum[:])[:12]
}

// LensState is one applicable lens and its state for one entity.
type LensState struct {
	template.LensView
	Q     string
	State string
}

// LensTarget is one entity a review covers: its file, its content revision
// (trust.Rev), its body, and every applicable lens with its state, in
// template order.
type LensTarget struct {
	Type, Slug, Path, Rev, Body string
	Lenses                      []LensState
}

// ID is the qualified id.
func (t LensTarget) ID() string { return t.Type + "/" + t.Slug }

// Open is the lenses not in state pass.
func (t LensTarget) Open() []LensState {
	out := []LensState{}
	for _, l := range t.Lenses {
		if l.State != LensPass {
			out = append(out, l)
		}
	}
	return out
}

// ReviewReport is `khub review`'s read: the targets, and the `--changed`
// paths that named no entity with an applicable lens.
type ReviewReport struct {
	Targets []LensTarget
	Skipped []string
}

// LensFinding is one applicable lens not in state pass on an active entity:
// the `lenses` bucket of `check --require-lenses`.
type LensFinding struct {
	Type, Slug, Lens, State string
}

// ID is the qualified id.
func (f LensFinding) ID() string { return f.Type + "/" + f.Slug }

// applicableLenses is the one definition of which lenses apply to an entity:
// the template's lenses whose `when` holds against the frontmatter with schema
// defaults resolved in, in template order. validate, review and check all
// call it. A nil template (none, or one that did not load) has none.
func applicableLenses(rtype *schema.ResolvedType, tpl *template.BodyTemplate, meta *omap.Map) []template.LensView {
	if tpl == nil {
		return []template.LensView{}
	}
	return tpl.ApplicableLenses(lensFront(rtype, meta))
}

// lensTemplates loads, per type that reads a template, the template — the
// load bodyFindings makes. A type whose template is absent, opted out or does
// not parse maps to nil: it has no lens to answer, and check reports the
// broken template on its own.
func lensTemplates(root string, resolved *schema.ResolvedSchema) map[string]*template.BodyTemplate {
	out := map[string]*template.BodyTemplate{}
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		if !rtype.ReadsTemplate() || rtype.TemplateName() == "" {
			continue
		}
		if tpl, err := template.LoadTemplate(root, rtype.TemplateName(), rtype.FieldNames()); err == nil {
			out[tname] = tpl
		}
	}
	return out
}

// lensStates reads each lens's state from the entity's lens events: the last
// event for the lens code decides. pass or fail when its verdict is that and
// both its `rev` and its `q` match now; stale when an event exists but either
// differs; unreviewed when there is none. An event whose verdict is neither
// pass nor fail is no verdict and is skipped.
func lensStates(lenses []template.LensView, meta *omap.Map, rev string) []LensState {
	last := map[string]*omap.Map{}
	for _, e := range trust.Events(meta) {
		code, isStr := get(e, "lens").(string)
		if !isStr {
			continue
		}
		if v, _ := get(e, "verdict").(string); v != VerdictPass && v != VerdictFail {
			continue
		}
		last[code] = e
	}
	out := make([]LensState, 0, len(lenses))
	for _, l := range lenses {
		q := LensQ(l.Instruction)
		state := LensUnreviewed
		if e, has := last[l.Code]; has {
			state = LensStale
			if r, _ := get(e, "rev").(string); r == rev {
				if eq, _ := get(e, "q").(string); eq == q {
					state, _ = get(e, "verdict").(string)
				}
			}
		}
		out = append(out, LensState{LensView: l, Q: q, State: state})
	}
	return out
}

func get(m *omap.Map, k string) any {
	v, _ := m.Get(k)
	return v
}

// lensTarget is one node's review target over a scan with bodies.
func lensTarget(resolved *schema.ResolvedSchema, valid *index.Index,
	templates map[string]*template.BodyTemplate, node index.Node) LensTarget {
	rtype, _ := resolved.Types.Get(node.Type)
	meta, body := valid.Meta[node], valid.Body[node]
	rev := trust.Rev(rtype, meta, body)
	return LensTarget{
		Type: node.Type, Slug: node.Slug, Path: valid.Paths[node], Rev: rev, Body: body,
		Lenses: lensStates(applicableLenses(rtype, templates[node.Type], meta), meta, rev),
	}
}

// skippablePath reports whether a path refusal means "no entity here", which
// a changed-file list holds routinely (a deleted file, code beside the
// workspace, a collection file): those paths are skipped, never refused.
func skippablePath(err error) bool {
	var located *errs.Located
	if !errors.As(err, &located) {
		return false
	}
	switch located.Code {
	case "path_not_entity", "path_is_collection", "path_outside_workspace":
		return true
	}
	return false
}

// changedNodes resolves the `--changed` paths as `get --path` does, over the
// unfiltered scan, and keeps the entities among valid: the nodes in sorted
// order, each once, and the paths that named none, in argument order. A stray
// is no entity. Any other refusal (`unsafe_path`, an OS error) is returned.
func changedNodes(root, base string, scanned, valid *index.Index, paths []string) ([]index.Node, []string, error) {
	resolve := entity.PathResolver(root, base, scanned)
	seen := map[index.Node]bool{}
	var nodes []index.Node
	skipped := []string{}
	for _, p := range paths {
		node, err := resolve(p)
		if err != nil {
			if skippablePath(err) {
				skipped = append(skipped, p)
				continue
			}
			return nil, nil, err
		}
		if !valid.Nodes[node] {
			skipped = append(skipped, p)
			continue
		}
		if !seen[node] {
			seen[node] = true
			nodes = append(nodes, node)
		}
	}
	index.SortNodes(nodes)
	return nodes, skipped, nil
}

// Review is `khub review`'s read. Without changed (nil), every active entity
// of a templated type with at least one applicable lens. With it, the
// entities stored at those paths (base is the caller's working directory, as
// for `get --path`), drafts included; a path naming no entity, or one with no
// applicable lens, lands in Skipped.
func Review(root, base string, changed []string) (*ReviewReport, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	scanned, err := index.BuildWithBodies(root, resolved)
	if err != nil {
		return nil, err
	}
	valid := index.Filter(scanned, index.StrayNodes(scanned))
	templates := lensTemplates(root, resolved)
	report := &ReviewReport{Targets: []LensTarget{}, Skipped: []string{}}
	if changed == nil {
		for _, node := range sortedNodes(valid) {
			rtype, _ := resolved.Types.Get(node.Type)
			if rtype.IsDraft(valid.Meta[node]) {
				continue
			}
			if t := lensTarget(resolved, valid, templates, node); len(t.Lenses) > 0 {
				report.Targets = append(report.Targets, t)
			}
		}
		return report, nil
	}
	nodes, skipped, err := changedNodes(root, base, scanned, valid, changed)
	if err != nil {
		return nil, err
	}
	report.Skipped = skipped
	lensless := map[index.Node]bool{}
	for _, node := range nodes {
		if t := lensTarget(resolved, valid, templates, node); len(t.Lenses) > 0 {
			report.Targets = append(report.Targets, t)
		} else {
			lensless[node] = true
		}
	}
	// A path whose entity has no lens to answer is skipped too, in argument
	// order with the others.
	if len(lensless) > 0 {
		resolve := entity.PathResolver(root, base, scanned)
		report.Skipped = report.Skipped[:0]
		for _, p := range changed {
			node, err := resolve(p)
			if err != nil || !valid.Nodes[node] || lensless[node] {
				report.Skipped = append(report.Skipped, p)
			}
		}
	}
	return report, nil
}

// lensFindings is the `lenses` bucket: every applicable lens not in state
// pass, over the active entities, or only those stored at changed when it is
// not nil.
func lensFindings(root, base string, resolved *schema.ResolvedSchema, scanned, valid *index.Index,
	templates map[string]*template.BodyTemplate, changed []string) ([]LensFinding, error) {
	nodes := sortedNodes(valid)
	if changed != nil {
		var err error
		if nodes, _, err = changedNodes(root, base, scanned, valid, changed); err != nil {
			return nil, err
		}
	}
	out := []LensFinding{}
	for _, node := range nodes {
		rtype, _ := resolved.Types.Get(node.Type)
		if rtype.IsDraft(valid.Meta[node]) {
			continue
		}
		for _, l := range lensTarget(resolved, valid, templates, node).Lenses {
			if l.State != LensPass {
				out = append(out, LensFinding{Type: node.Type, Slug: node.Slug, Lens: l.Code, State: l.State})
			}
		}
	}
	return out, nil
}

// LensVerdict is one verdict of a reply, as `khub review --record` reads it.
// Index is its position in the reply's list. PlanLensVerdicts fills Node, Q
// (the lens's question hash now) and the type's review list check.
type LensVerdict struct {
	Index                  int
	ID, Lens, Rev, Verdict string
	Note                   string
	HasNote                bool
	Node                   index.Node
	Q                      string
}

// PlanLensVerdicts checks a reply against the workspace, all or nothing: each
// verdict's id must resolve, its lens must apply to the entity now, its
// verdict must be pass or fail (a fail with a note), and its rev must be the
// current content revision. The first verdict that fails a check refuses the
// whole batch. On success each verdict carries its node, the lens's current
// question hash and the current rev. The caller holds the lock and scanned
// idx under it.
func PlanLensVerdicts(root string, resolved *schema.ResolvedSchema, idx *index.Index, verdicts []LensVerdict) error {
	valid := index.Filter(idx, index.StrayNodes(idx))
	templates := lensTemplates(root, resolved)
	for i := range verdicts {
		v := &verdicts[i]
		node, err := entity.ResolveID(valid, v.ID)
		if err != nil {
			return err
		}
		rtype, _ := resolved.Types.Get(node.Type)
		meta, body := valid.Meta[node], valid.Body[node]
		lenses := applicableLenses(rtype, templates[node.Type], meta)
		at := slices.IndexFunc(lenses, func(l template.LensView) bool { return l.Code == v.Lens })
		if at < 0 {
			codes := make([]string, len(lenses))
			for j, l := range lenses {
				codes[j] = l.Code
			}
			return errs.LensNotApplicable(v.Index, v.Lens, node.Type+"/"+node.Slug, codes)
		}
		rev := trust.Rev(rtype, meta, body)
		if v.Rev != rev {
			return errs.StaleReply(v.Index, node.Type+"/"+node.Slug, v.Rev, rev)
		}
		v.Node, v.Q = node, LensQ(lenses[at].Instruction)
	}
	return nil
}
