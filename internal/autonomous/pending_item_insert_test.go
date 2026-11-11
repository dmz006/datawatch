package autonomous

import (
	"strings"
	"testing"
)

const insertItemTitle = "design review follow-up"

// itemMemberOf returns the member that carries item itemID.
func itemMemberOf(t *testing.T, a *Automation, itemID string) AutomationMember {
	t.Helper()
	for _, mem := range a.Members {
		if mem.PendingItemID == itemID {
			return mem
		}
	}
	t.Fatalf("no member for pending item %s in %+v", itemID, a.Members)
	return AutomationMember{}
}

func TestPendingItemInsertDraftLandsAtOnce(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	item := newPendingItem(t, h.m, insertItemTitle)
	got, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, Actor: "alice", Reason: "later work", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Members) != 3 || len(got.PendingInserts) != 0 {
		t.Fatalf("members %d pending inserts %d", len(got.Members), len(got.PendingInserts))
	}
	mem := itemMemberOf(t, got, item.ID)
	if mem.Kind != MemberKindPending || mem.PRDID != "" || mem.Status != MemberPending || mem.AddedBy != "alice" || mem.AddedReason != "later work" {
		t.Fatalf("member %+v", mem)
	}
	if automationMember(got, "m1").OnComplete != mem.ID || mem.OnComplete != "" {
		t.Fatalf("edges: m1->%q new->%q", automationMember(got, "m1").OnComplete, mem.OnComplete)
	}
	d := got.Decisions[len(got.Decisions)-1]
	if d.Kind != "automation_insert_member" || !strings.Contains(d.Note, item.ID) || !strings.Contains(d.Note, insertItemTitle) {
		t.Fatalf("decision %+v", d)
	}
	det := h.auditDetail("automation_insert_member")
	if det == nil || det["pending_item_id"] != item.ID || det["title"] != insertItemTitle || det["held"] != false {
		t.Fatalf("audit %v", det)
	}
}

func TestPendingItemInsertHeldOnActiveAutomation(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	item := newPendingItem(t, h.m, insertItemTitle)

	_, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, ExpectedVersion: a.Version})
	wantErr(t, err, "needs a reason")

	got, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, Actor: "agent:x1", Reason: "tail work", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Members) != 2 || len(got.PendingInserts) != 1 {
		t.Fatalf("members %d pending inserts %d", len(got.Members), len(got.PendingInserts))
	}
	p := got.PendingInserts[0]
	if p.PendingItemID != item.ID || p.PRDID != "" || p.Status != MemberPending || p.Actor != "agent:x1" {
		t.Fatalf("pending insert %+v", p)
	}
	det := h.auditDetail("automation_insert_member")
	if det == nil || det["held"] != true || det["pending_item_id"] != item.ID {
		t.Fatalf("audit %v", det)
	}

	// A draft under require_operator also holds an agent's insert.
	h2, _ := newControlHarness(t, 3)
	h2.m.cfg.Automation.AgentInsertPolicy = PolicyRequireOperator
	d := insertDraft(t, h2)
	item2 := newPendingItem(t, h2.m, "second")
	held, err := h2.m.InsertMember(d.ID, InsertMemberInput{PendingItemID: item2.ID, Actor: "session:s1", ExpectedVersion: d.Version})
	if err != nil {
		t.Fatal(err)
	}
	if len(held.Members) != 2 || len(held.PendingInserts) != 1 {
		t.Fatalf("members %d pending inserts %d", len(held.Members), len(held.PendingInserts))
	}
}

func TestPendingItemInsertApproveLandsPendingMember(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	item := newPendingItem(t, h.m, insertItemTitle)
	held, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, Actor: "agent:x1", Reason: "tail work", ExpectedVersion: a.Version})
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
	mem := automationMember(got, pid)
	if mem == nil || mem.Kind != MemberKindPending || mem.PRDID != "" || mem.PendingItemID != item.ID ||
		mem.Status != MemberPending || mem.AddedBy != "agent:x1" || mem.AddedReason != "tail work" {
		t.Fatalf("member %+v", mem)
	}
	if automationMember(got, "m1").OnComplete != pid || mem.OnComplete != "" {
		t.Fatalf("m1->%q new->%q", automationMember(got, "m1").OnComplete, mem.OnComplete)
	}
	if owner := h.m.automations.PendingItemOwnerOf(item.ID); owner != a.ID {
		t.Fatalf("item owner %q, want %s", owner, a.ID)
	}
	d := got.Decisions[len(got.Decisions)-1]
	if d.Kind != "automation_approve_insert" || !strings.Contains(d.Note, item.ID) || !strings.Contains(d.Note, insertItemTitle) {
		t.Fatalf("decision %+v", d)
	}
	det := h.auditDetail("automation_approve_insert")
	if det == nil || det["member"] != pid || det["pending_item_id"] != item.ID || det["title"] != insertItemTitle {
		t.Fatalf("audit %v", det)
	}
}

func TestPendingItemInsertDuplicateRefused(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	item := newPendingItem(t, h.m, insertItemTitle)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	held, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, Reason: "once", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}

	// Already a pending insert of the same Automation.
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, Reason: "twice", ExpectedVersion: held.Version})
	wantErr(t, err, "409: ", "already has pending insert")

	// A pending insert of another Automation also blocks it.
	h2, err := h.m.CreateAutomation(AutomationInput{
		Name: "other", EntryMemberID: "m2", Members: []AutomationMember{h.cm(2, "", "", "")},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.m.InsertMember(h2.ID, InsertMemberInput{PendingItemID: item.ID, Reason: "elsewhere", ExpectedVersion: h2.Version})
	wantErr(t, err, "409: ", "already has pending insert", a.ID)

	// Once approved it is a member of a, and a member of another Automation's chain is refused.
	got, err := h.m.ApproveInsertedMember(a.ID, held.PendingInserts[0].ID, "operator", held.Version)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, Reason: "again", ExpectedVersion: got.Version})
	wantErr(t, err, "409: ", "pending item "+item.ID)
	_, err = h.m.InsertMember(h2.ID, InsertMemberInput{PendingItemID: item.ID, Reason: "elsewhere", ExpectedVersion: h2.Version})
	wantErr(t, err, "409: ", "already belongs to automation", a.ID)
}

func TestPendingItemInsertUnknownItemRefused(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	_, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: "deadbeef", ExpectedVersion: a.Version})
	wantErr(t, err, "404: ", "deadbeef")
	if got, _ := h.m.automations.Get(a.ID); got.Version != a.Version || len(got.Members) != 2 {
		t.Fatalf("automation changed: version %d members %d", got.Version, len(got.Members))
	}
}

func TestPendingItemInsertNotWaitingRefused(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	item := newPendingItem(t, h.m, insertItemTitle)
	if _, err := h.m.pendingItems.Update(item.ID, func(p *PendingItem) error {
		p.State, p.AutomatonID = PendingItemAttached, h.prds[2].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, ExpectedVersion: a.Version})
	wantErr(t, err, "409: ", "attached", "waiting")

	done := newPendingItem(t, h.m, "finished work")
	if _, err := h.m.pendingItems.Update(done.ID, func(p *PendingItem) error {
		p.State = PendingItemDone
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: done.ID, ExpectedVersion: a.Version})
	wantErr(t, err, "409: ", "done", "waiting")
}

func TestPendingItemInsertUrgentTakesNextSlot(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	item := newPendingItem(t, h.m, insertItemTitle)
	held, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, Urgent: true, Reason: "hotfix", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	if !held.PendingInserts[0].Urgent {
		t.Fatalf("pending insert %+v", held.PendingInserts[0])
	}

	// The one-urgent cap still applies, to an item and to an Automaton alike.
	item2 := newPendingItem(t, h.m, "second urgent")
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item2.ID, Urgent: true, Reason: "more", ExpectedVersion: held.Version})
	wantErr(t, err, "409: ", "pending urgent insert", item.ID)
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, Urgent: true, Reason: "more", ExpectedVersion: held.Version})
	wantErr(t, err, "409: ", "pending urgent insert", item.ID)

	pid := held.PendingInserts[0].ID
	got, err := h.m.ApproveInsertedMember(a.ID, pid, "operator", held.Version)
	if err != nil {
		t.Fatal(err)
	}
	mem := automationMember(got, pid)
	if automationMember(got, "m0").OnComplete != pid || mem.OnComplete != "m1" || mem.Kind != MemberKindPending {
		t.Fatalf("m0->%q new->%q kind %s", automationMember(got, "m0").OnComplete, mem.OnComplete, mem.Kind)
	}
	if m0 := automationMember(got, "m0"); m0.Status != MemberRunning {
		t.Fatalf("running member status %s", m0.Status)
	}
}

func TestPendingItemInsertBothIDsRefused(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	item := newPendingItem(t, h.m, insertItemTitle)

	_, err := h.m.InsertMember(a.ID, InsertMemberInput{PRDID: h.prds[2].ID, PendingItemID: item.ID, ExpectedVersion: a.Version})
	wantErr(t, err, "400: ", "exactly one")
	_, err = h.m.InsertMember(a.ID, InsertMemberInput{ExpectedVersion: a.Version})
	wantErr(t, err, "400: ", "exactly one")
	if got, _ := h.m.automations.Get(a.ID); got.Version != a.Version || len(got.Members) != 2 {
		t.Fatalf("automation changed: version %d members %d", got.Version, len(got.Members))
	}
}
