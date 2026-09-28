// Package index ports core/index.py — the shared entity scan behind status,
// query, and the write verbs. A snapshot of the entity tree keyed by
// (type, slug): the nodes that exist, the types each bare slug maps to, and
// each node's frontmatter.
package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// Node is the (type, slug) key every projection is built on.
type Node struct{ Type, Slug string }

// ID renders the qualified id carried by every JSON record.
func (n Node) ID() string { return n.Type + "/" + n.Slug }

// Less orders nodes the way Python sorts (type, slug) tuples: field by field,
// never on the joined string.
func (n Node) Less(o Node) bool {
	if n.Type != o.Type {
		return n.Type < o.Type
	}
	return n.Slug < o.Slug
}

// SortNodes orders nodes by (type, slug), the graph's ordering rule.
func SortNodes(ns []Node) { sort.Slice(ns, func(i, j int) bool { return ns[i].Less(ns[j]) }) }

// Index is a scanned view of the entity tree. Order holds the scan order
// (schema declaration order × sorted glob) so downstream projections that rely
// on insertion order — FTS rowids, adjacency — stay deterministic.
type Index struct {
	Resolved    *schema.ResolvedSchema
	Collections map[string]*omap.Map // parsed inventories, reused by batch reads
	Nodes       map[Node]bool
	Order       []Node
	TypesBySlug map[string]map[string]bool
	Meta        map[Node]*omap.Map
	Body        map[Node]string // nil unless built by BuildWithBodies
	Malformed   []string        // workspace-relative, sorted

	// folded maps casefolded slug -> stored slugs, built once on first
	// case-insensitive miss. Without it CanonicalSlug casefolded EVERY stored
	// slug per miss, which made the read path quadratic: on a 5000-entity
	// workspace that was 428 misses x 5000 slugs = 2.1M casefolds, 19% of CPU.
	foldOnce sync.Once
	folded   map[string][]string
}

// foldIndex builds (once) the casefolded slug lookup.
func (idx *Index) foldIndex() map[string][]string {
	idx.foldOnce.Do(func() {
		idx.folded = make(map[string][]string, len(idx.TypesBySlug))
		for s := range idx.TypesBySlug {
			f := casefold(s)
			idx.folded[f] = append(idx.folded[f], s)
		}
	})
	return idx.folded
}

// CanonicalSlug returns the stored slug matching case-insensitively when
// exactly one does. Exact match always wins; this is only the fallback (so
// `add --id CMP-001-Api` and `get CMP-001-Api` cannot disagree).
func (idx *Index) CanonicalSlug(slug string) (string, bool) {
	if _, ok := idx.TypesBySlug[slug]; ok {
		return slug, true
	}
	if hits := idx.foldIndex()[casefold(slug)]; len(hits) == 1 {
		return hits[0], true
	}
	return "", false
}

// ResolveTarget returns the nodes a relation value resolves to.
func (idx *Index) ResolveTarget(rel *schema.ResolvedRelation, target string) map[Node]bool {
	return resolveTarget(idx, rel, target)
}

// Build scans every declared type into an Index.
func Build(root string, resolved *schema.ResolvedSchema) (*Index, error) {
	return build(root, resolved, false)
}

// BuildWithBodies is Build that also keeps each entity's body, for search.
// Other reads leave bodies to the garbage collector.
func BuildWithBodies(root string, resolved *schema.ResolvedSchema) (*Index, error) {
	return build(root, resolved, true)
}

func build(root string, resolved *schema.ResolvedSchema, bodies bool) (*Index, error) {
	idx := &Index{
		Resolved:    resolved,
		Collections: map[string]*omap.Map{},
		Nodes:       map[Node]bool{},
		TypesBySlug: map[string]map[string]bool{},
		Meta:        map[Node]*omap.Map{},
	}
	if bodies {
		idx.Body = map[Node]string{}
	}
	var malformed []string
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		pairs, bad, err := scanType(root, rtype, idx.Collections)
		if err != nil {
			return nil, err
		}
		for _, p := range pairs {
			node := Node{Type: tname, Slug: p.Slug}
			idx.Nodes[node] = true
			idx.Order = append(idx.Order, node)
			if idx.TypesBySlug[p.Slug] == nil {
				idx.TypesBySlug[p.Slug] = map[string]bool{}
			}
			idx.TypesBySlug[p.Slug][tname] = true
			idx.Meta[node] = p.Meta
			if bodies {
				idx.Body[node] = p.Body
			}
		}
		malformed = append(malformed, bad...)
	}
	rel := make([]string, 0, len(malformed))
	for _, p := range malformed {
		r, err := filepath.Rel(root, p)
		if err != nil {
			r = p
		}
		rel = append(rel, filepath.ToSlash(r))
	}
	sort.Strings(rel)
	idx.Malformed = rel
	return idx, nil
}

// Pair is one scanned entity: its slug, frontmatter and body.
type Pair struct {
	Slug string
	Meta *omap.Map
	Body string
}

// ScanType returns the (slug, frontmatter) pairs stored for one type, plus its
// malformed files. A single unparseable file becomes a malformed entry instead
// of raising — validate/check/query/status/get never crash on one bad file.
func ScanType(root string, rtype *schema.ResolvedType) ([]Pair, []string, error) {
	return scanType(root, rtype, nil)
}
func scanType(root string, rtype *schema.ResolvedType, collections map[string]*omap.Map) ([]Pair, []string, error) {
	base := filepath.Join(root, rtype.StorageRelpath())
	r, baseRel, err := fsio.OpenPath(root, base)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	info, err := r.Stat(baseRel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if rtype.Storage.Layout == schema.LayoutCollection {
		return scanCollection(root, rtype, collections)
	}
	if rtype.Storage.Layout == schema.LayoutSingleton {
		if !info.Mode().IsRegular() {
			return nil, nil, &fs.PathError{Op: "scan", Path: base, Err: fs.ErrInvalid}
		}
		raw, err := r.ReadFile(baseRel)
		if err != nil {
			return nil, nil, err
		}
		res := parseEntry(raw, base, rtype.Name, rtype.Storage.Fmt)
		if res.malformed != "" {
			return nil, []string{res.malformed}, nil
		}
		return []Pair{res.pair}, nil, nil
	}
	entries, err := fsio.ReadDirIn(r, baseRel, base)
	if err != nil {
		return nil, nil, err
	}
	// One Root scoped to the type directory. Reading through the workspace
	// Root re-opened every path component per file (three openat calls for
	// knowledge/requirements/req-x.md), which was two thirds of a query's time
	// at 2000 entities. From here a file-layout read is one component and a
	// folder-layout read two; the leaf still opens O_NOFOLLOW, and the
	// directory descriptor is pinned, so a swap after OpenPath validated it
	// cannot redirect a read.
	dir, err := r.OpenRoot(baseRel)
	if err != nil {
		return nil, nil, err
	}
	defer dir.Close()

	// Entries are read and parsed concurrently. Each result lands at its
	// entry's index and is merged in listing order, so Order, Malformed and
	// the first error reported are the same as a serial scan's.
	results := make([]entryResult, len(entries))
	each(len(entries), func(i int) {
		results[i] = scanEntry(dir, root, base, rtype, entries[i])
	})
	var out []Pair
	var malformed []string
	for _, res := range results {
		switch {
		case res.err != nil:
			return nil, nil, res.err
		case res.skip:
		case res.malformed != "":
			malformed = append(malformed, res.malformed)
		default:
			out = append(out, res.pair)
		}
	}
	return out, malformed, nil
}

// entryResult is one directory entry's scan outcome: a pair, a malformed
// path, a skip, or an error that aborts the whole scan.
type entryResult struct {
	pair      Pair
	malformed string
	skip      bool
	err       error
}

// scanEntry reads and parses one entry of a type directory. It depends on
// nothing but the entry, so entries may be scanned in any order.
func scanEntry(dir *os.Root, root, base string, rtype *schema.ResolvedType, entry fs.DirEntry) entryResult {
	name := entry.Name()
	path := filepath.Join(base, name)
	if entry.Type()&os.ModeSymlink != 0 {
		return entryResult{err: errs.New("unsafe_path", fmt.Sprintf("Symlinks are not allowed in workspace storage: %s", path))}
	}
	slug := strings.TrimSuffix(name, "."+rtype.Storage.Fmt)
	inDir := name
	if rtype.Storage.Layout == schema.LayoutFolder {
		if !entry.IsDir() {
			return entryResult{skip: true}
		}
		slug = name
		inDir = filepath.Join(name, "_index."+rtype.Storage.Fmt)
		path = filepath.Join(base, inDir)
		st, err := dir.Lstat(inDir)
		if errors.Is(err, fs.ErrNotExist) {
			return entryResult{skip: true}
		}
		if err != nil {
			return entryResult{err: rootRelative(err, root, path)}
		}
		if !st.Mode().IsRegular() {
			return entryResult{err: &fs.PathError{Op: "scan", Path: path, Err: fs.ErrInvalid}}
		}
	} else {
		rel, _ := filepath.Rel(root, path)
		if !rtype.AcceptsEntityPath(rel) {
			return entryResult{skip: true}
		}
		if !entry.Type().IsRegular() {
			return entryResult{err: &fs.PathError{Op: "scan", Path: path, Err: fs.ErrInvalid}}
		}
	}
	raw, err := dir.ReadFile(inDir)
	if err != nil {
		return entryResult{err: rootRelative(err, root, path)}
	}
	return parseEntry(raw, path, slug, rtype.Storage.Fmt)
}

// parseEntry parses one entity file. A parse failure marks the file malformed
// instead of failing the scan.
func parseEntry(raw []byte, path, slug, format string) entryResult {
	meta, body, err := canon.Parse(canon.NormalizeNewlines(string(raw)), format)
	if err != nil {
		return entryResult{malformed: path}
	}
	return entryResult{pair: Pair{Slug: slug, Meta: meta, Body: body}}
}

// each calls fn(0..n-1) on up to GOMAXPROCS goroutines and waits for all of
// them. fn must write only to state owned by its own index.
func each(n int, fn func(i int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), n) {
		wg.Go(func() {
			for i := int(next.Add(1) - 1); i < n; i = int(next.Add(1) - 1) {
				fn(i)
			}
		})
	}
	wg.Wait()
}

// rootRelative re-anchors a PathError from the type-directory Root to the
// workspace-relative path the workspace Root would have reported, so error
// text does not depend on which Root performed the read.
func rootRelative(err error, root, path string) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		if rel, rerr := filepath.Rel(root, path); rerr == nil {
			return &fs.PathError{Op: pe.Op, Path: rel, Err: pe.Err}
		}
	}
	return err
}

func scanCollection(root string, rtype *schema.ResolvedType, collections map[string]*omap.Map) ([]Pair, []string, error) {
	cpath := filepath.Join(root, rtype.CollectionRelpath())
	text, err := canon.ReadTextIn(root, cpath)
	if err != nil {
		return nil, nil, err
	}
	rows, err := canon.LoadCollection(text, rtype.Storage.Fmt)
	if err != nil {
		return nil, []string{cpath}, nil
	}
	if collections != nil {
		collections[rtype.Name] = rows
	}
	var out []Pair
	for _, slug := range rows.Keys() {
		rowAny, _ := rows.Get(slug)
		row, ok := rowAny.(*omap.Map)
		if !ok {
			return nil, []string{cpath}, nil
		}
		meta, body, err := canon.SplitRow(row, rtype.Storage.Fmt)
		if err != nil {
			return nil, []string{cpath}, nil
		}
		if _, has := meta.Get("type"); !has {
			meta.Set("type", rtype.Name)
		}
		out = append(out, Pair{Slug: slug, Meta: meta, Body: body})
	}
	return out, nil, nil
}

func resolveTarget(idx *Index, rel *schema.ResolvedRelation, target string) map[Node]bool {
	nodes, typesBySlug := idx.Nodes, idx.TypesBySlug
	out := map[Node]bool{}
	if strings.Contains(target, "/") {
		parts := strings.SplitN(target, "/", 2)
		t, s := parts[0], parts[1]
		if !nodes[Node{Type: t, Slug: s}] {
			canonSlug, ok := idx.CanonicalSlug(s)
			if !ok {
				return out
			}
			match := ""
			var candidates []string
			for ct := range typesBySlug[canonSlug] {
				candidates = append(candidates, ct)
			}
			sort.Strings(candidates)
			for _, ct := range candidates {
				if casefold(ct) == casefold(t) {
					match = ct
					break
				}
			}
			if match == "" {
				return out
			}
			t, s = match, canonSlug
		}
		if rel.Kind == "any" || contains(rel.Targets, t) {
			out[Node{Type: t, Slug: s}] = true
		}
		return out
	}
	if canonSlug, ok := idx.CanonicalSlug(target); ok {
		target = canonSlug
	}
	if rel.Kind == "any" {
		for t := range typesBySlug[target] {
			out[Node{Type: t, Slug: target}] = true
		}
		return out
	}
	for _, t := range rel.Targets {
		n := Node{Type: t, Slug: target}
		if nodes[n] {
			out[n] = true
		}
	}
	return out
}

// StrayNodes are scanned files whose internal `type` does not match their
// layout type — derived from the existing scan, no second walk.
func StrayNodes(idx *Index) map[Node]bool {
	out := map[Node]bool{}
	for node, meta := range idx.Meta {
		v, _ := meta.Get("type")
		s, _ := v.(string)
		if s != node.Type {
			out[node] = true
		}
	}
	return out
}

// Filter returns a view of idx with drop nodes removed.
func Filter(idx *Index, drop map[Node]bool) *Index {
	if len(drop) == 0 {
		return idx
	}
	out := &Index{
		Resolved:    idx.Resolved,
		Collections: idx.Collections,
		Nodes:       map[Node]bool{},
		TypesBySlug: map[string]map[string]bool{},
		Meta:        map[Node]*omap.Map{},
		Malformed:   idx.Malformed,
	}
	if idx.Body != nil {
		out.Body = map[Node]string{}
	}
	for _, n := range idx.Order {
		if drop[n] {
			continue
		}
		out.Nodes[n] = true
		out.Order = append(out.Order, n)
		if out.TypesBySlug[n.Slug] == nil {
			out.TypesBySlug[n.Slug] = map[string]bool{}
		}
		out.TypesBySlug[n.Slug][n.Type] = true
		out.Meta[n] = idx.Meta[n]
		if idx.Body != nil {
			out.Body[n] = idx.Body[n]
		}
	}
	return out
}

// RejectMalformed refuses to derive a written projection from a scan that
// dropped files (reindex/viz would otherwise emit an artifact missing a whole
// type and exit 0).
func RejectMalformed(idx *Index, verb string) error {
	if len(idx.Malformed) == 0 {
		return nil
	}
	shown := idx.Malformed
	if len(shown) > 3 {
		shown = shown[:3]
	}
	more := ""
	if len(idx.Malformed) > 3 {
		more = fmt.Sprintf(" (+%d more)", len(idx.Malformed)-3)
	}
	return errs.New("malformed_projection", fmt.Sprintf(
		"Refusing to %s: %d file(s) could not be parsed, so the graph is incomplete — %s%s. "+
			"Run `khub check` for the list.", verb, len(idx.Malformed), strings.Join(shown, ", "), more))
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
