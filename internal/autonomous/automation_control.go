// Automation control operations (BL431 Phase 3b): the operator-driven pause,
// resume, cancel, skip, remove and edge-reorder of a running Automation. Each
// is a Manager method that takes the expected Version, refuses a stale or
// wrongly-stated Automation before it mutates anything, writes through the same
// compare-and-set the coordinator uses, and records one Decision and one audit
// record. In a forked Automation more than one member is active; skip names the
// branch (the member ID) it acts on (design doc "Branch naming").

package autonomous

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// Control operation audit actions and Decision kinds (one string serves both).
const (
	controlPause        = "automation_pause"
	controlResume       = "automation_resume"
	controlCancel       = "automation_cancel"
	controlSkipMember   = "automation_skip_member"
	controlRemoveMember = "automation_remove_member"
	controlReorderEdges = "automation_reorder_edges"
)

// cancelRequest carries the operator's cancel arguments to the Manager. The
// in-flight choice has no default: CancelRequested records that the operator
// supplied the choice, CancelInFlight is the choice itself, and a request
// without CancelRequested is refused.
type cancelRequest struct {
	CancelRequested bool `json:"cancel_requested"` // the operator made the in-flight choice
	CancelInFlight  bool `json:"cancel_in_flight"` // true ends in-flight member sessions; false leaves them running
}

// validate refuses a cancel whose in-flight choice is missing.
func (r cancelRequest) validate() error {
	if !r.CancelRequested {
		return errors.New("cancel needs an explicit in-flight choice and there is no default: the operator must pick cancel_in_flight=true (end the session of every in-flight member) or cancel_in_flight=false (leave in-flight members running)")
	}
	return nil
}

// memberCanceller is the optional part of a starter that ends a member PRD's
// session and marks it cancelled (API.Cancel). It is checked by type
// assertion, like nestedStarter, so a starter without it still satisfies
// AutomationStarter.
type memberCanceller interface {
	Cancel(id string) error
}

// controlAudit emits one audit record for a control operation, through the
// coordinator's sink when one is wired and else the Manager's. Nil-guarded.
func (m *Manager) controlAudit(action string, details map[string]any) {
	m.mu.Lock()
	c, fn := m.coordinator, m.auditFn
	m.mu.Unlock()
	switch {
	case c != nil:
		c.audit(action, details)
	case fn != nil:
		fn(action, details)
	}
}

// controlDecision appends a control Decision to a.
func controlDecision(a *Automation, kind, actor, note string) {
	a.Decisions = trimDecisions(append(a.Decisions, Decision{At: time.Now(), Kind: kind, Actor: actor, Note: note}))
}

// statusNames joins statuses for an error message.
func statusNames(allowed []AutomationStatus) string {
	names := make([]string, len(allowed))
	for i, s := range allowed {
		names[i] = string(s)
	}
	return strings.Join(names, " or ")
}

// loadForControl reads Automation id and refuses it unless expectedVersion is
// its stored Version and its status is one of allowed. The version is checked
// first so a stale caller learns of the conflict whatever the state.
func (m *Manager) loadForControl(id string, expectedVersion int64, allowed ...AutomationStatus) (*Automation, error) {
	a, ok := m.automations.Get(id)
	if !ok {
		return nil, fmt.Errorf("404: automation %s not found", id)
	}
	if a.Version != expectedVersion {
		return nil, fmt.Errorf("409: version mismatch: expected version %d, current version is %d", expectedVersion, a.Version)
	}
	if err := checkControlStatus(a, allowed); err != nil {
		return nil, err
	}
	return a, nil
}

// checkControlStatus returns a 409 unless a's status is one of allowed.
func checkControlStatus(a *Automation, allowed []AutomationStatus) error {
	for _, s := range allowed {
		if a.Status == s {
			return nil
		}
	}
	if a.Status == AutomationDraft {
		return fmt.Errorf("409: automation %s is a draft; activate it first", a.ID)
	}
	return fmt.Errorf("409: automation %s is %s; this operation needs an automation that is %s", a.ID, a.Status, statusNames(allowed))
}

// PauseAutomation moves an active Automation to paused. Members already in
// flight keep running and nothing executing is ended or signalled; a paused
// Automation moves its cursors on when a member ends but starts nothing.
func (m *Manager) PauseAutomation(id, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	if _, err := m.loadForControl(id, expectedVersion, AutomationActive); err != nil {
		return nil, err
	}
	updated, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if err := checkControlStatus(a, []AutomationStatus{AutomationActive}); err != nil {
			return err
		}
		a.Status = AutomationPaused
		controlDecision(a, controlPause, actor, "paused by "+actor+"; in-flight members keep running")
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.controlAudit(controlPause, map[string]any{
		"automation_id": id, "actor": actor, "version": updated.Version,
	})
	return updated, nil
}

// ResumeAutomation moves a paused Automation back to active and hands it to the
// coordinator, which carries on from ActiveCursors: a cursor that moved while
// paused and was never started is started, and a member that ended meanwhile is
// routed. Finished members are not restarted.
//
// An Automation stopped by a budget breach may be resumed too: a blocked one
// only by an operator, a paused one by the operator or by the daemon at the
// window rollover (actor "budget"). The gate is re-evaluated first, and a
// dollar ceiling that is still reached refuses the resume with a 409 error
// carrying the reason.
func (m *Manager) ResumeAutomation(id, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	pre, ok := m.automations.Get(id)
	if !ok {
		return nil, fmt.Errorf("404: automation %s not found", id)
	}
	if _, err := m.loadForControl(id, expectedVersion, resumableStatuses(pre)...); err != nil {
		return nil, err
	}
	if budgetEpisodeOpen(pre) {
		if v, refused := m.budgetRefusal(pre); refused {
			return nil, &budgetRefusedError{autoID: id, verdict: v}
		}
	}
	var episode *BudgetState
	updated, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if err := checkControlStatus(a, resumableStatuses(a)); err != nil {
			return err
		}
		episode = nil
		if budgetEpisodeOpen(a) {
			st := *a.BudgetState
			episode = &st
		}
		a.Status = AutomationActive
		a.BudgetState = nil
		if episode != nil {
			a.IdleSince = nil
		}
		controlDecision(a, controlResume, actor, "resumed by "+actor)
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.controlAudit(controlResume, map[string]any{
		"automation_id": id, "actor": actor, "version": updated.Version,
	})
	m.mu.Lock()
	c := m.coordinator
	m.mu.Unlock()
	if episode != nil {
		m.budgetResumed(c, updated, episode, actor)
	}
	if c != nil {
		c.reconcileOne(updated)
		if final, ok := m.automations.Get(id); ok {
			return final, nil
		}
	}
	return updated, nil
}

// CancelAutomation cancels an Automation. The in-flight choice is required and
// has no default: with hasChoice false it returns an error naming
// cancel_in_flight=true and cancel_in_flight=false. With cancelInFlight true
// every in-flight member's session is ended (API.Cancel) and the member marked
// cancelled; with false no running member is touched and in-flight members are
// left as they are (the cancelled Automation ignores their later ends). Members
// not yet run become cancelled either way.
//
// The Automation is written cancelled before any session is ended, so the end of
// a member's session is seen against a cancelled Automation and advances
// nothing. If ending a session fails the Automation stays cancelled and the
// updated record is returned together with an error naming the members.
func (m *Manager) CancelAutomation(id, actor string, cancelInFlight bool, hasChoice bool, expectedVersion int64) (*Automation, error) {
	return m.cancelAutomation(id, actor, cancelRequest{CancelRequested: hasChoice, CancelInFlight: cancelInFlight}, expectedVersion)
}

func (m *Manager) cancelAutomation(id, actor string, req cancelRequest, expectedVersion int64) (*Automation, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	if actor == "" {
		actor = "operator"
	}
	a, err := m.loadForControl(id, expectedVersion, AutomationActive, AutomationPaused, AutomationBlocked)
	if err != nil {
		return nil, err
	}
	canceller := m.memberCanceller()
	if req.CancelInFlight && canceller == nil {
		for _, mem := range inFlightMembers(a) {
			if mem.kind() == MemberKindPRD {
				return nil, errors.New("409: automation coordinator cannot end member sessions; cancel with cancel_in_flight=false or start the coordinator")
			}
		}
	}

	// Members to end after the write; collected inside the mutation so they
	// come from the version that wins the compare-and-set.
	var endPRDs, endChildren, inFlightIDs []string
	updated, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if err := checkControlStatus(a, []AutomationStatus{AutomationActive, AutomationPaused, AutomationBlocked}); err != nil {
			return err
		}
		endPRDs, endChildren, inFlightIDs = nil, nil, nil
		now := time.Now()
		for i := range a.Members {
			mem := &a.Members[i]
			switch mem.Status {
			case MemberRunning:
				inFlightIDs = append(inFlightIDs, mem.ID)
				if !req.CancelInFlight {
					continue
				}
				switch mem.kind() {
				case MemberKindPRD:
					endPRDs = append(endPRDs, mem.PRDID)
				case MemberKindAutomation:
					endChildren = append(endChildren, mem.AutomationID)
				}
				mem.Status = MemberCancelled
				mem.Outcome = "cancelled"
				mem.FinishedAt = &now
				mem.EdgeTaken = ""
				dropCursor(a, mem.ID)
			case MemberPending, "":
				mem.Status = MemberCancelled
				mem.Outcome = "automation-cancelled"
				mem.FinishedAt = &now
				mem.EdgeTaken = ""
			}
		}
		if req.CancelInFlight {
			if automationIsLinear(a) {
				a.CurrentMemberID = ""
			} else {
				a.ActiveCursors = nil
			}
		}
		a.Status = AutomationCancelled
		a.FinishedAt = &now
		a.IdleSince = nil
		note := fmt.Sprintf("cancelled by %s with cancel_in_flight=%t; %d member(s) were in flight", actor, req.CancelInFlight, len(inFlightIDs))
		controlDecision(a, controlCancel, actor, note)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// A cancelled Automation owns no member Automaton. Release them before any
	// session is ended so their end events find no owning Automation.
	var failures []string
	if err := m.syncAutomationMembership(id, updated.Members, nil, true); err != nil {
		failures = append(failures, "release membership: "+err.Error())
	}
	for _, prdID := range endPRDs {
		if m.prdFinished(prdID) {
			continue // already ended on its own; do not rewrite its outcome
		}
		if canceller == nil {
			failures = append(failures, fmt.Sprintf("member prd %s: no session canceller", prdID))
			continue
		}
		if err := canceller.Cancel(prdID); err != nil {
			failures = append(failures, fmt.Sprintf("member prd %s: %v", prdID, err))
		}
	}
	for _, childID := range endChildren {
		child, ok := m.automations.Get(childID)
		if !ok || child.Status == AutomationDraft || child.Status == AutomationCompleted || child.Status == AutomationCancelled {
			continue
		}
		if _, err := m.cancelAutomation(childID, actor, cancelRequest{CancelRequested: true, CancelInFlight: true}, child.Version); err != nil {
			failures = append(failures, fmt.Sprintf("nested automation %s: %v", childID, err))
		}
	}
	m.emit(LifecycleEvent{
		Kind:         AutomationStatusKind(AutomationCancelled),
		Level:        LevelAutomation,
		AutomationID: id,
		NewStatus:    string(AutomationCancelled),
	})
	details := map[string]any{
		"automation_id": id, "actor": actor,
		"cancel_in_flight": req.CancelInFlight, "in_flight": inFlightIDs,
		"version": updated.Version,
	}
	if len(failures) > 0 {
		details["errors"] = failures
	}
	m.controlAudit(controlCancel, details)
	if len(failures) > 0 {
		return updated, fmt.Errorf("automation %s is cancelled but ending in-flight members failed: %s", id, strings.Join(failures, "; "))
	}
	return updated, nil
}

// memberCanceller returns the coordinator's starter as a canceller, or nil.
func (m *Manager) memberCanceller() memberCanceller {
	m.mu.Lock()
	c := m.coordinator
	m.mu.Unlock()
	if c == nil {
		return nil
	}
	mc, _ := c.starter.(memberCanceller)
	return mc
}

// inFlightMembers returns the members of a that are running.
func inFlightMembers(a *Automation) []AutomationMember {
	var out []AutomationMember
	for _, mem := range a.Members {
		if mem.Status == MemberRunning {
			out = append(out, mem)
		}
	}
	return out
}

// prdFinished reports whether the PRD is no longer running: it reached a
// terminal status or went back to approved. A missing PRD counts as finished.
func (m *Manager) prdFinished(prdID string) bool {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return true
	}
	var status PRDStatus
	m.readLocked(func() { status = prd.Status })
	_, ended := prdEnd(status)
	return ended
}

// resolveBranch names the active cursor an operation acts on. A linear
// Automation has one cursor and needs no name. When several branches are
// active, memberID must be one of them; anything else is refused with the list
// of active branch member IDs.
func resolveBranch(a *Automation, memberID string) (string, error) {
	cursors := liveCursorIDs(a)
	if len(cursors) == 0 {
		return "", fmt.Errorf("409: automation %s has no active member to act on", a.ID)
	}
	if memberID == "" {
		if len(cursors) == 1 {
			return cursors[0], nil
		}
		return "", fmt.Errorf("automation %s has %d active branches; name the branch member to act on, one of [%s]", a.ID, len(cursors), strings.Join(cursors, " "))
	}
	for _, c := range cursors {
		if c == memberID {
			return memberID, nil
		}
	}
	return "", fmt.Errorf("member %s is not an active branch of automation %s; active branches are [%s]", memberID, a.ID, strings.Join(cursors, " "))
}

// SkipMember skips an active member and moves its cursor along the named edge,
// as if the member had ended that way: edge is on_complete (the default when
// empty), on_blocked or on_failed. The member is marked skipped with EdgeTaken
// set, and the next member is started (unless paused) through the same step the
// coordinator applies when a member ends. A forked Automation with more than one
// active branch needs memberID to name the branch; a linear Automation needs no
// name. A session the skipped member is running is not ended.
func (m *Manager) SkipMember(id, memberID, edge string, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	if edge == "" {
		edge = edgeOnComplete
	}
	switch edge {
	case edgeOnComplete, edgeOnBlocked, edgeOnFailed:
	default:
		return nil, fmt.Errorf("skip edge %q is not valid; use %s, %s or %s", edge, edgeOnComplete, edgeOnBlocked, edgeOnFailed)
	}
	a, err := m.loadForControl(id, expectedVersion, AutomationActive, AutomationPaused)
	if err != nil {
		return nil, err
	}
	target, err := resolveBranch(a, memberID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	c := m.coordinator
	m.mu.Unlock()
	if c == nil {
		return nil, errors.New("automation coordinator is not running")
	}

	in := stepInput{memberID: target, end: endOutcome{
		edge: edge, memStatus: MemberSkipped, outcome: "skipped", success: edge == edgeOnComplete,
	}}
	snap := c.snapshotMembers(a)
	dry, err := step(cloneAutomation(a), in, snap)
	if err != nil {
		return nil, fmt.Errorf("409: member %s is no longer an active branch of automation %s", target, id)
	}
	releases := c.beginStarts(dry.starts)
	var eff *stepEffects
	updated, err := m.automations.Update(id, expectedVersion, func(cur *Automation) error {
		if err := checkControlStatus(cur, []AutomationStatus{AutomationActive, AutomationPaused}); err != nil {
			return err
		}
		var serr error
		eff, serr = step(cur, in, snap)
		if errors.Is(serr, errStaleStep) {
			return fmt.Errorf("409: member %s is no longer an active branch of automation %s", target, id)
		}
		if serr != nil {
			return serr
		}
		if mem := automationMember(cur, target); mem != nil {
			mem.EdgeTaken = edge // a branch's step clears the edge; a skip records the one chosen
		}
		controlDecision(cur, controlSkipMember, actor, fmt.Sprintf("skipped member %s via %s", target, edge))
		return nil
	})
	if err != nil {
		releaseAll(releases)
		return nil, err
	}
	c.afterAdvance(updated, eff)
	releaseAll(releases)
	m.controlAudit(controlSkipMember, map[string]any{
		"automation_id": id, "actor": actor, "member": target, "edge": edge, "version": updated.Version,
	})
	return updated, nil
}

// RemoveMember removes a member that has not started and has no outgoing
// edges. The entry member, an in-flight or finished member, a fork or join and a
// fork or join branch are refused. Edges of other members that named the
// removed member are cleared.
func (m *Manager) RemoveMember(id, memberID, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	a, err := m.loadForControl(id, expectedVersion, AutomationActive, AutomationPaused)
	if err != nil {
		return nil, err
	}
	if err := checkRemovable(a, memberID); err != nil {
		return nil, err
	}
	oldMembers := append([]AutomationMember(nil), a.Members...)
	updated, err := m.automations.Update(id, expectedVersion, func(cur *Automation) error {
		if err := checkControlStatus(cur, []AutomationStatus{AutomationActive, AutomationPaused}); err != nil {
			return err
		}
		if err := checkRemovable(cur, memberID); err != nil {
			return err
		}
		kept := make([]AutomationMember, 0, len(cur.Members)-1)
		for _, mem := range cur.Members {
			if mem.ID == memberID {
				continue
			}
			if mem.OnComplete == memberID {
				mem.OnComplete = ""
			}
			if mem.OnBlocked == memberID {
				mem.OnBlocked = ""
			}
			if mem.OnFailed == memberID {
				mem.OnFailed = ""
			}
			kept = append(kept, mem)
		}
		cur.Members = kept
		controlDecision(cur, controlRemoveMember, actor, "removed member "+memberID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := m.syncAutomationMembership(id, oldMembers, updated.Members, false); err != nil {
		return nil, err
	}
	m.controlAudit(controlRemoveMember, map[string]any{
		"automation_id": id, "actor": actor, "member": memberID, "version": updated.Version,
	})
	return updated, nil
}

// checkRemovable reports why member memberID of a cannot be removed, or nil.
func checkRemovable(a *Automation, memberID string) error {
	mem := automationMember(a, memberID)
	if mem == nil {
		return fmt.Errorf("404: member %s not found in automation %s", memberID, a.ID)
	}
	if mem.OnComplete != "" || mem.OnBlocked != "" || mem.OnFailed != "" {
		return fmt.Errorf("409: member %s has outgoing edges (on_complete=%q on_blocked=%q on_failed=%q); only a member with no outgoing edges can be removed", memberID, mem.OnComplete, mem.OnBlocked, mem.OnFailed)
	}
	if memberID == a.EntryMemberID {
		return fmt.Errorf("409: member %s is the entry member of automation %s and cannot be removed", memberID, a.ID)
	}
	if isLiveCursor(a, memberID) || mem.Status == MemberRunning {
		return fmt.Errorf("409: member %s is in flight and cannot be removed", memberID)
	}
	if mem.Status != MemberPending && mem.Status != "" {
		return fmt.Errorf("409: member %s is %s; only a member that has not started can be removed", memberID, mem.Status)
	}
	if k := mem.kind(); k == MemberKindFork || k == MemberKindJoin {
		return fmt.Errorf("409: member %s is a %s and cannot be removed on its own", memberID, k)
	}
	for _, other := range a.Members {
		for _, b := range append(append([]string(nil), other.ForkMembers...), other.JoinMembers...) {
			if b == memberID {
				return fmt.Errorf("409: member %s is a branch of %s %s and cannot be removed", memberID, other.kind(), other.ID)
			}
		}
	}
	return nil
}

// ReorderEdges rewires the three outgoing edges of a member that has not
// started: each argument is the member ID the edge now names, or empty for no
// edge. A member that is in flight or finished is refused, as is a new edge
// target that has already started. The store re-validates the graph (unknown
// targets, self-edges, cycles, unreachable members).
func (m *Manager) ReorderEdges(id, memberID string, onCompleted, onBlocked, onFailed string, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	a, err := m.loadForControl(id, expectedVersion, AutomationActive, AutomationPaused)
	if err != nil {
		return nil, err
	}
	if err := checkReorderable(a, memberID, onCompleted, onBlocked, onFailed); err != nil {
		return nil, err
	}
	var before [3]string
	updated, err := m.automations.Update(id, expectedVersion, func(cur *Automation) error {
		if err := checkControlStatus(cur, []AutomationStatus{AutomationActive, AutomationPaused}); err != nil {
			return err
		}
		if err := checkReorderable(cur, memberID, onCompleted, onBlocked, onFailed); err != nil {
			return err
		}
		mem := automationMember(cur, memberID)
		before = [3]string{mem.OnComplete, mem.OnBlocked, mem.OnFailed}
		mem.OnComplete, mem.OnBlocked, mem.OnFailed = onCompleted, onBlocked, onFailed
		controlDecision(cur, controlReorderEdges, actor, fmt.Sprintf("rewired member %s: on_complete=%q on_blocked=%q on_failed=%q", memberID, onCompleted, onBlocked, onFailed))
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.controlAudit(controlReorderEdges, map[string]any{
		"automation_id": id, "actor": actor, "member": memberID, "version": updated.Version,
		"edges":    map[string]string{edgeOnComplete: onCompleted, edgeOnBlocked: onBlocked, edgeOnFailed: onFailed},
		"previous": map[string]string{edgeOnComplete: before[0], edgeOnBlocked: before[1], edgeOnFailed: before[2]},
	})
	return updated, nil
}

// checkReorderable reports why member memberID of a cannot be rewired to the
// given edges, or nil.
func checkReorderable(a *Automation, memberID, onCompleted, onBlocked, onFailed string) error {
	mem := automationMember(a, memberID)
	if mem == nil {
		return fmt.Errorf("404: member %s not found in automation %s", memberID, a.ID)
	}
	if isLiveCursor(a, memberID) || mem.Status == MemberRunning {
		return fmt.Errorf("409: member %s is in flight; the edges of a member that has started cannot be rewired", memberID)
	}
	if mem.Status != MemberPending && mem.Status != "" {
		return fmt.Errorf("409: member %s is %s; only a member that has not started can be rewired", memberID, mem.Status)
	}
	for _, e := range []struct{ name, old, target string }{
		{edgeOnComplete, mem.OnComplete, onCompleted},
		{edgeOnBlocked, mem.OnBlocked, onBlocked},
		{edgeOnFailed, mem.OnFailed, onFailed},
	} {
		if e.target == "" || e.target == e.old {
			continue
		}
		next := automationMember(a, e.target)
		if next == nil {
			return fmt.Errorf("%s edge names unknown member %s", e.name, e.target)
		}
		if next.Status != MemberPending && next.Status != "" {
			return fmt.Errorf("409: %s edge names member %s, which is already %s", e.name, e.target, next.Status)
		}
	}
	return nil
}

// resumableStatuses are the statuses ResumeAutomation accepts for a: paused,
// and blocked when a budget breach blocked it.
func resumableStatuses(a *Automation) []AutomationStatus {
	if budgetEpisodeOpen(a) && a.Status == AutomationBlocked {
		return []AutomationStatus{AutomationPaused, AutomationBlocked}
	}
	return []AutomationStatus{AutomationPaused}
}

// budgetResumed audits the end of a budget episode and, when the daemon
// resumed it by itself, raises an info alert.
func (m *Manager) budgetResumed(c *Coordinator, a *Automation, ep *BudgetState, actor string) {
	reason := fmt.Sprintf("budget %s lifted by %s", ep.Action, actor)
	m.controlAudit(audit.ActionAutomationBudgetResumed, budgetAuditDetails(a, ep, ep.Action, actor, reason))
	if actor != budgetActor {
		return
	}
	title := "Automation resumed: budget available"
	body := fmt.Sprintf("Automation %q (%s) resumed by itself: the budget window reset and the %s ceiling no longer stops it.", a.Name, a.ID, ep.Window)
	switch {
	case c != nil:
		c.alert("info", title, body)
	default:
		if fn := m.AlertSink(); fn != nil {
			fn("info", title, body)
		}
	}
}
