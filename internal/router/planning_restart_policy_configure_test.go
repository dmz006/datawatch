package router

import (
	"strings"
	"testing"
)

// planningRestartPolicyConfigure returns a router whose configure function
// records every key/value it is asked to set.
func planningRestartPolicyConfigure(t *testing.T) (*Router, *[][2]string) {
	t.Helper()
	var calls [][2]string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetConfigureFunc(func(key, value string) error {
		calls = append(calls, [2]string{key, value})
		return nil
	})
	return r, &calls
}

// The usage text and the key list both name the key and its three values.
func TestPlanningRestartPolicyConfigure_HelpAndListNameKey(t *testing.T) {
	r, _ := planningRestartPolicyConfigure(t)
	for _, text := range []string{"help", "list"} {
		replies := r.HandleTestMessage("configure " + text)
		joined := strings.Join(replies, "\n")
		if !strings.Contains(joined, "autonomous.planning_restart_policy") {
			t.Fatalf("configure %s lacks autonomous.planning_restart_policy: %q", text, joined)
		}
		if !strings.Contains(joined, "reattach") || !strings.Contains(joined, "restart") || !strings.Contains(joined, "draft") {
			t.Errorf("configure %s does not name the three values: %q", text, joined)
		}
		// The line for this key must not use PRD wording.
		for _, line := range strings.Split(joined, "\n") {
			if strings.Contains(line, "planning_restart_policy") && strings.Contains(line, "PRD") {
				t.Errorf("configure %s uses PRD wording on the planning key line: %q", text, line)
			}
		}
	}
}

// An invalid value is refused in the handler: the daemon is never asked to set
// it, and the reply names the valid values.
func TestPlanningRestartPolicyConfigure_RefusesInvalidValue(t *testing.T) {
	for _, v := range []string{"bogus", "never", "instant"} {
		r, calls := planningRestartPolicyConfigure(t)
		replies := r.HandleTestMessage("configure autonomous.planning_restart_policy=" + v)
		if len(*calls) != 0 {
			t.Fatalf("value %q reached configureFn: %v", v, *calls)
		}
		joined := strings.Join(replies, "\n")
		if !strings.Contains(joined, "refused") {
			t.Errorf("value %q: reply is not a refusal: %q", v, joined)
		}
		for _, want := range []string{"reattach", "restart", "draft"} {
			if !strings.Contains(joined, want) {
				t.Errorf("value %q: reply does not name %q: %q", v, want, joined)
			}
		}
	}
}

// Each valid value reaches the configure function under the full key.
func TestPlanningRestartPolicyConfigure_AcceptsEachValue(t *testing.T) {
	for _, v := range []string{"reattach", "restart", "draft", "RESTART"} {
		r, calls := planningRestartPolicyConfigure(t)
		r.HandleTestMessage("configure autonomous.planning_restart_policy=" + v)
		if len(*calls) != 1 {
			t.Fatalf("value %q: want one configure call, got %v", v, *calls)
		}
		if (*calls)[0] != [2]string{"autonomous.planning_restart_policy", v} {
			t.Fatalf("value %q: configureFn got %v", v, (*calls)[0])
		}
	}
}

// Keys other than this one pass through unchanged.
func TestPlanningRestartPolicyConfigure_OtherKeysUnaffected(t *testing.T) {
	r, calls := planningRestartPolicyConfigure(t)
	r.HandleTestMessage("configure session.console_cols=120")
	if len(*calls) != 1 || (*calls)[0] != [2]string{"session.console_cols", "120"} {
		t.Fatalf("other key not passed through: %v", *calls)
	}
}

// The refusal helper is empty for valid values and for other keys, and names
// the values for an empty or unknown value of this key.
func TestPlanningRestartPolicyConfigure_RefusalHelper(t *testing.T) {
	if got := planningRestartPolicyRefusal("session.console_cols", "bogus"); got != "" {
		t.Errorf("unrelated key refused: %q", got)
	}
	if got := planningRestartPolicyRefusal("autonomous.planning_restart_policy", "draft"); got != "" {
		t.Errorf("valid value refused: %q", got)
	}
	got := planningRestartPolicyRefusal("autonomous.planning_restart_policy", "")
	if !strings.Contains(got, "reattach, restart or draft") {
		t.Errorf("empty value refusal does not name the values: %q", got)
	}
}
