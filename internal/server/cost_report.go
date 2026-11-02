// Cost report REST — exposes the cost report engine
// (internal/costreport) over the server API.
//
//   GET /api/cost/report                        report over the configured
//                                               default window/grouping
//   GET /api/cost/report?group_by=<g>           one of automaton,
//                                               automation, backend, week
//   GET /api/cost/report?since=<RFC3339|YYYY-MM-DD>&until=<...>
//
// The response is the engine's costreport.Report, including the
// retention_days and lifetime_fallback fields that explain which spend
// horizon the dollar figures rest on.

package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// managerCostSource serves costreport.Source from the daemon's recorded
// sessions: one Record per session that has usage, stamped with the
// session's last update time. The engine does the windowing, grouping
// and pricing; this adapter only hands it rows.
type managerCostSource struct {
	m *session.Manager
}

func (src managerCostSource) Records(opts costreport.SourceOptions) (*costreport.SourceResult, error) {
	res := &costreport.SourceResult{}
	if src.m == nil {
		return res, nil
	}
	for _, sess := range src.m.ListSessions() {
		if sess == nil || (sess.TokensIn == 0 && sess.TokensOut == 0) {
			continue
		}
		res.Records = append(res.Records, costreport.Record{
			SessionID: sess.ID,
			Backend:   sess.BackendFamily,
			At:        sess.UpdatedAt,
			TokensIn:  sess.TokensIn,
			TokensOut: sess.TokensOut,
		})
	}
	return res, nil
}

// parseCostReportBound accepts an RFC3339 timestamp or a bare
// YYYY-MM-DD date (interpreted with zero time in the local zone, the
// same reading time.Parse gives a layout without a zone).
func parseCostReportBound(v string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// handleCostReport exposes the cost report engine (internal/costreport)
// built over the cost_report config block (s.cfg.CostReport) with rows
// from the recorded sessions.
func (s *Server) handleCostReport(w http.ResponseWriter, r *http.Request) {
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
	groupBy := costreport.GroupBy(q.Get("group_by"))
	if groupBy != "" && !groupBy.Valid() {
		http.Error(w, `group_by must be one of automaton, automation, backend, week`, http.StatusBadRequest)
		return
	}
	since := time.Time{}
	if v := q.Get("since"); v != "" {
		t, ok := parseCostReportBound(v)
		if !ok {
			http.Error(w, "since must be RFC3339 or YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		since = t
	}
	until := time.Time{}
	if v := q.Get("until"); v != "" {
		t, ok := parseCostReportBound(v)
		if !ok {
			http.Error(w, "until must be RFC3339 or YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		until = t
	}

	cr := config.CostReportConfig{}
	if s.cfg != nil {
		cr = s.cfg.CostReport
	}
	rep, err := costreport.Build(
		cr,
		costreport.Request{GroupBy: groupBy, Since: since, Until: until},
		managerCostSource{m: s.manager},
		s.exposedRate,
		time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}
