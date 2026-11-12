package server

import (
	"encoding/json"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// mergedMemberPolicySurface reads merged_member_policy from GET /api/config.
func mergedMemberPolicySurface(t *testing.T, s *Server) string {
	t.Helper()
	var a struct {
		Policy *string `json:"merged_member_policy"`
	}
	if err := json.Unmarshal(getAutonomousConfigMap(t, s)["automation"], &a); err != nil {
		t.Fatalf("decode automation: %v", err)
	}
	if a.Policy == nil {
		t.Fatal("GET /api/config dropped merged_member_policy")
	}
	return *a.Policy
}

func TestMergedMemberPolicyConfigDefaultRead(t *testing.T) {
	s, _, _ := newActivateServer(t, false)
	if got := mergedMemberPolicySurface(t, s); got != config.MergedMemberPolicyCompleted {
		t.Fatalf("GET default = %q, want completed", got)
	}
	// An unset value reads the same default.
	s.cfg.Autonomous.Automation.MergedMemberPolicy = ""
	if got := mergedMemberPolicySurface(t, s); got != "completed" {
		t.Fatalf("unset value reads %q, want completed", got)
	}
}

func TestMergedMemberPolicyConfigSetEachValidValue(t *testing.T) {
	s, mgr, _ := newActivateServer(t, false)
	for _, want := range []string{"manual", "block", "completed"} {
		pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.merged_member_policy": want})
		if got := mergedMemberPolicySurface(t, s); got != want {
			t.Fatalf("GET after PUT /api/config %s = %q", want, got)
		}
		// The live manager reads the same value with no restart.
		if got := mgr.Config().Automation.EffectiveMergedMemberPolicy(); got != want {
			t.Fatalf("live manager after PUT /api/config %s = %q", want, got)
		}
	}
	for _, want := range []string{"block", "manual", "completed"} {
		pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"merged_member_policy": want}})
		if got := mergedMemberPolicySurface(t, s); got != want {
			t.Fatalf("GET after autonomous PUT %s = %q", want, got)
		}
		if got := mgr.Config().Automation.EffectiveMergedMemberPolicy(); got != want {
			t.Fatalf("live manager after autonomous PUT %s = %q", want, got)
		}
	}
}

func TestMergedMemberPolicyConfigRejectsUnknownValue(t *testing.T) {
	s, mgr, _ := newActivateServer(t, false)
	for _, bad := range []any{"always", "COMPLETED", "", 1, true} {
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.merged_member_policy": bad})
	}
	pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"merged_member_policy": "sometimes"}})
	if got := mergedMemberPolicySurface(t, s); got != "completed" {
		t.Fatalf("rejected PUT changed the policy to %q", got)
	}
	if got := mgr.Config().Automation.EffectiveMergedMemberPolicy(); got != "completed" {
		t.Fatalf("rejected PUT changed the live manager to %q", got)
	}
}
