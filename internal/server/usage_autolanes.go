// usage_autolanes.go — the read-only status surface of the usage
// calculator's auto-lanes (usage.calculator.auto_lanes):
// GET /api/usage/autolanes reports the settings the daemon runs with
// (enabled flag, policy, operator maximum), the inflight cap this daemon
// applies right now, what the fresh advice's decision would do to that
// cap (target and direction), the number of special items open at the
// configured priority threshold, the last change the hourly loop
// recorded, and the loop's recent decisions, newest first.
//
// The hourly loop and its decision log live in cmd/datawatch
// (usage_autolanes_wiring.go). Both it and this handler recompute the
// same pure decision (usagecalc.DecideAutoLanes) from the same inputs,
// so the status always reflects exactly the rule the loop applies.
// There is no write route here: changing the cap goes through the
// operator's LLM update path, the same path the loop itself uses.

package server

import (
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// autoLanesMaxRecentChanges is how many of the newest decisions the
// autoLanesStatus' recent_changes carries.
const autoLanesMaxRecentChanges = 50

// autoLanesChange is one decision from the daemon's auto-lanes decision
// log: the inflight cap moved from From to To at At for Reason.
type autoLanesChange struct {
	At     time.Time `json:"at"`
	From   int       `json:"from"`
	To     int       `json:"to"`
	Reason string    `json:"reason"`
}

// autoLanesStatus is the body of GET /api/usage/autolanes.
type autoLanesStatus struct {
	Enabled bool `json:"enabled"`
	// Policy is the active usage policy the advice (and the auto-lanes
	// decision) reads from.
	Policy string `json:"policy"`
	// Max is the operator maximum (usage.calculator.auto_lanes_max).
	Max int `json:"max"`
	// CurrentMaxInflight is the cap the daemon applies now — the first
	// enabled claude-code LLM entry's max_inflight, the entry the hourly
	// loop adjusts. Nil when the registry has no such entry.
	CurrentMaxInflight *int `json:"current_max_inflight"`
	// Target is the decision's inflight cap for the current advice. Nil
	// when auto-lanes is off or the advice is not servable right now.
	Target *int `json:"target"`
	// Direction is usagecalc.AutoLanesUp / AutoLanesDown /
	// AutoLanesUnchanged against CurrentMaxInflight. Nil with Target.
	Direction *string `json:"direction"`
	// LastChange is the newest recorded decision that actually moved the
	// cap; nil when the loop has never moved it.
	LastChange *autoLanesChange `json:"last_change"`
	// SpecialItems is the number of open items at the configured
	// auto-lanes special priority threshold, counted now.
	SpecialItems int `json:"special_items"`
	// RecentChanges is the decision log, newest first, at most
	// autoLanesMaxRecentChanges entries.
	RecentChanges []autoLanesChange `json:"recent_changes"`
}

// autoLanesTargetEntry is the first enabled claude-code LLM entry — the
// one the hourly auto-lanes loop adjusts.
func autoLanesTargetEntry(reg *inference.Registry) (*inference.LLM, bool) {
	if reg == nil {
		return nil, false
	}
	for _, l := range reg.List() {
		if l != nil && l.Kind == inference.KindClaudeCode && l.Enabled() {
			return l, true
		}
	}
	return nil, false
}

// autoLanesSpecial returns the wired special-item count for the priority
// threshold; 0 when no counter is wired or the counter reports a negative.
func (s *Server) autoLanesSpecial(threshold int) int {
	if s.autoLanesSpecialCounter == nil {
		return 0
	}
	n := s.autoLanesSpecialCounter(threshold)
	if n < 0 {
		return 0
	}
	return n
}

// autoLanesChanges reads the decision log fresh from disk — the loop opens
// its own handle and appends atomically, so a read opened per request
// always sees the newest entries — and returns the newest
// autoLanesMaxRecentChanges decisions, newest first. A log that cannot be
// opened yields no changes rather than an error: the settings part of the
// status is still worth serving.
func (s *Server) autoLanesChanges() []autoLanesChange {
	out := []autoLanesChange{}
	logStore, err := usagecalc.OpenAutoLanesLog(s.usageDataDir())
	if err != nil {
		return out
	}
	entries := logStore.Entries() // append order, oldest first
	for i := len(entries) - 1; i >= 0 && len(out) < autoLanesMaxRecentChanges; i-- {
		e := entries[i]
		out = append(out, autoLanesChange{At: e.At, From: e.From, To: e.To, Reason: e.Reason})
	}
	return out
}

// autoLanesLastChange is the newest decision in changes that actually
// moved the cap, or ok false if none of them did.
func autoLanesLastChange(changes []autoLanesChange) (*autoLanesChange, bool) {
	for i := range changes {
		if changes[i].From != changes[i].To {
			c := changes[i]
			return &c, true
		}
	}
	return nil, false
}

// handleUsageAutoLanes serves GET /api/usage/autolanes (read-only).
func (s *Server) handleUsageAutoLanes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, usageCapabilityRead) {
		return
	}

	calcCfg := config.UsageCalculatorConfig{}
	if s.cfg != nil {
		calcCfg = s.cfg.Usage.Calculator
	}
	e := config.EffectiveUsageCalculator(calcCfg)

	status := autoLanesStatus{
		Enabled:       e.AutoLanes,
		Policy:        e.Policy,
		Max:           e.AutoLanesMax,
		SpecialItems:  s.autoLanesSpecial(e.AutoLanesSpecialPriority),
		RecentChanges: s.autoLanesChanges(),
	}
	if last, ok := autoLanesLastChange(status.RecentChanges); ok {
		status.LastChange = last
	}
	if entry, ok := autoLanesTargetEntry(s.inferenceReg); ok && entry != nil {
		cur := entry.MaxInflight
		status.CurrentMaxInflight = &cur
	}

	if !e.AutoLanes {
		// Off: report the settings, the current cap and the log; there is
		// no decision to show.
		writeJSONOK(w, status)
		return
	}

	svc := s.UsageCalculator()
	if svc == nil {
		http.Error(w, "usage calculator: not wired on this daemon", http.StatusServiceUnavailable)
		return
	}
	res, err := svc.Advice(r.Context(), usagecalc.Options{})
	if err != nil || res == nil {
		// The advice is not servable right now (e.g. no readings yet):
		// the settings, cap and decision log are still worth serving.
		writeJSONOK(w, status)
		return
	}
	current := 0
	if status.CurrentMaxInflight != nil {
		current = *status.CurrentMaxInflight
	}
	d := usagecalc.DecideAutoLanes(res.Advice.Advice.LanesSuggested, status.SpecialItems, e.AutoLanesMax, current)
	status.Target = &d.Target
	dir := d.Direction
	status.Direction = &dir
	writeJSONOK(w, status)
}

// SetAutoLanesSpecialCounter wires the counter behind the auto-lanes
// status' special_items: the number of open items at the given priority
// threshold (daemon main wires this to the autonomous manager's open-PRD
// count). Nil unwires it and the status reports 0.
func (s *Server) SetAutoLanesSpecialCounter(c func(threshold int) int) {
	s.autoLanesSpecialCounter = c
}

// SetAutoLanesSpecialCounter wires the counter on the embedded API server.
func (s *HTTPServer) SetAutoLanesSpecialCounter(c func(threshold int) int) {
	if s.api != nil {
		s.api.SetAutoLanesSpecialCounter(c)
	}
}
