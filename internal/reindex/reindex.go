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
	"path/filepath"
	"sort"
	"strings"

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
// to; reindex stamps it in the root index's frontmatter, the one place OKF 0.2
// allows frontmatter in an index.md (SPEC §8, §12).
const OKFVersion = "0.2"

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
		current := ""
		if raw, rerr := fsio.ReadFile(root, path); rerr == nil {
			current = string(raw)
		}
		diff := unifiedDiff(
			splitLinesKeepEnds(current), splitLinesKeepEnds(content), IndexName, IndexName)
		return &Result{
			Count: count, Path: path, Content: content, Diff: diff,
			Wrote: false, Malformed: malformed,
		}, nil
	}
	if err := fsio.AtomicWriteIn(root, path, []byte(content)); err != nil {
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

// RenderIndex is the OKF 0.2 root index (SPEC §8): frontmatter carrying only
// okf_version, then one `# <type>` section per type with an entity, each entry
// `* [Title](link) - description`. The description is the entity's own, and an
// entry without one ends at the link. Resolved outbound edges follow an entry
// as indented sub-bullets, so the entry line stays in §8's shape. An empty
// workspace gets the frontmatter alone.
func RenderIndex(
	root string, resolved *schema.ResolvedSchema, idx *index.Index, g *graph.Graph,
) string {
	front := "---\nokf_version: '" + OKFVersion + "'\n---\n"
	var sections []string
	for _, group := range GroupByType(idx.Nodes, resolved) {
		lines := []string{"# " + group.Type, ""}
		for _, slug := range group.Slugs {
			node := index.Node{Type: group.Type, Slug: slug}
			entry := "* [" + Label(idx.Meta[node], slug) + "](" + EntityLink(root, idx, node) + ")"
			if desc := Description(idx.Meta[node]); desc != "" {
				entry += " - " + desc
			}
			lines = append(lines, entry)
			for _, xlink := range CrossLinks(root, resolved, idx, g, node) {
				lines = append(lines, "  * "+xlink)
			}
		}
		sections = append(sections, strings.Join(lines, "\n")+"\n")
	}
	return front + strings.Join(sections, "\n")
}

// Description is the entity's `description` on one line, Markdown-escaped
// like a label, or "" when it has none. It is user-authored, so unescaped it
// could inject a link into the index row.
func Description(meta *omap.Map) string {
	raw, _ := meta.Get("description")
	if !values.Truthy(raw) {
		return ""
	}
	return MDEscape(strings.Join(strings.Fields(values.Str(raw)), " "))
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
		out = append(out, e.Predicate+" → ["+Label(idx.Meta[e.To], e.To.Slug)+"]("+
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

// Label is a human label for a link to an entity: its `title`, else its
// `name`, else its slug, Markdown-escaped. reindex and export share it.
//
// The title is user-authored, so "]" or a "](url)" sequence would otherwise
// break the row or inject an arbitrary link target.
func Label(meta *omap.Map, slug string) string {
	for _, k := range []string{"title", "name"} {
		if v, _ := meta.Get(k); values.Truthy(v) {
			return MDEscape(values.Str(v))
		}
	}
	return MDEscape(slug)
}

// MDEscape backslash-escapes the characters that are structural inside
// Markdown link text.
func MDEscape(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, "[", `\[`)
	return strings.ReplaceAll(text, "]", `\]`)
}
