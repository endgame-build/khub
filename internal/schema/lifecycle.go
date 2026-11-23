package schema

import (
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/values"
)

// OwnsDraft reports whether the type declares a `draft` field of its own,
// which is then domain data and never the legacy publish flag.
func (t *ResolvedType) OwnsDraft() bool {
	return t.Declares("draft")
}

// EffectiveStatus is values.EffectiveStatus for an entity of this type.
func (t *ResolvedType) EffectiveStatus(meta *omap.Map) string {
	return values.EffectiveStatus(meta, t.OwnsDraft())
}

// IsDraft reports whether an entity of this type is unpublished.
func (t *ResolvedType) IsDraft(meta *omap.Map) bool {
	return values.IsDraftOf(meta, t.OwnsDraft())
}
