package values

import (
	"testing"

	"github.com/endgame-build/khub/internal/canon"
)

// A loaded timestamp is read from its date head, not a ten-character slice,
// and a day that does not exist fails both the reading and validate.
func TestCalendarDate(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string // "" = not a calendar date
	}{
		{canon.Date{ISO: "2026-01-15"}, "2026-01-15"},
		{canon.DateTime{ISO: "2099-1-5 10:00:00"}, "2099-01-05"},
		{canon.DateTime{ISO: "2099-01-05T23:30:00-05:00"}, "2099-01-05"},
		{canon.Date{ISO: "2099-02-30"}, ""},
		{canon.DateTime{ISO: "2099-13-01 00:00:00"}, ""},
		{"2026-01-15", ""}, // a string is not a loaded date
	} {
		d, ok := CalendarDate(tc.in)
		got := ""
		if ok {
			got = d.Format("2006-01-02")
		}
		if got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.in, got, tc.want)
		}
		if IsDateish(tc.in) != (tc.want != "") && tc.in != "2026-01-15" {
			t.Errorf("%v: IsDateish disagrees with CalendarDate", tc.in)
		}
	}
}
