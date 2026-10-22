// automation_set_budget MCP tool and the budget view on the read tools,
// proxied against an httptest backend.

package mcp

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestMCPAutomationSetBudget(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12","status":"active","budget":{"state":"within"}}`)
	res, err := s.handleAutomationSetBudget(context.Background(), callReq(map[string]any{
		"automation_id": "ab12", "expected_version": float64(4), "actor": "alice", "reason": "weekly cap",
		"member_id": "m2", "ceiling_type": "dollar", "ceiling_value": 25.5, "window": "week",
		"pacing": "even", "on_breach": "pause", "backend": "claude-code",
		"allocation_estimate": true, "peer_window": []any{"laptop", "gpu-box"},
		"reset_time": "06:00", "reset_weekday": "monday", "reset_day": float64(0), "reset_zone": "America/New_York",
		"estimated_cost_usd": float64(3), "clear": false,
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/automations/ab12/set_budget" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	want := map[string]any{
		"expected_version": float64(4), "actor": "alice", "reason": "weekly cap",
		"member_id": "m2", "ceiling_type": "dollar", "ceiling_value": 25.5, "window": "week",
		"pacing": "even", "on_breach": "pause", "backend": "claude-code", "allocation_estimate": true,
		"reset_time": "06:00", "reset_weekday": "monday", "reset_zone": "America/New_York",
		"estimated_cost_usd": float64(3),
	}
	for k, v := range want {
		if rec.body[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, rec.body[k], v)
		}
	}
	peers, _ := rec.body["peer_window"].([]any)
	if len(peers) != 2 || peers[0] != "laptop" || peers[1] != "gpu-box" {
		t.Errorf("peer_window = %v", rec.body["peer_window"])
	}
	for _, k := range []string{"clear", "reset_day"} {
		if _, ok := rec.body[k]; ok {
			t.Errorf("%s should not be sent when false or zero: %v", k, rec.body)
		}
	}
	if text := automationText(t, res); !strings.Contains(text, `"budget"`) {
		t.Errorf("result = %s", text)
	}

	// clear alone, and a zero estimate on a member, are sent as given.
	if _, err := s.handleAutomationSetBudget(context.Background(), callReq(map[string]any{
		"automation_id": "ab12", "expected_version": float64(5), "clear": true,
	})); err != nil {
		t.Fatal(err)
	}
	if rec.body["clear"] != true || len(rec.body) != 2 {
		t.Errorf("clear body = %v", rec.body)
	}
	if _, err := s.handleAutomationSetBudget(context.Background(), callReq(map[string]any{
		"automation_id": "ab12", "expected_version": float64(6), "member_id": "m2", "estimated_cost_usd": float64(0),
	})); err != nil {
		t.Fatal(err)
	}
	if v, ok := rec.body["estimated_cost_usd"]; !ok || v != float64(0) {
		t.Errorf("zero estimate dropped: %v", rec.body)
	}
}

func TestMCPAutomationSetBudgetRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s, rec := activateServer(t, http.StatusBadRequest, "budget invalid: ceiling 0 must be positive")

	// Calls the tool rejects itself never reach the server.
	local := map[string]map[string]any{
		"no expected_version":    {"automation_id": "ab12", "ceiling_type": "dollar"},
		"no automation_id":       {"expected_version": float64(1), "ceiling_type": "dollar"},
		"ceiling_value a string": {"automation_id": "ab12", "expected_version": float64(1), "ceiling_value": "ten"},
		"clear not a bool":       {"automation_id": "ab12", "expected_version": float64(1), "clear": "yes"},
		"window not a string":    {"automation_id": "ab12", "expected_version": float64(1), "window": 7},
		"peer_window not array":  {"automation_id": "ab12", "expected_version": float64(1), "peer_window": "laptop"},
		"peer_window non-string": {"automation_id": "ab12", "expected_version": float64(1), "peer_window": []any{1}},
	}
	for name, args := range local {
		rec.method = ""
		res, err := s.handleAutomationSetBudget(ctx, callReq(args))
		if err != nil || res == nil {
			t.Fatalf("%s: err=%v res=%v", name, err, res)
		}
		if text := automationText(t, res); !strings.HasPrefix(text, "error:") {
			t.Errorf("%s: result = %s", name, text)
		}
		if rec.method != "" {
			t.Errorf("%s: reached the server", name)
		}
	}

	// A value the server refuses comes back as an error result.
	res, err := s.handleAutomationSetBudget(ctx, callReq(map[string]any{
		"automation_id": "ab12", "expected_version": float64(1), "ceiling_type": "dollar", "window": "week",
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "budget invalid") {
		t.Errorf("invalid value not surfaced: %s", text)
	}

	// An unknown Automation is a 404 surfaced as an error result.
	s, rec = activateServer(t, http.StatusNotFound, "automation nope not found")
	res, err = s.handleAutomationSetBudget(ctx, callReq(map[string]any{
		"automation_id": "nope", "expected_version": float64(1), "clear": true,
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "not found") {
		t.Errorf("unknown automation not surfaced: %s", text)
	}
	if rec.path != "/api/automations/nope/set_budget" {
		t.Errorf("proxied %s", rec.path)
	}
}

func TestMCPAutonomousStatusCarriesAutomationBudget(t *testing.T) {
	reply := `{"running":true,"automations":[{"id":"ab12","status":"paused","budget":{"state":"breached","resumes_at":"2026-10-26T00:00:00Z","allocation_pct":42,"allocation_estimated":true}}]}`
	s, rec := activateServer(t, http.StatusOK, reply)
	res, err := s.handleAutonomousStatus(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.path != "/api/autonomous/status" {
		t.Errorf("proxied %s", rec.path)
	}
	text := automationText(t, res)
	for _, want := range []string{`"automations"`, `"budget"`, `"breached"`, `"resumes_at"`, `"allocation_estimated"`} {
		if !strings.Contains(text, want) {
			t.Errorf("status text lost %s: %s", want, text)
		}
	}

	// The read tools proxy the REST views, which carry the same budget field.
	s, rec = activateServer(t, http.StatusOK, `{"id":"ab12","budget":{"state":"within"}}`)
	for name, call := range map[string]func() (string, error){
		"get": func() (string, error) {
			r, err := s.handleAutomationGet(context.Background(), callReq(map[string]any{"id": "ab12"}))
			return automationText(t, r), err
		},
		"list": func() (string, error) {
			r, err := s.handleAutomationList(context.Background(), callReq(nil))
			return automationText(t, r), err
		},
	} {
		got, err := call()
		if err != nil || !strings.Contains(got, `"budget"`) {
			t.Errorf("%s: err=%v result=%s", name, err, got)
		}
	}
	for name, d := range map[string]string{
		"automation_get":  (&Server{}).toolAutomationGet().Description,
		"automation_list": (&Server{}).toolAutomationList().Description,
	} {
		if !strings.Contains(d, "budget view") {
			t.Errorf("%s description does not mention the budget view: %s", name, d)
		}
	}
}

func TestMCPAutomationSetBudgetCapRegistered(t *testing.T) {
	tool := (&Server{}).toolAutomationSetBudget()
	if tool.Name != "automation_set_budget" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	if got, ok := federation.MCPToolCap["automation_set_budget"]; !ok || got != federation.CapAutonomousWrite {
		t.Errorf("capability = %v (present %v), want the write capability", got, ok)
	}

	props := tool.InputSchema.Properties
	for _, arg := range []string{"automation_id", "expected_version", "member_id", "ceiling_type", "ceiling_value",
		"window", "pacing", "on_breach", "backend", "allocation_estimate", "peer_window", "reset_time",
		"reset_weekday", "reset_day", "reset_zone", "estimated_cost_usd", "clear"} {
		if _, ok := props[arg]; !ok {
			t.Errorf("argument %s missing", arg)
		}
	}
	required := map[string]bool{}
	for _, r := range tool.InputSchema.Required {
		required[r] = true
	}
	if !required["automation_id"] || !required["expected_version"] || len(required) != 2 {
		t.Errorf("required = %v, want automation_id and expected_version only", tool.InputSchema.Required)
	}

	// The description states the operator decisions and carries no backlog ID.
	d := strings.ToLower(tool.Description)
	for _, want := range []string{"resumes by itself", "only when the reset time is known", "blocked",
		"never pauses or blocks", "only for peers named", "not confirmed automatically"} {
		if !strings.Contains(d, want) {
			t.Errorf("description does not say %q", want)
		}
	}
	if regexp.MustCompile(`\bBL-?\d+`).MatchString(tool.Description) {
		t.Errorf("description holds a backlog identifier: %s", tool.Description)
	}
}
