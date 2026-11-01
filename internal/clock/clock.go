// Package clock is khub's one clock seam. KHUB_PARITY_NOW pins it, as the
// parity recorder does: a date (2026-01-15) is that day's local midnight, an
// offset datetime (2026-01-15T10:00:00Z) is that instant. Any other value is
// refused, never read as the real clock: a typo would silently make a
// fixture depend on the day it ran.
//
// Dates (`created`, `updated`, day deltas) are the local civil date, as a
// person writing the file means them; an OKF `at` stamp is UTC.
package clock

import (
	"os"
	"time"

	"github.com/endgame-build/khub/internal/errs"
)

// Check refuses an unparseable KHUB_PARITY_NOW. The CLI runs it before any
// command, so the refusal lands before a write.
func Check() error {
	_, err := pinned()
	return err
}

// Now is the current instant, in local time. A pin Check refuses panics here:
// every CLI path has refused it already, so reaching this is a caller's bug.
func Now() time.Time {
	t, err := pinned()
	if err != nil {
		panic(err)
	}
	return t
}

func pinned() (time.Time, error) {
	pin := os.Getenv("KHUB_PARITY_NOW")
	if pin == "" {
		return time.Now(), nil
	}
	if d, err := time.ParseInLocation("2006-01-02", pin, time.Local); err == nil {
		return d, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, pin); err == nil {
		return t.In(time.Local), nil
	}
	return time.Time{}, errs.ClockPinInvalid(pin)
}

// Today is today's local date, YYYY-MM-DD.
func Today() string { return Now().Format("2006-01-02") }

// Stamp is the current instant as an OKF datetime in UTC: ISO 8601, a `T`,
// seconds, and `Z`.
func Stamp() string { return Now().UTC().Format("2006-01-02T15:04:05Z") }
