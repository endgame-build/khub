// Package okfimport reads an OKF bundle into a workspace: `khub import --okf`.
//
// Import is the permissive consume side (OKF §11). An external bundle's
// producer never saw this workspace's schema, so a concept lands as a draft
// in the type its `type` maps to, its frontmatter kept as written — unknown
// keys, nested values and key order included — and its body unchanged.
// Nothing is written back to the bundle, and nothing is sent anywhere.
//
// Three things are quarantined rather than trusted. The bundle's `verified`
// moves to `okf_verified`, kept verbatim and never counted by trust, because
// this workspace's reviewers have not reviewed it; `generated` stays as the
// producer's provenance claim. A relation value no entity answers to moves to
// `okf_unresolved`, so no dangling edge is written. And `status` becomes
// `draft`.
//
// A real run holds the workspace lock from the first read to the last write.
// It plans every concept first, to a fixed point, then writes the ones that
// land, all or none. A refusal of the whole run (no bundle, a bundle that
// overlaps the workspace, a bad type map, a write that fails) leaves the
// workspace as it was; a concept that cannot land is skipped and reported,
// and never stops the others.
package okfimport

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// Options controls one import. TypeMap is the path of a YAML mapping from
// OKF `type` strings to khub type names, or "". Actor stamps `generated` on a
// concept that carries none; "" walks the actor ladder.
type Options struct {
	TypeMap, Actor string
	DryRun         bool
}

// Imported is one concept that landed, or would land. ID is its qualified
// khub id; Alias the bundle stem kept in `aliases` when the id differs;
// AliasDropped the stem left out of `aliases` because another entity already
// answers to it; StatusWas the published `status` it had before it became a
// draft; VerifiedQuarantined that its `verified` moved to `okf_verified`.
type Imported struct {
	Concept, ID, Path, OKFType, Alias, AliasDropped, StatusWas string
	VerifiedQuarantined                                        bool
}

// Skipped is one concept that did not land, with a code and the reason.
type Skipped struct {
	Concept, Reason, Message string
}

// Unresolved is one relation value no entity answers to, moved to
// `okf_unresolved`.
type Unresolved struct {
	Concept, Field, Value string
}

// Links counts the links import read: resolved to an imported concept,
// broken (a local concept path that no imported concept holds), and external.
type Links struct {
	Resolved, Broken, External int
}

// Result is one import. Every slice is empty, never nil.
type Result struct {
	Imported        []Imported
	Skipped         []Skipped
	Unresolved      []Unresolved
	ReferencesAdded int
	Links           Links
}

// concept is one parsed bundle document on its way into the workspace.
type concept struct {
	id, okfType, statusWas string
	quarantined            bool
	raw                    entity.RawEntity
	related                []relatedEntry
	node                   index.Node
}

type relatedEntry struct {
	predicate, dest string
}

// Import reads the bundle at bundle into the workspace at root.
func Import(root, bundle string, opt Options) (*Result, error) {
	if opt.DryRun {
		return run(root, bundle, opt)
	}
	return fsio.Locked(root, func() (*Result, error) { return run(root, bundle, opt) })
}

func run(root, bundle string, opt Options) (*Result, error) {
	if err := refuseBundle(root, bundle); err != nil {
		return nil, err
	}
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	typeMap, err := loadTypeMap(opt.TypeMap, resolved)
	if err != nil {
		return nil, err
	}
	res := &Result{Imported: []Imported{}, Skipped: []Skipped{}, Unresolved: []Unresolved{}}
	// The root itself may be a link to the bundle; only links inside it are
	// refused.
	walkRoot, err := filepath.EvalSymlinks(bundle)
	if err != nil {
		return nil, errs.ImportBundleUnreadable(bundle, "cannot be read: "+err.Error())
	}
	var concepts []*concept
	err = walkConcepts(walkRoot, func(id, file string, err error) {
		switch {
		case errors.Is(err, errSymlink):
			res.Skipped = append(res.Skipped, Skipped{id, "symlink", err.Error()})
			return
		case err != nil:
			res.Skipped = append(res.Skipped, Skipped{id, "unreadable", err.Error()})
			return
		}
		c, skip := parseConcept(id, file, resolved, typeMap)
		if skip != nil {
			res.Skipped = append(res.Skipped, *skip)
			return
		}
		concepts = append(concepts, c)
	})
	if err != nil {
		return nil, err
	}

	// Pass one plans every concept's id. Links resolve only to concepts that
	// land.
	placed, err := plan(root, concepts, opt.Actor, res)
	if err != nil {
		return nil, err
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	nodes := map[string]index.Node{} // bundle concept id -> node
	types := map[string]map[string]bool{}
	for slug, ts := range scanned.TypesBySlug {
		types[slug] = maps(ts)
	}
	for _, c := range placed {
		nodes[c.id] = c.node
		if types[c.node.Slug] == nil {
			types[c.node.Slug] = map[string]bool{}
		}
		types[c.node.Slug][c.node.Type] = true
	}
	spell := func(n index.Node) string {
		if len(types[n.Slug]) > 1 {
			return n.ID() // a bare slug two types share would be ambiguous
		}
		return n.Slug
	}
	for _, c := range placed {
		resolveLinks(c, resolved, nodes, spell, res)
	}

	// Pass two plans again with the links added, and writes. The links name
	// only concepts pass one placed, so nothing new is refused; what pass two
	// reports is what was written.
	items := make([]entity.RawEntity, len(placed))
	for i, c := range placed {
		items[i] = c.raw
	}
	results, err := entity.WriteRawHeld(root, items, opt.Actor, opt.DryRun)
	if err != nil {
		return nil, err
	}
	for i, r := range results {
		c := placed[i]
		if r.Err != nil {
			res.Skipped = append(res.Skipped, skipFor(c.id, r.Err))
			continue
		}
		for _, field := range r.Unresolved.Keys() {
			lost, _ := r.Unresolved.Get(field)
			for _, v := range lost.([]string) {
				res.Unresolved = append(res.Unresolved, Unresolved{c.id, field, v})
			}
		}
		rel, _ := filepath.Rel(root, r.Path)
		res.Imported = append(res.Imported, Imported{
			Concept: c.id, ID: c.node.ID(), Path: filepath.ToSlash(rel), OKFType: c.okfType,
			Alias: r.Alias, AliasDropped: r.AliasDropped, StatusWas: c.statusWas, VerifiedQuarantined: c.quarantined,
		})
	}
	return res, nil
}

func maps(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// plan keeps the concepts the workspace accepts, each with its id, and
// reports the others.
func plan(root string, concepts []*concept, actor string, res *Result) ([]*concept, error) {
	items := make([]entity.RawEntity, len(concepts))
	for i, c := range concepts {
		items[i] = c.raw
	}
	results, err := entity.WriteRawHeld(root, items, actor, true)
	if err != nil {
		return nil, err
	}
	var out []*concept
	for i, r := range results {
		c := concepts[i]
		if r.Err != nil {
			res.Skipped = append(res.Skipped, skipFor(c.id, r.Err))
			continue
		}
		c.node = index.Node{Type: c.raw.Type, Slug: r.Slug}
		// The stem goes into `aliases` now, ahead of the edges links add.
		if r.Alias != "" {
			cur, _ := c.raw.Meta.Get("aliases")
			c.raw.Meta.Set("aliases", append(slices.Clone(values.Items(cur)), r.Alias))
		}
		out = append(out, c)
	}
	return out, nil
}

func skipFor(id string, err error) Skipped {
	var l *errs.Located
	if errors.As(err, &l) {
		return Skipped{id, l.Code, l.Message}
	}
	return Skipped{id, "unwritable", err.Error()}
}

// refuseBundle refuses a bundle that is not a readable directory, or that
// overlaps the workspace: inside it, the workspace already holds it; holding
// it, import would read the workspace's own files as concepts.
func refuseBundle(root, bundle string) error {
	info, err := os.Stat(bundle)
	if err != nil {
		return errs.ImportBundleUnreadable(bundle, "cannot be read: "+err.Error())
	}
	if !info.IsDir() {
		return errs.ImportBundleUnreadable(bundle, "is not a directory")
	}
	overlap, err := fsio.Overlap(root, bundle)
	if err != nil {
		return err
	}
	if overlap {
		return errs.ImportBundleOverlaps(bundle)
	}
	return nil
}

// loadTypeMap reads the --type-map file: a YAML mapping of OKF type strings
// to declared khub type names. An entry naming no declared type refuses the
// run, since every concept of that type would otherwise be skipped quietly.
func loadTypeMap(file string, resolved *schema.ResolvedSchema) (map[string]string, error) {
	out := map[string]string{}
	if file == "" {
		return out, nil
	}
	text, err := canon.ReadText(file)
	if err != nil {
		return nil, errs.ImportTypeMapInvalid(file, "cannot be read: "+err.Error())
	}
	m, nonMap, err := canon.LoadDocMap(text, canon.Mode12)
	if err != nil || nonMap != nil {
		return nil, errs.ImportTypeMapInvalid(file, "is not a YAML mapping")
	}
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		s, ok := v.(string)
		if !ok || s == "" {
			return nil, errs.ImportTypeMapInvalid(file, fmt.Sprintf("maps '%s' to something other than a type name", k))
		}
		if !resolved.Types.Has(s) {
			return nil, errs.ImportTypeMapInvalid(file, fmt.Sprintf("maps '%s' to '%s', which this workspace does not declare", k, s))
		}
		out[k] = s
	}
	return out, nil
}

// errSymlink marks a link inside the bundle, file or directory, which import
// never follows: a bundle is untrusted input, and a link could reach outside it.
var errSymlink = errors.New("a symbolic link; links inside a bundle are never followed")

// walkConcepts visits every concept document in the bundle in lexical order:
// each `.md` that is not a reserved name (index.md, log.md), outside hidden
// directories. A link, to a file or a directory, is reported, never followed.
func walkConcepts(bundle string, visit func(id, file string, err error)) error {
	return filepath.WalkDir(bundle, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(bundle, p)
		rel = filepath.ToSlash(rel)
		if err != nil {
			if rel == "." {
				return err
			}
			visit(strings.TrimSuffix(rel, ".md"), "", err)
			return nil
		}
		if rel != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			visit(strings.TrimSuffix(rel, ".md"), "", errSymlink)
			return nil
		}
		if d.IsDir() || path.Ext(rel) != ".md" || schema.ReservedEntityName(d.Name()) {
			return nil
		}
		id := strings.TrimSuffix(rel, ".md")
		if !d.Type().IsRegular() {
			visit(id, "", errors.New("not a regular file"))
			return nil
		}
		visit(id, p, nil)
		return nil
	})
}

// parseConcept reads one document and maps it onto a khub type, or says why
// it cannot land.
func parseConcept(id, file string, resolved *schema.ResolvedSchema, typeMap map[string]string) (*concept, *Skipped) {
	text, err := readConcept(file)
	if errors.Is(err, errSymlink) {
		return nil, &Skipped{id, "symlink", err.Error()}
	}
	if err != nil {
		return nil, &Skipped{id, "unreadable", err.Error()}
	}
	yamlText, body, err := canon.SplitFrontmatter(text)
	if err != nil {
		return nil, &Skipped{id, "no_frontmatter", "The document has no `---` frontmatter block, so it is not an OKF concept"}
	}
	meta, nonMap, err := canon.LoadDocMap(yamlText, canon.Mode12)
	if err != nil || nonMap != nil {
		msg := "The frontmatter is not a YAML mapping"
		if err != nil {
			msg = "The frontmatter is not valid YAML: " + strings.SplitN(err.Error(), "\n", 2)[0]
		}
		return nil, &Skipped{id, "malformed_frontmatter", msg}
	}
	okfType, _ := meta.Get("type")
	t, isStr := okfType.(string)
	if !isStr || strings.TrimSpace(t) == "" {
		return nil, &Skipped{id, "missing_type", "The frontmatter has no `type`, which OKF requires"}
	}
	khubType, ok := mapType(t, resolved, typeMap)
	if !ok {
		return nil, &Skipped{id, "unmapped_type", fmt.Sprintf(
			"OKF type '%s' is not a type of this workspace; map it with --type-map", t)}
	}
	rtype, _ := resolved.Types.Get(khubType)
	c := &concept{id: id, okfType: t}
	if stripped, entries, ok := splitRelated(body, rtype); ok {
		body, c.related = stripped, entries
	}
	// A key import writes must not be in the concept already: writing it
	// would overwrite what the producer put there.
	var renames []canon.Rename
	if _, has := meta.Get("verified"); has {
		if _, taken := meta.Get("okf_verified"); taken {
			return nil, quarantineTaken(id, "okf_verified", "its `verified`")
		}
		meta = renamed(meta, "verified", "okf_verified")
		renames = append(renames, canon.Rename{Path: []string{"verified"}, To: "okf_verified"})
		c.quarantined = true
	}
	// The OKF type is kept when it differs, appended rather than placed after
	// `type`, so the concept's own formatting and comments splice through.
	meta.Set("type", khubType)
	if t != khubType {
		if _, taken := meta.Get("okf_type"); taken {
			return nil, quarantineTaken(id, "okf_type", "its OKF type")
		}
		meta.Set("okf_type", t)
	}
	// Everything imported is a draft: an external producer's `stable` is not
	// this workspace's review.
	if s, _ := meta.Get("status"); s != values.StatusDraft {
		if str, ok := s.(string); ok {
			c.statusWas = str
		}
		meta.Set("status", values.StatusDraft)
	}
	stem := path.Base(id)
	if stem == "_index" && path.Dir(id) != "." {
		stem = path.Base(path.Dir(id)) // a folder entity exported as <slug>/_index.md
	}
	c.raw = entity.RawEntity{
		Type: khubType, Stem: stem, Meta: meta, Body: body, Source: text, Renames: renames,
		Quarantine: "okf_unresolved",
	}
	return c, nil
}

func quarantineTaken(id, key, what string) *Skipped {
	l := errs.QuarantineTaken(key, what)
	return &Skipped{id, l.Code, l.Message}
}

// openFile opens a concept file; a test swaps the file between the check and
// the open through it.
var openFile = os.Open

// readConcept reads a concept file the walk found, refusing it if it is no
// longer that file: a regular file swapped for a link between the walk's
// look and this read would otherwise be followed. The file is checked before
// it is opened and again once it is open, so the two are the same file.
func readConcept(file string) (string, error) {
	before, err := os.Lstat(file)
	if err != nil {
		return "", err
	}
	if fsio.IsLink(before) {
		return "", errSymlink
	}
	f, err := openFile(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(before, opened) {
		return "", errSymlink
	}
	if !opened.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, canon.MaxTextBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > canon.MaxTextBytes {
		return "", errs.TextTooLarge(filepath.ToSlash(file), len(raw), canon.MaxTextBytes)
	}
	return canon.NormalizeNewlines(string(raw)), nil
}

// mapType is the khub type an OKF type lands in: the type map's entry, else a
// declared type of that name or of its slug.
func mapType(t string, resolved *schema.ResolvedSchema, typeMap map[string]string) (string, bool) {
	if mapped, ok := typeMap[t]; ok {
		return mapped, true // loadTypeMap checked it is declared
	}
	for _, name := range []string{t, entity.Slugify(t)} {
		if resolved.Types.Has(name) {
			return name, true
		}
	}
	return "", false
}

func renamed(m *omap.Map, from, to string) *omap.Map {
	out := omap.New()
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		if k == from {
			k = to
		}
		out.Set(k, v)
	}
	return out
}

// relatedLine and relatedLink are the entry forms `khub export` writes:
// `- <predicate>: [Title](/bundle/path.md)` for a resolved target and
// `- <predicate>: <value>` for one it could not resolve.
var (
	relatedLine = regexp.MustCompile(`^- ([^:\s]+): (.*)$`)
	relatedLink = regexp.MustCompile(`^\[(?:[^\]\\]|\\.)*\]\((</[^>]*>|/[^)\s]*)\)$`)
)

// splitRelated recognises the `# Related` section export appends: the last
// section of the body, every entry in export's form and every predicate one
// the mapped type declares. Only then is it stripped and its entries
// returned; anything else is the producer's own prose and stays in the body,
// its links read as ordinary links.
func splitRelated(body string, rtype *schema.ResolvedType) (string, []relatedEntry, bool) {
	const marker = "\n# Related\n\n"
	i := strings.LastIndex(body, marker)
	if i < 0 || (i > 0 && body[i-1] != '\n') {
		return body, nil, false
	}
	var entries []relatedEntry
	for _, line := range strings.Split(strings.TrimRight(body[i+len(marker):], "\n"), "\n") {
		m := relatedLine.FindStringSubmatch(line)
		if m == nil || !rtype.Relations.Has(m[1]) {
			return body, nil, false
		}
		e := relatedEntry{predicate: m[1]}
		if l := relatedLink.FindStringSubmatch(m[2]); l != nil {
			e.dest = strings.Trim(l[1], "<>")
		} else if strings.Contains(m[2], "](") {
			return body, nil, false // a link, but not export's
		}
		entries = append(entries, e)
	}
	return body[:i], entries, true
}

// mdLink is an inline markdown link; an image (a leading `!`) is skipped.
var mdLink = regexp.MustCompile(`(!?)\[(?:[^\]\\]|\\.)*\]\(\s*(<[^>]*>|[^)\s]+)(?:\s+(?:"[^"]*"|'[^']*'))?\s*\)`)

// resolveLinks reads the concept's body links, its `sources[].resource`
// values and its `# Related` entries. One that names another imported concept
// becomes an edge: its predicate for a Related entry, else `references`. The
// body and the sources stay as written. spell is how an edge names a node.
func resolveLinks(c *concept, resolved *schema.ResolvedSchema, nodes map[string]index.Node,
	spell func(index.Node) string, res *Result) {
	rtype, _ := resolved.Types.Get(c.raw.Type)
	var refs []index.Node
	count := func(dest string) (index.Node, bool) {
		target, kind := classify(c.id, dest, nodes)
		switch kind {
		case "resolved":
			res.Links.Resolved++
			return target, true
		case "broken":
			res.Links.Broken++
		case "external":
			res.Links.External++
		}
		return index.Node{}, false
	}
	for _, m := range mdLink.FindAllStringSubmatch(c.raw.Body, -1) {
		if m[1] == "!" {
			continue
		}
		if t, ok := count(strings.Trim(m[2], "<>")); ok {
			refs = append(refs, t)
		}
	}
	srcs, _ := c.raw.Meta.Get("sources")
	for _, s := range values.Items(srcs) {
		if sm, ok := s.(*omap.Map); ok {
			if r, ok := sm.Get("resource"); ok {
				if t, ok := count(values.Str(r)); ok {
					refs = append(refs, t)
				}
			}
		}
	}
	for _, e := range c.related {
		if e.dest == "" {
			res.Links.Broken++ // export lists an unresolved target as plain text
			continue
		}
		if t, ok := count(e.dest); ok {
			rel, _ := rtype.Relations.Get(e.predicate)
			addEdge(c.raw.Meta, e.predicate, spell(t), rel.Many)
		}
	}
	if !rtype.Relations.Has("references") {
		return
	}
	for _, t := range refs {
		if t != c.node && addEdge(c.raw.Meta, "references", spell(t), true) {
			res.ReferencesAdded++
		}
	}
}

// classify reads one link destination from the concept at id: an absolute
// URL is external; a local `.md` path (bundle-absolute or relative, anchor and
// query dropped, percent-escapes decoded, backslashes read as separators)
// resolves to an imported
// concept's node, or is broken when it names none or leaves the bundle.
// Other local targets (a directory, a reserved file, a non-md file, a bare
// anchor) are not concept links and are not counted.
func classify(id, dest string, nodes map[string]index.Node) (index.Node, string) {
	if strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") {
		return index.Node{}, "external"
	}
	dest, _, _ = strings.Cut(strings.ReplaceAll(dest, `\`, "/"), "#")
	dest, _, _ = strings.Cut(dest, "?")
	if decoded, err := url.PathUnescape(dest); err == nil {
		dest = decoded // `my%20notes.md` names `my notes.md`
	}
	if path.Ext(dest) != ".md" || schema.ReservedEntityName(path.Base(dest)) {
		return index.Node{}, ""
	}
	p := path.Join(path.Dir(id), dest)
	if strings.HasPrefix(dest, "/") {
		p = strings.TrimPrefix(dest, "/")
	}
	clean, ok := index.LocalRef(p)
	if !ok {
		return index.Node{}, "broken"
	}
	if n, ok := nodes[strings.TrimSuffix(clean, ".md")]; ok {
		return n, "resolved"
	}
	return index.Node{}, "broken"
}

// addEdge appends target to the relation field. A single-valued field that is
// already set keeps its value. Two spellings of one target are folded later,
// by resolution, when the entity is written. Reports whether it appended.
func addEdge(meta *omap.Map, field, target string, many bool) bool {
	cur, has := meta.Get(field)
	list := values.Items(cur)
	for _, v := range list {
		if values.Str(v) == target {
			return false
		}
	}
	if !many {
		if has && cur != nil {
			return false
		}
		meta.Set(field, target)
		return true
	}
	meta.Set(field, append(append([]any{}, list...), target))
	return true
}
