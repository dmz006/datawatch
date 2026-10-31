package autonomous

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const holdItemTitle = "wait for the design"

// holdHarness is a control harness over three approved Automatons whose
// Automation is pending item m0 -> Automaton m1 -> Automaton m2, still a draft.
func holdHarness(t *testing.T) (*coordHarness, *PendingItem, *Automation) {
	t.Helper()
	h, _ := newControlHarness(t, 3)
	item := newPendingItem(t, h.m, holdItemTitle)
	a, err := h.m.CreateAutomation(AutomationInput{
		Name: "hold", EntryMemberID: "m0",
		Members: []AutomationMember{
			pendingMember("m0", item.ID, "m1"),
			h.cm(1, "m2", "", ""),
			h.cm(2, "", "", ""),
		},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	return h, item, a
}

// activateHeld activates the draft and returns the held Automation.
func activateHeld(t *testing.T, h *coordHarness, a *Automation) *Automation {
	t.Helper()
	got, err := h.m.ActivateAutomation(a.ID, "operator", 0)
	if err != nil {
		t.Fatalf("ActivateAutomation: %v", err)
	}
	return got
}

func wantNoRuns(t *testing.T, h *coordHarness) {
	t.Helper()
	if runs, resumes := h.fake.counts(); len(runs) != 0 || len(resumes) != 0 {
		t.Fatalf("nothing should start: runs %v resumes %v", runs, resumes)
	}
}

func lastHoldDecision(a *Automation, kind string) (Decision, bool) {
	for i := len(a.Decisions) - 1; i >= 0; i-- {
		if a.Decisions[i].Kind == kind {
			return a.Decisions[i], true
		}
	}
	return Decision{}, false
}

func holdReasonFor(item *PendingItem) string {
	return `member "` + item.Title + `" is a pending item waiting for an Automaton to be attached (item ` + item.ID + `); attach one to continue`
}

// attachItem converts member m0 in place into an ordinary Automaton member over
// prds[0], as an attach does, and marks the item attached.
func attachItem(t *testing.T, h *coordHarness, item *PendingItem, a *Automation) {
	t.Helper()
	if _, err := h.m.pendingItems.Update(item.ID, func(p *PendingItem) error {
		p.State, p.AutomatonID = PendingItemAttached, h.prds[0].ID
		return nil
	}); err != nil {
		t.Fatalf("attach item: %v", err)
	}
	if _, err := h.m.automations.Update(a.ID, 0, func(cur *Automation) error {
		m0 := automationMember(cur, "m0")
		m0.Kind, m0.PRDID = MemberKindPRD, h.prds[0].ID
		return nil
	}); err != nil {
		t.Fatalf("convert member: %v", err)
	}
}

func TestPendingItemHoldHoldsUntilAttached(t *testing.T) {
	h, item, a := holdHarness(t)
	got := activateHeld(t, h, a)
	if got.Status != AutomationBlocked || got.CurrentMemberID != "m0" {
		t.Fatalf("status %s cursor %q, want blocked at m0", got.Status, got.CurrentMemberID)
	}
	if m0 := automationMember(got, "m0"); m0.Status != MemberPending || m0.StartedAt != nil {
		t.Fatalf("m0 %s started %v, want pending and not started", m0.Status, m0.StartedAt)
	}
	d, ok := lastHoldDecision(got, "blocked")
	if !ok || d.Note != holdReasonFor(item) {
		t.Fatalf("blocked decision %+v", d)
	}
	wantNoRuns(t, h)

	// The operator attaches an Automaton: the member is converted in place and
	// a resume starts it as an ordinary Automaton member.
	attachItem(t, h, item, got)
	cur, _ := h.m.automations.Get(a.ID)
	resumed, err := h.m.ResumeAutomation(a.ID, "operator", cur.Version)
	if err != nil {
		t.Fatalf("ResumeAutomation: %v", err)
	}
	if resumed.Status != AutomationActive || resumed.IdleSince != nil {
		t.Fatalf("status %s idle %v after resume", resumed.Status, resumed.IdleSince)
	}
	m0 := automationMember(resumed, "m0")
	if m0.Status != MemberRunning || m0.PendingItemID != item.ID || m0.PRDID != h.prds[0].ID {
		t.Fatalf("m0 %+v", m0)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[0].ID {
		t.Fatalf("runs %v, want only the attached Automaton", runs)
	}
}

func TestPendingItemHoldSuccessorStaysBlocked(t *testing.T) {
	h, _, a := holdHarness(t)
	activateHeld(t, h, a)
	// The successor's Automaton ends while the item is still waiting: the
	// Automation neither advances nor starts anything.
	h.setStatus(1, PRDCompleted)
	h.event(1, PRDCompleted)
	got := h.get(a)
	if got.Status != AutomationBlocked || got.CurrentMemberID != "m0" {
		t.Fatalf("status %s cursor %q, want blocked at m0", got.Status, got.CurrentMemberID)
	}
	for _, id := range []string{"m1", "m2"} {
		if mem := automationMember(got, id); mem.Status != MemberPending && mem.Status != "" {
			t.Fatalf("%s is %s, want not started", id, mem.Status)
		}
	}
	wantNoRuns(t, h)
}

func TestPendingItemHoldSkipPolicyPassesThrough(t *testing.T) {
	h, item, a := holdHarness(t)
	h.m.cfg.Automation.PendingItemPolicy = config.PendingItemPolicySkip
	got := activateHeld(t, h, a)
	if got.Status != AutomationActive || got.CurrentMemberID != "m1" {
		t.Fatalf("status %s cursor %q, want active at m1", got.Status, got.CurrentMemberID)
	}
	m0 := automationMember(got, "m0")
	if m0.Status != MemberSkipped || m0.Outcome != "skipped_pending_item" || m0.EdgeTaken != edgeOnComplete || m0.FinishedAt == nil {
		t.Fatalf("m0 %+v", m0)
	}
	if m1 := automationMember(got, "m1"); m1.Status != MemberRunning {
		t.Fatalf("m1 %s, want running", m1.Status)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs %v, want [%s]", runs, h.prds[1].ID)
	}
	d, ok := lastHoldDecision(got, "pending_item")
	if !ok || !strings.Contains(d.Note, item.ID) || !strings.Contains(d.Note, "skipped") {
		t.Fatalf("decision %+v", d)
	}
	if p, _ := h.m.pendingItemByID(item.ID); p.State != PendingItemWaiting {
		t.Fatalf("the item itself must stay %s, got %s", PendingItemWaiting, p.State)
	}
	if h.alertCount("warn|Automation blocked") != 0 {
		t.Fatal("skip policy must not block")
	}
}

func TestPendingItemHoldDoneItemReleasesHold(t *testing.T) {
	h, item, a := holdHarness(t)
	got := activateHeld(t, h, a)
	if got.Status != AutomationBlocked {
		t.Fatalf("status %s, want blocked", got.Status)
	}
	// The registry reports the work done (for example a file registry did).
	if _, err := h.m.pendingItems.Update(item.ID, func(p *PendingItem) error {
		p.State = PendingItemDone
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cur, _ := h.m.automations.Get(a.ID)
	resumed, err := h.m.ResumeAutomation(a.ID, "operator", cur.Version)
	if err != nil {
		t.Fatalf("ResumeAutomation: %v", err)
	}
	if resumed.Status != AutomationActive || resumed.CurrentMemberID != "m1" {
		t.Fatalf("status %s cursor %q, want active at m1", resumed.Status, resumed.CurrentMemberID)
	}
	m0 := automationMember(resumed, "m0")
	if m0.Status != MemberCompleted || m0.Outcome != "pending_item_done" || m0.EdgeTaken != edgeOnComplete {
		t.Fatalf("m0 %+v", m0)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs %v, want [%s]", runs, h.prds[1].ID)
	}
}

func TestPendingItemHoldSurvivesRestartReconcile(t *testing.T) {
	h, item, a := holdHarness(t)
	activateHeld(t, h, a)

	// A fresh coordinator over the same store, as after a daemon restart.
	restarted := NewCoordinator(h.m, h.fake)
	restarted.alertFn = func(level, title, body string) string { return "" }
	restarted.auditFn = func(string, map[string]any) {}
	restarted.reconcile()
	got := h.get(a)
	if got.Status != AutomationBlocked || got.CurrentMemberID != "m0" {
		t.Fatalf("status %s cursor %q, want still blocked at m0", got.Status, got.CurrentMemberID)
	}
	wantNoRuns(t, h)

	// A crash between the cursor move and the block write leaves the
	// Automation active on the waiting member: reconcile holds it again and
	// never tries to start the member as an Automaton.
	if _, err := h.m.automations.Update(a.ID, 0, func(cur *Automation) error {
		cur.Status, cur.IdleSince = AutomationActive, nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	restarted.reconcile()
	restarted.reconcile() // a second pass changes nothing
	got = h.get(a)
	if got.Status != AutomationBlocked || got.CurrentMemberID != "m0" {
		t.Fatalf("status %s cursor %q, want blocked at m0 after reconcile", got.Status, got.CurrentMemberID)
	}
	if d, ok := lastHoldDecision(got, "blocked"); !ok || d.Note != holdReasonFor(item) {
		t.Fatalf("blocked decision %+v", d)
	}
	if m0 := automationMember(got, "m0"); m0.Status != MemberPending || m0.StartedAt != nil {
		t.Fatalf("m0 %s started %v", m0.Status, m0.StartedAt)
	}
	wantNoRuns(t, h)
}

func TestPendingItemHoldAlertNamesItem(t *testing.T) {
	h, item, a := holdHarness(t)
	got := activateHeld(t, h, a)
	if h.alertCount("warn|Automation blocked|") != 1 {
		t.Fatalf("alerts %v", h.alerts)
	}
	h.mu.Lock()
	body := h.alerts[len(h.alerts)-1]
	h.mu.Unlock()
	for _, want := range []string{holdItemTitle, item.ID, "pending item", "attach one to continue"} {
		if !strings.Contains(body, want) {
			t.Fatalf("alert %q does not contain %q", body, want)
		}
	}
	d, ok := lastHoldDecision(got, "blocked")
	if !ok || !strings.Contains(d.Note, holdItemTitle) || !strings.Contains(d.Note, item.ID) {
		t.Fatalf("decision log %+v", d)
	}
	if det := h.auditDetail("automation_blocked"); det == nil || det["reason"] != holdReasonFor(item) || det["pending_item_id"] != item.ID {
		t.Fatalf("audit %v", det)
	}
	if strings.Contains(strings.ToLower(body+d.Note), "prd") {
		t.Fatalf("hold text must say Automaton, not PRD: %q / %q", body, d.Note)
	}
}

func TestPendingItemHoldPausedAutomationDoesNotBlock(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	item := newPendingItem(t, h.m, holdItemTitle)
	a := h.graph(
		h.cm(0, "m1", "", ""),
		pendingMember("m1", item.ID, "m2"),
		h.cm(2, "", "", ""),
	)
	paused, err := h.m.PauseAutomation(a.ID, "operator", a.Version)
	if err != nil {
		t.Fatal(err)
	}
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	got := h.get(a)
	if got.Status != AutomationPaused || got.CurrentMemberID != "m1" || got.Version <= paused.Version {
		t.Fatalf("status %s cursor %q version %d, want paused at m1", got.Status, got.CurrentMemberID, got.Version)
	}
	if m1 := automationMember(got, "m1"); !memberUnstarted(m1) {
		t.Fatalf("m1 %s, want not started", m1.Status)
	}
	if h.alertCount("warn|Automation blocked") != 0 || h.hasAudit("automation_blocked") {
		t.Fatalf("a paused Automation must not block: alerts %v", h.alerts)
	}
	wantNoRuns(t, h)
}

func TestPendingItemHoldResumeWhileWaitingReblocks(t *testing.T) {
	h, item, a := holdHarness(t)
	first := activateHeld(t, h, a)
	if first.Status != AutomationBlocked {
		t.Fatalf("status %s, want blocked", first.Status)
	}
	resumed, err := h.m.ResumeAutomation(a.ID, "operator", first.Version)
	if err != nil {
		t.Fatalf("ResumeAutomation on a held Automation: %v", err)
	}
	if resumed.Status != AutomationBlocked || resumed.CurrentMemberID != "m0" {
		t.Fatalf("status %s cursor %q, want blocked at m0", resumed.Status, resumed.CurrentMemberID)
	}
	n := 0
	for _, d := range resumed.Decisions {
		if d.Kind == "blocked" {
			n++
			if d.Note != holdReasonFor(item) {
				t.Fatalf("reason changed: %q", d.Note)
			}
		}
	}
	if n != 2 {
		t.Fatalf("%d blocked decisions, want 2 (hold, then the same hold again)", n)
	}
	if resumed.IdleSince == nil || time.Since(*resumed.IdleSince) > time.Minute {
		t.Fatalf("idle marker %v, want set again", resumed.IdleSince)
	}
	if h.alertCount("warn|Automation blocked|") != 2 {
		t.Fatalf("alerts %v", h.alerts)
	}
	wantNoRuns(t, h)
}
