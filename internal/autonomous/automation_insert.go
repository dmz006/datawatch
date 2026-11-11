// Automation member insert and approve (BL431 Phase 3b). An insert adds one
// Automaton member, or one pending item, to an Automation's chain. A draft takes a direct insert; an insert from
// an agent under agent_insert_policy=require_operator, and any insert into a
// non-draft Automation, is held as a PendingInsert instead of editing the graph,
// so the Automation keeps advancing until an operator approves it. An urgent
// insert takes the next slot, a normal one goes to the end of the chain, and at
// most one urgent insert is pending per Automation.

package autonomous

import (
	"context"
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

// InsertMemberInput is one request to insert a member into an Automation. It
// names exactly one of an Automaton (PRDID) or a pending item (PendingItemID).
type InsertMemberInput struct {
	PRDID         string // the member Automaton to insert
	PendingItemID string // the pending item to insert instead; exclusive with PRDID
	Name          string // optional display name, kept on the pending record, decision and audit
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
	PendingItemID string       `json:"pending_item_id,omitempty"` // set, with an empty PRDID, for a pending item insert
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
	return strings.HasPrefix(actor, "session:") || strings.HasPrefix(actor, "agent:") || strings.HasPrefix(actor, "assistant:")
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

// subject names what a pending insert carries, for messages.
func (p PendingInsert) subject() string {
	if p.PendingItemID != "" {
		return "pending item " + p.PendingItemID
	}
	return "Automaton " + p.PRDID
}

func urgentCapError(a *Automation, existing *PendingInsert) error {
	return fmt.Errorf("409: automation %s already has a pending urgent insert %s (%s); only one urgent insert may be pending at a time", a.ID, existing.ID, existing.subject())
}

// InsertMember inserts an Automaton or a pending item member. On a draft Automation the member lands in
// the graph at once, unless policy is require_operator and the actor is an agent,
// when it is held. On an active, paused or blocked Automation the insert is
// always held and needs a reason. A completed or cancelled Automation refuses it.
func (m *Manager) InsertMember(id string, in InsertMemberInput) (*Automation, error) {
	if in.Actor == "" {
		in.Actor = operatorActor
	}
	hasPRD, hasItem := strings.TrimSpace(in.PRDID) != "", strings.TrimSpace(in.PendingItemID) != ""
	if hasPRD == hasItem {
		return nil, fmt.Errorf("400: an insert needs exactly one of prd_id (an Automaton) or pending_item_id (a pending item)")
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
	if hasItem {
		err = m.checkInsertPendingItem(a, in.PendingItemID)
	} else {
		err = m.checkInsertPRD(a, in.PRDID)
	}
	if err != nil {
		return nil, err
	}
	hold := a.Status != AutomationDraft || (isAgentActor(in.Actor) && m.agentInsertPolicy() == PolicyRequireOperator)

	var oldMembers []AutomationMember
	var memberID string
	itemTitle := m.pendingItemTitle(in.PendingItemID)
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
		if err := checkPendingDuplicate(cur, in.PRDID, in.PendingItemID); err != nil {
			return err
		}
		oldMembers = append([]AutomationMember(nil), cur.Members...)
		now := time.Now()
		memberID = newAutomationID()
		if hold {
			cur.PendingInserts = append(cur.PendingInserts, PendingInsert{
				ID: memberID, PRDID: in.PRDID, PendingItemID: in.PendingItemID, Name: in.Name, Urgent: in.Urgent,
				AfterMemberID: in.AfterMemberID, Actor: in.Actor, Reason: in.Reason,
				AddedAt: now, Status: MemberPending,
			})
			controlDecision(cur, controlInsertMember, in.Actor, insertNote("held pending operator approval", memberID, itemTitle, in))
			return nil
		}
		mem := newInsertedMember(memberID, in.PRDID, in.PendingItemID, in.Actor, in.Reason, now)
		if err := spliceMember(cur, mem, in.Urgent, in.AfterMemberID); err != nil {
			return err
		}
		controlDecision(cur, controlInsertMember, in.Actor, insertNote("inserted", memberID, itemTitle, in))
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
	details := map[string]any{
		"automation_id": id, "actor": in.Actor, "member": memberID, "prd_id": in.PRDID,
		"name": in.Name, "urgent": in.Urgent, "held": hold, "reason": in.Reason,
		"version": updated.Version,
	}
	m.addItemDetails(details, in.PendingItemID)
	m.controlAudit(controlInsertMember, details)
	return updated, nil
}

// AssistInsertCandidate is a corrective PRD the assistant may propose inserting.
type AssistInsertCandidate struct {
	PRDID string
	Name  string
}

// AssistInsertTriage asks the decision assistant whether to insert one of the
// candidate corrective PRDs after memberID. A live, confident pick of
// insert:<prd_id> goes through InsertMember with actor "assistant:<model>" and
// the assistant's reason as AddedReason. The insert is held as a PendingInsert
// and needs the operator's approval before it joins the graph, so it never
// runs unapproved; a draft Automation (where an insert would land at once) is
// refused. Escalate results and shadow mode insert nothing.
func (m *Manager) AssistInsertTriage(ctx context.Context, id, memberID string, candidates []AssistInsertCandidate) (AssistantResult, *Automation, error) {
	a, err := m.loadForControl(id, m.currentVersion(id), AutomationActive, AutomationPaused, AutomationBlocked)
	if err != nil {
		return AssistantResult{}, nil, err
	}
	if automationMember(a, memberID) == nil {
		return AssistantResult{}, nil, fmt.Errorf("404: automation %s has no member %s", id, memberID)
	}
	if !m.assistantHookOn(id, memberID, DecisionHookInsertMember) {
		return AssistantResult{Outcome: AssistantEscalate, Hook: DecisionHookInsertMember, Reason: "decision assistant is off or the hook is disabled"}, a, nil
	}
	options := make([]string, 0, len(candidates)+1)
	byOption := make(map[string]AssistInsertCandidate, len(candidates))
	for _, c := range candidates {
		o := assistOptionInsertPrefix + c.PRDID
		options = append(options, o)
		byOption[o] = c
	}
	options = append(options, assistOptionNeedsOperator)
	res := m.decide(ctx, id, memberID, DecisionHookInsertMember, DecisionEvidence{
		MemberID: memberID,
		Progress: automationProgress(a),
	}, options)
	if !res.Actionable() {
		return res, a, nil
	}
	c, ok := byOption[res.Option]
	if !ok {
		return res, a, nil // needs_operator
	}
	updated, err := m.InsertMember(id, InsertMemberInput{
		PRDID: c.PRDID, Name: c.Name, AfterMemberID: memberID,
		Actor: m.assistantActorFor(id, memberID), Reason: res.Reason, ExpectedVersion: m.currentVersion(id),
	})
	if err != nil {
		return res, a, err
	}
	return res, updated, nil
}

func insertNote(what, memberID, title string, in InsertMemberInput) string {
	kind := "normal"
	if in.Urgent {
		kind = "urgent"
	}
	subject := "Automaton " + in.PRDID
	if in.PendingItemID != "" {
		subject = "pending item " + in.PendingItemID
		if title != "" {
			subject += " (" + title + ")"
		}
	}
	note := fmt.Sprintf("%s %s insert of %s as member %s by %s", what, kind, subject, memberID, in.Actor)
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
	return checkPendingDuplicate(a, prdID, "")
}

// checkInsertPendingItem refuses a pending item that does not exist (404), is not
// waiting (409), or is already a member, or a pending insert, of a or of any
// other non-terminal Automation (409).
func (m *Manager) checkInsertPendingItem(a *Automation, itemID string) error {
	item, err := m.GetPendingItem(itemID)
	if err != nil {
		return err
	}
	if item.State != PendingItemWaiting {
		return fmt.Errorf("409: pending item %s is %s; only a waiting pending item can be inserted", itemID, item.State)
	}
	if owner := m.automations.PendingItemOwnerOf(itemID); owner != "" && owner != a.ID {
		return fmt.Errorf("409: pending item %s already belongs to automation %s", itemID, owner)
	}
	for _, other := range m.automations.List() {
		if other.ID == a.ID || other.Status == AutomationCompleted || other.Status == AutomationCancelled {
			continue
		}
		for _, p := range other.PendingInserts {
			if p.PendingItemID == itemID {
				return fmt.Errorf("409: pending item %s already has pending insert %s in automation %s", itemID, p.ID, other.ID)
			}
		}
	}
	return checkPendingDuplicate(a, "", itemID)
}

// checkPendingDuplicate refuses an Automaton (prdID) or a pending item (itemID)
// that is already a member, or already pending, of a. Pass "" for the one that
// is not being inserted.
func checkPendingDuplicate(a *Automation, prdID, itemID string) error {
	for _, mem := range a.Members {
		if prdID != "" && mem.PRDID == prdID {
			return fmt.Errorf("409: prd %s is already a member of automation %s", prdID, a.ID)
		}
		if itemID != "" && mem.PendingItemID == itemID {
			return fmt.Errorf("409: pending item %s is already a member of automation %s", itemID, a.ID)
		}
	}
	for _, p := range a.PendingInserts {
		if prdID != "" && p.PRDID == prdID {
			return fmt.Errorf("409: prd %s already has pending insert %s in automation %s", prdID, p.ID, a.ID)
		}
		if itemID != "" && p.PendingItemID == itemID {
			return fmt.Errorf("409: pending item %s already has pending insert %s in automation %s", itemID, p.ID, a.ID)
		}
	}
	return nil
}

// newInsertedMember builds the member an insert lands: an Automaton member, or a
// pending_item member (no prd_id) when itemID is set.
func newInsertedMember(id, prdID, itemID, actor, reason string, at time.Time) AutomationMember {
	mem := AutomationMember{ID: id, AddedAt: at, AddedBy: actor, AddedReason: reason, Status: MemberPending}
	if itemID != "" {
		mem.Kind, mem.PendingItemID = MemberKindPending, itemID
	} else {
		mem.PRDID = prdID
	}
	return mem
}

// pendingItemTitle returns the title of item id, or "" when there is none.
func (m *Manager) pendingItemTitle(id string) string {
	if id == "" {
		return ""
	}
	if item, err := m.GetPendingItem(id); err == nil {
		return item.Title
	}
	return ""
}

// addItemDetails adds the item's id and title to audit details; no-op for "".
func (m *Manager) addItemDetails(details map[string]any, itemID string) {
	if itemID == "" {
		return
	}
	details["pending_item_id"] = itemID
	details["title"] = m.pendingItemTitle(itemID)
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
		landedSubject := landed.subject()
		if landed.PendingItemID != "" {
			if title := m.pendingItemTitle(landed.PendingItemID); title != "" {
				landedSubject += " (" + title + ")"
			}
		}
		oldMembers = append([]AutomationMember(nil), cur.Members...)
		mem := newInsertedMember(landed.ID, landed.PRDID, landed.PendingItemID, landed.Actor, landed.Reason, time.Now())
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
		controlDecision(cur, controlApproveInsert, actor, fmt.Sprintf("approved insert of %s as member %s requested by %s", landedSubject, landed.ID, landed.Actor))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := m.syncAutomationMembership(id, oldMembers, updated.Members, false); err != nil {
		return nil, err
	}
	details := map[string]any{
		"automation_id": id, "actor": actor, "member": landed.ID, "prd_id": landed.PRDID,
		"urgent": landed.Urgent, "requested_by": landed.Actor, "version": updated.Version,
	}
	m.addItemDetails(details, landed.PendingItemID)
	m.controlAudit(controlApproveInsert, details)
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
