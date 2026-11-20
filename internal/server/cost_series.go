// The daemon's cost-series REST surface: one route, GET /api/cost/series,
// the cost report's time dimension. It buckets the spend ledger into an
// hour or day series per Automaton, project or model, with tokens as the
// value on every point and the money per billing unit (dollars, points,
// none) carried beside it, never summed across units.
//
// Query params, all optional: group_by (automaton, project or model;
// default automaton), window (how far back from now: a duration such as
// 36h, or 7d / 2w; default the cost_report default window) and bucket
// (hour or day; default hour for windows up to 48 hours, day beyond).
// Groups with no spend in the window are omitted.

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// projectTagPrefix marks the placement-derived project tag an Automaton
// carries in its system tags ("project:<name>").
const projectTagPrefix = "project:"

// maxProjectParentDepth bounds how far a parent chain is followed.
const maxProjectParentDepth = 8

// projectTagName returns the project named by the Automaton's project tag,
// or "". A tag whose value equals the Automaton's own id is no link.
func projectTagName(p *autonomous.PRD) string {
	for _, tag := range p.SystemTags {
		if !strings.HasPrefix(tag, projectTagPrefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(tag, projectTagPrefix))
		if name != "" && name != p.ID {
			return name
		}
	}
	return ""
}

// projectDirName returns the base name of the Automaton's project
// directory, or "" (never the Automaton's own id).
func projectDirName(p *autonomous.PRD) string {
	dir := strings.TrimSpace(p.ProjectDir)
	if dir == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(dir))
	if base == "." || base == string(filepath.Separator) || base == p.ID {
		return ""
	}
	return base
}

// automatonProject names the project an Automaton belongs to: its project
// tag, else its parent Automaton's project (followed through byID, bounded
// and cycle-safe), else the base name of its project directory, else "".
func automatonProject(p *autonomous.PRD, byID map[string]*autonomous.PRD) string {
	return automatonProjectWalk(p, byID, map[string]bool{}, 0)
}

func automatonProjectWalk(p *autonomous.PRD, byID map[string]*autonomous.PRD, seen map[string]bool, depth int) string {
	if p == nil {
		return ""
	}
	if name := projectTagName(p); name != "" {
		return name
	}
	seen[p.ID] = true
	if parentID := strings.TrimSpace(p.ParentPRDID); parentID != "" && depth < maxProjectParentDepth && !seen[parentID] {
		if name := automatonProjectWalk(byID[parentID], byID, seen, depth+1); name != "" && name != p.ID {
			return name
		}
	}
	return projectDirName(p)
}

// prdIndex maps Automaton id to Automaton.
func prdIndex(prds []*autonomous.PRD) map[string]*autonomous.PRD {
	byID := make(map[string]*autonomous.PRD, len(prds))
	for _, p := range prds {
		if p != nil {
			byID[p.ID] = p
		}
	}
	return byID
}

// costSeriesProjectOf builds the Automaton-to-project lookup the series
// builder groups by project with. An Automaton that resolves to no project
// resolves to "" and lands under "unassigned".
func costSeriesProjectOf(prds []*autonomous.PRD) func(string) string {
	byID := prdIndex(prds)
	return func(id string) string { return automatonProject(byID[id], byID) }
}

// handleCostSeries — GET /api/cost/series. Validate group_by, window and
// bucket, read the spend ledger, and return the costreport.Series as JSON.
func (s *Server) handleCostSeries(w http.ResponseWriter, r *http.Request) {
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
	if v := strings.TrimSpace(q.Get("window")); v != "" {
		d, err := costreport.ParseSeriesWindow(v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Window = d
	}
	if v := strings.TrimSpace(q.Get("bucket")); v != "" {
		b := costreport.SeriesBucket(strings.ToLower(v))
		if b == costreport.SeriesBucketAuto || !b.Valid() {
			http.Error(w, `bucket must be hour or day`, http.StatusBadRequest)
			return
		}
		req.Bucket = b
	}

	opts := costreport.SeriesOptions{Now: time.Now(), RateFor: s.exposedRate}
	if s.cfg != nil {
		opts.Config = s.cfg.CostReport
		opts.Billing = s.resolvedBilling()
	}
	if mgr := s.costReportAutonomousManager(); mgr != nil {
		opts.ProjectOf = costSeriesProjectOf(mgr.Store().ListPRDs())
	}
	var entries []spendledger.Entry
	if s.costReportLedger != nil {
		entries = s.costReportLedger.Entries()
	}

	series, err := costreport.BuildSeries(req, entries, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(series)
}
