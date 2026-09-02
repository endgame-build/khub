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
		len(r.Misplaced) == 0
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

	// BuildGraph skips self-edges, so a stored self-reference on an acyclic
	// predicate never reaches the cycle detector — detect it directly.
	cycles := append(graphCycles(g, resolved), selfCycles(resolved, valid, entityNodes)...)

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
	}, nil
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

// skipDirs is integrity._SKIP_DIRS.
var skipDirs = map[string]bool{
	".git": true, ".khub": true, ".kb": true,
	"node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
}

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

// misplacedFiles is integrity._misplaced: markdown outside every layout whose
// frontmatter names a type the schema knows.
//
// Deliberately narrow. A README carries no `type`, and a doc about something
// else carries an unknown one — neither fires. It takes a file that positively
// claims to be, say, a `component` while sitting where components are not kept,
// which is what a moved path or a swapped schema leaves behind.
func misplacedFiles(root string, resolved *schema.ResolvedSchema) ([]Misplaced, error) {
	scannedDirs := map[string]bool{}
	scannedFiles := map[string]bool{}
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		switch {
		case rtype.Storage.Layout == schema.LayoutSingleton && rtype.Storage.Path != nil:
			scannedFiles[resolvePath(filepath.Join(root, *rtype.Storage.Path))] = true
		case rtype.Storage.Layout == schema.LayoutCollection:
			scannedFiles[resolvePath(filepath.Join(root, rtype.CollectionRelpath()))] = true
		case rtype.Storage.Path != nil && *rtype.Storage.Path != "":
			scannedDirs[resolvePath(filepath.Join(root, *rtype.Storage.Path))] = true
		}
	}

	candidates, err := rglobMD(root)
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
		resolvedPath := resolvePath(path)
		if scannedFiles[resolvedPath] {
			continue
		}
		if underAny(resolvedPath, scannedDirs) {
			continue // inside a layout: a bad file there is a stray, reported already
		}
		meta := loadMeta(path)
		if meta == nil {
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
		expected := rtype.CollectionRelpath()
		if rtype.Storage.Path != nil && *rtype.Storage.Path != "" {
			expected = *rtype.Storage.Path
		}
		out = append(out, Misplaced{Path: rel, Type: tname, Expected: expected})
	}
	return out, nil
}

// rglobMD is `sorted(root.rglob("*.md"))`: every entry (file OR directory)
// whose name ends in .md, anywhere below root. Symlinked directories are not
// descended, matching pathlib's recursive selector.
//
// Divergence: sorted() over Path objects compares part lists on CPython 3.11
// and the whole string on 3.12+. khub supports both, so the two orders are
// already not a pinned contract; this uses the 3.12+ string order.
func rglobMD(root string) ([]string, error) {
	var out []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil // an unreadable directory yields nothing, like scandir's
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if strings.HasSuffix(e.Name(), ".md") {
				out = append(out, path)
			}
			if e.IsDir() && !skipDirs[e.Name()] && !strings.HasPrefix(e.Name(), ".") {
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

func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

// skipped applies the _SKIP_DIRS / leading-dot filter to every part of the
// workspace-relative path, the file name included.
func skipped(rel string) bool {
	for _, part := range pathParts(rel) {
		if skipDirs[part] || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// loadMeta is formats.load_meta: the document's metadata, or nil if
// unparseable (a directory named *.md lands here and reads as nil).
func loadMeta(path string) *omap.Map {
	text, err := canon.ReadText(path)
	if err != nil {
		return nil
	}
	meta, _, err := canon.Parse(text, canon.FmtOf(path))
	if err != nil {
		return nil
	}
	return meta
}

// resolvePath is Path.resolve(): absolute, symlinks followed where they exist.
func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// underAny is `any(d == path.parent or d in path.parents for d in dirs)`.
func underAny(path string, dirs map[string]bool) bool {
	for d := filepath.Dir(path); ; {
		if dirs[d] {
			return true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false
		}
		d = parent
	}
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

// graphCycles is integrity._cycles: elementary cycles on each
// acyclic-by-contract predicate, as id lists.
//
// graph.Cycles is Johnson's algorithm, the same one nx.simple_cycles uses.
// Rotation and enumeration order are implementation-defined on both sides, so
// only set membership and count are contract (go-port-plan R14).
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

// selfCycles is integrity._self_cycles: one-node cycles — a stored
// acyclic-predicate value resolving to the entity itself.
//
// BuildGraph skips self-edges, so a self-referential depends_on (or a
// self-superseding ADR) never reaches the graph cycle detector. `link` refuses
// a self-edge, but a hand-edit, an import, or a merge resolution can still
// write one.
func selfCycles(
	resolved *schema.ResolvedSchema, idx *index.Index, nodes []index.Node,
) [][]string {
	out := [][]string{}
	predicates := acyclicPredicates(resolved)
	for _, node := range nodes {
		rtype, _ := resolved.Types.Get(node.Type)
		for _, predicate := range predicates {
			rel, ok := rtype.Relations.Get(predicate)
			if !ok {
				continue
			}
			value := metaGet(idx.Meta[node], predicate)
			if !present(value) {
				continue
			}
			hit := false
			for _, target := range asList(value) {
				if idx.ResolveTarget(rel, pyStr(target))[node] {
					hit = true
					break
				}
			}
			if hit {
				out = append(out, []string{node.ID()})
				break // one self-cycle entry per entity, whichever predicate caused it
			}
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
