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
	"strings"
	"sync"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
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
// A lowered cap is kept only while the entry's own effective cap is still what it was when
// the guard lowered it (see get); the pool limit always follows the entry once the operator
// changes it.
var burnGuardInflight = &inflightOverrides{m: map[string]inflightOverride{}}

// inflightOverride is one lowered cap and the entry's effective cap at the time the guard
// lowered it, which tells a later reader whether the operator has changed the entry since.
type inflightOverride struct {
	value    int
	baseline int
}

type inflightOverrides struct {
	mu sync.Mutex
	m  map[string]inflightOverride
}

// get returns the lowered cap for name, if the guard still holds one. effective is the
// entry's effective cap now: when it differs from the one the guard lowered from, the
// operator changed the entry after the lowering, so the lowered cap is dropped and the
// entry's value stands.
func (o *inflightOverrides) get(name string, effective int) (int, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	v, ok := o.m[name]
	if !ok {
		return 0, false
	}
	if v.baseline != effective {
		delete(o.m, name)
		return 0, false
	}
	return v.value, true
}

// set records the cap the guard lowered name to and the entry's effective cap it lowered from.
func (o *inflightOverrides) set(name string, n, baseline int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.m[name] = inflightOverride{value: n, baseline: baseline}
}

func (o *inflightOverrides) clear(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.m, name)
}

// burnGuardRoutes records the backends whose new work is routed elsewhere.
var burnGuardRoutes = &routeTable{m: map[string]string{}}

// burnGuardModelRoutes records, by backend, the model new work is run with when the
// route_cheaper_model rung sends that backend's new work to a cheaper model.
var burnGuardModelRoutes = &routeTable{m: map[string]string{}}

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
	// pace is the registry the pace policy holds new work through (see
	// burn_guard_pace_wiring.go); nil means the daemon's own.
	pace *paceHolds
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
	effective := inference.EffectiveMaxInflight(*l, a.cfg.Autonomous.PlanTierMaxInflight)
	if v, ok := burnGuardInflight.get(l.Name, effective); ok {
		return v
	}
	return effective
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
		burnGuardInflight.set(l.Name, n, original)
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

// RouteModel is the route_cheaper_model seam: it records (on) or forgets (off) which
// model new work of backend runs with. It changes only the model of new work on the
// same backend and never ends a running session.
func (a burnGuardAdmission) RouteModel(backend, model string, on bool) error {
	burnGuardModelRoutes.mu.Lock()
	defer burnGuardModelRoutes.mu.Unlock()
	if on && model != "" {
		burnGuardModelRoutes.m[backend] = model
	} else {
		delete(burnGuardModelRoutes.m, backend)
	}
	return nil
}

func (a burnGuardAdmission) HoldNewWork(backend, rung string, on bool) error {
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
	flag := "burn guard: weekly burn projects over the pace line"
	if backend != "" {
		flag = fmt.Sprintf("burn guard: entry %s burns over its pace line", backend)
	}
	a.bo.SetWithRung(backend, until.In(time.Local), flag, burnGuardBackoffSource, resetKnown, rung)
	return nil
}

// HoldModels holds (on) or releases (off) new work on some models of backend only: the
// guarded entry's held models. The flag carries the models, so work on the backend's
// other models keeps running.
func (a burnGuardAdmission) HoldModels(backend string, models []string, rung string, on bool) error {
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
	a.bo.SetWithModels(backend, until.In(time.Local), fmt.Sprintf("burn guard: entry %s burns over its pace line (held models: %s)", backend, strings.Join(models, ", ")), burnGuardBackoffSource, resetKnown, rung, models)
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
		cfg: cfg, ledger: led, bo: autonomousBackoff, pace: burnGuardPaceHolds,
		reg: func() *inference.Registry { return httpServer.InferenceRegistry() },
	}
	if err := httpServer.AttachBurnGuard(adm, burnGuardSessions{mgr: mgr}, claudeTranscriptDir()); err != nil {
		fmt.Fprintf(os.Stderr, "[burn-guard] not wired: %v\n", err)
		return
	}
	// Admission reads the route when it resolves a task's backend.
	autonomouspkg.SetBackendRoute(burnGuardRoutes.RouteFor)
	autonomouspkg.SetBackendModelRoute(burnGuardModelRoutes.RouteFor)
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
