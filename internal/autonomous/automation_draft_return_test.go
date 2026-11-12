package autonomous

import (
	"strings"
	"testing"
	"time"
)

// blockNeverStarted lays a blocked-on-the-ground state over a draft: blocked
// with an IdleSince and a budget episode, but no member started and no
// cursor, the shape startFailed and the budget gate leave behind before any
// member ran.
func (h *coordHarness) blockNeverStarted(a *Automation) *Automation {
	h.t.Helper()
	h.forceState(a, func(a *Automation) {
		now := time.Now()
		a.Status = AutomationBlocked
		a.IdleSince = &now
		a.BudgetState = &BudgetState{Action: "blocked", Window: "automation", SpentUSD: 10, CeilingUSD: 5}
	})
	return h.get(a)
}

// blockAfterStart lays a blocked state where the entry member had started and
// finished and the cursor had moved to m1 before the block landed.
func (h *coordHarness) blockAfterStart(a *Automation) *Automation {
	h.t.Helper()
	h.forceState(a, func(a *Automation) {
		now := time.Now()
		a.Status = AutomationBlocked
		a.IdleSince = &now
		a.CurrentMemberID = a.Members[1].ID
		m0 := &a.Members[0]
		m0.Status = MemberCompleted
		m0.StartedAt = &now
		m0.FinishedAt = &now
		m0.Outcome = "completed"
		m0.EdgeTaken = edgeOnComplete
	})
	return h.get(a)
}

// wantRefusal checks that err is a 409 that names the automation and its
// state or the reason.
func wantRefusal(t *testing.T, err error, what string, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want a refusal, got nil", what)
	}
	if !strings.Contains(err.Error(), "409") {
		t.Fatalf("%s: error %q is not a 409", what, err)
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("%s: error %q does not name %q", what, err, w)
		}
	}
}

// blockedDraft makes a two-member draft chain m0 -> m1.
func (h *coordHarness) blockedDraft() *Automation {
	h.t.Helper()
	return h.makeAuto([][3]string{{"m1", "", ""}}, 2)
}

func TestReturnAutomationToDraft(t *testing.T) {
	t.Run("blocked never started returns to draft", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		b := h.blockNeverStarted(h.blockedDraft())
		got, err := h.m.ReturnAutomationToDraft(b.ID, "alice", b.Version)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != AutomationDraft || got.Version != b.Version+1 {
			t.Fatalf("status %s version %d (want draft %d)", got.Status, got.Version, b.Version+1)
		}
		if got.IdleSince != nil || got.StartedAt != nil || got.BudgetState != nil {
			t.Fatalf("runtime state not cleared: idle=%v started=%v budget=%+v", got.IdleSince, got.StartedAt, got.BudgetState)
		}
		if got.CurrentMemberID != "" || len(got.ActiveCursors) != 0 {
			t.Fatalf("cursors left behind: %q %v", got.CurrentMemberID, got.ActiveCursors)
		}
		for _, mem := range got.Members {
			if mem.Status != MemberPending && mem.Status != "" {
				t.Fatalf("member %s is %s (want pending)", mem.ID, mem.Status)
			}
			if mem.StartedAt != nil || mem.FinishedAt != nil {
				t.Fatalf("member %s timestamps not cleared", mem.ID)
			}
		}
		if len(got.Decisions) == 0 {
			t.Fatal("no decision recorded")
		}
		d := got.Decisions[len(got.Decisions)-1]
		if d.Kind != "return_to_draft" || d.Actor != "alice" || d.Note == "" {
			t.Fatalf("decision %+v (want kind return_to_draft, actor alice, note)", d)
		}
		det := h.auditDetail("automation_return_to_draft")
		if det == nil || det["actor"] != "alice" || det["automation_id"] != b.ID {
			t.Fatalf("audit %v", det)
		}
	})

	t.Run("blocked after a member started is refused", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		b := h.blockAfterStart(h.blockedDraft())
		_, err := h.m.ReturnAutomationToDraft(b.ID, "", b.Version)
		wantRefusal(t, err, "started member", b.ID, "started")
		if a, ok := h.m.automations.Get(b.ID); !ok || a.Status != AutomationBlocked {
			t.Fatal("refused automation was modified")
		}
		if h.auditDetail("automation_return_to_draft") != nil {
			t.Fatal("refusal audited")
		}
		if m0 := automationMember(h.get(b), "m0"); m0.StartedAt == nil || m0.Status != MemberCompleted {
			t.Fatalf("member state changed after refusal: %+v", m0)
		}
	})

	t.Run("active, paused and completed are refused", func(t *testing.T) {
		t.Run("active", func(t *testing.T) {
			h, _ := newControlHarness(t, 2)
			a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
			_, err := h.m.ReturnAutomationToDraft(a.ID, "", a.Version)
			wantRefusal(t, err, "active", a.ID, "active")
		})
		t.Run("paused", func(t *testing.T) {
			h, _ := newControlHarness(t, 2)
			a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
			p, err := h.m.PauseAutomation(a.ID, "", a.Version)
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.m.ReturnAutomationToDraft(p.ID, "", p.Version)
			wantRefusal(t, err, "paused", p.ID, "paused")
		})
		t.Run("completed", func(t *testing.T) {
			h, _ := newControlHarness(t, 2)
			a := h.blockedDraft()
			h.forceState(a, func(a *Automation) {
				now := time.Now()
				a.Status = AutomationCompleted
				a.FinishedAt = &now
			})
			a = h.get(a)
			_, err := h.m.ReturnAutomationToDraft(a.ID, "", a.Version)
			wantRefusal(t, err, "completed", a.ID, "completed")
		})
		t.Run("draft stays a draft and is refused", func(t *testing.T) {
			h, _ := newControlHarness(t, 2)
			a := h.blockedDraft()
			_, err := h.m.ReturnAutomationToDraft(a.ID, "", a.Version)
			wantRefusal(t, err, "draft", a.ID, "draft")
		})
	})

	t.Run("version mismatch is refused", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		b := h.blockNeverStarted(h.blockedDraft())
		_, err := h.m.ReturnAutomationToDraft(b.ID, "", b.Version+7)
		wantRefusal(t, err, "stale version", "mismatch")
		if a, ok := h.m.automations.Get(b.ID); !ok || a.Version != b.Version || a.Status != AutomationBlocked {
			t.Fatal("stale return modified the automation")
		}
		// Zero expectedVersion skips the check and works.
		got, err := h.m.ReturnAutomationToDraft(b.ID, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != AutomationDraft {
			t.Fatalf("status %s", got.Status)
		}
	})

	t.Run("after the return, ActivateAutomation succeeds", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		b := h.blockNeverStarted(h.blockedDraft())
		got, err := h.m.ReturnAutomationToDraft(b.ID, "", b.Version)
		if err != nil {
			t.Fatal(err)
		}
		got, err = h.m.ActivateAutomation(got.ID, "", got.Version)
		if err != nil {
			t.Fatalf("activation after return: %v", err)
		}
		if got.Status != AutomationActive || got.StartedAt == nil {
			t.Fatalf("activated state: %s started=%v", got.Status, got.StartedAt)
		}
		if m0 := automationMember(got, "m0"); m0.Status != MemberRunning || m0.StartedAt == nil {
			t.Fatalf("entry member %s started=%v", m0.Status, m0.StartedAt)
		}
		if r, _ := h.fake.counts(); len(r) != 1 || r[0] != h.prds[0].ID {
			t.Fatalf("entry member run: %v", r)
		}
	})
}
