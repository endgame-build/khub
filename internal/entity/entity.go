// Port of src/khub/core/entity.py — the write surface over the graph (FS-002).
//
// create and get are the foundational pair: mint a typed entity as one document
// (slug minting, layout resolution, field/enum/pattern validation, referential-
// integrity hard-fail) and read one back (id resolution, ambiguity detection,
// read-time inverse-edge derivation). update/link/unlink/delete extend them.
//
// `status: draft` is the manual publish flag: add writes it only for --draft,
// and `edit <id> status draft|stable` moves it. An absent status reads as the
// base default, stable, and is never written. khub never derives it from
// completeness — capture is never blocked, and an active-but-incomplete entity
// is surfaced by check, not by this flag.
//
// Every verb is schema-generic: it introspects the compiled schema at runtime
// and has no per-type code path. The schema and git are the only gates.

package entity

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/clock"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/trust"
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
		raw, _ := part.rels.Get(predicate)
		vals, err := aliasTargets(idx, rel, raw.([]string))
		if err != nil {
			return nil, err
		}
		for _, value := range vals {
			if _, err := resolveWriteTarget(rel, value, idx, predicate, "relation"); err != nil {
				return nil, err
			}
		}
		unique := uniqueTargets(idx, rel, vals)
		if !rel.Many && len(unique) > 1 {
			return nil, errs.CardinalityViolation(predicate)
		}
		part.rels.Set(predicate, unique)
	}

	// --draft writes `status: draft`. An explicit --status that says otherwise
	// is a contradiction, refused rather than settled by either flag.
	if status, explicit := part.attrs.Get("status"); opts.Draft {
		if explicit && status != values.StatusDraft {
			return nil, errs.StatusDraftConflict(values.Str(status))
		}
		part.attrs.Set("status", values.StatusDraft)
	}

	stamp(rtype, part.attrs, opts.Actor)

	meta := omap.New()
	meta.Set("type", typeName)
	meta.Set("created", today())
	meta.Set("updated", today())
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
	isDraft := rtype.IsDraft(meta)

	explicit := opts.ID != ""
	slug := typeName // a singleton's slug IS its type name
	if rtype.Storage.Layout == schema.LayoutSingleton {
		// An --id saying otherwise is a mistake.
		if explicit && opts.ID != typeName {
			return nil, errs.New("singleton_id", fmt.Sprintf(
				"'%s' is a singleton — its id is always '%s' (drop --id)", typeName, typeName))
		}
	} else {
		// Minted after the frontmatter is assembled, so a --created override
		// dates the id. Minting reads no siblings, so the same name or title
		// mints the same slug twice: placeNew's index pre-check names the
		// refusal cheaply, and the exclusive create is the real gate against a
		// concurrent add. Neither an explicit nor a minted slug is ever suffixed.
		if slug, err = chooseSlug(rtype, meta, opts.ID); err != nil {
			return nil, err
		}
	}
	naming := errs.NamedByID
	if !explicit {
		naming = errs.Minted
	}
	path, err := placeNew(root, resolved, idx, rtype, slug, meta, naming)
	if err != nil {
		return nil, err
	}
	if rtype.Storage.Layout == schema.LayoutCollection {
		if err := createRow(root, rtype, typeName, meta, body, slug, naming); err != nil {
			return nil, err
		}
		return &CreateResult{
			Type:    typeName,
			Slug:    slug,
			Path:    path,
			Draft:   isDraft,
			Locator: locator(rtype, slug),
		}, nil
	}
	if err := fsio.MkdirAll(root, filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := writeNew(root, path, meta, body); err != nil {
		if errors.Is(err, fs.ErrExist) {
			if rtype.Storage.Layout == schema.LayoutSingleton {
				return nil, singletonExists(root, typeName, path)
			}
			return nil, errs.SlugTaken(slug, typeName, naming)
		}
		return nil, err
	}
	return &CreateResult{Type: typeName, Slug: slug, Path: path, Draft: isDraft}, nil
}

// placeNew is the file a new entity of rtype under slug is written to, or the
// gate that refuses it: a taken singleton, slug or alias, a Windows device
// name, a reserved OKF navigation name, an ignored path. add and import both
// pass through it, so an import lands only where an add could. naming says
// how the slug was chosen, which the refusals' advice depends on. A
// collection row's file is the collection's, and its slug is gated again by
// the in-lock read that inserts it.
func placeNew(
	root string, resolved *schema.ResolvedSchema, idx *index.Index,
	rtype *schema.ResolvedType, slug string, meta *omap.Map, naming errs.Naming,
) (string, error) {
	node := index.Node{Type: rtype.Name, Slug: slug}
	switch rtype.Storage.Layout {
	case schema.LayoutSingleton:
		path := entityPath(root, idx, node)
		if idx.Nodes[node] {
			return "", singletonExists(root, rtype.Name, path)
		}
		return path, refuseIgnored(root, path, resolved.Ignore, rtype.Name)
	case schema.LayoutCollection:
		path := collectionFile(root, rtype)
		if err := refuseIgnored(root, path, resolved.Ignore, rtype.Name); err != nil {
			return "", err
		}
		if idx.Nodes[node] {
			return "", errs.SlugTaken(slug, rtype.Name, naming)
		}
		return path, aliasTaken(idx, slug, naming)
	}
	// The slug is the file stem (file layout) or the folder name (folder
	// layout), so a Windows device name is refused before anything is written.
	if schema.ReservedDeviceName(slug) {
		return "", errs.ReservedSlug(slug, rtype.Name, naming)
	}
	if idx.Nodes[node] {
		return "", errs.SlugTaken(slug, rtype.Name, naming)
	}
	if err := aliasTaken(idx, slug, naming); err != nil {
		return "", err
	}
	path := entityPath(root, idx, node)
	if rtype.Templated() {
		rel, err := templatedPath(rtype, slug, meta)
		if err != nil {
			return "", err
		}
		path = filepath.Join(root, filepath.FromSlash(rel))
	}
	// index.md and log.md are OKF navigation, never read back as an entity. A
	// folder's _index.md is not.
	if name := filepath.Base(path); schema.ReservedEntityName(name) {
		return "", errs.NavigationSlug(slug, rtype.Name, name, naming)
	}
	return path, refuseIgnored(root, path, resolved.Ignore, rtype.Name)
}

func singletonExists(root, typeName, path string) *errs.Located {
	return errs.New("singleton_exists", fmt.Sprintf(
		"Singleton '%s' already exists at %s; edit it instead of adding another",
		typeName, relSlash(root, path)))
}

// aliasTaken refuses a new slug that is already another entity's alias: the
// name is taken for that entity, and a second one under it is a duplicate.
func aliasTaken(idx *index.Index, slug string, naming errs.Naming) error {
	if owners := idx.AliasOwners(slug); len(owners) > 0 {
		return errs.AliasTaken(slug, owners[0].ID(), naming)
	}
	return nil
}

// createRow inserts one new row into a collection under slug.
//
// Uniqueness is gated by the fresh in-lock read, not the index: a taken slug
// refuses, minted or explicit — nothing is suffixed. The row omits `type` (the
// collection's schema binding supplies it at scan) and carries prose in the
// reserved `body` key.
func createRow(
	root string, rtype *schema.ResolvedType, typeName string,
	meta *omap.Map, body, slug string, naming errs.Naming,
) error {
	row := newRow(meta, body)
	return mutateCollection(root, rtype, func(rows *omap.Map) (bool, error) {
		if _, taken := rows.Get(slug); taken {
			return false, errs.SlugTaken(slug, typeName, naming)
		}
		rows.Set(slug, row)
		return true, nil
	})
}

// newRow is a collection row for an entity: its fields without `type`, and
// its body in the reserved `body` key.
func newRow(meta *omap.Map, body string) *omap.Map {
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
	return row
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
		// `draft` was the publish flag before `status` carried it.
		if key == "draft" && !rtype.Declares(key) {
			return nil, errs.DraftRetired()
		}
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
	// A mapping has no command-line spelling: storing the string would replace
	// the object with text.
	if attr.HoldsObjects() {
		return nil, errs.ValueShape(raw, attr.Name, attr.BaseType == "list")
	}
	if attr.Enum != nil {
		for _, allowed := range attr.Enum {
			if allowed == raw {
				return raw, nil
			}
		}
		return nil, errs.EnumViolation(raw, attr.Name, attr.Enum)
	}
	if attr.Pattern != nil && !attr.MatchPattern(raw) {
		return nil, errs.PatternViolation(raw, attr.Name, *attr.Pattern)
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
			return nil, errs.DateViolation(raw, attr.BaseType, attr.Name)
		}
		// A datetime that declares a pattern (the OKF datetimes) keeps the text
		// the caller wrote: the pattern fixed its shape, and normalizing would
		// trade ISO 8601's `T` for the space form the pattern then refuses.
		if attr.BaseType == "datetime" && attr.Pattern != nil {
			return canon.DateTime{ISO: raw}, nil
		}
		return asDateObj(raw, storageFmt), nil
	case "list":
		parts := strings.Split(raw, ",")
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = strings.TrimSpace(p)
			if attr.Items != nil {
				v, err := validateAttr(attr.Items, out[i].(string), storageFmt)
				if err != nil {
					return nil, err
				}
				out[i] = v
			}
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
	path := entityPath(root, idx, node)

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
		Trust:   trust.Tier(rtype, meta, body),
	}
	return view, nil
}

// --- update ------------------------------------------------------------------

// Update edits id's fields: re-validate, bump `updated`, write a minimal diff.
// `status` moves only when the user edits it (`edit <id> status draft|stable`);
// no completeness recompute, no auto-promote.
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
		raw, _ := part.rels.Get(predicate)
		vals, err := aliasTargets(idx, rel, raw.([]string))
		if err != nil {
			return nil, err
		}
		for _, value := range vals {
			matches, rerr := resolveWriteTarget(rel, value, idx, predicate, "relation")
			if rerr != nil {
				return nil, rerr
			}
			if matches[node] { // same self-edge gate as link
				return nil, selfLink(id, predicate)
			}
		}
		unique := uniqueTargets(idx, rel, vals)
		if !rel.Many && len(unique) > 1 {
			return nil, errs.CardinalityViolation(predicate)
		}
		part.rels.Set(predicate, unique)
	}

	path := entityPath(root, idx, node)
	apply := func(m *omap.Map) {
		for _, name := range part.attrs.Keys() {
			v, _ := part.attrs.Get(name)
			m.Set(name, v)
		}
		// Writing `status` settles the publish state, so a retired `draft`
		// flag left beside it goes in the same write: the entity is migrated,
		// and upgrade no longer sees two disagreeing flags.
		if _, writesStatus := part.attrs.Get("status"); writesStatus && !rtype.OwnsDraft() {
			m.Delete("draft")
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
	}
	// settle finishes an edit of m (body is its body after the edit). An edit
	// that changed nothing writes nothing. Otherwise `updated` is bumped,
	// unless the user backdated it explicitly in this edit (reconciling an
	// import), and `generated` is stamped only when content moved: a tag or
	// other bookkeeping edit changes the file without a new writer.
	settle := func(m *omap.Map, before, beforeRev, body string) bool {
		if snapshot(m, body) == before {
			return false
		}
		_, inAttrs := part.attrs.Get("updated")
		_, inExtras := part.extras.Get("updated")
		if !inAttrs && !inExtras {
			m.Set("updated", today())
		}
		if trust.Rev(rtype, m, body) != beforeRev {
			stamp(rtype, m, opts.Actor)
		}
		return true
	}

	if rtype.Storage.Layout == schema.LayoutCollection {
		newDraft := false
		err := mutateCollection(root, rtype, func(rows *omap.Map) (bool, error) {
			rowAny, has := rows.Get(node.Slug)
			if !has {
				return false, errs.LookupError(id)
			}
			row := rowAny.(*omap.Map)
			before, beforeRev := snapshot(row, ""), trust.Rev(rtype, row, rowBody(row))
			apply(row)
			if opts.Body != nil {
				if *opts.Body != "" {
					row.Set("body", *opts.Body)
				} else {
					row.Delete("body") // --body '' clears (empty body writes no key)
				}
			}
			newDraft = rtype.IsDraft(row)
			return settle(row, before, beforeRev, rowBody(row)), nil
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
	before, beforeRev := snapshot(meta, bodyText), trust.Rev(rtype, meta, bodyText)
	apply(meta)
	if opts.Body != nil {
		bodyText = mdNormalized(*opts.Body, rtype)
	}
	if settle(meta, before, beforeRev, bodyText) {
		if err := writeDoc(root, path, meta, bodyText); err != nil {
			return nil, err
		}
	}
	return &UpdateResult{
		Type: node.Type, Slug: node.Slug, Path: path, Draft: rtype.IsDraft(meta),
	}, nil
}

// --- link / unlink -----------------------------------------------------------

// Link adds a schema-checked edge predicate → target on the source entity. A
// no-op link (the edge already exists) leaves the file untouched and returns
// Changed=false. A change is stamped as actor (see Actor); "" walks the ladder.
func Link(root, id, predicate, target, actor string) (*LinkResult, error) {
	return fsio.Locked(root, func() (*LinkResult, error) { return link(root, id, predicate, target, actor) })
}

func link(root, id, predicate, target, actor string) (*LinkResult, error) {
	_, idx, node, rel, err := edgeContext(root, id, predicate)
	if err != nil {
		return nil, err
	}
	if target, err = aliasTarget(idx, rel, target); err != nil {
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

	changed, err := applyEdgeMutation(root, idx, node, id, actor, apply)
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
// Changed=false. A change is stamped as actor, as Link's is.
func Unlink(root, id, predicate, target, actor string) (*LinkResult, error) {
	return fsio.Locked(root, func() (*LinkResult, error) { return unlink(root, id, predicate, target, actor) })
}

func unlink(root, id, predicate, target, actor string) (*LinkResult, error) {
	_, idx, node, rel, err := edgeContext(root, id, predicate)
	if err != nil {
		return nil, err
	}
	// An alias names its owner's edge, reported by the owner's slug as link
	// reports it; the literal spelling still matches, so a hand-written alias
	// value on disk can be unlinked too.
	literal := target
	if target, err = aliasTarget(idx, rel, target); err != nil {
		return nil, err
	}
	matchesTarget := func(v string) bool {
		return sameTarget(idx, rel, v, target) || sameTarget(idx, rel, v, literal)
	}
	apply := func(m *omap.Map) (bool, error) {
		existing, has := m.Get(predicate)
		if rel.Many {
			list := asList(existing)
			hit := false
			remaining := []any{}
			for _, v := range list {
				if matchesTarget(values.Str(v)) {
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
		if has && existing != nil && matchesTarget(values.Str(existing)) {
			m.Delete(predicate)
			return true, nil
		}
		return false, nil
	}
	changed, err := applyEdgeMutation(root, idx, node, id, actor, apply)
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
	root string, idx *index.Index, node index.Node, id, actor string, edit func(*omap.Map) (bool, error),
) (bool, error) {
	rtype, _ := idx.Resolved.Types.Get(node.Type)
	// A no-op touches nothing. A change is stamped only when it moved
	// content; tidying an existing list without a new target moves none. The
	// body is never touched here, so the revision compares frontmatter alone.
	apply := func(m *omap.Map) (bool, error) {
		before := trust.Rev(rtype, m, "")
		changed, err := edit(m)
		if changed && err == nil && trust.Rev(rtype, m, "") != before {
			stamp(rtype, m, actor)
		}
		return changed, err
	}
	if rtype.Storage.Layout == schema.LayoutCollection {
		changed := false
		err := mutateCollection(root, rtype, func(rows *omap.Map) (bool, error) {
			rowAny, has := rows.Get(node.Slug)
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
	path := entityPath(root, idx, node)
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
	path := entityPath(root, idx, node)
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

// today is the clock's date as a date value, so YAML writes it unquoted.
func today() canon.Date { return canon.Date{ISO: clock.Today()} }

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
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
