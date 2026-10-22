package mcp

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func usageBackoffReq(args map[string]any) mcpsdk.CallToolRequest {
	var req mcpsdk.CallToolRequest
	req.Params.Arguments = args
	return req
}

func usageBackoffText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

func TestUsageBackoff_ToolNamesAndRequiredArgs(t *testing.T) {
	s := &Server{}
	if got := s.toolUsageBackoffStatus().Name; got != "usage_backoff_status" {
		t.Errorf("status name = %q", got)
	}
	set := s.toolUsageBackoffSet()
	if set.Name != "usage_backoff_set" {
		t.Errorf("set name = %q", set.Name)
	}
	if len(set.InputSchema.Required) != 1 || set.InputSchema.Required[0] != "backend" {
		t.Errorf("set required = %v, want [backend]", set.InputSchema.Required)
	}
	for _, arg := range []string{"backend", "minutes", "until", "reason"} {
		if _, ok := set.InputSchema.Properties[arg]; !ok {
			t.Errorf("usage_backoff_set missing %q argument", arg)
		}
	}
	clr := s.toolUsageBackoffClear()
	if clr.Name != "usage_backoff_clear" {
		t.Errorf("clear name = %q", clr.Name)
	}
	if len(clr.InputSchema.Required) != 0 {
		t.Errorf("clear required = %v, want none", clr.InputSchema.Required)
	}
	if _, ok := clr.InputSchema.Properties["backend"]; !ok {
		t.Error("usage_backoff_clear missing backend argument")
	}
}

func TestUsageBackoff_SetArgValidation(t *testing.T) {
	// webPort 0: any call that reached the loopback would say so, so these
	// cases prove validation rejects before the request is made.
	s := &Server{}
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing backend", map[string]any{"minutes": 5.0}, "backend is required"},
		{"blank backend", map[string]any{"backend": "  ", "minutes": 5.0}, "backend is required"},
		{"minutes zero", map[string]any{"backend": "b", "minutes": 0.0}, "minutes must be >= 1"},
		{"minutes negative", map[string]any{"backend": "b", "minutes": -3.0}, "minutes must be >= 1"},
		{"both minutes and until", map[string]any{"backend": "b", "minutes": 5.0, "until": "2099-01-01T00:00:00Z"}, "exactly one of minutes or until"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := s.handleUsageBackoffSet(context.Background(), usageBackoffReq(c.args))
			if err != nil {
				t.Fatalf("err = %v, want tool error result", err)
			}
			if !res.IsError {
				t.Fatalf("want IsError result, got %q", usageBackoffText(t, res))
			}
			if txt := usageBackoffText(t, res); !strings.Contains(txt, c.want) {
				t.Errorf("error = %q, want it to contain %q", txt, c.want)
			}
		})
	}
}

func TestUsageBackoff_LoopbackUnavailable(t *testing.T) {
	s := &Server{}
	res, _ := s.handleUsageBackoffStatus(context.Background(), usageBackoffReq(nil))
	if !res.IsError {
		t.Error("status: want error when loopback is unavailable")
	}
	res, _ = s.handleUsageBackoffSet(context.Background(), usageBackoffReq(map[string]any{"backend": "b"}))
	if !res.IsError {
		t.Error("set: want error when loopback is unavailable")
	}
	res, _ = s.handleUsageBackoffClear(context.Background(), usageBackoffReq(nil))
	if !res.IsError {
		t.Error("clear: want error when loopback is unavailable")
	}
}

func TestUsageBackoff_DispatchToREST(t *testing.T) {
	var path, method string
	var body map[string]any
	s := newRejudgeTestServer(t, &path, &method, &body)
	call := func(fn func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error), args map[string]any) {
		t.Helper()
		path, method, body = "", "", map[string]any{}
		res, err := fn(context.Background(), usageBackoffReq(args))
		if err != nil || res.IsError {
			t.Fatalf("call failed: err=%v res=%v", err, res)
		}
	}

	call(s.handleUsageBackoffStatus, nil)
	if method != "GET" || path != "/api/usage-backoff" {
		t.Errorf("status -> %s %s", method, path)
	}

	call(s.handleUsageBackoffSet, map[string]any{"backend": " claude-code ", "minutes": 45.0, "reason": "weekly cap"})
	if method != "POST" || path != "/api/usage-backoff" {
		t.Errorf("set -> %s %s", method, path)
	}
	if body["backend"] != "claude-code" || body["minutes"] != 45.0 || body["reason"] != "weekly cap" {
		t.Errorf("set body = %v", body)
	}
	if _, ok := body["until"]; ok {
		t.Errorf("set body carried until: %v", body)
	}

	call(s.handleUsageBackoffSet, map[string]any{"backend": "pi", "until": "2099-01-01T00:00:00Z"})
	if body["until"] != "2099-01-01T00:00:00Z" {
		t.Errorf("set-until body = %v", body)
	}
	if _, ok := body["minutes"]; ok {
		t.Errorf("set-until body carried minutes: %v", body)
	}

	call(s.handleUsageBackoffClear, map[string]any{"backend": "pi"})
	if method != "DELETE" || path != "/api/usage-backoff" || body["backend"] != "pi" {
		t.Errorf("clear -> %s %s %v", method, path, body)
	}

	call(s.handleUsageBackoffClear, nil)
	if method != "DELETE" {
		t.Errorf("clear-all method = %s", method)
	}
	if _, ok := body["backend"]; ok {
		t.Errorf("clear-all body carried backend: %v", body)
	}
}

func TestUsageBackoff_ConfigSetForwardsNewKeys(t *testing.T) {
	var path, method string
	var body map[string]any
	s := newRejudgeTestServer(t, &path, &method, &body)

	body = map[string]any{}
	if _, err := s.handleAutonomousConfigSet(context.Background(), usageBackoffReq(map[string]any{
		"usage_backoff": "off", "backoff_max_wait_minutes": 90.0,
	})); err != nil {
		t.Fatal(err)
	}
	if body["usage_backoff"] != "off" || body["backoff_max_wait_minutes"] != 90.0 {
		t.Errorf("body = %v", body)
	}

	// Unset keys are not sent, so the stored values are kept.
	body = map[string]any{}
	if _, err := s.handleAutonomousConfigSet(context.Background(), usageBackoffReq(map[string]any{"poll_interval_seconds": 5.0})); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["usage_backoff"]; ok {
		t.Errorf("usage_backoff sent although unset: %v", body)
	}
	if _, ok := body["backoff_max_wait_minutes"]; ok {
		t.Errorf("backoff_max_wait_minutes sent although unset: %v", body)
	}

	props := s.toolAutonomousConfigSet().InputSchema.Properties
	for _, k := range []string{"usage_backoff", "backoff_max_wait_minutes"} {
		if _, ok := props[k]; !ok {
			t.Errorf("autonomous_config_set missing %q argument", k)
		}
	}
}
