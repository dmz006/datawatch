// The comm channel's configure path for usage.burn_guard.max_projection_gap_points
// uses the shared config validator: valid values pass, invalid ones are refused, and
// other keys are not touched.

package router

import "testing"

func TestCheckBurnGuardConfigureProjectionGap(t *testing.T) {
	key := "usage.burn_guard.max_projection_gap_points"
	for _, ok := range []string{"0", "25", "100"} {
		if err := checkBurnGuardConfigure(key, ok); err != nil {
			t.Fatalf("%s: err = %v", ok, err)
		}
	}
	for _, bad := range []string{"-0.1", "100.01", "NaN", "Inf", "abc", ""} {
		if err := checkBurnGuardConfigure(key, bad); err == nil {
			t.Fatalf("%q accepted, want refusal", bad)
		}
	}
	if err := checkBurnGuardConfigure("usage.burn_guard.margin_points", "999"); err != nil {
		t.Fatalf("other key refused: %v", err)
	}
}
