package autonomous

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// Concurrent Store.SavePRD vs the PRD writers that were moved under
// Store.WithLock / Store.MutatePRD. SavePRD itself rewrites the stored PRD
// (UpdatedAt, Decisions trimming, Rev, stale blocked reason) under the store
// lock, so any writer that still mutates the shared *PRD, a Story or a Task
// without that lock is a data race against it. Each test below runs one
// SavePRD loop plus one goroutine per writer, every writer growing and
// shrinking the Decisions / Verdicts / task slices, then waits for all of them
// and asserts nothing panicked. The tests only prove something under
// `go test -race`: a writer that regresses to an unlocked write is then
// reported as a data race.
//
//	env -u TMUX go test -race -count=3 -run TestStoreRace ./internal/autonomous/

const storeRaceIterations = 200

// raceWriter is one fixed writer: run is called once per iteration with the
// iteration number.
type raceWriter struct {
	name string
	run  func(i int)
}

// runStoreRace starts a SavePRD loop on the stored PRD and one goroutine per
// writer, each looping storeRaceIterations times, waits for all of them with a
// WaitGroup and fails the test if any goroutine panicked.
func runStoreRace(t *testing.T, m *Manager, prdID string, writers []raceWriter) {
	t.Helper()
	saved, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("stored PRD missing")
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var panics []string
	guard := func(name string) {
		if r := recover(); r != nil {
			mu.Lock()
			panics = append(panics, fmt.Sprintf("%s: %v", name, r))
			mu.Unlock()
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer guard("SavePRD")
		for i := 0; i < storeRaceIterations; i++ {
			if err := m.Store().SavePRD(saved); err != nil {
				t.Errorf("SavePRD iteration %d: %v", i, err)
				return
			}
		}
	}()
	for _, w := range writers {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer guard(w.name)
			for i := 0; i < storeRaceIterations; i++ {
				w.run(i)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(panics) > 0 {
		t.Fatalf("goroutine panicked: %v", panics)
	}
}

// shrinkWriter grows and shrinks the PRD's Decisions and Verdicts and the
// task's AttemptRefs/History under the store lock, so SavePRD's own trimming
// and JSON encoding is raced against slices whose length keeps changing.
func shrinkWriter(m *Manager, prd *PRD, task *Task) raceWriter {
	return raceWriter{name: "shrink", run: func(i int) {
		m.Store().WithLock(func() {
			prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "race_probe", Actor: "test", Note: "grow"})
			prd.Verdicts = append(prd.Verdicts, GuardrailVerdict{Guardrail: "rules", Outcome: "pass", VerdictAt: time.Now()})
			task.AttemptRefs = append(task.AttemptRefs, fmt.Sprintf("ref-%d", i))
			if i%3 == 2 {
				prd.Decisions = prd.Decisions[:len(prd.Decisions)/2]
				prd.Verdicts = prd.Verdicts[:len(prd.Verdicts)/2]
				task.AttemptRefs = task.AttemptRefs[:len(task.AttemptRefs)/2]
			}
		})
	}}
}

func newStoreRaceFixture(t *testing.T) (*Manager, *PRD, *Task) {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	// Keep the alert paths quiet.
	m.SetAlertSink(func(string, string, string) string { return "" })
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	saved, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("stored PRD missing")
	}
	var task *Task
	m.Store().WithLock(func() { task = &saved.Story[0].Tasks[0] })
	return m, saved, task
}

// TestStoreRace_FixedWritersVsSavePRD runs every fixed writer that can be
// driven without a daemon against a concurrent SavePRD loop: the push-outcome
// writer, the parallel-metrics write (the exact locked step the completion
// roll-up uses), the infra-retry writer, the defect-intake record, the
// stale-rollup decision, the rules-review hold, the attempt-snapshot task
// edit, the shipped record and the tag assignment.
//
// Deliberately not driven here: parkPRDForBootResume and SetTaskEffortCeiling.
// Their writes run under Store.WithLock, but each still reads shared PRD
// fields outside the lock (parkPRDForBootResume calls lastDecision, which
// reads prd.Decisions; SetTaskEffortCeiling calls taskOverrideLocked, which
// reads prd.Status), and `go test -race` reports those reads against a
// concurrent SavePRD. Add them back once those reads are locked.
func TestStoreRace_FixedWritersVsSavePRD(t *testing.T) {
	m, prd, task := newStoreRaceFixture(t)
	id := prd.ID

	tag, err := m.CreateTag("race-tag", "", "", "")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	pushed := git.PushResult{Pushed: true, Ref: "refs/heads/x", SHA: "abc"}
	notPushed := git.PushResult{Reason: "no remote"}
	plan := infraRetryPlan{Class: "storage_write", Reason: "disk full", Number: 1, MaxTries: 3, Delay: time.Second}
	verdict := GuardrailVerdict{Guardrail: "rules", Outcome: "block", Category: "content", Summary: "bad", VerdictAt: time.Now()}

	writers := []raceWriter{
		shrinkWriter(m, prd, task),
		{name: "recordPushOutcome", run: func(i int) {
			if i%2 == 0 {
				m.recordPushOutcome(prd, pushed, git.PushBranch)
			} else {
				m.recordPushOutcome(prd, notPushed, git.PushBranch)
			}
		}},
		{name: "applyParallelMetrics", run: func(int) {
			// The same locked step executor.go runs on completion.
			m.Store().WithLock(func() { applyParallelMetrics(prd) })
		}},
		{name: "recordInfraRetry", run: func(i int) {
			p := plan
			p.Number = i + 1
			m.recordInfraRetry(prd, task, p)
		}},
		{name: "recordDefectIntake", run: func(i int) {
			m.recordDefectIntake(prd, "filed", "auto", fmt.Sprintf("defect %d", i))
		}},
		{name: "recordRollupAborted", run: func(i int) {
			m.recordRollupAborted(id, fmt.Sprintf("stale %d", i))
		}},
		{name: "holdRulesReviewForOperator", run: func(int) {
			m.holdRulesReviewForOperator(prd, verdict)
		}},
		{name: "RecordTaskAttemptSnapshot", run: func(i int) {
			_ = m.RecordTaskAttemptSnapshot(id, task.ID, fmt.Sprintf("snap-%d", i))
		}},
		{name: "RecordPRDShipped", run: func(i int) {
			_, _ = m.RecordPRDShipped(id, "v1.0.0", time.Now(), fmt.Sprintf("commit%d", i), ShippedSourceMergeStep)
		}},
		{name: "SetPRDTagsAndPrimary", run: func(i int) {
			ids := []string{tag.ID}
			if i%2 == 1 {
				ids = nil
			}
			if err := m.SetPRDTags(id, ids); err != nil {
				t.Errorf("SetPRDTags: %v", err)
			}
		}},
	}

	runStoreRace(t, m, id, writers)

	if _, ok := m.Store().SnapshotPRD(id); !ok {
		t.Fatal("stored PRD missing after the race")
	}
}

// TestStoreRace_PushOutcomeVsSavePRD isolates the push-outcome writer so a
// regression there is reported on its own, and checks the final state.
func TestStoreRace_PushOutcomeVsSavePRD(t *testing.T) {
	m, prd, _ := newStoreRaceFixture(t)
	pushed := git.PushResult{Pushed: true, Ref: "refs/heads/x", SHA: "abc"}
	notPushed := git.PushResult{Reason: "no remote"}

	runStoreRace(t, m, prd.ID, []raceWriter{
		{name: "recordPushOutcome", run: func(i int) {
			if i%2 == 0 {
				m.recordPushOutcome(prd, pushed, git.PushBranch)
			} else {
				m.recordPushOutcome(prd, notPushed, git.PushBranch)
			}
		}},
	})

	final, ok := m.Store().SnapshotPRD(prd.ID)
	if !ok {
		t.Fatal("stored PRD missing")
	}
	// Iteration 199 was the not-pushed result.
	if final.Git.PushState != PushStateNotPushed {
		t.Fatalf("final PushState = %q, want %q", final.Git.PushState, PushStateNotPushed)
	}
}

// TestStoreRace_CompletionMetricsVsSavePRD drives a one-task PRD to completion
// through the real executor (fake spawn, passing verify) while a SavePRD loop
// hammers the same stored PRD, so the applyParallelMetrics step on the
// completion path and the executor's own PRD/task writes race SavePRD for
// real rather than through a re-implementation of the step.
func TestStoreRace_CompletionMetricsVsSavePRD(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	m.SetAlertSink(func(string, string, string) string { return "" })

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "all good"}, nil
	}

	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	saved, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("stored PRD missing")
	}
	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var panicked any
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				mu.Lock()
				panicked = r
				mu.Unlock()
			}
		}()
		for i := 0; i < storeRaceIterations; i++ {
			_ = m.Store().SavePRD(saved)
			select {
			case <-done:
				return
			default:
			}
		}
	}()

	// The concurrent SavePRD calls bump Rev, so the executor's roll-up may
	// legitimately abort as stale (BL451) and leave the PRD running; the
	// outcome is therefore not asserted. Wait until the PRD finishes or the
	// SavePRD loop has had its chance to overlap the completion path.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sn, ok := m.Store().SnapshotPRD(prd.ID); ok && sn.Status == PRDCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(done)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if panicked != nil {
		t.Fatalf("SavePRD goroutine panicked: %v", panicked)
	}
	if _, ok := m.Store().SnapshotPRD(prd.ID); !ok {
		t.Fatal("stored PRD missing after completion")
	}
}
