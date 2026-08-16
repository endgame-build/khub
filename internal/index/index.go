// Package index ports core/index.py — the shared entity scan behind status,
// query, and the write verbs. A snapshot of the entity tree keyed by
// (type, slug): the nodes that exist, the types each bare slug maps to, and
// each node's frontmatter.
package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// Node is the (type, slug) key every projection is built on.
type Node struct{ Type, Slug string }

// ID renders the qualified id carried by every JSON record.
func (n Node) ID() string { return n.Type + "/" + n.Slug }

// Less orders nodes the way Python sorts (type, slug) tuples: field by field,
// never on the joined string (go-port-plan R7).
func (n Node) Less(o Node) bool {
	if n.Type != o.Type {
		return n.Type < o.Type
	}
	return n.Slug < o.Slug
}

func SortNodes(ns []Node) { sort.Slice(ns, func(i, j int) bool { return ns[i].Less(ns[j]) }) }

// Index is a scanned view of the entity tree. Order holds the scan order
// (schema declaration order × sorted glob) so downstream projections that rely
// on insertion order — FTS rowids, adjacency — stay deterministic.
type Index struct {
	Resolved    *schema.ResolvedSchema
	Nodes       map[Node]bool
	Order       []Node
	TypesBySlug map[string]map[string]bool
	Meta        map[Node]*omap.Map
	Malformed   []string // workspace-relative, sorted

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
	idx := &Index{
		Resolved:    resolved,
		Nodes:       map[Node]bool{},
		TypesBySlug: map[string]map[string]bool{},
		Meta:        map[Node]*omap.Map{},
	}
	var malformed []string
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		pairs, bad, err := ScanType(root, rtype)
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

// Pair is one scanned entity: its slug and frontmatter.
type Pair struct {
	Slug string
	Meta *omap.Map
}

// ScanType returns the (slug, frontmatter) pairs stored for one type, plus its
// malformed files. A single unparseable file becomes a malformed entry instead
// of raising — validate/check/query/status/get never crash on one bad file.
func ScanType(root string, rtype *schema.ResolvedType) ([]Pair, []string, error) {
	switch rtype.Storage.Layout {
	case "collection":
		return scanCollection(root, rtype)
	case "singleton":
		spath := filepath.Join(root, storagePath(rtype))
		if !isFile(spath) {
			return nil, nil, nil
		}
		meta := loadMeta(spath)
		if meta == nil {
			return nil, []string{spath}, nil
		}
		return []Pair{{Slug: rtype.Name, Meta: meta}}, nil, nil
	}
	if storagePath(rtype) == "" {
		return nil, nil, nil
	}
	base := filepath.Join(root, storagePath(rtype))
	if _, err := os.Stat(base); err != nil {
		return nil, nil, nil
	}
	var out []Pair
	var malformed []string
	ext := rtype.Storage.Fmt
	if rtype.Storage.Layout == "folder" {
		dirs, _ := os.ReadDir(base)
		var matches []string
		for _, d := range dirs {
			if d.IsDir() {
				matches = append(matches, filepath.Join(base, d.Name(), "_index."+ext))
			}
		}
		for _, idxPath := range matches {
			if !isFile(idxPath) {
				continue
			}
			meta := loadMeta(idxPath)
			if meta == nil {
				malformed = append(malformed, idxPath)
				continue
			}
			out = append(out, Pair{Slug: filepath.Base(filepath.Dir(idxPath)), Meta: meta})
		}
		return out, malformed, nil
	}
	// os.ReadDir over filepath.Glob + os.Stat: the directory entry already
	// carries the file type, so the regular-file check is free. The Stat was
	// 55% of the scan's syscalls on a 10k-entity workspace — one extra syscall
	// per file, purely to re-learn what readdir had already reported.
	// ReadDir returns entries sorted by filename, the order Glob+sort produced.
	entries, _ := os.ReadDir(base)
	suffix := "." + ext
	for _, e := range entries {
		name := e.Name()
		if name == "_index"+suffix || !strings.HasSuffix(name, suffix) || !isRegular(base, e) {
			continue
		}
		f := filepath.Join(base, name)
		meta := loadMeta(f)
		if meta == nil {
			malformed = append(malformed, f)
			continue
		}
		out = append(out, Pair{Slug: strings.TrimSuffix(name, suffix), Meta: meta})
	}
	return out, malformed, nil
}

func scanCollection(root string, rtype *schema.ResolvedType) ([]Pair, []string, error) {
	cpath := filepath.Join(root, rtype.CollectionRelpath())
	if !isFile(cpath) {
		return nil, nil, nil
	}
	text, err := canon.ReadText(cpath)
	if err != nil {
		return nil, []string{cpath}, nil
	}
	rows, err := canon.LoadCollection(text, rtype.Storage.Fmt)
	if err != nil {
		return nil, []string{cpath}, nil
	}
	var out []Pair
	for _, slug := range rows.Keys() {
		rowAny, _ := rows.Get(slug)
		row, ok := rowAny.(*omap.Map)
		if !ok {
			return nil, []string{cpath}, nil
		}
		meta, _, err := canon.SplitRow(row, rtype.Storage.Fmt)
		if err != nil {
			return nil, []string{cpath}, nil
		}
		if _, has := meta.Get("type"); !has {
			meta.Set("type", rtype.Name)
		}
		out = append(out, Pair{Slug: slug, Meta: meta})
	}
	return out, nil, nil
}

// loadMeta is the scan-altitude read: metadata, or nil if unparseable (the
// malformed-file contract — try_parse guards ANY read/parse failure).
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

// isRegular reports whether a directory entry is a regular file. A symlink is
// resolved with a Stat, matching Python's Path.is_file(); everything else is
// answered from the readdir entry with no syscall.
func isRegular(dir string, e os.DirEntry) bool {
	if e.Type().IsRegular() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false // directory, socket, device: not an entity, not malformed
	}
	return isFile(filepath.Join(dir, e.Name()))
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func storagePath(rtype *schema.ResolvedType) string {
	if rtype.Storage.Path == nil {
		return ""
	}
	return *rtype.Storage.Path
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
		Nodes:       map[Node]bool{},
		TypesBySlug: map[string]map[string]bool{},
		Meta:        map[Node]*omap.Map{},
		Malformed:   idx.Malformed,
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
