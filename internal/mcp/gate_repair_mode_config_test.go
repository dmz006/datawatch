package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// gateRepairModeRequest is one request the fake daemon received.
type gateRepairModeRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// gateRepairModeDaemon records every request the tool sends, so a test can see
// which path carried the mode, or that nothing was sent at all.
func gateRepairModeDaemon(t *testing.T) (*Server, *[]gateRepairModeRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []gateRepairModeRequest
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		mu.Lock()
		seen = append(seen, gateRepairModeRequest{Method: r.Method, Path: r.URL.Path, Body: body})
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	})
	return s, &seen
}

// Each valid mode is sent on PUT /api/config under the dotted daemon key, with
// no autonomous-manager body for it.
func TestGateRepairModeConfigSet_SendsValidValueToDaemonConfig(t *testing.T) {
	for _, want := range []string{"off", "report", "on"} {
		t.Run(want, func(t *testing.T) {
			s, seen := gateRepairModeDaemon(t)
			res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"gate_repair_mode": want}))
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError {
				t.Fatalf("valid mode %q was refused: %s", want, planningRestartPolicyResultText(res))
			}
			if len(*seen) != 1 {
				t.Fatalf("want one request, got %d: %+v", len(*seen), *seen)
			}
			got := (*seen)[0]
			if got.Method != http.MethodPut || got.Path != "/api/config" {
				t.Fatalf("request = %s %s, want PUT /api/config", got.Method, got.Path)
			}
			if got.Body["autonomous.gate_repair.mode"] != want {
				t.Fatalf("body = %v, want autonomous.gate_repair.mode=%q", got.Body, want)
			}
		})
	}
}

// A value that is not off, report or on is refused in the tool (and so is a
// non-string), and no request reaches the daemon. The refusal names the three
// accepted modes.
func TestGateRepairModeConfigSet_RefusesInvalidBeforeSending(t *testing.T) {
	for _, v := range []any{"bogus", "never", 7} {
		s, seen := gateRepairModeDaemon(t)
		res, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"gate_repair_mode": v}))
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("value %#v was accepted, want refused", v)
		}
		if len(*seen) != 0 {
			t.Fatalf("value %#v reached the daemon: %+v", v, *seen)
		}
		text := planningRestartPolicyResultText(res)
		for _, want := range []string{"off", "report", "on"} {
			if !strings.Contains(text, want) {
				t.Errorf("refusal for %#v does not name %q: %s", v, want, text)
			}
		}
	}
}

// When the caller does not mention the mode, no config request carries it.
func TestGateRepairModeConfigSet_AbsentIsNotSent(t *testing.T) {
	s, seen := gateRepairModeDaemon(t)
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{"capacity_enabled": true})); err != nil {
		t.Fatal(err)
	}
	for _, r := range *seen {
		if _, ok := r.Body["autonomous.gate_repair.mode"]; ok {
			t.Fatalf("mode sent though the caller never gave it: %+v", r)
		}
	}
}

// The schema names the three modes with an enum and uses Automaton wording
// rather than PRD.
func TestGateRepairModeConfigSet_SchemaDeclaresEnum(t *testing.T) {
	tool := (&Server{}).toolAutonomousConfigSet()
	prop, ok := tool.InputSchema.Properties["gate_repair_mode"].(map[string]any)
	if !ok {
		t.Fatal("autonomous_config_set has no gate_repair_mode property")
	}
	enum, _ := prop["enum"].([]string)
	want := []string{"off", "report", "on"}
	if len(enum) != len(want) {
		t.Fatalf("enum = %v, want %v", prop["enum"], want)
	}
	for i := range want {
		if enum[i] != want[i] {
			t.Fatalf("enum = %v, want %v", enum, want)
		}
	}
	desc, _ := prop["description"].(string)
	if !strings.Contains(desc, "autonomous.gate_repair.mode") {
		t.Errorf("description lacks the config key: %s", desc)
	}
	if strings.Contains(desc, "PRD") {
		t.Errorf("description uses PRD wording: %s", desc)
	}
}
