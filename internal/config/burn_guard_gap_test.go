// ValidateMaxProjectionGapPoints: the one rule for max_projection_gap_points.

package config

import (
	"math"
	"testing"
)

func TestValidateMaxProjectionGapPoints(t *testing.T) {
	for _, ok := range []float64{0, 25, BurnGuardProjectionGapMax} {
		if err := ValidateMaxProjectionGapPoints(ok); err != nil {
			t.Fatalf("%v: err = %v", ok, err)
		}
	}
	for _, bad := range []float64{-1, BurnGuardProjectionGapMax + 0.01, math.NaN(), math.Inf(1)} {
		if err := ValidateMaxProjectionGapPoints(bad); err == nil {
			t.Fatalf("%v accepted, want refusal", bad)
		}
	}
}
