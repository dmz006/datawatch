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
//   - lane_count: the operator's lane bounds come from the lane control. With
//     no control the step records "not configured" and moves nothing.
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

// SupervisorLaneControl is the operator's lane control: the bounds the
// operator configured, the running lane count, the count the loop wants and a
// way to move it. The step never sets a count outside Bounds.
type SupervisorLaneControl interface {
	// Bounds returns the lowest and highest lane count the operator allows.
	Bounds() (lo, hi int)
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

	mu           sync.Mutex
	infraRetried map[string]bool // held tasks already retried by the loop
	lastDeployed string          // newest shipped release already deployed
	lastConflict string          // newest merge conflict recorded
}

// NewSupervisorLoopSteps registers the steps over the coordinator. engine
// supplies the clock, the run store and the escalator; its Settings are
// replaced from hooks.Settings on every pass.
func NewSupervisorLoopSteps(c *Coordinator, engine *supervisorloop.Engine, hooks SupervisorLoopHooks) *SupervisorLoopSteps {
	if c == nil || engine == nil {
		return nil
	}
	return &SupervisorLoopSteps{c: c, engine: engine, hooks: hooks, infraRetried: map[string]bool{}}
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

// supervisorLoopPass is the idle tick's one pass over the registered steps.
func (c *Coordinator) supervisorLoopPass(ctx context.Context) {
	if c == nil {
		return
	}
	c.mu.Lock()
	steps := c.slSteps
	c.mu.Unlock()
	steps.RunPass(ctx)
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
func (s *SupervisorLoopSteps) closeFinishedRecords(_ context.Context) (string, error) {
	var errs []error
	closed := 0
	for _, p := range s.c.m.Store().ListPRDs() {
		switch p.Status {
		case PRDCompleted, PRDRejected, PRDCancelled:
		default:
			continue
		}
		if _, err := s.c.m.Archive(p.ID); err != nil {
			errs = append(errs, fmt.Errorf("archive %s: %w", p.ID, err))
			continue
		}
		closed++
	}
	return fmt.Sprintf("closed %d finished records", closed), errors.Join(errs...)
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
	return applyLaneCount(lc)
}

// applyLaneCount is the clamp: it returns the account of what it did.
func applyLaneCount(lc SupervisorLaneControl) (string, error) {
	lo, hi := lc.Bounds()
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
		body = body[:questionTextMaxBytes]
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
