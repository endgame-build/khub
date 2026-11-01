// Package export renders a workspace as an OKF 0.2 bundle: `khub export --okf`.
//
// The bundle holds the workspace's entities and nothing else. It is built from
// the scan, never from the file tree, so `.khub/`, the agent wiring files
// (CLAUDE.md, AGENTS.md), the workspace's own index.md, reference docs and
// every ignored path stay out by construction. A stray — a file in a type's
// layout whose `type` names another type — is not an entity either, and is
// listed rather than dropped silently.
//
// Each entity becomes one concept at a path mirroring its workspace path:
//
//   - an md entity is copied as written, frontmatter comments and formatting
//     included, since it is already an OKF concept;
//   - a json or yaml entity becomes `<path>.md`, and a collection row
//     `<collection path without its extension>/<slug>.md`, with the entity's
//     fields as frontmatter emitted in the canon safe profile (width 4096: a
//     long scalar stays on one line, which a foreign parser was seen to lose
//     when folded) and its `body` as the body.
//
// The type's declared relations are appended as one `# Related` section of
// bundle-absolute links (§6.1). A value that resolves to no concept is listed
// as plain text, since OKF tolerates broken links; inverse edges are derived,
// never stored, so they are not written. Every directory gets an §8 index.md,
// and the root one declares `okf_version`. No log.md is written.
//
// The bundle is built in a staging directory beside the target and swapped
// in at the end, so a failed export leaves the target as it was. A hidden
// `.khub-export` at the bundle root lists every file khub wrote; `--force`
// replaces a non-empty target only when that list accounts for everything in
// it, so a workspace, a repository or a user's own file is never deleted.
package export

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/mdlink"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/reindex"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// OKFVersion is the OKF version the bundle declares in its root index.md, the
// one reindex stamps.
const OKFVersion = reindex.OKFVersion

const indexName = "index.md"

// Options controls one export. Force replaces a non-empty target that holds
// a previous export and nothing else. DryRun plans the bundle and writes
// nothing.
type Options struct {
	Force, DryRun bool
}

// Result lists what an export wrote, or would write: bundle-relative,
// slash-separated paths, concepts in scan order and indexes sorted.
// SkippedStrays lists the workspace-relative files the scan read as strays.
// CleanupLeft lists the replaced bundle when --force swapped in the new one
// but could not delete the old: the export succeeded, and that directory is
// left for the user to remove.
type Result struct {
	Concepts      []string
	Indexes       []string
	SkippedStrays []string
	CleanupLeft   []string
}

// file is one planned bundle file; node is the entity a concept holds.
type file struct {
	path, text string
	node       index.Node
}

// Export writes the bundle for the workspace at root into target.
func Export(root, target string, opt Options) (*Result, error) {
	overlap, err := fsio.Overlap(root, target)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, errs.ExportTargetOverlaps(target)
	}
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	scanned, err := index.BuildWithBodies(root, resolved)
	if err != nil {
		return nil, err
	}
	// A bundle missing a whole unparseable file would pass for complete.
	if err := index.RejectMalformed(scanned, "export"); err != nil {
		return nil, err
	}
	strays := index.StrayNodes(scanned)
	idx := index.Filter(scanned, strays)
	concepts, err := planConcepts(root, resolved, idx)
	if err != nil {
		return nil, err
	}
	indexes, err := planIndexes(resolved, idx, concepts)
	if err != nil {
		return nil, err
	}
	// A target that is a link is replaced where it points, so the swap
	// replaces the bundle and leaves the link as it was.
	if info, err := os.Lstat(target); err == nil && fsio.IsLink(info) {
		if target, err = filepath.EvalSymlinks(target); err != nil {
			return nil, err
		}
	}
	replace, err := checkTarget(target, opt.Force)
	if err != nil {
		return nil, err
	}
	res := &Result{
		Concepts: []string{}, Indexes: []string{}, SkippedStrays: strayPaths(scanned, strays), CleanupLeft: []string{},
	}
	for _, f := range concepts {
		res.Concepts = append(res.Concepts, f.path)
	}
	for _, f := range indexes {
		res.Indexes = append(res.Indexes, f.path)
	}
	if opt.DryRun {
		return res, nil
	}
	files := append(concepts, indexes...)
	mark, err := marker(files)
	if err != nil {
		return nil, err
	}
	left, err := publish(target, replace, append(files, mark))
	if err != nil {
		return nil, err
	}
	if left != "" {
		res.CleanupLeft = append(res.CleanupLeft, filepath.ToSlash(left))
	}
	return res, nil
}

// strayPaths are the strays' files, workspace-relative and sorted; a stray
// row is its collection file and slug.
func strayPaths(idx *index.Index, strays map[index.Node]bool) []string {
	out := []string{}
	for node := range strays {
		if p, ok := idx.Paths[node]; ok {
			out = append(out, p)
			continue
		}
		if rtype, ok := idx.Resolved.Types.Get(node.Type); ok && rtype.Storage.Layout == schema.LayoutCollection {
			out = append(out, rtype.CollectionRelpath()+"#"+node.Slug)
		}
	}
	sort.Strings(out)
	return out
}

// markerName is the hidden file at a bundle's root that lists the files khub
// wrote there. Hidden, so OKF consumers and conformance walks pass over it; a
// workspace never has one, so it is what lets --force tell a previous export
// from anything else.
const markerName = ".khub-export"

// checkTarget refuses a target that is a file, or that already holds files
// and is not safe to replace. It reports whether the target exists and will
// be replaced.
func checkTarget(target string, force bool) (bool, error) {
	info, err := os.Stat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, errs.ExportTargetNotDirectory(target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return true, nil
	}
	if !force {
		return false, errs.ExportTargetNotEmpty(target)
	}
	return true, replaceable(target)
}

// replaceable allows --force to replace target only when nothing but a
// previous export would be lost: its marker is there, it holds no `.khub/`
// or `.git/` (a workspace or a repository), and every file in it is one the
// marker lists. Anything else refuses, naming what would be deleted.
func replaceable(target string) error {
	listed, err := readMarker(target)
	if err != nil {
		return errs.ExportTargetNotBundle(target)
	}
	for _, guard := range []string{".khub", ".git"} {
		if _, err := os.Lstat(filepath.Join(target, guard)); err == nil {
			return errs.ExportTargetUnsafe(target, guard+"/")
		}
	}
	return filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(target, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() || rel == markerName {
			return nil
		}
		if !listed[rel] {
			return errs.ExportTargetUnsafe(target, rel)
		}
		return nil
	})
}

// readMarker reads a bundle's marker: the bundle-relative files khub wrote.
func readMarker(dir string) (map[string]bool, error) {
	text, err := canon.ReadText(filepath.Join(dir, markerName))
	if err != nil {
		return nil, err
	}
	m, nonMap, err := canon.LoadDocMap(text, canon.Mode12)
	if err != nil || nonMap != nil {
		return nil, errors.New("not a khub export marker")
	}
	files, _ := m.Get("files")
	list, ok := files.([]any)
	if !ok {
		return nil, errors.New("not a khub export marker")
	}
	out := map[string]bool{}
	for _, f := range list {
		out[values.Str(f)] = true
	}
	return out, nil
}

// marker is the marker file listing files.
func marker(files []file) (file, error) {
	paths := make([]any, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	m := omap.New()
	m.Set("okf_version", OKFVersion)
	m.Set("files", paths)
	text, err := canon.DumpWide(m)
	if err != nil {
		return file{}, err
	}
	return file{path: markerName, text: "# Written by khub export: the files of this bundle. --force replaces\n" +
		"# the bundle only while it holds nothing else.\n" + text}, nil
}

// publish writes the files into a staging directory beside target, then
// swaps it in: a target that exists is moved aside, the staging directory
// takes its name, and the old one is removed. Until the swap, the target is
// untouched; a failure removes the staging directory and leaves it so. It
// returns the old bundle's path when the swap succeeded but its removal did
// not.
func publish(target string, replace bool, files []file) (string, error) {
	parentDir, base := filepath.Dir(target), filepath.Base(target)
	if err := os.MkdirAll(parentDir, 0o777); err != nil {
		return "", err
	}
	staging := filepath.Join(parentDir, "."+base+".khub-export-"+rand.Text())
	if err := os.Mkdir(staging, 0o777); err != nil {
		return "", err
	}
	if err := writeAll(staging, files); err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}
	if !replace {
		if err := os.Rename(staging, target); err != nil {
			_ = os.RemoveAll(staging)
			return "", err
		}
		return "", nil
	}
	old := filepath.Join(parentDir, "."+base+".khub-old-"+rand.Text())
	if err := os.Rename(target, old); err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}
	if err := os.Rename(staging, target); err != nil {
		_ = os.Rename(old, target)
		_ = os.RemoveAll(staging)
		return "", err
	}
	// The new bundle is in place; an old one that will not delete is left
	// and reported, not a failure.
	if err := os.RemoveAll(old); err != nil {
		return old, nil
	}
	return "", nil
}

func writeAll(root string, files []file) error {
	for _, f := range files {
		dest := filepath.Join(root, filepath.FromSlash(f.path))
		if err := fsio.MkdirAll(root, filepath.Dir(dest)); err != nil {
			return err
		}
		if err := fsio.WriteNewIn(root, dest, []byte(f.text)); err != nil {
			return err
		}
	}
	return nil
}

// conceptPath is where node's concept lives in the bundle.
func conceptPath(rtype *schema.ResolvedType, idx *index.Index, node index.Node) (string, bool) {
	if rtype.Storage.Layout == schema.LayoutCollection {
		rel := rtype.CollectionRelpath()
		return strings.TrimSuffix(rel, path.Ext(rel)) + "/" + node.Slug + ".md", true
	}
	rel, ok := idx.Paths[node]
	if !ok {
		return "", false
	}
	return strings.TrimSuffix(rel, path.Ext(rel)) + ".md", true
}

// planConcepts renders one concept per entity, in scan order.
func planConcepts(root string, resolved *schema.ResolvedSchema, idx *index.Index) ([]file, error) {
	paths := map[index.Node]string{}
	owner := map[string]index.Node{}
	for _, node := range idx.Order {
		rtype, _ := resolved.Types.Get(node.Type)
		p, ok := conceptPath(rtype, idx, node)
		if !ok {
			continue
		}
		if other, taken := owner[p]; taken {
			return nil, errs.ExportPathCollision(node.ID(), p, other.ID())
		}
		if schema.ReservedEntityName(path.Base(p)) {
			return nil, errs.ExportPathCollision(node.ID(), p, "a reserved OKF name")
		}
		owner[p] = node
		paths[node] = p
	}
	var out []file
	for _, node := range idx.Order {
		p, ok := paths[node]
		if !ok {
			continue
		}
		rtype, _ := resolved.Types.Get(node.Type)
		text, err := conceptText(root, rtype, idx, node)
		if err != nil {
			return nil, err
		}
		if lines := related(rtype, idx, node, paths); len(lines) > 0 {
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += "\n# Related\n\n" + strings.Join(lines, "\n") + "\n"
		}
		out = append(out, file{p, text, node})
	}
	return out, nil
}

// conceptText is the concept as written: an md entity's file, or a json,
// yaml or row entity materialized as md.
func conceptText(root string, rtype *schema.ResolvedType, idx *index.Index, node index.Node) (string, error) {
	if rtype.Storage.Fmt == "md" {
		return canon.ReadTextIn(root, filepath.Join(root, filepath.FromSlash(idx.Paths[node])))
	}
	front, err := canon.DumpWide(idx.Meta[node])
	if err != nil {
		return "", err
	}
	body := idx.Body[node]
	if body != "" {
		body = "\n" + body
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
	}
	return canon.RenderMD(front, body), nil
}

// related lists the entity's declared relations as `- <predicate>: <target>`,
// in declaration order and then value order: a resolved target as a
// bundle-absolute link, anything else as plain text.
func related(rtype *schema.ResolvedType, idx *index.Index, node index.Node, paths map[index.Node]string) []string {
	var out []string
	meta := idx.Meta[node]
	for _, pred := range rtype.Relations.Keys() {
		rel, _ := rtype.Relations.Get(pred)
		raw, _ := meta.Get(pred)
		for _, v := range values.Items(raw) {
			if !values.Truthy(v) {
				continue
			}
			text := values.Str(v)
			var targets []index.Node
			for t := range idx.ResolveTarget(rel, text) {
				if _, ok := paths[t]; ok {
					targets = append(targets, t)
				}
			}
			if len(targets) == 0 {
				out = append(out, "- "+pred+": "+reindex.MDEscape(text))
				continue
			}
			index.SortNodes(targets)
			for _, t := range targets {
				out = append(out, "- "+pred+": ["+reindex.Label(idx.Meta[t], t.Slug)+"]("+
					mdlink.LinkDest("/"+paths[t])+")")
			}
		}
	}
	return out
}

// planIndexes writes an §8 index.md in every directory the bundle holds: its
// concepts grouped under their type in schema order, then its subdirectories.
// Only the root one carries frontmatter, the `okf_version` it declares.
func planIndexes(resolved *schema.ResolvedSchema, idx *index.Index, concepts []file) ([]file, error) {
	here := map[string][]file{}
	subdirs := map[string]map[string]bool{"": {}}
	for _, c := range concepts {
		dir := parent(c.path)
		here[dir] = append(here[dir], c)
		for d := dir; d != ""; d = parent(d) {
			if subdirs[parent(d)] == nil {
				subdirs[parent(d)] = map[string]bool{}
			}
			subdirs[parent(d)][path.Base(d)] = true
			if subdirs[d] == nil {
				subdirs[d] = map[string]bool{}
			}
		}
	}
	var out []file
	for _, dir := range sortedKeys(subdirs) {
		entries := here[dir]
		sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
		var sections []string
		for _, tname := range resolved.Types.Keys() {
			var rows []string
			for _, e := range entries {
				if e.node.Type != tname {
					continue
				}
				row := "* [" + reindex.Label(idx.Meta[e.node], e.node.Slug) + "](" + mdlink.LinkDest(path.Base(e.path)) + ")"
				if desc := reindex.Description(idx.Meta[e.node]); desc != "" {
					row += " - " + desc
				}
				rows = append(rows, row)
			}
			if len(rows) > 0 {
				sections = append(sections, "# "+tname+"\n\n"+strings.Join(rows, "\n")+"\n")
			}
		}
		if names := sortedKeys(subdirs[dir]); len(names) > 0 {
			rows := make([]string, len(names))
			for i, n := range names {
				rows[i] = "* [" + reindex.MDEscape(n) + "](" + mdlink.LinkDest(n+"/"+indexName) + ")"
			}
			sections = append(sections, "# Subdirectories\n\n"+strings.Join(rows, "\n")+"\n")
		}
		text := strings.Join(sections, "\n")
		p := indexName
		if dir != "" {
			p = dir + "/" + indexName
		} else {
			front := omap.New()
			front.Set("okf_version", OKFVersion)
			y, err := canon.DumpWide(front)
			if err != nil {
				return nil, err
			}
			text = canon.RenderMD(y, text)
		}
		out = append(out, file{path: p, text: text})
	}
	return out, nil
}

func parent(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
