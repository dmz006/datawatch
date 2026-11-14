package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestGateRepairMCPToolsAreListed(t *testing.T) {
	// Schema shape.
	status := (&Server{}).toolGateRepairStatus()
	if status.Name != "autonomous_gate_repair_status" {
		t.Fatalf("status tool name = %q", status.Name)
	}
	if status.Annotations.ReadOnlyHint == nil || !*status.Annotations.ReadOnlyHint {
		t.Error("status tool must be read-only")
	}
	if len(status.InputSchema.Properties) != 0 {
		t.Errorf("status tool takes no arguments, schema has %v", status.InputSchema.Properties)
	}
	run := (&Server{}).toolGateRepairRun()
	if run.Name != "autonomous_gate_repair_run" {
		t.Fatalf("run tool name = %q", run.Name)
	}
	for _, arg := range []string{"automaton_id", "task_id"} {
		if _, ok := run.InputSchema.Properties[arg]; !ok {
			t.Errorf("run tool schema lacks the %q argument", arg)
		}
	}

	// Capabilities.
	if got := federation.MCPToolCap["autonomous_gate_repair_status"]; got != federation.CapAutonomousRead {
		t.Errorf("status capability = %q, want %q", got, federation.CapAutonomousRead)
	}
	if got := federation.MCPToolCap["autonomous_gate_repair_run"]; got != federation.CapAutonomousRun {
		t.Errorf("run capability = %q, want %q", got, federation.CapAutonomousRun)
	}
}

// gateRepairRecorder records method, path and the decoded body of each request
// the tool forwards to the fake daemon.
type gateRepairRecorder struct {
	method string
	path   string
	body   map[string]any
	called bool
}

func newGateRepairRecorder(wantStatus string) (handler http.HandlerFunc, last *gateRepairRecorder) {
	last = &gateRepairRecorder{}
	h := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		*last = gateRepairRecorder{method: r.Method, path: r.URL.Path, body: body, called: true}
		_, _ = w.Write([]byte(wantStatus))
	}
	return h, last
}

func TestGateRepairMCPStatusProxiesGet(t *testing.T) {
	want := `{"mode":"report","budget":["aut/1/infra"],"records":[]}`
	h, last := newGateRepairRecorder(want)
	s := capacityTestServer(t, h)
	res, err := s.handleGateRepairStatus(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if last.method != http.MethodGet || last.path != "/api/autonomous/gate-repair" {
		t.Errorf("proxied %s %s, want GET /api/autonomous/gate-repair", last.method, last.path)
	}
	if !strings.Contains(automationText(t, res), `"mode":"report"`) {
		t.Errorf("body not passed through: %s", automationText(t, res))
	}
}

func TestGateRepairMCPStatusRefusesArguments(t *testing.T) {
	h, last := newGateRepairRecorder(`{}`)
	s := capacityTestServer(t, h)
	res, err := s.handleGateRepairStatus(context.Background(), callReq(map[string]any{"mode": "on"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want a tool error, got err=%v res=%+v", err, res)
	}
	if last.called {
		t.Error("a refused call must not reach the daemon")
	}
	if !strings.Contains(automationText(t, res), "mode") {
		t.Errorf("error does not name the argument: %s", automationText(t, res))
	}
}

func TestGateRepairMCPRunProxiesSingleGate(t *testing.T) {
	h, last := newGateRepairRecorder(`{"runs":[],"status":{}}`)
	s := capacityTestServer(t, h)
	res, err := s.handleGateRepairRun(context.Background(), callReq(map[string]any{"automaton_id": "aut", "task_id": "t1"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if last.method != http.MethodPost || last.path != "/api/autonomous/gate-repair/run" {
		t.Errorf("proxied %s %s, want POST /api/autonomous/gate-repair/run", last.method, last.path)
	}
	if last.body["automaton_id"] != "aut" || last.body["task_id"] != "t1" {
		t.Errorf("body = %v, want both ids", last.body)
	}
}

func TestGateRepairMCPRunProxiesPass(t *testing.T) {
	h, last := newGateRepairRecorder(`{"runs":[],"status":{}}`)
	s := capacityTestServer(t, h)
	res, err := s.handleGateRepairRun(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if last.method != http.MethodPost || last.path != "/api/autonomous/gate-repair/run" {
		t.Errorf("proxied %s %s, want POST /api/autonomous/gate-repair/run", last.method, last.path)
	}
	if len(last.body) != 0 {
		t.Errorf("a pass must send an empty body, got %v", last.body)
	}
}

func TestGateRepairMCPRunRefusesOneOfTwo(t *testing.T) {
	for _, body := range []map[string]any{
		{"automaton_id": "aut"},
		{"task_id": "t1"},
	} {
		h, last := newGateRepairRecorder(`{}`)
		s := capacityTestServer(t, h)
		res, err := s.handleGateRepairRun(context.Background(), callReq(body))
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("args %v: want a tool error, got err=%v res=%+v", body, err, res)
		}
		if last.called {
			t.Errorf("args %v: a refused call must not reach the daemon", body)
		}
		text := automationText(t, res)
		if _, hasAuto := body["automaton_id"]; hasAuto && !strings.Contains(text, "task_id") {
			t.Errorf("refusal with only automaton_id must name task_id: %s", text)
		}
		if _, hasTask := body["task_id"]; hasTask && !strings.Contains(text, "automaton_id") {
			t.Errorf("refusal with only task_id must name automaton_id: %s", text)
		}
	}
}

func TestGateRepairMCPRunRefusesUnknownArg(t *testing.T) {
	h, last := newGateRepairRecorder(`{}`)
	s := capacityTestServer(t, h)
	res, err := s.handleGateRepairRun(context.Background(), callReq(map[string]any{"mode": "on"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want a tool error, got err=%v res=%+v", err, res)
	}
	if last.called {
		t.Error("a refused call must not reach the daemon")
	}
	if !strings.Contains(automationText(t, res), "mode") {
		t.Errorf("error does not name the argument: %s", automationText(t, res))
	}
}

func TestGateRepairMCPStatusBackendError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
	})
	res, err := s.handleGateRepairStatus(context.Background(), callReq(nil))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want a tool error for a 503 backend, got err=%v res=%+v", err, res)
	}
}

func TestGateRepairMCPToolNames(t *testing.T) {
	got := GateRepairToolNames()
	if len(got) != 2 || got[0] != "autonomous_gate_repair_status" || got[1] != "autonomous_gate_repair_run" {
		t.Errorf("names = %v", got)
	}
}
