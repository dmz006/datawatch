package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// pendingMember is a pending item member for item, with the given outcome edge.
func pendingMember(id, itemID, onComplete string) AutomationMember {
	return AutomationMember{ID: id, Kind: MemberKindPending, PendingItemID: itemID, OnComplete: onComplete}
}

// newPendingItem records a waiting item through the Manager.
func newPendingItem(t *testing.T, m *Manager, title string) *PendingItem {
	t.Helper()
	p, err := m.CreatePendingItem(title, "", "tester")
	if err != nil {
		t.Fatalf("CreatePendingItem: %v", err)
	}
	return p
}

func wantInvalid(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error containing %v, got none", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("error %q does not contain %q", err.Error(), p)
		}
	}
}

func TestPendingItemMemberValidShape(t *testing.T) {
	a := &Automation{
		Name: "shape", Status: AutomationDraft, EntryMemberID: "m0",
		Members: []AutomationMember{
			{ID: "m0", PRDID: "p1", OnComplete: "m1"},
			pendingMember("m1", "aaaa1111", "m2"),
			// An Automaton was attached to this item: kind prd keeps the item as provenance.
			{ID: "m2", Kind: MemberKindPRD, PRDID: "p2", PendingItemID: "bbbb2222"},
		},
	}
	if err := validateAutomation(a); err != nil {
		t.Fatalf("valid shape rejected: %v", err)
	}

	raw, err := json.Marshal(a.Members[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"pending_item_id":"aaaa1111"`) || !strings.Contains(string(raw), `"kind":"pending_item"`) {
		t.Fatalf("member JSON %s", raw)
	}
	if strings.Contains(string(raw), "prd_id\":\"p") {
		t.Fatalf("pending member must not carry a prd_id: %s", raw)
	}

	// The store accepts a real item and keeps the field through a round trip.
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	item := newPendingItem(t, m, "write the docs")
	prds := makeAutomationMemberPRDs(t, m, 1)
	got, err := m.CreateAutomation(AutomationInput{
		Name:          "with item",
		EntryMemberID: "m0",
		Members: []AutomationMember{
			{ID: "m0", PRDID: prds[0].ID, OnComplete: "m1"},
			pendingMember("m1", item.ID, ""),
		},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	stored, ok := m.GetAutomation(got.ID)
	if !ok || stored.Members[1].PendingItemID != item.ID || stored.Members[1].kind() != MemberKindPending {
		t.Fatalf("stored member %+v", stored.Members)
	}
	if owner := m.automations.PendingItemOwnerOf(item.ID); owner != got.ID {
		t.Fatalf("item owner %q, want %q", owner, got.ID)
	}
}

func TestPendingItemMemberMissingItemIDRejected(t *testing.T) {
	for _, id := range []string{"", "   ", "\t"} {
		a := &Automation{
			Name: "missing", Status: AutomationDraft, EntryMemberID: "m0",
			Members: []AutomationMember{pendingMember("m0", id, "")},
		}
		wantInvalid(t, validateAutomation(a), "member m0", "pending_item_id")
	}
}

func TestPendingItemMemberRejectsPRDIDAndOtherKindFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(m *AutomationMember)
		want   string
	}{
		{"prd_id", func(m *AutomationMember) { m.PRDID = "p1" }, "prd_id"},
		{"automation_id", func(m *AutomationMember) { m.AutomationID = "child" }, "automation_id"},
		{"fork_members", func(m *AutomationMember) { m.ForkMembers = []string{"a", "b"} }, "fork_members"},
		{"join_members", func(m *AutomationMember) { m.JoinMembers = []string{"a", "b"} }, "join_members"},
		{"on_branch_failure", func(m *AutomationMember) { m.OnBranchFailure = BranchAllMustSucceed }, "on_branch_failure"},
		{"estimated_cost_usd", func(m *AutomationMember) { m.EstimatedCostUSD = 1.5 }, "estimated_cost_usd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := pendingMember("m0", "aaaa1111", "")
			tc.mutate(&mem)
			a := &Automation{Name: "bad", Status: AutomationDraft, EntryMemberID: "m0", Members: []AutomationMember{mem}}
			wantInvalid(t, validateAutomation(a), "member m0", tc.want)
		})
	}

	// Every other kind refuses a pending item id; only kind prd may carry it.
	for _, tc := range []struct {
		name string
		mem  AutomationMember
	}{
		{"automation", AutomationMember{ID: "m0", Kind: MemberKindAutomation, AutomationID: "child", PendingItemID: "aaaa1111"}},
		{"fork", AutomationMember{ID: "m0", Kind: MemberKindFork, ForkMembers: []string{"a", "b"}, PendingItemID: "aaaa1111"}},
		{"join", AutomationMember{ID: "m0", Kind: MemberKindJoin, JoinMembers: []string{"a", "b"}, PendingItemID: "aaaa1111"}},
	} {
		t.Run("item id on "+tc.name, func(t *testing.T) {
			err := validateMemberKind(&Automation{ID: "self"}, tc.mem)
			wantInvalid(t, err, "member m0", "pending_item_id")
		})
	}
}

func TestPendingItemMemberUnknownItemRejectedByStore(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.CreateAutomation(AutomationInput{
		Name: "unknown", EntryMemberID: "m0",
		Members: []AutomationMember{pendingMember("m0", "deadbeef", "")},
	}, "operator")
	wantInvalid(t, err, "pending item deadbeef not found")

	// Update refuses an unknown item too, and leaves the stored version alone.
	item := newPendingItem(t, m, "real work")
	prds := makeAutomationMemberPRDs(t, m, 1)
	a, err := m.CreateAutomation(AutomationInput{
		Name: "update", EntryMemberID: "m0",
		Members: []AutomationMember{{ID: "m0", PRDID: prds[0].ID, OnComplete: "m1"}, pendingMember("m1", item.ID, "")},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.automations.Update(a.ID, a.Version, func(cur *Automation) error {
		cur.Members[1].PendingItemID = "deadbeef"
		return nil
	})
	wantInvalid(t, err, "pending item deadbeef not found")
	if cur, _ := m.GetAutomation(a.ID); cur.Version != a.Version || cur.Members[1].PendingItemID != item.ID {
		t.Fatalf("store changed on a rejected update: %+v", cur.Members[1])
	}

	// A store with no lookup wired refuses a pending item member.
	bare, err := NewAutomationStore(t.TempDir(), func(string) (*PRD, bool) { return nil, false })
	if err != nil {
		t.Fatal(err)
	}
	_, err = bare.Create(&Automation{Name: "bare", Members: []AutomationMember{pendingMember("m0", item.ID, "")}})
	wantInvalid(t, err, "pending item registry is not available")
}

func TestPendingItemMemberDuplicateItemRejected(t *testing.T) {
	// The same item twice in one Automation.
	a := &Automation{
		Name: "dup", Status: AutomationDraft, EntryMemberID: "m0",
		Members: []AutomationMember{pendingMember("m0", "aaaa1111", "m1"), pendingMember("m1", "aaaa1111", "")},
	}
	wantInvalid(t, validateAutomation(a), "pending item aaaa1111 is a member more than once")

	// An item that already belongs to another non-terminal Automation.
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	item := newPendingItem(t, m, "shared work")
	first, err := m.CreateAutomation(AutomationInput{
		Name: "first", EntryMemberID: "m0", Members: []AutomationMember{pendingMember("m0", item.ID, "")},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.CreateAutomation(AutomationInput{
		Name: "second", EntryMemberID: "m0", Members: []AutomationMember{pendingMember("m0", item.ID, "")},
	}, "operator")
	wantInvalid(t, err, "already belongs to automation "+first.ID)

	// Update on a second Automation is refused the same way.
	other, err := m.CreateAutomation(AutomationInput{Name: "other"}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.automations.Update(other.ID, other.Version, func(cur *Automation) error {
		cur.Members = []AutomationMember{pendingMember("m0", item.ID, "")}
		cur.EntryMemberID = "m0"
		return nil
	})
	wantInvalid(t, err, "already belongs to automation "+first.ID)

	// Deleting the owner frees the item.
	if err := m.DeleteAutomation(first.ID); err != nil {
		t.Fatalf("DeleteAutomation: %v", err)
	}
	if owner := m.automations.PendingItemOwnerOf(item.ID); owner != "" {
		t.Fatalf("item still owned by %q after delete", owner)
	}
	if _, err := m.CreateAutomation(AutomationInput{
		Name: "third", EntryMemberID: "m0", Members: []AutomationMember{pendingMember("m0", item.ID, "")},
	}, "operator"); err != nil {
		t.Fatalf("item should be free after delete: %v", err)
	}
}

// pendingEntryHarness is a control harness whose Automation starts with a
// pending item member m0 holding the cursor, followed by PRD member m1.
func pendingEntryHarness(t *testing.T) (*coordHarness, *cancelStarter, *PendingItem, *Automation) {
	t.Helper()
	h, st := newControlHarness(t, 2)
	item := newPendingItem(t, h.m, "wait for the design")
	a := h.graph(pendingMember("m0", item.ID, "m1"), h.cm(1, "", "", ""))
	return h, st, item, a
}

func TestPendingItemMemberSkip(t *testing.T) {
	h, _, _, a := pendingEntryHarness(t)
	got, err := h.m.SkipMember(a.ID, "", "", "dave", a.Version)
	if err != nil {
		t.Fatalf("SkipMember: %v", err)
	}
	m0 := automationMember(got, "m0")
	if m0.Status != MemberSkipped || m0.EdgeTaken != edgeOnComplete || m0.FinishedAt == nil {
		t.Fatalf("m0 %s edge %q", m0.Status, m0.EdgeTaken)
	}
	if got.CurrentMemberID != "m1" {
		t.Fatalf("cursor %q, want m1", got.CurrentMemberID)
	}
	if next := automationMember(got, "m1"); next.Status != MemberRunning {
		t.Fatalf("next member %s", next.Status)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs %v", runs)
	}
	if det := h.auditDetail("automation_skip_member"); det == nil || det["member"] != "m0" || det["actor"] != "dave" {
		t.Fatalf("audit %v", det)
	}
}

func TestPendingItemMemberRemove(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	item := newPendingItem(t, h.m, "optional work")
	a := h.graph(
		h.cm(0, "m1", "m2", ""),
		pendingMember("m1", item.ID, ""),
		h.cm(2, "", "", ""),
	)
	if owner := h.m.automations.PendingItemOwnerOf(item.ID); owner != a.ID {
		t.Fatalf("item owner %q, want %q", owner, a.ID)
	}
	got, err := h.m.RemoveMember(a.ID, "m1", "erin", a.Version)
	if err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if automationMember(got, "m1") != nil || len(got.Members) != 2 || got.Version != a.Version+1 {
		t.Fatalf("members %d version %d", len(got.Members), got.Version)
	}
	if got.Members[0].OnComplete != "" {
		t.Fatalf("m0 still names the removed member: %q", got.Members[0].OnComplete)
	}
	if owner := h.m.automations.PendingItemOwnerOf(item.ID); owner != "" {
		t.Fatalf("removed item still owned by %q", owner)
	}
	// The item itself is untouched in the registry and can join another Automation.
	if p, ok := h.m.pendingItemByID(item.ID); !ok || p.State != PendingItemWaiting {
		t.Fatalf("item after remove: %+v ok=%v", p, ok)
	}
	if det := h.auditDetail("automation_remove_member"); det == nil || det["member"] != "m1" || det["actor"] != "erin" {
		t.Fatalf("audit %v", det)
	}
}

func TestPendingItemMemberStatusSummaryShowsWaiting(t *testing.T) {
	h, _, item, a := pendingEntryHarness(t)
	sums := h.m.automationStatusSummaries(time.Now())
	var found *AutomationStatusSummary
	for i := range sums {
		if sums[i].ID == a.ID {
			found = &sums[i]
		}
	}
	if found == nil || len(found.ActiveCursors) != 1 {
		t.Fatalf("summary %+v", found)
	}
	c := found.ActiveCursors[0]
	if c.MemberID != "m0" || c.Kind != MemberKindPending || c.PendingItemID != item.ID ||
		c.PendingItemTitle != "wait for the design" || !c.Waiting || c.PRDID != "" {
		t.Fatalf("cursor summary %+v", c)
	}
	raw, _ := json.Marshal(c)
	if !strings.Contains(string(raw), `"waiting":true`) || !strings.Contains(string(raw), `"kind":"pending_item"`) {
		t.Fatalf("cursor JSON %s", raw)
	}

	// Once the item is done it is no longer waiting.
	if _, err := h.m.pendingItems.Update(item.ID, func(p *PendingItem) error {
		now := time.Now()
		p.State = PendingItemDone
		p.DoneAt = &now
		return nil
	}); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	for _, s := range h.m.automationStatusSummaries(time.Now()) {
		if s.ID == a.ID && s.ActiveCursors[0].Waiting {
			t.Fatalf("done item still shown as waiting: %+v", s.ActiveCursors[0])
		}
	}
}

func TestPendingItemMemberCancelIsNotInFlight(t *testing.T) {
	h, st, _, a := pendingEntryHarness(t)
	if n := len(inFlightMembers(a)); n != 0 {
		t.Fatalf("a pending item member counted as in flight: %d", n)
	}
	got, err := h.m.CancelAutomation(a.ID, "dave", false, true, a.Version)
	if err != nil {
		t.Fatalf("CancelAutomation: %v", err)
	}
	if got.Status != AutomationCancelled {
		t.Fatalf("status %s", got.Status)
	}
	if m0 := automationMember(got, "m0"); m0.Status != MemberCancelled || m0.FinishedAt == nil {
		t.Fatalf("m0 %s", m0.Status)
	}
	if ended := st.ended(); len(ended) != 0 {
		t.Fatalf("cancel ended sessions for a pending item: %v", ended)
	}
	if det := h.auditDetail("automation_cancel"); det != nil {
		if ids, _ := det["in_flight"].([]string); len(ids) != 0 {
			t.Fatalf("in_flight %v", ids)
		}
	}
}

func TestPendingItemMemberCostAndBudgetAreZero(t *testing.T) {
	h, _, _, a := pendingEntryHarness(t)
	if got := h.m.AutomationSpendUSD(a, time.Time{}, ""); got != 0 {
		t.Fatalf("spend %v", got)
	}
	mem := automationMember(a, "m0")
	if v := h.m.budgetVerdictFor(a, mem); v.Gate {
		t.Fatalf("pending item gated: %+v", v)
	}
	_, err := h.m.SetAutomationBudgetPolicy(a.ID, BudgetSetRequest{MemberID: "m0", CeilingType: "dollar", CeilingValue: 5, Window: "automation"}, "operator", a.Version)
	if err == nil {
		t.Fatal("a budget on a pending item member should be refused")
	}
}
