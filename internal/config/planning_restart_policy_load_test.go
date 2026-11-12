package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// planningRestartPolicyLoadFrom writes yaml to a temp config, loads it, and
// returns the loaded policy plus every startup warning line captured.
func planningRestartPolicyLoadFrom(t *testing.T, yaml string) (string, []string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	var lines []string
	prev := planningRestartPolicyLogf
	planningRestartPolicyLogf = func(format string, args ...interface{}) {
		lines = append(lines, strings.TrimSpace(fmt.Sprintf(format, args...)))
	}
	defer func() { planningRestartPolicyLogf = prev }()
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg.Autonomous.PlanningRestartPolicy, lines
}

func TestPlanningRestartPolicyLoad_InvalidWarnsAndReadsDefault(t *testing.T) {
	got, lines := planningRestartPolicyLoadFrom(t, "autonomous:\n  planning_restart_policy: later\n")
	if got != PlanningRestartPolicyReattach {
		t.Fatalf("loaded policy = %q, want %q", got, PlanningRestartPolicyReattach)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "later") || !strings.Contains(lines[0], "autonomous.planning_restart_policy") {
		t.Fatalf("warnings = %q, want one line naming the key and value", lines)
	}
}

func TestPlanningRestartPolicyLoad_ValidAndUnsetAreSilent(t *testing.T) {
	got, lines := planningRestartPolicyLoadFrom(t, "autonomous:\n  planning_restart_policy: Draft\n")
	if got != PlanningRestartPolicyDraft || len(lines) != 0 {
		t.Fatalf("valid load = %q warnings %q, want draft and none", got, lines)
	}
	got, lines = planningRestartPolicyLoadFrom(t, "autonomous: {}\n")
	if got != "" || len(lines) != 0 {
		t.Fatalf("unset load = %q warnings %q, want empty and none", got, lines)
	}
}
