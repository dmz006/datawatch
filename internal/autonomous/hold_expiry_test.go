package autonomous

// Hold expiry: an operator hold's look-again time passes.
//
// Test coverage:
//
//   - TestHoldExpiryRaisesOneAlertAcrossPasses — expiry raises exactly one
//     alert no matter how many passes run before, at or after the time;
//   - TestHoldExpiryDoesNotStartAutomaton — the status stays held, lane
//     fill still refuses it (operator_hold) and nothing is started;
//   - TestHoldExpirySurvivesRestart — the mark is persisted; a reload of
//     the same store raises nothing more;
//   - TestHoldExpiryNoUntilNeverExpires — a hold without a look-again time
//     never expires;
//   - TestHoldExpiryExtendRearms — extending the hold clears the expiry
//     mark and the next pass expires again at the new time.

import (
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	holdExpiryStart = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	holdExpiryUntil = holdExpiryStart.Add(time.Hour)
)

// holdExpiryRecorder is the fake alert sink the expiry tests assert on.
type holdExpiryRecorder struct {
	mu     sync.Mutex
	alerts []string // "level|title|body"
}

func (r *holdExpiryRecorder) fn(level, title, body string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alerts = append(r.alerts, level+"|"+title+"|"+body)
	return ""
}

func (r *holdExpiryRecorder) record() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.alerts...)
}

// holdExpiryRig builds a manager with a recorder alert sink and one running
// Automaton.
func holdExpiryRig(t *testing.T) (*Manager, *PRD, *holdExpiryRecorder) {
	t.Helper()
	m := newEmitManager(t, DefaultConfig(), nil)
	rec := &holdExpiryRecorder{}
	m.SetAlertSink(rec.fn)
	prd := newEmitPRD(t, m, PRDBlocked)
	if p, ok := m.Store().GetPRD(prd.ID); ok {
		p.BlockedReason = "judge said block"
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
	}
	prd.Title = "alpha"
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd, rec
}

func TestHoldExpiryRaisesOneAlertAcrossPasses(t *testing.T) {
	m, prd, rec := holdExpiryRig(t)
	if _, err := m.Hold(prd.ID, HoldRequest{Reason: "wait for the release", Until: &holdExpiryUntil}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	// Before the look-again time: nothing happens.
	if got := m.ExpireHolds(holdExpiryStart); len(got) != 0 {
		t.Fatalf("ExpireHolds before until = %v, want none", got)
	}
	if got := rec.record(); len(got) != 0 {
		t.Fatalf("alerts before until = %d, want 0", len(got))
	}

	// At the look-again time: it expires.
	if got := m.ExpireHolds(holdExpiryUntil); len(got) != 1 || got[0] != prd.ID {
		t.Fatalf("ExpireHolds at until = %v, want [%s]", got, prd.ID)
	}
	got := rec.record()
	if len(got) != 1 {
		t.Fatalf("alerts at until = %d, want 1", len(got))
	}
	level, rest, _ := strings.Cut(got[0], "|")
	title, body, _ := strings.Cut(rest, "|")
	if level != "warn" {
		t.Errorf("alert level = %q, want warn", level)
	}
	if title != "Automaton hold expired" {
		t.Errorf("alert title = %q, want \"Automaton hold expired\"", title)
	}
	for _, want := range []string{"alpha", "wait for the release", "release the hold", "new hold-until time"} {
		if !strings.Contains(body, want) {
			t.Errorf("alert body %q missing %q", body, want)
		}
	}

	// After the look-again time, in every later pass: still only one alert.
	if got := m.ExpireHolds(holdExpiryUntil.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("ExpireHolds after until = %v, want none", got)
	}
	if got := m.ExpireHolds(holdExpiryUntil.Add(2 * time.Hour)); len(got) != 0 {
		t.Fatalf("second pass after until = %v, want none", got)
	}
	if got := rec.record(); len(got) != 1 {
		t.Fatalf("alerts across passes = %d, want 1", len(got))
	}

	// The record: still held, ExpiredAt set, one hold_expired decision.
	p, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("Automaton missing")
	}
	if p.Status != PRDHeld {
		t.Fatalf("Status = %q, want held", p.Status)
	}
	if p.Hold == nil || p.Hold.ExpiredAt == nil || !p.Hold.ExpiredAt.Equal(holdExpiryUntil) {
		t.Fatalf("Hold.ExpiredAt = %+v, want %s", p.Hold, holdExpiryUntil)
	}
	if n := holdDecisionCount(p, holdExpiredDecisionKind); n != 1 {
		t.Fatalf("hold_expired decisions = %d, want 1", n)
	}
}

func TestHoldExpiryDoesNotStartAutomaton(t *testing.T) {
	m, prd, rec := holdExpiryRig(t)
	if _, err := m.Hold(prd.ID, HoldRequest{Reason: "wait for the release", Until: &holdExpiryUntil}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if got := m.ExpireHolds(holdExpiryUntil); len(got) != 1 {
		t.Fatalf("ExpireHolds = %v, want [%s]", got, prd.ID)
	}

	p, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("Automaton missing")
	}
	if p.Status != PRDHeld {
		t.Fatalf("Status = %q, want held", p.Status)
	}
	// Lane fill reads this gate: a held Automaton is refused.
	code, _ := PRDHoldCode(p, nil)
	if code != LaneFillReasonOperatorHold {
		t.Fatalf("PRDHoldCode = %q, want %q", code, LaneFillReasonOperatorHold)
	}
	// No run happened.
	if n := holdDecisionCount(p, "run"); n != 0 {
		t.Fatalf("run decisions = %d, want 0", n)
	}
	if got := rec.record(); len(got) != 1 {
		t.Fatalf("alerts = %d, want 1", len(got))
	}
}

func TestHoldExpirySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	rec1 := &holdExpiryRecorder{}
	m1.SetAlertSink(rec1.fn)
	prd := newEmitPRD(t, m1, PRDBlocked)
	if p, ok := m1.Store().GetPRD(prd.ID); ok {
		p.BlockedReason = "judge said block"
		if err := m1.Store().SavePRD(p); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
	}
	until := holdExpiryUntil
	if _, err := m1.Hold(prd.ID, HoldRequest{Reason: "wait for the release", Until: &until}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if got := m1.ExpireHolds(holdExpiryUntil); len(got) != 1 {
		t.Fatalf("ExpireHolds (first manager) = %v, want one id", got)
	}
	if got := rec1.record(); len(got) != 1 {
		t.Fatalf("first manager alerts = %d, want 1", len(got))
	}

	// A restart reloads the same store; a later pass raises nothing more.
	m2, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager (restart): %v", err)
	}
	rec2 := &holdExpiryRecorder{}
	m2.SetAlertSink(rec2.fn)
	if got := m2.ExpireHolds(holdExpiryUntil.Add(2 * time.Hour)); len(got) != 0 {
		t.Fatalf("ExpireHolds (restarted manager) = %v, want none", got)
	}
	if got := rec2.record(); len(got) != 0 {
		t.Fatalf("restarted manager alerts = %d, want 0", len(got))
	}
	p, ok := m2.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("Automaton missing after restart")
	}
	if p.Status != PRDHeld || p.Hold == nil || p.Hold.ExpiredAt == nil {
		t.Fatalf("after restart: Status = %q, Hold = %+v, want held with ExpiredAt", p.Status, p.Hold)
	}
	if n := holdDecisionCount(p, holdExpiredDecisionKind); n != 1 {
		t.Fatalf("hold_expired decisions after restart = %d, want 1", n)
	}
}

func TestHoldExpiryNoUntilNeverExpires(t *testing.T) {
	m, prd, rec := holdExpiryRig(t)
	if _, err := m.Hold(prd.ID, HoldRequest{Reason: "wait for the release"}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	at := holdExpiryStart.Add(30 * 24 * time.Hour)
	if got := m.ExpireHolds(at); len(got) != 0 {
		t.Fatalf("ExpireHolds on a hold without until = %v, want none", got)
	}
	if got := rec.record(); len(got) != 0 {
		t.Fatalf("alerts = %d, want 0", len(got))
	}
	p, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("Automaton missing")
	}
	if p.Status != PRDHeld {
		t.Fatalf("Status = %q, want held", p.Status)
	}
	if p.Hold == nil || p.Hold.Until != nil || p.Hold.ExpiredAt != nil {
		t.Fatalf("Hold = %+v, want Until nil and ExpiredAt nil", p.Hold)
	}
	if n := holdDecisionCount(p, holdExpiredDecisionKind); n != 0 {
		t.Fatalf("hold_expired decisions = %d, want 0", n)
	}
}

func TestHoldExpiryExtendRearms(t *testing.T) {
	m, prd, rec := holdExpiryRig(t)
	if _, err := m.Hold(prd.ID, HoldRequest{Reason: "wait for the release", Until: &holdExpiryUntil}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if got := m.ExpireHolds(holdExpiryUntil); len(got) != 1 {
		t.Fatalf("first ExpireHolds = %v, want one id", got)
	}
	if got := rec.record(); len(got) != 1 {
		t.Fatalf("alerts after first expiry = %d, want 1", len(got))
	}

	// The operator extends the hold: the expiry mark clears.
	newUntil := holdExpiryStart.Add(3 * time.Hour)
	if _, err := m.ExtendHold(prd.ID, HoldRequest{Reason: "the release slipped", Until: &newUntil}, "operator"); err != nil {
		t.Fatalf("ExtendHold: %v", err)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	if p.Hold == nil || p.Hold.ExpiredAt != nil {
		t.Fatalf("after extend: Hold = %+v, want ExpiredAt nil", p.Hold)
	}
	if p.Hold.Until == nil || !p.Hold.Until.Equal(newUntil) {
		t.Fatalf("after extend: Until = %v, want %v", p.Hold.Until, newUntil)
	}

	// The next pass, before the new time: nothing.
	beforeNew := holdExpiryUntil.Add(2*time.Hour - time.Minute)
	if got := m.ExpireHolds(beforeNew); len(got) != 0 {
		t.Fatalf("ExpireHolds before new until = %v, want none", got)
	}
	if got := rec.record(); len(got) != 1 {
		t.Fatalf("alerts before new until = %d, want 1", len(got))
	}

	// At the new time: it expires again, one more alert.
	if got := m.ExpireHolds(newUntil); len(got) != 1 {
		t.Fatalf("ExpireHolds at new until = %v, want one id", got)
	}
	if got := rec.record(); len(got) != 2 {
		t.Fatalf("alerts after re-expiry = %d, want 2", len(got))
	}
	p, _ = m.Store().GetPRD(prd.ID)
	if n := holdDecisionCount(p, holdExpiredDecisionKind); n != 2 {
		t.Fatalf("hold_expired decisions = %d, want 2", n)
	}
}
