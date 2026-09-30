// The `validate` gate — src/khub/core/integrity.py:56-399.
//
// validate is per-entity. It checks each PRESENT declared field against the
// schema (type, enum, pattern, cardinality), confirms every relation resolves,
// and leaves undeclared extensions alone unless strict closes the schema. It
// collects every error rather than stopping at the first (REQ-INT001-02). It
// does NOT enforce required-completeness — that is check's job.

package integrity

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
)

// FieldError is integrity.FieldError: one per-entity validation failure —
// which entity, which field, why.
type FieldError struct {
	Type   string
	Slug   string
	Field  string
	Reason string
}

// ID is FieldError.id.
func (e FieldError) ID() string { return e.Type + "/" + e.Slug }

// ValidateReport is integrity.ValidateReport: how many typed entities were
// validated, every error found, and — split from the errors, gating nothing —
// every gap (kb `cmd_validate`).
//
// Deliberately NOT a superset of CheckReport: the two gates carry different
// findings and the JSON contract has no "fixed" key on this one — validate
// never writes (repairing a missing date is `khub backfill`'s job).
type ValidateReport struct {
	Count  int // typed entities validated (reference markdown is never scanned)
	Errors []FieldError
	// Body-rule findings (`'## <heading>' <complaint>`): the document is what
	// it claims to be, unfinished. Reported, never gating — every member of
	// Errors used to be the only kind, and collapsing the two would make an
	// unfinished body fail a gate documented as "1 means broken".
	Gaps []FieldError
	// Single-target only (a `type/slug` of a type that reads a template), nil
	// otherwise: the body's prose word count and one entry per section. The
	// metrics and lenses exist for a reviewer reading one document; computing
	// them for a whole corpus would put hundreds of section counts and a
	// repeated checklist in front of someone who asked about a file.
	Body *template.SectionMetrics
	// The lenses that apply to the single target, in template order; empty
	// when there is no single target or its template did not load clean.
	Lenses []template.LensView
}

// OK is ValidateReport.ok: no errors. Gaps do not count.
func (r *ValidateReport) OK() bool { return len(r.Errors) == 0 }

// Validate is integrity.validate: validate present declared fields and
// referential integrity over the tree.
//
// target restricts to a type or "type/slug" (nil: the whole workspace) and is
// Python's `str | None`, so an explicitly empty string is a target that matches
// nothing (and raises), not an absent one. strict rejects undeclared keys.
func Validate(root string, target *string, strict bool) (*ValidateReport, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	// Strays are not entities of their layout.
	valid := index.Filter(scanned, index.StrayNodes(scanned))

	errors := []FieldError{}
	count := 0
	// The one entity a `type/slug` target names — kb `_reviewed`: the body
	// metrics and lenses are computed for it alone.
	var reviewed *index.Node
	for _, node := range sortedNodes(valid) {
		if !inTarget(node, target) {
			continue
		}
		count++
		if target != nil && strings.Contains(*target, "/") {
			reviewed = &node
		}
		rtype, _ := resolved.Types.Get(node.Type)
		errors = append(errors,
			validateEntity(rtype, node.Type, node.Slug, valid.Meta[node], valid, strict)...)
	}

	// Body contract: an md type with a workspace template requires the
	// template's section headings in every instance body, in order (extras
	// allowed) — an error — and holds each section's prose to the template's
	// rules — a gap. The index carries frontmatter only, so bodies are re-read
	// here, and only for templated types.
	body := bodyFindings(root, resolved, valid, target)
	errors = append(errors, body.templateInvalid...)
	errors = append(errors, body.shape...)

	var metrics *template.SectionMetrics
	lenses := []template.LensView{}
	if reviewed != nil {
		rtype, _ := resolved.Types.Get(reviewed.Type)
		if rtype.ReadsTemplate() {
			if text, ok := readBody(entity.EntityPath(root, rtype, reviewed.Slug)); ok {
				m := template.Metrics(text)
				metrics = &m
			}
			// Only a template that loaded clean can say which lenses apply; a
			// broken one is already an error above.
			if tpl := body.templates[reviewed.Type]; tpl != nil {
				lenses = tpl.ApplicableLenses(lensFront(rtype, valid.Meta[*reviewed]))
			}
		}
	}

	// A file inside a layout that could not be parsed is a frontmatter error, not
	// a silent skip — one bad file is reported, never a raised parse error.
	malformedErrors := malformedErrors(resolved, scanned.Malformed, target)
	errors = append(errors, malformedErrors...)

	// A target that selects nothing is a typo (`--stagee`) returning a false-clean
	// pass, EXCEPT a bare target naming a declared type — a type with zero
	// entities is a legitimate count-0 run.
	if target != nil && count == 0 && len(malformedErrors) == 0 {
		isDeclaredType := !strings.Contains(*target, "/") && resolved.Types.Has(*target)
		if !isDeclaredType {
			// A struct literal rather than errs.New: Python's LocatedError carries
			// `target=target` here, and there is no factory for this code.
			return nil, &errs.Located{
				Code: "validate_target",
				Message: fmt.Sprintf(
					"No entity or type matches validate target '%s'; "+
						"use a type (e.g. client) or a type/slug (e.g. client/acme)", *target),
				Target: *target,
			}
		}
	}
	return &ValidateReport{
		Count: count, Errors: errors, Gaps: body.gaps, Body: metrics, Lenses: lenses,
	}, nil
}

// readBody is entity._read_doc narrowed to the body, guarded the way Python's
// bare `except` around it is.
func readBody(path string) (string, bool) {
	text, err := canon.ReadText(path)
	if err != nil {
		return "", false
	}
	_, body, err := canon.Parse(text, canon.FmtOf(path))
	if err != nil {
		return "", false
	}
	return body, true
}

// errReason is Python's `getattr(err, "message", None) or str(err)`.
func errReason(err error) string {
	var located *errs.Located
	if errors.As(err, &located) && located.Message != "" {
		return located.Message
	}
	return err.Error()
}

// typeInTarget is integrity._type_in_target: whether a whole TYPE is in scope —
// for findings reported per type, not per node.
func typeInTarget(tname string, target *string) bool {
	if target == nil {
		return true
	}
	head, _, _ := strings.Cut(*target, "/")
	return head == tname
}

// inTarget is integrity._in_target: whether a node falls inside the (optional)
// target selector.
func inTarget(node index.Node, target *string) bool {
	if target == nil {
		return true
	}
	if strings.Contains(*target, "/") {
		return node.ID() == *target
	}
	return node.Type == *target
}

// malformedErrors is integrity._malformed_errors: a `frontmatter` error per
// unparseable file, located to its layout type/slug.
//
// A malformed COLLECTION file matches any target of its type: `validate
// repo/acme` must report "the inventory is broken", not misdiagnose the row id
// as a typo (the row exists — it is unreadable).
func malformedErrors(
	resolved *schema.ResolvedSchema, malformed []string, target *string,
) []FieldError {
	errors := []FieldError{}
	for _, relpath := range malformed {
		tname, slug := malformedIdentity(resolved, relpath)
		wholeType := false
		if rtype, ok := resolved.Types.Get(tname); ok && target != nil {
			head, _, _ := strings.Cut(*target, "/")
			wholeType = rtype.Storage.Layout == schema.LayoutCollection && head == tname
		}
		if !inTarget(index.Node{Type: tname, Slug: slug}, target) && !wholeType {
			continue
		}
		errors = append(errors, FieldError{
			tname, slug, "frontmatter", "could not parse frontmatter of " + relpath,
		})
	}
	return errors
}

// malformedIdentity is integrity._malformed_identity: the (type, slug) a
// malformed file belongs to, derived from its layout path.
func malformedIdentity(resolved *schema.ResolvedSchema, relpath string) (string, string) {
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		if rtype.Storage.Layout == schema.LayoutCollection {
			if relpath == rtype.CollectionRelpath() { // the file IS the whole inventory
				return tname, stem(relpath)
			}
			continue
		}
		if rtype.Storage.Path == nil || *rtype.Storage.Path == "" {
			continue
		}
		rel, ok := relativeTo(relpath, *rtype.Storage.Path)
		if !ok {
			continue
		}
		if rtype.Storage.Layout == schema.LayoutFolder {
			if len(rel) > 0 {
				return tname, rel[0]
			}
			return tname, stem(relpath)
		}
		return tname, stem(relpath)
	}
	return "", relpath
}

// validateEntity is integrity._validate_entity: every error on one entity — its
// id, present fields, relations, strict keys.
func validateEntity(
	rtype *schema.ResolvedType, tname, slug string, meta *omap.Map, idx *index.Index, strict bool,
) []FieldError {
	var errors []FieldError
	if reason, bad := idError(rtype, slug, meta); bad {
		errors = append(errors, FieldError{tname, slug, "id", reason})
	}
	for _, key := range meta.Keys() {
		value, _ := meta.Get(key)
		if attr, ok := rtype.Attributes.Get(key); ok {
			if reason, bad := attrError(attr, value); bad {
				errors = append(errors, FieldError{tname, slug, key, reason})
			}
		} else if _, ok := rtype.Relations.Get(key); ok {
			errors = append(errors, relationErrors(rtype, tname, slug, key, value, idx)...)
		} else if strict {
			errors = append(errors, FieldError{
				tname, slug, key, "undeclared key rejected under --strict",
			})
		}
		// else: an undeclared key is an allowed extension — left unchecked.
	}
	return errors
}

// The retired scheme, `<prefix>-NNN-<slug>`, matched only so the finding that
// rejects it can name the ordinal; and the dated opening `id_date` mints.
//
// Go's \d is ASCII where Python's is Unicode-wide; a slug is a filename token,
// so the classes coincide for every value that can reach here.
var (
	retiredOrdinal = regexp.MustCompile(`^(\d+)-([a-z0-9-]+)$`)
	datedSlug      = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-([a-z0-9-]+)$`)
)

// idError is the id gate: the slug must agree with the id scheme its type
// declares (kb `_id_error`).
//
// Three independent arms — prefix, date, retired ordinal — reporting at most
// one message, because validate prints one line per finding and two findings
// for one slug would say the same thing twice. Every non-singleton type is
// checked: a type declaring nothing still rejects the retired `NNN-` ordinal,
// so a corpus that has not been migrated fails the gate rather than drifting.
//
// The prefix arm is the by-value form's point: an entity whose deciding
// attribute was edited afterwards, or whose file was hand-named, says one
// thing in its filename and another in its frontmatter — a disagreement every
// other gate is blind to.
func idError(rtype *schema.ResolvedType, slug string, meta *omap.Map) (string, bool) {
	if rtype.Storage.Layout == schema.LayoutSingleton {
		return "", false
	}
	shape := rtype.IDShape()
	rest := slug

	if prefix := rtype.IDPrefix; prefix != nil {
		expected, ok := prefix.Resolve(meta)
		if !ok {
			// The deciding attribute is unset, so no prefix can be right and
			// `check` already names the cause as an incomplete entity.
			return "", false
		}
		// Longest first, so a declared `my-type` is not read as prefix `my`.
		// Matching declared prefixes literally is also why a multi-word one
		// works at all.
		found := ""
		for _, p := range longestFirst(prefix.All()) {
			if strings.HasPrefix(slug, p+"-") {
				found = p
				break
			}
		}
		if found == "" {
			return fmt.Sprintf("slug does not start with a prefix this type mints (%s)", shape), true
		}
		if found != expected {
			deciding := ""
			if prefix.By != nil {
				deciding = fmt.Sprintf(" for %s '%s'", *prefix.By, pyStr(metaGet(meta, *prefix.By)))
			}
			return fmt.Sprintf("slug says '%s-' but the schema mints '%s-'%s",
				found, expected, deciding), true
		}
		rest = slug[len(found)+1:]
	}

	if rtype.IDDate {
		// Presence only, never absence: slugBase slugifies the raw title, so a
		// title like "2026 07 28 audit" legitimately mints a date-shaped opening
		// on an undated type. Requiring its absence would reject correct ids.
		m := datedSlug.FindStringSubmatch(rest)
		if m == nil {
			return fmt.Sprintf("slug carries no date — this type mints %s", shape), true
		}
		rest = m[2]
	}

	if m := retiredOrdinal.FindStringSubmatch(rest); m != nil {
		// `<prefix>-NNN-<slug>` was retired. Telling it apart from a title that
		// genuinely starts with digits costs one slugify: "404 handling"
		// slugifies to `404-handling`, which the slug reproduces; "Public API"
		// does not produce the `001-` in `cmp-001-public-api`.
		source, _ := entity.SlugSource(meta)
		if !strings.HasPrefix(entity.Slugify(source), m[1]+"-") {
			return fmt.Sprintf("slug uses the retired '%s-' ordinal scheme; this type mints %s — "+
				"`git mv` the file to drop the ordinal, then fix references to it", m[1], shape), true
		}
	}
	return "", false
}

// longestFirst orders prefixes by descending length, stable so equal lengths
// keep declaration order.
func longestFirst(prefixes []string) []string {
	out := slices.Clone(prefixes)
	slices.SortStableFunc(out, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return out
}

// attrError is integrity._attr_error: a reason if value is illegal for attr.
//
// Enum and pattern are the strong checks; the scalar-type check is conservative
// — it flags a clear mismatch (a word where a number belongs) but never a value
// the write path already coerced and stored.
func attrError(attr *schema.ResolvedAttribute, value any) (string, bool) {
	if value == nil {
		return "", false // an absent value is a completeness concern, not well-formedness
	}
	if s, isStr := value.(string); isStr && strings.TrimSpace(s) == "" {
		// `check` already counts '' as missing (via present), and the write path
		// already rejects it for an enum. validate let it through for a plain text
		// field, so the two gates disagreed about the same byte. null is the way to
		// say "absent" — it passes validate and is what backfill scaffolds.
		return fmt.Sprintf("%s is empty; omit the field or write null, not ''", attr.Name), true
	}
	if attr.Enum != nil {
		if !containsStr(attr.Enum, pyStr(value)) {
			return fmt.Sprintf("'%s' is not a valid %s (%s)",
				pyStr(value), attr.Name, strings.Join(attr.Enum, ", ")), true
		}
		return "", false
	}
	// A pattern that MATCHES falls through to the scalar-type check, exactly as
	// in Python — the pattern branch is a guard clause, not a terminal one.
	if attr.Pattern != nil && !attr.MatchPattern(pyStr(value)) {
		return fmt.Sprintf("'%s' does not match the pattern for %s (%s)",
			pyStr(value), attr.Name, *attr.Pattern), true
	}
	return typeError(attr, value)
}

// typeError is integrity._type_error.
func typeError(attr *schema.ResolvedAttribute, value any) (string, bool) {
	switch attr.BaseType {
	case "bool":
		if !isBool(value) {
			return fmt.Sprintf("'%s' is not a valid bool for %s", pyStr(value), attr.Name), true
		}
	case "number":
		if !isNumber(value) {
			return fmt.Sprintf("'%s' is not a valid number for %s", pyStr(value), attr.Name), true
		}
	case "date", "datetime":
		if !isDateish(value) {
			return fmt.Sprintf("'%s' is not a valid %s for %s",
				pyStr(value), attr.BaseType, attr.Name), true
		}
	}
	return "", false
}

// relationErrors is integrity._relation_errors: cardinality and
// referential-integrity errors for one relation value.
func relationErrors(
	rtype *schema.ResolvedType, tname, slug, predicate string, value any, idx *index.Index,
) []FieldError {
	rel, _ := rtype.Relations.Get(predicate)
	var errors []FieldError
	if list, isList := value.([]any); isList && !rel.Many && len(list) > 1 {
		errors = append(errors, FieldError{
			tname, slug, predicate, "single-valued relation has multiple values",
		})
	}
	for _, target := range asList(value) {
		if !present(target) {
			continue
		}
		text := pyStr(target)
		matches := idx.ResolveTarget(rel, text)
		if len(matches) == 0 {
			errors = append(errors, FieldError{
				tname, slug, predicate, fmt.Sprintf("no %s '%s' to satisfy relation '%s'",
					strings.Join(rel.Targets, "/"), text, predicate),
			})
		} else if !strings.Contains(text, "/") && len(matches) > 1 {
			// A stored bare slug resolving to >1 node is ambiguous — the write path
			// rejects it, but a hand-authored file bypasses that gate; qualify it.
			candidates := make([]string, 0, len(matches))
			for n := range matches {
				candidates = append(candidates, n.ID())
			}
			sort.Strings(candidates)
			errors = append(errors, FieldError{
				tname, slug, predicate, fmt.Sprintf(
					"'%s' is ambiguous — qualify as type/slug (candidates: %s)",
					text, strings.Join(candidates, ", ")),
			})
		}
	}
	return errors
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// --- workspace-relative path helpers (PurePath semantics) --------------------

// stem is PurePath.stem: the file name without its final suffix. A leading dot
// is part of the name, not a suffix (".hidden" stems to ".hidden").
func stem(relpath string) string {
	name := relpath
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[:i]
	}
	return name
}

// relativeTo is PurePath.relative_to: the remaining parts of relpath under
// base, or ok=false where Python raises ValueError. Compared part by part, so
// "clients2/x.md" is NOT under "clients".
func relativeTo(relpath, base string) ([]string, bool) {
	p, b := pathParts(relpath), pathParts(base)
	if len(b) > len(p) {
		return nil, false
	}
	for i, part := range b {
		if p[i] != part {
			return nil, false
		}
	}
	return p[len(b):], true
}

func pathParts(p string) []string {
	out := []string{}
	for _, part := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}
