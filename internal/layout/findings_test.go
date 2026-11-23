package layout

import (
	"reflect"
	"testing"

	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/trust"
)

// TestCheckFindingsMirrorsPassed fills each slice bucket of a CheckReport
// alone and holds checkFindings to CheckReport.Passed: a bucket that fails
// the gate yields a finding, and an informational one yields none. A gating
// bucket added to Passed and not here would let a migration hide its finding.
func TestCheckFindingsMirrorsPassed(t *testing.T) {
	fields := reflect.TypeFor[integrity.CheckReport]()
	for i := range fields.NumField() {
		f := fields.Field(i)
		if f.Type.Kind() != reflect.Slice {
			continue
		}
		r := &integrity.CheckReport{StaleGate: true, TierGate: trust.Tiers[len(trust.Tiers)-1]}
		v := reflect.ValueOf(r).Elem().Field(i)
		v.Set(reflect.MakeSlice(f.Type, 1, 1))
		gates := !r.Passed()
		found := len(checkFindings(r)) > 0
		if gates != found {
			t.Errorf("%s: Passed fails the gate = %v, checkFindings reports a finding = %v", f.Name, gates, found)
		}
	}
}
