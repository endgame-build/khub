package clock

import (
	"errors"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/errs"
)

// localIn runs the test with time.Local set to zone, as TZ would.
func localIn(t *testing.T, zone *time.Location) {
	t.Helper()
	prev := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = prev })
}

func TestPinnedClock(t *testing.T) {
	localIn(t, time.UTC)
	for _, c := range []struct{ pin, today, stamp string }{
		{"2026-01-15", "2026-01-15", "2026-01-15T00:00:00Z"},
		{"2026-01-15T10:30:00Z", "2026-01-15", "2026-01-15T10:30:00Z"},
		{"2026-01-15T01:00:00+02:00", "2026-01-14", "2026-01-14T23:00:00Z"},
	} {
		t.Setenv("KHUB_PARITY_NOW", c.pin)
		if err := Check(); err != nil {
			t.Fatalf("%s: %v", c.pin, err)
		}
		if got := Today(); got != c.today {
			t.Errorf("%s: Today = %s, want %s", c.pin, got, c.today)
		}
		if got := Stamp(); got != c.stamp {
			t.Errorf("%s: Stamp = %s, want %s", c.pin, got, c.stamp)
		}
	}
}

// Dates are the local civil date; an OKF stamp is UTC. Late evening in
// New York is already tomorrow in UTC.
func TestDatesAreLocalStampsAreUTC(t *testing.T) {
	localIn(t, time.FixedZone("EST", -5*3600))
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15T22:30:00-05:00")
	if got := Today(); got != "2026-01-15" {
		t.Errorf("Today = %s, want the local date 2026-01-15", got)
	}
	if got := Stamp(); got != "2026-01-16T03:30:00Z" {
		t.Errorf("Stamp = %s, want 2026-01-16T03:30:00Z", got)
	}
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	if got := Today(); got != "2026-01-15" {
		t.Errorf("a pinned date read as %s", got)
	}
}

// An unparseable pin is refused, never read as the real clock.
func TestUnparseablePinRefuses(t *testing.T) {
	t.Setenv("KHUB_PARITY_NOW", "2026-1-15")
	var le *errs.Located
	if err := Check(); !errors.As(err, &le) || le.Code != "clock_pin_invalid" {
		t.Fatalf("Check = %v, want clock_pin_invalid", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("Now read an unparseable pin")
		}
	}()
	Now()
}
