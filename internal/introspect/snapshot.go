package introspect

// The schema snapshot: the resolved schema reduced to the facts that decide
// whether an entity is valid, keyed by name so two snapshots diff by walking
// keys. It records the RESOLVED form, never the authored layer files — three
// layer files and one document carrying all three blocks resolve identically,
// and only the resolved form decides validity.
//
// Derived inverses are left out (they follow from a forward edge already in
// the snapshot), and so are preset provenance and the `when` routing prose:
// neither decides whether an entity is valid.

import (
	"reflect"
	"strings"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// SnapshotView is the snapshot of one resolved schema, types in declaration
// order.
func SnapshotView(resolved *schema.ResolvedSchema) *omap.Map {
	types := omap.New()
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		types.Set(name, snapshotType(rtype))
	}
	v := omap.New()
	v.Set("types", types)
	return v
}

func snapshotType(rtype *schema.ResolvedType) *omap.Map {
	v := omap.New()
	v.Set("layout", rtype.Storage.Layout)
	v.Set("format", rtype.Storage.Fmt)
	v.Set("path", strPtr(rtype.Storage.Path))
	v.Set("id_prefix", idPrefixView(rtype.IDPrefix))
	v.Set("id_date", rtype.IDDate)
	v.Set("required", rtype.Required)
	v.Set("orphan", rtype.Orphan)
	if rtype.TemplateOff {
		v.Set("template", false)
	} else {
		v.Set("template", strPtr(rtype.Template))
	}
	attrs := omap.New()
	for _, name := range rtype.Attributes.Keys() {
		a, _ := rtype.Attributes.Get(name)
		f := attrView(a)
		f.Delete("name")
		attrs.Set(name, f)
	}
	v.Set("attributes", attrs)
	rels := omap.New()
	for _, name := range rtype.Relations.Keys() {
		r, _ := rtype.Relations.Get(name)
		f := relationView(r)
		f.Delete("predicate")
		f.Delete("kind")
		f.Delete("derived")
		rels.Set(name, f)
	}
	v.Set("relations", rels)
	return v
}

// DiffSnapshot lists what changed from old to cur, one record per changed
// node: `op` (added | removed | changed), a dotted `path`, and `from`/`to`.
// Key order never counts as a change. Records follow cur's declaration order,
// and removals at each level follow the additions and changes, in old's order.
func DiffSnapshot(old, cur *omap.Map) []any {
	changes := []any{}
	diffMaps(old, cur, nil, &changes)
	return changes
}

func diffMaps(old, cur *omap.Map, path []string, out *[]any) {
	for _, k := range cur.Keys() {
		c, _ := cur.Get(k)
		o, inOld := old.Get(k)
		at := append(append([]string{}, path...), k)
		switch {
		case !inOld:
			*out = append(*out, change("added", at, nil, c))
		default:
			om, oIsMap := o.(*omap.Map)
			cm, cIsMap := c.(*omap.Map)
			if oIsMap && cIsMap {
				diffMaps(om, cm, at, out)
			} else if !equalValue(o, c) {
				*out = append(*out, change("changed", at, o, c))
			}
		}
	}
	for _, k := range old.Keys() {
		if _, inCur := cur.Get(k); !inCur {
			o, _ := old.Get(k)
			*out = append(*out, change("removed", append(append([]string{}, path...), k), o, nil))
		}
	}
}

func change(op string, path []string, from, to any) *omap.Map {
	m := omap.New()
	m.Set("op", op)
	m.Set("path", strings.Join(path, "."))
	m.Set("from", from)
	m.Set("to", to)
	return m
}

// equalValue compares two loaded values, ignoring mapping key order.
func equalValue(a, b any) bool {
	am, aIsMap := a.(*omap.Map)
	bm, bIsMap := b.(*omap.Map)
	if aIsMap || bIsMap {
		if !aIsMap || !bIsMap || am.Len() != bm.Len() {
			return false
		}
		for _, k := range am.Keys() {
			bv, ok := bm.Get(k)
			av, _ := am.Get(k)
			if !ok || !equalValue(av, bv) {
				return false
			}
		}
		return true
	}
	al, aIsList := a.([]any)
	bl, bIsList := b.([]any)
	if aIsList || bIsList {
		if !aIsList || !bIsList || len(al) != len(bl) {
			return false
		}
		for i := range al {
			if !equalValue(al[i], bl[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}
