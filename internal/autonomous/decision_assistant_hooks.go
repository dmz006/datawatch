// Decision assistant hooks: the helpers the four hook points share. The hook
// bodies live with the code they extend (empty-edge resolution in the
// coordinator, skip pre-screening in the control surface, insert triage in the
// insert path, breach response in the budget gate). Each hook consults the
// engine only when the effective setting enables it; any escalate result leaves
// the existing behaviour untouched, and shadow mode records the would-be
// decision and then behaves as before.

package autonomous

import (
	"fmt"
	"strings"
)

// Option names the hooks offer the assistant.
const (
	assistOptionPause         = "pause_automation"
	assistOptionNeedsOperator = "needs_operator"
	assistOptionKeepWaiting   = "keep_waiting"
	assistOptionSkipPrefix    = "skip_member:"
	assistOptionInsertPrefix  = "insert:"
	assistBreachPause         = "pause"
	assistBreachBlock         = "block"
)

// assistantEffective resolves the assistant that applies to member memberID of
// Automation autoID ("" = the Automation).
func (m *Manager) assistantEffective(autoID, memberID string) *DecisionAssistant {
	a, ok := m.automations.Get(autoID)
	if !ok {
		return nil
	}
	var member *AutomationMember
	if memberID != "" {
		member = automationMember(a, memberID)
	}
	return EffectiveDecisionAssistant(a, member)
}

// assistantHookOn reports whether hook is enabled for the member.
func (m *Manager) assistantHookOn(autoID, memberID, hook string) bool {
	return m.assistantEffective(autoID, memberID).HookEnabled(hook)
}

// currentVersion is the Automation's stored Version now. The control methods
// check an exact version, and recording the assistant's Decision bumps it, so a
// hook reads it again after the model answered. A change in the instant between
// this read and the write is refused as a version conflict and acts as an
// escalate.
func (m *Manager) currentVersion(autoID string) int64 {
	if a, ok := m.automations.Get(autoID); ok {
		return a.Version
	}
	return 0
}

// assistantActorFor is the actor "assistant:<model>" for the member's assistant.
func (m *Manager) assistantActorFor(autoID, memberID string) string {
	if da := m.assistantEffective(autoID, memberID); da != nil {
		return "assistant:" + da.Model
	}
	return "assistant:unknown"
}

// skipEdgeOptions lists the skip_member:<edge> options for mem: on_complete
// always (an empty target ends the path as a completion), and the blocked and
// failed edges only when they name a member, except the edge exclude.
func skipEdgeOptions(mem *AutomationMember, exclude string) []string {
	opts := []string{assistOptionSkipPrefix + edgeOnComplete}
	for _, e := range []string{edgeOnBlocked, edgeOnFailed} {
		if e != exclude && edgeTarget(mem, e) != "" {
			opts = append(opts, assistOptionSkipPrefix+e)
		}
	}
	return opts
}

// skipOptionEdge returns the edge of a skip_member:<edge> option.
func skipOptionEdge(option string) (string, bool) {
	if !strings.HasPrefix(option, assistOptionSkipPrefix) {
		return "", false
	}
	switch e := strings.TrimPrefix(option, assistOptionSkipPrefix); e {
	case edgeOnComplete, edgeOnBlocked, edgeOnFailed:
		return e, true
	}
	return "", false
}

// automationProgress is the short progress line of the evidence packet.
func automationProgress(a *Automation) string {
	done, started := 0, 0
	for _, mem := range a.Members {
		switch mem.Status {
		case MemberCompleted, MemberSkipped:
			done++
		case MemberRunning, MemberBlocked, MemberFailed:
			started++
		}
	}
	return fmt.Sprintf("%d of %d members finished, %d started and not finished", done, len(a.Members), started)
}
