package router

import (
	"strings"
	"testing"
)

const decisionAssistantUsagePrefix = "usage: automation set-decision-assistant"

func TestAutomationCommDecisionAssistant(t *testing.T) {
	r, calls := setLLMFixture(t)
	path := "/api/automations/ab12/set_decision_assistant"
	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"set", "automation set-decision-assistant ab12 backend=pi-prompt model=qwen3.8:27b", path,
			map[string]any{"actor": "comm", "backend": "pi-prompt", "model": "qwen3.8:27b"}},
		{"full", "automation set-decision-assistant ab12 backend=pi-prompt model=m mode=live hooks=empty_edge,budget_breach threshold.empty_edge=0.8 member=m1 expected_version=4", path,
			map[string]any{"actor": "comm", "backend": "pi-prompt", "model": "m", "mode": "live",
				"hooks": []any{"empty_edge", "budget_breach"}, "thresholds": map[string]any{"empty_edge": 0.8},
				"member_id": "m1", "expected_version": float64(4)}},
		{"clear", "automation set-decision-assistant ab12 clear=true", path,
			map[string]any{"actor": "comm", "clear": true}},
		{"empty backend clears", "automation set-decision-assistant ab12 backend=", path,
			map[string]any{"actor": "comm", "backend": ""}},
		{"member clear", "automation set-decision-assistant ab12 clear=true member=m1", path,
			map[string]any{"actor": "comm", "clear": true, "member_id": "m1"}},
	})
}

func TestAutomationCommDecisionAssistantUsage(t *testing.T) {
	r, calls := setLLMFixture(t)
	runSetLLMUsage(t, r, calls, []string{
		"automation set-decision-assistant",
		"automation set-decision-assistant ab12",
		"automation set-decision-assistant ab12 clear=false",
	}, decisionAssistantUsagePrefix)

	// Input that cannot be a valid request names the problem, then the usage
	// line, and never reaches the daemon.
	for _, in := range []string{
		"automation set-decision-assistant ab12 backend=pi-prompt",
		"automation set-decision-assistant ab12 model=m",
		"automation set-decision-assistant ab12 mode=live",
		"automation set-decision-assistant ab12 clear=true backend=pi-prompt model=m",
		"automation set-decision-assistant ab12 bogus=1",
		"automation set-decision-assistant ab12 backend",
		"automation set-decision-assistant ab12 threshold.empty_edge=high backend=b model=m",
		"automation set-decision-assistant ab12 clear=maybe",
	} {
		*calls = nil
		resp := r.HandleTestMessage(in)
		if len(*calls) != 0 {
			t.Errorf("%q reached the daemon", in)
		}
		if len(resp) == 0 || !strings.Contains(resp[0], decisionAssistantUsagePrefix) {
			t.Errorf("%q reply = %v", in, resp)
		}
	}
}

func TestAutomationCommDecisionAssistantVerbListed(t *testing.T) {
	var listed bool
	for _, v := range AutomationCommVerbs() {
		listed = listed || v == "set-decision-assistant"
	}
	if !listed {
		t.Error("AutomationCommVerbs lacks set-decision-assistant")
	}
	if !strings.Contains(automationUsage, "set-decision-assistant") {
		t.Error("the automation usage line does not mention set-decision-assistant")
	}
}
