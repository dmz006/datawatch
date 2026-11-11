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
//	                                                settings for this one call; lanes=
//	                                                is a what-if (0 is honoured) and is
//	                                                never saved
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
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
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

// UsageCalculatorSettings maps the daemon's usage config — the
// usage.calculator settings plus the claude-code billing profile — into the
// service's live settings. The calculator's numeric fields already carry
// defaults after EffectiveUsageCalculator, and the reset zone is the
// effective one from EffectiveUsageResetZone: the operator's set zone, or the daemon's
// local zone when the operator left it unset, or UTC as a last resort. The
// serverZone and fellBack markers ride along so the advice can show the
// "(server zone)" marker and note the UTC fallback.
//
// Two more inputs come from the claude-code billing profile, not from the
// usage.calculator settings (usage.billing.claude-code):
//
//   - the plan's weekly-allowance reset. When the profile names a reset
//     weekday and an HH:MM time, they replace the calculator's weekday and
//     time (Settings.Schedule; the service applies them to the reset and the
//     advice carries reset_from_profile). The zone stays the calculator's —
//     the profile says when, the settings say in whose clock. A weekday or
//     time the profile does not name, or that does not parse, leaves the
//     calculator's own schedule standing and Settings.Schedule zero.
//   - the point value. PointCostEquivalent derives the estimated cost-
//     equivalent of one usage point from the profile's monthly price, and a
//     week of the allowance is WeeklyAllowance times it (Settings.PointValue).
//     It is always an estimate, and PointValue.Source says whether the figure
//     came from a profile the operator set ("profile") or from the assumed
//     default profile, price zero ("default").
func UsageCalculatorSettings(u config.UsageConfig) usagecalc.Settings {
	c := u.Calculator
	e := config.EffectiveUsageCalculator(c)
	zone, serverZone, fellBack := config.EffectiveUsageResetZone(c)
	safety := e.SafetyPoints
	baseline := e.BaselinePer24h
	reset := usagecalc.ResetSpec{
		Weekday:    usageWeekday[strings.ToLower(strings.TrimSpace(e.ResetWeekday))],
		Clock:      strings.TrimSpace(e.ResetTime),
		Zone:       zone,
		ServerZone: serverZone,
		FellBack:   fellBack,
	}

	eff := u.EffectiveBillingProfile("claude-code")
	p := eff.Profile
	var schedule usagecalc.Schedule // provenance of reset.Weekday/Clock when set
	if wd, ok := usageWeekday[strings.ToLower(strings.TrimSpace(p.ResetWeekday))]; ok {
		if clock := strings.TrimSpace(p.ResetTime); clock != "" {
			cand := reset
			cand.Weekday, cand.Clock = wd, clock
			if cand.Validate() == nil {
				reset.Weekday, reset.Clock = wd, clock
				schedule = usagecalc.Schedule{Weekday: wd, Clock: clock}
			}
		}
	}
	perPoint := p.PointCostEquivalent()
	if math.IsNaN(perPoint) || perPoint < 0 {
		perPoint = 0
	}
	source := usagecalc.PointValueSourceDefault
	if !eff.IsDefault {
		source = usagecalc.PointValueSourceProfile
	}
	return usagecalc.Settings{
		Variant:        e.Variant,
		Policy:         e.Policy,
		CostPerTask:    e.CostPerTaskPoints,
		MaxLanes:       e.MaxLanes,
		SafetyPoints:   &safety,
		BaselinePer24h: &baseline,
		Reset:          reset,
		Schedule:       schedule,
		PointValue: &usagecalc.PointValue{
			Estimate: true,
			Source:   source,
			PerPoint: perPoint,
			PerWeek:  perPoint * usagecalc.WeeklyAllowance,
			Currency: strings.ToUpper(strings.TrimSpace(p.Currency)),
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

// UsageCalculator returns the attached advice service, or nil when no API
// server is wired or the service was never attached. The hourly auto-lanes
// loop reads the freshest advice through this so it reasons with the same
// settings, readings series and store as the REST surface.
func (s *HTTPServer) UsageCalculator() *usagecalc.Service {
	if s.api == nil {
		return nil
	}
	return s.api.UsageCalculator()
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
		return UsageCalculatorSettings(s.cfg.Usage)
	}
	s.SetUsageCalculator(usagecalc.NewService(series, store, settings, queue,
		usagecalc.WithLiveLanes(usageLaneCounter{s: s}),
		usagecalc.WithStartupSource(s.usageStartupProfiles)))
	return nil
}

// usageStartupProfiles returns the start-up figures per worker profile for the advice's
// start-up line, read from the same cost report the cost_report surface serves (the
// per-profile startup table over the daemon's recorded attempts). It is read-only. The
// unprofiled row is left out: it names no profile to change. It returns nil when the daemon
// has no session manager or the report cannot be built, so the advice carries no line.
func (s *Server) usageStartupProfiles() []usagecalc.StartupProfile {
	if s == nil || s.manager == nil {
		return nil
	}
	mgr := s.costReportAutonomousManager()
	src := s.newManagerCostSource(mgr)
	cr := config.CostReportConfig{}
	if s.cfg != nil {
		cr = s.cfg.CostReport
	}
	rep, err := costreport.Build(cr, costreport.Request{}, src, src, s.exposedRate, nil, time.Now())
	if err != nil || rep == nil {
		return nil
	}
	out := make([]usagecalc.StartupProfile, 0, len(rep.StartupByProfile))
	for _, row := range rep.StartupByProfile {
		if row.Key == costreport.UnprofiledKey {
			continue
		}
		p := usagecalc.StartupProfile{Profile: row.Key, Attempts: row.Attempts}
		if row.AvgStartupTokens != nil {
			p.AvgTokens = *row.AvgStartupTokens
			p.HasAverage = true
		}
		out = append(out, p)
	}
	return out
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
		// An explicit lanes=0 is a what-if of zero lanes; only a missing parameter
		// leaves Lanes nil.
		o.Lanes = &n
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
