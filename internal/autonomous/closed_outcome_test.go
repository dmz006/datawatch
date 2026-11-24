package autonomous

import (
	"strings"
	"testing"
	"time"
)

// coMgr builds a Manager with the archive guard in the given mode.
func coMgr(t *testing.T, mode string) *Manager {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ClosedOutcomeArchive = mode
	return newEmitManager(t, cfg, nil)
}

// coPRD stores an Automaton with one pending task in the given status.
func coPRD(t *testing.T, m *Manager, status PRDStatus) *PRD {
	t.Helper()
	return newEmitPRD(t, m, status)
}

func coShip(t *testing.T, m *Manager, id string) {
	t.Helper()
	m.Store().MutatePRD(id, func(p *PRD) bool {
		p.MergedInRelease, p.MergeCommit = "v9.0.1", "abc1234567"
		return true
	})
}

func coGet(t *testing.T, m *Manager, id string) *PRD {
	t.Helper()
	p, ok := m.Store().GetPRD(id)
	if !ok {
		t.Fatalf("prd %s missing", id)
	}
	return p
}

// A finished Automaton with no shipped record and no outcome is not archived:
// archived must never mix delivered, superseded and dropped work.
func TestClosedOutcomeArchiveRefusesRecordWithNoOutcome(t *testing.T) {
	m := coMgr(t, ClosedOutcomeOn)
	p := coPRD(t, m, PRDCompleted)
	_, err := m.Archive(p.ID, "operator")
	if err == nil || !strings.Contains(err.Error(), "outcome") {
		t.Fatalf("archive of a record with no outcome must be refused naming the outcome, got %v", err)
	}
	if got := coGet(t, m, p.ID); got.Status != PRDCompleted {
		t.Fatalf("status = %s, a refused archive changes nothing", got.Status)
	}
}

// A shipped record is proof of delivery: the archive step records delivered in
// the same step.
func TestClosedOutcomeArchiveDerivesDeliveredFromShippedRecord(t *testing.T) {
	m := coMgr(t, ClosedOutcomeOn)
	p := coPRD(t, m, PRDCompleted)
	coShip(t, m, p.ID)
	if _, err := m.Archive(p.ID, "operator"); err != nil {
		t.Fatalf("archive of shipped work: %v", err)
	}
	got := coGet(t, m, p.ID)
	if got.Status != PRDArchived || got.ClosedOutcome != OutcomeDelivered {
		t.Fatalf("status=%s outcome=%q, want archived + delivered", got.Status, got.ClosedOutcome)
	}
	if d := lastDecision(got, DecisionClosedOutcome); d == nil {
		t.Fatal("the outcome must be written to the decision log")
	}
}

// An explicit outcome is recorded in the archive step itself.
func TestClosedOutcomeArchiveRecordsExplicitOutcomeInSameStep(t *testing.T) {
	m := coMgr(t, ClosedOutcomeOn)
	p := coPRD(t, m, PRDCancelled)
	got, err := m.ArchiveWithOutcome(p.ID, "operator", OutcomeInput{Outcome: OutcomeDropped, Note: "scratch probe"})
	if err != nil {
		t.Fatalf("ArchiveWithOutcome: %v", err)
	}
	if got.Status != PRDArchived || got.ClosedOutcome != OutcomeDropped || got.ClosedOutcomeNote != "scratch probe" || got.ClosedOutcomeAt == nil {
		t.Fatalf("got status=%s outcome=%q note=%q at=%v", got.Status, got.ClosedOutcome, got.ClosedOutcomeNote, got.ClosedOutcomeAt)
	}
}

// delivered_via names the twin that delivered the work; the twin must exist
// and carry a shipped record. superseded needs the replacement to exist.
func TestClosedOutcomeTwinValidation(t *testing.T) {
	m := coMgr(t, ClosedOutcomeOn)
	twin := coPRD(t, m, PRDCompleted)
	a := coPRD(t, m, PRDCancelled)
	if _, err := m.ArchiveWithOutcome(a.ID, "operator", OutcomeInput{Outcome: OutcomeDelivered, DeliveredVia: twin.ID}); err == nil ||
		!strings.Contains(err.Error(), "shipped") {
		t.Fatalf("a twin with no shipped record cannot have delivered the work, got %v", err)
	}
	coShip(t, m, twin.ID)
	if _, err := m.ArchiveWithOutcome(a.ID, "operator", OutcomeInput{Outcome: OutcomeDelivered, DeliveredVia: a.ID}); err == nil {
		t.Fatal("a record cannot be its own twin")
	}
	if _, err := m.ArchiveWithOutcome(a.ID, "operator", OutcomeInput{Outcome: OutcomeDelivered, DeliveredVia: "nosuchid"}); err == nil {
		t.Fatal("an unknown twin must be refused")
	}
	if _, err := m.ArchiveWithOutcome(a.ID, "operator", OutcomeInput{Outcome: OutcomeSuperseded}); err == nil {
		t.Fatal("superseded must name its replacement")
	}
	if _, err := m.ArchiveWithOutcome(a.ID, "operator", OutcomeInput{Outcome: "bogus"}); err == nil {
		t.Fatal("an unknown outcome class must be refused")
	}
	got, err := m.ArchiveWithOutcome(a.ID, "operator", OutcomeInput{Outcome: OutcomeDelivered, DeliveredVia: twin.ID})
	if err != nil {
		t.Fatalf("delivered via a shipped twin: %v", err)
	}
	if got.DeliveredVia != twin.ID || got.ClosedOutcome != OutcomeDelivered {
		t.Fatalf("got outcome=%q via=%q", got.ClosedOutcome, got.DeliveredVia)
	}
}

// The archive step refuses an unmet hold, and a sweep never drops one.
func TestClosedOutcomeArchiveRefusesUnmetHold(t *testing.T) {
	future := time.Now().Add(48 * time.Hour)
	cases := map[string]func(p *PRD){
		"outcome held":         func(p *PRD) { p.ClosedOutcome = OutcomeHeld },
		"hold record unmet":    func(p *PRD) { p.Hold = &HoldState{Reason: "wait", Condition: "after 10.0", Until: &future} },
		"spec declares a hold": func(p *PRD) { p.Spec = "STATUS: HOLD until the operator says go" },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			m := coMgr(t, ClosedOutcomeOn)
			p := coPRD(t, m, PRDCancelled)
			m.Store().MutatePRD(p.ID, func(p *PRD) bool { set(p); return true })
			// Even an outcome the daemon could derive must not close a hold.
			coShipIfNeeded := name == "spec declares a hold"
			if coShipIfNeeded {
				m.Store().MutatePRD(p.ID, func(p *PRD) bool { p.ClosedOutcome = ""; return true })
			}
			_, err := m.Archive(p.ID, "supervisor-loop")
			if err == nil || !strings.Contains(err.Error(), "hold") {
				t.Fatalf("archive must refuse an unmet hold naming it, got %v", err)
			}
			if got := coGet(t, m, p.ID); got.Status != PRDCancelled {
				t.Fatalf("status = %s, the hold must survive", got.Status)
			}
		})
	}
}

// Once the operator records an explicit outcome the spec's own hold wording is
// resolved, and a hold that has expired no longer blocks.
func TestClosedOutcomeExplicitOutcomeResolvesSpecHold(t *testing.T) {
	m := coMgr(t, ClosedOutcomeOn)
	p := coPRD(t, m, PRDCancelled)
	m.Store().MutatePRD(p.ID, func(p *PRD) bool { p.Spec = "HOLD: build later"; return true })
	if _, err := m.ArchiveWithOutcome(p.ID, "operator", OutcomeInput{Outcome: OutcomeDropped, Note: "no longer wanted"}); err != nil {
		t.Fatalf("an explicit outcome resolves the spec hold: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	q := coPRD(t, m, PRDCancelled)
	m.Store().MutatePRD(q.ID, func(p *PRD) bool {
		p.Hold = &HoldState{Reason: "wait", Until: &past}
		p.ClosedOutcome = OutcomeDropped
		return true
	})
	if _, err := m.Archive(q.ID, "operator"); err != nil {
		t.Fatalf("an expired hold no longer blocks: %v", err)
	}
}

// Running, planning and held work is never archived, by any caller.
func TestClosedOutcomeNeverArchivesRunningOrHeldWork(t *testing.T) {
	for _, st := range []PRDStatus{PRDRunning, PRDPlanning, PRDHeld, PRDPaused, PRDApproved, PRDDraft, PRDBlocked, PRDFailed} {
		m := coMgr(t, ClosedOutcomeOn)
		p := coPRD(t, m, st)
		if _, err := m.ArchiveWithOutcome(p.ID, "supervisor-loop", OutcomeInput{Outcome: OutcomeDropped}); err == nil {
			t.Fatalf("status %s must never be archived", st)
		}
		if got := coGet(t, m, p.ID); got.Status != st {
			t.Fatalf("status %s was rewritten to %s", st, got.Status)
		}
	}
}

// Shadow only logs; off does nothing: both still archive.
func TestClosedOutcomeArchiveShadowAndOffStillArchive(t *testing.T) {
	for _, mode := range []string{ClosedOutcomeShadow, ClosedOutcomeOff, ""} {
		m := coMgr(t, mode)
		p := coPRD(t, m, PRDCompleted)
		if _, err := m.Archive(p.ID, "operator"); err != nil {
			t.Fatalf("mode %q must still archive: %v", mode, err)
		}
		if got := coGet(t, m, p.ID); got.Status != PRDArchived {
			t.Fatalf("mode %q: status %s", mode, got.Status)
		}
	}
}

// Cancel and reject record the outcome in the same step.
func TestClosedOutcomeCancelAndRejectRecordOutcome(t *testing.T) {
	m := coMgr(t, ClosedOutcomeOn)
	api := NewAPI(m)
	p := coPRD(t, m, PRDRunning)
	if err := api.Cancel(p.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got := coGet(t, m, p.ID)
	if got.Status != PRDCancelled || got.ClosedOutcome != OutcomeDropped {
		t.Fatalf("cancel of unfinished work: status=%s outcome=%q, want cancelled + dropped", got.Status, got.ClosedOutcome)
	}

	s := coPRD(t, m, PRDRunning)
	coShip(t, m, s.ID)
	if err := api.Cancel(s.ID); err != nil {
		t.Fatalf("Cancel shipped: %v", err)
	}
	if got := coGet(t, m, s.ID); got.ClosedOutcome != OutcomeDelivered {
		t.Fatalf("cancel of shipped work: outcome=%q, want delivered", got.ClosedOutcome)
	}

	h := coPRD(t, m, PRDRunning)
	if err := api.CancelWithOutcome(h.ID, OutcomeInput{Outcome: OutcomeHeld, Note: "until 10.0"}); err != nil {
		t.Fatalf("CancelWithOutcome: %v", err)
	}
	if got := coGet(t, m, h.ID); got.ClosedOutcome != OutcomeHeld || got.ClosedOutcomeNote != "until 10.0" {
		t.Fatalf("explicit cancel outcome = %q %q", got.ClosedOutcome, got.ClosedOutcomeNote)
	}

	r := coPRD(t, m, PRDNeedsReview)
	if _, err := m.Reject(r.ID, "operator", "not wanted"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if got := coGet(t, m, r.ID); got.ClosedOutcome != OutcomeDropped {
		t.Fatalf("reject: outcome=%q, want dropped", got.ClosedOutcome)
	}
}

// Setting the outcome of a closed record later (the labelling pass) works, and
// a running record cannot be labelled closed.
func TestClosedOutcomeSetOnClosedRecordOnly(t *testing.T) {
	m := coMgr(t, ClosedOutcomeOn)
	p := coPRD(t, m, PRDArchived)
	got, err := m.SetClosedOutcome(p.ID, "operator", OutcomeInput{Outcome: OutcomeDropped, Note: "old probe"})
	if err != nil || got.ClosedOutcome != OutcomeDropped {
		t.Fatalf("SetClosedOutcome on archived: %v %+v", err, got)
	}
	run := coPRD(t, m, PRDRunning)
	if _, err := m.SetClosedOutcome(run.ID, "operator", OutcomeInput{Outcome: OutcomeDropped}); err == nil {
		t.Fatal("a running record has no closed outcome to set")
	}
	if _, err := m.SetClosedOutcome(p.ID, "", OutcomeInput{Outcome: OutcomeDropped}); err == nil {
		t.Fatal("an actor is required")
	}
}
