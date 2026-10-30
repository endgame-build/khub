// Dead relative links in bodies — the remainder of the body-assertions port
// (issue #52). A body that links `../decisions/use-postgres.md` after that
// file was renamed is still the document it claims to be, with a broken
// pointer in it: a gap, like a thin section, reported and never gating.

package integrity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/mdlink"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
)

// deadLinkGaps reads the body of every md entity with a file of its own (a
// collection row has none to be relative to) inside target, and reports each
// distinct relative link whose file does not exist as a `body` gap. Nodes in
// sorted order, links in order of first appearance. Fenced and indented code,
// HTML comments and code spans are not read: a link written there is an
// example. bodies holds the bodies the template pass already read.
//
// Bodies are untrusted, so the answer must not say anything about the
// machine outside the workspace: a link that resolves outside the root, or
// through a symlink that leads out of it, is skipped — neither live nor dead
// — and nothing outside the root is ever touched.
func deadLinkGaps(
	root string, resolved *schema.ResolvedSchema, valid *index.Index, target *string,
	bodies map[index.Node]string,
) []FieldError {
	gaps := []FieldError{}
	ws, err := os.OpenRoot(root)
	if err != nil {
		return gaps
	}
	defer func() { _ = ws.Close() }()
	tree := linkTree{root: ws, names: map[string]map[string]bool{}}
	for _, node := range sortedNodes(valid) {
		if !inTarget(node, target) {
			continue
		}
		rtype, _ := resolved.Types.Get(node.Type)
		if !rtype.ReadsTemplate() { // md and not a collection: a body file
			continue
		}
		body, ok := bodies[node]
		var file string
		if !ok {
			file = entity.EntityPath(root, valid, node)
			if body, ok = linkBody(file); !ok {
				continue // an unreadable body is the shape check's finding, if anyone's
			}
		}
		if !hasLinkSyntax(body) {
			continue // skip the masking passes
		}
		if file == "" {
			file = entity.EntityPath(root, valid, node)
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			continue
		}
		dir := path.Dir(filepath.ToSlash(rel))
		for _, link := range mdlink.RelativeLinks(template.Prose(body)) {
			if tree.state(path.Join(dir, link.Path)) != linkDead {
				continue
			}
			gaps = append(gaps, FieldError{
				Type: node.Type, Slug: node.Slug, Field: "body",
				Reason: fmt.Sprintf("dead link '%s' (no such file)", link.Dest),
			})
		}
	}
	return gaps
}

// linkBody reads a body the template pass did not: the file split at its
// frontmatter fence, the YAML left unparsed — the index already parsed it,
// and links need only the body. A file with no link syntax is not split.
func linkBody(file string) (string, bool) {
	text, err := canon.ReadText(file)
	if err != nil || !hasLinkSyntax(text) {
		return "", false
	}
	_, body, err := canon.SplitFrontmatter(text)
	return body, err == nil
}

// hasLinkSyntax is the cheap pre-check: no `](` and no `]:` means no link.
func hasLinkSyntax(s string) bool {
	return strings.Contains(s, "](") || strings.Contains(s, "]:")
}

type linkState int

const (
	linkLive linkState = iota
	linkDead
	linkSkipped // outside the workspace: not khub's to answer
)

// linkTree answers for workspace-relative, `/`-separated, cleaned paths,
// through an os.Root so no symlink is followed out of the workspace, with
// directory listings cached for exact-name matching.
type linkTree struct {
	root  *os.Root
	names map[string]map[string]bool // dir → its entries; nil: unreadable
}

// state reports whether rel names a file or directory in the workspace.
// Every component must be spelled exactly as on disk: a case-insensitive
// filesystem (macOS, Windows) would otherwise call `Decision.md` live while
// the link is dead on Linux, in git and on every forge, and the same
// workspace would answer differently by platform. Each component is matched
// against its parent's listing before anything is opened, so a name the OS
// would read specially (a `\`, a drive) never reaches it: it is simply not
// in the listing. A path above the root, and a directory or file reached
// through a symlink that leaves it (os.Root refuses to follow), is skipped.
func (t linkTree) state(rel string) linkState {
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return linkSkipped
	}
	if rel == "." {
		return linkLive
	}
	dir := "."
	for _, part := range strings.Split(rel, "/") {
		names, ok := t.list(dir)
		if !ok {
			return linkSkipped
		}
		if !names[part] {
			return linkDead
		}
		dir = path.Join(dir, part)
	}
	if _, err := t.root.Stat(rel); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return linkDead // a broken symlink inside the workspace
		}
		return linkSkipped // a symlink out of the workspace
	}
	return linkLive
}

// list returns dir's entry names; ok is false when the directory cannot be
// read through the root (a symlink out of the workspace among them).
func (t linkTree) list(dir string) (map[string]bool, bool) {
	if set, cached := t.names[dir]; cached {
		return set, set != nil
	}
	var set map[string]bool
	if f, err := t.root.Open(dir); err == nil {
		if entries, err := f.ReadDir(-1); err == nil {
			set = make(map[string]bool, len(entries))
			for _, e := range entries {
				set[e.Name()] = true
			}
		}
		_ = f.Close()
	}
	t.names[dir] = set
	return set, set != nil
}
