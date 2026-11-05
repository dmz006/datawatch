package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestComputeNodeVerifierFlagsExist checks both commands define the three
// flags as ints with the "unset" sentinels: -1 for the two nullable settings,
// 0 for the plain context figure.
func TestComputeNodeVerifierFlagsExist(t *testing.T) {
	for _, update := range []bool{false, true} {
		cmd := newComputeNodeAddCmd(update)
		for flag, def := range map[string]string{
			"verifier-slots":           "-1",
			"verifier-min-free-mem-mb": "-1",
			"verifier-context-mb":      "0",
		} {
			f := cmd.Flags().Lookup(flag)
			if f == nil {
				t.Errorf("%s: missing --%s", cmd.Name(), flag)
				continue
			}
			if f.Value.Type() != "int" {
				t.Errorf("%s --%s type = %s, want int", cmd.Name(), flag, f.Value.Type())
			}
			if f.DefValue != def {
				t.Errorf("%s --%s default = %s, want %s", cmd.Name(), flag, f.DefValue, def)
			}
			if !strings.Contains(f.Usage, "verifier") {
				t.Errorf("%s --%s usage = %q, want the verifier wording", cmd.Name(), flag, f.Usage)
			}
		}
	}
}

// TestComputeNodeVerifierFlagsLandInBody checks the settings arrive as JSON
// numbers, for add (POST) and update (PUT) alike.
func TestComputeNodeVerifierFlagsLandInBody(t *testing.T) {
	for _, update := range []bool{false, true} {
		method, path := http.MethodPost, "/api/compute/nodes"
		if update {
			method, path = http.MethodPut, "/api/compute/nodes/gpu-1"
		}
		t.Run("all three set", func(t *testing.T) {
			d := withComputeTLSDaemon(t)
			if _, err := runComputeNodeWrite(t, update, "gpu-1", "--kind", "ollama",
				"--verifier-slots", "2", "--verifier-min-free-mem-mb", "512", "--verifier-context-mb", "128"); err != nil {
				t.Fatalf("command: %v", err)
			}
			if d.method != method || d.path != path {
				t.Errorf("request = %s %s, want %s %s", d.method, d.path, method, path)
			}
			if got := d.body["verifier_slots"]; got != float64(2) {
				t.Errorf("verifier_slots = %v, want the number 2", got)
			}
			if got := d.body["verifier_min_free_mem_mb"]; got != float64(512) {
				t.Errorf("verifier_min_free_mem_mb = %v, want the number 512", got)
			}
			if got := d.body["verifier_context_mb"]; got != float64(128) {
				t.Errorf("verifier_context_mb = %v, want the number 128", got)
			}
		})
		t.Run("explicit zero stays explicit", func(t *testing.T) {
			d := withComputeTLSDaemon(t)
			if _, err := runComputeNodeWrite(t, update, "gpu-1", "--kind", "ollama", "--verifier-slots", "0"); err != nil {
				t.Fatalf("command: %v", err)
			}
			if got := d.body["verifier_slots"]; got != float64(0) {
				t.Errorf("verifier_slots = %v, want the number 0 (= no verifier slots), not absent", got)
			}
		})
		t.Run("omitted settings drop out of the body", func(t *testing.T) {
			d := withComputeTLSDaemon(t)
			if _, err := runComputeNodeWrite(t, update, "gpu-1", "--kind", "ollama", "--tags", "a,b"); err != nil {
				t.Fatalf("command: %v", err)
			}
			for _, k := range []string{"verifier_slots", "verifier_min_free_mem_mb"} {
				if _, present := d.body[k]; present {
					t.Errorf("%s was sent although the flag was not passed (default -1 = unset): %s", k, d.body[k])
				}
			}
			if got := d.body["verifier_context_mb"]; got != float64(0) {
				t.Errorf("verifier_context_mb = %v, want the number 0 (plain int, 0 = not used in sizing)", got)
			}
		})
	}
}

// TestComputeNodeVerifierTypoRefusedLocally checks a value below the unset
// sentinel (-2) is refused before it reaches the daemon, with wording that
// names the flag.
func TestComputeNodeVerifierTypoRefusedLocally(t *testing.T) {
	cases := []struct {
		flag string
		want string
	}{
		{"--verifier-slots", "--verifier-slots must be -1 (unset) or >= 0"},
		{"--verifier-min-free-mem-mb", "--verifier-min-free-mem-mb must be -1 (unset) or >= 0"},
		{"--verifier-context-mb", "--verifier-context-mb must be >= 0"},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			d := withComputeTLSDaemon(t)
			_, err := runComputeNodeWrite(t, false, "gpu-1", "--kind", "ollama", tc.flag, "-2")
			if err == nil {
				t.Fatal("expected the local refusal, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
			if d.method != "" {
				t.Errorf("a request went out: %s %s; the typo must be caught before the daemon", d.method, d.path)
			}
		})
	}
}
