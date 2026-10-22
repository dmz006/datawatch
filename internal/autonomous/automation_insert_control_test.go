package autonomous

import "testing"

// insertDraft is a draft m0 -> m1 over the harness PRDs 0 and 1.
func insertDraft(t *testing.T, h *coordHarness) *Automation {
	t.Helper()
	a, err := h.m.CreateAutomation(AutomationInput{
		Name: "ins", EntryMemberID: "m0",
		Members: []AutomationMember{h.cm(0, "m1", "", ""), h.cm(1, "", "", "")},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func insertedMemberByPRD(t *testing.T, a *Automation, prdID string) AutomationMember {
	t.Helper()
	for _, mem := range a.Members {
		if mem.PRDID == prdID {
			return mem
		}
	}
	t.Fatalf("no member for prd %s in %+v", prdID, a.Members)
	return AutomationMember{}
}

func TestAutomationControlInsertNormalAppendsToEnd(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	got, err := h.m.InsertMember(a.ID, InsertMemberInput{
		PRDID: h.prds[2].ID, Name: "gamma", Actor: "alice", Reason: "late addition", ExpectedVersion: a.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != a.Version+1 || len(got.Members) != 3 || got.EntryMemberID != "m0" {
		t.Fatalf("version %d members %d entry %s", got.Version, len(got.Members), got.EntryMemberID)
	}
	added := insertedMemberByPRD(t, got, h.prds[2].ID)
	if automationMember(got, "m1").OnComplete != added.ID || added.OnComplete != "" {
		t.Fatalf("edges: m1->%q new->%q", automationMember(got, "m1").OnComplete, added.OnComplete)
	}
	if added.AddedBy != "alice" || added.AddedReason != "late addition" || added.Status != MemberPending {
		t.Fatalf("member %+v", added)
	}
	if prd, _ := h.m.store.GetPRD(h.prds[2].ID); prd.AutomationID != a.ID {
		t.Fatalf("prd automation id %q", prd.AutomationID)
	}
	d := got.Decisions[len(got.Decisions)-1]
	if d.Kind != "automation_insert_member" || d.Actor != "alice" {
		t.Fatalf("decision %+v", d)
	}
	det := h.auditDetail("automation_insert_member")
	if det == nil || det["actor"] != "alice" || det["held"] != false || det["version"] != got.Version {
		t.Fatalf("audit %v", det)
	}
}

func TestAutomationControlInsertUrgentTakesFirstSlot(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	got, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Urgent: true, ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	added := insertedMemberByPRD(t, got, h.prds[2].ID)
	if got.EntryMemberID != added.ID || added.OnComplete != "m0" {
		t.Fatalf("entry %s, new->%q", got.EntryMemberID, added.OnComplete)
	}
	if added.AddedBy != "operator" {
		t.Fatalf("added by %q", added.AddedBy)
	}
}

func TestAutomationControlInsertAfterMember(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	got, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, AfterMemberID: "m0", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	added := insertedMemberByPRD(t, got, h.prds[2].ID)
	if automationMember(got, "m0").OnComplete != added.ID || added.OnComplete != "m1" {
		t.Fatalf("m0->%q new->%q", automationMember(got, "m0").OnComplete, added.OnComplete)
	}
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Urgent: true, AfterMemberID: "m0", ExpectedVersion: got.Version})
	wantErr(t, err, "urgent", "after_member_id")
}

func TestAutomationControlInsertVersionConflict(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	_, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, ExpectedVersion: a.Version + 5})
	wantErr(t, err, "409: ", "version mismatch")
	cur, _ := h.m.GetAutomation(a.ID)
	if cur.Version != a.Version || len(cur.Members) != 2 {
		t.Fatalf("state changed: version %d members %d", cur.Version, len(cur.Members))
	}
}

func TestAutomationControlInsertRejectsBadPRD(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	_, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[1].ID, ExpectedVersion: a.Version})
	wantErr(t, err, "409: ", "already a member")
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PRDID: "nope", ExpectedVersion: a.Version})
	wantErr(t, err, "not found")
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{ExpectedVersion: a.Version})
	wantErr(t, err, "prd_id")
}

func TestAutomationControlInsertAgentPolicy(t *testing.T) {
	t.Run("allow lets an agent insert directly", func(t *testing.T) {
		h, _ := newControlHarness(t, 3)
		a := insertDraft(t, h)
		got, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Actor: "session:ab12", ExpectedVersion: a.Version})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Members) != 3 || len(got.PendingInserts) != 0 {
			t.Fatalf("members %d pending %d", len(got.Members), len(got.PendingInserts))
		}
	})
	t.Run("require_operator holds an agent insert", func(t *testing.T) {
		h, _ := newControlHarness(t, 3)
		h.m.cfg.Automation.AgentInsertPolicy = PolicyRequireOperator
		a := insertDraft(t, h)
		got, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Name: "gamma", Actor: "agent:x1", Reason: "needed", ExpectedVersion: a.Version})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Members) != 2 || automationMember(got, "m1").OnComplete != "" {
			t.Fatalf("graph edited: %+v", got.Members)
		}
		if len(got.PendingInserts) != 1 {
			t.Fatalf("pending %+v", got.PendingInserts)
		}
		p := got.PendingInserts[0]
		if p.PRDID != h.prds[2].ID || p.Name != "gamma" || p.Actor != "agent:x1" || p.Reason != "needed" || p.Status != MemberPending || p.AddedAt.IsZero() {
			t.Fatalf("pending %+v", p)
		}
		if det := h.auditDetail("automation_insert_member"); det == nil || det["held"] != true {
			t.Fatalf("audit %v", det)
		}
	})
	t.Run("require_operator lets the operator insert directly", func(t *testing.T) {
		h, _ := newControlHarness(t, 3)
		h.m.cfg.Automation.AgentInsertPolicy = PolicyRequireOperator
		a := insertDraft(t, h)
		got, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Actor: "operator", ExpectedVersion: a.Version})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Members) != 3 || len(got.PendingInserts) != 0 {
			t.Fatalf("members %d pending %d", len(got.Members), len(got.PendingInserts))
		}
	})
}

func TestAutomationControlInsertIntoActiveIsHeldWithReason(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	_, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, ExpectedVersion: a.Version})
	wantErr(t, err, "reason")
	got, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Reason: "follow-up", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationActive || len(got.Members) != 2 || len(got.PendingInserts) != 1 {
		t.Fatalf("status %s members %d pending %d", got.Status, len(got.Members), len(got.PendingInserts))
	}
	if m := automationMember(got, "m0"); m.Status != MemberRunning {
		t.Fatalf("running member touched: %s", m.Status)
	}
}

func TestAutomationControlInsertRefusesTerminal(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	c, err := h.m.CancelAutomation(a.ID, "", false, true, a.Version)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Reason: "x", ExpectedVersion: c.Version})
	wantErr(t, err, "409: ", "cancelled")
}

func TestAutomationControlApprovePromotesNormalInsert(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	held, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Actor: "agent:x1", Reason: "tail work", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	pid := held.PendingInserts[0].ID
	got, err := h.m.ApproveInsertedMember(a.ID, pid, "operator", held.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PendingInserts) != 0 || len(got.Members) != 3 {
		t.Fatalf("pending %d members %d", len(got.PendingInserts), len(got.Members))
	}
	added := automationMember(got, pid)
	if added == nil || added.PRDID != h.prds[2].ID || added.AddedBy != "agent:x1" || added.AddedReason != "tail work" {
		t.Fatalf("member %+v", added)
	}
	if automationMember(got, "m1").OnComplete != pid || added.OnComplete != "" {
		t.Fatalf("m1->%q new->%q", automationMember(got, "m1").OnComplete, added.OnComplete)
	}
	if prd, _ := h.m.store.GetPRD(h.prds[2].ID); prd.AutomationID != a.ID {
		t.Fatalf("prd automation id %q", prd.AutomationID)
	}
	d := got.Decisions[len(got.Decisions)-1]
	if d.Kind != "automation_approve_insert" || d.Actor != "operator" {
		t.Fatalf("decision %+v", d)
	}
	if det := h.auditDetail("automation_approve_insert"); det == nil || det["member"] != pid || det["requested_by"] != "agent:x1" {
		t.Fatalf("audit %v", det)
	}
}

func TestAutomationControlApproveUrgentTakesNextSlot(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	held, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Urgent: true, Reason: "hotfix", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	pid := held.PendingInserts[0].ID
	got, err := h.m.ApproveInsertedMember(a.ID, pid, "", held.Version)
	if err != nil {
		t.Fatal(err)
	}
	added := automationMember(got, pid)
	if automationMember(got, "m0").OnComplete != pid || added.OnComplete != "m1" {
		t.Fatalf("m0->%q new->%q", automationMember(got, "m0").OnComplete, added.OnComplete)
	}
	if m := automationMember(got, "m0"); m.Status != MemberRunning {
		t.Fatalf("running member status %s", m.Status)
	}
}

func TestAutomationControlApproveDraftPending(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	h.m.cfg.Automation.AgentInsertPolicy = PolicyRequireOperator
	a := insertDraft(t, h)
	held, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Urgent: true, Actor: "session:s1", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	pid := held.PendingInserts[0].ID
	got, err := h.m.ApproveInsertedMember(a.ID, pid, "operator", held.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.EntryMemberID != pid || automationMember(got, pid).OnComplete != "m0" {
		t.Fatalf("entry %s new->%q", got.EntryMemberID, automationMember(got, pid).OnComplete)
	}
}

func TestAutomationControlApproveRejections(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	_, err := h.m.ApproveInsertedMember(a.ID, "nothing", "operator", a.Version)
	wantErr(t, err, "404: ", "no pending insert")

	held, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Reason: "r", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	pid := held.PendingInserts[0].ID
	_, err = h.m.ApproveInsertedMember(a.ID, pid, "agent:x1", held.Version)
	wantErr(t, err, "409: ", "operator")
	_, err = h.m.ApproveInsertedMember(a.ID, pid, "operator", held.Version-1)
	wantErr(t, err, "409: ", "version mismatch")
	cur, _ := h.m.GetAutomation(a.ID)
	if len(cur.PendingInserts) != 1 || len(cur.Members) != 2 || cur.Version != held.Version {
		t.Fatalf("state changed by a rejected approve: %+v", cur)
	}
}

func TestAutomationUrgentOneAtATime(t *testing.T) {
	h, _ := newControlHarness(t, 4)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	first, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Urgent: true, Reason: "one", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	firstID := first.PendingInserts[0].ID

	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[3].ID, Urgent: true, Reason: "two", ExpectedVersion: first.Version})
	wantErr(t, err, "409: ", firstID, "urgent")
	cur, _ := h.m.GetAutomation(a.ID)
	if len(cur.PendingInserts) != 1 || cur.Version != first.Version {
		t.Fatalf("second urgent changed state: %+v", cur.PendingInserts)
	}

	// A normal insert is not capped.
	normal, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[3].ID, Reason: "three", ExpectedVersion: first.Version})
	if err != nil {
		t.Fatalf("normal insert beside a pending urgent: %v", err)
	}
	if len(normal.PendingInserts) != 2 {
		t.Fatalf("pending %d", len(normal.PendingInserts))
	}

	// The cap holds on the approve path too: a second urgent that got past it
	// (stored state) is not promoted while another urgent is pending.
	if _, err := h.m.automations.Update(a.ID, normal.Version, func(cur *Automation) error {
		for i := range cur.PendingInserts {
			if cur.PendingInserts[i].PRDID == h.prds[3].ID {
				cur.PendingInserts[i].Urgent = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cur, _ = h.m.GetAutomation(a.ID)
	_, err = h.m.ApproveInsertedMember(a.ID, cur.PendingInserts[1].ID, "operator", cur.Version)
	wantErr(t, err, "409: ", firstID)
	if _, err := h.m.automations.Update(a.ID, cur.Version, func(cur *Automation) error {
		cur.PendingInserts[1].Urgent = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Approve the first urgent; the next urgent insert is then accepted.
	cur, _ = h.m.GetAutomation(a.ID)
	approved, err := h.m.ApproveInsertedMember(a.ID, firstID, "operator", cur.Version)
	if err != nil {
		t.Fatal(err)
	}
	if pendingUrgent(approved, "") != nil {
		t.Fatalf("urgent still pending after approve: %+v", approved.PendingInserts)
	}
	if len(approved.PendingInserts) != 1 || approved.PendingInserts[0].PRDID != h.prds[3].ID {
		t.Fatalf("remaining pending inserts: %+v", approved.PendingInserts)
	}
}

func TestAutomationUrgentOneAtATimeAfterApprove(t *testing.T) {
	h, _ := newControlHarness(t, 4)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	first, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Urgent: true, Reason: "one", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := h.m.ApproveInsertedMember(a.ID, first.PendingInserts[0].ID, "operator", first.Version)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[3].ID, Urgent: true, Reason: "two", ExpectedVersion: approved.Version})
	if err != nil {
		t.Fatalf("urgent insert after the first was approved: %v", err)
	}
	if len(second.PendingInserts) != 1 || !second.PendingInserts[0].Urgent {
		t.Fatalf("pending %+v", second.PendingInserts)
	}
}
