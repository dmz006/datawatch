// Attaching an Automaton to a pending item. A pending item is named work with
// no Automaton yet; an Automation member can wait on it (see
// pending_item_hold.go). Attaching turns that wait into an ordinary Automaton
// member: every Automation that holds the item has the member converted in
// place (same position, edges and status), a held insert for the item is
// converted the same way, and the item moves from waiting to attached. An
// Automation that was blocked on the member is resumed through the same path
// as an operator's resume, unless auto-resume is off. When the converted
// member's Automaton later completes, the item moves from attached to done.

package autonomous

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// Audit action and Decision kind for an attach (one string serves both), and
// the audit action when an attached item's Automaton completes.
const (
	controlPendingItemAttach = "pending_item_attach"
	controlPendingItemDone   = "pending_item_done"
)

// pendingItemResumeTries bounds how often a resume is retried when the
// Automation's version moves between the attach and the resume.
const pendingItemResumeTries = 3

// pendingItemAttachMu serializes attaches, so two callers cannot both see the
// same item waiting and attach it twice.
var pendingItemAttachMu sync.Mutex

// PendingItemAttachConversion names what an attach converted in one Automation.
type PendingItemAttachConversion struct {
	AutomationID string   `json:"automation_id"`
	MemberIDs    []string `json:"member_ids,omitempty"`         // pending item members now Automaton members
	InsertIDs    []string `json:"pending_insert_ids,omitempty"` // held inserts now carrying the Automaton
}

// PendingItemAttachResult is what AttachPendingItem did.
type PendingItemAttachResult struct {
	Item      *PendingItem                  `json:"item"`
	Converted []PendingItemAttachConversion `json:"converted_automations"`
	Resumed   []string                      `json:"resumed_automations"` // Automations the attach resumed
	Notes     []string                      `json:"notes,omitempty"`     // Automations left blocked, and why
}

// attachPlan is what the attach finds out about one Automation before it
// changes anything.
type attachPlan struct {
	automationID string
	holdsMember  bool
	holdsInsert  bool
}

// AttachPendingItem attaches the Automaton automatonID to the waiting pending
// item itemID. The item must be waiting (409 otherwise, so attaching twice is
// refused); the Automaton must exist (404) and must not already belong to a
// non-terminal Automation, be held in an insert, or be attached to another
// item (409).
func (m *Manager) AttachPendingItem(itemID, automatonID, actor string) (*PendingItemAttachResult, error) {
	itemID, automatonID = strings.TrimSpace(itemID), strings.TrimSpace(automatonID)
	if actor == "" {
		actor = operatorActor
	}
	if automatonID == "" {
		return nil, fmt.Errorf("400: an attach needs the id of the Automaton to attach")
	}
	pendingItemAttachMu.Lock()
	defer pendingItemAttachMu.Unlock()

	item, err := m.GetPendingItem(itemID)
	if err != nil {
		return nil, err
	}
	if err := checkAttachableItem(item); err != nil {
		return nil, err
	}
	plans, err := m.planAttach(item, automatonID)
	if err != nil {
		return nil, err
	}

	autoResume := m.Config().Automation.EffectivePendingItemAutoResume()
	res := &PendingItemAttachResult{Converted: []PendingItemAttachConversion{}, Resumed: []string{}}
	var blocked []string // Automations that were blocked on the converted member
	for _, p := range plans {
		conv, wasBlocked, err := m.convertForAttach(p, item, automatonID, actor, autoResume)
		if err != nil {
			return nil, err
		}
		res.Converted = append(res.Converted, conv)
		if wasBlocked {
			blocked = append(blocked, p.automationID)
		}
	}

	attached, err := m.pendingItems.Update(itemID, func(cur *PendingItem) error {
		if err := checkAttachableItem(cur); err != nil {
			return err
		}
		cur.State, cur.AutomatonID = PendingItemAttached, automatonID
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("500: the Automaton was placed in %d automation(s) but pending item %s could not be marked attached: %w", len(plans), itemID, err)
	}
	res.Item = attached

	m.controlAudit(controlPendingItemAttach, map[string]any{
		"pending_item_id": attached.ID, "ref": attached.Ref, "title": attached.Title,
		"automaton_id": automatonID, "actor": actor, "converted": res.Converted,
	})

	for _, id := range blocked {
		if !autoResume {
			res.Notes = append(res.Notes, fmt.Sprintf("automation %s stays blocked: auto-resume is off, so resume it to start the Automaton", id))
			continue
		}
		if err := m.resumeAfterAttach(id, actor); err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("automation %s was not resumed: %v", id, err))
			continue
		}
		res.Resumed = append(res.Resumed, id)
	}
	return res, nil
}

// checkAttachableItem returns a 409 unless the item is waiting.
func checkAttachableItem(item *PendingItem) error {
	switch item.State {
	case PendingItemWaiting:
		return nil
	case PendingItemAttached:
		return fmt.Errorf("409: pending item %s already has Automaton %s attached", item.ID, item.AutomatonID)
	}
	return fmt.Errorf("409: pending item %s is %s; only a waiting pending item can have an Automaton attached", item.ID, item.State)
}

// planAttach checks the Automaton and finds every non-terminal Automation that
// holds the item, as a member or as a held insert.
func (m *Manager) planAttach(item *PendingItem, automatonID string) ([]attachPlan, error) {
	rec, ok := m.store.GetPRD(automatonID)
	if !ok {
		return nil, fmt.Errorf("404: Automaton %s not found", automatonID)
	}
	var template bool
	var parent string
	m.readLocked(func() { template, parent = rec.IsTemplate, rec.ParentPRDID })
	if template {
		return nil, fmt.Errorf("409: Automaton %s is a template and cannot be attached to a pending item", automatonID)
	}
	if parent != "" {
		return nil, fmt.Errorf("409: Automaton %s is a child Automaton run by its parent and cannot be attached to a pending item", automatonID)
	}
	for _, other := range m.pendingItems.List(PendingItemFilter{State: PendingItemAttached}) {
		if other.AutomatonID == automatonID {
			return nil, fmt.Errorf("409: Automaton %s is already attached to pending item %s", automatonID, other.ID)
		}
	}

	var plans []attachPlan
	for _, a := range m.automations.List() {
		if a.Status == AutomationCompleted || a.Status == AutomationCancelled {
			continue
		}
		p := attachPlan{automationID: a.ID}
		for _, mem := range a.Members {
			if mem.kind() == MemberKindPending && mem.PendingItemID == item.ID {
				p.holdsMember = true
			}
		}
		for _, ins := range a.PendingInserts {
			if ins.PendingItemID == item.ID && ins.PRDID == "" {
				p.holdsInsert = true
			}
			if ins.PRDID == automatonID {
				return nil, fmt.Errorf("409: Automaton %s already has pending insert %s in automation %s", automatonID, ins.ID, a.ID)
			}
		}
		if p.holdsMember || p.holdsInsert {
			plans = append(plans, p)
		}
	}
	if owner := m.automations.OwnerOf(automatonID); owner != "" {
		for _, p := range plans {
			if p.automationID == owner {
				return nil, fmt.Errorf("409: Automaton %s is already a member of automation %s", automatonID, owner)
			}
		}
		return nil, fmt.Errorf("409: Automaton %s already belongs to automation %s", automatonID, owner)
	}
	return plans, nil
}

// convertForAttach converts the item's member and held insert in one
// Automation, inside the store's Update, and brings the ownership indexes in
// line. wasBlocked reports that the Automation was blocked with the converted
// member at its cursor, so a resume would start it.
func (m *Manager) convertForAttach(p attachPlan, item *PendingItem, automatonID, actor string, autoResume bool) (PendingItemAttachConversion, bool, error) {
	conv := PendingItemAttachConversion{AutomationID: p.automationID}
	var oldMembers []AutomationMember
	var wasBlocked bool
	updated, err := m.automations.Update(p.automationID, 0, func(cur *Automation) error {
		conv.MemberIDs, conv.InsertIDs, wasBlocked = nil, nil, false
		oldMembers = append([]AutomationMember(nil), cur.Members...)
		for i := range cur.Members {
			mem := &cur.Members[i]
			if mem.kind() != MemberKindPending || mem.PendingItemID != item.ID {
				continue
			}
			mem.Kind, mem.PRDID = MemberKindPRD, automatonID
			conv.MemberIDs = append(conv.MemberIDs, mem.ID)
			if cur.Status == AutomationBlocked && isLiveCursor(cur, mem.ID) && memberUnstarted(mem) && !budgetEpisodeOpen(cur) {
				wasBlocked = true
			}
		}
		for i := range cur.PendingInserts {
			ins := &cur.PendingInserts[i]
			if ins.PendingItemID == item.ID && ins.PRDID == "" {
				ins.PRDID = automatonID
				conv.InsertIDs = append(conv.InsertIDs, ins.ID)
			}
		}
		if len(conv.MemberIDs) == 0 && len(conv.InsertIDs) == 0 {
			return fmt.Errorf("409: automation %s no longer holds pending item %s", cur.ID, item.ID)
		}
		controlDecision(cur, controlPendingItemAttach, actor, attachNote(item, automatonID, actor, conv, wasBlocked, autoResume))
		return nil
	})
	if err != nil {
		return conv, false, err
	}
	if len(conv.MemberIDs) > 0 {
		if err := m.syncAutomationMembership(p.automationID, oldMembers, updated.Members, false); err != nil {
			return conv, false, fmt.Errorf("500: automation %s took the Automaton but its ownership could not be saved: %w", p.automationID, err)
		}
	}
	return conv, wasBlocked, nil
}

// attachNote is the Decision text recorded on an Automation the attach changed.
func attachNote(item *PendingItem, automatonID, actor string, conv PendingItemAttachConversion, wasBlocked, autoResume bool) string {
	subject := "pending item " + item.ID
	if item.Title != "" {
		subject += " (" + item.Title + ")"
	}
	note := fmt.Sprintf("%s: Automaton %s attached by %s", subject, automatonID, actor)
	if n := len(conv.MemberIDs); n > 0 {
		note += fmt.Sprintf("; %d member(s) converted in place [%s]", n, strings.Join(conv.MemberIDs, " "))
	}
	if n := len(conv.InsertIDs); n > 0 {
		note += fmt.Sprintf("; %d held insert(s) converted [%s]", n, strings.Join(conv.InsertIDs, " "))
	}
	switch {
	case wasBlocked && autoResume:
		note += "; the Automation was blocked on it and is being resumed"
	case wasBlocked:
		note += "; the Automation stays blocked because auto-resume is off: resume it to start the Automaton"
	}
	return note
}

// resumeAfterAttach resumes Automation id through ResumeAutomation, the path an
// operator's resume takes, so the converted member starts through the normal
// gate. A version that moves under the call is retried.
func (m *Manager) resumeAfterAttach(id, actor string) error {
	var err error
	for try := 0; try < pendingItemResumeTries; try++ {
		if _, err = m.ResumeAutomation(id, actor, m.currentVersion(id)); err == nil {
			return nil
		}
		if !strings.HasPrefix(err.Error(), "409: version mismatch") {
			return err
		}
	}
	return err
}

// completeAttachedPendingItem moves a pending item from attached to done once
// the Automaton attached to it has completed. Any other state, or an item
// attached to a different Automaton, is left alone.
func (m *Manager) completeAttachedPendingItem(itemID, automatonID string) {
	if itemID == "" || m == nil || m.pendingItems == nil {
		return
	}
	done, err := m.pendingItems.Update(itemID, func(cur *PendingItem) error {
		if cur.State != PendingItemAttached || cur.AutomatonID != automatonID {
			return errPendingItemNotAttached
		}
		cur.State = PendingItemDone
		return nil
	})
	switch {
	case err == nil:
		m.controlAudit(controlPendingItemDone, map[string]any{
			"pending_item_id": done.ID, "ref": done.Ref, "title": done.Title,
			"automaton_id": automatonID, "actor": coordinatorActor,
		})
	case err != errPendingItemNotAttached:
		log.Printf("[autonomous] pending item %s: mark done after Automaton %s completed: %v", itemID, automatonID, err)
	}
}

// errPendingItemNotAttached stops completeAttachedPendingItem's update when
// there is nothing to move.
var errPendingItemNotAttached = fmt.Errorf("pending item is not attached to this Automaton")
