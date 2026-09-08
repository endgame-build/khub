// Port of the resolution half of src/khub/core/entity.py: resolve_id,
// _same_target, _canonical_target, _resolve_write_target, _edges and
// _inbound_edges.
package entity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// resolveID resolves a bare slug (or qualified `type/slug`) to one node.
//
// A bare slug shared by two types is ambiguous; a `type/slug` qualifier is
// exact. Case is resolved leniently as a fallback (see index.CanonicalSlug):
// writes slugify to lowercase, so an agent that reuses the --id it passed must
// still be able to read the entity back.
func resolveID(idx *index.Index, id string) (index.Node, error) {
	if strings.Contains(id, "/") {
		parts := strings.SplitN(id, "/", 2)
		typeName, slug := parts[0], parts[1]
		if idx.Nodes[index.Node{Type: typeName, Slug: slug}] {
			return index.Node{Type: typeName, Slug: slug}, nil
		}
		if canonSlug, ok := idx.CanonicalSlug(slug); ok {
			for _, t := range sortedTypes(idx.TypesBySlug[canonSlug]) {
				if foldEqual(t, typeName) {
					return index.Node{Type: t, Slug: canonSlug}, nil
				}
			}
		}
		return index.Node{}, errs.LookupError(id)
	}
	types := idx.TypesBySlug[id]
	if len(types) == 0 {
		if canonSlug, ok := idx.CanonicalSlug(id); ok {
			id, types = canonSlug, idx.TypesBySlug[canonSlug]
		}
	}
	if len(types) == 0 {
		return index.Node{}, errs.LookupError(id)
	}
	names := sortedTypes(types)
	if len(names) > 1 {
		candidates := make([]string, len(names))
		for i, t := range names {
			candidates[i] = t + "/" + id
		}
		return index.Node{}, errs.AmbiguousSlug(id, candidates)
	}
	return index.Node{Type: names[0], Slug: id}, nil
}

// sameTarget reports whether two target spellings name one node.
// Case-insensitive, because lookup is.
func sameTarget(idx *index.Index, rel *schema.ResolvedRelation, a, b string) bool {
	left, right := idx.ResolveTarget(rel, a), idx.ResolveTarget(rel, b)
	if len(left) == 1 && len(right) == 1 {
		for node := range left {
			return right[node]
		}
	}
	// Literal fallback lets unlink repair dangling or explicitly authored ambiguous values.
	return foldEqual(a, b)
}

func uniqueTargets(idx *index.Index, rel *schema.ResolvedRelation, list []string) []string {
	out := make([]string, 0, len(list))
	for _, value := range list {
		duplicate := false
		for _, old := range out {
			if sameTarget(idx, rel, old, value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, value)
		}
	}
	return out
}

// canonicalTarget is the spelling to STORE for a resolved target, preserving
// the caller's qualified form.
//
// Resolution folds case, so storing the caller's raw string let `CMP-Api`
// and `cmp-api` sit side by side as two parallel edges to ONE node, each
// reporting changed: true — and made an unlink of the other spelling a silent
// no-op. One node, one stored value.
func canonicalTarget(target string, matches map[index.Node]bool) string {
	if len(matches) != 1 {
		return target
	}
	var only index.Node
	for n := range matches {
		only = n
	}
	if strings.Contains(target, "/") {
		return only.Type + "/" + only.Slug
	}
	return only.Slug
}

// resolveWriteTarget is the nodes a write-time relation value resolves to,
// gated for the write verbs. An unresolvable target hard-fails (referential
// integrity); a bare slug that hits more than one node must be qualified as
// `type/slug`. Returns the match set so link can spot a self-edge.
func resolveWriteTarget(
	rel *schema.ResolvedRelation, target string, idx *index.Index, predicate, noun string,
) (map[index.Node]bool, error) {
	matches := idx.ResolveTarget(rel, target)
	if len(matches) == 0 {
		return nil, errs.ReferentialIntegrity(strings.Join(rel.Targets, "/"), target, predicate, noun)
	}
	if !strings.Contains(target, "/") && len(matches) > 1 {
		candidates := make([]string, 0, len(matches))
		for n := range matches {
			candidates = append(candidates, n.Type+"/"+n.Slug)
		}
		sort.Strings(candidates)
		return nil, errs.AmbiguousSlug(target, candidates)
	}
	return matches, nil
}

// entityEdges are the entity's stored forward edges plus its read-time derived
// inverses.
func entityEdges(
	idx *index.Index, resolved *schema.ResolvedSchema, node index.Node, meta *omap.Map,
) []Edge {
	edges := []Edge{}
	rtype, _ := resolved.Types.Get(node.Type)
	for _, predicate := range rtype.Relations.Keys() {
		value, _ := meta.Get(predicate)
		if !truthy(value) {
			continue
		}
		rel, _ := rtype.Relations.Get(predicate)
		for _, target := range asTargetList(value) {
			ids := []string{}
			for hit := range idx.ResolveTarget(rel, target) {
				ids = append(ids, hit.ID())
			}
			sort.Strings(ids)
			edges = append(edges, Edge{Predicate: predicate, Target: target, ResolvedTargets: ids})
		}
	}
	// Derived inverses: any stored edge declaring an `inverse` that resolves to
	// us. The derived edge is qualified `type/slug` — the source type is not
	// recoverable from a bare slug (any type may declare the inverse), unlike a
	// stored forward edge.
	for _, other := range idx.Order {
		if other == node { // a self-reference is not its own inverse
			continue
		}
		emeta := idx.Meta[other]
		otype, _ := resolved.Types.Get(other.Type)
		for _, predicate := range otype.Relations.Keys() {
			rel, _ := otype.Relations.Get(predicate)
			if rel.Inverse == nil {
				continue
			}
			value, _ := emeta.Get(predicate)
			if !truthy(value) {
				continue
			}
			for _, target := range asTargetList(value) {
				if idx.ResolveTarget(rel, target)[node] {
					edges = append(edges, Edge{
						Predicate:       *rel.Inverse,
						Target:          other.Type + "/" + other.Slug,
						Derived:         true,
						ResolvedTargets: []string{other.ID()},
					})
				}
			}
		}
	}
	return edges
}

// inboundEdges is every edge from another entity that resolves to node.
func inboundEdges(idx *index.Index, resolved *schema.ResolvedSchema, node index.Node) []Inbound {
	inbound := []Inbound{}
	for _, other := range idx.Order {
		if other == node {
			continue
		}
		emeta := idx.Meta[other]
		otype, _ := resolved.Types.Get(other.Type)
		for _, predicate := range otype.Relations.Keys() {
			rel, _ := otype.Relations.Get(predicate)
			value, _ := emeta.Get(predicate)
			if !truthy(value) {
				continue
			}
			for _, target := range asTargetList(value) {
				if idx.ResolveTarget(rel, target)[node] {
					inbound = append(inbound, Inbound{
						SourceType: other.Type,
						SourceSlug: other.Slug,
						Predicate:  predicate,
					})
				}
			}
		}
	}
	return inbound
}

// asTargetList is Python's `value if isinstance(value, list) else [value]`
// followed by str() on each element.
func asTargetList(value any) []string {
	if list, ok := value.([]any); ok {
		out := make([]string, len(list))
		for i, v := range list {
			out[i] = values.Str(v)
		}
		return out
	}
	return []string{values.Str(value)}
}

func sortedTypes(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func selfLink(id, predicate string) *errs.Located {
	return errs.New("self_link",
		fmt.Sprintf("Cannot link '%s' to itself via '%s'", id, predicate))
}
