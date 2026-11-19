package autonomous

import (
	"strings"
	"testing"
	"time"
)

// holdClockStart is the fake clock's first reading for the hold tests.
var holdClockStart = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// holdClock is a fake clock the hold tests advance by hand.
type holdClock struct{ t time.Time }

func (c *holdClock) now() time.Time { return c.t }

// newHoldManager returns a manager reading a fake clock.
func newHoldManager(t *testing.T) (*Manager, *holdClock) {
	t.Helper()
	m := newEmitManager(t, DefaultConfig(), nil)
	clk := &holdClock{t: holdClockStart}
	m.now = clk.now
	return m, clk
}

// newOperatorHoldPRD returns an Automaton forced to status, with its task in a
// distinctive state so the tests can prove a hold leaves tasks alone.
func newOperatorHoldPRD(t *testing.T, m *Manager, status PRDStatus, blockedReason string) *PRD {
	t.Helper()
	prd := newEmitPRD(t, m, PRDDraft)
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Story[0].Tasks[0].Status = TaskBlocked
	prd.Status = status
	prd.BlockedReason = blockedReason
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	return got
}

// holdTaskStatuses lists every task status of the Automaton in order.
func holdTaskStatuses(p *PRD) []TaskStatus {
	var out []TaskStatus
	for _, s := range p.Story {
		for _, tk := range s.Tasks {
			out = append(out, tk.Status)
		}
	}
	return out
}

func holdDecisionCount(p *PRD, kind string) int {
	n := 0
	for _, d := range p.Decisions {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

func TestHoldFromBlockedKeepsTasksAndRestoresReasonOnRelease(t *testing.T) {
	m, _ := newHoldManager(t)
	p := newOperatorHoldPRD(t, m, PRDBlocked, "judge said block")
	before := holdTaskStatuses(p)

	held, err := m.Hold(p.ID, HoldRequest{Reason: "  waiting for the release  ", Condition: "until after the release"}, "operator")
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if held.Status != PRDHeld {
		t.Fatalf("Status = %q, want held", held.Status)
	}
	h := held.Hold
	if h == nil {
		t.Fatal("Hold record is nil")
	}
	if h.Reason != "waiting for the release" {
		t.Errorf("Reason = %q, want trimmed", h.Reason)
	}
	if h.Condition != "until after the release" || h.From != PRDBlocked || h.HeldBy != "operator" ||
		h.PriorBlockedReason != "judge said block" || !h.HeldAt.Equal(holdClockStart) {
		t.Errorf("hold record = %+v", h)
	}
	if held.BlockedReason != "" {
		t.Errorf("BlockedReason = %q while held, want empty", held.BlockedReason)
	}
	if got := holdTaskStatuses(held); len(got) != len(before) || got[0] != before[0] {
		t.Errorf("task statuses changed by hold: %v -> %v", before, got)
	}

	released, err := m.ReleaseHold(p.ID, "operator", "release is out")
	if err != nil {
		t.Fatalf("ReleaseHold: %v", err)
	}
	if released.Status != PRDBlocked || released.BlockedReason != "judge said block" || released.Hold != nil {
		t.Errorf("after release: status=%q reason=%q hold=%v", released.Status, released.BlockedReason, released.Hold)
	}
	if got := holdTaskStatuses(released); got[0] != before[0] {
		t.Errorf("task statuses changed by release: %v -> %v", before, got)
	}
}

func TestHoldAllowedFromEachStatus(t *testing.T) {
	for _, from := range []PRDStatus{PRDDraft, PRDNeedsReview, PRDApproved, PRDRevisionsAsked, PRDPaused, PRDFailed} {
		t.Run(string(from), func(t *testing.T) {
			m, _ := newHoldManager(t)
			p := newOperatorHoldPRD(t, m, from, "")
			held, err := m.Hold(p.ID, HoldRequest{Reason: "wait"}, "operator")
			if err != nil {
				t.Fatalf("Hold from %s: %v", from, err)
			}
			if held.Status != PRDHeld || held.Hold.From != from || held.Hold.PriorBlockedReason != "" {
				t.Fatalf("status=%q hold=%+v", held.Status, held.Hold)
			}
			released, err := m.ReleaseHold(p.ID, "operator", "")
			if err != nil {
				t.Fatalf("ReleaseHold: %v", err)
			}
			if released.Status != from {
				t.Fatalf("released to %q, want %q", released.Status, from)
			}
		})
	}
}

func TestHoldRefusedFromOtherStatuses(t *testing.T) {
	for _, from := range []PRDStatus{PRDRunning, PRDPlanning, PRDCompleted, PRDCancelled, PRDRejected, PRDArchived} {
		t.Run(string(from), func(t *testing.T) {
			m, _ := newHoldManager(t)
			p := newOperatorHoldPRD(t, m, from, "")
			_, err := m.Hold(p.ID, HoldRequest{Reason: "wait"}, "operator")
			if err == nil {
				t.Fatalf("Hold from %s was accepted", from)
			}
			msg := err.Error()
			if !strings.Contains(msg, `"`+string(from)+`"`) || !strings.Contains(msg, "draft") || !strings.Contains(msg, "failed") {
				t.Errorf("error does not name the status and what is allowed: %s", msg)
			}
			if strings.Contains(msg, "PRD") {
				t.Errorf("error says PRD: %s", msg)
			}
			got, _ := m.Store().GetPRD(p.ID)
			if got.Status != from || got.Hold != nil || holdDecisionCount(got, "hold") != 0 {
				t.Errorf("refused hold changed the Automaton: status=%q hold=%v", got.Status, got.Hold)
			}
		})
	}
}

func TestHoldRefusedWhenAlreadyHeld(t *testing.T) {
	m, _ := newHoldManager(t)
	p := newOperatorHoldPRD(t, m, PRDDraft, "")
	if _, err := m.Hold(p.ID, HoldRequest{Reason: "first"}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	_, err := m.Hold(p.ID, HoldRequest{Reason: "second"}, "operator")
	if err == nil {
		t.Fatal("second Hold accepted")
	}
	if !strings.Contains(err.Error(), "already held") || !strings.Contains(err.Error(), "extend") {
		t.Errorf("error = %v", err)
	}
	got, _ := m.Store().GetPRD(p.ID)
	if got.Hold.Reason != "first" || holdDecisionCount(got, "hold") != 1 {
		t.Errorf("second hold changed the record: %+v", got.Hold)
	}
}

func TestHoldReasonRules(t *testing.T) {
	m, _ := newHoldManager(t)
	p := newOperatorHoldPRD(t, m, PRDDraft, "")
	for _, reason := range []string{"", "   \t\n"} {
		if _, err := m.Hold(p.ID, HoldRequest{Reason: reason}, "operator"); err == nil || !strings.Contains(err.Error(), "reason") {
			t.Errorf("reason %q: err = %v, want a reason error", reason, err)
		}
	}
	if _, err := m.Hold(p.ID, HoldRequest{Reason: strings.Repeat("x", 501)}, "operator"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("over-long reason: err = %v, want a 500 limit error", err)
	}
	// Characters are counted, not bytes: 500 multi-byte characters pass.
	if _, err := m.Hold(p.ID, HoldRequest{Reason: strings.Repeat("é", 500)}, "operator"); err != nil {
		t.Errorf("500-character reason refused: %v", err)
	}
	got, _ := m.Store().GetPRD(p.ID)
	if got.Status != PRDHeld || holdDecisionCount(got, "hold") != 1 {
		t.Errorf("only the valid hold should have landed: status=%q decisions=%d", got.Status, holdDecisionCount(got, "hold"))
	}
}

func TestHoldUntilMustBeInTheFuture(t *testing.T) {
	m, clk := newHoldManager(t)
	p := newOperatorHoldPRD(t, m, PRDDraft, "")
	past := clk.t.Add(-time.Minute)
	now := clk.t
	for name, u := range map[string]*time.Time{"past": &past, "now": &now} {
		if _, err := m.Hold(p.ID, HoldRequest{Reason: "wait", Until: u}, "operator"); err == nil || !strings.Contains(err.Error(), "future") {
			t.Errorf("until %s: err = %v, want a future-time error", name, err)
		}
	}
	if got, _ := m.Store().GetPRD(p.ID); got.Status != PRDDraft {
		t.Fatalf("refused hold changed status to %q", got.Status)
	}
	until := clk.t.Add(2 * time.Hour)
	held, err := m.Hold(p.ID, HoldRequest{Reason: "wait", Until: &until}, "operator")
	if err != nil {
		t.Fatalf("Hold with a future time: %v", err)
	}
	if held.Hold.Until == nil || !held.Hold.Until.Equal(until) {
		t.Errorf("Until = %v, want %v", held.Hold.Until, until)
	}
	// The stored time is a copy, not the caller's pointer.
	until = until.Add(time.Hour)
	if got, _ := m.Store().GetPRD(p.ID); got.Hold.Until.Equal(until) {
		t.Error("hold shares the caller's Until pointer")
	}
}

func TestHoldExtendReplacesFieldsAndClearsExpiry(t *testing.T) {
	m, clk := newHoldManager(t)
	p := newOperatorHoldPRD(t, m, PRDNeedsReview, "")
	first := clk.t.Add(time.Hour)
	if _, err := m.Hold(p.ID, HoldRequest{Reason: "old reason", Condition: "old condition", Until: &first}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	// Time passes and the look-again time expires: the expiry mark is set.
	clk.t = clk.t.Add(2 * time.Hour)
	stored, _ := m.Store().GetPRD(p.ID)
	exp := clk.t
	stored.Hold.ExpiredAt = &exp
	if err := m.Store().SavePRD(stored); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	later := clk.t.Add(24 * time.Hour)
	got, err := m.ExtendHold(p.ID, HoldRequest{Reason: "new reason", Condition: "new condition", Until: &later}, "operator2")
	if err != nil {
		t.Fatalf("ExtendHold: %v", err)
	}
	h := got.Hold
	if got.Status != PRDHeld || h.Reason != "new reason" || h.Condition != "new condition" ||
		h.Until == nil || !h.Until.Equal(later) || h.ExpiredAt != nil {
		t.Fatalf("after extend: status=%q hold=%+v", got.Status, h)
	}
	if h.From != PRDNeedsReview || h.HeldBy != "operator" || !h.HeldAt.Equal(holdClockStart) {
		t.Errorf("extend changed the original hold facts: %+v", h)
	}

	// A field left out is kept.
	got, err = m.ExtendHold(p.ID, HoldRequest{Reason: "only reason"}, "operator")
	if err != nil {
		t.Fatalf("ExtendHold reason only: %v", err)
	}
	if got.Hold.Reason != "only reason" || got.Hold.Condition != "new condition" || !got.Hold.Until.Equal(later) {
		t.Errorf("unsupplied fields were not kept: %+v", got.Hold)
	}

	// Extend applies the same rules as Hold.
	past := clk.t.Add(-time.Second)
	if _, err := m.ExtendHold(p.ID, HoldRequest{Until: &past}, "operator"); err == nil {
		t.Error("extend with a past time was accepted")
	}
	if _, err := m.ExtendHold(p.ID, HoldRequest{Reason: strings.Repeat("x", 501)}, "operator"); err == nil {
		t.Error("extend with an over-long reason was accepted")
	}
}

func TestHoldExtendAndReleaseRefusedWhenNotHeld(t *testing.T) {
	m, _ := newHoldManager(t)
	p := newOperatorHoldPRD(t, m, PRDApproved, "")
	_, err := m.ExtendHold(p.ID, HoldRequest{Reason: "x"}, "operator")
	if err == nil || !strings.Contains(err.Error(), `"approved"`) || !strings.Contains(err.Error(), "held") {
		t.Errorf("extend on approved: err = %v", err)
	}
	_, err = m.ReleaseHold(p.ID, "operator", "")
	if err == nil || !strings.Contains(err.Error(), `"approved"`) || !strings.Contains(err.Error(), "held") {
		t.Errorf("release on approved: err = %v", err)
	}
	if strings.Contains(err.Error(), "PRD") {
		t.Errorf("error says PRD: %v", err)
	}
	if _, err := m.Hold("no-such-id", HoldRequest{Reason: "x"}, "operator"); err == nil || strings.Contains(err.Error(), "PRD") {
		t.Errorf("unknown id on Hold: err = %v", err)
	}
	if _, err := m.ExtendHold("no-such-id", HoldRequest{}, "operator"); err == nil {
		t.Error("unknown id on ExtendHold accepted")
	}
	if _, err := m.ReleaseHold("no-such-id", "operator", ""); err == nil {
		t.Error("unknown id on ReleaseHold accepted")
	}
	got, _ := m.Store().GetPRD(p.ID)
	if got.Status != PRDApproved || got.Hold != nil || holdDecisionCount(got, "hold_extend") != 0 || holdDecisionCount(got, "hold_release") != 0 {
		t.Errorf("refused operations changed the Automaton: status=%q hold=%v", got.Status, got.Hold)
	}
}

func TestHoldSurvivesStoreSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	clk := &holdClock{t: holdClockStart}
	m.now = clk.now
	p := newOperatorHoldPRD(t, m, PRDBlocked, "judge said block")
	until := clk.t.Add(3 * time.Hour)
	if _, err := m.Hold(p.ID, HoldRequest{Reason: "wait", Condition: "after the release", Until: &until}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	got, ok := reloaded.GetPRD(p.ID)
	if !ok {
		t.Fatal("Automaton missing after reload")
	}
	if got.Status != PRDHeld || got.Hold == nil {
		t.Fatalf("after reload: status=%q hold=%v", got.Status, got.Hold)
	}
	h := got.Hold
	if h.Reason != "wait" || h.Condition != "after the release" || h.From != PRDBlocked ||
		h.PriorBlockedReason != "judge said block" || h.HeldBy != "operator" ||
		h.Until == nil || !h.Until.Equal(until) || !h.HeldAt.Equal(holdClockStart) || h.ExpiredAt != nil {
		t.Errorf("hold record changed over save and reload: %+v", h)
	}
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q after reload; the store clears it while held", got.BlockedReason)
	}
}

func TestHoldStoreClearKeepsHoldRecordAndReason(t *testing.T) {
	// The store clears BlockedReason whenever the status is not blocked. The
	// reason a hold needs lives in Hold.PriorBlockedReason, which that clear
	// must never touch.
	p := &PRD{
		Status:        PRDHeld,
		BlockedReason: "stale",
		Hold:          &HoldState{Reason: "wait", From: PRDBlocked, PriorBlockedReason: "judge said block"},
	}
	clearStaleBlockedReason(p)
	if p.BlockedReason != "" {
		t.Errorf("BlockedReason = %q on a held Automaton, want cleared", p.BlockedReason)
	}
	if p.Hold == nil || p.Hold.PriorBlockedReason != "judge said block" {
		t.Errorf("clear lost the hold's own reason: %+v", p.Hold)
	}
}

func TestHoldOneDecisionEntryPerOperation(t *testing.T) {
	m, clk := newHoldManager(t)
	p := newOperatorHoldPRD(t, m, PRDFailed, "")
	until := clk.t.Add(time.Hour)
	if _, err := m.Hold(p.ID, HoldRequest{Reason: "wait", Until: &until}, "alice"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	later := clk.t.Add(2 * time.Hour)
	if _, err := m.ExtendHold(p.ID, HoldRequest{Until: &later}, "bob"); err != nil {
		t.Fatalf("ExtendHold: %v", err)
	}
	got, err := m.ReleaseHold(p.ID, "carol", "all clear")
	if err != nil {
		t.Fatalf("ReleaseHold: %v", err)
	}
	want := map[string]string{"hold": "alice", "hold_extend": "bob", "hold_release": "carol"}
	for kind, actor := range want {
		if n := holdDecisionCount(got, kind); n != 1 {
			t.Errorf("%s decisions = %d, want 1", kind, n)
			continue
		}
		d := lastDecision(got, kind)
		if d.Actor != actor {
			t.Errorf("%s actor = %q, want %q", kind, d.Actor, actor)
		}
		if !d.At.Equal(holdClockStart) {
			t.Errorf("%s time = %v, want the fake clock", kind, d.At)
		}
	}
	if note := lastDecision(got, "hold_release").Note; !strings.Contains(note, "all clear") || !strings.Contains(note, "failed") {
		t.Errorf("release note = %q, want the operator note and the restored status", note)
	}
	if note := lastDecision(got, "hold").Note; !strings.Contains(note, "wait") {
		t.Errorf("hold note = %q, want the reason", note)
	}
}

func TestHoldOneStatusEventPerOperation(t *testing.T) {
	m, clk := newHoldManager(t)
	p := newOperatorHoldPRD(t, m, PRDDraft, "")
	other := newOperatorHoldPRD(t, m, PRDNeedsReview, "")
	c := &collector{}
	defer m.Subscribe(LifecycleFilter{}, c.add)()

	if _, err := m.Hold(p.ID, HoldRequest{Reason: "wait"}, "op"); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	later := clk.t.Add(time.Hour)
	if _, err := m.ExtendHold(p.ID, HoldRequest{Until: &later}, "op"); err != nil {
		t.Fatalf("ExtendHold: %v", err)
	}
	// Refused operations emit nothing.
	if _, err := m.Hold(p.ID, HoldRequest{Reason: "again"}, "op"); err == nil {
		t.Fatal("second Hold accepted")
	}
	if _, err := m.ReleaseHold(p.ID, "op", "done"); err != nil {
		t.Fatalf("ReleaseHold: %v", err)
	}
	if _, err := m.ReleaseHold(p.ID, "op", "again"); err == nil {
		t.Fatal("second ReleaseHold accepted")
	}
	// Sentinel: events are delivered in order, so once it arrives every
	// earlier event has been delivered.
	if _, err := m.Reject(other.ID, "op", "x"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	waitFor(t, "sentinel reject event", func() bool {
		for _, ev := range c.snapshot() {
			if ev.PRDID == other.ID && ev.Kind == PRDStatusKind(PRDRejected) {
				return true
			}
		}
		return false
	})

	type seen struct {
		kind, old, new, decision string
	}
	var got []seen
	for _, ev := range c.snapshot() {
		if ev.PRDID != p.ID || ev.Decision == nil || !strings.HasPrefix(ev.Decision.Kind, "hold") {
			continue
		}
		got = append(got, seen{ev.Kind, ev.OldStatus, ev.NewStatus, ev.Decision.Kind})
	}
	want := []seen{
		{PRDStatusKind(PRDHeld), "draft", "held", "hold"},
		{PRDStatusKind(PRDHeld), "held", "held", "hold_extend"},
		{PRDStatusKind(PRDDraft), "held", "draft", "hold_release"},
	}
	if len(got) != len(want) {
		t.Fatalf("hold status events = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHoldKindIsRegisteredAndNotNotifiedByDefault(t *testing.T) {
	kind := PRDStatusKind(PRDHeld)
	if kind != "prd.held" {
		t.Fatalf("kind = %q", kind)
	}
	if !IsKnownEventKind(kind) {
		t.Errorf("%s is not a known lifecycle event kind", kind)
	}
	var desc string
	for _, k := range ListEventKinds() {
		if k.Kind == kind {
			desc = k.Description
		}
	}
	if want := "An Automaton was put on hold by the operator; it waits by decision and nothing starts it."; desc != want {
		t.Errorf("description = %q", desc)
	}
	for _, k := range DefaultNotifyEvents() {
		if k == kind {
			t.Errorf("%s is in the default notify set", kind)
		}
	}
	if err := ValidateNotifyEvents([]string{kind}); err != nil {
		t.Errorf("%s is not a valid notify event: %v", kind, err)
	}
}

func TestHoldAPIWrappersDoNotStartAnExecutor(t *testing.T) {
	m, clk := newHoldManager(t)
	a := &API{M: m}
	p := newOperatorHoldPRD(t, m, PRDDraft, "")
	got, err := a.Hold(p.ID, HoldRequest{Reason: "wait"}, "op")
	if err != nil {
		t.Fatalf("API.Hold: %v", err)
	}
	if held := got.(*PRD); held.Status != PRDHeld {
		t.Fatalf("API.Hold status = %q", held.Status)
	}
	later := clk.t.Add(time.Hour)
	if _, err := a.ExtendHold(p.ID, HoldRequest{Until: &later}, "op"); err != nil {
		t.Fatalf("API.ExtendHold: %v", err)
	}
	got, err = a.ReleaseHold(p.ID, "op", "go")
	if err != nil {
		t.Fatalf("API.ReleaseHold: %v", err)
	}
	if rel := got.(*PRD); rel.Status != PRDDraft {
		t.Fatalf("API.ReleaseHold status = %q, want draft (nothing started it)", rel.Status)
	}
	if _, err := a.Hold("no-such-id", HoldRequest{Reason: "x"}, "op"); err == nil {
		t.Error("API.Hold on an unknown id accepted")
	}
}
