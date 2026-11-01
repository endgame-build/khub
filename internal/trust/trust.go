// Package trust derives an entity's review state from its OKF `verified`
// events: the content revision a verification records, which events still
// hold, and the tier they add up to. Nothing here is ever written but `rev`,
// which `khub verify` records; the tier is computed on every read.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// The review tiers.
const (
	Unverified = "unverified"
	Machine    = "machine"
	Human      = "human"
)

// Tiers lists the review tiers lowest first; every surface that prints or
// compares tiers takes its order from here.
var Tiers = []string{Unverified, Machine, Human}

// Rank is a tier's place in Tiers, -1 for a name that is no tier.
func Rank(tier string) int { return slices.Index(Tiers, tier) }

// bookkeeping are the declared fields that are not content: identity,
// provenance and housekeeping, which change without changing what the entity
// says. An edit to any of them leaves a verification standing and stamps no
// `generated`. `status` is content: a lifecycle change voids a review.
var bookkeeping = map[string]bool{
	"type": true, "author": true, "created": true, "updated": true,
	"generated": true, "verified": true, "tags": true, "aliases": true, "stale_after": true,
}

// Bookkeeping reports whether a declared field is not content (see
// bookkeeping). Rev and the write verbs' `generated` stamp share it.
func Bookkeeping(name string) bool { return bookkeeping[name] }

// Rev is the content revision a verification records: the first 12 hex
// digits of a SHA-256 over the entity's declared content fields (attributes
// and relations, bookkeeping excluded), by name in sorted order, and its body
// with line endings normalized and trailing newlines dropped. An undeclared
// key is not content. Every reader hashes through here, so `get` and `query`
// agree on a CRLF file.
func Rev(rtype *schema.ResolvedType, meta *omap.Map, body string) string {
	names := slices.Concat(rtype.Attributes.Keys(), rtype.Relations.Keys())
	slices.Sort(names)
	content := omap.New()
	for _, name := range names {
		if bookkeeping[name] {
			continue
		}
		if v, _ := meta.Get(name); v != nil {
			content.Set(name, v)
		}
	}
	content.Set("body", strings.TrimRight(canon.NormalizeNewlines(body), "\n"))
	text, err := canon.EncodeCLI(content)
	if err != nil {
		panic(err) // loaded values always encode; a failure is a bug, not input
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:12]
}

// Events reads `verified` as a list of mappings: a bare mapping is one event
// (OKF), and an entry that is not a mapping is no event.
func Events(meta *omap.Map) []*omap.Map {
	raw, _ := meta.Get("verified")
	list, isList := raw.([]any)
	if !isList {
		list = []any{raw}
	}
	var out []*omap.Map
	for _, e := range list {
		if m, isMap := e.(*omap.Map); isMap {
			out = append(out, m)
		}
	}
	return out
}

// Live reports whether an event still holds for content at rev. An event
// counts only when its `by` follows the actor convention and its `at` is an
// OKF datetime. One carrying a `rev` (khub's extension) holds while it
// matches. One without holds always, as OKF §5.3 reads every review.
func Live(event *omap.Map, rev string) bool {
	if by, _ := get(event, "by").(string); !ValidActor(by) {
		return false
	}
	if _, ok := values.OKFInstant(get(event, "at")); !ok {
		return false
	}
	if r, has := event.Get("rev"); has && r != nil {
		s, _ := r.(string)
		return s == rev
	}
	return true
}

// actorConvention is OKF's actor shape: `human:<name>`, `process:<name>`, or a
// producer token with an optional `/<version>` (`khub`, `agent/claude-code`).
var actorConvention = regexp.MustCompile(`^(?:(?:human|process):\S+|[A-Za-z0-9][A-Za-z0-9._-]*(?:/\S+)?)$`)

// ValidActor reports whether an actor string follows the convention. validate
// reports one that does not; a tier never counts it.
func ValidActor(s string) bool { return actorConvention.MatchString(s) }

// Tier is the entity's review tier from its live events: human when a live
// event's `by` is `human:…`, machine when any other event is live, else
// unverified.
func Tier(rtype *schema.ResolvedType, meta *omap.Map, body string) string {
	events := Events(meta)
	if len(events) == 0 {
		return Unverified
	}
	rev := Rev(rtype, meta, body)
	tier := Unverified
	for _, e := range events {
		if !Live(e, rev) {
			continue
		}
		if by, _ := get(e, "by").(string); strings.HasPrefix(by, "human:") {
			return Human
		}
		tier = Machine
	}
	return tier
}

// Superseded lists the indices of events whose recorded `rev` no longer
// matches the content: a review of a version that has since changed.
func Superseded(rtype *schema.ResolvedType, meta *omap.Map, body string) []int {
	events := Events(meta)
	if len(events) == 0 {
		return nil
	}
	rev := Rev(rtype, meta, body)
	var out []int
	for i, e := range events {
		if r, has := e.Get("rev"); has && r != nil && r != rev {
			out = append(out, i)
		}
	}
	return out
}

func get(m *omap.Map, k string) any {
	v, _ := m.Get(k)
	return v
}
