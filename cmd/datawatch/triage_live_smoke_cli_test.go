package main

// CLI surface smoke for `datawatch autonomous triage`: the command
// talks to a daemon endpoint that serves triage records a real Manager
// over a real file-backed store produced, and renders them through the
// same triage text the operator sees in the triage alert — one wording
// everywhere. No fake orchestrator, no canned body: the record comes
// from the real store via the real route's JSON shape.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestTriageSmokeCLI(t *testing.T) {
	// Real manager, real record.
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

	// The daemon endpoint the command calls: the real route's JSON shape
	// built from the real manager's view of the store, with the query
	// recorded so the command's filters are checked too.
	var gotPath, gotMethod string
	var gotQuery url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotQuery = r.URL.Path, r.Method, r.URL.Query()
		if r.URL.Path != "/api/autonomous/triage" || r.Method != http.MethodGet {
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		st := gotQuery.Get("status")
		var recs []*autonomous.TriageRecord
		for _, rcd := range m.ListTriageRecords() {
			if st == "" || rcd.Status == st {
				recs = append(recs, rcd)
			}
		}
		if recs == nil {
			recs = []*autonomous.TriageRecord{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"records":       recs,
			"alert_minutes": m.TriageAlertMinutes(),
		})
	}))
	defer ts.Close()

	// Point the command at this daemon through its config, the way the
	// other CLI smoke tests do.
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("daemon URL: %v", err)
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out := func(args ...string) (string, error) {
		t.Helper()
		gotPath, gotQuery = "", nil
		c := newAutonomousTriageCmd()
		var buf bytes.Buffer
		c.SetOut(&buf)
		c.SetErr(&buf)
		c.SetArgs(args)
		if err := c.Execute(); err != nil {
			return buf.String(), err
		}
		return buf.String(), nil
	}

	got, err := out()
	if err != nil {
		t.Fatalf("datawatch autonomous triage: %v (out: %s)", err, got)
	}
	if gotPath != "/api/autonomous/triage" || gotMethod != http.MethodGet {
		t.Errorf("called %s %s, want GET /api/autonomous/triage", gotMethod, gotPath)
	}
	for _, want := range []string{
		`Automaton "build the queue" (id `,
		"Failed task: run the build (id t-fail)",
		"Task error: the build session died before writing the plan",
		"Session end reason: operator ended it",
		"Retry budget:",
		"ceiling 7 left of 7 total",
		"alert threshold: 30 min",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("CLI output lacks %q\ngot:\n%s", want, got)
		}
	}

	// A filter the operator types must reach the daemon as the matching
	// query and narrow the list to what the daemon returns.
	got, err = out("--status", "blocked")
	if err != nil {
		t.Fatalf("--status blocked: %v (out: %s)", err, got)
	}
	if got, q := gotQuery.Get("status"), gotQuery; got != "blocked" || len(q) != 1 {
		t.Errorf("query = %v, want status=blocked and nothing else", q)
	}
	if !strings.Contains(got, "No failed or blocked Automata to triage") {
		t.Errorf("blocked filter should show the empty list; got:\n%s", got)
	}
	if !strings.Contains(got, "alert threshold: 30 min") {
		t.Errorf("threshold line missing in the empty list; got:\n%s", got)
	}

	// A status the daemon would refuse must fail before any request
	gotPath = ""
	_, err = out("--status", "crashed")
	if err == nil {
		t.Error("--status crashed should be refused")
	}
	if gotPath != "" {
		t.Errorf("a refused status still hit the daemon at %q", gotPath)
	}

	// --json prints the raw daemon body, unchanged.
	raw, err := out("--json")
	if err != nil {
		t.Fatalf("--json: %v", err)
	}
	var body struct {
		Records      json.RawMessage `json:"records"`
		AlertMinutes *int            `json:"alert_minutes"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("--json body does not parse: %v (%s)", err, raw)
	}
	if body.AlertMinutes == nil || *body.AlertMinutes != 30 {
		t.Errorf("--json alert_minutes = %v, want 30", body.AlertMinutes)
	}
}
