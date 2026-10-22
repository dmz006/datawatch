// Automation member insert and approve (BL431 Phase 3b). An insert adds one PRD
// member to an Automation's chain. A draft takes a direct insert; an insert from
// an agent under agent_insert_policy=require_operator, and any insert into a
// non-draft Automation, is held as a PendingInsert instead of editing the graph,
// so the Automation keeps advancing until an operator approves it. An urgent
// insert takes the next slot, a normal one goes to the end of the chain, and at
// most one urgent insert is pending per Automation.

package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// Agent insert policy values (autonomous.automation.agent_insert_policy).
const (
	PolicyAllow           = "allow"
	PolicyRequireOperator = "require_operator"
)

// Insert audit actions and Decision kinds (one string serves both).
const (
	controlInsertMember  = "automation_insert_member"
	controlApproveInsert = "automation_approve_insert"
)

// operatorActor is the one actor that may approve a pending insert.
const operatorActor = "operator"

// InsertMemberInput is one request to insert a PRD member into an Automation.
type InsertMemberInput struct {
	PRDID string // the member PRD to insert
	Name  string // optional display name, kept on the pending record, decision and audit
	// AfterMemberID puts a normal insert directly after that member. Empty means
	// the end of the chain. An urgent insert always takes the next slot, so it
	// must leave this empty.
	AfterMemberID   string
	Urgent          bool   // take the next slot instead of the end of the chain
	Actor           string // "operator" (default), "session:<id>" or "agent:<id>"
	Reason          string // recorded as the member's AddedReason; required for a non-draft Automation
	ExpectedVersion int64  // the Automation Version the caller read
}

// PendingInsert is an insert held for operator approval. ID is the member ID the
// insert gets when approved, so it names both the pending insert and the member.
type PendingInsert struct {
	ID            string       `json:"id"`
	PRDID         string       `json:"prd_id"`
	Name          string       `json:"name,omitempty"`
	Urgent        bool         `json:"urgent,omitempty"`
	AfterMemberID string       `json:"after_member_id,omitempty"`
	Actor         string       `json:"actor"`
	Reason        string       `json:"reason,omitempty"`
	AddedAt       time.Time    `json:"added_at"`
	Status        MemberStatus `json:"status"` // MemberPending: visible, not yet in the graph
}

// isAgentActor reports whether actor is an agent or session rather than a person.
func isAgentActor(actor string) bool {
	return strings.HasPrefix(actor, "session:") || strings.HasPrefix(actor, "agent:")
}

// agentInsertPolicy returns the policy in effect; empty means allow.
func (m *Manager) agentInsertPolicy() string {
	if p := m.Config().Automation.AgentInsertPolicy; p != "" {
		return p
	}
	return PolicyAllow
}

// pendingUrgent returns the pending urgent insert of a other than exceptID.
func pendingUrgent(a *Automation, exceptID string) *PendingInsert {
	for i := range a.PendingInserts {
		if p := &a.PendingInserts[i]; p.Urgent && p.ID != exceptID {
			return p
		}
	}
	return nil
}

func urgentCapError(a *Automation, existing *PendingInsert) error {
	return fmt.Errorf("409: automation %s already has a pending urgent insert %s (prd %s); only one urgent insert may be pending at a time", a.ID, existing.ID, existing.PRDID)
}

// InsertMember inserts a PRD member. On a draft Automation the member lands in
// the graph at once, unless policy is require_operator and the actor is an agent,
// when it is held. On an active, paused or blocked Automation the insert is
// always held and needs a reason. A completed or cancelled Automation refuses it.
func (m *Manager) InsertMember(id string, in InsertMemberInput) (*Automation, error) {
	if in.Actor == "" {
		in.Actor = operatorActor
	}
	if strings.TrimSpace(in.PRDID) == "" {
		return nil, fmt.Errorf("insert needs a prd_id")
	}
	if in.Urgent && in.AfterMemberID != "" {
		return nil, fmt.Errorf("an urgent insert takes the next slot; leave after_member_id empty")
	}
	allowed := []AutomationStatus{AutomationDraft, AutomationActive, AutomationPaused, AutomationBlocked}
	a, err := m.loadForControl(id, in.ExpectedVersion, allowed...)
	if err != nil {
		return nil, err
	}
	if a.Status != AutomationDraft && strings.TrimSpace(in.Reason) == "" {
		return nil, fmt.Errorf("an insert into a %s automation needs a reason", a.Status)
	}
	if err := m.checkInsertPRD(a, in.PRDID); err != nil {
		return nil, err
	}
	hold := a.Status != AutomationDraft || (isAgentActor(in.Actor) && m.agentInsertPolicy() == PolicyRequireOperator)

	var oldMembers []AutomationMember
	var memberID string
	updated, err := m.automations.Update(id, in.ExpectedVersion, func(cur *Automation) error {
		if err := checkControlStatus(cur, allowed); err != nil {
			return err
		}
		if cur.Status != AutomationDraft && strings.TrimSpace(in.Reason) == "" {
			return fmt.Errorf("an insert into a %s automation needs a reason", cur.Status)
		}
		if in.Urgent {
			if existing := pendingUrgent(cur, ""); existing != nil {
				return urgentCapError(cur, existing)
			}
		}
		if err := checkPendingDuplicate(cur, in.PRDID); err != nil {
			return err
		}
		oldMembers = append([]AutomationMember(nil), cur.Members...)
		now := time.Now()
		memberID = newAutomationID()
		if hold {
			cur.PendingInserts = append(cur.PendingInserts, PendingInsert{
				ID: memberID, PRDID: in.PRDID, Name: in.Name, Urgent: in.Urgent,
				AfterMemberID: in.AfterMemberID, Actor: in.Actor, Reason: in.Reason,
				AddedAt: now, Status: MemberPending,
			})
			controlDecision(cur, controlInsertMember, in.Actor, insertNote("held pending operator approval", memberID, in))
			return nil
		}
		mem := AutomationMember{
			ID: memberID, PRDID: in.PRDID, AddedAt: now, AddedBy: in.Actor,
			AddedReason: in.Reason, Status: MemberPending,
		}
		if err := spliceMember(cur, mem, in.Urgent, in.AfterMemberID); err != nil {
			return err
		}
		controlDecision(cur, controlInsertMember, in.Actor, insertNote("inserted", memberID, in))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !hold {
		if err := m.syncAutomationMembership(id, oldMembers, updated.Members, false); err != nil {
			return nil, err
		}
	}
	m.controlAudit(controlInsertMember, map[string]any{
		"automation_id": id, "actor": in.Actor, "member": memberID, "prd_id": in.PRDID,
		"name": in.Name, "urgent": in.Urgent, "held": hold, "reason": in.Reason,
		"version": updated.Version,
	})
	return updated, nil
}

func insertNote(what, memberID string, in InsertMemberInput) string {
	kind := "normal"
	if in.Urgent {
		kind = "urgent"
	}
	note := fmt.Sprintf("%s %s insert of prd %s as member %s by %s", what, kind, in.PRDID, memberID, in.Actor)
	if in.Name != "" {
		note += " (" + in.Name + ")"
	}
	if in.Reason != "" {
		note += ": " + in.Reason
	}
	return note
}

// checkInsertPRD refuses a PRD that is already a member, or already pending, of a,
// or that another non-terminal Automation owns.
func (m *Manager) checkInsertPRD(a *Automation, prdID string) error {
	if _, ok := m.store.GetPRD(prdID); !ok {
		return fmt.Errorf("prd %s not found", prdID)
	}
	if owner := m.automations.OwnerOf(prdID); owner != "" && owner != a.ID {
		return fmt.Errorf("409: prd %s already belongs to automation %s", prdID, owner)
	}
	return checkPendingDuplicate(a, prdID)
}

func checkPendingDuplicate(a *Automation, prdID string) error {
	for _, mem := range a.Members {
		if mem.PRDID == prdID {
			return fmt.Errorf("409: prd %s is already a member of automation %s", prdID, a.ID)
		}
	}
	for _, p := range a.PendingInserts {
		if p.PRDID == prdID {
			return fmt.Errorf("409: prd %s already has pending insert %s in automation %s", prdID, p.ID, a.ID)
		}
	}
	return nil
}

// ApproveInsertedMember promotes a pending insert, named by its pending ID (which
// is also its member ID), into the graph at its reserved position. Only the
// operator may approve. A pending insert that is urgent while another urgent
// insert is pending is refused, naming the other.
func (m *Manager) ApproveInsertedMember(id, pendingID, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = operatorActor
	}
	if actor != operatorActor {
		return nil, fmt.Errorf("409: only the operator may approve a pending insert; actor %q cannot", actor)
	}
	allowed := []AutomationStatus{AutomationDraft, AutomationActive, AutomationPaused, AutomationBlocked}
	a, err := m.loadForControl(id, expectedVersion, allowed...)
	if err != nil {
		return nil, err
	}
	if findPending(a, pendingID) == nil {
		return nil, fmt.Errorf("404: automation %s has no pending insert %s", id, pendingID)
	}

	var oldMembers []AutomationMember
	var landed PendingInsert
	updated, err := m.automations.Update(id, expectedVersion, func(cur *Automation) error {
		if err := checkControlStatus(cur, allowed); err != nil {
			return err
		}
		p := findPending(cur, pendingID)
		if p == nil {
			return fmt.Errorf("404: automation %s has no pending insert %s", id, pendingID)
		}
		if p.Urgent {
			if other := pendingUrgent(cur, p.ID); other != nil {
				return urgentCapError(cur, other)
			}
		}
		landed = *p
		oldMembers = append([]AutomationMember(nil), cur.Members...)
		mem := AutomationMember{
			ID: landed.ID, PRDID: landed.PRDID, AddedAt: time.Now(), AddedBy: landed.Actor,
			AddedReason: landed.Reason, Status: MemberPending,
		}
		if err := spliceMember(cur, mem, landed.Urgent, landed.AfterMemberID); err != nil {
			return err
		}
		kept := cur.PendingInserts[:0:0]
		for _, q := range cur.PendingInserts {
			if q.ID != landed.ID {
				kept = append(kept, q)
			}
		}
		cur.PendingInserts = kept
		controlDecision(cur, controlApproveInsert, actor, fmt.Sprintf("approved insert of prd %s as member %s requested by %s", landed.PRDID, landed.ID, landed.Actor))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := m.syncAutomationMembership(id, oldMembers, updated.Members, false); err != nil {
		return nil, err
	}
	m.controlAudit(controlApproveInsert, map[string]any{
		"automation_id": id, "actor": actor, "member": landed.ID, "prd_id": landed.PRDID,
		"urgent": landed.Urgent, "requested_by": landed.Actor, "version": updated.Version,
	})
	return updated, nil
}

func findPending(a *Automation, id string) *PendingInsert {
	for i := range a.PendingInserts {
		if a.PendingInserts[i].ID == id {
			return &a.PendingInserts[i]
		}
	}
	return nil
}

// spliceMember adds mem to a's chain by rewiring on_complete edges. An urgent
// member takes the next slot: on a draft it becomes the entry member, and on a
// started Automation it goes between the one running member and that member's
// on_complete target. A normal member goes after afterID, or after the last
// member of the chain when afterID is empty.
func spliceMember(a *Automation, mem AutomationMember, urgent bool, afterID string) error {
	if len(a.Members) == 0 {
		a.Members = []AutomationMember{mem}
		a.EntryMemberID = mem.ID
		return nil
	}
	switch {
	case urgent && a.Status == AutomationDraft:
		mem.OnComplete = a.EntryMemberID
		a.Members = append(a.Members, mem)
		a.EntryMemberID = mem.ID
		return nil
	case urgent:
		cursors := liveCursorIDs(a)
		if len(cursors) != 1 {
			return fmt.Errorf("409: an urgent insert needs exactly one active member to follow; automation %s has %d [%s]", a.ID, len(cursors), strings.Join(cursors, " "))
		}
		afterID = cursors[0]
	case afterID == "":
		tail, err := chainTail(a)
		if err != nil {
			return err
		}
		afterID = tail
	}
	prev := automationMember(a, afterID)
	if prev == nil {
		return fmt.Errorf("404: member %s not found in automation %s", afterID, a.ID)
	}
	if k := prev.kind(); k == MemberKindFork {
		return fmt.Errorf("409: member %s is a fork and carries no on_complete edge; insert after its join", prev.ID)
	}
	if !urgent && a.Status != AutomationDraft && prev.Status != MemberPending && prev.Status != MemberRunning && prev.Status != "" {
		return fmt.Errorf("409: member %s is already %s; an insert cannot follow a member that has finished", prev.ID, prev.Status)
	}
	mem.OnComplete = prev.OnComplete
	prev.OnComplete = mem.ID
	a.Members = append(a.Members, mem)
	return nil
}

// chainTail returns the ID of the last member of a's chain: it follows
// on_complete from the entry member, crossing a fork to its paired join.
func chainTail(a *Automation) (string, error) {
	cur := a.EntryMemberID
	seen := make(map[string]bool, len(a.Members))
	for {
		if seen[cur] {
			return "", fmt.Errorf("409: automation %s chain loops at member %s", a.ID, cur)
		}
		seen[cur] = true
		mem := automationMember(a, cur)
		if mem == nil {
			return "", fmt.Errorf("404: member %s not found in automation %s", cur, a.ID)
		}
		if mem.kind() == MemberKindFork {
			join := ""
			for _, j := range a.Members {
				if j.kind() == MemberKindJoin && sameMemberSet(j.JoinMembers, mem.ForkMembers) {
					join = j.ID
				}
			}
			if join == "" {
				return "", fmt.Errorf("409: fork %s has no paired join", mem.ID)
			}
			cur = join
			continue
		}
		if mem.OnComplete == "" {
			return cur, nil
		}
		cur = mem.OnComplete
	}
}
