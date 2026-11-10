package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// runCapLeaseFixture is a manager with one running PRD task, a capacity ledger
// whose "host" pool has one slot, and an audit capture. The verifier of the
// task holds the slot under VerifyHolder(task id).
func runCapLeaseFixture(t *testing.T) (m *Manager, led *capacity.Ledger, cap *auditCapture, prdID, taskID string) {
	t.Helper()
	m, cap, prdID, ids := escalationFixture(t)
	taskID = ids[0]
	led = capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	led.SetLimit("host", 1)
	m.SetCapacity(led, func(string, string) ([]string, string) { return []string{"host"}, "" })
	req := capacity.Request{Holder: VerifyHolder(taskID), PRDID: prdID, Pools: []string{"host"}}
	if err := led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatalf("Acquire verifier lease: %v", err)
	}
	return m, led, cap, prdID, taskID
}

func runCapDecisions(m *Manager, prdID string) []Decision {
	prd, _ := m.Store().GetPRD(prdID)
	var out []Decision
	for _, d := range prd.Decisions {
		if d.Kind == verifierRunCapDecisionKind {
			out = append(out, d)
		}
	}
	return out
}

func TestVerifierRunCapLeaseDecisionReleasesLeaseExactlyOnce(t *testing.T) {
	m, led, _, prdID, taskID := runCapLeaseFixture(t)
	if !led.Held(VerifyHolder(taskID)) {
		t.Fatal("verifier lease should be held before the cap trips")
	}
	// A second holder queues behind the verifier's slot.
	acquired := make(chan error, 1)
	go func() {
		acquired <- led.Acquire(context.Background(),
			capacity.Request{Holder: "waiter", PRDID: prdID, Pools: []string{"host"}}, 5*time.Second, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(led.Snapshot().Waiting) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(led.Snapshot().Waiting) == 0 {
		t.Fatal("waiter never queued")
	}

	released, err := m.HandleVerifierRunCap(prdID, taskID, 90*time.Second, 60)
	if err != nil {
		t.Fatalf("HandleVerifierRunCap: %v", err)
	}
	if !released {
		t.Fatal("the first call must release the held lease")
	}
	if led.Held(VerifyHolder(taskID)) {
		t.Fatal("the verifier lease is still held after the release")
	}
	select {
	case aerr := <-acquired:
		if aerr != nil {
			t.Fatalf("waiter acquire: %v", aerr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the released slot never reached the waiter")
	}

	// A repeat finds no lease: nothing is released, and the waiter's lease stays.
	released, err = m.HandleVerifierRunCap(prdID, taskID, 91*time.Second, 60)
	if err != nil {
		t.Fatalf("second HandleVerifierRunCap: %v", err)
	}
	if released {
		t.Fatal("the second call must not release a lease again")
	}
	if !led.Held("waiter") {
		t.Fatal("the waiter's lease must survive the second call")
	}
}

func TestVerifierRunCapLeaseDecisionNoteHasElapsedAndCap(t *testing.T) {
	m, _, _, prdID, taskID := runCapLeaseFixture(t)
	if _, err := m.HandleVerifierRunCap(prdID, taskID, 2700*time.Second+400*time.Millisecond, 2700); err != nil {
		t.Fatal(err)
	}
	ds := runCapDecisions(m, prdID)
	if len(ds) != 1 {
		t.Fatalf("want one run-cap decision, got %d: %+v", len(ds), ds)
	}
	for _, want := range []string{"task=" + taskID, "elapsed_seconds=2700", "cap_seconds=2700"} {
		if !strings.Contains(ds[0].Note, want) {
			t.Fatalf("note %q missing %q", ds[0].Note, want)
		}
	}
	if ds[0].FailureCategory != "infra" || ds[0].Actor != "autonomous" {
		t.Fatalf("decision = %+v, want infra category and autonomous actor", ds[0])
	}
}

func TestVerifierRunCapLeaseDecisionWritesAuditRecord(t *testing.T) {
	m, _, cap, prdID, taskID := runCapLeaseFixture(t)
	if err := m.RecordVerifierRunCap(prdID, taskID, 61*time.Second, 60); err != nil {
		t.Fatal(err)
	}
	entries := cap.all()
	if len(entries) != 1 {
		t.Fatalf("want one audit entry, got %+v", entries)
	}
	e := entries[0]
	if e.Action != VerifierRunCapAuditAction || e.Subject != taskID {
		t.Fatalf("audit entry = %+v", e)
	}
	if e.Details["elapsed_seconds"] != 61 || e.Details["cap_seconds"] != 60 || e.Details["end_reason"] != VerifierMaxRunEndReason {
		t.Fatalf("audit details = %+v", e.Details)
	}
}

func TestVerifierRunCapLeaseDecisionEndReasonIsVerifierMaxRun(t *testing.T) {
	if VerifierMaxRunEndReason != "verifier_max_run" {
		t.Fatalf("end reason = %q, want verifier_max_run", VerifierMaxRunEndReason)
	}
	f := attemptFailure{
		Outcome: attemptOutcomeFailed, Category: "infra", EndReason: attemptEndVerifierFailed,
		Verification: VerificationResult{Category: "infra", EndReason: VerifierMaxRunEndReason},
	}
	for _, adopted := range []bool{false, true} {
		if got := deriveAttemptEndReason(f, adopted); got != "verifier_max_run" {
			t.Fatalf("adopted=%v: attempt end reason = %q, want verifier_max_run", adopted, got)
		}
	}
	// Any other infra failure keeps its derived reason.
	other := attemptFailure{Outcome: attemptOutcomeFailed, Category: "infra"}
	if got := deriveAttemptEndReason(other, false); got != attemptEndSessionDied {
		t.Fatalf("plain infra end reason = %q, want %q", got, attemptEndSessionDied)
	}
}

func TestVerifierRunCapLeaseDecisionNoLeaseOrLedgerStillRecords(t *testing.T) {
	m, cap, prdID, ids := escalationFixture(t) // no ledger wired
	released, err := m.HandleVerifierRunCap(prdID, ids[0], time.Minute, 30)
	if err != nil || released {
		t.Fatalf("released=%v err=%v, want false, nil with no ledger", released, err)
	}
	if len(runCapDecisions(m, prdID)) != 1 || len(cap.all()) != 1 {
		t.Fatal("the decision and audit record must be written without a lease")
	}
	if err := m.RecordVerifierRunCap("missing", ids[0], time.Minute, 30); err == nil {
		t.Fatal("an unknown PRD must be an error")
	}
}
