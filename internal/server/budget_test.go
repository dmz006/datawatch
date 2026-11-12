package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/inference"
)

// budgetTestServer returns a server whose ledger reads budgets from its LLM
// registry, with "claude" holding a 5-hour rolling $10 budget and "plain"
// holding none. The ledger clock is fixed.
func budgetTestServer(t *testing.T, now time.Time) (*Server, *autonomous.BudgetLedger) {
	t.Helper()
	s := newLLMServer(t)
	for _, name := range []string{"claude", "plain"} {
		l := &inference.LLM{Name: name, Kind: "ollama"}
		if name == "claude" {
			l.Budget = &inference.BudgetWindow{CeilingUSD: 10, Hours: 5}
		}
		if err := s.inferenceReg.Add(l); err != nil {
			t.Fatal(err)
		}
	}
	bl := autonomous.NewBudgetLedger(filepath.Join(t.TempDir(), "budget-ledger.jsonl"))
	bl.Clock = func() time.Time { return now }
	bl.Budgets = func(llm string) *inference.BudgetWindow {
		if l, err := s.inferenceReg.Get(llm); err == nil {
			return l.Budget
		}
		return nil
	}
	bl.LLMs = func() []string {
		var names []string
		for _, l := range s.inferenceReg.List() {
			names = append(names, l.Name)
		}
		return names
	}
	s.SetBudgetLedger(bl)
	return s, bl
}

func budgetGet(s *Server, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if strings.HasPrefix(req.URL.Path, "/api/budget/usage") {
		s.handleBudgetUsage(rec, req)
	} else {
		s.handleBudget(rec, req)
	}
	return rec
}

func TestBudgetEndpoint(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	s, bl := budgetTestServer(t, now)
	bl.PeerSpend = func(string) float64 { return 7 }
	bl.Record("claude", now.Add(-time.Hour), 3, 10, 10)
	bl.Record("claude", now.Add(-4*time.Hour), 2, 10, 10)
	bl.Record("claude", now.Add(-10*time.Hour), 50, 10, 10) // outside the window

	// List: only LLMs with a budget.
	rec := budgetGet(s, "/api/budget")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Budgets []autonomous.BudgetStatus `json:"budgets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Budgets) != 1 || list.Budgets[0].LLM != "claude" || list.Budgets[0].SpendUSD != 5 || list.Budgets[0].PeerSpendUSD != 7 {
		t.Fatalf("list = %+v", list.Budgets)
	}

	// Single.
	rec = budgetGet(s, "/api/budget?llm=claude")
	var one autonomous.BudgetStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("single: %d %v %s", rec.Code, err, rec.Body.String())
	}
	if one.LLM != "claude" || one.CeilingUSD != 10 || !one.Full {
		t.Fatalf("single = %+v", one)
	}

	// 404 for an LLM without a budget, and for an unknown one.
	for _, name := range []string{"plain", "missing"} {
		if rec = budgetGet(s, "/api/budget?llm="+name); rec.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", name, rec.Code)
		}
	}

	// Method check.
	rec = httptest.NewRecorder()
	s.handleBudget(rec, httptest.NewRequest(http.MethodPost, "/api/budget", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: want 405, got %d", rec.Code)
	}

	// Usage without from/to: this daemon's own window, local spend only.
	rec = budgetGet(s, "/api/budget/usage?llm=claude")
	var u budgetUsageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("usage: %d %v %s", rec.Code, err, rec.Body.String())
	}
	if u.LLM != "claude" || u.SpendUSD != 5 || !u.WindowStart.Equal(now.Add(-5*time.Hour)) || !u.WindowEnd.Equal(now) {
		t.Fatalf("usage = %+v", u)
	}
	var generic map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &generic)
	if len(generic) != 4 {
		t.Errorf("usage body must carry only llm, window_start, window_end, spend_usd: %v", generic)
	}

	// Usage with from/to: the caller's window.
	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Format(time.RFC3339)
	rec = budgetGet(s, "/api/budget/usage?llm=claude&from="+from+"&to="+to)
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("usage from/to: %d %v %s", rec.Code, err, rec.Body.String())
	}
	if u.SpendUSD != 3 || !u.WindowStart.Equal(now.Add(-2*time.Hour)) || !u.WindowEnd.Equal(now) {
		t.Fatalf("usage from/to = %+v", u)
	}

	// A wide window reaches the old entry; peer spend never appears.
	from = now.Add(-24 * time.Hour).Format(time.RFC3339)
	rec = budgetGet(s, "/api/budget/usage?llm=claude&from="+from+"&to="+to)
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if u.SpendUSD != 55 {
		t.Fatalf("wide window spend = %v, want 55 (local only)", u.SpendUSD)
	}

	// A budgetless LLM answers when the caller names both bounds.
	bl.Record("plain", now.Add(-time.Hour), 1.5, 1, 1)
	rec = budgetGet(s, "/api/budget/usage?llm=plain&from="+from+"&to="+to)
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if rec.Code != http.StatusOK || u.SpendUSD != 1.5 {
		t.Fatalf("plain with bounds: %d %+v", rec.Code, u)
	}

	// Errors.
	for target, want := range map[string]int{
		"/api/budget/usage":                                       http.StatusBadRequest,
		"/api/budget/usage?llm=plain":                             http.StatusNotFound,
		"/api/budget/usage?llm=claude&from=yesterday":             http.StatusBadRequest,
		"/api/budget/usage?llm=claude&to=tomorrow":                http.StatusBadRequest,
		"/api/budget/usage?llm=claude&from=" + to + "&to=" + from: http.StatusBadRequest,
	} {
		if rec = budgetGet(s, target); rec.Code != want {
			t.Errorf("%s: want %d, got %d (%s)", target, want, rec.Code, rec.Body.String())
		}
	}

	// Unwired ledger.
	if rec = budgetGet(&Server{}, "/api/budget"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired: want 503, got %d", rec.Code)
	}
}

func llmRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.URL.Path = path
	rec := httptest.NewRecorder()
	s.handleLLMs(rec, req)
	return rec
}

func TestBudgetRoundTrip(t *testing.T) {
	s := newLLMServer(t)
	if rec := llmRequest(s, http.MethodPost, "/api/llms", `{"name":"claude","kind":"ollama","budget":{"ceiling_usd":25,"hours":24,"allocation_pct":true,"peer_window":["laptop"]}}`); rec.Code != http.StatusOK {
		t.Fatalf("create with budget: %d %s", rec.Code, rec.Body.String())
	}

	getBudget := func() *inference.BudgetWindow {
		t.Helper()
		rec := llmRequest(s, http.MethodGet, "/api/llms/claude", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
		}
		var v struct {
			Budget *inference.BudgetWindow `json:"budget"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.Budget
	}

	b := getBudget()
	if b == nil || b.CeilingUSD != 25 || b.Hours != 24 || !b.AllocationPct || len(b.PeerWindow) != 1 || b.PeerWindow[0] != "laptop" {
		t.Fatalf("created budget = %+v", b)
	}

	// The list carries the budget too.
	rec := llmRequest(s, http.MethodGet, "/api/llms", "")
	if !strings.Contains(rec.Body.String(), `"ceiling_usd":25`) {
		t.Fatalf("list lacks budget: %s", rec.Body.String())
	}

	// PUT replaces the budget as a whole: weekly replaces rolling without
	// tripping the hours/weekday exclusion.
	if rec = llmRequest(s, http.MethodPut, "/api/llms/claude", `{"budget":{"ceiling_usd":40,"weekday":"monday","time":"06:00","zone":"UTC"}}`); rec.Code != http.StatusOK {
		t.Fatalf("put weekly: %d %s", rec.Code, rec.Body.String())
	}
	b = getBudget()
	if b == nil || b.CeilingUSD != 40 || b.Hours != 0 || b.Weekday != "monday" || b.Time != "06:00" || b.AllocationPct || len(b.PeerWindow) != 0 {
		t.Fatalf("weekly budget = %+v", b)
	}

	// An unrelated edit leaves the budget alone.
	if rec = llmRequest(s, http.MethodPut, "/api/llms/claude", `{"max_inflight":2}`); rec.Code != http.StatusOK {
		t.Fatalf("put unrelated: %d %s", rec.Code, rec.Body.String())
	}
	if b = getBudget(); b == nil || b.CeilingUSD != 40 {
		t.Fatalf("budget lost on unrelated edit: %+v", b)
	}

	// Invalid budgets are rejected with 400 and change nothing.
	for name, body := range map[string]string{
		"zero ceiling":    `{"budget":{"ceiling_usd":0,"hours":5}}`,
		"both styles":     `{"budget":{"ceiling_usd":5,"hours":5,"weekday":"monday"}}`,
		"no style":        `{"budget":{"ceiling_usd":5}}`,
		"hours too large": `{"budget":{"ceiling_usd":5,"hours":9999}}`,
		"bad weekday":     `{"budget":{"ceiling_usd":5,"weekday":"someday"}}`,
		"bad zone":        `{"budget":{"ceiling_usd":5,"hours":5,"zone":"Mars/Base"}}`,
		"unknown field":   `{"budget":{"ceiling_usd":5,"hours":5,"bogus":1}}`,
		"wrong type":      `{"budget":"lots"}`,
	} {
		if rec = llmRequest(s, http.MethodPut, "/api/llms/claude", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d (%s)", name, rec.Code, rec.Body.String())
		}
	}
	if rec = llmRequest(s, http.MethodPost, "/api/llms", `{"name":"bad","kind":"ollama","budget":{"ceiling_usd":-1,"hours":5}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("create invalid: want 400, got %d", rec.Code)
	}
	if b = getBudget(); b == nil || b.CeilingUSD != 40 || b.Weekday != "monday" {
		t.Fatalf("budget changed by rejected request: %+v", b)
	}

	// An explicit null clears it; so does an empty object.
	if rec = llmRequest(s, http.MethodPut, "/api/llms/claude", `{"budget":null}`); rec.Code != http.StatusOK {
		t.Fatalf("clear null: %d %s", rec.Code, rec.Body.String())
	}
	if b = getBudget(); b != nil {
		t.Fatalf("budget after null = %+v", b)
	}
	if rec = llmRequest(s, http.MethodPut, "/api/llms/claude", `{"budget":{"ceiling_usd":5,"hours":2}}`); rec.Code != http.StatusOK {
		t.Fatalf("set again: %d %s", rec.Code, rec.Body.String())
	}
	if rec = llmRequest(s, http.MethodPut, "/api/llms/claude", `{"budget":{}}`); rec.Code != http.StatusOK {
		t.Fatalf("clear empty: %d %s", rec.Code, rec.Body.String())
	}
	if b = getBudget(); b != nil {
		t.Fatalf("budget after empty object = %+v", b)
	}
}

func TestBudgetCapacitySection(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	// No ledger: the section is an empty array, not null.
	rec := httptest.NewRecorder()
	(&Server{}).handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if !strings.Contains(rec.Body.String(), `"budgets":[]`) {
		t.Fatalf("unwired capacity must carry an empty budgets array: %s", rec.Body.String())
	}

	s, bl := budgetTestServer(t, now)
	bl.Record("claude", now.Add(-time.Hour), 4, 1, 1)
	rec = httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("capacity: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Budgets []autonomous.BudgetStatus `json:"budgets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Budgets) != 1 || body.Budgets[0].LLM != "claude" || body.Budgets[0].SpendUSD != 4 || body.Budgets[0].RemainingUSD != 6 {
		t.Fatalf("capacity budgets = %+v", body.Budgets)
	}
}
