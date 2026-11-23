package explore

import (
	"slices"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// Directions a relation column can face.
const (
	DirOut = "out"
	DirIn  = "in"
)

// anySource is the source type of a collapsed incoming base column. A base
// relation is declared on every type, so one column per declaring type would be
// N near-empty columns on every list; the base relations are counted once per
// predicate instead, whichever type stores the edge.
const anySource = "*"

// Column is one relation a type takes part in, as the list counts it — one
// entry of /api/rows' `relations`.
type Column struct {
	Key       string   // out:<predicate> | in:<source type>.<predicate> | in:*.<predicate>
	Label     string   // the predicate; incoming: the declared inverse, else <source type>.<predicate>
	Direction string   // DirOut | DirIn
	Predicate string   // the stored predicate (incoming: the source's)
	PeerTypes []string // out: target types ("any" for any); in: the source type ("*" for collapsed base)
	Base      bool     // a base relation (related, references, depends_on)

	sourceType string // the type storing the edge; anySource for a collapsed base column
}

// Columns derives typeName's relation columns from the resolved schema, in the
// order the list shows them in (pinned by columns_test.go): declared outgoing relations, then
// base outgoing relations, then incoming relations by source type and predicate
// in schema order, then the collapsed incoming base relations. Incoming columns
// cover sources with and without a declared inverse, and the type's own
// relations to itself.
func Columns(resolved *schema.ResolvedSchema, typeName string) []Column {
	rtype, ok := resolved.Types.Get(typeName)
	if !ok {
		return nil
	}
	isBase := func(predicate string) bool {
		return resolved.BaseRelations != nil && resolved.BaseRelations.Has(predicate)
	}
	var declared, baseOut, incoming, baseIn []Column
	for _, predicate := range rtype.Relations.Keys() {
		rel, _ := rtype.Relations.Get(predicate)
		col := Column{
			Key:        DirOut + ":" + predicate,
			Label:      predicate,
			Direction:  DirOut,
			Predicate:  predicate,
			PeerTypes:  peerTargets(rel),
			Base:       isBase(predicate),
			sourceType: typeName,
		}
		if col.Base {
			baseOut = append(baseOut, col)
		} else {
			declared = append(declared, col)
		}
	}
	seenBaseIn := map[string]bool{}
	for _, source := range resolved.Types.Keys() {
		stype, _ := resolved.Types.Get(source)
		for _, predicate := range stype.Relations.Keys() {
			rel, _ := stype.Relations.Get(predicate)
			if !targets(rel, typeName) {
				continue
			}
			if isBase(predicate) {
				if seenBaseIn[predicate] {
					continue
				}
				seenBaseIn[predicate] = true
				baseIn = append(baseIn, Column{
					Key:        DirIn + ":" + anySource + "." + predicate,
					Label:      inverseLabel(rel, anySource, predicate),
					Direction:  DirIn,
					Predicate:  predicate,
					PeerTypes:  []string{anySource},
					Base:       true,
					sourceType: anySource,
				})
				continue
			}
			incoming = append(incoming, Column{
				Key:        DirIn + ":" + source + "." + predicate,
				Label:      inverseLabel(rel, source, predicate),
				Direction:  DirIn,
				Predicate:  predicate,
				PeerTypes:  []string{source},
				sourceType: source,
			})
		}
	}
	return slices.Concat(declared, baseOut, incoming, baseIn)
}

// columnKey is the key of the column a graph group lands in: the key Columns
// gives the same (direction, source type, predicate).
func columnKey(out bool, sourceType, predicate string, base bool) string {
	if out {
		return DirOut + ":" + predicate
	}
	if base {
		return DirIn + ":" + anySource + "." + predicate
	}
	return DirIn + ":" + sourceType + "." + predicate
}

// inverseLabel names an incoming column the way `khub get --edges` names the
// derived edge: the declared inverse, else <source type>.<predicate>.
func inverseLabel(rel *schema.ResolvedRelation, source, predicate string) string {
	if rel.Inverse != nil && *rel.Inverse != "" {
		return *rel.Inverse
	}
	return source + "." + predicate
}

// targets reports whether rel may point at typeName.
func targets(rel *schema.ResolvedRelation, typeName string) bool {
	return rel.Kind == schema.KindAny || slices.Contains(rel.Targets, typeName)
}

func peerTargets(rel *schema.ResolvedRelation) []string {
	if rel.Kind == schema.KindAny || len(rel.Targets) == 0 {
		return []string{"any"}
	}
	return slices.Clone(rel.Targets)
}

// record renders the column; the field order is the JSON contract the page reads.
func (c Column) record() *omap.Map {
	m := omap.New()
	m.Set("key", c.Key)
	m.Set("label", c.Label)
	m.Set("direction", c.Direction)
	m.Set("predicate", c.Predicate)
	m.Set("peer_types", strList(c.PeerTypes))
	m.Set("base", c.Base)
	return m
}

// strList is []string as []any: the CLI encoder serializes []any as a JSON
// array and a bare []string as one string.
func strList(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}
