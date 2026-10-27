package index

// Scanning a templated type, and path references.
//
// A templated type (schema.PathTemplate) is walked template by template: a
// literal segment is one lookup, a placeholder segment lists its directory.
// A matching file is this type's entity only when its frontmatter `type`
// names the type, so types sharing a directory each take their own files and
// leave the rest. Symlinked directories are never followed; a symlinked file
// a template matches is refused, as in a plain layout.

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/schema"
)

// scanTemplated returns the pairs a templated type holds, its malformed files
// and its duplicates, and records where each entity lives (SetLocated).
func scanTemplated(root string, rtype *schema.ResolvedType) ([]Pair, []string, []Duplicate, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, nil, err
	}
	defer r.Close()
	var pairs []Pair
	var malformed []string
	var dups []Duplicate
	located := map[string]string{}
	for _, tp := range rtype.Storage.Templates {
		var files []string
		if err := walkTemplate(r, root, tp, 0, "", &files); err != nil {
			return nil, nil, nil, err
		}
		sort.Strings(files)
		for _, rel := range files {
			caps, ok := tp.Match(rel)
			if !ok {
				continue
			}
			raw, err := r.ReadFile(filepath.FromSlash(rel))
			if err != nil {
				return nil, nil, nil, err
			}
			abs := filepath.Join(root, filepath.FromSlash(rel))
			res := parseEntry(raw, abs, caps[schema.SlugField], rtype.Storage.Fmt)
			if res.malformed != "" {
				malformed = append(malformed, res.malformed)
				continue
			}
			if t, _ := res.pair.Meta.Get("type"); t != rtype.Name {
				continue
			}
			slug := res.pair.Slug
			if holder, taken := located[slug]; taken {
				if holder != rel {
					dups = append(dups, Duplicate{Path: rel, Type: rtype.Name, Holder: holder})
				}
				continue
			}
			located[slug] = rel
			res.pair.Rel = rel
			pairs = append(pairs, res.pair)
		}
	}
	rtype.SetLocated(located)
	return pairs, malformed, dups, nil
}

// walkTemplate collects the files under dir (workspace-relative, slash) that
// segments i.. of tp can reach.
func walkTemplate(r *os.Root, root string, tp *schema.PathTemplate, i int, dir string, out *[]string) error {
	leaf := i == tp.Depth()-1
	visit := func(name string) error {
		rel := path.Join(dir, name)
		info, err := r.Lstat(filepath.FromSlash(rel))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fsio.IsLink(info) {
			if leaf {
				return fsio.RefuseLink(info, filepath.Join(root, filepath.FromSlash(rel)))
			}
			return nil
		}
		if leaf {
			if info.Mode().IsRegular() {
				*out = append(*out, rel)
			}
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		return walkTemplate(r, root, tp, i+1, rel, out)
	}
	if lit, ok := tp.Literal(i); ok {
		return visit(lit)
	}
	listing := dir
	if listing == "" {
		listing = "."
	}
	f, err := r.Open(filepath.FromSlash(listing))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return err
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	for _, e := range entries {
		if !tp.AcceptsName(i, e.Name()) {
			continue
		}
		if err := visit(e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// cleanRef is a path reference as a workspace-relative slash path: an
// anchor and a leading `./` dropped. ok is false for a value that cannot be
// a path inside the workspace.
func cleanRef(target string) (string, bool) {
	target, _, _ = strings.Cut(target, "#")
	target = strings.TrimSpace(strings.ReplaceAll(target, `\`, "/"))
	if target == "" {
		return "", false
	}
	clean := path.Clean(target)
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", false
	}
	return clean, true
}

// pathNode is the node whose file target names.
func (idx *Index) pathNode(target string) (Node, bool) {
	rel, ok := cleanRef(target)
	if !ok {
		return Node{}, false
	}
	idx.pathOnce.Do(func() {
		idx.byPath = make(map[string]Node, len(idx.Paths))
		for _, n := range idx.Order {
			if p, has := idx.Paths[n]; has {
				idx.byPath[p] = n
			}
		}
	})
	n, ok := idx.byPath[rel]
	return n, ok
}

// ResolvesToFile reports whether target, which names no entity, names a file
// or directory in the workspace that satisfies rel without an edge: a path
// reference (rel.ByPath) on an `any` relation.
func (idx *Index) ResolvesToFile(rel *schema.ResolvedRelation, target string) bool {
	if !rel.ByPath || rel.Kind != schema.KindAny || idx.Root == "" {
		return false
	}
	clean, ok := cleanRef(target)
	if !ok {
		return false
	}
	_, err := fsio.Stat(idx.Root, filepath.Join(idx.Root, filepath.FromSlash(clean)))
	return err == nil
}

// Satisfies reports whether a relation value resolves: to an entity, or, by
// path, to a file.
func (idx *Index) Satisfies(rel *schema.ResolvedRelation, target string) bool {
	return len(idx.ResolveTarget(rel, target)) > 0 || idx.ResolvesToFile(rel, target)
}
