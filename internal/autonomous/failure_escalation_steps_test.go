package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// Valid backend and model pairs for the escalation ladder. The normal worker
// of the fixture PRD is opencode with no model, which is never model-checked.
const (
	escClaudeBackend = "claude-code"
	escClaudeModel   = "claude-haiku-5-5"
	escPiBackend     = "pi-prompt"
	escPiModel       = "anthropic/claude-haiku-5-5"
	escNormalWorker  = "opencode/"
)

// escalationSpawnLog records every spawn request a test run makes, in order.
type escalationSpawnLog struct {
	mu   sync.Mutex
	reqs []SpawnRequest
}

func (l *escalationSpawnLog) spawn(_ context.Context, req SpawnRequest) (SpawnResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reqs = append(l.reqs, req)
	return SpawnResult{SessionID: fmt.Sprintf("sess-%d", len(l.reqs))}, nil
}

// workers returns "backend/model" for each recorded spawn, in spawn order.
func (l *escalationSpawnLog) workers() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.reqs))
	for _, r := range l.reqs {
		out = append(out, r.Backend+"/"+r.Model)
	}
	return out
}

// alwaysFailVerify fails every attempt, so a task runs until its budget is spent.
func alwaysFailVerify(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
	return VerificationResult{OK: false, Summary: "tests still fail"}, nil
}

// runEscalationPRD runs prd to a terminal state with the given spawn log and
// verifier.
func runEscalationPRD(t *testing.T, m *Manager, api *API, prd *PRD, log *escalationSpawnLog, verify VerifyFn) {
	t.Helper()
	api.SetExecutors(log.spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollEscalationSettled(t, m, prd.ID, 10*time.Second)
}

// pollEscalationSettled waits until the Automaton is terminal, or blocked
// with a task held for operator review, which is where an exhausted task
// with escalation on leaves it.
func pollEscalationSettled(t *testing.T, m *Manager, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got, ok := m.Store().SnapshotPRD(id)
		if ok {
			if got.Status == PRDCompleted || got.Status == PRDFailed {
				return
			}
			if got.Status == PRDBlocked && m.reviewHeldTaskID(got) != "" {
				return
			}
		}
		if time.Now().After(deadline) {
			var status string
			var states []string
			if ok {
				status = string(got.Status)
				for si := range got.Story {
					for _, tk := range got.Story[si].Tasks {
						states = append(states, tk.Title+"="+string(tk.Status))
					}
				}
			}
			t.Fatalf("pollEscalationSettled: PRD %s not settled within %s (status %q, tasks %v)",
				id, timeout, status, states)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// escalationOn returns a pointer to v, for the Enabled field of a policy.
func escalationOn(v bool) *bool { return &v }

func escalationClaudeStep(attempts int) FailureEscalationStep {
	return FailureEscalationStep{Backend: escClaudeBackend, Model: escClaudeModel, Attempts: attempts}
}

func escalationPiStep(attempts int) FailureEscalationStep {
	return FailureEscalationStep{Backend: escPiBackend, Model: escPiModel, Attempts: attempts}
}

// escalationTask returns the single task of prd as stored.
func escalationTask(t *testing.T, m *Manager, prdID string) *Task {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s not found", prdID)
	}
	return &prd.Story[0].Tasks[0]
}

func TestFailureEscalation_SelectStep(t *testing.T) {
	ladder := func(steps ...FailureEscalationStep) EffectiveFailureEscalation {
		return EffectiveFailureEscalation{Enabled: true, MaxFailures: 9, Steps: steps}
	}
	tests := []struct {
		name     string
		eff      EffectiveFailureEscalation
		failures int
		wantIdx  int
		wantOK   bool
		wantBack string
	}{
		{"empty ladder uses the normal worker", ladder(), 0, 0, false, ""},
		{"first attempt starts on step 1", ladder(escalationClaudeStep(0), escalationPiStep(0)), 0, 0, true, escClaudeBackend},
		{"second attempt moves to step 2", ladder(escalationClaudeStep(0), escalationPiStep(0)), 1, 1, true, escPiBackend},
		{"last step covers every later attempt", ladder(escalationClaudeStep(0), escalationPiStep(0)), 5, 1, true, escPiBackend},
		{"step attempts keep the step in place", ladder(escalationClaudeStep(2), escalationPiStep(0)), 1, 0, true, escClaudeBackend},
		{"step attempts end where the count reaches them", ladder(escalationClaudeStep(2), escalationPiStep(0)), 2, 1, true, escPiBackend},
		{"empty step uses the normal worker", ladder(FailureEscalationStep{}, escalationPiStep(0)), 0, 0, false, ""},
		{"backend-only step is used with an empty model", ladder(FailureEscalationStep{Backend: escPiBackend}), 0, 0, true, escPiBackend},
		{"model without backend uses the normal worker", ladder(FailureEscalationStep{Model: escPiModel}), 0, 0, false, ""},
		{"negative count reads as the first attempt", ladder(escalationClaudeStep(0), escalationPiStep(0)), -3, 0, true, escClaudeBackend},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step, idx, ok := selectEscalationStep(tc.eff, tc.failures)
			if idx != tc.wantIdx || ok != tc.wantOK || step.Backend != tc.wantBack {
				t.Fatalf("selectEscalationStep(%d) = (%q, idx %d, ok %v), want (%q, idx %d, ok %v)",
					tc.failures, step.Backend, idx, ok, tc.wantBack, tc.wantIdx, tc.wantOK)
			}
		})
	}
}

// TestFailureEscalation_FailsOverToNextWorker runs a task whose every attempt
// fails. The first spawn runs on step 1 and the second on step 2, and the
// capacity ledger admits each attempt on the backend of its own step.
func TestFailureEscalation_FailsOverToNextWorker(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())

	var amu sync.Mutex
	var admitted []string
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("node:n1", 5)
	m.SetCapacity(led, func(backend, _ string) ([]string, string) {
		amu.Lock()
		defer amu.Unlock()
		admitted = append(admitted, backend)
		return []string{"node:n1"}, "n1"
	})

	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{Steps: []FailureEscalationStep{
			escalationClaudeStep(0), escalationPiStep(0),
		}}
	})
	var log escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &log, alwaysFailVerify)

	want := []string{escClaudeBackend + "/" + escClaudeModel, escPiBackend + "/" + escPiModel, escPiBackend + "/" + escPiModel}
	if got := log.workers(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("spawn workers = %v, want %v", got, want)
	}
	amu.Lock()
	seen := strings.Join(admitted, ",")
	amu.Unlock()
	if !strings.HasPrefix(seen, escClaudeBackend) || !strings.Contains(seen, escPiBackend) {
		t.Fatalf("capacity admission backends = %q, want claude-code first and pi-prompt after", seen)
	}

	task := escalationTask(t, m, prd.ID)
	if task.ExecutorSource != ExecutorSourceEscalation {
		t.Fatalf("ExecutorSource = %q, want %q", task.ExecutorSource, ExecutorSourceEscalation)
	}
	if task.Backend != "" || task.Model != "" {
		t.Fatalf("escalation must not write the task's own worker: backend=%q model=%q", task.Backend, task.Model)
	}
	if len(task.Attempts) != 3 || task.Attempts[0].Escalated != true || task.Attempts[0].Backend != escClaudeBackend {
		t.Fatalf("attempt records = %+v, want 3 with the first on claude-code and escalated", task.Attempts)
	}

	stored, _ := m.Store().GetPRD(prd.ID)
	var changes []Decision
	for _, d := range stored.Decisions {
		if d.Kind == "failure_escalation" {
			changes = append(changes, d)
		}
	}
	// Only the claude-code to pi-prompt change moves the worker; the second
	// failure stays on pi-prompt and writes no decision.
	if len(changes) != 1 {
		t.Fatalf("failure_escalation decisions = %d, want 1 (one per worker change)", len(changes))
	}
	if !strings.Contains(changes[0].Note, "attempt 1 failed on claude-code/"+escClaudeModel) ||
		!strings.Contains(changes[0].Note, "next attempt uses step 2: "+escPiBackend+"/"+escPiModel) {
		t.Fatalf("decision note = %q", changes[0].Note)
	}
	if changes[0].Actor != "autonomous" || changes[0].Backend != escPiBackend || changes[0].Model != escPiModel {
		t.Fatalf("decision actor/worker = %q %q/%q", changes[0].Actor, changes[0].Backend, changes[0].Model)
	}
}

// TestFailureEscalation_StepAttemptsRespected: step 1 covers two attempts, so
// it runs twice before step 2 takes over.
func TestFailureEscalation_StepAttemptsRespected(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			MaxFailures: 4,
			Steps:       []FailureEscalationStep{escalationClaudeStep(2), escalationPiStep(0)},
		}
	})
	var log escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &log, alwaysFailVerify)

	want := []string{
		escClaudeBackend + "/" + escClaudeModel, escClaudeBackend + "/" + escClaudeModel,
		escPiBackend + "/" + escPiModel, escPiBackend + "/" + escPiModel,
	}
	if got := log.workers(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("spawn workers = %v, want %v", got, want)
	}
}

// TestFailureEscalation_LastStepRepeats: once the ladder is exhausted, every
// remaining attempt stays on the last step.
func TestFailureEscalation_LastStepRepeats(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			MaxFailures: 4,
			Steps:       []FailureEscalationStep{escalationClaudeStep(1), escalationPiStep(1)},
		}
	})
	var log escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &log, alwaysFailVerify)

	want := []string{
		escClaudeBackend + "/" + escClaudeModel,
		escPiBackend + "/" + escPiModel,
		escPiBackend + "/" + escPiModel,
		escPiBackend + "/" + escPiModel,
	}
	if got := log.workers(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("spawn workers = %v, want %v", got, want)
	}
}

// TestFailureEscalation_EmptyStepUsesNormalWorker: a first step with no backend
// and no model means the first attempt runs on the normally resolved worker.
func TestFailureEscalation_EmptyStepUsesNormalWorker(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			MaxFailures: 3,
			Steps:       []FailureEscalationStep{{}, escalationPiStep(0)},
		}
	})
	var log escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &log, alwaysFailVerify)

	want := []string{escNormalWorker, escPiBackend + "/" + escPiModel, escPiBackend + "/" + escPiModel}
	if got := log.workers(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("spawn workers = %v, want %v", got, want)
	}
	task := escalationTask(t, m, prd.ID)
	if task.Attempts[0].Escalated {
		t.Fatalf("first attempt ran on the normal worker and must not be marked escalated")
	}
	if task.Attempts[1].Escalated != true {
		t.Fatalf("second attempt ran on a step and must be marked escalated")
	}
}

// TestFailureEscalation_TaskOverrideStepsBeatAutomaton: the task's own steps
// win over the Automaton's, while MaxFailures still comes from the Automaton
// because it is set only there.
func TestFailureEscalation_TaskOverrideStepsBeatAutomaton(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			MaxFailures: 4,
			Steps:       []FailureEscalationStep{escalationPiStep(0)},
		}
		p.Story[0].Tasks[0].FailureEscalation = &FailureEscalationPolicy{
			Steps: []FailureEscalationStep{escalationClaudeStep(0)},
		}
	})
	var log escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &log, alwaysFailVerify)

	got := log.workers()
	if len(got) != 4 {
		t.Fatalf("spawn count = %d, want 4 from the Automaton's max_failures: %v", len(got), got)
	}
	for i, w := range got {
		if w != escClaudeBackend+"/"+escClaudeModel {
			t.Fatalf("spawn %d worker = %q, want the task's step %q", i+1, w, escClaudeBackend+"/"+escClaudeModel)
		}
	}
}

// TestFailureEscalation_DisabledKeepsLegacyBound: with escalation off the task
// gets one attempt plus auto_fix_retries, all on the normal worker, and the
// ladder is ignored.
func TestFailureEscalation_DisabledKeepsLegacyBound(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			Enabled: escalationOn(false),
			Steps:   []FailureEscalationStep{escalationPiStep(0)},
		}
	})
	var log escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &log, alwaysFailVerify)

	// The fixture sets auto_fix_retries to 3, so the legacy bound is 4 attempts.
	got := log.workers()
	if len(got) != 4 {
		t.Fatalf("spawn count = %d, want 4 (retries + 1): %v", len(got), got)
	}
	for i, w := range got {
		if w != escNormalWorker {
			t.Fatalf("spawn %d worker = %q, want the normal worker with escalation disabled", i+1, w)
		}
	}
}

// TestFailureEscalation_InvalidStepFailsPreflight: a step pairing a bare
// Claude alias with pi-prompt is refused before the Automaton starts, and the
// error names the task and the step.
func TestFailureEscalation_InvalidStepFailsPreflight(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			Steps: []FailureEscalationStep{escalationClaudeStep(0), {Backend: escPiBackend, Model: escClaudeModel}},
		}
	})
	var log escalationSpawnLog
	err := m.Run(context.Background(), prd.ID, log.spawn, alwaysFailVerify)
	if err == nil {
		t.Fatalf("Run accepted an invalid escalation step")
	}
	if !errors.Is(err, ErrModelBackendMismatch) {
		t.Fatalf("Run error = %v, want ErrModelBackendMismatch", err)
	}
	task := escalationTask(t, m, prd.ID)
	for _, want := range []string{"task " + task.ID, "failure escalation step 2", escClaudeModel} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Run error %q missing %q", err.Error(), want)
		}
	}
	if n := len(log.workers()); n != 0 {
		t.Fatalf("spawned %d sessions after a refused preflight, want 0", n)
	}
}

// TestFailureEscalation_RestartContinuesAtRightStep fails the first attempt,
// then reads the stored PRD from a second store on the same directory while
// the second attempt is being spawned. The stored failure count must be 1 and
// the second attempt must run on step 2, which is what a restart would see.
func TestFailureEscalation_RestartContinuesAtRightStep(t *testing.T) {
	dir := t.TempDir()
	m, api := newFailureEscalationFixture(t, dir)
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.FailureEscalation = &FailureEscalationPolicy{
			MaxFailures: 3,
			Steps:       []FailureEscalationStep{escalationClaudeStep(0), escalationPiStep(0)},
		}
	})

	var storedFailures, storedAttempts int
	var sawStored bool
	var log escalationSpawnLog
	spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		if len(log.workers()) == 1 {
			reloaded, err := NewStore(dir)
			if err != nil {
				return SpawnResult{}, err
			}
			p, ok := reloaded.GetPRD(prd.ID)
			if !ok {
				return SpawnResult{}, fmt.Errorf("PRD %s missing after reload", prd.ID)
			}
			task := &p.Story[0].Tasks[0]
			storedFailures, storedAttempts, sawStored = task.EscalationFailures, len(task.Attempts), true
		}
		return log.spawn(ctx, req)
	}
	verifies := 0
	var vmu sync.Mutex
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		vmu.Lock()
		defer vmu.Unlock()
		verifies++
		return VerificationResult{OK: false, Summary: "tests still fail"}, nil
	}
	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollEscalationSettled(t, m, prd.ID, 10*time.Second)

	if !sawStored {
		t.Fatalf("second spawn never ran, so the reload was not checked")
	}
	if storedFailures != 1 || storedAttempts != 1 {
		t.Fatalf("stored after first failure: EscalationFailures=%d attempts=%d, want 1 and 1", storedFailures, storedAttempts)
	}
	want := []string{
		escClaudeBackend + "/" + escClaudeModel,
		escPiBackend + "/" + escPiModel,
		escPiBackend + "/" + escPiModel,
	}
	if got := log.workers(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("spawn workers = %v, want %v", got, want)
	}
}

// TestFailureEscalation_SpawnCountMaxFailures checks the exact number of
// attempts for a task whose every attempt fails: MaxFailures spawns, for each
// of the allowed values.
func TestFailureEscalation_SpawnCountMaxFailures(t *testing.T) {
	for _, max := range []int{3, 4} {
		t.Run(fmt.Sprintf("max_failures_%d", max), func(t *testing.T) {
			m, api := newFailureEscalationFixture(t, t.TempDir())
			prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
				p.FailureEscalation = &FailureEscalationPolicy{MaxFailures: max}
			})
			var log escalationSpawnLog
			runEscalationPRD(t, m, api, prd, &log, alwaysFailVerify)
			if n := len(log.workers()); n != max {
				t.Fatalf("spawn count = %d, want %d", n, max)
			}
			if task := escalationTask(t, m, prd.ID); task.EscalationFailures != max {
				t.Fatalf("EscalationFailures = %d, want %d", task.EscalationFailures, max)
			}
		})
	}
}
