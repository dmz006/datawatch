package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// The sweep archives what its own facts classify, keeps what it cannot, never
// drops a hold and never rewrites the status of running or held work. A record
// it keeps is a counted detail of the step, not a step error retried loudly.
func TestClosedOutcomeSweepKeepsUnclassifiedAndHeldWork(t *testing.T) {
	h := newCoordHarness(t, 5)
	store := &slMemStore{}
	eng := &supervisorloop.Engine{Settings: supervisorloop.DefaultSettings(), Clock: slClock{}, Store: store}
	steps := NewSupervisorLoopSteps(h.c, eng, SupervisorLoopHooks{Settings: func() supervisorloop.Settings { return supervisorloop.DefaultSettings() }})
	cfg := h.m.Config()
	grace := 0
	cfg.ArchiveGraceMinutes = &grace
	cfg.ClosedOutcomeArchive = ClosedOutcomeOn
	h.m.SetConfig(cfg)

	// 0: finished all its tasks, never shipped: nothing proves an outcome.
	h.m.Store().MutatePRD(h.prds[0].ID, func(p *PRD) bool {
		for si := range p.Story {
			for ti := range p.Story[si].Tasks {
				p.Story[si].Tasks[ti].Status = TaskCompleted
			}
		}
		return true
	})
	h.setStatus(0, PRDCompleted)
	// 1: cancelled, its spec declares a hold.
	h.m.Store().MutatePRD(h.prds[1].ID, func(p *PRD) bool { p.Spec = "STATUS: HOLD until all features ship"; return true })
	slCancelRecord(t, h, 1, slBase.Add(-90*time.Minute))
	// 2: held by the operator. 3: running.
	h.setStatus(2, PRDHeld)
	h.setStatus(3, PRDRunning)
	// 4: cancelled with its work unfinished: the sweep may classify it dropped.
	slCancelRecord(t, h, 4, slBase.Add(-90*time.Minute))
	slSetClock(t, steps, slBase)

	r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if r.Err != nil {
		t.Fatalf("a record kept for want of an outcome is not a step error: %v", r.Err)
	}
	for i, want := range map[int]PRDStatus{0: PRDCompleted, 1: PRDCancelled, 2: PRDHeld, 3: PRDRunning, 4: PRDArchived} {
		if got := slStatus(t, h, i); got != want {
			t.Errorf("record %d status = %s, want %s", i, got, want)
		}
	}
	if p, _ := h.m.Store().GetPRD(h.prds[4].ID); p.ClosedOutcome != OutcomeDropped {
		t.Errorf("record 4 outcome = %q, want dropped", p.ClosedOutcome)
	}
	if !strings.Contains(r.Detail, "outcome") {
		t.Errorf("detail %q must say how many were kept for want of an outcome or a hold", r.Detail)
	}
}
