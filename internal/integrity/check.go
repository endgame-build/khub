// The `check` gate — src/khub/core/integrity.py:402-777.
//
// check is graph-wide over the active (draft:false) subgraph. It computes
// required-completeness from the schema, never from the draft flag
// (REQ-INT002-05), and finds the structural gaps that define it:
// active-but-incomplete entities, drafts that cannot satisfy a required
// relation, orphans, dangling edges, stray files, misplaced files, malformed
// files, and edge cycles.

package integrity

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/values"
)

// Incomplete is integrity.Incomplete: an active entity missing required fields
// and/or required relations.
type Incomplete struct {
	Type             string
	Slug             string
	MissingFields    []string
	MissingRelations []string
}

// ID is Incomplete.id.
func (i Incomplete) ID() string { return i.Type + "/" + i.Slug }

// Dangling is integrity.Dangling: a stored relation value that no longer
// resolves to an existing entity.
type Dangling struct {
	Type      string
	Slug      string
	Predicate string
	Target    string
}

// ID is Dangling.id.
func (d Dangling) ID() string { return d.Type + "/" + d.Slug }

// Misplaced is integrity.Misplaced: a file that CLAIMS to be an entity but sits
// where no layout looks.
//
// The mirror of a stray: a stray is a non-entity inside a layout, this is an
// entity outside every layout. It is the only shape of breakage the scan cannot
// see by construction — the globs follow the schema, so a file the schema does
// not cover is not "absent", it is unscanned, and every gate passes over it.
type Misplaced struct {
	Path     string
	Type     string
	Expected string
}

// AliasConflict is one alias that names more than one entity: two or more
// entities declare it, or it is some other entity's slug. Either way an ID
// lookup through it is ambiguous or silently lands on the slug's owner.
type AliasConflict struct {
	Alias     string
	Claimants []string // qualified ids, sorted
}

// CheckReport is integrity.CheckReport: the graph-wide structural verdict —
// empty everywhere means pass.
//
// Deliberately NOT shared with ValidateReport: `check` is graph-wide and
// `validate` is per-entity, and collapsing them would make a missing required
// field block capture.
type CheckReport struct {
	Incomplete []Incomplete
	Orphans    []string
	Dangling   []Dangling
	Strays     []string
	// Template files no type claims — the mirror of a stray entity file: a
	// stray is a non-entity inside a type's layout, this is a template inside
	// .khub/templates/ that no type's effective template name (declared, or
	// the type's own name by convention) resolves to. The one it usually
	// catches is a renamed template, which would otherwise silently disable
	// both add's scaffolding and validate's body contract.
	StrayTemplates []string
	// A declared `template:` name resolving to no file — the renamed-template
	// hole seen from the claiming side. A `check` finding rather than a schema
	// load error: capture is never blocked, so a broken template link must not
	// take `add` down with it. Entries are "<type>: .khub/templates/<name>.yaml".
	MissingTemplates []string
	// A template that exists but does not parse — one finding against the
	// TYPE (slug "*", field "template"), never repeated per body it was meant
	// to judge. A contract that does not parse cannot judge a body, so that
	// type's shape and rule checks go quiet while every other finding lands.
	TemplateInvalid []FieldError
	// A body whose required headings are missing or out of order: not the
	// document it claims to be. Fails the gate.
	BodyShape []FieldError
	// A body whose prose does not satisfy a section rule: that document,
	// unfinished. Informational — never consulted by Passed, `--strict`
	// included. Every rule a template gained would otherwise turn a green
	// corpus red on upgrade, which is the one thing that would stop anyone
	// from declaring a rule at all.
	Thin      []FieldError
	Cycles    [][]string
	Malformed []string
	// Orphans are informational by default — a fully disconnected entity can be
	// legitimate (a dormant client whose engagements were archived). Strict makes
	// a fully connected graph a gate requirement.
	Strict bool
	// Dangling reports suppressed because their target type's collection file is
	// malformed — derivative noise rolled into the malformed finding.
	SuppressedDangling int
	// Singleton types declared required with no node on disk at all (absent, or
	// present-but-stray). A drafted one is reported by DraftSingletons instead —
	// it is right there on disk, and saying "missing" sent people hunting for a
	// file they had.
	MissingSingletons []string
	// ANY singleton present on disk but unpublished. Not restricted to required
	// types: a drafted optional singleton silently leaves the active subgraph,
	// and reporting it nowhere meant `check` passed while the workspace had
	// quietly lost a document.
	DraftSingletons []string
	// The subset of DraftSingletons whose type is required — the only drafts that
	// fail the gate, since an unpublished PRD must not turn the whole gate green.
	DraftRequiredSingletons []string
	// Files declaring a known type that live outside every declared layout — an
	// entity the scan never reaches. See Misplaced.
	Misplaced []Misplaced
	// Aliases that name more than one entity. Fails the gate.
	AliasConflicts []AliasConflict
}

// Passed is CheckReport.passed.
func (r *CheckReport) Passed() bool {
	if r.Strict && len(r.Orphans) > 0 {
		return false
	}
	return len(r.Incomplete) == 0 &&
		len(r.Dangling) == 0 &&
		len(r.Strays) == 0 &&
		len(r.StrayTemplates) == 0 &&
		len(r.MissingTemplates) == 0 &&
		len(r.TemplateInvalid) == 0 &&
		len(r.BodyShape) == 0 &&
		len(r.Cycles) == 0 &&
		len(r.Malformed) == 0 &&
		len(r.MissingSingletons) == 0 &&
		len(r.DraftRequiredSingletons) == 0 &&
		len(r.Misplaced) == 0 &&
		len(r.AliasConflicts) == 0
}

// Check is integrity.check: walk the active subgraph for completeness,
// orphans, dangling, strays, cycles.
//
// Strays are dropped from the working index up front, so every downstream check
// — edge resolution, degree, completeness — sees only real entities: an edge
// that points at a stray dangles instead of silently resolving to a non-entity.
// A file that could not be parsed is a malformed entry, not a node, and fails
// the check.
func Check(root string, strict bool) (*CheckReport, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	strays := index.StrayNodes(scanned)
	valid := index.Filter(scanned, strays)
	g := graph.BuildGraph(valid)
	entityNodes := sortedNodes(valid)

	incomplete := incompleteEntities(resolved, valid, entityNodes)
	dangling := danglingEdges(resolved, valid, entityNodes)

	// A malformed COLLECTION file removes every row of its type at once, so each
	// edge into that type would dangle derivatively — suppress those and count
	// them: the actionable error is "fix the file", not N dangles burying it.
	// `check` still fails on the malformed entry itself.
	malformedSet := map[string]bool{}
	for _, p := range scanned.Malformed {
		malformedSet[p] = true
	}
	brokenTypes := map[string]bool{}
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		if rtype.Storage.Layout == schema.LayoutCollection && malformedSet[rtype.CollectionRelpath()] {
			brokenTypes[tname] = true
		}
	}
	suppressed := 0
	if len(brokenTypes) > 0 {
		kept := []Dangling{}
		for _, d := range dangling {
			if derivativeDangle(resolved, d, brokenTypes) {
				suppressed++
			} else {
				kept = append(kept, d)
			}
		}
		dangling = kept
	}

	// A type declaring `orphan: true` is exempt. The sweep asks "was this
	// captured and never wired in?", which presupposes an author who could have
	// wired it — false for a narrative root nothing points at by design. The
	// exemption is declared per type, never inferred from `layout: singleton`: a
	// singleton that DOES carry relations must still be swept. No signal is lost
	// either way — a missing required edge is required-completeness's finding,
	// and it names the field.
	orphans := []string{}
	for _, n := range entityNodes {
		rtype, _ := resolved.Types.Get(n.Type)
		if g.InDegree(n) == 0 && g.OutDegree(n) == 0 && !rtype.Orphan {
			orphans = append(orphans, n.ID())
		}
	}

	// Include self references in the cycle projection only; orphan degrees
	// and walks keep their existing self-edge-free graph.
	cycles := graphCycles(graph.BuildCycleGraph(valid), resolved)

	strayLocators := map[string]bool{}
	for n := range strays {
		strayLocators[strayLocator(root, resolved, n.Type, n.Slug)] = true
	}
	strayPaths := sortedKeys(strayLocators)

	// A singleton gap is one the graph cannot express as incompleteness, so
	// report it directly. Two distinct conditions, deliberately not conflated:
	//   missing — no node on disk at all, and the type is required.
	//   draft   — present but unpublished. Swept for EVERY singleton.
	// Only a drafted REQUIRED singleton fails the gate: a draft is unpublished,
	// and the sibling rule already says a draft target never satisfies a required
	// relation, so it cannot satisfy its own type's requiredness either.
	var singletons []string
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		if rtype.Storage.Layout == schema.LayoutSingleton {
			singletons = append(singletons, tname)
		}
	}
	draftSingletons := []string{}
	missingSingletons := []string{}
	for _, tname := range singletons {
		rtype, _ := resolved.Types.Get(tname)
		node := index.Node{Type: tname, Slug: tname}
		if valid.Nodes[node] {
			if values.AsBool(draftFlag(valid.Meta[node])) {
				draftSingletons = append(draftSingletons, tname)
			}
		} else if rtype.Required {
			missingSingletons = append(missingSingletons, tname)
		}
	}
	sort.Strings(draftSingletons)
	sort.Strings(missingSingletons)
	draftRequired := []string{}
	for _, tname := range draftSingletons {
		rtype, _ := resolved.Types.Get(tname)
		if rtype.Required {
			draftRequired = append(draftRequired, tname)
		}
	}

	misplaced, err := misplacedFiles(root, resolved)
	if err != nil {
		return nil, err
	}
	strayTemplates, missingTemplates, err := templateFindings(root, resolved)
	if err != nil {
		return nil, err
	}
	// Bodies, over every valid node of every templated type — the same pass
	// validate runs, whole-workspace.
	body := bodyFindings(root, resolved, valid, nil)
	return &CheckReport{
		TemplateInvalid:         body.templateInvalid,
		BodyShape:               body.shape,
		Thin:                    body.gaps,
		Incomplete:              incomplete,
		Orphans:                 orphans,
		Dangling:                dangling,
		Strays:                  strayPaths,
		Cycles:                  cycles,
		Malformed:               append([]string{}, scanned.Malformed...),
		Strict:                  strict,
		SuppressedDangling:      suppressed,
		DraftRequiredSingletons: draftRequired,
		MissingSingletons:       missingSingletons,
		DraftSingletons:         draftSingletons,
		Misplaced:               misplaced,
		StrayTemplates:          strayTemplates,
		MissingTemplates:        missingTemplates,
		AliasConflicts:          aliasConflicts(valid, entityNodes),
	}, nil
}

// aliasConflicts groups every declared alias case-insensitively, the way ID
// lookup matches it, and reports each one naming more than one entity: a
// second claimant, or another entity whose slug it is. An entity whose alias
// repeats its own slug conflicts with nothing. Drafts count: lookup resolves
// their aliases too, so a draft claimant makes the alias just as ambiguous.
func aliasConflicts(valid *index.Index, nodes []index.Node) []AliasConflict {
	spelling := map[string]string{}
	claimants := map[string]map[string]bool{}
	var keys []string
	for _, n := range nodes {
		for _, a := range index.AliasesOf(valid.Meta[n]) {
			k := index.Casefold(a)
			if claimants[k] == nil {
				claimants[k] = map[string]bool{}
				spelling[k] = a
				keys = append(keys, k)
			}
			claimants[k][n.ID()] = true
		}
	}
	sort.Strings(keys)
	out := []AliasConflict{}
	for _, k := range keys {
		ids := claimants[k]
		if slug, ok := valid.CanonicalSlug(spelling[k]); ok {
			for t := range valid.TypesBySlug[slug] {
				ids[t+"/"+slug] = true
			}
		}
		if len(ids) > 1 {
			out = append(out, AliasConflict{Alias: spelling[k], Claimants: sortedKeys(ids)})
		}
	}
	return out
}

// draftFlag is `meta.get("draft", False)` — a missing key defaults to False,
// which as_bool then reads (a hand-authored `draft: "false"` is PUBLISHED).
func draftFlag(meta *omap.Map) any {
	v, ok := meta.Get("draft")
	if !ok {
		return false
	}
	return v
}

// skipDir is integrity._SKIP_DIRS: the shared prune list plus khub's own
// control directory.
func skipDir(name string) bool { return name == ".khub" || fsio.SkipDirs[name] }

// templateFindings sweeps the template link both ways.
//
// Strays: .khub/templates/*.yaml no type claims. Claimed = some type's
// effective template name resolves to the file's stem — a declared `template:`
// name or, by convention, the type's own name. A type declaring `template:
// false` claims its conventional stem too: the opt-out deliberately leaves
// that file unused, and the documented way to keep a template around must not
// fail the gate.
//
// Missing: a declared `template:` name resolving to no file — the
// renamed-template hole seen from the claiming side. The convention stays
// soft (absence just means "not templated"), but an explicit name pointing at
// nothing would silently disable add's seeding and validate's body contract.
func templateFindings(root string, resolved *schema.ResolvedSchema) (strays, missing []string, err error) {
	// One directory listing answers both questions: the stems that exist.
	existing := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(template.TemplatesDir)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		existing[strings.TrimSuffix(name, ".yaml")] = true
	}
	claimed := map[string]bool{}
	for _, tname := range resolved.Types.Keys() {
		rt, _ := resolved.Types.Get(tname)
		// Only a type that reads a template may claim a stem. Claiming from
		// one that does not (a collection, or any non-md format) would shield
		// the file it names from the stray sweep while no verb ever consults
		// it — the renamed-template hole, seen from the claiming side.
		if !rt.ReadsTemplate() {
			continue
		}
		if rt.TemplateOff {
			claimed[tname] = true
			continue
		}
		stem := rt.TemplateName()
		claimed[stem] = true
		// The resolver's storage matrix guarantees a DECLARED template sits on
		// a type that actually reads one (per-item/singleton md), so a missing
		// file here is always a live break.
		if rt.Template != nil && !existing[stem] {
			missing = append(missing, tname+": "+template.TemplatesDir+"/"+stem+".yaml")
		}
	}
	for stem := range existing {
		if !claimed[stem] {
			strays = append(strays, template.TemplatesDir+"/"+stem+".yaml")
		}
	}
	sort.Strings(strays)
	sort.Strings(missing)
	return strays, missing, nil
}

// misplacedFiles is integrity._misplaced: a file outside every layout whose
// frontmatter names a type the schema knows. Markdown is a candidate anywhere
// in the workspace; a `.json`/`.yaml` only inside a declared storage tree,
// because outside one it is a data file (`package.json`, a lockfile, a compose
// file) and parsing every such file on each `check` costs time and can
// misreport one whose top-level `type` happens to name a declared type.
//
// Deliberately narrow. A README carries no `type`, and a doc about something
// else carries an unknown one — neither fires. It takes a file that positively
// claims to be, say, a `component` while sitting where components are not kept,
// which is what a moved path or a swapped schema leaves behind.
func misplacedFiles(root string, resolved *schema.ResolvedSchema) ([]Misplaced, error) {
	candidates, err := rglobMD(root, resolved)
	if err != nil {
		return nil, err
	}
	out := []Misplaced{}
	for _, path := range candidates {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		if skipped(rel) {
			continue
		}
		accepted := false
		for _, name := range resolved.Types.Keys() {
			rt, _ := resolved.Types.Get(name)
			if rt.AcceptsEntityPath(rel) {
				accepted = true
				break
			}
		}
		if accepted {
			continue
		}
		text, err := canon.ReadTextIn(root, path)
		if err != nil {
			return nil, err
		}
		meta, _, err := canon.Parse(text, canon.FmtOf(path))
		if err != nil {
			continue
		}
		tnameAny, _ := meta.Get("type")
		tname, isStr := tnameAny.(string)
		if !isStr {
			continue
		}
		rtype, known := resolved.Types.Get(tname)
		if !known {
			continue
		}
		// StorageRelpath, not CollectionRelpath: a path-less file or folder type
		// lives under `<name>/`, and the collection default is only right for
		// collection and singleton layouts.
		out = append(out, Misplaced{Path: rel, Type: tname, Expected: rtype.StorageRelpath()})
	}
	return out, nil
}

// rglobMD is `sorted(root.rglob("*.md"))`: every regular file whose name ends
// in .md, anywhere below root, plus every .json/.yaml inside a declared storage
// tree (see misplacedFiles). Symlinked directories are not descended, matching
// pathlib's recursive selector.
//
// Divergence: sorted() over Path objects compares part lists on CPython 3.11
// and the whole string on 3.12+. khub supports both, so the two orders are
// already not a pinned contract; this uses the 3.12+ string order.
func rglobMD(root string, resolved *schema.ResolvedSchema) ([]string, error) {
	trees := storageTrees(resolved)
	var out []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := fsio.ReadDir(root, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if e.Type().IsRegular() {
				name := e.Name()
				if strings.HasSuffix(name, ".md") {
					out = append(out, path)
				} else if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".yaml") {
					if rel, err := filepath.Rel(root, path); err == nil && underStorageTree(rel, trees) {
						out = append(out, path)
					}
				}
			}
			if e.IsDir() && !skipDir(e.Name()) && !strings.HasPrefix(e.Name(), ".") {
				if isSymlink(path) {
					continue
				}
				if err := walk(path); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// storageTrees is every workspace-relative directory a declared type keeps
// entities under: the storage path of a file or folder type, the parent
// directory of a collection or singleton file. The workspace root itself is
// never a tree, so a data file beside `.khub/` is never a candidate.
func storageTrees(resolved *schema.ResolvedSchema) []string {
	var trees []string
	for _, name := range resolved.Types.Keys() {
		rt, _ := resolved.Types.Get(name)
		tree := rt.StorageRelpath()
		if rt.Storage.Layout == schema.LayoutCollection || rt.Storage.Layout == schema.LayoutSingleton {
			tree = filepath.Dir(tree)
		}
		if tree = filepath.ToSlash(filepath.Clean(tree)); tree != "." {
			trees = append(trees, tree)
		}
	}
	return trees
}

func underStorageTree(rel string, trees []string) bool {
	rel = filepath.ToSlash(rel)
	for _, tree := range trees {
		if strings.HasPrefix(rel, tree+"/") {
			return true
		}
	}
	return false
}

func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

// skipped applies the _SKIP_DIRS / leading-dot filter to every part of the
// workspace-relative path, the file name included.
func skipped(rel string) bool {
	for _, part := range pathParts(rel) {
		if skipDir(part) || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// derivativeDangle is integrity._derivative_dangle: whether a dangle is
// derivative of a malformed collection (suppress) or real (keep).
//
// Suppress only when the target provably points into a broken type: a qualified
// "type/slug" naming it, or a bare slug whose EVERY declared home is broken. A
// union edge with a healthy alternative target type is kept — the dangle might
// be a genuinely missing entity of the healthy type, and hiding it until the
// collection is repaired would mislead. `any`-kind bare slugs are likewise kept
// (their home is unknowable while the file is down).
func derivativeDangle(resolved *schema.ResolvedSchema, d Dangling, broken map[string]bool) bool {
	rtype, _ := resolved.Types.Get(d.Type)
	rel, _ := rtype.Relations.Get(d.Predicate)
	if strings.Contains(d.Target, "/") {
		head, _, _ := strings.Cut(d.Target, "/")
		return broken[head]
	}
	if rel.Kind == schema.KindAny {
		return false
	}
	// `set(rel.targets) <= broken`: an empty target set is a subset of anything,
	// so it suppresses — unreachable today (typed has one target, union has one
	// or more), kept faithful rather than second-guessed.
	for _, t := range rel.Targets {
		if !broken[t] {
			return false
		}
	}
	return true
}

// strayLocator is integrity._stray_locator: a stray's address — its file path,
// or "path#slug" for a collection row (N stray rows in one file must not
// collapse into N copies of the same path).
func strayLocator(root string, resolved *schema.ResolvedSchema, tname, slug string) string {
	rtype, _ := resolved.Types.Get(tname)
	rel, err := filepath.Rel(root, entity.EntityPath(root, rtype, slug))
	if err != nil {
		rel = entity.EntityPath(root, rtype, slug)
	}
	if rtype.Storage.Layout == schema.LayoutCollection {
		return rel + "#" + slug
	}
	return rel
}

// incompleteEntities is integrity._incomplete: active entities missing a
// required field or required relation.
//
// Completeness is derived from the schema over the active subgraph only: a
// draft is exempt, and a draft target never satisfies another entity's required
// relation. A required relation whose value resolves to NOTHING is a dangling
// edge (reported separately), not counted here.
func incompleteEntities(
	resolved *schema.ResolvedSchema, idx *index.Index, nodes []index.Node,
) []Incomplete {
	out := []Incomplete{}
	for _, node := range nodes {
		meta := idx.Meta[node]
		if values.AsBool(draftFlag(meta)) {
			continue // drafts are exempt from required-completeness
		}
		rtype, _ := resolved.Types.Get(node.Type)
		// Both lists stay non-nil: an Incomplete is emitted when EITHER is
		// non-empty, and the other must serialize as [] (Python's empty list),
		// never JSON null.
		missingFields := []string{}
		for _, name := range rtype.Attributes.Keys() {
			attr, _ := rtype.Attributes.Get(name)
			if attr.Required && !present(metaGet(meta, attr.Name)) {
				missingFields = append(missingFields, attr.Name)
			}
		}
		missingRelations := []string{}
		for _, predicate := range rtype.Relations.Keys() {
			rel, _ := rtype.Relations.Get(predicate)
			if !rel.Required {
				continue
			}
			value := metaGet(meta, predicate)
			if !present(value) {
				missingRelations = append(missingRelations, predicate)
				continue
			}
			resolvedTo := resolvedNodes(rel, value, idx)
			if len(resolvedTo) == 0 {
				continue // present but unresolvable → dangling, not incomplete
			}
			anyActive := false
			for t := range resolvedTo {
				if !values.AsBool(draftFlag(idx.Meta[t])) {
					anyActive = true
					break
				}
			}
			if !anyActive {
				missingRelations = append(missingRelations, predicate) // all targets are drafts
			}
		}
		if len(missingFields) > 0 || len(missingRelations) > 0 {
			out = append(out, Incomplete{
				Type: node.Type, Slug: node.Slug,
				MissingFields: missingFields, MissingRelations: missingRelations,
			})
		}
	}
	return out
}

// danglingEdges is integrity._dangling: stored relation values that resolve to
// no existing entity.
func danglingEdges(
	resolved *schema.ResolvedSchema, idx *index.Index, nodes []index.Node,
) []Dangling {
	out := []Dangling{}
	for _, node := range nodes {
		meta := idx.Meta[node]
		rtype, _ := resolved.Types.Get(node.Type)
		for _, predicate := range rtype.Relations.Keys() {
			rel, _ := rtype.Relations.Get(predicate)
			value := metaGet(meta, predicate)
			if !present(value) {
				continue
			}
			for _, target := range asList(value) {
				if !present(target) {
					continue
				}
				text := pyStr(target)
				if len(idx.ResolveTarget(rel, text)) == 0 {
					out = append(out, Dangling{node.Type, node.Slug, predicate, text})
				}
			}
		}
	}
	return out
}

// alwaysAcyclic is integrity._ALWAYS_ACYCLIC.
//
// `depends_on` is acyclic by contract in every khub schema — it was hardcoded
// before the flag existed. Keep it built in: the base block declares
// `acyclic: true` on it, but a type that redeclares the predicate owns that
// declaration outright (relations whole-replace, never facet-merge), and
// keying purely off the schema would let a redeclaration silently switch
// cycle detection off.
var alwaysAcyclic = []string{"depends_on"}

// acyclicPredicates is integrity._acyclic_predicates: the built-in contract
// plus whatever the schema marks, sorted.
func acyclicPredicates(resolved *schema.ResolvedSchema) []string {
	declared := map[string]bool{}
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		for _, predicate := range rtype.Relations.Keys() {
			rel, _ := rtype.Relations.Get(predicate)
			if rel.Acyclic {
				declared[predicate] = true
			}
		}
	}
	for _, p := range alwaysAcyclic {
		declared[p] = true
	}
	return sortedKeys(declared)
}

// graphCycles reports one real witness per cyclic component and predicate.
func graphCycles(g *graph.Graph, resolved *schema.ResolvedSchema) [][]string {
	out := [][]string{}
	for _, predicate := range acyclicPredicates(resolved) {
		for _, cycle := range graph.Cycles(g, predicate) {
			ids := make([]string, len(cycle))
			for i, n := range cycle {
				ids[i] = n.ID()
			}
			out = append(out, ids)
		}
	}
	return out
}

// resolvedNodes is integrity._resolved_nodes.
func resolvedNodes(rel *schema.ResolvedRelation, value any, idx *index.Index) map[index.Node]bool {
	nodes := map[index.Node]bool{}
	for _, target := range asList(value) {
		for n := range idx.ResolveTarget(rel, pyStr(target)) {
			nodes[n] = true
		}
	}
	return nodes
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
