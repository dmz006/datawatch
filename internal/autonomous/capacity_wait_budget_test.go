package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// The capacity wait bound is one budget per admission: queue time already
// spent by an earlier acquireLease (node-load failover re-enters it) counts,
// so re-entry does not start a fresh window.
func TestAcquireLease_WaitBudgetCarriesAcrossReentry(t *testing.T) {
	m, _, led := capFixtureFakeClock(t, 1)
	ac := newAuditCapture()
	m.SetCapacityAudit(ac.recorder())
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 60
	m.mu.Unlock()
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holder, queued := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1]
	if err := led.Acquire(context.Background(), capNodeReq(holder.ID, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	// 45s of an earlier acquireLease already used.
	m.addCapacityWaitSpent(queued.ID, 45*time.Second)
	if got := m.capacityWaitRemaining(queued.ID, 60*time.Second); got != 15*time.Second {
		t.Fatalf("remaining = %v, want 15s", got)
	}
	// Used up entirely: the next acquireLease times out at once, even though
	// the node stays busy; a fresh window would block here.
	m.addCapacityWaitSpent(queued.ID, 15*time.Second)
	done := make(chan error, 1)
	go func() {
		done <- m.acquireLease(context.Background(), cur, &queued, "opencode", "", capacity.RoleWorker, nil, "")
	}()
	select {
	case err := <-done:
		if !errors.Is(err, capacity.ErrWaitTimeout) {
			t.Fatalf("want ErrWaitTimeout, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("re-entered acquireLease started a fresh wait window")
	}

	// A finished admission starts the next one with a full bound.
	m.resetCapacityWait(queued.ID)
	if got := m.capacityWaitRemaining(queued.ID, 60*time.Second); got != 60*time.Second {
		t.Fatalf("after reset remaining = %v, want 60s", got)
	}
}

// Time queued inside acquireLease is charged to the budget, measured on the
// audit recorder's clock, however often the reason text changes.
func TestAcquireLease_ChargesQueuedTime(t *testing.T) {
	m, _, led := capFixtureFakeClock(t, 1)
	ac := newAuditCapture()
	m.SetCapacityAudit(ac.recorder())
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holder, queued := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1]
	if err := led.Acquire(context.Background(), capNodeReq(holder.ID, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- m.acquireLease(context.Background(), cur, &queued, "opencode", "", capacity.RoleWorker, nil, "")
	}()
	capWaitUntil(t, func() bool { return len(ac.all()) == 1 })
	ac.Advance(40 * time.Second)
	led.Release(holder.ID)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := m.capacityWaitRemaining(queued.ID, 100*time.Second); got != 60*time.Second {
		t.Fatalf("remaining = %v, want 60s after 40s queued", got)
	}
}

// The reason a queued worker shows, on the task record and in the audit log,
// names the pool its explicit backend asked for, and still classifies and
// parses as the ledger's own reason.
func TestAcquireLease_WaitReasonNamesRequestedPool(t *testing.T) {
	m, _, led := capFixtureFakeClock(t, 1)
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"llm:qwen", "node:n1"}, "n1" })
	ac := newAuditCapture()
	m.SetCapacityAudit(ac.recorder())
	prd := newPRDWithTasks(t, m, 2)
	cur, _ := m.Store().GetPRD(prd.ID)
	holder, queued := cur.Story[0].Tasks[0], cur.Story[0].Tasks[1]
	if err := led.Acquire(context.Background(), capNodeReq(holder.ID, prd.ID), 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- m.acquireLease(context.Background(), cur, &queued, "opencode", "", capacity.RoleWorker, nil, "")
	}()
	capWaitUntil(t, func() bool {
		snap, ok := m.Store().TaskSnapshot(queued.ID)
		return ok && snap.Status == TaskWaitingCapacity && snap.WaitReason != ""
	})
	snap, _ := m.Store().TaskSnapshot(queued.ID)
	if !strings.Contains(snap.WaitReason, "requested pool llm:qwen") {
		t.Fatalf("task wait reason does not name the requested pool: %q", snap.WaitReason)
	}
	if got := ClassifyWaitReason(snap.WaitReason); got != WaitCauseNodeBusy {
		t.Fatalf("classified %q, want node busy (reason %q)", got, snap.WaitReason)
	}
	if got := waitReasonHolder(WaitCauseNodeBusy, snap.WaitReason); got != holder.ID {
		t.Fatalf("holder = %q, want %q", got, holder.ID)
	}
	led.Release(holder.ID)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
