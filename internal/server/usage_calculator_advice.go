// usage_calculator_advice.go — the REST surface and daemon wiring of the usage calculator's
// advice service (docs/plans/2026-10-23-usage-calculator-design.md; decisions,
// docs/plans/2026-10-25-usage-calculator-decisions.md).
//
// The service (usagecalc.Service) composes the shared readings series, this daemon's local
// store, the operator's live settings and the daemon's queued-task count into one advice:
//
//	GET    /api/usage/advice            usage:read   one AdviceResult; the variant=,
//	                                                policy=, lanes= and cost= query
//	                                                parameters override the stored
//	                                                settings for this one call
//	PUT    /api/usage/remaining-work    usage:write  set the operator's typed remaining
//	                                                work: {"points": 15.5}
//	DELETE /api/usage/remaining-work    usage:write  remove the typed figure
//
// The settings getter re-reads the live config on every advice call, so a config patch at
// runtime takes effect without a restart.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// usageWeekday maps the config's lowercase weekday names to the calendar weekday the
// calculator's reset spec takes.
var usageWeekday = map[string]time.Weekday{
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
	"sunday":    time.Sunday,
}

// UsageCalculatorSettings maps one effective calculator config into the service's live
// settings. All of the config's numeric fields already carry defaults after
// EffectiveUsageCalculator. The reset zone is the effective one from
// EffectiveUsageResetZone: the operator's set zone, or the daemon's local zone when the
// operator left it unset, or UTC as a last resort. The serverZone and fellBack
// markers ride along so the advice can show the "(server zone)" marker and note
// the UTC fallback.
// UsageCalculatorSettings maps one calculator config into the service's live
// settings. It takes the config as stored (defaults and the effective reset
// zone are applied here, not by the caller): an unset zone follows
// EffectiveUsageResetZone, so the advice sees the effective zone with the
// serverZone and fellBack markers telling the UI and the text renderings to
// show the "(server zone)" mark and name the UTC fallback.
func UsageCalculatorSettings(c config.UsageCalculatorConfig) usagecalc.Settings {
	e := config.EffectiveUsageCalculator(c)
	zone, serverZone, fellBack := config.EffectiveUsageResetZone(c)
	safety := e.SafetyPoints
	baseline := e.BaselinePer24h
	return usagecalc.Settings{
		Variant:        e.Variant,
		Policy:         e.Policy,
		CostPerTask:    e.CostPerTaskPoints,
		MaxLanes:       e.MaxLanes,
		SafetyPoints:   &safety,
		BaselinePer24h: &baseline,
		Reset: usagecalc.ResetSpec{
			Weekday:    usageWeekday[strings.ToLower(strings.TrimSpace(e.ResetWeekday))],
			Clock:      strings.TrimSpace(e.ResetTime),
			Zone:       zone,
			ServerZone: serverZone,
			FellBack:   fellBack,
		},
	}
}

// SetUsageCalculator wires the advice service the advice and remaining-work endpoints
// serve; a nil argument unwires it. The pointer write is the only mutation, and the
// service is safe for concurrent use, so it may be called while handlers are running.
func (s *Server) SetUsageCalculator(c *usagecalc.Service) {
	s.usageCalc = c
}

// UsageCalculator returns the wired advice service, or nil until SetUsageCalculator has
// run.
func (s *Server) UsageCalculator() *usagecalc.Service {
	return s.usageCalc
}

// SetUsageCalculator wires the advice service on the embedded API server.
func (s *HTTPServer) SetUsageCalculator(c *usagecalc.Service) {
	if s.api != nil {
		s.api.SetUsageCalculator(c)
	}
}

// AttachUsageCalculator composes the advice service on the embedded API server (see
// (*Server) AttachUsageCalculator); the daemon's own configuration, series and store are
// used.
func (s *HTTPServer) AttachUsageCalculator(queue usagecalc.QueueCounter) error {
	if s.api == nil {
		return fmt.Errorf("usage calculator: no API server to wire")
	}
	return s.api.AttachUsageCalculator(queue)
}

// AttachUsageCalculator composes the advice service from this daemon's live settings, the
// shared readings series and the daemon-local store, and sets it on the API server. queue
// counts this daemon's unfinished automation tasks, the finish-early estimate of
// decision 6; nil disables that estimate.
func (s *Server) AttachUsageCalculator(queue usagecalc.QueueCounter) error {
	series, store, err := s.usageSeriesAndStore()
	if err != nil {
		return fmt.Errorf("usage calculator: %v", err)
	}
	settings := func() usagecalc.Settings {
		if s.cfg == nil {
			return usagecalc.Settings{}
		}
		return UsageCalculatorSettings(s.cfg.Usage.Calculator)
	}
	s.SetUsageCalculator(usagecalc.NewService(series, store, settings, queue))
	return nil
}

// usageSeriesAndStore returns the shared series and the daemon-local store behind it.
func (s *Server) usageSeriesAndStore() (*usagecalc.SharedSeries, *usagecalc.Store, error) {
	series, err := s.usageSeries()
	if err != nil {
		return nil, nil, err
	}
	return series, s.usageSeriesCache().store, nil
}

// handleUsageAdvice serves GET /api/usage/advice. The query parameters variant=, policy=,
// lanes= and cost= override the stored settings for this one call (variant and policy are
// independent: the variant picks the rendering, the policy picks the target).
func (s *Server) handleUsageAdvice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, usageCapabilityRead) {
		return
	}
	calc := s.UsageCalculator()
	if calc == nil {
		http.Error(w, "usage calculator: not wired on this daemon", http.StatusServiceUnavailable)
		return
	}
	o := usagecalc.Options{}
	if v := strings.TrimSpace(r.URL.Query().Get("variant")); v != "" {
		o.Variant = v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("policy")); v != "" {
		o.Policy = v
	}
	if v := strings.TrimSpace(r.URL.Query().Get("lanes")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, "lanes: a whole number of 0 or more is needed", http.StatusBadRequest)
			return
		}
		o.Lanes = n
	}
	if v := strings.TrimSpace(r.URL.Query().Get("cost")); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || n < 0 {
			http.Error(w, "cost: a number of 0 or more points is needed", http.StatusBadRequest)
			return
		}
		cost := n
		o.CostPerTask = &cost
	}
	res, err := calc.Advice(r.Context(), o)
	if err != nil {
		status := http.StatusServiceUnavailable
		if isUsageSelectionError(err) {
			// A bad variant or policy is a request problem, not a daemon one.
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	// A stale series was served from the last copy because the keeper could not be
	// reached (decision 8); the reading endpoints carry the same markers.
	if res.Stale {
		w.Header().Set("X-Usage-Stale", res.StaleReason)
		if res.CopiedAt != nil {
			w.Header().Set("X-Usage-Copied-At", res.CopiedAt.UTC().Format(time.RFC3339))
		}
	}
	writeJSONOK(w, res)
}

// isUsageSelectionError reports the service's own "unknown variant / unknown policy"
// errors, the ones a client can fix on the next request.
func isUsageSelectionError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unknown usage calculator variant") ||
		strings.Contains(msg, "unknown usage calculator policy")
}

// handleUsageRemainingWork serves PUT (set the typed figure) and DELETE (remove it). The
// figure is daemon-local (decision 6): it stays in this daemon's store and never travels
// with the shared account record.
func (s *Server) handleUsageRemainingWork(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, usageCapabilityWrite) {
		return
	}
	calc := s.UsageCalculator()
	if calc == nil {
		http.Error(w, "usage calculator: not wired on this daemon", http.StatusServiceUnavailable)
		return
	}
	var points *float64
	switch r.Method {
	case http.MethodPut:
		var in struct {
			Points *float64 `json:"points"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
			http.Error(w, "a remaining-work figure is needed: "+err.Error(), http.StatusBadRequest)
			return
		}
		points = in.Points
	case http.MethodDelete:
		// points stays nil: remove the typed figure.
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := calc.SetRemainingWork(r.Context(), points); err != nil {
		if strings.Contains(err.Error(), "no local store is wired") {
			http.Error(w, "usage calculator: not wired on this daemon", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The figure is daemon-local (decision 6): the daemon that stores the write is
	// this one, so it emits its own audit entry per the Audit Logging Rule.
	s.auditUsageRemainingWorkSet(r, points)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
