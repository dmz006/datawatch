package autonomous

import (
	"strings"
	"testing"
)

// attachTarget is the Automaton the attach tests attach: the harness Automaton
// that holdHarness leaves out of its Automation.
func attachTarget(h *coordHarness) string { return h.prds[0].ID }

func itemNow(t *testing.T, h *coordHarness, id string) *PendingItem {
	t.Helper()
	got, ok := h.m.pendingItems.Get(id)
	if !ok {
		t.Fatalf("pending item %s missing", id)
	}
	return got
}

func hasDecisionKind(a *Automation, kind string) bool {
	_, ok := lastHoldDecision(a, kind)
	return ok
}

func automatonOwnerOf(t *testing.T, h *coordHarness, prdID string) string {
	t.Helper()
	p, ok := h.m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("automaton %s missing", prdID)
	}
	return p.AutomationID
}

func TestPendingItemAttachConvertsMemberToAutomaton(t *testing.T) {
	h, item, a := holdHarness(t)
	target := attachTarget(h)
	statusBefore := automationMember(h.get(a), "m0").Status

	res, err := h.m.AttachPendingItem(item.ID, target, "alice")
	if err != nil {
		t.Fatalf("AttachPendingItem: %v", err)
	}
	if res.Item.State != PendingItemAttached || res.Item.AutomatonID != target || res.Item.AttachedAt == nil {
		t.Fatalf("item %+v", res.Item)
	}
	if len(res.Converted) != 1 || res.Converted[0].AutomationID != a.ID || len(res.Converted[0].MemberIDs) != 1 || res.Converted[0].MemberIDs[0] != "m0" {
		t.Fatalf("converted %+v", res.Converted)
	}
	if len(res.Resumed) != 0 {
		t.Fatalf("a draft Automation is not resumed: %v", res.Resumed)
	}

	got := h.get(a)
	m0 := automationMember(got, "m0")
	if m0.Kind != MemberKindPRD || m0.PRDID != target || m0.PendingItemID != item.ID {
		t.Fatalf("m0 %+v", m0)
	}
	if m0.OnComplete != "m1" || m0.Status != statusBefore || got.EntryMemberID != "m0" || len(got.Members) != 3 {
		t.Fatalf("m0 edges/status/position changed: %+v (entry %s, %d members)", m0, got.EntryMemberID, len(got.Members))
	}
	if got.Version <= a.Version {
		t.Fatalf("version %d, want above %d", got.Version, a.Version)
	}
	if owner := automatonOwnerOf(t, h, target); owner != a.ID {
		t.Fatalf("Automaton owner %q, want %s", owner, a.ID)
	}
	if owner := h.m.automations.OwnerOf(target); owner != a.ID {
		t.Fatalf("owner index %q, want %s", owner, a.ID)
	}
	if owner := h.m.automations.PendingItemOwnerOf(item.ID); owner != a.ID {
		t.Fatalf("item owner index %q, want %s", owner, a.ID)
	}

	stored := itemNow(t, h, item.ID)
	if stored.State != PendingItemAttached || stored.AutomatonID != target || stored.AttachedAt == nil {
		t.Fatalf("stored item %+v", stored)
	}
	d, ok := lastHoldDecision(got, "pending_item_attach")
	if !ok || d.Actor != "alice" || !strings.Contains(d.Note, item.ID) || !strings.Contains(d.Note, holdItemTitle) || !strings.Contains(d.Note, target) {
		t.Fatalf("decision %+v", d)
	}
	det := h.auditDetail("pending_item_attach")
	if det == nil || det["pending_item_id"] != item.ID || det["automaton_id"] != target || det["actor"] != "alice" || det["title"] != holdItemTitle {
		t.Fatalf("audit %v", det)
	}
	wantNoRuns(t, h)
}

func TestPendingItemAttachResumesHeldAutomation(t *testing.T) {
	h, item, a := holdHarness(t)
	held := activateHeld(t, h, a)
	if held.Status != AutomationBlocked {
		t.Fatalf("status %s, want blocked", held.Status)
	}
	target := attachTarget(h)

	res, err := h.m.AttachPendingItem(item.ID, target, "operator")
	if err != nil {
		t.Fatalf("AttachPendingItem: %v", err)
	}
	if len(res.Resumed) != 1 || res.Resumed[0] != a.ID || len(res.Notes) != 0 {
		t.Fatalf("resumed %v notes %v", res.Resumed, res.Notes)
	}
	got := h.get(a)
	if got.Status != AutomationActive || got.IdleSince != nil {
		t.Fatalf("status %s idle %v, want active", got.Status, got.IdleSince)
	}
	m0 := automationMember(got, "m0")
	if m0.Status != MemberRunning || m0.StartedAt == nil || m0.PRDID != target || m0.PendingItemID != item.ID {
		t.Fatalf("m0 %+v", m0)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != target {
		t.Fatalf("runs %v, want only the attached Automaton", runs)
	}
	if !hasDecisionKind(got, "pending_item_attach") || !hasDecisionKind(got, "automation_resume") {
		t.Fatalf("decisions %+v", got.Decisions)
	}
	if !h.hasAudit("pending_item_attach") || !h.hasAudit("automation_resume") {
		t.Fatal("want an attach and a resume audit entry")
	}
}

func TestPendingItemAttachNoAutoResumeLeavesBlocked(t *testing.T) {
	h, item, a := holdHarness(t)
	off := false
	h.m.cfg.Automation.PendingItemAutoResume = &off
	activateHeld(t, h, a)
	target := attachTarget(h)

	res, err := h.m.AttachPendingItem(item.ID, target, "operator")
	if err != nil {
		t.Fatalf("AttachPendingItem: %v", err)
	}
	if len(res.Resumed) != 0 || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "resume it") {
		t.Fatalf("resumed %v notes %v", res.Resumed, res.Notes)
	}
	got := h.get(a)
	if got.Status != AutomationBlocked {
		t.Fatalf("status %s, want blocked", got.Status)
	}
	m0 := automationMember(got, "m0")
	if m0.Kind != MemberKindPRD || m0.PRDID != target || m0.Status != MemberPending || m0.StartedAt != nil {
		t.Fatalf("m0 %+v", m0)
	}
	d, ok := lastHoldDecision(got, "pending_item_attach")
	if !ok || !strings.Contains(d.Note, "auto-resume is off") || !strings.Contains(d.Note, "resume it") {
		t.Fatalf("decision %+v", d)
	}
	wantNoRuns(t, h)

	// The operator's resume starts the converted member.
	if _, err := h.m.ResumeAutomation(a.ID, "operator", got.Version); err != nil {
		t.Fatalf("ResumeAutomation: %v", err)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != target {
		t.Fatalf("runs %v after the resume", runs)
	}
}

func TestPendingItemAttachDraftAutomatonStillBlocks(t *testing.T) {
	h, item, a := holdHarness(t)
	activateHeld(t, h, a)
	h.setStatus(0, PRDDraft)

	res, err := h.m.AttachPendingItem(item.ID, attachTarget(h), "operator")
	if err != nil {
		t.Fatalf("AttachPendingItem: %v", err)
	}
	if len(res.Resumed) != 1 {
		t.Fatalf("resumed %v", res.Resumed)
	}
	got := h.get(a)
	if got.Status != AutomationBlocked {
		t.Fatalf("status %s, want blocked by the draft Automaton", got.Status)
	}
	if m0 := automationMember(got, "m0"); m0.Status != MemberPending {
		t.Fatalf("m0 %s, want not started", m0.Status)
	}
	wantNoRuns(t, h)
}

func TestPendingItemAttachUnknownAutomatonRefused(t *testing.T) {
	h, item, a := holdHarness(t)

	_, err := h.m.AttachPendingItem(item.ID, "deadbeef", "operator")
	wantErrPrefix(t, err, "404:")
	wantErr(t, err, "Automaton deadbeef not found")
	if got := itemNow(t, h, item.ID); got.State != PendingItemWaiting || got.AutomatonID != "" {
		t.Fatalf("item changed: %+v", got)
	}
	if m0 := automationMember(h.get(a), "m0"); m0.Kind != MemberKindPending || m0.PRDID != "" {
		t.Fatalf("member changed: %+v", m0)
	}

	_, err = h.m.AttachPendingItem(item.ID, "  ", "operator")
	wantErrPrefix(t, err, "400:")
	_, err = h.m.AttachPendingItem("ffffffff", attachTarget(h), "operator")
	wantErrPrefix(t, err, "404:")
	wantErr(t, err, "pending item ffffffff not found")
}

func TestPendingItemAttachAutomatonOwnedElsewhereRefused(t *testing.T) {
	h, item, a := holdHarness(t)
	target := attachTarget(h)
	other, err := h.m.CreateAutomation(AutomationInput{
		Name: "other", EntryMemberID: "o0",
		Members: []AutomationMember{{ID: "o0", PRDID: target}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}

	_, err = h.m.AttachPendingItem(item.ID, target, "operator")
	wantErrPrefix(t, err, "409:")
	wantErr(t, err, "already belongs to automation "+other.ID)
	if got := itemNow(t, h, item.ID); got.State != PendingItemWaiting {
		t.Fatalf("item %+v", got)
	}
	if m0 := automationMember(h.get(a), "m0"); m0.Kind != MemberKindPending {
		t.Fatalf("member changed: %+v", m0)
	}

	// An Automaton that is already a member of the same Automation is refused too.
	_, err = h.m.AttachPendingItem(item.ID, h.prds[1].ID, "operator")
	wantErrPrefix(t, err, "409:")
	wantErr(t, err, "already a member of automation "+a.ID)
}

func TestPendingItemAttachItemNotWaitingRefused(t *testing.T) {
	h, item, a := holdHarness(t)
	if _, err := h.m.pendingItems.Update(item.ID, func(p *PendingItem) error {
		p.State = PendingItemDone
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	_, err := h.m.AttachPendingItem(item.ID, attachTarget(h), "operator")
	wantErrPrefix(t, err, "409:")
	wantErr(t, err, "is done")
	if m0 := automationMember(h.get(a), "m0"); m0.Kind != MemberKindPending || m0.PRDID != "" {
		t.Fatalf("member changed: %+v", m0)
	}
	if owner := automatonOwnerOf(t, h, attachTarget(h)); owner != "" {
		t.Fatalf("Automaton owner %q, want none", owner)
	}
}

func TestPendingItemAttachTwiceRefused(t *testing.T) {
	h, item, a := holdHarness(t)
	target := attachTarget(h)
	if _, err := h.m.AttachPendingItem(item.ID, target, "operator"); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	before := h.get(a)

	_, err := h.m.AttachPendingItem(item.ID, target, "operator")
	wantErrPrefix(t, err, "409:")
	wantErr(t, err, "already has Automaton "+target+" attached")

	// A different Automaton is refused as well, and nothing changes.
	_, err = h.m.AttachPendingItem(item.ID, h.prds[1].ID, "operator")
	wantErrPrefix(t, err, "409:")
	after := h.get(a)
	if after.Version != before.Version || automationMember(after, "m0").PRDID != target {
		t.Fatalf("automation changed by a refused attach: version %d -> %d", before.Version, after.Version)
	}
	if got := itemNow(t, h, item.ID); got.AutomatonID != target {
		t.Fatalf("item %+v", got)
	}
}

func TestPendingItemAttachCompletionMarksItemDone(t *testing.T) {
	h, item, a := holdHarness(t)
	activateHeld(t, h, a)
	target := attachTarget(h)
	if _, err := h.m.AttachPendingItem(item.ID, target, "operator"); err != nil {
		t.Fatalf("AttachPendingItem: %v", err)
	}
	if got := itemNow(t, h, item.ID); got.State != PendingItemAttached {
		t.Fatalf("item %s before the Automaton ends, want attached", got.State)
	}

	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	done := itemNow(t, h, item.ID)
	if done.State != PendingItemDone || done.DoneAt == nil || done.AutomatonID != target {
		t.Fatalf("item %+v, want done", done)
	}
	if det := h.auditDetail("pending_item_done"); det == nil || det["pending_item_id"] != item.ID || det["automaton_id"] != target {
		t.Fatalf("audit %v", det)
	}
	got := h.get(a)
	if m0 := automationMember(got, "m0"); m0.Status != MemberCompleted {
		t.Fatalf("m0 %s, want completed", m0.Status)
	}
	if got.CurrentMemberID != "m1" {
		t.Fatalf("cursor %q, want m1", got.CurrentMemberID)
	}
}

func TestPendingItemAttachFailedAutomatonLeavesItemAttached(t *testing.T) {
	h, item, a := holdHarness(t)
	activateHeld(t, h, a)
	if _, err := h.m.AttachPendingItem(item.ID, attachTarget(h), "operator"); err != nil {
		t.Fatalf("AttachPendingItem: %v", err)
	}
	h.setStatus(0, PRDFailed)
	h.event(0, PRDFailed)
	if got := itemNow(t, h, item.ID); got.State != PendingItemAttached || got.DoneAt != nil {
		t.Fatalf("item %+v, want still attached", got)
	}
	if h.hasAudit("pending_item_done") {
		t.Fatal("a failed Automaton must not mark the item done")
	}
}

func TestPendingItemAttachHeldInsertConverted(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	item := newPendingItem(t, h.m, insertItemTitle)
	held, err := h.m.InsertMember(a.ID, InsertMemberInput{PendingItemID: item.ID, Actor: "agent:x1", Reason: "tail work", ExpectedVersion: a.Version})
	if err != nil {
		t.Fatal(err)
	}
	if len(held.PendingInserts) != 1 || held.PendingInserts[0].PRDID != "" {
		t.Fatalf("pending inserts %+v", held.PendingInserts)
	}
	pid := held.PendingInserts[0].ID
	target := h.prds[2].ID

	res, err := h.m.AttachPendingItem(item.ID, target, "operator")
	if err != nil {
		t.Fatalf("AttachPendingItem: %v", err)
	}
	if len(res.Converted) != 1 || res.Converted[0].AutomationID != a.ID || len(res.Converted[0].InsertIDs) != 1 || res.Converted[0].InsertIDs[0] != pid || len(res.Converted[0].MemberIDs) != 0 {
		t.Fatalf("converted %+v", res.Converted)
	}
	got := h.get(a)
	if len(got.PendingInserts) != 1 || got.PendingInserts[0].PRDID != target || got.PendingInserts[0].PendingItemID != item.ID {
		t.Fatalf("pending insert %+v", got.PendingInserts)
	}
	if len(got.Members) != 2 {
		t.Fatalf("members %d, a held insert is not a member yet", len(got.Members))
	}
	if !hasDecisionKind(got, "pending_item_attach") {
		t.Fatalf("decisions %+v", got.Decisions)
	}

	// Approving the converted insert lands an ordinary Automaton member that
	// keeps the item as provenance.
	approved, err := h.m.ApproveInsertedMember(a.ID, pid, "operator", got.Version)
	if err != nil {
		t.Fatalf("ApproveInsertedMember: %v", err)
	}
	mem := automationMember(approved, pid)
	if mem == nil || mem.kind() != MemberKindPRD || mem.PRDID != target || mem.PendingItemID != item.ID ||
		mem.Status != MemberPending || mem.AddedBy != "agent:x1" || mem.AddedReason != "tail work" {
		t.Fatalf("member %+v", mem)
	}
	if automationMember(approved, "m1").OnComplete != pid {
		t.Fatalf("m1 -> %q, want %s", automationMember(approved, "m1").OnComplete, pid)
	}
	if owner := automatonOwnerOf(t, h, target); owner != a.ID {
		t.Fatalf("Automaton owner %q, want %s", owner, a.ID)
	}
	if got := itemNow(t, h, item.ID); got.State != PendingItemAttached || got.AutomatonID != target {
		t.Fatalf("item %+v", got)
	}
}

func TestPendingItemAttachItemInNoAutomation(t *testing.T) {
	h, _ := newControlHarness(t, 2)
	item := newPendingItem(t, h.m, "loose work")

	res, err := h.m.AttachPendingItem(item.ID, h.prds[0].ID, "")
	if err != nil {
		t.Fatalf("AttachPendingItem: %v", err)
	}
	if res.Item.State != PendingItemAttached || len(res.Converted) != 0 || len(res.Resumed) != 0 {
		t.Fatalf("result %+v", res)
	}
	if owner := automatonOwnerOf(t, h, h.prds[0].ID); owner != "" {
		t.Fatalf("Automaton owner %q, want none", owner)
	}
	if det := h.auditDetail("pending_item_attach"); det == nil || det["actor"] != "operator" {
		t.Fatalf("audit %v", det)
	}
}

func TestPendingItemAttachRefusesTemplateAndChildAutomaton(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	item := newPendingItem(t, h.m, "work")
	tmpl, _ := h.m.store.GetPRD(h.prds[0].ID)
	tmpl.IsTemplate = true
	if err := h.m.store.SavePRD(tmpl); err != nil {
		t.Fatal(err)
	}
	child, _ := h.m.store.GetPRD(h.prds[1].ID)
	child.ParentPRDID = h.prds[2].ID
	if err := h.m.store.SavePRD(child); err != nil {
		t.Fatal(err)
	}

	_, err := h.m.AttachPendingItem(item.ID, h.prds[0].ID, "operator")
	wantErrPrefix(t, err, "409:")
	wantErr(t, err, "template")
	_, err = h.m.AttachPendingItem(item.ID, h.prds[1].ID, "operator")
	wantErrPrefix(t, err, "409:")
	wantErr(t, err, "child Automaton")
	if got := itemNow(t, h, item.ID); got.State != PendingItemWaiting {
		t.Fatalf("item %+v", got)
	}
}

func TestPendingItemAttachAutomatonTakenByAnotherItemRefused(t *testing.T) {
	h, _ := newControlHarness(t, 2)
	first := newPendingItem(t, h.m, "first")
	second := newPendingItem(t, h.m, "second")
	target := h.prds[0].ID
	if _, err := h.m.AttachPendingItem(first.ID, target, "operator"); err != nil {
		t.Fatal(err)
	}
	_, err := h.m.AttachPendingItem(second.ID, target, "operator")
	wantErrPrefix(t, err, "409:")
	wantErr(t, err, "already attached to pending item "+first.ID)
	if got := itemNow(t, h, second.ID); got.State != PendingItemWaiting {
		t.Fatalf("item %+v", got)
	}
}

func TestPendingItemAttachNoRegistry(t *testing.T) {
	h, _ := newControlHarness(t, 1)
	h.m.pendingItems = nil
	_, err := h.m.AttachPendingItem("aaaaaaaa", h.prds[0].ID, "operator")
	wantErrPrefix(t, err, "500:")
	h.m.completeAttachedPendingItem("aaaaaaaa", h.prds[0].ID) // nothing to do, must not panic
}
