package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// The autonomous_config_set tool declares the merged-member policy argument
// and forwards it into the nested automation object that
// PUT /api/autonomous/config reads.
func TestMergedMemberPolicyConfigMCPDeclaresArgument(t *testing.T) {
	if !strings.Contains(configSetArgNames(), "automation_merged_member_policy") {
		t.Error("autonomous_config_set does not declare automation_merged_member_policy")
	}
}

func TestMergedMemberPolicyConfigMCPSetsEachValidValue(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	for _, want := range []string{"completed", "manual", "block"} {
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
			"automation_merged_member_policy": want,
		})); err != nil {
			t.Fatalf("config set %s: %v", want, err)
		}
		auto, _ := rec.body["automation"].(map[string]any)
		if auto["merged_member_policy"] != want {
			t.Errorf("automation = %v, want merged_member_policy %s", rec.body["automation"], want)
		}
	}
}

// An argument that is not sent leaves the key out of the PUT, so the current
// value is kept.
func TestMergedMemberPolicyConfigMCPAbsentArgumentSendsNothing(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_idle_alert_minutes": 0,
	})); err != nil {
		t.Fatal(err)
	}
	auto, _ := rec.body["automation"].(map[string]any)
	if _, ok := auto["merged_member_policy"]; ok {
		t.Errorf("merged_member_policy leaked into an idle-only PUT: %v", rec.body)
	}
}

// An unknown value is forwarded as sent; the daemon refuses it with 400 and
// the tool reports that refusal to the caller instead of success.
func TestMergedMemberPolicyConfigMCPRejectsUnknownValue(t *testing.T) {
	refusal := `autonomous.automation.merged_member_policy: must be "completed", "manual" or "block"`
	s, rec := activateServer(t, http.StatusBadRequest, refusal)
	_, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_merged_member_policy": "sometimes",
	}))
	auto, _ := rec.body["automation"].(map[string]any)
	if auto["merged_member_policy"] != "sometimes" {
		t.Errorf("automation = %v, want the unknown value forwarded for the daemon to refuse", rec.body["automation"])
	}
	if err == nil || !strings.Contains(err.Error(), refusal) {
		t.Errorf("a refused value must come back with the daemon's refusal, got %v", err)
	}
}
