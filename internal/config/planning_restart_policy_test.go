package config

import "testing"

func TestPlanningRestartPolicyValues(t *testing.T) {
	cases := []struct {
		name string
		v    string
		want string
	}{
		{"reattach", PlanningRestartPolicyReattach, "reattach"},
		{"restart", PlanningRestartPolicyRestart, "restart"},
		{"draft", PlanningRestartPolicyDraft, "draft"},
	}
	for _, c := range cases {
		if c.v != c.want {
			t.Fatalf("constant %s = %q, want %q", c.name, c.v, c.want)
		}
		if got := NormalizePlanningRestartPolicy(c.v); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.v, got, c.want)
		}
		if !ValidPlanningRestartPolicy(c.v) {
			t.Errorf("Valid(%q) = false, want true", c.v)
		}
	}
}

func TestPlanningRestartPolicyCaseAndWhitespace(t *testing.T) {
	inputs := []string{
		"RESTART",
		"  Restart  ",
		"\tdraft\n",
		" REATTACH ",
		"Draft",
	}
	want := map[string]string{
		"RESTART":     "restart",
		"  Restart  ": "restart",
		"\tdraft\n":   "draft",
		" REATTACH ":  "reattach",
		"Draft":       "draft",
	}
	for _, in := range inputs {
		if got := NormalizePlanningRestartPolicy(in); got != want[in] {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want[in])
		}
		if !ValidPlanningRestartPolicy(in) {
			t.Errorf("Valid(%q) = false, want true", in)
		}
	}
}

func TestPlanningRestartPolicyEmptyReadsAsUnset(t *testing.T) {
	// Empty reads as the default on load, but the write-path validator
	// reports it as not valid so callers can decide.
	for _, in := range []string{"", "   ", "\t\n"} {
		if got := NormalizePlanningRestartPolicy(in); got != PlanningRestartPolicyReattach {
			t.Errorf("Normalize(%q) = %q, want reattach", in, got)
		}
		if ValidPlanningRestartPolicy(in) {
			t.Errorf("Valid(%q) = true, want false", in)
		}
	}
}

func TestPlanningRestartPolicyUnknownValues(t *testing.T) {
	// Values that look close to a real one, or belong to the other
	// restart_policy setting, are unknown here.
	for _, in := range []string{"wait_quiet", "immediate", "drain", "abort", "restarts", "re-attach", "none"} {
		if got := NormalizePlanningRestartPolicy(in); got != PlanningRestartPolicyReattach {
			t.Errorf("Normalize(%q) = %q, want reattach", in, got)
		}
		if ValidPlanningRestartPolicy(in) {
			t.Errorf("Valid(%q) = true, want false", in)
		}
	}
}
