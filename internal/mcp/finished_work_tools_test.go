package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestFinishedWorkToolsDefinitionAndCaps(t *testing.T) {
	s := &Server{}
	read, run := s.toolAutonomousFinishedWork(), s.toolAutonomousFinishedWorkRun()
	if read.Name != "autonomous_finished_work" || run.Name != "autonomous_finished_work_run" {
		t.Fatalf("names = %q, %q", read.Name, run.Name)
	}
	if read.Annotations.ReadOnlyHint == nil || !*read.Annotations.ReadOnlyHint || len(read.InputSchema.Properties) != 0 {
		t.Error("the digest tool must be read-only and take no arguments")
	}
	if got := federation.MCPToolCap["autonomous_finished_work"]; got != federation.CapAutonomousRead {
		t.Errorf("read tool cap = %q, want %q", got, federation.CapAutonomousRead)
	}
	if got := federation.MCPToolCap["autonomous_finished_work_run"]; got != federation.CapAutonomousWrite {
		t.Errorf("run tool cap = %q, want %q", got, federation.CapAutonomousWrite)
	}
	if len(FinishedWorkToolNames()) != 2 {
		t.Error("FinishedWorkToolNames must list both tools")
	}
}

func TestFinishedWorkToolsProxy(t *testing.T) {
	var path, method, body string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if res, err := s.handleAutonomousFinishedWork(context.Background(), callReq(nil)); err != nil || res.IsError {
		t.Fatalf("read: err=%v res=%+v", err, res)
	}
	if path != "/api/autonomous/finished-work" || method != http.MethodGet {
		t.Errorf("read proxied %s %s", method, path)
	}
	if res, _ := s.handleAutonomousFinishedWork(context.Background(), callReq(map[string]any{"x": 1})); res == nil || !res.IsError {
		t.Error("an argument to the read tool must be refused")
	}
	if res, err := s.handleAutonomousFinishedWorkRun(context.Background(), callReq(map[string]any{"run": "Audit"})); err != nil || res.IsError {
		t.Fatalf("run: err=%v res=%+v", err, res)
	}
	if path != "/api/autonomous/finished-work/run" || method != http.MethodPost || !strings.Contains(body, `"run":"audit"`) {
		t.Errorf("run proxied %s %s %s", method, path, body)
	}
}

func TestPRDCancelToolPassesTheDecision(t *testing.T) {
	var method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, rawQuery = r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	})
	if _, err := s.handleAutonomousPRDCancel(context.Background(), callReq(map[string]any{"id": "abcd0001", "decision": "keep", "reason": "for review"})); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || !strings.Contains(rawQuery, "decision=keep") || !strings.Contains(rawQuery, "reason=for+review") {
		t.Errorf("cancel proxied %s ?%s", method, rawQuery)
	}
	if _, err := s.handleAutonomousPRDCancel(context.Background(), callReq(map[string]any{"id": "abcd0001"})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawQuery, "decision") {
		t.Errorf("a plain cancel must carry no decision, got %q", rawQuery)
	}
}
