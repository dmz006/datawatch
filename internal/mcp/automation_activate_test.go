// automation_activate tool, the autonomous_status automations array and the
// automation_idle_alert_minutes config argument, proxied against an
// httptest backend.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type activateRecord struct {
	method, path string
	body         map[string]any
}

// activateServer answers every request with reply (status code ok) and
// records the last one.
func activateServer(t *testing.T, code int, reply string) (*Server, *activateRecord) {
	t.Helper()
	rec := &activateRecord{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		rec.method, rec.path, rec.body = r.Method, r.URL.Path, nil
		_ = json.Unmarshal(raw, &rec.body)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}, rec
}

func TestAutomationActivateToolName(t *testing.T) {
	if got := (&Server{}).toolAutomationActivate().Name; got != "automation_activate" {
		t.Fatalf("tool name = %q", got)
	}
}

func TestAutomationActivateProxiesPOST(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"id":"ab12","status":"active"}`)
	res, err := s.handleAutomationActivate(context.Background(), callReq(map[string]any{
		"id": "ab12", "actor": "alice", "reason": "ship it",
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/automations/ab12/activate" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["actor"] != "alice" || rec.body["reason"] != "ship it" {
		t.Errorf("body = %v", rec.body)
	}
	if text := automationText(t, res); !strings.Contains(text, `"active"`) {
		t.Errorf("result = %s", text)
	}
}

func TestAutomationActivateOmitsEmptyFieldsAndReportsConflict(t *testing.T) {
	s, rec := activateServer(t, http.StatusConflict, "automation ab12 is active, only a draft can be activated")
	res, err := s.handleAutomationActivate(context.Background(), callReq(map[string]any{"id": "ab12"}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if _, ok := rec.body["actor"]; ok {
		t.Errorf("empty actor should not be sent: %v", rec.body)
	}
	if text := automationText(t, res); !strings.HasPrefix(text, "error:") || !strings.Contains(text, "only a draft") {
		t.Errorf("conflict not surfaced: %s", text)
	}
}

func TestAutomationStatusToolReturnsAutomationsArray(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{"running":true,"automations":[{"id":"ab12","status":"active","pending_members":2}]}`)
	res, err := s.handleAutonomousStatus(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.path != "/api/autonomous/status" {
		t.Errorf("proxied %s", rec.path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"automations"`) || !strings.Contains(text, "pending_members") {
		t.Errorf("status text lost the automations array: %s", text)
	}
	if d := (&Server{}).toolAutonomousStatus().Description; !strings.Contains(d, "automations") {
		t.Errorf("autonomous_status description does not mention automations: %s", d)
	}
}

func TestAutomationIdleAlertConfigSetForwardsNestedValue(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	// An explicit 0 (off) must be sent, not dropped.
	for _, v := range []float64{45, 0} {
		if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
			"automation_idle_alert_minutes": v,
		})); err != nil {
			t.Fatalf("config set: %v", err)
		}
		if rec.method != http.MethodPut || rec.path != "/api/autonomous/config" {
			t.Errorf("proxied %s %s", rec.method, rec.path)
		}
		auto, _ := rec.body["automation"].(map[string]any)
		if got, ok := auto["idle_alert_minutes"].(float64); !ok || got != v {
			t.Errorf("automation = %v, want idle_alert_minutes %v", rec.body["automation"], v)
		}
	}
	// Absent argument sends no automation key.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"poll_interval_seconds": 5})); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.body["automation"]; ok {
		t.Errorf("automation key sent without the argument: %v", rec.body)
	}
	if !strings.Contains((&Server{}).toolAutonomousConfigSet().Description+configSetArgNames(), "automation_idle_alert_minutes") {
		t.Error("autonomous_config_set does not declare automation_idle_alert_minutes")
	}
}

func configSetArgNames() string {
	b, _ := json.Marshal((&Server{}).toolAutonomousConfigSet().InputSchema.Properties)
	return string(b)
}
