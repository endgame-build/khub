// Package reindex ports src/khub/core/reindex.py — `khub reindex`, which
// regenerates the OKF index.md navigation (WPK-005-1, FS-005).
//
// It walks the typed graph rebuilt from the live Markdown on demand — never the
// prior index.md (PRJ-001/REQ-PRJ001-03) — and renders an OKF index.md:
// entities grouped by type, cross-links along resolved edges, stamped with the
// OKF version it conforms to. --dry-run diffs the new index against the current
// file and writes nothing; --check is that diff as a gate. The graph iteration and group-by-type step are
// shared with viz; only the render target (Markdown vs HTML) differs.
package reindex

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/mdlink"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// OKFVersion is the Open Knowledge Format version the generated index conforms
// to. OKF v0.1 is "a git tree of .md concepts with a required type,
// cross-links, index.md, log.md" (design-memo); reindex stamps it so a consumer
// knows the contract the file meets.
const OKFVersion = "0.1"

// IndexName is the generated file's name.
const IndexName = "index.md"

// Result is reindex.ReindexResult: the derived index, how many entities, and
// whether it was written. Diff is populated only under dry-run and check.
type Result struct {
	Count   int
	Path    string
	Content string
	Diff    string
	Wrote   bool
	// Malformed is the unparseable files a dry run previewed OVER. Empty on a
	// real reindex, which refuses instead. The caller must surface it: the
	// preview is honest only if it says what the graph is missing.
	Malformed []string
	// Missing is set when no index.md exists to diff against (dry run and
	// check only).
	Missing bool
	// Stale is set when index.md, read with universal newlines, differs from
	// Content byte for byte (dry run and check only). It is the verdict; Diff
	// is for display, and its line split cannot see every byte difference.
	Stale bool
}

// Reindex is reindex.reindex: regenerate index.md from the live graph;
// dry-run diffs and writes nothing.
func Reindex(root string, dryRun bool) (*Result, error) {
	if dryRun {
		return reindex(root, true)
	}
	return fsio.Locked(root, func() (*Result, error) { return reindex(root, false) })
}

// ReindexHeld is Reindex for a caller that already holds the workspace lock
// (an init or upgrade running its tails inside its own lock). fsio.Locked is
// not re-entrant, so calling Reindex there would block forever.
func ReindexHeld(root string, dryRun bool) (*Result, error) { return reindex(root, dryRun) }
func reindex(root string, dryRun bool) (*Result, error) {
	content, count, malformed, err := buildIndexDoc(root, dryRun)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, IndexName)
	if dryRun {
		// A preview stays lenient: an unreadable index.md diffs as empty.
		cur, _ := readCurrent(root, path, content)
		return &Result{
			Count: count, Path: path, Content: content, Diff: cur.diff,
			Wrote: false, Malformed: malformed, Missing: cur.missing, Stale: cur.stale,
		}, nil
	}
	if err := fsio.AtomicWriteIn(root, path, []byte(content)); err != nil {
		return nil, err
	}
	return &Result{Count: count, Path: path, Content: content, Wrote: true}, nil
}

// Check is `khub reindex --check`: the dry-run diff, as a gate. It writes
// nothing and takes no lock. Unlike a dry run it refuses an incomplete scan,
// exactly as a real reindex does: a verdict of "current" or "stale" over a
// graph missing whole files would be wrong either way. For the same reason it
// refuses an index.md it cannot read (a symlink, a directory, no permission)
// rather than call it stale: the `khub reindex` a stale verdict names would
// refuse it too. Result.Stale is the verdict.
func Check(root string) (*Result, error) {
	content, count, _, err := buildIndexDoc(root, false)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, IndexName)
	cur, err := readCurrent(root, path, content)
	if err != nil {
		return nil, err
	}
	return &Result{
		Count: count, Path: path, Content: content, Diff: cur.diff,
		Missing: cur.missing, Stale: cur.stale,
	}, nil
}

// current is index.md on disk measured against the derived index.
type current struct {
	diff    string
	missing bool
	stale   bool
}

// readCurrent compares the derived index with the file on disk, read with
// universal newlines as every khub text read is, so a CRLF checkout (git's
// core.autocrlf) of a current index is current. An absent file compares as
// empty and is reported missing. Any other read error is returned alongside a
// comparison against empty, which a dry run may use and a check must not.
func readCurrent(root, path, content string) (current, error) {
	text, err := readIndex(root, path)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil {
		text = ""
	}
	cur := current{
		diff: unifiedDiff(
			splitLinesKeepEnds(text), splitLinesKeepEnds(content), IndexName, IndexName),
		missing: missing,
		stale:   missing || text != content,
	}
	if err != nil && !missing {
		return cur, err
	}
	return cur, nil
}

// readIndex reads index.md with universal newlines. Something other than a
// regular file there is refused as the write in a real reindex refuses it, so
// a check never reports a state the corrective reindex cannot fix. (Reading a
// directory would otherwise surface as a path khub built wrong.)
func readIndex(root, path string) (string, error) {
	info, err := fsio.Stat(root, path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", &fs.PathError{Op: "read", Path: path, Err: fs.ErrInvalid}
	}
	return canon.ReadTextIn(root, path)
}

// BuildIndexDoc is reindex.build_index_doc: the rendered OKF index.md and its
// entity count, derived from the live graph. The prior index.md is never read
// as input (derived-not-stored, PRJ-001).
func BuildIndexDoc(root string) (string, int, error) {
	content, count, _, err := buildIndexDoc(root, false)
	return content, count, err
}

// buildIndexDoc derives the index. `preview` relaxes the malformed-scan
// refusal.
//
// A real reindex must refuse an incomplete scan: writing an index.md with a
// whole type silently missing is the failure that refusal exists to prevent.
// A dry run writes nothing and exists to show what the index WOULD become —
// which is exactly what someone wants while diagnosing the bad merge that
// caused the malformed file. Refusing there withheld the diagnostic at the
// moment it was most useful. The preview returns the malformed list so the
// caller can say what it could not see.
func buildIndexDoc(root string, preview bool) (string, int, []string, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return "", 0, nil, err
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return "", 0, nil, err
	}
	if !preview {
		if err := index.RejectMalformed(scanned, "reindex"); err != nil {
			return "", 0, nil, err
		}
	}
	malformed := append([]string{}, scanned.Malformed...)
	idx := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities
	g := graph.BuildGraph(idx)
	return RenderIndex(root, resolved, idx, g), len(idx.Nodes), malformed, nil
}

// Group is one section of the rendered index: a type and the slugs under it.
// It replaces the Python dict[str, list[str]], whose iteration order (schema
// declaration order) is the section order on the page.
type Group struct {
	Type  string
	Slugs []string
}

// RenderIndex is reindex.render_index: a version-stamped body grouping entities
// by type with cross-links.
func RenderIndex(
	root string, resolved *schema.ResolvedSchema, idx *index.Index, g *graph.Graph,
) string {
	front := "---\nokf_version: '" + OKFVersion + "'\nentity_count: " +
		strconv.Itoa(len(idx.Nodes)) + "\n---\n"
	lines := []string{"# Index", ""}
	groups := GroupByType(idx.Nodes, resolved)
	if len(groups) == 0 {
		// A valid, stamped, zero-row index (AC-003 / U06).
		lines = append(lines, "_No entities._", "")
	}
	for _, group := range groups {
		lines = append(lines, "## "+group.Type, "")
		for _, slug := range group.Slugs {
			node := index.Node{Type: group.Type, Slug: slug}
			link := EntityLink(root, idx, node)
			label := label(idx.Meta[node], slug)
			suffix := ""
			if xlinks := CrossLinks(root, resolved, idx, g, node); len(xlinks) > 0 {
				suffix = " — " + strings.Join(xlinks, ", ")
			}
			lines = append(lines, "- ["+label+"]("+link+")"+suffix)
		}
		lines = append(lines, "")
	}
	return front + strings.Join(lines, "\n") + "\n"
}

// GroupByType is reindex.group_by_type: one section per type, in
// schema-declared (stable) order. Only types with at least one entity get a
// section; slugs sort within a type.
func GroupByType(nodes map[index.Node]bool, resolved *schema.ResolvedSchema) []Group {
	groups := []Group{}
	for _, tname := range resolved.Types.Keys() {
		var slugs []string
		for n := range nodes {
			if n.Type == tname {
				slugs = append(slugs, n.Slug)
			}
		}
		if len(slugs) > 0 {
			sort.Strings(slugs)
			groups = append(groups, Group{Type: tname, Slugs: slugs})
		}
	}
	return groups
}

// CrossLinks is reindex.cross_links: one "predicate → [label](link)" per
// resolved outbound edge, sorted for determinism.
//
// The label is the target's title when present, else its slug (the OKF core
// title field is optional everywhere but case-study).
func CrossLinks(
	root string, resolved *schema.ResolvedSchema, idx *index.Index,
	g *graph.Graph, node index.Node,
) []string {
	var out []string
	for _, e := range g.OutEdges(node) {
		out = append(out, e.Predicate+" → ["+label(idx.Meta[e.To], e.To.Slug)+"]("+
			EntityLink(root, idx, e.To)+")")
	}
	sort.Strings(out)
	return out
}

// EntityLink is reindex._entity_link: the entity's path relative to the
// workspace root (where index.md lives).
//
// A path carrying a space or parenthesis breaks a plain (path) Markdown link,
// so it is wrapped in angle brackets (the CommonMark escape); a clean path is
// left as-is so the common case stays unadorned. Exported because the Python
// suite exercises it directly.
func EntityLink(root string, idx *index.Index, node index.Node) string {
	path := entity.EntityPath(root, idx, node)
	link, err := filepath.Rel(root, path)
	if err != nil {
		link = path
	}
	// index.md is committed: a Markdown link is slash-separated on every host.
	return mdlink.LinkDest(filepath.ToSlash(link))
}

// label is reindex._label: a human label for a link — the title if set, else
// the slug, Markdown-escaped.
//
// The title is user-authored, so "]" or a "](url)" sequence would otherwise
// break the row or inject an arbitrary link target.
func label(meta *omap.Map, slug string) string {
	title, _ := meta.Get("title")
	if !values.Truthy(title) {
		return slug
	}
	return mdEscape(values.Str(title))
}

// mdEscape is reindex._md_escape: backslash-escape the characters that are
// structural inside Markdown link text.
func mdEscape(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, "[", `\[`)
	return strings.ReplaceAll(text, "]", `\]`)
}
