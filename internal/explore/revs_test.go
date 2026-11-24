package explore

import (
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/omap"
)

// The revision the page marks is the one a review would record, and it moves
// with the content.
func TestRevsAgreeWithVerify(t *testing.T) {
	root, path := historyWS(t)
	read := func() any {
		t.Helper()
		out, err := Revs(root, []string{"capability/cap-pay", "capability/nope", "plain"}, now)
		if err != nil {
			t.Fatal(err)
		}
		revs := get(t, out, "revs").(*omap.Map)
		for _, id := range []string{"capability/nope", "plain"} {
			if v := get(t, revs, id); v != nil {
				t.Errorf("%s has revision %v", id, v)
			}
		}
		page, err := Page(root, "capability/cap-pay", now)
		if err != nil {
			t.Fatal(err)
		}
		if get(t, page, "rev") != get(t, revs, "capability/cap-pay") {
			t.Errorf("page %v, revs %v", get(t, page, "rev"), revs)
		}
		return get(t, revs, "capability/cap-pay")
	}
	before := read()
	res, err := entity.Verify(root, "cap-pay", "human:ana", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rev != before || read() != before {
		t.Errorf("verify recorded %s, the page had %v", res.Rev, before)
	}
	edit(t, path, "title: Payments", "title: Payouts")
	if read() == before {
		t.Error("an edit left the revision standing")
	}
}
