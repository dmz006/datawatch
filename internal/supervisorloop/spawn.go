package supervisorloop

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Supervisor spawning. The daemon runs a fresh, short supervisor session
// for every recurring tick and for every trigger event, in place of one
// long-lived supervisor pane: the session starts with the lean supervisor
// profile, does its one pass, and ends. The loop package stays free of
// the session layer — the daemon injects a SessionLauncher and,
// optionally, an AlertSink and a ResultReader. Every spawn carries an
// explicit model: the supervisor must stay lean, so Sonnet or Haiku —
// never the heavy tier.

// LaunchRoleSupervisor is the launch role every supervisor spawn carries,
// so the session layer applies the lean supervisor profile.
const LaunchRoleSupervisor = "supervisor"

// TickKind names the recurring supervisor passes.
type TickKind string

const (
	// TickWatch is the light cadence pass: check pinned state, triage what
	// changed, record what was done.
	TickWatch TickKind = "watch"
	// TickWindowGuard is the projection pass: re-estimate the burn window
	// and act (hold, nudge, escalate) before the pace line is crossed.
	TickWindowGuard TickKind = "window_guard"
	// TickFull is the full pass: steps plus model jobs, run as the daemon
	// sees fit.
	TickFull TickKind = "full"
)

// AllTicks lists the recurring ticks in their usual order.
var AllTicks = []TickKind{TickWatch, TickWindowGuard, TickFull}

// Valid reports whether k is one of the recurring tick kinds.
func (k TickKind) Valid() bool {
	for _, t := range AllTicks {
		if k == t {
			return true
		}
	}
	return false
}

// TriggerKind names the event-driven supervisor wakes.
type TriggerKind string

const (
	// TriggerPriorityStall: a high-priority item has been waiting past its
	// due line and nothing is making progress on it.
	TriggerPriorityStall TriggerKind = "priority_stall"
	// TriggerJudgeVerdict: a judge verdict landed and the supervisor must
	// act on it (hold the Automaton, record the correction, or move on).
	TriggerJudgeVerdict TriggerKind = "judge_verdict"
	// TriggerFailedSchedule: a spawn schedule went to the failed state.
	// The spawner alerts the operator and restarts a supervisor, so the
	// loss of the cadence is seen and repaired instead of staying silent.
	TriggerFailedSchedule TriggerKind = "failed_schedule"
)

// AllTriggers lists the trigger kinds.
var AllTriggers = []TriggerKind{TriggerPriorityStall, TriggerJudgeVerdict, TriggerFailedSchedule}

// Valid reports whether k is one of the trigger kinds.
func (k TriggerKind) Valid() bool {
	for _, t := range AllTriggers {
		if k == t {
			return true
		}
	}
	return false
}

// LaunchRequest is one fresh supervisor session, as the spawner asks the
// launcher for it.
type LaunchRequest struct {
	Project string
	// Tick is set for recurring passes; Trigger for event-driven ones.
	Tick    TickKind
	Trigger TriggerKind
	// Role is always LaunchRoleSupervisor; the spawner fills it on every
	// request, kept explicit so the launcher can assert it.
	Role string
	// Model is the explicit session model (validated on the spawner).
	Model string
	// OneShot: the session does its one pass and ends. The spawner sets it
	// true; a launcher that instead runs a long job reports Detached on
	// the result, and the next tick reads it back.
	OneShot bool
	// Detail is the human context for the spawn (the stalled item, the
	// failed schedule name and failure, the verdict summary).
	Detail string
}

// Reason renders the request into its spawn reason ("watch", "full",
// "judge_verdict", …), which is what the spawner de-dupes on.
func (r LaunchRequest) Reason() string {
	switch {
	case r.Tick != "":
		return string(r.Tick)
	case r.Trigger != "":
		return string(r.Trigger)
	default:
		return LaunchRoleSupervisor
	}
}

// LaunchResult reports what the launcher did with the request.
type LaunchResult struct {
	SessionID string
	// Detached marks a long job the launcher does not complete inside the
	// launch call: the spawner keeps it pending and the next tick reads
	// its result via the ResultReader.
	Detached bool
}

// SessionLauncher starts a supervisor session on the daemon's session
// layer. The daemon wires the real implementation; this package never
// imports the session layer directly.
type SessionLauncher interface {
	Launch(ctx context.Context, req LaunchRequest) (*LaunchResult, error)
}

// AlertSink fires an operator-visible alert.
type AlertSink interface {
	Alert(title, body string)
}

// ResultReader reads the result of a detached supervisor job. It returns
// the final output and whether the job has reached its end state. A
// non-nil error leaves the job pending so the next tick retries the read.
type ResultReader interface {
	Read(ctx context.Context, sessionID string) (output string, done bool, err error)
}

// PendingJob is a detached long job the spawner is waiting on for a
// result, read on a later tick.
type PendingJob struct {
	SessionID string
	Reason    string
	StartedAt time.Time
}

// DetachedResult is the outcome of a pending job, read on the next tick.
type DetachedResult struct {
	SessionID string
	Reason    string
	Output    string
}

// TickReport is what one OnTick pass produced.
type TickReport struct {
	// Tick is the recurring pass this report covers.
	Tick TickKind
	// Results are the detached jobs whose results were read this pass.
	Results []DetachedResult
	// Spawned is true when a supervisor session was started for the tick.
	Spawned bool
	// SessionID is the launched session (empty when Spawned is false).
	SessionID string
	// Skipped is the non-launch outcome: "quiet" within the debounce window.
	Skipped string
	// LaunchError is non-nil when the launcher refused the spawn.
	LaunchError error
}

// TriggerReport is what one OnTrigger pass produced.
type TriggerReport struct {
	Trigger TriggerKind
	// Spawned is true when a supervisor was restarted for the trigger.
	Spawned bool
	// SessionID is the restarted supervisor (empty when not spawned).
	SessionID string
	// Alerted is true when an operator alert fired for the trigger.
	Alerted bool
	// Skipped is the non-launch outcome: "quiet" within the debounce window.
	Skipped string
	// LaunchError is non-nil when the launcher refused the spawn.
	LaunchError error
}

// DefaultQuiet is the debounce between two spawns for the same reason. It
// is short on purpose: it stops event bursts (and a restart racing its own
// cadence tick) from double-spawning, while a scheduled tick a minute or
// more later always fires.
const DefaultQuiet = 30 * time.Second

// Spawner turns recurring ticks and trigger events into fresh short
// supervisor sessions through an injected SessionLauncher.
type Spawner struct {
	Project  string
	Model    string
	Launcher SessionLauncher
	// Clock stamps spawn decisions; tests inject a deterministic one.
	Clock Clock
	// Quiet is the debounce per spawn reason; zero uses DefaultQuiet.
	Quiet time.Duration
	// Alert fires an operator-visible alert. May be nil: then a failed
	// schedule is still restarted, only not announced.
	Alert AlertSink
	// Reader reads detached job results on the next tick. May be nil: then
	// detached jobs stay pending until SetReader is called.
	Reader ResultReader

	mu      sync.Mutex
	last    map[string]time.Time
	pend    []*PendingJob
	nowFunc func() time.Time
}

// SpawnerOptions builds a Spawner.
type SpawnerOptions struct {
	Project  string
	Model    string
	Launcher SessionLauncher
	Clock    Clock
	Quiet    time.Duration
	Alert    AlertSink
	Reader   ResultReader
}

// NewSpawner builds a Spawner, enforcing the supervisor model policy up
// front: an empty or non-Sonnet/Haiku model is refused here, not at spawn
// time.
func NewSpawner(o SpawnerOptions) (*Spawner, error) {
	if o.Project == "" {
		return nil, errors.New("supervisor spawner: project is required")
	}
	if o.Launcher == nil {
		return nil, errors.New("supervisor spawner: a SessionLauncher is required")
	}
	if o.Clock == nil {
		return nil, errors.New("supervisor spawner: a Clock is required")
	}
	if err := ValidateSupervisorModel(o.Model); err != nil {
		return nil, err
	}
	q := o.Quiet
	if q <= 0 {
		q = DefaultQuiet
	}
	return &Spawner{
		Project:  o.Project,
		Model:    o.Model,
		Launcher: o.Launcher,
		Clock:    o.Clock,
		Quiet:    q,
		Alert:    o.Alert,
		Reader:   o.Reader,
		last:     map[string]time.Time{},
		nowFunc:  o.Clock.Now,
	}, nil
}

// now reports the spawner's clock (never the wall clock in tests).
func (s *Spawner) now() time.Time { return s.nowFunc() }

// SetReader installs the ResultReader detached jobs are read through, or
// nil to pause result reading (jobs then stay pending).
func (s *Spawner) SetReader(r ResultReader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Reader = r
}

// spawnerSpawn decides whether a spawn for reason is granted: it is held
// while within quiet of the last granted spawn for that reason, and the
// grant is recorded in s.last.
func spawnerSpawn(s *Spawner, reason string, quiet time.Duration) (bool, time.Time) {
	now := s.nowFunc()
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.last[reason]; ok && now.Sub(t) < quiet {
		return false, t.Add(quiet)
	}
	s.last[reason] = now
	return true, now.Add(quiet)
}

// QuietError reports a spawn skipped by the quiet window.
type QuietError struct {
	Reason    string
	HeldUntil time.Time
}

// Error implements error.
func (q *QuietError) Error() string {
	return fmt.Sprintf("supervisor spawn %q is within the quiet window until %s", q.Reason, q.HeldUntil.UTC().Format(time.RFC3339))
}

// LaunchWith launches a supervisor session with the exact request the
// caller composed. The spawner stamps its supervisor role and its own
// model onto the request before handing it over, so a caller cannot drift
// from the policy.
func (s *Spawner) LaunchWith(ctx context.Context, req LaunchRequest) (*LaunchResult, error) {
	req.Role = LaunchRoleSupervisor
	req.Model = s.Model
	res, err := s.Launcher.Launch(ctx, req)
	if err != nil {
		return res, err
	}
	if res != nil && res.Detached && res.SessionID != "" {
		s.mu.Lock()
		s.pend = append(s.pend, &PendingJob{SessionID: res.SessionID, Reason: req.Reason(), StartedAt: s.now()})
		s.mu.Unlock()
	}
	return res, nil
}

// isQuiet reports whether err is a QuietError.
func isQuiet(err error) bool {
	var q *QuietError
	return errors.As(err, &q)
}

// Launch starts a supervisor session for reason (a tick kind or a trigger
// kind, string-form) with the human detail. The quiet window is per
// reason, so a priority stall and a judge verdict in the same minute do
// not cancel each other.
func (s *Spawner) Launch(ctx context.Context, reason, detail string) (*LaunchResult, error) {
	started, heldUntil := spawnerSpawn(s, reason, s.Quiet)
	if !started {
		return nil, &QuietError{Reason: reason, HeldUntil: heldUntil}
	}
	req := LaunchRequest{
		Project: s.Project,
		OneShot: true,
		Detail:  detail,
	}
	if t := TickKind(reason); t.Valid() {
		req.Tick = t
	} else if tr := TriggerKind(reason); tr.Valid() {
		req.Trigger = tr
	} else {
		return nil, fmt.Errorf("supervisor spawner: %q is neither a tick nor a trigger", reason)
	}
	return s.LaunchWith(ctx, req)
}

// OnTick runs one recurring supervisor pass: it first reads the results
// of detached jobs left over from earlier passes, then launches a fresh
// short supervisor session for the tick (held by the quiet window when a
// spawn for that tick just happened). A launch failure is reported on the
// report, not swallowed — the daemon decides whether it becomes a
// failed-schedule trigger worth alerting on.
func (s *Spawner) OnTick(ctx context.Context, tick TickKind) (*TickReport, error) {
	if !tick.Valid() {
		return nil, fmt.Errorf("supervisor spawner: unknown tick %q", tick)
	}
	rep := &TickReport{Tick: tick, Results: s.collectPending(ctx)}
	res, err := s.Launch(ctx, string(tick), "")
	switch {
	case isQuiet(err):
		rep.Skipped = "quiet"
	case err != nil:
		rep.LaunchError = err
	default:
		if res != nil {
			rep.Spawned = true
			rep.SessionID = res.SessionID
		}
	}
	return rep, nil
}

// OnTrigger runs one event-driven supervisor wake. A failed schedule is
// the special trigger: it alerts the operator and then restarts a
// supervisor, the "alert and restart" contract for a schedule that went
// to the failed state.
func (s *Spawner) OnTrigger(ctx context.Context, trig TriggerKind, detail string) (*TriggerReport, error) {
	if !trig.Valid() {
		return nil, fmt.Errorf("supervisor spawner: unknown trigger %q", trig)
	}
	rep := &TriggerReport{Trigger: trig}
	if trig == TriggerFailedSchedule && s.Alert != nil {
		body := detail
		if body == "" {
			body = "a supervisor spawn schedule went to the failed state"
		}
		s.Alert.Alert("Supervisor spawn failed", fmt.Sprintf("project %s: %s", s.Project, body))
		rep.Alerted = true
	}
	res, err := s.Launch(ctx, string(trig), detail)
	switch {
	case isQuiet(err):
		rep.Skipped = "quiet"
	case err != nil:
		rep.LaunchError = err
	default:
		if res != nil {
			rep.Spawned = true
			rep.SessionID = res.SessionID
		}
	}
	return rep, nil
}

// OnScheduleFailed is the failed-schedule entry point: alert the operator
// and restart the supervisor. detail names the schedule and its failure;
// it reaches both the alert body and the restart's spawn detail.
func (s *Spawner) OnScheduleFailed(ctx context.Context, detail string) (*TriggerReport, error) {
	return s.OnTrigger(ctx, TriggerFailedSchedule, detail)
}

// collectPending reads the results of detached jobs that have reached
// their end state; jobs still running (or with a read error) stay pending
// for the next tick.
func (s *Spawner) collectPending(ctx context.Context) []DetachedResult {
	s.mu.Lock()
	reader := s.Reader
	if reader == nil {
		s.mu.Unlock()
		return nil
	}
	jobs := append([]*PendingJob(nil), s.pend...)
	s.pend = s.pend[:0]
	s.mu.Unlock()

	var out []DetachedResult
	still := make([]*PendingJob, 0, len(jobs))
	for _, j := range jobs {
		output, done, err := reader.Read(ctx, j.SessionID)
		if err != nil || !done {
			still = append(still, j)
			continue
		}
		out = append(out, DetachedResult{SessionID: j.SessionID, Reason: j.Reason, Output: output})
	}
	s.mu.Lock()
	s.pend = append(still, s.pend...)
	s.mu.Unlock()

	sort.Slice(out, func(i, j int) bool { return out[i].Reason < out[j].Reason })
	return out
}

// PendingJobs is a copy of the detached jobs still waiting for a result.
func (s *Spawner) PendingJobs() []PendingJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PendingJob, 0, len(s.pend))
	for _, p := range s.pend {
		out = append(out, *p)
	}
	return out
}

// SupervisorScheduleName is the deterministic, project-scoped name of a
// project's recurring supervisor-spawn schedule. The name is a pure
// function of the project so set-up is idempotent: ensuring the same
// project twice yields exactly one schedule, not two.
func SupervisorScheduleName(project string) string {
	if project == "" {
		project = "default"
	}
	return "supervisor-spawn-" + project
}

// SupervisorTickPrompt is the default starting prompt for a recurring
// supervisor pass. The session does its one short pass and ends; any
// human context from the request is appended after it.
func SupervisorTickPrompt(tick TickKind) string {
	base := "You are a datawatch supervisor on a recurring pass. "
	switch tick {
	case TickWatch:
		base += "Watch pass: check the pinned state and recent sessions, triage what changed, record findings, and act only if something is stuck."
	case TickWindowGuard:
		base += "Window guard pass: re-estimate the project's burn window and pace line, and act (hold, nudge, or escalate) before the line is crossed."
	case TickFull:
		base += "Full pass: run the mechanical steps you can verify from the record, then the model jobs, and record what each did."
	}
	base += " Keep the pass short. " + EndingInstruction
	return base
}

// SupervisorTriggerPrompt is the default starting prompt for an
// event-driven supervisor wake.
func SupervisorTriggerPrompt(trig TriggerKind) string {
	base := "You are a datawatch supervisor woken by an event. "
	switch trig {
	case TriggerPriorityStall:
		base += "A high-priority item is stalled past its due line: inspect it, unblock or re-plan it, and record what you did."
	case TriggerJudgeVerdict:
		base += "A judge verdict landed: apply it. If the work is held, keep the hold and record the reason; if it is a correction, apply it and record the correction."
	case TriggerFailedSchedule:
		base += "A supervisor spawn schedule went to the failed state: find why it failed, repair or re-ensure it, and record the repair."
	}
	base += " Keep the pass short. " + EndingInstruction
	return base
}

// Prompt renders the request's default starting prompt: the base
// instruction for its tick or trigger, plus the request's human context
// when set.
func (r LaunchRequest) Prompt() string {
	base := string(r.Reason())
	switch {
	case r.Tick != "":
		base = SupervisorTickPrompt(r.Tick)
	case r.Trigger != "":
		base = SupervisorTriggerPrompt(r.Trigger)
	}
	if r.Detail != "" {
		base += "\n\nContext: " + r.Detail
	}
	return base
}

// SpawnScheduleSpec is the recurring supervisor-spawn schedule the
// spawner assembles for its project. The name is deterministic, so the
// schedule layer can de-dupe by name.
type SpawnScheduleSpec struct {
	// Name is the schedule's name (deterministic per project).
	Name string
	// Ticks are the passes the schedule fires through, in order.
	// Empty means every tick, in order.
	Ticks []TickKind
	// Model is the session model; the spawner stamps its own.
	Model string
	// Role is the launch role; the spawner stamps LaunchRoleSupervisor.
	Role string
	// OneShot marks each fire as a fresh short session.
	OneShot bool
}

// ScheduleStore is the narrow surface of the schedule layer the spawner
// needs for idempotent named set-up. The session package (or the daemon
// over it) implements it; this package only names it, so the loop stays
// free of the session layer.
type ScheduleStore interface {
	// HasScheduleName reports whether a pending spawn schedule with the
	// given name already exists.
	HasScheduleName(name string) (bool, error)
	// AddNamedSpawn adds a spawn schedule under the spec's name. A
	// schedule added for the same name as an existing one MUST be
	// rejected or return the existing one so set-up stays idempotent.
	AddNamedSpawn(spec SpawnScheduleSpec) (id string, err error)
}

// EnsureProjectSchedule is the idempotent set-up for a named project: it
// assembles the project's SpawnScheduleSpec and asks the schedule layer
// to ensure exactly one recurring spawn schedule under it. A second
// call for the same project does not add a second schedule.
func (s *Spawner) EnsureProjectSchedule(store ScheduleStore) (string, error) {
	if store == nil {
		return "", errors.New("supervisor spawner: a ScheduleStore is required")
	}
	if err := ValidateSupervisorModel(s.Model); err != nil {
		return "", err
	}
	spec := SpawnScheduleSpec{
		Name:    SupervisorScheduleName(s.Project),
		Ticks:   append([]TickKind(nil), AllTicks...),
		Model:   s.Model,
		Role:    LaunchRoleSupervisor,
		OneShot: true,
	}
	has, err := store.HasScheduleName(spec.Name)
	if err != nil {
		return "", err
	}
	if has {
		return spec.Name, nil
	}
	if _, err := store.AddNamedSpawn(spec); err != nil {
		return "", err
	}
	return spec.Name, nil
}
