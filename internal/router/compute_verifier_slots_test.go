package router

import "testing"

// TestComputeVerifierSettingsKV checks the comm channel reads the three
// verifier settings as whole numbers, so `compute node update <name>
// verifier_slots=2 verifier_min_free_mem_mb=512 verifier_context_mb=128`
// stores exactly those numbers.
func TestComputeVerifierSettingsKV(t *testing.T) {
	out := parseKVPairs("kind=ollama verifier_slots=2 verifier_min_free_mem_mb=512 verifier_context_mb=128")
	for k, want := range map[string]int{"verifier_slots": 2, "verifier_min_free_mem_mb": 512, "verifier_context_mb": 128} {
		got, ok := out[k].(int)
		if !ok {
			t.Fatalf("%s = %#v, want an int", k, out[k])
		}
		if got != want {
			t.Errorf("%s = %d, want %d", k, got, want)
		}
	}
}

// TestComputeVerifierZeroIsExplicit checks a passed =0 stays 0 — "no verifier
// slots", or "check off" for the memory bound — distinct from the key absent.
func TestComputeVerifierZeroIsExplicit(t *testing.T) {
	out := parseKVPairs("verifier_slots=0 verifier_min_free_mem_mb=0")
	if got := out["verifier_slots"].(int); got != 0 {
		t.Errorf("verifier_slots = %d, want the explicit 0", got)
	}
	if got := out["verifier_min_free_mem_mb"].(int); got != 0 {
		t.Errorf("verifier_min_free_mem_mb = %d, want the explicit 0", got)
	}

	absent := parseKVPairs("kind=ollama")
	for _, k := range []string{"verifier_slots", "verifier_min_free_mem_mb", "verifier_context_mb"} {
		if _, present := absent[k]; present {
			t.Errorf("%s present without the key", k)
		}
	}
}

// TestComputeVerifierNegativeKVPassesThrough checks negative values reach the
// daemon as negative numbers instead of being clamped, so the daemon's own
// "must be >= 0" wording is the one the operator sees.
func TestComputeVerifierNegativeKVPassesThrough(t *testing.T) {
	out := parseKVPairs("verifier_slots=-3")
	if got := out["verifier_slots"].(int); got != -3 {
		t.Errorf("verifier_slots = %d, want -3 to pass through (the daemon refuses it)", got)
	}
}
