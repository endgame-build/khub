// Package integrity ports src/khub/core/integrity.py — the v1 integrity gate,
// two verbs and two engines shipped together:
//
//   - validate.go — per-entity well-formedness over PRESENT declared fields
//     plus referential integrity. A missing required field is NOT a validate
//     error.
//   - check.go — graph-wide over the ACTIVE (draft:false) subgraph:
//     required-completeness, orphans, dangling edges, strays, misplaced files,
//     malformed files, cycles, singleton gaps.
//
// The two gates stay in separate files with separate report types on purpose.
// They answer different questions and must never be collapsed: capture is never
// blocked, so a missing required field leaves an entity incomplete for `check`
// to report while `validate` still passes it.
//
// Both uphold one invariant: khub guarantees structural integrity, never
// semantic truth (INT-SHARED-003). A false-but-legal write passes both gates by
// design — git history, not a gate, is the backstop.
package integrity

import (
	"strings"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/values"
)

// The write gate and the integrity gate check the same values through one
// module, so a value the write path accepts is exactly a value validate
// accepts (draft included). These are thin aliases of internal/values, kept
// named so the correspondence with integrity.py's re-bound `_present` /
// `_is_bool` / `_is_number` / `_is_dateish` stays visible.
var (
	present   = values.Present
	isBool    = values.IsBool
	isNumber  = values.IsNumber
	isDateish = values.IsDateish
)

// sortedNodes is `sorted(index.nodes)` — the (type, slug) tuple order both
// gates iterate in.
func sortedNodes(idx *index.Index) []index.Node {
	out := make([]index.Node, 0, len(idx.Nodes))
	for n := range idx.Nodes {
		out = append(out, n)
	}
	index.SortNodes(out)
	return out
}

// metaGet is `meta.get(key)`: a missing key and a null value read alike.
func metaGet(meta *omap.Map, key string) any {
	v, _ := meta.Get(key)
	return v
}

// asList is Python's `value if isinstance(value, list) else [value]`.
func asList(value any) []any {
	if l, ok := value.([]any); ok {
		return l
	}
	return []any{value}
}

// pyStr renders a value the way Python's str() does inside integrity's
// messages. Scalars go through internal/values (the shared str() port); a list
// falls back to Python's list repr shape, which no shipped schema reaches (a
// list-valued ATTRIBUTE with an enum or pattern) but which would otherwise
// print an empty string into an error message.
func pyStr(v any) string {
	switch x := v.(type) {
	case []any:
		parts := make([]string, len(x))
		for i, it := range x {
			if s, isStr := it.(string); isStr {
				parts[i] = "'" + s + "'"
			} else {
				parts[i] = pyStr(it)
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return values.Str(v)
	}
}
