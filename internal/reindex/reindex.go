// Package reindex ports src/khub/core/reindex.py — `khub reindex`, which
// regenerates the OKF index.md navigation (WPK-005-1, FS-005).
//
// It walks the typed graph rebuilt from the live Markdown on demand — never the
// prior index.md (PRJ-001/REQ-PRJ001-03) — and renders an OKF index.md:
// entities grouped by type, cross-links along resolved edges, stamped with the
// OKF version it conforms to. --dry-run diffs the new index against the current
// file and writes nothing. The graph iteration and group-by-type step are
// shared with viz; only the render target (Markdown vs HTML) differs.
package reindex

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/entity"
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
// whether it was written. Diff is populated only under dry-run.
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
}

// Reindex is reindex.reindex: regenerate index.md from the live graph;
// dry-run diffs and writes nothing.
func Reindex(root string, dryRun bool) (*Result, error) {
	content, count, malformed, err := buildIndexDoc(root, dryRun)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, IndexName)
	if dryRun {
		current := ""
		if raw, rerr := os.ReadFile(path); rerr == nil {
			current = string(raw)
		}
		diff := unifiedDiff(
			splitLinesKeepEnds(current), splitLinesKeepEnds(content), IndexName, IndexName)
		return &Result{
			Count: count, Path: path, Content: content, Diff: diff,
			Wrote: false, Malformed: malformed,
		}, nil
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		return nil, err
	}
	return &Result{Count: count, Path: path, Content: content, Wrote: true}, nil
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
			link := EntityLink(root, resolved, group.Type, slug)
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
			EntityLink(root, resolved, e.To.Type, e.To.Slug)+")")
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
func EntityLink(root string, resolved *schema.ResolvedSchema, tname, slug string) string {
	rtype, _ := resolved.Types.Get(tname)
	link, err := filepath.Rel(root, entity.EntityPath(root, rtype, slug))
	if err != nil {
		link = entity.EntityPath(root, rtype, slug)
	}
	return mdlink.LinkDest(link)
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
