// max_projection_gap_points in the CLI usage burn-guard config: a number from 0 to
// the maximum is forwarded as a number, and anything else is refused before a PUT is sent.

package main

import (
	"strings"
	"testing"
)

func TestBurnGuardProjectionGapCLIAcceptsRange(t *testing.T) {
	for in, want := range map[string]float64{"0": 0, "25": 25, "100": 100, "12.5": 12.5} {
		body, err := parseBurnGuardSettings([]string{"max_projection_gap_points=" + in})
		if err != nil {
			t.Fatalf("%s: err = %v", in, err)
		}
		if got, _ := body["max_projection_gap_points"].(float64); got != want {
			t.Fatalf("%s: body = %v, want %v", in, body["max_projection_gap_points"], want)
		}
	}
}

func TestBurnGuardProjectionGapCLIRefusesInvalid(t *testing.T) {
	for _, bad := range []string{"-1", "100.5", "101", "NaN", "Inf", "many", ""} {
		_, err := parseBurnGuardSettings([]string{"max_projection_gap_points=" + bad})
		if err == nil {
			t.Fatalf("max_projection_gap_points=%q accepted, want a refusal", bad)
		}
		if !strings.Contains(err.Error(), "max_projection_gap_points") {
			t.Fatalf("error = %q, want it to name the key", err)
		}
	}
}
