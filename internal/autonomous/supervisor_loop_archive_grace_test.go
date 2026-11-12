package autonomous

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// slBase is the fixed "wall clock" the archive-grace tests reason over; the
// steps' clock is pointed at it so the grace is measured purely against the
// record's cancel time. It sits well after the host clock (SavePRD stamps
// UpdatedAt with the host now) so the cancel decision always dominates.
var slBase = time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)

// slSetClock pins the steps to a single fixed instant.
func slSetClock(t *testing.T, s *SupervisorLoopSteps, at time.Time) {
	t.Helper()
	s.now = func() time.Time { return at }
}

// slCancelRecord flips record i to cancelled and stamps it with the latest
// cancel decision at cancelAt (with the same UpdatedAt as the fallback).
func slCancelRecord(t *testing.T, h *coordHarness, i int, cancelAt time.Time) {
	t.Helper()
	p, ok := h.m.Store().GetPRD(h.prds[i].ID)
	if !ok {
		t.Fatalf("prd %d missing", i)
	}
	// Re-cancel: append a later cancel decision on top of the earlier one.
	p.Status = PRDCancelled
	p.UpdatedAt = cancelAt
	p.Decisions = append(p.Decisions, Decision{At: cancelAt, Kind: "cancel", Actor: "operator"})
	if err := h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
}

// slGraceSet returns a fresh steps rig whose archive_cancelled_after_minutes
// setting is grace, reading a controllable clock.
func slGraceRig(t *testing.T, grace int) (*coordHarness, *SupervisorLoopSteps) {
	t.Helper()
	set := supervisorloop.DefaultSettings()
	if err := set.Set(supervisorloop.KeyArchiveCancelledAfterMinutes, grace); err != nil {
		t.Fatal(err)
	}
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{Settings: func() supervisorloop.Settings { return set }})
	return h, steps
}

func slStatus(t *testing.T, h *coordHarness, i int) PRDStatus {
	t.Helper()
	p, ok := h.m.Store().GetPRD(h.prds[i].ID)
	if !ok {
		t.Fatalf("prd %d missing", i)
	}
	return p.Status
}

func TestSupervisorLoopArchiveGraceInsideKept(t *testing.T) {
	h, steps := slGraceRig(t, 60)
	slCancelRecord(t, h, 0, slBase.Add(-30*time.Minute))
	slSetClock(t, steps, slBase)
	r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h, 0) != PRDCancelled {
		t.Errorf("record inside grace archived, want kept")
	}
}

func TestSupervisorLoopArchiveGracePastArchived(t *testing.T) {
	h, steps := slGraceRig(t, 60)
	slCancelRecord(t, h, 0, slBase.Add(-90*time.Minute))
	slSetClock(t, steps, slBase)
	r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("record past grace = %s, want archived", slStatus(t, h, 0))
	}
}

func TestSupervisorLoopArchiveGraceReCancelRestarts(t *testing.T) {
	h, steps := slGraceRig(t, 60)
	// An old cancel that is well past the grace ...
	slCancelRecord(t, h, 0, slBase.Add(-90*time.Minute))
	// ... is then cancelled again recently, restarting the grace.
	slCancelRecord(t, h, 0, slBase.Add(-10*time.Minute))
	slSetClock(t, steps, slBase)
	r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h, 0) != PRDCancelled {
		t.Errorf("re-cancelled record archived, want kept by the restarted grace")
	}
}

func TestSupervisorLoopArchiveGraceZeroArchivesAtOnce(t *testing.T) {
	h, steps := slGraceRig(t, 0)
	slCancelRecord(t, h, 0, slBase) // cancelled right now
	slSetClock(t, steps, slBase)
	r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("zero-grace record = %s, want archived at once", slStatus(t, h, 0))
	}
}

func TestSupervisorLoopArchiveGraceCompletedAtOnce(t *testing.T) {
	h, steps := slGraceRig(t, 60) // grace is high; it must not shield a completed record
	h.setStatus(0, PRDCompleted)
	slSetClock(t, steps, slBase)
	r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("completed record = %s, want archived at once", slStatus(t, h, 0))
	}
}
