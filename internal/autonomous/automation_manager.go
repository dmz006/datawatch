package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
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
	PlanningBackend     string             `json:"planning_backend"`
	PlanningModel       string             `json:"planning_model"`
	VerificationBackend string             `json:"verification_backend"`
	VerificationModel   string             `json:"verification_model"`
	VerificationEffort  Effort             `json:"verification_effort"`
	// DecisionAssistant is optional; an update that omits it keeps the stored
	// setting (SetDecisionAssistant clears it).
	DecisionAssistant *DecisionAssistant `json:"decision_assistant,omitempty"`
	// AutoTags is the optional per-Automation override of the daemon-level
	// autonomous.auto_tags block; an update that omits it (nil) keeps the
	// stored setting, and a non-nil override's unset fields inherit the
	// daemon's.
	AutoTags *AutoTagsOverride `json:"auto_tags,omitempty"`
	// Milestone marks the Automation as a milestone arc. Create takes it as
	// given (omitted means false); an update changes it only when the field is
	// present, so nil keeps the stored value.
	Milestone *bool `json:"milestone,omitempty"`
	// MaxParallel caps how many members of the Automation run at once (0 or
	// unset = no limit). Create takes it as given; an update changes it only
	// when the field is present, so nil keeps the stored value.
	MaxParallel *int `json:"max_parallel,omitempty"`
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
		PlanningBackend:     in.PlanningBackend,
		PlanningModel:       in.PlanningModel,
		VerificationBackend: in.VerificationBackend,
		VerificationModel:   in.VerificationModel,
		VerificationEffort:  in.VerificationEffort,
		DecisionAssistant:   in.DecisionAssistant,
		AutoTags:            in.AutoTags,
	}
	if in.Milestone != nil {
		a.Milestone = *in.Milestone
	}
	if in.MaxParallel != nil {
		a.MaxParallel = *in.MaxParallel
	}
	for i := range a.Members {
		a.Members[i].AddedBy = actor
	}
	a, err := m.automations.Create(a)
	if err != nil {
		return nil, err
	}
	if err := m.syncAutomationMembership(a.ID, nil, a.Members, false); err != nil {
		// A refused tag sync (e.g. a name an operator tag owns) must not
		// leave an Automation behind: roll the creation back.
		_ = m.automations.Delete(a.ID)
		return nil, err
	}
	return a, nil
}

// GetAutomation returns one Automation by id.
func (m *Manager) GetAutomation(id string) (*Automation, bool) {
	return m.automations.Get(id)
}

// AutomationForPRD returns a copy of the Automation that prd is a member of,
// or nil when prd is nil, the Manager has no Automation store, or prd is
// standalone. A member of a nested child Automation resolves to that child
// only; nothing is inherited from the parent.
func (m *Manager) AutomationForPRD(prd *PRD) *Automation {
	if prd == nil || m.automations == nil {
		return nil
	}
	id := prd.AutomationID
	if id == "" {
		id = m.automations.OwnerOf(prd.ID)
	}
	if id == "" {
		return nil
	}
	a, ok := m.automations.Get(id)
	if !ok {
		return nil
	}
	return a
}

// StageOfPRD returns the Automaton's stage name — the label operator gave the
// PRD's member slot of its Automation — or "" when the PRD is not
// stage-typed: the PRD is standalone, is not a member of an Automation, or
// its member slot's Stage label is empty. The empty value is what the
// placement stage axis reads as "no stage": the axis takes no tag on it.
func (m *Manager) StageOfPRD(prd *PRD) string {
	if prd == nil {
		return ""
	}
	a := m.AutomationForPRD(prd)
	if a == nil {
		return ""
	}
	for i := range a.Members {
		if a.Members[i].PRDID == prd.ID {
			return a.Members[i].Stage
		}
	}
	return ""
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
		a.PlanningBackend = in.PlanningBackend
		a.PlanningModel = in.PlanningModel
		a.VerificationBackend = in.VerificationBackend
		a.VerificationModel = in.VerificationModel
		a.VerificationEffort = in.VerificationEffort
		if in.DecisionAssistant != nil {
			a.DecisionAssistant = in.DecisionAssistant
		}
		if in.AutoTags != nil {
			a.AutoTags = in.AutoTags
		}
		if in.Milestone != nil {
			a.Milestone = *in.Milestone
		}
		if in.MaxParallel != nil {
			a.MaxParallel = *in.MaxParallel
		}
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
				if im.DecisionAssistant == nil {
					im.DecisionAssistant = em.DecisionAssistant
				}
			} else if im.ID == "" && im.PRDID != "" {
				if em, ok := byPRD[im.PRDID]; ok {
					im.ID = em.ID
					im.AddedAt = em.AddedAt
					im.AddedBy = em.AddedBy
					im.AddedReason = em.AddedReason
					if im.DecisionAssistant == nil {
						im.DecisionAssistant = em.DecisionAssistant
					}
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

// RenameAutomationStage renames the Stage label from→to on every member of
// automationID that currently carries from. Both names are trimmed and must
// be non-empty and different; to must not already be another member's stage
// in the same Automation. A stale expectedVersion, a terminal Automation or
// a rename that matches no member is refused before anything mutates; on
// success the Version is bumped (through the same compare-and-set the
// coordinator uses) and one Decision is recorded and one audit record
// emitted.
func (m *Manager) RenameAutomationStage(automationID, from, to string, expectedVersion int64) (*Automation, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return nil, fmt.Errorf("a stage rename needs non-empty from and to names")
	}
	if from == to {
		return nil, fmt.Errorf("a stage rename needs different from and to names")
	}
	allowed := []AutomationStatus{AutomationDraft, AutomationActive, AutomationPaused, AutomationBlocked}
	if _, err := m.loadForControl(automationID, expectedVersion, allowed...); err != nil {
		return nil, err
	}
	var renamed int
	updated, err := m.automations.Update(automationID, expectedVersion, func(cur *Automation) error {
		if err := checkControlStatus(cur, allowed); err != nil {
			return err
		}
		for i := range cur.Members {
			mm := &cur.Members[i]
			if mm.Stage != from {
				if mm.Stage == to {
					return fmt.Errorf("409: automation %s already has stage %q on member %s", cur.ID, to, mm.ID)
				}
				continue
			}
			mm.Stage = to
			renamed++
		}
		if renamed == 0 {
			return fmt.Errorf("404: no member of automation %s carries stage %q", cur.ID, from)
		}
		controlDecision(cur, "automation_rename_stage", operatorActor, fmt.Sprintf("renamed stage %q to %q on %d member(s)", from, to, renamed))
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.controlAudit("automation_rename_stage", map[string]any{
		"automation_id": automationID, "from": from, "to": to,
		"members": renamed, "version": updated.Version,
	})
	// Engine tags (auto_tags.go): the renamed stage's tag record is
	// migrated in place first (same tag ID and assignments, new name and
	// marker), then the Automation syncs so orphaned stage tags are
	// reclaimed.
	resolved := ResolveAutoTags(m.autoTagsDaemonCfg(), updated.AutoTags)
	if resolved.Enabled {
		if toName, err := RenderAutoTag(resolved, "stage", map[string]string{
			"stage":      to,
			"automation": updated.Name,
		}); err == nil {
			if err := m.migrateAutoStageTag(updated.ID, from, to, toName); err != nil {
				return nil, err
			}
		}
	}
	if err := m.syncAutoTags(updated); err != nil {
		return nil, err
	}
	return updated, nil
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
	// Engine tags (auto_tags.go): a membership write just landed, so the
	// Automation's system tags sync now. Members that left are stripped of
	// the Automation's tags (shared kind tags and operator tags stay); when
	// the Automation itself is gone, its footprints are cleared and its
	// un-referenced tag records reclaimed.
	if a, ok := m.automations.Get(automationID); ok {
		for prdID := range old {
			prd, ok := m.store.GetPRD(prdID)
			if !ok {
				continue
			}
			if err := m.stripAutomationAutoTags(automationID, prd); err != nil {
				return err
			}
		}
		return m.syncAutoTags(a)
	}
	if len(old) > 0 {
		return m.clearAutoTagsFor(&Automation{ID: automationID, Members: oldMembers})
	}
	return nil
}
