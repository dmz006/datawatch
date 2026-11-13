// REST wiring for the three daemon-task settings:
//
//   - autonomous.task_templates.mode          (object {template id: mode},
//     and the dotted form autonomous.task_templates.mode.<id>)
//   - autonomous.script_gate_timeout_seconds  (whole seconds, 0 = no cap)
//   - autonomous.project_gate_command         (single line of shell text)
//
// Each must be readable on GET /api/config, settable on PUT /api/config,
// propagate to the live autonomous manager, survive the YAML save, and be
// refused (400, nothing stored) when invalid.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// putDaemonTaskConfig sends a raw PUT /api/config and returns the status
// code and body (unlike putJSON, a non-2xx response is not an error here —
// the refusal tests assert on the 400 and the message).
func putDaemonTaskConfig(t *testing.T, s *Server, body map[string]any) (int, string) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal PUT body: %v", err)
	}
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr.Code, rr.Body.String()
}

// daemonTaskConfigGetMode decodes GET /api/config
// autonomous.task_templates.mode into a plain map.
func daemonTaskConfigGetMode(t *testing.T, s *Server) map[string]string {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["task_templates"]
	if !ok {
		t.Fatalf("GET /api/config autonomous lacks task_templates")
	}
	var tt struct {
		Mode map[string]string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &tt); err != nil {
		t.Fatalf("decode autonomous.task_templates: %v", err)
	}
	if tt.Mode == nil {
		tt.Mode = map[string]string{}
	}
	return tt.Mode
}

// daemonTaskConfigGetString decodes GET /api/config autonomous[key] as a
// JSON string (unescaping, so a command with && compares equal to the
// plain shell text).
func daemonTaskConfigGetString(t *testing.T, s *Server, key string) string {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)[key]
	if !ok {
		t.Fatalf("GET /api/config autonomous lacks %s", key)
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode autonomous.%s as string: %v", key, err)
	}
	return out
}

// daemonTaskConfigGetInt decodes GET /api/config autonomous[key] as a JSON
// number and returns its string form for stable comparison.
func daemonTaskConfigGetInt(t *testing.T, s *Server, key string) string {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)[key]
	if !ok {
		t.Fatalf("GET /api/config autonomous lacks %s", key)
	}
	var out int
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode autonomous.%s as int: %v", key, err)
	}
	return strconv.Itoa(out)
}

// TestDaemonTaskConfigAPI_ModeObjectRoundTrip: the whole
// autonomous.task_templates.mode object PUTs, reads back on GET, propagates
// to the live manager and is saved to the config file.
func TestDaemonTaskConfigAPI_ModeObjectRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	code, body := putDaemonTaskConfig(t, s, map[string]any{
		"autonomous.task_templates.mode": map[string]any{
			"final_gate": "off",
			"docs_set":   "llm",
		},
	})
	if code != http.StatusOK {
		t.Fatalf("PUT task_templates.mode: want 200, got %d body=%s", code, body)
	}
	mode := daemonTaskConfigGetMode(t, s)
	if mode["final_gate"] != "off" || mode["docs_set"] != "llm" {
		t.Fatalf("GET task_templates.mode = %v, want final_gate=off docs_set=llm", mode)
	}
	live := mgr.Config().TaskTemplates.Mode
	if live["final_gate"] != "off" || live["docs_set"] != "llm" {
		t.Fatalf("live manager TaskTemplates.Mode = %v, want final_gate=off docs_set=llm", live)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload saved config: %v", err)
	}
	if loaded.Autonomous.TaskTemplates.Mode["final_gate"] != "off" {
		t.Fatalf("saved config task_templates.mode[final_gate] = %q, want off",
			loaded.Autonomous.TaskTemplates.Mode["final_gate"])
	}
}

// TestDaemonTaskConfigAPI_ModeDottedKeyRoundTrip: the dotted form
// autonomous.task_templates.mode.<id> sets one template at a time.
func TestDaemonTaskConfigAPI_ModeDottedKeyRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	code, body := putDaemonTaskConfig(t, s, map[string]any{
		"autonomous.task_templates.mode.final_gate": "off",
	})
	if code != http.StatusOK {
		t.Fatalf("PUT task_templates.mode.final_gate: want 200, got %d body=%s", code, body)
	}
	if mode := daemonTaskConfigGetMode(t, s); mode["final_gate"] != "off" {
		t.Fatalf("GET task_templates.mode = %v, want final_gate=off", mode)
	}
	if got := mgr.Config().TaskTemplates.Mode["final_gate"]; got != "off" {
		t.Fatalf("live manager mode[final_gate] = %q, want off", got)
	}
	// A second dotted PUT changes the same id without dropping it.
	code, body = putDaemonTaskConfig(t, s, map[string]any{
		"autonomous.task_templates.mode.final_gate": "llm",
	})
	if code != http.StatusOK {
		t.Fatalf("PUT task_templates.mode.final_gate (llm): want 200, got %d body=%s", code, body)
	}
	if mode := daemonTaskConfigGetMode(t, s); mode["final_gate"] != "llm" {
		t.Fatalf("GET task_templates.mode = %v, want final_gate=llm", mode)
	}
}

// TestDaemonTaskConfigAPI_ModeRefuses: every illegal mode value rejects the
// whole PUT with the shared load-time rule's message and stores nothing.
func TestDaemonTaskConfigAPI_ModeRefuses(t *testing.T) {
	rows := []struct {
		name string
		body map[string]any
		msg  string // the rejection must name this
	}{
		{"unknown mode object",
			map[string]any{"autonomous.task_templates.mode": map[string]any{"final_gate": "banana"}},
			"unknown mode"},
		{"unknown mode dotted",
			map[string]any{"autonomous.task_templates.mode.docs_set": "banana"},
			"unknown mode"},
		{"daemon on final_gate",
			map[string]any{"autonomous.task_templates.mode": map[string]any{"final_gate": "daemon"}},
			"daemon is not allowed for final_gate"},
		{"daemon on non-capable id",
			map[string]any{"autonomous.task_templates.mode.docs_set": "daemon"},
			"daemon needs a daemon-capable id"},
		{"non-string entry object",
			map[string]any{"autonomous.task_templates.mode": map[string]any{"final_gate": 5}},
			"must be"},
		{"non-object mode map",
			map[string]any{"autonomous.task_templates.mode": "llm"},
			"must be an object"},
		{"non-string dotted value",
			map[string]any{"autonomous.task_templates.mode.final_gate": true},
			"must be a string"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			code, body := putDaemonTaskConfig(t, s, row.body)
			if code != http.StatusBadRequest {
				t.Fatalf("PUT %v: want 400, got %d body=%s", row.body, code, body)
			}
			if !strings.Contains(body, row.msg) {
				t.Fatalf("400 body %q does not name the rule %q", body, row.msg)
			}
			// Nothing stored: the stored map stays empty on GET and the live
			// manager never sees a value.
			if mode := daemonTaskConfigGetMode(t, s); len(mode) != 0 {
				t.Fatalf("stored mode after rejected PUT = %v, want {}", mode)
			}
			if live := mgr.Config().TaskTemplates.Mode; len(live) != 0 {
				t.Fatalf("live manager mode after rejected PUT = %v, want {}", live)
			}
		})
	}
}

// TestDaemonTaskConfigAPI_ScriptGateTimeoutRoundTrip: a whole number of
// seconds PUTs, reads back, propagates and is saved (0 = no cap sticks).
func TestDaemonTaskConfigAPI_ScriptGateTimeoutRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	code, body := putDaemonTaskConfig(t, s, map[string]any{
		"autonomous.script_gate_timeout_seconds": 120,
	})
	if code != http.StatusOK {
		t.Fatalf("PUT script_gate_timeout_seconds: want 200, got %d body=%s", code, body)
	}
	if got := daemonTaskConfigGetInt(t, s, "script_gate_timeout_seconds"); got != "120" {
		t.Fatalf("GET script_gate_timeout_seconds = %s, want 120", got)
	}
	if got := mgr.Config().ScriptGateTimeoutSeconds; got != 120 {
		t.Fatalf("live manager ScriptGateTimeoutSeconds = %d, want 120", got)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload saved config: %v", err)
	}
	if got := loaded.Autonomous.ScriptGateTimeoutSeconds; got != 120 {
		t.Fatalf("saved ScriptGateTimeoutSeconds = %d, want 120", got)
	}
	// 0 = no cap sticks as 0 (it is not an absent key).
	code, body = putDaemonTaskConfig(t, s, map[string]any{
		"autonomous.script_gate_timeout_seconds": 0,
	})
	if code != http.StatusOK {
		t.Fatalf("PUT script_gate_timeout_seconds 0: want 200, got %d body=%s", code, body)
	}
	if got := daemonTaskConfigGetInt(t, s, "script_gate_timeout_seconds"); got != "0" {
		t.Fatalf("GET script_gate_timeout_seconds = %s, want 0", got)
	}
}

// TestDaemonTaskConfigAPI_ScriptGateTimeoutRefuses: a negative, non-whole
// or non-numeric value rejects the whole PUT and stores nothing.
func TestDaemonTaskConfigAPI_ScriptGateTimeoutRefuses(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	before := daemonTaskConfigGetInt(t, s, "script_gate_timeout_seconds")
	liveBefore := mgr.Config().ScriptGateTimeoutSeconds
	for _, bad := range []any{
		-1,    // negative
		2.5,   // not a whole number
		"abc", // not a number
		true,  // wrong type
	} {
		code, body := putDaemonTaskConfig(t, s, map[string]any{
			"autonomous.script_gate_timeout_seconds": bad,
		})
		if code != http.StatusBadRequest {
			t.Fatalf("PUT script_gate_timeout_seconds %v: want 400, got %d body=%s", bad, code, body)
		}
		if !strings.Contains(body, "whole number of seconds >= 0") {
			t.Fatalf("400 body %q does not name the rule", body)
		}
	}
	if got := daemonTaskConfigGetInt(t, s, "script_gate_timeout_seconds"); got != before {
		t.Fatalf("stored value after rejected PUTs = %s, want %s", got, before)
	}
	if got := mgr.Config().ScriptGateTimeoutSeconds; got != liveBefore {
		t.Fatalf("live manager value after rejected PUTs = %d, want %d", got, liveBefore)
	}
}

// TestDaemonTaskConfigAPI_ProjectGateCommandRoundTrip: a single-line shell
// command PUTs, reads back, propagates and is saved.
func TestDaemonTaskConfigAPI_ProjectGateCommandRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	cmd := "rtk go build ./... && rtk go test ./..."
	code, body := putDaemonTaskConfig(t, s, map[string]any{
		"autonomous.project_gate_command": cmd,
	})
	if code != http.StatusOK {
		t.Fatalf("PUT project_gate_command: want 200, got %d body=%s", code, body)
	}
	if got := daemonTaskConfigGetString(t, s, "project_gate_command"); got != cmd {
		t.Fatalf("GET project_gate_command = %s, want %q", got, cmd)
	}
	if got := mgr.Config().ProjectGateCommand; got != cmd {
		t.Fatalf("live manager ProjectGateCommand = %q, want %q", got, cmd)
	}
	saved, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(saved), "project_gate_command: "+cmd) {
		t.Fatalf("saved config lacks project_gate_command %q", cmd)
	}
}

// TestDaemonTaskConfigAPI_ProjectGateCommandRefuses: a multi-line command,
// a carriage return, an over-long command or a non-string value rejects the
// whole PUT and stores nothing.
func TestDaemonTaskConfigAPI_ProjectGateCommandRefuses(t *testing.T) {
	rows := []struct {
		name string
		bad  any
		msg  string
	}{
		{"newline", "go test ./...\ngo vet ./...", "single line"},
		{"carriage return", "go test ./...\r\necho done", "single line"},
		{"over long", strings.Repeat("x", 2000), "1024"},
		{"non string", 42, "must be a string"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			before := daemonTaskConfigGetString(t, s, "project_gate_command")
			liveBefore := mgr.Config().ProjectGateCommand
			code, body := putDaemonTaskConfig(t, s, map[string]any{
				"autonomous.project_gate_command": row.bad,
			})
			if code != http.StatusBadRequest {
				t.Fatalf("PUT project_gate_command %v: want 400, got %d body=%s", row.bad, code, body)
			}
			if !strings.Contains(body, row.msg) {
				t.Fatalf("400 body %q does not name the rule %q", body, row.msg)
			}
			if got := daemonTaskConfigGetString(t, s, "project_gate_command"); got != before {
				t.Fatalf("stored value after rejected PUT = %s, want %s", got, before)
			}
			if got := mgr.Config().ProjectGateCommand; got != liveBefore {
				t.Fatalf("live manager value after rejected PUT = %q, want %q", got, liveBefore)
			}
		})
	}
}
