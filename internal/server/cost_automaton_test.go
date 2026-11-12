package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/spendledger"
)

func automatonCostLedger(now time.Time) fakeSpendLedger {
	return fakeSpendLedger{
		{SessionID: "s1", AutomatonID: "prd-a", Role: "worker", Backend: "claude-code",
			StartedAt: now.Add(-time.Hour), EndedAt: now.Add(-50 * time.Minute),
			TokensIn: 1000, TokensOut: 400, ToolCalls: 7, ActiveSeconds: 120},
		{SessionID: "s2", AutomatonID: "prd-a", Role: "verifier", Backend: "claude-code",
			StartedAt: now.Add(-40 * time.Minute), EndedAt: now.Add(-30 * time.Minute),
			TokensIn: 500, TokensOut: 100, ToolCalls: 3, ActiveSeconds: 60},
		{SessionID: "s3", AutomatonID: "prd-b", Role: "worker", Backend: "ollama",
			StartedAt: now.Add(-time.Hour), EndedAt: now, TokensIn: 9, TokensOut: 9},
		{SessionID: "s4", Role: "worker", Backend: "ollama", StartedAt: now, TokensIn: 1},
	}
}

func TestAutomatonCost_ForAutomaton(t *testing.T) {
	s := bl90Server(t)
	s.costReportLedger = automatonCostLedger(time.Now())
	c := s.automatonCostFor("prd-a")
	if c == nil {
		t.Fatal("nil cost")
	}
	if c.Key != "prd-a" || c.Sessions != 2 || c.TokensIn != 1500 || c.TokensOut != 500 || c.ToolCalls != 10 {
		t.Fatalf("unexpected cost: %+v", c.Row)
	}
	if c.ActiveSeconds != 180 {
		t.Errorf("active_seconds = %v, want 180", c.ActiveSeconds)
	}
}

func TestAutomatonCost_NoSpendIsZeroRow(t *testing.T) {
	s := bl90Server(t)
	s.costReportLedger = automatonCostLedger(time.Now())
	c := s.automatonCostFor("prd-none")
	if c == nil || c.Key != "prd-none" || c.Sessions != 0 || c.TokensIn != 0 {
		t.Fatalf("want zero row, got %+v", c)
	}
}

func TestAutomatonCost_NoLedgerOrID(t *testing.T) {
	s := bl90Server(t)
	if s.automatonCostFor("prd-a") != nil {
		t.Error("cost without a ledger")
	}
	s.costReportLedger = automatonCostLedger(time.Now())
	if s.automatonCostFor("") != nil {
		t.Error("cost for empty id")
	}
	var nilSrv *Server
	if nilSrv.automatonCostFor("x") != nil {
		t.Error("cost on nil server")
	}
}

func TestAutomatonCost_WithAutomatonCost(t *testing.T) {
	s := bl90Server(t)
	in := map[string]any{"id": "prd-a", "title": "t"}
	if got := s.withAutomatonCost(in, "prd-a"); got == nil {
		t.Fatal("nil")
	} else if m, ok := got.(map[string]any); ok && m["cost"] != nil {
		t.Error("cost added without a ledger")
	}
	s.costReportLedger = automatonCostLedger(time.Now())
	raw, err := json.Marshal(s.withAutomatonCost(in, "prd-a"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	cost, ok := m["cost"].(map[string]any)
	if !ok {
		t.Fatalf("cost missing: %v", m)
	}
	if cost["sessions"].(float64) != 2 || cost["tokens_in"].(float64) != 1500 {
		t.Errorf("cost = %v", cost)
	}
	if m["title"] != "t" {
		t.Error("payload fields lost")
	}
	// an unencodable payload is returned unchanged
	ch := make(chan int)
	if got := s.withAutomatonCost(ch, "prd-a"); got != any(ch) {
		t.Error("unencodable payload changed")
	}
	// a non-object payload is returned unchanged
	if got := s.withAutomatonCost([]int{1}, "prd-a"); got == nil {
		t.Error("nil for array payload")
	}
}

// TestAutomatonCost_ReportCarriesColumnsAndChecks — GET /api/cost/report
// carries the new row columns, the window notes and the attribution check.
func TestAutomatonCost_ReportCarriesColumnsAndChecks(t *testing.T) {
	s := bl90Server(t)
	now := time.Now()
	led := automatonCostLedger(now)
	led = append(led, spendledger.Entry{SessionID: "s5", Role: "worker", TaskID: "t1", Backend: "ollama", StartedAt: now})
	s.costReportLedger = led
	rr := httptest.NewRecorder()
	s.handleCostReport(rr, httptest.NewRequest(http.MethodGet, "/api/cost/report?group_by=automaton", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var rep struct {
		Rows []struct {
			Key           string  `json:"key"`
			ToolCalls     int     `json:"tool_calls"`
			ActiveSeconds float64 `json:"active_seconds"`
			SpanSeconds   float64 `json:"span_seconds"`
		} `json:"rows"`
		Attribution struct {
			AutomatonLaunched int  `json:"automaton_launched"`
			Unattributed      int  `json:"unattributed"`
			OK                bool `json:"ok"`
		} `json:"attribution"`
		RetentionNote string `json:"retention_note"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rep.Rows {
		if r.Key == "prd-a" {
			found = true
			if r.ToolCalls != 10 || r.ActiveSeconds != 180 || r.SpanSeconds <= 0 {
				t.Errorf("prd-a columns wrong: %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("prd-a row missing: %s", rr.Body.String())
	}
	if rep.Attribution.Unattributed < 1 || rep.Attribution.OK || rep.Attribution.AutomatonLaunched < 3 {
		t.Errorf("attribution = %+v", rep.Attribution)
	}
	if rep.RetentionNote == "" {
		t.Error("retention_note empty")
	}
}
