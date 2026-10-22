package autonomous

import (
	"encoding/json"
	"fmt"
	"time"
)

// AutomationInput is the operator-facing shape for creating or updating an
// Automation over the wire (see the orchestration design doc, "Validation").
// It carries only the fields an operator may set directly; runtime state
// (status, cursor, member runtime fields, timestamps) is owned by the store.
type AutomationInput struct {
	Name                string             `json:"name"`
	Description         string             `json:"description"`
	EntryMemberID       string             `json:"entry_member_id"`
	Members             []AutomationMember `json:"members"`
	BudgetPolicy        *BudgetPolicy      `json:"budget_policy"`
	ProjectDir          string             `json:"project_dir"`
	Backend             string             `json:"backend"`
	Model               string             `json:"model"`
	Effort              Effort             `json:"effort"`
	VerificationBackend string             `json:"verification_backend"`
	VerificationModel   string             `json:"verification_model"`
	VerificationEffort  Effort             `json:"verification_effort"`
}

// CreateAutomation creates a new draft Automation from operator input,
// stamping every new member with the actor (defaulting to "operator") and
// delegating ID defaulting and validation to the store, like
// CreateRetryProfile delegates to SaveRetryProfile.
func (m *Manager) CreateAutomation(in AutomationInput, actor string) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	a := &Automation{
		Name:                in.Name,
		Description:         in.Description,
		EntryMemberID:       in.EntryMemberID,
		Members:             in.Members,
		BudgetPolicy:        in.BudgetPolicy,
		ProjectDir:          in.ProjectDir,
		Backend:             in.Backend,
		Model:               in.Model,
		Effort:              in.Effort,
		VerificationBackend: in.VerificationBackend,
		VerificationModel:   in.VerificationModel,
		VerificationEffort:  in.VerificationEffort,
	}
	for i := range a.Members {
		a.Members[i].AddedBy = actor
	}
	a, err := m.automations.Create(a)
	if err != nil {
		return nil, err
	}
	if err := m.syncAutomationMembership(a.ID, nil, a.Members, false); err != nil {
		return nil, err
	}
	return a, nil
}

// GetAutomation returns one Automation by id.
func (m *Manager) GetAutomation(id string) (*Automation, bool) {
	return m.automations.Get(id)
}

// ListAutomations returns all Automations.
func (m *Manager) ListAutomations() []*Automation {
	return m.automations.List()
}

// UpdateAutomation replaces the operator-mutable fields (Name, Description,
// BudgetPolicy, ProjectDir and the execution/verification defaults) plus
// Members and EntryMemberID. Members and EntryMemberID may only actually
// change while the Automation is a draft: an incoming members/entry that
// differs from the stored ones on a non-draft Automation is a 409. A blank
// Name keeps the existing name, like UpdateRetryProfile. New members get
// AddedBy=actor and AddedAt=now; members already in the stored Automation
// (matched by ID, or by PRD ID when the incoming member has no ID) keep
// their IDs and provenance.
func (m *Manager) UpdateAutomation(id string, expectedVersion int64, in AutomationInput, actor string) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	var oldMembers []AutomationMember
	a, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		oldMembers = append([]AutomationMember(nil), a.Members...)
		// Partial-update semantics: omitted members (nil) and a blank entry
		// member ID mean "leave unchanged", matching the blank-Name rule
		// and the operator contract ("provide only the fields to change")
		// — the REST body cannot distinguish an omitted list from an empty
		// one, and the dirty check below must only trip on fields the
		// caller actually supplied, or a plain name rename 409s on a
		// non-draft Automation and silently wipes a draft's members.
		membersProvided := in.Members != nil
		membersChanged := false
		if membersProvided {
			stored, err := json.Marshal(a.Members)
			if err != nil {
				return err
			}
			incoming, err := json.Marshal(in.Members)
			if err != nil {
				return err
			}
			membersChanged = string(incoming) != string(stored)
		}
		entryChanged := in.EntryMemberID != "" && in.EntryMemberID != a.EntryMemberID
		if membersChanged || entryChanged {
			if a.Status != AutomationDraft {
				return fmt.Errorf("409: members can only be edited while the automation is a draft")
			}
		}
		if in.Name != "" {
			a.Name = in.Name
		}
		a.Description = in.Description
		a.BudgetPolicy = in.BudgetPolicy
		a.ProjectDir = in.ProjectDir
		a.Backend = in.Backend
		a.Model = in.Model
		a.Effort = in.Effort
		a.VerificationBackend = in.VerificationBackend
		a.VerificationModel = in.VerificationModel
		a.VerificationEffort = in.VerificationEffort
		if in.EntryMemberID != "" {
			a.EntryMemberID = in.EntryMemberID
		}
		if !membersProvided {
			return nil
		}
		now := time.Now()
		byID := make(map[string]AutomationMember, len(a.Members))
		byPRD := make(map[string]AutomationMember, len(a.Members))
		for _, em := range a.Members {
			byID[em.ID] = em
			if em.PRDID != "" {
				if _, ok := byPRD[em.PRDID]; !ok {
					byPRD[em.PRDID] = em
				}
			}
		}
		members := make([]AutomationMember, 0, len(in.Members))
		for _, im := range in.Members {
			if em, ok := byID[im.ID]; ok {
				im.ID = em.ID
				im.AddedAt = em.AddedAt
				im.AddedBy = em.AddedBy
				im.AddedReason = em.AddedReason
			} else if im.ID == "" && im.PRDID != "" {
				if em, ok := byPRD[im.PRDID]; ok {
					im.ID = em.ID
					im.AddedAt = em.AddedAt
					im.AddedBy = em.AddedBy
					im.AddedReason = em.AddedReason
				}
			}
			if im.ID == "" {
				im.ID = newAutomationID()
			}
			if im.AddedAt.IsZero() {
				im.AddedAt = now
			}
			if im.AddedBy == "" {
				im.AddedBy = actor
			}
			members = append(members, im)
		}
		a.Members = members
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := m.syncAutomationMembership(id, oldMembers, a.Members, false); err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteAutomation removes an Automation (the store enforces the
// draft/completed/cancelled-only rule) and clears PRD.AutomationID on all
// of its member PRDs.
func (m *Manager) DeleteAutomation(id string) error {
	a, ok := m.automations.Get(id)
	if !ok {
		return fmt.Errorf("404: automation %s not found", id)
	}
	if err := m.automations.Delete(id); err != nil {
		return err
	}
	return m.syncAutomationMembership(id, a.Members, nil, true)
}

// syncAutomationMembership keeps PRD.AutomationID in sync with an
// Automation's membership set: every PRD newly in the set gets
// AutomationID=automationID, and every PRD removed from the set — or all
// members when the Automation is deleted or terminal — gets it cleared,
// but only when it still equals automationID. Persisted like the other
// PRD setters (GetPRD + SavePRD); nothing reads AutomationID for
// execution yet.
func (m *Manager) syncAutomationMembership(automationID string, oldMembers, newMembers []AutomationMember, terminal bool) error {
	// Only prd members own a PRD. Fork, join and nested members carry no
	// PRD ID, so they take no part in PRD membership.
	oldMembers = prdMembersOnly(oldMembers)
	newMembers = prdMembersOnly(newMembers)
	old := make(map[string]struct{}, len(oldMembers))
	for _, mem := range oldMembers {
		old[mem.PRDID] = struct{}{}
	}
	for _, mem := range newMembers {
		if _, was := old[mem.PRDID]; was {
			continue
		}
		prd, ok := m.store.GetPRD(mem.PRDID)
		if !ok {
			return fmt.Errorf("prd %q not found", mem.PRDID)
		}
		prd.AutomationID = automationID
		if err := m.store.SavePRD(prd); err != nil {
			return fmt.Errorf("prd %q: %w", mem.PRDID, err)
		}
	}
	if len(old) == 0 {
		return nil
	}
	if !terminal {
		newSet := make(map[string]struct{}, len(newMembers))
		for _, mem := range newMembers {
			newSet[mem.PRDID] = struct{}{}
		}
		for prdID := range old {
			if _, still := newSet[prdID]; still {
				delete(old, prdID)
			}
		}
	}
	for prdID := range old {
		prd, ok := m.store.GetPRD(prdID)
		if !ok {
			continue // the PRD itself is gone
		}
		if prd.AutomationID != automationID {
			continue // already points elsewhere
		}
		prd.AutomationID = ""
		if err := m.store.SavePRD(prd); err != nil {
			return fmt.Errorf("prd %q: %w", prdID, err)
		}
	}
	return nil
}
