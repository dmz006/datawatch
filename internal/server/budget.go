// Per-LLM dollar budget REST surface.
//
//	GET /api/budget              {"budgets": [...]} for every LLM with a budget
//	GET /api/budget?llm=<name>   one LLM's budget state (404 without a budget)
//	GET /api/budget/usage        this daemon's own spend for ?llm=<name>
//
// The usage endpoint reports only spend recorded by this daemon, never any
// peer spend, so a peer counting this daemon toward its own ceiling cannot
// count the same dollars twice. It takes optional RFC3339 from and to
// parameters so a peer can ask for the spend inside the caller's window; the
// bounds default to this daemon's own window.

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
)

// SetBudgetLedger wires the per-LLM dollar budget ledger exposed at
// /api/budget and in the capacity response.
func (s *Server) SetBudgetLedger(b *autonomous.BudgetLedger) { s.budgetLedger = b }

// SetBudgetLedger wires the budget ledger on the HTTP server.
func (s *HTTPServer) SetBudgetLedger(b *autonomous.BudgetLedger) {
	if s.api != nil {
		s.api.SetBudgetLedger(b)
	}
}

// budgetStatuses snapshots every budgeted LLM's state; empty, never nil,
// when no ledger is wired or no LLM has a budget.
func (s *Server) budgetStatuses() []autonomous.BudgetStatus {
	if s.budgetLedger == nil {
		return []autonomous.BudgetStatus{}
	}
	if out := s.budgetLedger.StatusAll(); out != nil {
		return out
	}
	return []autonomous.BudgetStatus{}
}

// budgetUsageResponse is the GET /api/budget/usage body.
type budgetUsageResponse struct {
	LLM         string    `json:"llm"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	SpendUSD    float64   `json:"spend_usd"`
}

func (s *Server) handleBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.budgetLedger == nil {
		http.Error(w, "budget ledger not available", http.StatusServiceUnavailable)
		return
	}
	if name := strings.TrimSpace(r.URL.Query().Get("llm")); name != "" {
		st, ok := s.budgetLedger.Status(name)
		if !ok {
			http.Error(w, "no budget for llm "+name, http.StatusNotFound)
			return
		}
		writeJSONOK(w, st)
		return
	}
	writeJSONOK(w, map[string]any{"budgets": s.budgetStatuses()})
}

func (s *Server) handleBudgetUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.budgetLedger == nil {
		http.Error(w, "budget ledger not available", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	name := strings.TrimSpace(q.Get("llm"))
	if name == "" {
		http.Error(w, "llm: required", http.StatusBadRequest)
		return
	}
	from, err := parseBudgetTime(q.Get("from"))
	if err != nil {
		http.Error(w, "from: must be an RFC3339 time", http.StatusBadRequest)
		return
	}
	to, err := parseBudgetTime(q.Get("to"))
	if err != nil {
		http.Error(w, "to: must be an RFC3339 time", http.StatusBadRequest)
		return
	}
	resp := budgetUsageResponse{LLM: name}
	if from == nil || to == nil {
		// A missing bound defaults to this daemon's own window, which only
		// exists for an LLM with a budget.
		st, ok := s.budgetLedger.Status(name)
		if !ok {
			http.Error(w, "no budget for llm "+name+"; pass both from and to", http.StatusNotFound)
			return
		}
		resp.WindowStart, resp.WindowEnd, resp.SpendUSD = st.WindowStart, st.WindowEnd, st.SpendUSD
		if from == nil && to == nil {
			writeJSONOK(w, resp)
			return
		}
	}
	if from != nil {
		resp.WindowStart = *from
	}
	if to != nil {
		resp.WindowEnd = *to
	}
	if resp.WindowEnd.Before(resp.WindowStart) {
		http.Error(w, "to: must not be before from", http.StatusBadRequest)
		return
	}
	resp.SpendUSD = s.budgetLedger.SpendBetween(name, resp.WindowStart, resp.WindowEnd)
	writeJSONOK(w, resp)
}

// parseBudgetTime parses an optional RFC3339 query value; nil when empty.
func parseBudgetTime(v string) (*time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// applyBudgetField applies the "budget" member of an LLM request body to l.
// An absent member keeps prior; null or an empty object clears the budget;
// any other object replaces it as a whole and must pass
// BudgetWindow.Validate.
func applyBudgetField(body []byte, l *inference.LLM, prior *inference.BudgetWindow) error {
	var probe struct {
		Budget json.RawMessage `json:"budget"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return err
	}
	raw := bytes.TrimSpace(probe.Budget)
	if len(raw) == 0 {
		l.Budget = prior
		return nil
	}
	if bytes.Equal(raw, []byte("null")) {
		l.Budget = nil
		return nil
	}
	var bw inference.BudgetWindow
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&bw); err != nil {
		return errors.New("budget: " + err.Error())
	}
	if bw.CeilingUSD == 0 && bw.Hours == 0 && strings.TrimSpace(bw.Weekday) == "" &&
		strings.TrimSpace(bw.Time) == "" && bw.Zone == "" && !bw.AllocationPct && len(bw.PeerWindow) == 0 {
		l.Budget = nil
		return nil
	}
	if err := bw.Validate(); err != nil {
		return err
	}
	l.Budget = &bw
	return nil
}
