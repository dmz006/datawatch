// burn_guard_wiring.go — the daemon-side admission seam of the burn guard
// (internal/server/burn_guard.go). The guard only changes admission: how many sessions a
// backend may start, where new work is routed, and whether new work waits. Nothing here
// ends, stops or restarts a session.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
	statspkg "github.com/dmz006/datawatch/internal/stats"
)

// burnGuardBackoffSource marks the back-off flag the hold rung sets.
const burnGuardBackoffSource = "burn_guard"

// burnGuardInflight holds the inflight caps the guard has lowered, by LLM entry name.
// syncLLMLimits consults it on every pass, so the 10s limit sync does not undo a lowered cap.
var burnGuardInflight = &inflightOverrides{m: map[string]int{}}

type inflightOverrides struct {
	mu sync.Mutex
	m  map[string]int
}

func (o *inflightOverrides) get(name string) (int, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	v, ok := o.m[name]
	return v, ok
}

func (o *inflightOverrides) set(name string, n int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.m[name] = n
}

func (o *inflightOverrides) clear(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.m, name)
}

// burnGuardRoutes records the backends whose new work is routed elsewhere.
var burnGuardRoutes = &routeTable{m: map[string]string{}}

type routeTable struct {
	mu sync.Mutex
	m  map[string]string
}

// RouteFor returns where new work meant for backend is routed, if the guard routes it.
func (r *routeTable) RouteFor(backend string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	to, ok := r.m[backend]
	return to, ok
}

// burnGuardAdmission implements server.BurnGuardAdmission on the capacity ledger, the LLM
// registry and the per-backend back-off registry.
type burnGuardAdmission struct {
	cfg    *config.Config
	reg    func() *inference.Registry
	ledger *capacity.Ledger
	bo     *autonomouspkg.Backoff
	now    func() time.Time
}

// entry finds the registry entry that serves backend (the entry name is the backend name).
func (a burnGuardAdmission) entry(backend string) (*inference.LLM, error) {
	var reg *inference.Registry
	if a.reg != nil {
		reg = a.reg()
	}
	if reg == nil {
		return nil, fmt.Errorf("no LLM registry")
	}
	l, err := reg.Get(backend)
	if err != nil || l == nil {
		return nil, fmt.Errorf("no LLM entry %q", backend)
	}
	return l, nil
}

func (a burnGuardAdmission) InflightCap(backend string) int {
	l, err := a.entry(backend)
	if err != nil {
		return 0
	}
	if v, ok := burnGuardInflight.get(l.Name); ok {
		return v
	}
	return inference.EffectiveMaxInflight(*l, a.cfg.Autonomous.PlanTierMaxInflight)
}

func (a burnGuardAdmission) SetInflight(backend string, n int) error {
	l, err := a.entry(backend)
	if err != nil {
		return err
	}
	original := inference.EffectiveMaxInflight(*l, a.cfg.Autonomous.PlanTierMaxInflight)
	// A release sets the original back; drop the override when it equals the configured cap.
	if n == original {
		burnGuardInflight.clear(l.Name)
	} else {
		burnGuardInflight.set(l.Name, n)
	}
	if a.ledger != nil {
		a.ledger.SetLimit("llm:"+l.Name, n)
	}
	return nil
}

func (a burnGuardAdmission) RouteBackend(from, to string, on bool) error {
	burnGuardRoutes.mu.Lock()
	defer burnGuardRoutes.mu.Unlock()
	if on {
		burnGuardRoutes.m[from] = to
	} else {
		delete(burnGuardRoutes.m, from)
	}
	return nil
}

func (a burnGuardAdmission) HoldNewWork(backend string, on bool) error {
	if a.bo == nil {
		return fmt.Errorf("no back-off registry")
	}
	if !on {
		a.bo.Clear(backend)
		return nil
	}
	now := time.Now
	if a.now != nil {
		now = a.now
	}
	until, resetKnown := a.holdDeadline(now())
	a.bo.SetWithRung(backend, until.In(time.Local), "burn guard: weekly burn projects over the pace line", burnGuardBackoffSource, resetKnown, burnguard.RungHoldClaude)
	return nil
}

// holdDeadline is the deadline of the hold flag: the earlier of the next weekly reset
// (from the usage calculator settings) and the next guard reading (now plus the guard's
// tick interval). The guard renews the flag on every tick that keeps the hold rung
// applied, so the deadline spans at most one reset or one reading — never a fixed
// long span. resetKnown is false when the reset cannot be resolved; then the deadline
// is the next guard reading and the surface shows it as a maximum back-off.
func (a burnGuardAdmission) holdDeadline(now time.Time) (time.Time, bool) {
	mins := 0
	if a.cfg != nil {
		mins = a.cfg.Usage.BurnGuard.IntervalMinutes
	}
	if mins < 1 {
		mins = config.BurnGuardIntervalMinutesDefault
	}
	until := now.Add(time.Duration(mins) * time.Minute)
	if a.cfg == nil {
		return until, false
	}
	reset, err := server.UsageCalculatorSettings(a.cfg.Usage).Reset.NextReset(now)
	if err != nil {
		return until, false
	}
	if reset.Before(until) {
		until = reset
	}
	return until, true
}

// burnGuardSessions is the read-only session view: it only counts.
type burnGuardSessions struct{ mgr *session.Manager }

func (s burnGuardSessions) RunningCount() int {
	if s.mgr == nil {
		return 0
	}
	n := 0
	for _, sess := range s.mgr.ListSessions() {
		if sess != nil && sess.State == session.StateRunning {
			n++
		}
	}
	return n
}

// claudeTranscriptDir is where Claude Code writes its transcripts.
func claudeTranscriptDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// wireBurnGuard attaches and starts the burn guard. A failure is logged; the daemon runs without the guard.
func wireBurnGuard(ctx context.Context, cfg *config.Config, httpServer *server.HTTPServer, mgr *session.Manager, led *capacity.Ledger) {
	adm := burnGuardAdmission{
		cfg: cfg, ledger: led, bo: autonomousBackoff,
		reg: func() *inference.Registry { return httpServer.InferenceRegistry() },
	}
	if err := httpServer.AttachBurnGuard(adm, burnGuardSessions{mgr: mgr}, claudeTranscriptDir()); err != nil {
		fmt.Fprintf(os.Stderr, "[burn-guard] not wired: %v\n", err)
		return
	}
	// Admission reads the route when it resolves a task's backend.
	autonomouspkg.SetBackendRoute(burnGuardRoutes.RouteFor)
	httpServer.StartBurnGuard(ctx)
}

// fillBurnGuardStats copies the guard's stats onto a SystemStats snapshot.
func fillBurnGuardStats(s *statspkg.SystemStats, g server.BurnGuardStats) {
	s.BurnGuardState = g.State
	s.BurnGuardLevel = g.Level
	s.BurnGuardProjected = g.Projected
	s.BurnGuardTarget = g.Target
	s.BurnGuardOverBy = g.OverBy
	s.BurnGuardPointsPerHour = g.PointsPerHour
	s.BurnGuardLastTickAge = g.LastTickAgeSec
	s.BurnGuardTicks = g.Counters.Ticks
	s.BurnGuardClimbs = g.Counters.Climbs
	s.BurnGuardStepBacks = g.Counters.StepBacks
	s.BurnGuardRefits = g.Counters.Refits
	s.BurnGuardErrors = g.Counters.Errors
}

// burnGuardLast holds the counter values last pushed to Prometheus, so each collection adds
// only the difference.
var burnGuardLast struct {
	mu                                     sync.Mutex
	ticks, climbs, stepBacks, refits, errs int64
}

// updateBurnGuardMetrics publishes the burn guard fields of a stats snapshot.
func updateBurnGuardMetrics(s statspkg.SystemStats) {
	metrics.BurnGuardLevel.Set(float64(s.BurnGuardLevel))
	metrics.BurnGuardProjectedPercent.Set(s.BurnGuardProjected)
	metrics.BurnGuardOverByPoints.Set(s.BurnGuardOverBy)
	metrics.BurnGuardPointsPerHour.Set(s.BurnGuardPointsPerHour)
	l := &burnGuardLast
	l.mu.Lock()
	defer l.mu.Unlock()
	add := func(prev *int64, now int64, inc func(float64)) {
		if now > *prev {
			inc(float64(now - *prev))
		}
		*prev = now
	}
	add(&l.ticks, s.BurnGuardTicks, metrics.BurnGuardTicksTotal.Add)
	add(&l.climbs, s.BurnGuardClimbs, metrics.BurnGuardStepsTotal.WithLabelValues("climb").Add)
	add(&l.stepBacks, s.BurnGuardStepBacks, metrics.BurnGuardStepsTotal.WithLabelValues("step_back").Add)
	add(&l.refits, s.BurnGuardRefits, metrics.BurnGuardRefitsTotal.Add)
	add(&l.errs, s.BurnGuardErrors, metrics.BurnGuardErrorsTotal.Add)
}
