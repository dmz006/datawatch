package mcp

// MCP surface smoke for the autonomous_triage tool: the tool's proxy
// reaches a daemon route that serves triage records a real Manager over
// a real file-backed store produced. The MCP layer itself is fully real
// (tool spec and proxy), the daemon side is the real route over the
// real store, so this proves the tool text the operator's MCP client
// reads carries the real failed task, error and threshold.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestTriageSmokeToolServesRealRecords(t *testing.T) {
	dir := t.TempDir()
	cfg := autonomous.DefaultConfig()
	th := 30
	cfg.Automation.TriageAlertMinutes = &th
	cfg.RetryPolicy = map[string]int{"infra": 2, "content": 1, "scope": 1, "policy": 1}
	cfg.RetryCeilingTotal = 7
	m, err := autonomous.NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Stop()

	p, err := m.CreatePRD("spec build the queue", "/smoke", "pi-prompt", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	p.Title = "build the queue"
	p.Status = autonomous.PRDFailed
	p.Story = []autonomous.Story{{Tasks: []autonomous.Task{{
		ID:               "t-fail",
		Title:            "run the build",
		Status:           autonomous.TaskFailed,
		Error:            "the build session died before writing the plan",
		SessionEndReason: "operator ended it",
	}}}}
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("save PRD: %v", err)
	}
	rec := m.BuildTriageRecordFor(p)
	if rec == nil {
		t.Fatal("no triage record for a failed Automaton")
	}
	if err := m.SaveTriageRecord(p.ID, rec); err != nil {
		t.Fatalf("SaveTriageRecord: %v", err)
	}

	// The daemon side of the proxy: the real route's JSON, built from
	// the real manager's view of the store.
	srv := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/triage" || r.Method != http.MethodGet {
			http.Error(w, "unexpected triage route call: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"records":       m.ListTriageRecords(),
			"alert_minutes": m.TriageAlertMinutes(),
		})
	})

	res, err := srv.handleAutonomousTriage(context.Background(), callReq(nil))
	if err != nil {
		t.Fatalf("autonomous_triage: %v", err)
	}
	text := automationText(t, res)
	if !strings.Contains(text, `"alert_minutes":30`) {
		t.Errorf("tool result lost the threshold: %s", text)
	}
	for _, want := range []string{
		`"error":"the build session died before writing the plan"`,
		`"session_end_reason":"operator ended it"`,
		`"status":"failed"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("tool result lacks %q\ngot:\n%s", want, text)
		}
	}
}
