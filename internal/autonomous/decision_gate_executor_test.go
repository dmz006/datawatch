package autonomous

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// The decision gate in the executor: a task whose spec names an operator
// decision that is not answered does not start. Every scenario runs against
// the fakes of the failure escalation tests, once with a single task slot (the
// sequential walk) and once with two (the concurrent pool). Fakes are
// coordinated by polling state, never by fixed sleeps; decisionWaitPoll stays
// at its default, so a task that starts within the wait was started by the
// answer waking the executor and not by the poll.

const (
	gateFreeID     = "gate-1-free"
	gateHeldID     = "gate-2-held"
	gateFollowerID = "gate-3-follower"

	gateWait = 10 * time.Second
	// gateAnswerStart is the longest a task may take to start once its
	// decision is answered. The poll interval is thirty seconds.
	gateAnswerStart = 5 * time.Second
)

var gateModes = []liveReviewMode{
	{name: "sequential", concurrency: 1},
	{name: "concurrent", concurrency: 2},
}

// decisionGateRig is one Automaton run against the fakes: a free task, a
// held task whose spec names a decision, and a follower that depends on the
// held task.
type decisionGateRig struct {
	m        *Manager
	api      *API
	store    *QuestionStore
	prdID    string
	decision string
	spawns   escalationSpawnLog
	alerts   atomic.Int32

	mu  sync.Mutex
	cfg config.AutonomousQuestionsConfig
}

type decisionGateOpts struct {
	mode        liveReviewMode
	gateMode    string
	gateDisable bool
	// spec is the held task's spec; %s is replaced by the decision id. Empty
	// uses a spec that names the decision.
	spec string
	// unknownID names a decision the store does not hold instead of a raised one.
	unknownID bool
}

const gateUnknownID = "q-0123456789abcdef0123456789abcdef"

func newDecisionGateRig(t *testing.T, o decisionGateOpts) *decisionGateRig {
	t.Helper()
	m, api := newFailureEscalationFixture(t, t.TempDir())
	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	r := &decisionGateRig{m: m, api: api, store: s}
	r.cfg.DecisionGateMode = o.gateMode
	if o.gateDisable {
		off := false
		r.cfg.DecisionGate = &off
	}
	// The change hook feeds the Manager and the API, as the daemon wires it.
	s.SetChangeHook(func(c QuestionChange) {
		m.RecordQuestionChange(c)
		api.OnQuestionChange(c)
	})
	m.SetDecisionGate(s, func() config.AutonomousQuestionsConfig {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.cfg
	})
	m.SetAlertSink(func(_, _, _ string) string {
		r.alerts.Add(1)
		return ""
	})

	r.decision = gateUnknownID
	if !o.unknownID {
		r.decision = mustRaise(t, s, sampleQuestion("Which way should the held task go?"))
	}
	spec := o.spec
	if spec == "" {
		spec = "do the held task once %s is answered"
	}
	spec = strings.ReplaceAll(spec, "%s", r.decision)
	task := func(id, title, spec string, deps ...string) Task {
		return Task{ID: id, Title: title, Spec: spec, ProjectDir: t.TempDir(), DependsOn: deps}
	}
	tasks := []Task{
		task(gateFreeID, "Free task", "do the free task"),
		task(gateHeldID, "Held task", spec),
		task(gateFollowerID, "Follower task", "do the follower task", "Held task"),
	}
	r.prdID = newHoldPRD(t, m, tasks, func(p *PRD) { p.MaxConcurrentTasks = o.mode.concurrency }).ID
	api.SetExecutors(r.spawns.spawn, func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	})
	return r
}

func (r *decisionGateRig) task(t *testing.T, id string) Task {
	t.Helper()
	p, ok := r.m.Store().SnapshotPRD(r.prdID)
	if !ok {
		t.Fatalf("Automaton %s not found", r.prdID)
	}
	for _, s := range p.Story {
		for _, tk := range s.Tasks {
			if tk.ID == id {
				return tk
			}
		}
	}
	t.Fatalf("task %s not found", id)
	return Task{}
}

func (r *decisionGateRig) status(t *testing.T, id string) TaskStatus {
	t.Helper()
	return normTaskStatus(r.task(t, id).Status)
}

func (r *decisionGateRig) prdStatus(t *testing.T) PRDStatus {
	t.Helper()
	p, ok := r.m.Store().SnapshotPRD(r.prdID)
	if !ok {
		t.Fatalf("Automaton %s not found", r.prdID)
	}
	return p.Status
}

func (r *decisionGateRig) live() bool {
	r.api.runMu.Lock()
	defer r.api.runMu.Unlock()
	_, live := r.api.runCancels[r.prdID]
	return live
}

// spawned reports whether the task's worker was started.
func (r *decisionGateRig) spawned(taskID string) bool {
	r.spawns.mu.Lock()
	defer r.spawns.mu.Unlock()
	for _, req := range r.spawns.reqs {
		if req.TaskID == taskID {
			return true
		}
	}
	return false
}

// runDecisions counts the run decisions on the Automaton: one per Manager.Run.
func (r *decisionGateRig) runDecisions(t *testing.T) int {
	t.Helper()
	p, _ := r.m.Store().SnapshotPRD(r.prdID)
	n := 0
	for _, d := range p.Decisions {
		if d.Kind == "run" {
			n++
		}
	}
	return n
}

func (r *decisionGateRig) decisionsOfKind(t *testing.T, kind string) []Decision {
	t.Helper()
	p, _ := r.m.Store().SnapshotPRD(r.prdID)
	var out []Decision
	for _, d := range p.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

// start runs the Automaton through the API and waits until the free task has
// finished and the held task is parked, which is where the executor waits.
func (r *decisionGateRig) startAndWaitParked(t *testing.T) {
	t.Helper()
	if err := r.api.Run(r.prdID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollUntil(t, gateWait, "free task completed and held task parked", func() bool {
		return r.status(t, gateFreeID) == TaskCompleted && r.status(t, gateHeldID) == TaskWaitingDecision
	})
}

// A task whose spec names an open decision is not started: it is parked with
// the reason, its dependent does not start, and the Automaton stays running.
func TestDecisionGateExecutorHoldsTaskWithOpenDecision(t *testing.T) {
	for _, mode := range gateModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDecisionGateRig(t, decisionGateOpts{mode: mode, gateMode: config.DecisionGateModeOn})
			r.startAndWaitParked(t)

			held := r.task(t, gateHeldID)
			want := "operator decision " + r.decision + " is waiting for an answer: Which way should the held task go?"
			if held.WaitReason != want {
				t.Errorf("wait reason = %q; want %q", held.WaitReason, want)
			}
			if r.spawned(gateHeldID) {
				t.Error("the held task's worker was started")
			}
			if got := r.status(t, gateFollowerID); got != TaskPending {
				t.Errorf("follower status = %s; want pending", got)
			}
			if r.spawned(gateFollowerID) {
				t.Error("the follower's worker was started behind a parked task")
			}
			// The Automaton is neither completed nor failed, and its
			// executor is still alive, waiting for the answer.
			if got := r.prdStatus(t); got != PRDRunning {
				t.Errorf("Automaton status = %s; want running", got)
			}
			if !r.live() {
				t.Error("the executor left while a task waits on a decision")
			}
			// The alert goes out just after the task parks, so wait for it
			// rather than reading the count the moment the status flips.
			pollUntil(t, gateWait, "the hold alert", func() bool { return r.alerts.Load() >= 1 })
			if got := r.alerts.Load(); got != 1 {
				t.Errorf("alerts = %d; want 1", got)
			}
		})
	}
}

// Answering the decision starts the task as soon as it is answered, with no
// second Manager.Run: the run is the one that was waiting.
func TestDecisionGateExecutorAnswerStartsTaskWithoutSecondRun(t *testing.T) {
	for _, mode := range gateModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDecisionGateRig(t, decisionGateOpts{mode: mode, gateMode: config.DecisionGateModeOn})
			r.startAndWaitParked(t)
			if n := r.runDecisions(t); n != 1 {
				t.Fatalf("run decisions before the answer = %d; want 1", n)
			}

			begun := time.Now()
			if _, err := r.store.Answer(r.decision, Answer{Quote: "go first"}); err != nil {
				t.Fatalf("Answer: %v", err)
			}
			pollUntil(t, gateAnswerStart, "held task started", func() bool { return r.spawned(gateHeldID) })
			if took := time.Since(begun); took >= gateAnswerStart {
				t.Errorf("task started %s after the answer; want under %s", took, gateAnswerStart)
			}
			pollUntil(t, gateWait, "Automaton completed", func() bool { return r.prdStatus(t) == PRDCompleted })

			if n := r.runDecisions(t); n != 1 {
				t.Errorf("run decisions after the answer = %d; want 1 (no second Run)", n)
			}
			for _, id := range []string{gateFreeID, gateHeldID, gateFollowerID} {
				if got := r.status(t, id); got != TaskCompleted {
					t.Errorf("task %s status = %s; want completed", id, got)
				}
			}
			if reason := r.task(t, gateHeldID).WaitReason; reason != "" {
				t.Errorf("wait reason after release = %q; want empty", reason)
			}
			if !r.spawned(gateFollowerID) {
				t.Error("the follower never started after its dependency ran")
			}
		})
	}
}

// A decision withdrawn without an answer keeps the task parked, and the
// reason says so. The same holds for an id the store does not know.
func TestDecisionGateExecutorWithdrawnOrUnknownKeepsHeld(t *testing.T) {
	for _, mode := range gateModes {
		t.Run(mode.name+"/withdrawn", func(t *testing.T) {
			r := newDecisionGateRig(t, decisionGateOpts{mode: mode, gateMode: config.DecisionGateModeOn})
			r.startAndWaitParked(t)
			if _, err := r.store.Withdraw(r.decision, "operator", "no longer needed"); err != nil {
				t.Fatalf("Withdraw: %v", err)
			}
			// A withdrawal does not wake the executor; nudge it so the
			// scan runs, then expect the reason to follow the new state.
			r.api.reviewWake.signal(r.prdID)
			want := "operator decision " + r.decision + " was withdrawn without an answer"
			pollUntil(t, gateWait, "wait reason names the withdrawal", func() bool {
				return r.task(t, gateHeldID).WaitReason == want
			})
			if got := r.status(t, gateHeldID); got != TaskWaitingDecision {
				t.Errorf("held task status = %s; want waiting_decision", got)
			}
			if r.spawned(gateHeldID) {
				t.Error("the held task started after its decision was withdrawn")
			}
		})
		t.Run(mode.name+"/unknown", func(t *testing.T) {
			r := newDecisionGateRig(t, decisionGateOpts{mode: mode, gateMode: config.DecisionGateModeOn, unknownID: true})
			r.startAndWaitParked(t)
			want := "operator decision " + gateUnknownID + " was not found"
			if got := r.task(t, gateHeldID).WaitReason; got != want {
				t.Errorf("wait reason = %q; want %q", got, want)
			}
			if r.spawned(gateHeldID) {
				t.Error("the held task started for an unknown decision")
			}
		})
	}
}

// Mode shadow runs the task and records one decision_gate_shadow entry; it
// parks nothing and raises no alert.
func TestDecisionGateExecutorShadowRunsAndRecordsOnce(t *testing.T) {
	for _, mode := range gateModes {
		t.Run(mode.name, func(t *testing.T) {
			// An empty mode is shadow.
			r := newDecisionGateRig(t, decisionGateOpts{mode: mode})
			if err := r.api.Run(r.prdID); err != nil {
				t.Fatalf("Run: %v", err)
			}
			pollUntil(t, gateWait, "Automaton completed", func() bool { return r.prdStatus(t) == PRDCompleted })
			if !r.spawned(gateHeldID) {
				t.Error("shadow mode did not run the task")
			}
			got := r.decisionsOfKind(t, "decision_gate_shadow")
			if len(got) != 1 {
				t.Fatalf("shadow entries = %d; want 1: %+v", len(got), got)
			}
			d := got[0]
			if !strings.HasPrefix(d.Note, "would hold: operator decision "+r.decision+" is waiting for an answer") {
				t.Errorf("note = %q; want it to start with the would-hold reason", d.Note)
			}
			if d.QuestionID != r.decision || d.DecisionState != "open" {
				t.Errorf("entry question/state = %q/%q; want %s/open", d.QuestionID, d.DecisionState, r.decision)
			}
			if r.alerts.Load() != 0 {
				t.Errorf("alerts = %d; want none in shadow mode", r.alerts.Load())
			}
		})
	}
}

// Scanning the same task with the same reasons again records no second entry.
func TestDecisionGateExecutorShadowDeduplicates(t *testing.T) {
	r := newDecisionGateRig(t, decisionGateOpts{mode: gateModes[0]})
	prd, _ := r.m.Store().GetPRD(r.prdID)
	held := lookupTask(prd, gateHeldID)
	other := lookupTask(prd, gateFollowerID)
	other.Spec = "also waits on " + r.decision
	for i := 0; i < 4; i++ {
		if r.m.decisionGateHold(prd, held) {
			t.Fatal("shadow mode held the task")
		}
	}
	if got := len(r.decisionsOfKind(t, "decision_gate_shadow")); got != 1 {
		t.Fatalf("entries after repeated scans = %d; want 1", got)
	}
	// A second task naming the same decision is its own record.
	r.m.decisionGateHold(prd, other)
	if got := len(r.decisionsOfKind(t, "decision_gate_shadow")); got != 2 {
		t.Fatalf("entries with a second task = %d; want 2", got)
	}
	// A new state of the same decision is a new record.
	if _, err := r.store.Withdraw(r.decision, "operator", "dropped"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	r.m.decisionGateHold(prd, held)
	if got := len(r.decisionsOfKind(t, "decision_gate_shadow")); got != 3 {
		t.Fatalf("entries after the state changed = %d; want 3", got)
	}
}

// With decision_gate false the task runs even when its decision is open and
// the mode is on.
func TestDecisionGateExecutorDisabledRunsTask(t *testing.T) {
	for _, mode := range gateModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDecisionGateRig(t, decisionGateOpts{mode: mode, gateMode: config.DecisionGateModeOn, gateDisable: true})
			if err := r.api.Run(r.prdID); err != nil {
				t.Fatalf("Run: %v", err)
			}
			pollUntil(t, gateWait, "Automaton completed", func() bool { return r.prdStatus(t) == PRDCompleted })
			if !r.spawned(gateHeldID) {
				t.Error("the task did not run with the gate off")
			}
			if got := len(r.decisionsOfKind(t, "decision_gate_shadow")); got != 0 {
				t.Errorf("shadow entries = %d; want none with the gate off", got)
			}
		})
	}
}

// The text HOLD line is a separate rule and reads exactly as before: it
// holds the Automaton whatever the decision gate says, and a spec that names
// an answered decision adds no hold of its own.
func TestDecisionGateExecutorTextHoldLineUnchanged(t *testing.T) {
	r := newDecisionGateRig(t, decisionGateOpts{mode: gateModes[0], gateMode: config.DecisionGateModeOn})
	prd, _ := r.m.Store().GetPRD(r.prdID)
	if got := PRDHoldReason(prd, nil); got != "" {
		t.Fatalf("hold reason with an open decision in a task spec = %q; want none", got)
	}
	r.m.Store().WithLock(func() { prd.Spec = "HOLD until the review lands\nthen run" })
	if got := PRDHoldReason(prd, nil); got != "a line starts with HOLD" {
		t.Errorf("hold reason = %q; want %q", got, "a line starts with HOLD")
	}
	if _, err := r.store.Answer(r.decision, Answer{Quote: "go first"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got := PRDHoldReason(prd, nil); got != "a line starts with HOLD" {
		t.Errorf("hold reason after the answer = %q; want the HOLD line to hold as before", got)
	}
}

// A task already running, completed, failed or cancelled is never checked,
// whatever its spec names.
func TestDecisionGateExecutorOnlyChecksStartableTasks(t *testing.T) {
	r := newDecisionGateRig(t, decisionGateOpts{mode: gateModes[0], gateMode: config.DecisionGateModeOn})
	prd, _ := r.m.Store().GetPRD(r.prdID)
	held := lookupTask(prd, gateHeldID)
	for _, st := range []TaskStatus{TaskInProgress, TaskRunningTests, TaskVerifying, TaskCompleted, TaskFailed, TaskCancelled} {
		r.m.Store().WithLock(func() { held.Status = st })
		if r.m.decisionGateHold(prd, held) {
			t.Errorf("status %s was held", st)
		}
		if held.Status != st {
			t.Errorf("status %s was changed to %s", st, held.Status)
		}
	}
	for _, st := range []TaskStatus{"", TaskPending, TaskQueued} {
		r.m.Store().WithLock(func() { held.Status = st })
		if !r.m.decisionGateHold(prd, held) || held.Status != TaskWaitingDecision {
			t.Errorf("status %q: held=%s; want waiting_decision", st, held.Status)
		}
		r.m.Store().WithLock(func() { held.Status = TaskPending; held.WaitReason = "" })
	}
}

// A parked task whose holds have cleared goes back to pending in the same
// scan with its wait reason cleared, and the gate being turned off does the
// same.
func TestDecisionGateExecutorReleasesParkedTask(t *testing.T) {
	r := newDecisionGateRig(t, decisionGateOpts{mode: gateModes[0], gateMode: config.DecisionGateModeOn})
	prd, _ := r.m.Store().GetPRD(r.prdID)
	held := lookupTask(prd, gateHeldID)
	if !r.m.decisionGateHold(prd, held) || held.Status != TaskWaitingDecision {
		t.Fatalf("task not parked: status %s", held.Status)
	}
	// Scanning again changes nothing and does not alert again.
	r.m.decisionGateHold(prd, held)
	if got := r.alerts.Load(); got != 1 {
		t.Errorf("alerts after two scans = %d; want 1", got)
	}
	if _, err := r.store.Answer(r.decision, Answer{Quote: "go first"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if r.m.decisionGateHold(prd, held) {
		t.Fatal("task still held after the answer")
	}
	if held.Status != TaskPending || held.WaitReason != "" {
		t.Errorf("released task = %s / %q; want pending with no reason", held.Status, held.WaitReason)
	}

	// Parked again, then the gate is switched off: the task is released.
	r.m.Store().WithLock(func() { held.Status = TaskWaitingDecision; held.WaitReason = "stale" })
	r.mu.Lock()
	off := false
	r.cfg.DecisionGate = &off
	r.mu.Unlock()
	r.m.Store().WithLock(func() { held.Spec = "waits on " + gateUnknownID })
	if r.m.decisionGateHold(prd, held) || held.Status != TaskPending {
		t.Errorf("task with the gate off = %s; want pending and not held", held.Status)
	}
}

// Several holds read as one sentence.
func TestDecisionGateExecutorReasonsJoinIntoOneSentence(t *testing.T) {
	r := newDecisionGateRig(t, decisionGateOpts{mode: gateModes[0], gateMode: config.DecisionGateModeOn})
	second := mustRaise(t, r.store, sampleQuestion("And the second thing?"))
	prd, _ := r.m.Store().GetPRD(r.prdID)
	held := lookupTask(prd, gateHeldID)
	r.m.Store().WithLock(func() { held.Spec = "needs " + r.decision + " and " + second })
	if !r.m.decisionGateHold(prd, held) {
		t.Fatal("task not held")
	}
	want := "operator decision " + r.decision + " is waiting for an answer: Which way should the held task go?; " +
		"operator decision " + second + " is waiting for an answer: And the second thing?"
	if held.WaitReason != want {
		t.Errorf("wait reason = %q; want %q", held.WaitReason, want)
	}
	// One answered: the reason follows, the task stays parked, no new alert.
	if _, err := r.store.Answer(r.decision, Answer{Quote: "go first"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if !r.m.decisionGateHold(prd, held) || !strings.HasPrefix(held.WaitReason, "operator decision "+second) {
		t.Errorf("after one answer: held=%s reason=%q", held.Status, held.WaitReason)
	}
	if got := r.alerts.Load(); got != 1 {
		t.Errorf("alerts = %d; want 1", got)
	}
}

// A Run with no live executor (no wake channel) does not wait: it leaves the
// Automaton running with the task parked and the reason visible. The answer
// then relaunches the run through the API, which starts the task.
func TestDecisionGateExecutorAnswerRelaunchesWhenNoRunLive(t *testing.T) {
	for _, mode := range gateModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDecisionGateRig(t, decisionGateOpts{mode: mode, gateMode: config.DecisionGateModeOn})
			// The boot-resume scan SetExecutors started must be over, or it
			// could relaunch the Automaton once Run leaves it running.
			if err := r.api.WaitIdle(gateWait); err != nil {
				t.Fatalf("WaitIdle: %v", err)
			}
			prd, _ := r.m.Store().GetPRD(r.prdID)
			if err := r.m.Run(context.Background(), prd.ID, r.spawns.spawn, func(context.Context, *PRD, *Task) (VerificationResult, error) {
				return VerificationResult{OK: true, Summary: "ok"}, nil
			}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := r.prdStatus(t); got != PRDRunning {
				t.Fatalf("Automaton status = %s; want running", got)
			}
			held := r.task(t, gateHeldID)
			if held.Status != TaskWaitingDecision || !strings.Contains(held.WaitReason, r.decision) {
				t.Fatalf("held task = %s / %q; want waiting_decision naming the decision", held.Status, held.WaitReason)
			}
			if r.live() {
				t.Fatal("an executor is live; the test wants none")
			}
			if _, err := r.store.Answer(r.decision, Answer{Quote: "go first"}); err != nil {
				t.Fatalf("Answer: %v", err)
			}
			pollUntil(t, gateWait, "Automaton completed", func() bool { return r.prdStatus(t) == PRDCompleted })
			if !r.spawned(gateHeldID) || !r.spawned(gateFollowerID) {
				t.Error("the answer did not start the held task and its follower")
			}
		})
	}
}

// An answer for a decision no task names does not start or wake anything.
func TestDecisionGateExecutorUnrelatedAnswerIgnored(t *testing.T) {
	r := newDecisionGateRig(t, decisionGateOpts{mode: gateModes[0], gateMode: config.DecisionGateModeOn})
	other := mustRaise(t, r.store, sampleQuestion("Something else entirely?"))
	if _, err := r.store.Answer(other, Answer{Quote: "go first"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if r.live() {
		t.Error("an unrelated answer started a run")
	}
}
