// Port of src/khub/core/entity.py — the write surface over the graph (FS-002).
//
// create and get are the foundational pair: mint a typed entity as one document
// (slug minting, layout resolution, field/enum/pattern validation, referential-
// integrity hard-fail) and read one back (id resolution, ambiguity detection,
// read-time inverse-edge derivation). update/link/unlink/delete extend them.
//
// `draft` is a manual publish flag: add defaults it to false, --draft sets it,
// and `edit <id> draft …` toggles it. khub never derives it from completeness —
// capture is never blocked, and an active-but-incomplete entity is surfaced by
// check, not by this flag.
//
// Every verb is schema-generic: it introspects the compiled schema at runtime
// and has no per-type code path. The schema and git are the only gates.
package entity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// --- create ------------------------------------------------------------------

// Create mints a new entity of typeName from opts.Fields (raw --field value
// strings).
//
// Validates each field and hard-fails (writing nothing) if a relation target
// does not resolve. Draft is the manual publish flag (default false); a missing
// required field never blocks capture — check surfaces the gap as
// active-but-incomplete.
func Create(root, typeName string, opts CreateOpts) (*CreateResult, error) {
	return fsio.Locked(root, func() (*CreateResult, error) { return create(root, typeName, opts) })
}

func create(root, typeName string, opts CreateOpts) (*CreateResult, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	rtype, ok := resolved.Types.Get(typeName)
	if !ok {
		preset, perr := presetName(root)
		if perr != nil {
			return nil, perr
		}
		known := append([]string{}, resolved.Types.Keys()...)
		sort.Strings(known)
		return nil, errs.UnknownType(typeName, preset, known)
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	idx = index.Filter(idx, index.StrayNodes(idx))
	part, err := partition(rtype, opts.Fields, opts.Strict)
	if err != nil {
		return nil, err
	}

	// Template seeding: an md type with a workspace template and no explicit
	// body starts from the scaffold (--no-template opts out). A broken template
	// never blocks capture — seed nothing and let validate report the template.
	body := opts.Body
	if stem := rtype.TemplateName(); strings.TrimSpace(body) == "" && rtype.ReadsTemplate() && stem != "" {
		tpl, terr := template.LoadTemplate(root, stem, rtype.FieldNames())
		if terr != nil {
			var located *errs.Located
			if errors.As(terr, &located) && located.Code == "unsafe_path" {
				return nil, terr
			}
			tpl = nil // validate carries the template finding
		}
		if tpl != nil {
			if !opts.UseTemplate {
				// --no-template on a type with required headings produced an
				// entity validate rejects on its very next run ("missing or
				// out-of-order section '## X'"): a documented flag whose only
				// outcome was a red workspace. Refuse instead. A template of
				// optional sections or a bare hint requires nothing, so opting
				// out of its scaffold is honoured.
				if len(tpl.RequiredHeadings()) > 0 {
					return nil, errs.New("template_required", fmt.Sprintf(
						"Type '%s' has a body template, so --no-template would create "+
							"an entity `khub validate` rejects. Omit the flag, or pass --body "+
							"with the template's sections", typeName))
				}
			} else if scaffold := tpl.Render(); scaffold != "" {
				// Seed whenever the template renders anything — a top-level
				// hint alone is a scaffold, not only headings.
				body = scaffold
			}
		}
	}
	body = mdNormalized(body, rtype)

	// Referential integrity (and bare-target ambiguity) hard-fail before any
	// byte is written.
	for _, predicate := range part.rels.Keys() {
		rel, _ := rtype.Relations.Get(predicate)
		vals, _ := part.rels.Get(predicate)
		for _, value := range vals.([]string) {
			if _, err := resolveWriteTarget(rel, value, idx, predicate, "relation"); err != nil {
				return nil, err
			}
		}
		unique := uniqueTargets(idx, rel, vals.([]string))
		if !rel.Many && len(unique) > 1 {
			return nil, errs.CardinalityViolation(predicate)
		}
		part.rels.Set(predicate, unique)
	}

	// `draft` is manual: the --draft flag, or an explicit `draft` field, else false.
	isDraft := opts.Draft
	if explicit, has := part.attrs.Get("draft"); has {
		part.attrs.Delete("draft")
		if explicit != nil {
			isDraft = truthy(explicit)
		}
	}

	meta := omap.New()
	meta.Set("type", typeName)
	meta.Set("created", today())
	meta.Set("updated", today())
	meta.Set("draft", isDraft)
	for _, name := range rtype.Attributes.Keys() {
		if v, has := part.attrs.Get(name); has {
			meta.Set(name, v)
		}
	}
	for _, predicate := range rtype.Relations.Keys() {
		vals, has := part.rels.Get(predicate)
		if !has {
			continue
		}
		rel, _ := rtype.Relations.Get(predicate)
		list := vals.([]string)
		if rel.Many {
			meta.Set(predicate, toAnyList(list))
		} else {
			meta.Set(predicate, list[0])
		}
	}
	for _, key := range part.extras.Keys() {
		v, _ := part.extras.Get(key)
		meta.Set(key, v)
	}

	switch rtype.Storage.Layout {
	case schema.LayoutSingleton:
		// The slug IS the type name; an --id saying otherwise is a mistake.
		if opts.ID != "" && opts.ID != typeName {
			return nil, errs.New("singleton_id", fmt.Sprintf(
				"'%s' is a singleton — its id is always '%s' (drop --id)", typeName, typeName))
		}
		path := entityPath(root, rtype, typeName)
		if err := fsio.MkdirAll(root, filepath.Dir(path)); err != nil {
			return nil, err
		}
		if err := writeNew(root, path, meta, body); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return nil, errs.New("singleton_exists", fmt.Sprintf(
					"Singleton '%s' already exists at %s; edit it instead of adding another",
					typeName, relSlash(root, path)))
			}
			return nil, err
		}
		return &CreateResult{Type: typeName, Slug: typeName, Path: path, Draft: isDraft}, nil

	case schema.LayoutCollection:
		slug, err := chooseSlug(rtype, meta, opts.ID)
		if err != nil {
			return nil, err
		}
		if err := createRow(root, rtype, typeName, meta, body, slug, opts.ID != ""); err != nil {
			return nil, err
		}
		return &CreateResult{
			Type:    typeName,
			Slug:    slug,
			Path:    entityPath(root, rtype, slug),
			Draft:   isDraft,
			Locator: locator(rtype, slug),
		}, nil
	}

	// Slug + O_EXCL write. Minted after the frontmatter is assembled, so a
	// --created override dates the id. Minting reads no siblings, so the same
	// name or title mints the same slug twice: the index pre-check names the
	// refusal cheaply, and the exclusive create is the real gate against a
	// concurrent add. Neither an explicit nor a minted slug is ever suffixed.
	slug, err := chooseSlug(rtype, meta, opts.ID)
	if err != nil {
		return nil, err
	}
	explicit := opts.ID != ""
	if idx.Nodes[index.Node{Type: typeName, Slug: slug}] {
		return nil, errs.SlugTaken(slug, typeName, !explicit)
	}
	path := entityPath(root, rtype, slug)
	if err := fsio.MkdirAll(root, filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := writeNew(root, path, meta, body); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, errs.SlugTaken(slug, typeName, !explicit)
		}
		return nil, err
	}
	return &CreateResult{Type: typeName, Slug: slug, Path: path, Draft: isDraft}, nil
}

// createRow inserts one new row into a collection under slug.
//
// Uniqueness is gated by the fresh in-lock read, not the index: a taken slug
// refuses, minted or explicit — nothing is suffixed. The row omits `type` (the
// collection's schema binding supplies it at scan) and carries prose in the
// reserved `body` key.
func createRow(
	root string, rtype *schema.ResolvedType, typeName string,
	meta *omap.Map, body, slug string, explicit bool,
) error {
	row := omap.New()
	for _, k := range meta.Keys() {
		if k == "type" {
			continue
		}
		v, _ := meta.Get(k)
		row.Set(k, v)
	}
	if body != "" {
		row.Set("body", body)
	}
	return mutateCollection(root, rtype, func(rows *omap.Map) (bool, error) {
		if _, taken := rows.Get(slug); taken {
			return false, errs.SlugTaken(slug, typeName, !explicit)
		}
		rows.Set(slug, row)
		return true, nil
	})
}

// partitioned splits raw fields into validated attributes, relation value-lists,
// and extras.
type partitioned struct {
	attrs  *omap.Map // name -> validated value
	rels   *omap.Map // predicate -> []string
	extras *omap.Map // name -> raw string
}

func partition(rtype *schema.ResolvedType, fields *omap.Map, strict bool) (*partitioned, error) {
	part := &partitioned{attrs: omap.New(), rels: omap.New(), extras: omap.New()}
	for _, key := range keysOf(fields) {
		rawAny, _ := fields.Get(key)
		raw := values.Str(rawAny)
		// The discriminator is set by the command, not the caller: a user `type`
		// field disagreeing with the layout type would silently mis-file it.
		if key == "type" && raw != rtype.Name {
			return nil, errs.New("type_field_forbidden", fmt.Sprintf(
				"The 'type' field is set by the command (%s); it cannot be overridden", rtype.Name))
		}
		// On a json/yaml type `body` is the prose channel (the reserved key the
		// reader pops); written as a field it would be clobbered by the next
		// render. md keeps `body` as an ordinary frontmatter field.
		if key == "body" && rtype.Storage.Fmt != "md" {
			return nil, errs.New("body_field_reserved", fmt.Sprintf(
				"'body' is reserved on a %s entity; pass --body/--body-file for prose",
				rtype.Storage.Fmt))
		}
		if attr, isAttr := rtype.Attributes.Get(key); isAttr {
			if strings.TrimSpace(raw) == "" {
				// `--field ""` means "clear it". Store null, not '': null is
				// absent (validate passes, check reports the gap), while '' is
				// malformed by khub's own rule. Mirrors unlink for relations.
				part.attrs.Set(key, nil)
				continue
			}
			value, err := validateAttr(attr, raw, rtype.Storage.Fmt)
			if err != nil {
				return nil, err
			}
			part.attrs.Set(key, value)
			continue
		}
		if _, isRel := rtype.Relations.Get(key); isRel {
			var vals []string
			if raw != "" {
				for _, v := range strings.Split(raw, ",") {
					if t := strings.TrimSpace(v); t != "" {
						vals = append(vals, t)
					}
				}
			}
			// A blank value has no meaning as an edge — refuse it (unlink
			// removes); a comma-list on a single-valued relation used to drop its
			// tail silently — refuse it with the cardinality error link raises.
			if len(vals) == 0 {
				return nil, errs.New("empty_relation_value", fmt.Sprintf(
					"Empty value for relation '%s'; use unlink to remove an edge", key))
			}
			part.rels.Set(key, vals)
			continue
		}
		if strict {
			return nil, errs.StrictUnknownField(key)
		}
		part.extras.Set(key, raw)
	}
	return part, nil
}

// validateAttr validates and coerces a raw string against one attribute's
// type/enum/pattern.
func validateAttr(attr *schema.ResolvedAttribute, raw, storageFmt string) (any, error) {
	if attr.Enum != nil {
		for _, allowed := range attr.Enum {
			if allowed == raw {
				return raw, nil
			}
		}
		return nil, errs.EnumViolation(raw, attr.Name, attr.Enum)
	}
	if attr.Pattern != nil {
		matched, err := attr.MatchPattern(raw)
		if err != nil {
			return nil, err
		}
		if !matched {
			return nil, errs.PatternViolation(raw, attr.Name, *attr.Pattern)
		}
	}
	switch attr.BaseType {
	case "bool":
		return toBool(raw)
	case "number":
		return toNumber(raw, attr.Name)
	case "date", "datetime":
		// Mirror the number gate: reject a value validate would flag (e.g. an
		// impossible 2026-13-45) at write time, not after it has landed.
		if !values.IsDateish(raw) {
			return nil, errs.New("date_violation", fmt.Sprintf(
				"'%s' is not a valid %s for %s", raw, attr.BaseType, attr.Name))
		}
		return asDateObj(raw, storageFmt), nil
	case "list":
		parts := strings.Split(raw, ",")
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = strings.TrimSpace(p)
		}
		return out, nil
	}
	return raw, nil
}

// --- get ---------------------------------------------------------------------

// Get reads one entity by id; edges optionally includes stored and derived edges.
func Get(root, id string, edges bool) (*EntityView, error) {
	views, err := GetMany(root, []string{id}, edges)
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

// GetMany resolves the whole request before reading results, preserving argument
// order and duplicates. All records share one schema/index/collection snapshot.
func GetMany(root string, ids []string, edges bool) ([]*EntityView, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	nodes := make([]index.Node, len(ids))
	for i, id := range ids {
		nodes[i], err = resolveID(idx, id)
		if err != nil {
			return nil, err
		}
	}
	valid := index.Filter(idx, index.StrayNodes(idx))
	views := make([]*EntityView, 0, len(ids))
	cache := map[index.Node]*EntityView{}
	for _, node := range nodes {
		view := cache[node]
		if view == nil {
			view, err = getView(root, idx, node)
			if err != nil {
				return nil, err
			}
			if edges {
				view.Edges = entityEdges(valid, resolved, node, view.Meta)
			}
			cache[node] = view
		}
		views = append(views, view)
	}
	return views, nil
}

func getView(root string, idx *index.Index, node index.Node) (*EntityView, error) {
	resolved := idx.Resolved
	var err error
	rtype, _ := resolved.Types.Get(node.Type)
	path := entityPath(root, rtype, node.Slug)

	var meta *omap.Map
	var body, raw string
	if rtype.Storage.Layout == schema.LayoutCollection {
		rows := idx.Collections[node.Type]
		rowAny, has := rows.Get(node.Slug)
		if !has { // indexed a moment ago; the row vanished mid-command
			return nil, errs.LookupError(node.ID())
		}
		row := rowAny.(*omap.Map)
		meta, body, err = canon.SplitRow(row, rtype.Storage.Fmt)
		if err != nil {
			return nil, err
		}
		if _, hasType := meta.Get("type"); !hasType {
			meta.Set("type", node.Type) // the binding supplies it, as at scan
		}
		raw, err = canon.RenderRow(node.Slug, row, rtype.Storage.Fmt) // the row, never the file
		if err != nil {
			return nil, err
		}
	} else {
		text, rerr := canon.ReadTextIn(root, path)
		if rerr != nil {
			return nil, rerr
		}
		raw = text
		meta, body, err = canon.Parse(raw, canon.FmtOf(path))
		if err != nil {
			return nil, err
		}
	}
	view := &EntityView{
		Type:    node.Type,
		Slug:    node.Slug,
		Path:    path,
		Meta:    meta,
		Body:    body,
		Raw:     raw,
		Locator: locator(rtype, node.Slug),
	}
	return view, nil
}

// --- update ------------------------------------------------------------------

// Update edits id's fields: re-validate, bump `updated`, write a minimal diff.
// `draft` moves only when the user edits it (`edit <id> draft true|false`); no
// completeness recompute, no auto-promote.
func Update(root, id string, opts UpdateOpts) (*UpdateResult, error) {
	return fsio.Locked(root, func() (*UpdateResult, error) { return update(root, id, opts) })
}

func update(root, id string, opts UpdateOpts) (*UpdateResult, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	node, err := resolveID(idx, id)
	if err != nil {
		return nil, err
	}
	rtype, _ := resolved.Types.Get(node.Type)

	// Validate everything before touching the file, so a rejected edit leaves it
	// byte-for-byte unchanged (enum/pattern/strict raise here).
	idx = index.Filter(idx, index.StrayNodes(idx))
	part, err := partition(rtype, opts.Fields, opts.Strict)
	if err != nil {
		return nil, err
	}
	for _, predicate := range part.rels.Keys() {
		rel, _ := rtype.Relations.Get(predicate)
		vals, _ := part.rels.Get(predicate)
		for _, value := range vals.([]string) {
			matches, rerr := resolveWriteTarget(rel, value, idx, predicate, "relation")
			if rerr != nil {
				return nil, rerr
			}
			if matches[node] { // same self-edge gate as link
				return nil, selfLink(id, predicate)
			}
		}
		unique := uniqueTargets(idx, rel, vals.([]string))
		if !rel.Many && len(unique) > 1 {
			return nil, errs.CardinalityViolation(predicate)
		}
		part.rels.Set(predicate, unique)
	}

	path := entityPath(root, rtype, node.Slug)
	apply := func(m *omap.Map) {
		for _, name := range part.attrs.Keys() {
			v, _ := part.attrs.Get(name)
			m.Set(name, v)
		}
		for _, predicate := range part.rels.Keys() {
			rel, _ := rtype.Relations.Get(predicate)
			vals, _ := part.rels.Get(predicate)
			list := append([]string(nil), vals.([]string)...)
			old, _ := m.Get(predicate)
			if old != nil {
				for i, value := range list {
					for _, prior := range asTargetList(old) {
						if sameTarget(idx, rel, prior, value) {
							list[i] = prior
							break
						}
					}
				}
			}
			if rel.Many {
				m.Set(predicate, toAnyList(list))
			} else {
				m.Set(predicate, list[0])
			}
		}
		for _, key := range part.extras.Keys() {
			v, _ := part.extras.Get(key)
			m.Set(key, v)
		}
		// Auto-bump `updated`, unless the user backdated it explicitly in this
		// edit (reconciling an import): their value wins over today.
		_, inAttrs := part.attrs.Get("updated")
		_, inExtras := part.extras.Get("updated")
		if !inAttrs && !inExtras {
			m.Set("updated", today())
		}
	}

	if rtype.Storage.Layout == schema.LayoutCollection {
		newDraft := false
		err := mutateCollection(root, rtype, func(rows *omap.Map) (bool, error) {
			rowAny, has := rows.Get(node.Slug)
			if !has {
				return false, errs.LookupError(id)
			}
			row := rowAny.(*omap.Map)
			apply(row)
			if opts.Body != nil {
				if *opts.Body != "" {
					row.Set("body", *opts.Body)
				} else {
					row.Delete("body") // --body '' clears (empty body writes no key)
				}
			}
			draftAny, _ := row.Get("draft")
			newDraft = values.AsBool(draftAny)
			return true, nil
		})
		if err != nil {
			return nil, err
		}
		return &UpdateResult{
			Type: node.Type, Slug: node.Slug, Path: path, Draft: newDraft,
			Locator: locator(rtype, node.Slug),
		}, nil
	}

	meta, bodyText, err := readDoc(root, path)
	if err != nil {
		return nil, err
	}
	apply(meta)
	if opts.Body != nil {
		bodyText = mdNormalized(*opts.Body, rtype)
	}
	if err := writeDoc(root, path, meta, bodyText); err != nil {
		return nil, err
	}
	// AsBool, not truthiness: a hand-authored draft: "false" must report active,
	// matching how check/query/status read the same flag.
	draftAny, _ := meta.Get("draft")
	return &UpdateResult{
		Type: node.Type, Slug: node.Slug, Path: path, Draft: values.AsBool(draftAny),
	}, nil
}

// --- link / unlink -----------------------------------------------------------

// Link adds a schema-checked edge predicate → target on the source entity. A
// no-op link (the edge already exists) leaves the file untouched and returns
// Changed=false.
func Link(root, id, predicate, target string) (*LinkResult, error) {
	return fsio.Locked(root, func() (*LinkResult, error) { return link(root, id, predicate, target) })
}

func link(root, id, predicate, target string) (*LinkResult, error) {
	resolved, idx, node, rel, err := edgeContext(root, id, predicate)
	if err != nil {
		return nil, err
	}
	matches, err := resolveWriteTarget(rel, target, idx, predicate, "predicate")
	if err != nil {
		return nil, err
	}
	if matches[node] { // a self-edge connects nothing; the graph skips it
		return nil, selfLink(id, predicate)
	}
	target = canonicalTarget(target, matches)

	apply := func(m *omap.Map) (bool, error) {
		existing, _ := m.Get(predicate)
		if rel.Many {
			// A scalar many-value reads as [value], never char-split.
			original := asList(existing)
			list := toAnyList(uniqueTargets(idx, rel, asTargetList(existing)))
			if !truthy(existing) {
				list = []any{}
			}
			changed := len(original) != len(list)
			if !containsTarget(idx, rel, list, target) {
				list = append(list, target)
				changed = true
			}
			m.Set(predicate, list)
			return changed, nil
		}
		current, hasCurrent := "", false
		if truthyScalar(existing) {
			current, hasCurrent = values.Str(existing), true
		}
		if hasCurrent && !sameTarget(idx, rel, current, target) {
			return false, errs.CardinalityViolation(predicate)
		}
		if hasCurrent && sameTarget(idx, rel, current, target) {
			return false, nil
		}
		changed := current != target
		m.Set(predicate, target)
		return changed, nil
	}

	rtype, _ := resolved.Types.Get(node.Type)
	changed, err := applyEdgeMutation(root, rtype, node.Slug, id, apply)
	if err != nil {
		return nil, err
	}
	return &LinkResult{
		Type: node.Type, Slug: node.Slug, Predicate: predicate, Target: target, Changed: changed,
	}, nil
}

func containsTarget(idx *index.Index, rel *schema.ResolvedRelation, list []any, target string) bool {
	for _, v := range list {
		if sameTarget(idx, rel, values.Str(v), target) {
			return true
		}
	}
	return false
}

// Unlink removes the edge predicate → target from the source; inverses
// recompute. A no-op unlink (no such edge) leaves the file untouched and returns
// Changed=false.
func Unlink(root, id, predicate, target string) (*LinkResult, error) {
	return fsio.Locked(root, func() (*LinkResult, error) { return unlink(root, id, predicate, target) })
}

func unlink(root, id, predicate, target string) (*LinkResult, error) {
	resolved, idx, node, rel, err := edgeContext(root, id, predicate)
	if err != nil {
		return nil, err
	}
	apply := func(m *omap.Map) (bool, error) {
		existing, has := m.Get(predicate)
		if rel.Many {
			list := asList(existing)
			hit := false
			remaining := []any{}
			for _, v := range list {
				if sameTarget(idx, rel, values.Str(v), target) {
					hit = true
					continue
				}
				remaining = append(remaining, v)
			}
			if !hit {
				return false, nil
			}
			if len(remaining) > 0 {
				m.Set(predicate, toAnyList(uniqueTargets(idx, rel, asTargetList(remaining))))
			} else {
				m.Delete(predicate)
			}
			return true, nil
		}
		if has && existing != nil && sameTarget(idx, rel, values.Str(existing), target) {
			m.Delete(predicate)
			return true, nil
		}
		return false, nil
	}
	rtype, _ := resolved.Types.Get(node.Type)
	changed, err := applyEdgeMutation(root, rtype, node.Slug, id, apply)
	if err != nil {
		return nil, err
	}
	return &LinkResult{
		Type: node.Type, Slug: node.Slug, Predicate: predicate, Target: target, Changed: changed,
	}, nil
}

// edgeContext is the shared head of link/unlink: schema, index, resolved source
// node, and the predicate's declaration (illegal predicate refuses here).
func edgeContext(root, id, predicate string) (
	*schema.ResolvedSchema, *index.Index, index.Node, *schema.ResolvedRelation, error,
) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, nil, index.Node{}, nil, err
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, nil, index.Node{}, nil, err
	}
	node, err := resolveID(idx, id)
	if err != nil {
		return nil, nil, index.Node{}, nil, err
	}
	rtype, _ := resolved.Types.Get(node.Type)
	rel, ok := rtype.Relations.Get(predicate)
	if !ok {
		return nil, nil, index.Node{}, nil, errs.IllegalPredicate(predicate, node.Type)
	}
	return resolved, index.Filter(idx, index.StrayNodes(idx)), node, rel, nil
}

// applyEdgeMutation runs one link/unlink mutation against per-item or collection
// storage. Per-item: round-trip read, apply, write only when changed (no
// spurious rewrite). Collection: the same mutation on the row inside the locked
// read-modify-write; an unchanged row skips the file write the same way.
func applyEdgeMutation(
	root string, rtype *schema.ResolvedType, slug, id string, apply func(*omap.Map) (bool, error),
) (bool, error) {
	if rtype.Storage.Layout == schema.LayoutCollection {
		changed := false
		err := mutateCollection(root, rtype, func(rows *omap.Map) (bool, error) {
			rowAny, has := rows.Get(slug)
			if !has {
				return false, errs.LookupError(id)
			}
			var aerr error
			changed, aerr = apply(rowAny.(*omap.Map))
			return changed, aerr
		})
		if err != nil {
			return false, err
		}
		return changed, nil
	}
	path := entityPath(root, rtype, slug)
	meta, body, err := readDoc(root, path)
	if err != nil {
		return false, err
	}
	changed, err := apply(meta)
	if err != nil {
		return false, err
	}
	if changed {
		if werr := writeDoc(root, path, meta, body); werr != nil {
			return false, werr
		}
	}
	return changed, nil
}

// --- delete ------------------------------------------------------------------

// Delete removes an entity, refusing while inbound edges resolve unless force.
// A forced removal deletes the entity and leaves the now-dangling inbound edges
// in place — khub check surfaces the breakage; khub never repairs it.
func Delete(root, id string, force bool) (*DeleteResult, error) {
	return fsio.Locked(root, func() (*DeleteResult, error) { return delete(root, id, force) })
}

func delete(root, id string, force bool) (*DeleteResult, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	node, err := resolveID(idx, id)
	if err != nil {
		return nil, err
	}
	rtype, _ := resolved.Types.Get(node.Type)

	inbound := inboundEdges(idx, resolved, node)
	if len(inbound) > 0 && !force {
		return &DeleteResult{Type: node.Type, Slug: node.Slug, Removed: false, Inbound: inbound}, nil
	}

	if rtype.Storage.Layout == schema.LayoutCollection {
		err := mutateCollection(root, rtype, func(rows *omap.Map) (bool, error) {
			if _, has := rows.Get(node.Slug); !has {
				return false, errs.LookupError(id)
			}
			rows.Delete(node.Slug) // an emptied collection keeps its (empty) file
			return true, nil
		})
		if err != nil {
			return nil, err
		}
		return &DeleteResult{Type: node.Type, Slug: node.Slug, Removed: true, Inbound: inbound}, nil
	}
	// Reuse the one path resolver rather than re-deriving: it is the only place
	// that knows a singleton's path IS the file, not a directory to append to.
	path := entityPath(root, rtype, node.Slug)
	if rtype.Storage.Layout == schema.LayoutFolder {
		// The entity is the folder, not just its _index.
		if err := fsio.Remove(root, filepath.Dir(path), true); err != nil {
			return nil, err
		}
	} else if err := fsio.Remove(root, path, false); err != nil {
		return nil, err
	}
	return &DeleteResult{Type: node.Type, Slug: node.Slug, Removed: true, Inbound: inbound}, nil
}

// --- shared ------------------------------------------------------------------

// today is date.today() at the write verbs' one clock seam. KHUB_PARITY_NOW —
// the same env var the parity recorder pins Python's clock with — overrides it.
func today() canon.Date {
	if pinned := os.Getenv("KHUB_PARITY_NOW"); pinned != "" {
		return canon.Date{ISO: pinned}
	}
	return canon.Date{ISO: time.Now().Format("2006-01-02")}
}

// presetName is entity._preset: the workspace's preset for the unknown-type
// message. A missing config.yaml raises here exactly as Python's
// FileNotFoundError does.
func presetName(root string) (string, error) {
	prov, err := workspace.Provenance(root)
	if err != nil {
		return "", err
	}
	name, _ := prov.Get("preset")
	return values.Str(name), nil
}

// asList is a stored many-relation value as a list: [] if blank, itself if a
// list, else [value]. A many-relation authored as a scalar (related: alice) must
// be read as ['alice'] before mutation — iterating the string would split it
// into characters.
func asList(value any) []any {
	if !truthy(value) {
		return []any{}
	}
	if list, ok := value.([]any); ok {
		return append([]any{}, list...)
	}
	return []any{value}
}

// truthyScalar is Python's `existing not in (None, "")` for a single-valued
// relation's current value.
func truthyScalar(v any) bool {
	if v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return s != ""
	}
	return true
}

func toAnyList(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// relSlash renders a workspace-relative path the way Python's
// Path.relative_to(root) prints inside a message.
func relSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
