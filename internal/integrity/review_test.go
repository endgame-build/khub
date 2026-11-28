package integrity

import (
	"testing"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/template"
)

func lensEvent(code, verdict, rev, q string) *omap.Map {
	e := omap.New()
	e.Set("by", "human:ana")
	e.Set("lens", code)
	e.Set("verdict", verdict)
	e.Set("rev", rev)
	e.Set("q", q)
	return e
}

// The last event for a lens decides; it holds only while both its rev and
// its q match, and an event whose verdict is neither pass nor fail is no
// verdict.
func TestLensStates(t *testing.T) {
	lenses := []template.LensView{
		{Code: "a", Instruction: "Ask a."}, {Code: "b", Instruction: "Ask b."},
		{Code: "c", Instruction: "Ask c."}, {Code: "d", Instruction: "Ask d."}, {Code: "e", Instruction: "Ask e."},
	}
	qa, qb, qc, qe := LensQ("Ask a."), LensQ("Ask b."), LensQ("Ask c."), LensQ("Ask e.")
	meta := omap.New()
	meta.Set("verified", []any{
		lensEvent("a", "fail", "r1", qa), lensEvent("a", "pass", "r1", qa), // the last wins
		lensEvent("b", "fail", "r1", qb),
		lensEvent("c", "pass", "r0", qc),                                                // an earlier body
		lensEvent("e", "pass", "r1", "000000000000"), lensEvent("e", "maybe", "r1", qe), // an earlier question
	})
	got := lensStates(lenses, meta, "r1")
	want := []string{LensPass, LensFail, LensStale, LensUnreviewed, LensStale}
	for i, l := range got {
		if l.State != want[i] {
			t.Errorf("lens %s = %s, want %s", l.Code, l.State, want[i])
		}
	}
	if len(qa) != 12 || qa == qb {
		t.Errorf("LensQ = %q, %q", qa, qb)
	}
}
