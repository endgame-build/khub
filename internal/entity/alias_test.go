package entity

import (
	"errors"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

// A link target named by alias is stored as its owner's real slug, qualified
// when the bare slug would be ambiguous under the relation: `related` points at
// any type, and both a client and a person are slugged `acme`.
func TestAliasTargetQualifiesAnAmbiguousSlug(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Create(ws, "client", CreateOpts{ID: "acme", Fields: fields("name", "Acme", "aliases", "ACME Corp")})
	requireNoError(t, err)
	_, err = Create(ws, "person", CreateOpts{ID: "acme", Fields: fields("name", "Acme Person")})
	requireNoError(t, err)
	_, err = Create(ws, "client", CreateOpts{ID: "globex", Fields: fields("name", "Globex")})
	requireNoError(t, err)

	res, err := Link(ws, "globex", "related", "acme corp")
	requireNoError(t, err)
	if res.Target != "client/acme" {
		t.Fatalf("stored target = %q, want client/acme", res.Target)
	}
}

// Aliases are free text, so case never picks one claimant: `Globex` on one
// entity and `globex` on another make one ambiguous alias in every spelling,
// the same grouping check reports as a conflict.
func TestAliasCaseNeverPicksAClaimant(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Create(ws, "client", CreateOpts{ID: "acme", Fields: fields("name", "Acme", "aliases", "Globex")})
	requireNoError(t, err)
	_, err = Create(ws, "person", CreateOpts{ID: "pat", Fields: fields("name", "Pat", "aliases", "globex")})
	requireNoError(t, err)
	for _, id := range []string{"Globex", "globex", "GLOBEX"} {
		_, err := GetMany(ws, []string{id}, false)
		var located *errs.Located
		if !errors.As(err, &located) || located.Code != "ambiguity_error" {
			t.Fatalf("get %s: want ambiguity_error, got %v", id, err)
		}
	}
	views, err := GetMany(ws, []string{"client/globex"}, false)
	requireNoError(t, err)
	if len(views) != 1 {
		t.Fatalf("client/globex resolved %d entities", len(views))
	}
}
