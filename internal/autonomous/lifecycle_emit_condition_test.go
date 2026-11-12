// Tests for the edge-triggered condition events in
// lifecycle_emit_condition.go: each fires once when its condition starts to
// hold, stays quiet while it holds, and fires again only after it cleared.

package autonomous

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// conditionCount counts the events of one kind the env's bus delivered for a
// PRD, after giving the dispatcher a moment to flush.
func (e *stuckWatchdogEnv) conditionCount(kind, prdID string) int {
	time.Sleep(50 * time.Millisecond)
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.events {
		if ev.Kind == kind && ev.PRDID == prdID {
			n++
		}
	}
	return n
}

func TestStuckRunningEvent(t *testing.T) {
	e := newStuckWatchdogEnv(t, 60)
	prd, _ := bl451RunningPRD(t, e.m, nil, TaskCompleted)

	e.m.sweepStuckRunning() // first sight, bound not passed
	if n := e.conditionCount(KindAutomatonStuckRunning, prd.ID); n != 0 {
		t.Fatalf("emitted %d before the bound, want 0", n)
	}
	e.advance(61 * time.Second)
	e.m.sweepStuckRunning()
	if n := e.conditionCount(KindAutomatonStuckRunning, prd.ID); n != 1 {
		t.Fatalf("emitted %d at the bound, want 1", n)
	}
	// Still stuck after another full bound: no second event.
	e.advance(30 * time.Second)
	e.m.sweepStuckRunning()
	if n := e.conditionCount(KindAutomatonStuckRunning, prd.ID); n != 1 {
		t.Fatalf("emitted %d while the condition held, want 1", n)
	}
	// Live work clears the condition; a later stuck episode emits again.
	e.mu.Lock()
	e.live[prd.ID] = true
	e.mu.Unlock()
	e.m.sweepStuckRunning()
	e.mu.Lock()
	e.live[prd.ID] = false
	e.mu.Unlock()
	e.m.sweepStuckRunning()
	e.advance(61 * time.Second)
	e.m.sweepStuckRunning()
	if n := e.conditionCount(KindAutomatonStuckRunning, prd.ID); n != 2 {
		t.Fatalf("emitted %d after the condition cleared and returned, want 2", n)
	}
}

func TestAllTasksDoneEvent(t *testing.T) {
	e := newStuckWatchdogEnv(t, 60)
	prd, _ := bl451RunningPRD(t, e.m, nil, TaskCompleted)

	e.m.sweepStuckRunning()
	e.m.sweepStuckRunning()
	if n := e.conditionCount(KindAutomatonTasksDone, prd.ID); n != 1 {
		t.Fatalf("emitted %d for a held condition, want 1", n)
	}
	// A failed task clears the condition; completing it again re-arms it.
	p, _ := e.m.Store().GetPRD(prd.ID)
	e.m.Store().WithLock(func() { p.Story[0].Tasks[0].Status = TaskFailed })
	e.m.sweepStuckRunning()
	e.m.Store().WithLock(func() { p.Story[0].Tasks[0].Status = TaskCancelled })
	e.m.sweepStuckRunning()
	if n := e.conditionCount(KindAutomatonTasksDone, prd.ID); n != 2 {
		t.Fatalf("emitted %d after clear and return, want 2", n)
	}
}

func TestMergeReadyEvent(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	col := &collector{}
	cancel := th.h.m.Subscribe(LifecycleFilter{Kinds: []string{KindAutomatonMergeReady}}, col.add)
	t.Cleanup(func() { cancel(); th.h.m.lifecycleBus().Close() })

	th.h.c.triagePass()
	th.h.c.triagePass()
	waitFor(t, "merge_ready delivery", func() bool { return len(col.snapshot()) >= 1 })
	time.Sleep(50 * time.Millisecond)
	if n := len(col.snapshot()); n != 1 {
		t.Fatalf("merge_ready events = %d while pending, want 1", n)
	}
	if ev := col.snapshot()[0]; ev.PRDID != th.h.prds[0].ID {
		t.Fatalf("PRDID = %q, want %q", ev.PRDID, th.h.prds[0].ID)
	}

	// Merged: the condition clears. Pending again: it emits again.
	git.setMerged("branch-1", "base-2")
	th.h.c.triagePass()
	git.setPending("branch-2")
	th.h.c.triagePass()
	waitFor(t, "second merge_ready", func() bool { return len(col.snapshot()) >= 2 })
}
