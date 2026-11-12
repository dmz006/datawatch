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

// slGraceRig returns a fresh steps rig whose autonomous.archive_grace_minutes
// setting is grace, reading a controllable clock.
func slGraceRig(t *testing.T, grace int) (*coordHarness, *SupervisorLoopSteps) {
	t.Helper()
	h, steps := slUnsetGraceRig(t)
	cfg := h.m.Config()
	cfg.ArchiveGraceMinutes = &grace
	h.m.SetConfig(cfg)
	return h, steps
}

// slUnsetGraceRig is a rig where autonomous.archive_grace_minutes was never set.
func slUnsetGraceRig(t *testing.T) (*coordHarness, *SupervisorLoopSteps) {
	t.Helper()
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{Settings: func() supervisorloop.Settings { return supervisorloop.DefaultSettings() }})
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

// Unset means the 60-minute default: a record cancelled 5 minutes ago is held,
// one cancelled 61 minutes ago is archived.
func TestSupervisorLoopArchiveGraceUnsetUsesDefault(t *testing.T) {
	h, steps := slUnsetGraceRig(t)
	slCancelRecord(t, h, 0, slBase.Add(-5*time.Minute))
	slSetClock(t, steps, slBase)
	if r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords); r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h, 0) != PRDCancelled {
		t.Errorf("record cancelled 5 minutes ago = %s, want held under the default", slStatus(t, h, 0))
	}
	if slStatus(t, h, 0) != PRDCancelled {
		t.Errorf("record cancelled 5 minutes ago = %s, still want held", slStatus(t, h, 0))
	}
	// A fresh rig, still unset: a record cancelled 61 minutes ago goes.
	h2, steps2 := slUnsetGraceRig(t)
	slCancelRecord(t, h2, 0, slBase.Add(-61*time.Minute))
	slSetClock(t, steps2, slBase)
	if r := resultFor(t, steps2.RunPass(context.Background()), stepCloseFinishedRecords); r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h2, 0) != PRDArchived {
		t.Errorf("record cancelled 61 minutes ago = %s, want archived", slStatus(t, h2, 0))
	}
}

// The superseded loop key does not drive the step any more.
func TestSupervisorLoopArchiveGraceIgnoresLoopKey(t *testing.T) {
	set := supervisorloop.DefaultSettings()
	if err := set.Set(supervisorloop.KeyArchiveCancelledAfterMinutes, 0); err != nil {
		t.Fatal(err)
	}
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{Settings: func() supervisorloop.Settings { return set }})
	slCancelRecord(t, h, 0, slBase.Add(-5*time.Minute))
	slSetClock(t, steps, slBase)
	if r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords); r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h, 0) != PRDCancelled {
		t.Errorf("loop key 0 archived a record inside the default grace")
	}
}

func TestSupervisorLoopArchiveGraceRejectedAtOnce(t *testing.T) {
	h, steps := slGraceRig(t, 60)
	h.setStatus(0, PRDRejected)
	slSetClock(t, steps, slBase)
	if r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords); r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("rejected record = %s, want archived at once", slStatus(t, h, 0))
	}
}

// A cancelled record with no cancel decision and a long-stale update time must
// earn a full grace from the first pass that sees it, not archive at once.
func TestSupervisorLoopArchiveGraceNoDecisionEarnsFullGrace(t *testing.T) {
	h, steps := slGraceRig(t, 60)
	p, _ := h.m.Store().GetPRD(h.prds[0].ID)
	p.Status = PRDCancelled
	p.UpdatedAt = slBase.Add(-5 * time.Hour)
	p.Decisions = nil
	if err := h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	slSetClock(t, steps, slBase)
	resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if slStatus(t, h, 0) != PRDCancelled {
		t.Fatalf("archived on first sight, want held for the full grace")
	}
	slSetClock(t, steps, slBase.Add(30*time.Minute))
	resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if slStatus(t, h, 0) != PRDCancelled {
		t.Fatalf("archived 30 minutes after first sight, want held")
	}
	slSetClock(t, steps, slBase.Add(61*time.Minute))
	resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if slStatus(t, h, 0) == PRDCancelled {
		t.Errorf("still cancelled 61 minutes after first sight, want archived")
	}
}
