// The supervisor loop's mechanical steps. Each step is a plain Go function
// with no model call: it finds work the existing code already knows how to do
// and calls that code. Every step runs through the supervisorloop engine, so
// each run leaves a run record, and each step has an enabled setting
// (supervisor_loop.<step>.enabled, default true).
//
// The idle tick (automation_idle.go) runs one pass per tick over the steps
// registered here. A step that errors is recorded and is not retried in the
// same tick: a pass runs every enabled step exactly once.
//
// Three steps need something this package cannot import, so they take a hook
// the daemon wiring fills:
//
//   - deploy_after_ship: the operator's deploy step lives in the merge
//     pipeline's ShipRunner (package mergepipeline, which imports this
//     package). With no hook the step records "not configured".
//   - lane_count: the lane control moves the count; the bounds are the
//     supervisor_loop.lane_count settings. With no control, or no bounds
//     configured, the step records "not configured" and moves nothing.
//   - run_merge_until_conflict: mergepipeline.Runner.Run is the existing merge
//     pipeline; the hook wraps it.

package autonomous

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// supervisorLoopActor is the actor named on records the loop writes.
const supervisorLoopActor = "supervisor-loop"

// Step names; they match supervisorloop.StepNames.
const (
	stepCloseFinishedRecords  = "close_finished_records"
	stepDeployAfterShip       = "deploy_after_ship"
	stepRetryFailedInfra      = "retry_failed_infra"
	stepCleanupSweep          = "cleanup_sweep"
	stepApplyAnswers          = "apply_answers"
	stepLaneCount             = "lane_count"
	stepStartNextChainStep    = "start_next_chain_step"
	stepRunMergeUntilConflict = "run_merge_until_conflict"
)

const stepNotConfigured = "not configured"

// ErrSupervisorStepNotConfigured is what a hook returns when the operator has
// set up nothing for its step; the step records "not configured" and does
// nothing, and the pass does not count it as a failure.
var ErrSupervisorStepNotConfigured = errors.New(stepNotConfigured)

// SupervisorLaneControl is the lane control the daemon supplies: the running
// lane count, the count the loop wants and a way to move it. The bounds are
// not part of it: they are the operator's supervisor_loop.lane_count.min and
// .max settings, and the step never sets a count outside them.
type SupervisorLaneControl interface {
	// Current returns the running lane count.
	Current() int
	// Wanted returns the lane count the loop would like.
	Wanted() int
	// SetCount moves the running lane count.
	SetCount(n int) error
}

// SupervisorMergeResult is what one run of the merge pipeline did.
type SupervisorMergeResult struct {
	// Merged names the branches merged and left in place.
	Merged []string
	// Conflict names the branch whose conflict was not settled; empty when
	// the run reached no conflict.
	Conflict string
	// Note is the pipeline's plain-language account.
	Note string
}

// SupervisorLoopHooks are the pieces the daemon wiring supplies. A nil field
// means that part is not configured.
type SupervisorLoopHooks struct {
	// Settings reads the supervisor_loop settings live; nil means defaults.
	Settings func() supervisorloop.Settings
	// Deploy runs the operator's deploy step (mergepipeline ShipRunner.Deploy)
	// and returns a short account. Nil records "not configured".
	Deploy func(ctx context.Context) (string, error)
	// Lanes is the operator's lane control. Nil records "not configured".
	Lanes SupervisorLaneControl
	// MergeUntilConflict runs the existing merge pipeline
	// (mergepipeline.Runner.Run) over the branches waiting to merge, stopping
	// at the first conflict. Nil records "not configured".
	MergeUntilConflict func(ctx context.Context) (SupervisorMergeResult, error)
}

// SupervisorLoopStepResult is one step's outcome in a pass.
type SupervisorLoopStepResult struct {
	Step string
	// Ran is false for a step switched off by its enabled setting.
	Ran    bool
	Detail string
	Err    error
}

// SupervisorLoopSteps runs the eight mechanical steps.
type SupervisorLoopSteps struct {
	c      *Coordinator
	engine *supervisorloop.Engine
	hooks  SupervisorLoopHooks

	// now is the clock the grace checks read. Injectable for tests; it
	// defaults to time.Now. The engine's clock is reserved for run records.
	now func() time.Time
	// archiveBranchCheck, when non-nil, names a record that must not be
	// archived this pass even when it is otherwise ready (a branch that must
	// be kept). Nil (the default) archives everything eligible.
	archiveBranchCheck func(*PRD) bool
	// mergeGit is the git runner the branch check uses. Nil means the
	// production runner; tests inject a fake.
	mergeGit MergeGitRunner

	// wakes, when set, counts each real scheduled pass (RunScheduled).
	wakes *supervisorloop.WakeRecord

	mu            sync.Mutex
	lastScheduled time.Time            // when the last scheduled pass ran
	infraRetried  map[string]bool      // held tasks already retried by the loop
	cancelSeen    map[string]time.Time // first pass that saw a cancelled record with no cancel decision
	lastDeployed  string               // newest shipped release already deployed
	lastConflict  string               // newest merge conflict recorded
}

// NewSupervisorLoopSteps registers the steps over the coordinator. engine
// supplies the clock, the run store and the escalator; its Settings are
// replaced from hooks.Settings on every pass.
func NewSupervisorLoopSteps(c *Coordinator, engine *supervisorloop.Engine, hooks SupervisorLoopHooks) *SupervisorLoopSteps {
	if c == nil || engine == nil {
		return nil
	}
	s := &SupervisorLoopSteps{c: c, engine: engine, hooks: hooks, now: time.Now, infraRetried: map[string]bool{}, cancelSeen: map[string]time.Time{}}
	s.archiveBranchCheck = s.archiveKeepsBranch
	return s
}

// LastConflict returns the newest merge conflict the merge step recorded.
func (s *SupervisorLoopSteps) LastConflict() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastConflict
}

func (s *SupervisorLoopSteps) settings() supervisorloop.Settings {
	if s.hooks.Settings != nil {
		return s.hooks.Settings()
	}
	return supervisorloop.DefaultSettings()
}

// stepEnabled reads the step's enabled setting; an unset step is on.
func stepEnabled(set supervisorloop.Settings, name string) bool {
	on, ok := set.Steps[name]
	return !ok || on
}

// jobs lists the steps in the order a pass runs them.
func (s *SupervisorLoopSteps) jobs() []supervisorloop.Job {
	mech := func(name string, fn func(ctx context.Context) (string, error)) supervisorloop.Job {
		return supervisorloop.Job{
			Name: name, Kind: supervisorloop.KindMechanical,
			Exec: func(ctx context.Context, _ string) (string, error) { return fn(ctx) },
		}
	}
	return []supervisorloop.Job{
		mech(stepCloseFinishedRecords, s.closeFinishedRecords),
		mech(stepDeployAfterShip, s.deployAfterShip),
		mech(stepRetryFailedInfra, s.retryFailedInfra),
		mech(stepCleanupSweep, s.cleanupSweep),
		mech(stepApplyAnswers, s.applyAnswers),
		mech(stepLaneCount, s.laneCount),
		mech(stepStartNextChainStep, s.startNextChainStep),
		mech(stepRunMergeUntilConflict, s.runMergeUntilConflict),
	}
}

// RunPass runs every enabled step once, in order. A step that returns an
// error has it recorded in its run record and in the result; it is not run
// again in this pass. A disabled step is not run and writes no run record.
func (s *SupervisorLoopSteps) RunPass(ctx context.Context) []SupervisorLoopStepResult {
	if s == nil {
		return nil
	}
	set := s.settings()
	if !set.Enabled {
		return nil
	}
	eng := *s.engine
	eng.Settings = set
	var out []SupervisorLoopStepResult
	for _, job := range s.jobs() {
		if !stepEnabled(set, job.Name) {
			out = append(out, SupervisorLoopStepResult{Step: job.Name})
			continue
		}
		if ctx.Err() != nil {
			break
		}
		detail, err := eng.Run(ctx, job, "")
		out = append(out, SupervisorLoopStepResult{Step: job.Name, Ran: true, Detail: detail, Err: err})
	}
	return out
}

// SetWakeRecord registers the record that counts scheduled passes. Nil
// stops counting.
func (s *SupervisorLoopSteps) SetWakeRecord(r *supervisorloop.WakeRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.wakes = r
	s.mu.Unlock()
}

// RunScheduled is the idle tick's entry: the scheduled check. While the event
// wake is off it runs a pass on every tick, as before. While
// supervisor_loop.wake.enabled is on, events run the pass at once and this
// check is the fallback: it runs a pass only when
// supervisor_loop.wake.fallback_interval_minutes have passed since the last
// scheduled pass. Each scheduled pass that runs is recorded in the wake record.
// It reports whether a pass ran.
func (s *SupervisorLoopSteps) RunScheduled(ctx context.Context) bool {
	if s == nil {
		return false
	}
	set := s.settings()
	if !set.Enabled {
		return false
	}
	now := s.now()
	s.mu.Lock()
	if set.Wake.Enabled {
		interval := time.Duration(set.Wake.FallbackIntervalMinutes) * time.Minute
		if !s.lastScheduled.IsZero() && now.Sub(s.lastScheduled) < interval {
			s.mu.Unlock()
			return false
		}
	}
	s.lastScheduled = now
	rec := s.wakes
	s.mu.Unlock()
	s.RunPass(ctx)
	if rec != nil {
		rec.MarkScheduledCheck()
	}
	return true
}

// supervisorLoopPass is the idle tick's one pass over the registered steps.
func (c *Coordinator) supervisorLoopPass(ctx context.Context) {
	if c == nil {
		return
	}
	c.mu.Lock()
	steps := c.slSteps
	c.mu.Unlock()
	steps.RunScheduled(ctx)
}

// SetSupervisorLoopSteps registers the steps the idle tick runs. Nil turns
// the pass off.
func (c *Coordinator) SetSupervisorLoopSteps(s *SupervisorLoopSteps) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.slSteps = s
	c.mu.Unlock()
}

// closeFinishedRecords closes every Automaton that reached a terminal state.
// Existing function: Manager.Archive (manager.go), which moves a completed,
// rejected or cancelled record to archived through the store and emits the
// lifecycle status event. The tasks live inside the Automaton record and
// close with it.
//
// Completed and rejected records are archived in the pass, as before. A
// cancelled record earns a grace first: it is archived only once the time
// since its most recent cancel reaches the autonomous.archive_grace_minutes
// setting (default 60 when unset; 0 archives it at once). The older
// supervisor_loop.archive_cancelled_after_minutes key is still accepted on
// every surface so existing configurations and callers keep working, but it no
// longer drives this step. The grace is so a cancelled branch can
// still be inspected, or re-cancelled to push it back, before it disappears.
func (s *SupervisorLoopSteps) closeFinishedRecords(_ context.Context) (string, error) {
	var errs []error
	closed := 0
	held := 0
	kept := 0
	now := s.now()
	grace := s.archiveGraceMinutes()
	// Read status and cancel time under the store lock: the executor and the
	// cancel path mutate these shared records.
	type fin struct {
		p         *PRD
		status    PRDStatus
		cancelled time.Time
	}
	var fins []fin
	st := s.c.m.Store()
	prds := st.ListPRDs()
	st.WithLock(func() {
		for _, p := range prds {
			switch p.Status {
			case PRDCompleted, PRDRejected, PRDCancelled:
				f := fin{p: p, status: p.Status}
				if p.Status == PRDCancelled {
					f.cancelled = s.cancelTime(p, now)
				}
				fins = append(fins, f)
			}
		}
	})
	live := map[string]bool{}
	for _, f := range fins {
		p := f.p
		if f.status == PRDCancelled {
			live[p.ID] = true
		}
		// The grace only shelters cancelled records; completed and rejected
		// close in the same pass they reached their terminal state.
		if f.status == PRDCancelled && grace > 0 && now.Sub(f.cancelled) < time.Duration(grace)*time.Minute {
			held++
			continue
		}
		// A cancelled record past its grace is still kept while its branch
		// holds commits that are not on the base branch.
		if f.status == PRDCancelled && s.archiveBranchCheck != nil && s.archiveBranchCheck(p) {
			kept++
			continue
		}
		if _, err := s.c.m.Archive(p.ID, supervisorLoopActor); err != nil {
			errs = append(errs, fmt.Errorf("archive %s: %w", p.ID, err))
			continue
		}
		closed++
	}
	// Forget first-seen times of records no longer cancelled, so a later
	// cancel of the same record starts a fresh grace.
	s.mu.Lock()
	for id := range s.cancelSeen {
		if !live[id] {
			delete(s.cancelSeen, id)
		}
	}
	s.mu.Unlock()
	detail := fmt.Sprintf("closed %d finished records", closed)
	if held > 0 {
		detail += fmt.Sprintf(" (held %d cancelled within the %d-minute grace)", held, grace)
	}
	if kept > 0 {
		detail += fmt.Sprintf(" (kept %d cancelled with unmerged branch work)", kept)
	}
	return detail, errors.Join(errs...)
}

// archiveGraceMinutes reads autonomous.archive_grace_minutes live from the
// manager's config. Unset is the 60-minute default, never zero, so a cancelled
// record is not archived early just because nothing was configured.
func (s *SupervisorLoopSteps) archiveGraceMinutes() int {
	if s == nil || s.c == nil || s.c.m == nil {
		return config.DefaultArchiveGraceMinutes
	}
	cfg := s.c.m.Config()
	if cfg.ArchiveGraceMinutes == nil || *cfg.ArchiveGraceMinutes < 0 {
		return config.DefaultArchiveGraceMinutes
	}
	return *cfg.ArchiveGraceMinutes
}

// cancelTime is when the grace for a cancelled record starts running: the
// timestamp of its latest cancel decision. A cancelled record with no cancel
// decision has no trustworthy cancel time (its update time may be hours old),
// so it earns a full grace from the first pass that sees it cancelled. Call
// with the store lock held.
func (s *SupervisorLoopSteps) cancelTime(p *PRD, now time.Time) time.Time {
	var t time.Time
	for _, d := range p.Decisions {
		if d.Kind == "cancel" && d.At.After(t) {
			t = d.At
		}
	}
	if !t.IsZero() {
		return t
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if first, ok := s.cancelSeen[p.ID]; ok {
		return first
	}
	s.cancelSeen[p.ID] = now
	return now
}

// deployAfterShip runs the operator's deploy step once per newly shipped
// release. The deploy step is the hook (mergepipeline ShipRunner.Deploy); the
// shipped release is read from the Automaton record that RecordPRDShipped
// (shipped.go) wrote. With no hook the step records "not configured" and does
// nothing.
func (s *SupervisorLoopSteps) deployAfterShip(ctx context.Context) (string, error) {
	if s.hooks.Deploy == nil {
		return stepNotConfigured, nil
	}
	release := s.newestShippedRelease()
	s.mu.Lock()
	seen := s.lastDeployed
	s.mu.Unlock()
	if release == "" || release == seen {
		return "nothing newly shipped", nil
	}
	detail, err := s.hooks.Deploy(ctx)
	if errors.Is(err, ErrSupervisorStepNotConfigured) {
		return stepNotConfigured, nil
	}
	if err != nil {
		return "", fmt.Errorf("deploy %s: %w", release, err)
	}
	s.mu.Lock()
	s.lastDeployed = release
	s.mu.Unlock()
	return fmt.Sprintf("deployed %s: %s", release, detail), nil
}

// newestShippedRelease is the release named by the most recently merged
// shipped Automaton.
func (s *SupervisorLoopSteps) newestShippedRelease() string {
	var best *PRD
	for _, p := range s.c.m.Store().ListPRDs() {
		if strings.TrimSpace(p.MergedInRelease) == "" || p.MergedAt == nil {
			continue
		}
		if best == nil || p.MergedAt.After(*best.MergedAt) {
			best = p
		}
	}
	if best == nil {
		return ""
	}
	return best.MergedInRelease
}

// retryFailedInfra re-runs tasks held after infrastructure retries ran out.
// Existing function: Manager.ResolveTaskReview (failure_escalation_review.go)
// with the retry action, the same call an operator's retry answer makes
// (resolveInfraHoldAnswer). Each hold is retried once by the loop; a task that
// is held again stays for the operator.
func (s *SupervisorLoopSteps) retryFailedInfra(_ context.Context) (string, error) {
	type held struct{ prd, task string }
	var holds []held
	st := s.c.m.Store()
	prds := st.ListPRDs()
	st.WithLock(func() {
		for _, prd := range prds {
			for i := range prd.Story {
				for j := range prd.Story[i].Tasks {
					t := &prd.Story[i].Tasks[j]
					if t.Status == TaskReviewNeeded && t.FailureReason != nil && t.FailureReason.Source == infraRetryHoldSource {
						holds = append(holds, held{prd.ID, t.ID})
					}
				}
			}
		}
	})
	live := map[string]bool{}
	var errs []error
	retried := 0
	for _, h := range holds {
		key := h.prd + "/" + h.task
		live[key] = true
		s.mu.Lock()
		done := s.infraRetried[key]
		s.infraRetried[key] = true
		s.mu.Unlock()
		if done {
			continue
		}
		req := ReviewResolution{Action: reviewActionRetry, Note: "retried by the supervisor loop after an infrastructure hold"}
		if _, err := s.c.m.ResolveTaskReview(h.prd, h.task, req, supervisorLoopActor); err != nil {
			if !errors.Is(err, ErrTaskNotInReview) {
				errs = append(errs, fmt.Errorf("retry %s: %w", key, err))
			}
			continue
		}
		retried++
	}
	s.mu.Lock()
	for k := range s.infraRetried {
		if !live[k] {
			delete(s.infraRetried, k) // no longer held: a later hold is a new one
		}
	}
	s.mu.Unlock()
	return fmt.Sprintf("retried %d of %d held tasks", retried, len(holds)), errors.Join(errs...)
}

// cleanupSweep runs the cleanup status sweep. Existing functions:
// Manager.CleanupDiskPass, which takes the disk readings and updates the
// cleanup status, and Manager.alertCleanupDisk when the pass says the alert is
// due (cleanup_status.go; the coordinator's triage pass calls the same pair).
func (s *SupervisorLoopSteps) cleanupSweep(_ context.Context) (string, error) {
	due, st := s.c.m.CleanupDiskPass()
	if due {
		s.c.m.alertCleanupDisk(st)
		return "cleanup status updated; disk alert raised", nil
	}
	return "cleanup status updated", nil
}

// applyAnswers applies the answers the operator already recorded in the
// question queue to the held work waiting on them. Existing functions:
// ReadQuestionsForTick (question_tick.go) reads the answered questions and
// Manager.ProcessHeldAnswer (held_question_answer.go) routes each answer. The
// second is idempotent, so an answer already applied is not applied twice.
func (s *SupervisorLoopSteps) applyAnswers(ctx context.Context) (string, error) {
	store, _ := s.c.m.operatorQuestionChannel()
	if store == nil {
		return "no question queue wired", nil
	}
	var errs []error
	seen := 0
	for _, q := range ReadQuestionsForTick(store, QuestionAnswered) {
		if !isHeldQuestion(&q) {
			continue
		}
		ans, ok := store.CurrentAnswer(q.ID)
		if !ok {
			continue
		}
		seen++
		if err := s.c.m.ProcessHeldAnswer(ctx, q, ans); err != nil && !errors.Is(err, ErrHeldStateChanged) {
			errs = append(errs, fmt.Errorf("answer to %s: %w", q.ID, err))
		}
	}
	return fmt.Sprintf("checked %d answered held questions", seen), errors.Join(errs...)
}

// laneCount moves the running lane count toward the wanted count, never
// outside the operator's bounds: a wanted count outside them is clamped to the
// nearest bound.
func (s *SupervisorLoopSteps) laneCount(_ context.Context) (string, error) {
	lc := s.hooks.Lanes
	if lc == nil {
		return stepNotConfigured, nil
	}
	set := s.settings()
	if set.LaneMax <= 0 {
		return stepNotConfigured + ": no supervisor_loop.lane_count bounds set", nil
	}
	return applyLaneCount(lc, set.LaneMin, set.LaneMax)
}

// applyLaneCount is the clamp: it returns the account of what it did.
func applyLaneCount(lc SupervisorLaneControl, lo, hi int) (string, error) {
	if lo < 0 || hi < lo {
		return "", fmt.Errorf("lane bounds %d..%d are invalid", lo, hi)
	}
	want := lc.Wanted()
	target := want
	if target < lo {
		target = lo
	}
	if target > hi {
		target = hi
	}
	cur := lc.Current()
	note := ""
	if target != want {
		note = fmt.Sprintf(" (wanted %d, clamped to bounds %d..%d)", want, lo, hi)
	}
	if target == cur {
		return fmt.Sprintf("lane count stays %d%s", cur, note), nil
	}
	if err := lc.SetCount(target); err != nil {
		return "", fmt.Errorf("set lane count %d: %w", target, err)
	}
	return fmt.Sprintf("lane count moved %d -> %d%s", cur, target, note), nil
}

// startNextChainStep advances every active Automation chain whose next step is
// waiting to start. Existing function: Coordinator.startPendingCursors
// (automation_budget_gate.go), which goes through Coordinator.advance and the
// same budget gate as a member ending.
func (s *SupervisorLoopSteps) startNextChainStep(_ context.Context) (string, error) {
	n := 0
	for _, a := range s.c.m.automations.List() {
		if a.Status != AutomationActive {
			continue
		}
		waiting := false
		for _, id := range liveCursorIDs(a) {
			if mem := automationMember(a, id); mem != nil && memberUnstarted(mem) {
				waiting = true
				break
			}
		}
		if !waiting {
			continue
		}
		s.c.startPendingCursors(a)
		n++
	}
	return fmt.Sprintf("advanced %d chains", n), nil
}

// runMergeUntilConflict runs the merge pipeline until it reports a conflict.
// Existing function: mergepipeline.Runner.Run, reached through the hook. On a
// conflict the step stops, remembers the branch and records the conflict in
// its run detail; a conflict is an outcome, not a step error.
func (s *SupervisorLoopSteps) runMergeUntilConflict(ctx context.Context) (string, error) {
	if s.hooks.MergeUntilConflict == nil {
		return stepNotConfigured, nil
	}
	res, err := s.hooks.MergeUntilConflict(ctx)
	if errors.Is(err, ErrSupervisorStepNotConfigured) {
		return stepNotConfigured, nil
	}
	if err != nil && res.Conflict == "" {
		return "", fmt.Errorf("merge pipeline: %w", err)
	}
	if res.Conflict != "" {
		detail := fmt.Sprintf("conflict on %s, stopped after merging %d", res.Conflict, len(res.Merged))
		if res.Note != "" {
			detail += ": " + res.Note
		}
		s.mu.Lock()
		s.lastConflict = detail
		s.mu.Unlock()
		return detail, nil
	}
	return fmt.Sprintf("merged %d branches, no conflict", len(res.Merged)), nil
}

// supervisorLoopEscalator raises an escalation as a question in the operator
// question queue, carrying the attempt summary.
type supervisorLoopEscalator struct{ m *Manager }

// SupervisorLoopEscalator is the daemon's supervisorloop.Escalator: an
// escalation becomes one open question in the question queue
// (questions.go) whose text is the attempt summary.
func (m *Manager) SupervisorLoopEscalator() supervisorloop.Escalator {
	return supervisorLoopEscalator{m: m}
}

const supervisorLoopQuestionAsker = "supervisor-loop"

// Escalate raises the question. An escalation for the same job and attempts
// while one is still open raises nothing new.
func (e supervisorLoopEscalator) Escalate(_ context.Context, esc supervisorloop.Escalation) error {
	if e.m == nil {
		return errors.New("no manager")
	}
	store, _ := e.m.operatorQuestionChannel()
	if store == nil {
		return errors.New("no question queue wired")
	}
	text := esc.Summary()
	sum := sha256.Sum256([]byte(esc.Job + "\x00" + text))
	body := fmt.Sprintf("The supervisor loop could not finish %s and needs you. %s", esc.Job, text)
	if len(body) > questionTextMaxBytes {
		body = truncateUTF8(body, questionTextMaxBytes)
	}
	q := Question{
		AskedBy:   supervisorLoopQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextNone,
		Text:      body,
		Options: []QuestionOption{
			{ID: "acknowledge", Label: "Acknowledge", Tradeoffs: "Records that you saw it; the loop tries the job again next time."},
			{ID: "hold_job", Label: "Hold the job", Tradeoffs: "Leave the job alone until you change its settings."},
		},
		Default:          "acknowledge",
		Priority:         QuestionPriorityHigh,
		SourceLineSHA256: hex.EncodeToString(sum[:]),
	}
	if _, _, err := store.RaiseUnlessOpenSourceLine(q); err != nil {
		return fmt.Errorf("raise escalation question: %w", err)
	}
	return nil
}

// truncateUTF8 cuts s to at most max bytes without splitting a multi-byte
// character.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// RunStep runs the named mechanical step once now, through the engine so the
// run is recorded. It runs whatever the step's enabled setting says: the
// operator asked for this run. found is false for a name that is no step.
func (s *SupervisorLoopSteps) RunStep(ctx context.Context, name string) (detail string, found bool, err error) {
	if s == nil {
		return "", false, nil
	}
	eng := *s.engine
	eng.Settings = s.settings()
	for _, job := range s.jobs() {
		if job.Name == name {
			detail, err = eng.Run(ctx, job, "")
			return detail, true, err
		}
	}
	return "", false, nil
}
