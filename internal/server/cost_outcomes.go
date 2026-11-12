// The daemon's cost-outcome REST surface: GET /api/cost/tool-usage and
// GET /api/cost/failures, the cost report's two outcome views.
//
// Tool usage buckets the spend ledger's per-session tool-call counts into an
// hour or day series per Automaton, project or model. The ledger keeps a
// count and never a tool name, so the payload carries a note saying there is
// no per-tool-name breakdown. Query params, all optional: group_by
// (automaton, project or model; default automaton), window (a duration such
// as 36h, or 7d / 2w; default the cost_report default window) and bucket
// (hour or day).
//
// Failures counts failed, stalled and cancelled task attempts per failure
// category and per week, grouped by project, Automation or Automaton, and
// sums the tasks' automatic retry counts. Query params, all optional:
// group_by (project, automation or automaton; default project) and window
// (default twelve weeks).

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// parseCostWindow reads the optional window param. ok is false after a 400
// has been written.
func parseCostWindow(w http.ResponseWriter, v string) (d time.Duration, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, true
	}
	d, err := costreport.ParseSeriesWindow(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return 0, false
	}
	return d, true
}

// handleCostToolUsage — GET /api/cost/tool-usage. Validate group_by, window
// and bucket, read the spend ledger, and return the costreport.ToolUsage as
// JSON.
func (s *Server) handleCostToolUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()

	req := costreport.SeriesRequest{}
	if v := strings.TrimSpace(q.Get("group_by")); v != "" {
		gb := costreport.SeriesGroupBy(strings.ToLower(v))
		if !gb.Valid() {
			http.Error(w, `group_by must be one of automaton, project, model`, http.StatusBadRequest)
			return
		}
		req.GroupBy = gb
	}
	d, ok := parseCostWindow(w, q.Get("window"))
	if !ok {
		return
	}
	req.Window = d
	if v := strings.TrimSpace(q.Get("bucket")); v != "" {
		b := costreport.SeriesBucket(strings.ToLower(v))
		if b == costreport.SeriesBucketAuto || !b.Valid() {
			http.Error(w, `bucket must be hour or day`, http.StatusBadRequest)
			return
		}
		req.Bucket = b
	}

	opts := costreport.SeriesOptions{Now: time.Now()}
	if s.cfg != nil {
		opts.Config = s.cfg.CostReport
	}
	if mgr := s.costReportAutonomousManager(); mgr != nil {
		opts.ProjectOf = costSeriesProjectOf(mgr.Store().ListPRDs())
	}
	var entries []spendledger.Entry
	if s.costReportLedger != nil {
		entries = s.costReportLedger.Entries()
	}

	usage, err := costreport.BuildToolUsage(req, entries, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(usage)
}

// handleCostFailures — GET /api/cost/failures. Validate group_by and window,
// flatten every Automaton's task attempts, and return the costreport.Failures
// as JSON.
func (s *Server) handleCostFailures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
	mgr := s.costReportAutonomousManager()
	if mgr == nil {
		http.Error(w, "autonomous manager not available", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()

	req := costreport.FailuresRequest{}
	if v := strings.TrimSpace(q.Get("group_by")); v != "" {
		gb := costreport.FailuresGroupBy(strings.ToLower(v))
		if !gb.Valid() {
			http.Error(w, `group_by must be one of project, automation, automaton`, http.StatusBadRequest)
			return
		}
		req.GroupBy = gb
	}
	d, ok := parseCostWindow(w, q.Get("window"))
	if !ok {
		return
	}
	req.Window = d

	failures, err := costreport.BuildFailures(req, costFailureRecords(mgr), time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(failures)
}

// costFailureRecords flattens every Automaton's task attempts into the
// records the failures builder counts. An attempt is stamped with its end
// time, or its start time when it never recorded an end. An Automaton's
// Automation is its AutomationID, else the Automation that lists it as a
// member.
func costFailureRecords(mgr *autonomous.Manager) []costreport.FailureRecord {
	if mgr == nil {
		return nil
	}
	automationOf := map[string]*autonomous.Automation{}
	for _, a := range mgr.ListAutomations() {
		if a == nil {
			continue
		}
		for _, mem := range a.Members {
			if mem.PRDID != "" {
				if _, ok := automationOf[mem.PRDID]; !ok {
					automationOf[mem.PRDID] = a
				}
			}
		}
	}

	var out []costreport.FailureRecord
	for _, p := range mgr.Store().ListPRDs() {
		if p == nil {
			continue
		}
		var auto *autonomous.Automation
		if p.AutomationID != "" {
			if got, ok := mgr.GetAutomation(p.AutomationID); ok {
				auto = got
			}
		}
		if auto == nil {
			auto = automationOf[p.ID]
		}
		automationID, automationName := p.AutomationID, ""
		if auto != nil {
			automationID, automationName = auto.ID, auto.Name
		}
		project := automatonProject(p)
		for _, st := range p.Story {
			for _, t := range st.Tasks {
				for _, at := range t.Attempts {
					when := at.EndedAt
					if when.IsZero() {
						when = at.StartedAt
					}
					out = append(out, costreport.FailureRecord{
						AutomatonID: p.ID, AutomatonTitle: p.Title,
						AutomationID: automationID, AutomationName: automationName,
						Project: project, TaskID: t.ID,
						At: when, Outcome: at.Outcome,
						Category: at.FailureCategory, Summary: at.FailureSummary,
						RetryCount: t.RetryCount,
					})
				}
			}
		}
	}
	return out
}
