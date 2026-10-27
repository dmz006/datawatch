package server

// REST surface smoke for the triage read: the very route
// GET /api/autonomous/triage serves triage records that a real Manager
// over a real file-backed store produced — no stubbed orchestrator, no
// canned JSON. The route is wired exactly as cmd/datawatch wires it.
// This is the same body the CLI, the MCP tool and the triage alert all
// read, so it also proves the read has one shape across every surface.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// seedTriageSmokeRecord creates one real failed Automaton and stores the
// triage record the coordinator's pass produces for it.
func seedTriageSmokeRecord(t *testing.T, m *autonomous.Manager) *autonomous.TriageRecord {
	t.Helper()
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
		t.Fatal("BuildTriageRecordFor returned no record for a failed Automaton")
	}
	if err := m.SaveTriageRecord(p.ID, rec); err != nil {
		t.Fatalf("SaveTriageRecord: %v", err)
	}
	return rec
}

func TestTriageSmokeOverREST(t *testing.T) {
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
	seedTriageSmokeRecord(t, m)

	// The production wiring: the daemon exposes the autonomous API
	// adapter over the Manager, and the route resolves the triage read
	// through it. Wiring the bare Manager here would hide that gap.
	s := &Server{}
	s.SetAutonomousAPI(autonomous.NewAPI(m))
	ts := httptest.NewServer(http.HandlerFunc(s.handleAutonomousTriage))
	defer ts.Close()

	get := func(rawQuery string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/autonomous/triage?"+rawQuery, nil)
		if err != nil {
			t.Fatalf("build GET: %v", err)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("GET /api/autonomous/triage?%s: %v", rawQuery, err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return resp.StatusCode, out
	}

	// No filters: the one waiting Automaton and the threshold in effect.
	st, raw := get("")
	if st != http.StatusOK {
		t.Fatalf("GET triage: status %d body=%s", st, raw)
	}
	var body struct {
		Records      []*autonomous.TriageRecord `json:"records"`
		AlertMinutes *int                       `json:"alert_minutes"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body does not parse: %v (raw %s)", err, raw)
	}
	if len(body.Records) != 1 {
		t.Fatalf("records = %d, want the one waiting Automaton", len(body.Records))
	}
	if body.AlertMinutes == nil || *body.AlertMinutes != 30 {
		t.Errorf("alert_minutes = %v, want 30", body.AlertMinutes)
	}
	r := body.Records[0]
	if r.Status != "failed" || r.TaskTitle != "run the build" || r.TaskID != "t-fail" {
		t.Errorf("record = %+v", r)
	}
	if r.Error != "the build session died before writing the plan" || r.SessionEndReason != "operator ended it" {
		t.Errorf("record error/reason = %q / %q", r.Error, r.SessionEndReason)
	}
	if r.RetryBudget.CeilingTotal != 7 || r.RetryBudget.CeilingLeft != 7 {
		t.Errorf("record ceiling = %d of %d, want 7 of 7", r.RetryBudget.CeilingLeft, r.RetryBudget.CeilingTotal)
	}

	// The filters the operator and the MCP pass use: each one reaches
	// the real route and narrows to the expected set.
	st, raw = get("status=failed")
	if st != http.StatusOK {
		t.Fatalf("status=failed: %d %s", st, raw)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("status=failed body: %v", err)
	}
	if len(body.Records) != 1 {
		t.Errorf("status=failed records = %d, want 1", len(body.Records))
	}
	st, raw = get("status=blocked")
	if st != http.StatusOK {
		t.Fatalf("status=blocked: %d %s", st, raw)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("status=blocked body: %v", err)
	}
	if len(body.Records) != 0 {
		t.Errorf("status=blocked records = %d, want 0", len(body.Records))
	}
	st, _ = get("status=crashed")
	if st != http.StatusBadRequest {
		t.Errorf("status=crashed: status %d, want 400", st)
	}
}
