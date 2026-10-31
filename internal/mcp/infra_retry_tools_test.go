package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestInfraRetry_ToolSchema(t *testing.T) {
	tool := (&Server{}).toolAutonomousInfraRetries()
	if tool.Name != "autonomous_infra_retries" {
		t.Fatalf("name = %q", tool.Name)
	}
	for _, p := range []string{"automaton", "limit"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("tool lacks the %s parameter", p)
		}
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("both parameters must be optional, got required %v", tool.InputSchema.Required)
	}
	if strings.Contains(tool.Description, "PRD") {
		t.Errorf("description must say Automaton, not PRD: %s", tool.Description)
	}
}

func TestInfraRetry_ReadOnlyAnnotation(t *testing.T) {
	tool := (&Server{}).toolAutonomousInfraRetries()
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("autonomous_infra_retries must be marked read-only")
	}
}

func TestInfraRetry_ToolIsListed(t *testing.T) {
	s := New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools {
		if tl.Name == "autonomous_infra_retries" {
			return
		}
	}
	t.Error("autonomous_infra_retries is not registered")
}

func TestInfraRetry_ProxiesGETWithAutomatonAndLimit(t *testing.T) {
	var path, method string
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, query = r.URL.Path, r.Method, r.URL.Query()
		_, _ = w.Write([]byte(`{"settings":{"enabled":true},"tasks":[]}`))
	})
	res, err := s.handleAutonomousInfraRetries(context.Background(), callReq(map[string]any{
		"automaton": "ab12", "limit": float64(25),
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/infra-retries" {
		t.Errorf("proxied %s %s", method, path)
	}
	if got := query["automaton"]; len(got) != 1 || got[0] != "ab12" {
		t.Errorf("automaton query = %v, want [ab12]", got)
	}
	if got := query["limit"]; len(got) != 1 || got[0] != "25" {
		t.Errorf("limit query = %v, want [25]", got)
	}
	if text := automationText(t, res); !strings.Contains(text, `"settings"`) {
		t.Errorf("result lost the response body: %s", text)
	}
}

func TestInfraRetry_LimitAsText(t *testing.T) {
	var query map[string][]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousInfraRetries(context.Background(), callReq(map[string]any{"limit": "7"})); err != nil {
		t.Fatal(err)
	}
	if got := query["limit"]; len(got) != 1 || got[0] != "7" {
		t.Errorf("limit query = %v, want [7]", got)
	}
}

func TestInfraRetry_NoQueryWhenArgumentsAbsent(t *testing.T) {
	var rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := s.handleAutonomousInfraRetries(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if rawQuery != "" {
		t.Errorf("query sent without arguments: %q", rawQuery)
	}
}

func TestInfraRetry_ErrorResponseIsPassedBack(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "limit must be a whole number from 1 to 1000", http.StatusBadRequest)
	})
	res, err := s.handleAutonomousInfraRetries(context.Background(), callReq(map[string]any{"limit": float64(0)}))
	if err != nil {
		t.Fatalf("a refusal must come back as a tool result, not a protocol error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a 400 from the daemon must come back as a tool error: %+v", res)
	}
	if text := automationText(t, res); !strings.Contains(text, "limit must be a whole number") {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestInfraRetry_ConfigSetReachesPutBodyUnchanged(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	s.cfg = &config.MCPConfig{AllowSelfConfig: true}
	res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{
		"key": "autonomous.infra_retry.max_tries", "value": "5",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/config" {
		t.Fatalf("proxied %s %s, want PUT /api/config", rec.method, rec.path)
	}
	if len(rec.body) != 1 {
		t.Fatalf("body = %v, want exactly the one key", rec.body)
	}
	if got, ok := rec.body["autonomous.infra_retry.max_tries"]; !ok || got != float64(5) {
		t.Errorf("body = %v, want autonomous.infra_retry.max_tries = 5", rec.body)
	}
}

func TestInfraRetry_ConfigSetPassesListValueUnchanged(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	s.cfg = &config.MCPConfig{AllowSelfConfig: true}
	if _, err := s.handleConfigSet(context.Background(), callReq(map[string]any{
		"key": "autonomous.infra_retry.backoff_seconds", "value": "[10,60,300]",
	})); err != nil {
		t.Fatal(err)
	}
	list, _ := rec.body["autonomous.infra_retry.backoff_seconds"].([]any)
	if len(list) != 3 || list[0] != float64(10) || list[2] != float64(300) {
		t.Errorf("body = %v, want the list [10 60 300]", rec.body)
	}
}

func TestInfraRetry_ConfigSetRefusalIsPassedBack(t *testing.T) {
	s, _ := activateServer(t, http.StatusBadRequest, "autonomous.infra_retry.max_tries must be a whole number from 1 to 1000")
	s.cfg = &config.MCPConfig{AllowSelfConfig: true}
	res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{
		"key": "autonomous.infra_retry.max_tries", "value": "0",
	}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a refused value must be a tool error: err=%v res=%+v", err, res)
	}
	if text := automationText(t, res); !strings.Contains(text, "must be a whole number") {
		t.Errorf("refusal text lost: %s", text)
	}
}

func TestInfraRetry_ConfigGetShowsTheBlock(t *testing.T) {
	var path, method string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"enabled":true,"infra_retry":{"enabled":true,"max_tries":3}}`))
	})
	res, err := s.handleAutonomousConfigGet(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/config" {
		t.Errorf("proxied %s %s", method, path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"infra_retry":{"enabled":true,"max_tries":3}`) {
		t.Errorf("autonomous_config_get dropped the infra_retry block: %s", text)
	}
}
