package entity

// Resolving a file path to the entity stored there, for `get --path`.
//
// The mapping reads the scan, never a type name: the index records the file
// of every per-item and singleton entity, a folder-layout entity owns its
// directory, and a collection type names its one file. Matching is lexical,
// byte for byte against those scanned paths, which hold no symlink, and the
// path is then opened the way storage operations open one, so a symlink on
// the way is refused rather than followed.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/schema"
)

// GetManyByPath is GetMany over file paths: each path resolves to the entity
// stored there. A relative path is read from base, the caller's working
// directory, as any file argument is; an absolute one stands alone. Either
// must land inside the workspace. A path that holds no entity, lies outside
// the workspace or names a collection file refuses the whole batch before any
// record is read.
func GetManyByPath(root, base string, paths []string, edges bool) ([]*EntityView, error) {
	return getMany(root, paths, edges, func(idx *index.Index) func(string) (index.Node, error) {
		return newPathResolver(root, base, idx).resolve
	})
}

// pathResolver maps paths to nodes for one scan.
type pathResolver struct {
	root, base string
	idx        *index.Index
	folders    map[string]index.Node // a folder-layout entity's directory
}

func newPathResolver(root, base string, idx *index.Index) *pathResolver {
	r := &pathResolver{root: root, base: base, idx: idx, folders: map[string]index.Node{}}
	for _, n := range idx.Order {
		p, ok := idx.Paths[n]
		if !ok { // a collection row has no file of its own
			continue
		}
		if rtype, _ := idx.Resolved.Types.Get(n.Type); rtype.Storage.Layout == schema.LayoutFolder {
			if dir := path.Dir(p); r.folders[dir] == (index.Node{}) {
				r.folders[dir] = n
			}
		}
	}
	return r
}

// resolve is the node arg names: the entity whose file it is, or, for a
// folder-layout entity, its directory or any file under it.
func (r *pathResolver) resolve(arg string) (index.Node, error) {
	rel, err := r.relative(arg)
	if err != nil {
		return index.Node{}, err
	}
	info, err := fsio.Stat(r.root, filepath.Join(r.root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fsio.NotADirectory) {
		// A file where the path wants a directory is the caller's path, not
		// one khub built: it names nothing, as a missing file does.
		return index.Node{}, errs.PathNotEntity(rel, "the file does not exist")
	}
	if err != nil {
		return index.Node{}, err
	}
	if n, ok := r.idx.FileNode(rel); ok {
		return n, nil
	}
	if err := r.collection(rel); err != nil {
		return index.Node{}, err
	}
	if n, ok := r.folderOf(rel); ok {
		return n, nil
	}
	switch {
	case slices.Contains(r.idx.Malformed, rel):
		return index.Node{}, errs.PathNotEntity(rel, "the file is malformed; khub check reports it")
	case r.idx.Resolved.Ignore.Ignored(rel, info.IsDir()):
		return index.Node{}, errs.PathNotEntity(rel, "it is ignored by .khub/ignore")
	}
	if scanned, ok := r.scannedSpelling(rel); ok {
		return index.Node{}, errs.PathNotEntity(rel,
			fmt.Sprintf("paths match byte for byte as scanned; khub scanned this one as '%s'", scanned))
	}
	if info.IsDir() {
		return index.Node{}, errs.PathNotEntity(rel, "it is a directory")
	}
	return index.Node{}, errs.PathNotEntity(rel, "no entity is stored in this file")
}

// folderOf is the folder-layout entity whose directory holds rel, the deepest
// one when folders nest.
func (r *pathResolver) folderOf(rel string) (index.Node, bool) {
	for d := rel; d != "."; d = path.Dir(d) {
		if n, ok := r.folders[d]; ok {
			return n, true
		}
	}
	return index.Node{}, false
}

// scannedSpelling is the scanned path rel spells in another case, for a file
// a case-insensitive filesystem found under a spelling the scan does not
// hold. Only case is caught: a Unicode normalization difference is not.
func (r *pathResolver) scannedSpelling(rel string) (string, bool) {
	for _, n := range r.idx.Order {
		p, ok := r.idx.Paths[n]
		if !ok {
			continue
		}
		if strings.EqualFold(p, rel) {
			return p, true
		}
		if dir := path.Dir(p); r.folders[dir] == n && len(rel) >= len(dir) &&
			strings.EqualFold(rel[:len(dir)], dir) && (len(rel) == len(dir) || rel[len(dir)] == '/') {
			return dir + rel[len(dir):], true
		}
	}
	return "", false
}

// collection refuses rel when it is a collection type's file: one path names
// one entity, and that file holds a row per entity.
func (r *pathResolver) collection(rel string) error {
	for _, tname := range r.idx.Resolved.Types.Keys() {
		rtype, _ := r.idx.Resolved.Types.Get(tname)
		if rtype.Storage.Layout != schema.LayoutCollection || path.Clean(rtype.CollectionRelpath()) != rel {
			continue
		}
		var ids []string
		for _, n := range r.idx.Order {
			if n.Type == tname {
				ids = append(ids, n.ID())
			}
		}
		switch {
		case slices.Contains(r.idx.Malformed, rel):
			return errs.PathNotEntity(rel, "the file is malformed; khub check reports it")
		case len(ids) == 0:
			return errs.PathNotEntity(rel, "the "+tname+" collection holds no entities")
		}
		return errs.PathIsCollection(rel, tname, ids)
	}
	return nil
}

// relative is arg as a clean workspace-relative slash path. A backslash
// separates as a slash does, as in a path reference.
func (r *pathResolver) relative(arg string) (string, error) {
	p := filepath.FromSlash(strings.ReplaceAll(arg, `\`, "/"))
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.base, p)
	}
	p = filepath.Clean(p)
	if rel, err := filepath.Rel(r.root, p); err == nil && filepath.IsLocal(rel) {
		return filepath.ToSlash(rel), nil
	}
	if rel, ok := underRootDir(r.root, p); ok {
		return rel, nil
	}
	return "", errs.PathOutsideWorkspace(arg, r.root)
}

// underRootDir is p relative to its outermost ancestor that is the workspace
// directory itself, for an absolute path that spells the root another way
// (macOS /tmp for /private/tmp). The walk runs from the top down and stops at
// the first such directory, so a symlink inside the workspace that points back
// at the root (`ln -s . self`) stays in the remainder, where resolve's
// fsio.Stat refuses it.
func underRootDir(root, p string) (string, bool) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return "", false
	}
	var chain []string
	for d := p; ; {
		chain = append(chain, d)
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	for i := len(chain) - 1; i >= 0; i-- {
		d := chain[i]
		if info, err := os.Stat(d); err == nil && os.SameFile(info, rootInfo) {
			rel, err := filepath.Rel(d, p)
			if err != nil || !filepath.IsLocal(rel) {
				return "", false
			}
			return filepath.ToSlash(rel), true
		}
	}
	return "", false
}
