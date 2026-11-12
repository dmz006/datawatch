// Decision assistant: the optional local-model setting that lets an Automation
// (or one of its members) hand a bounded judgment call to a configured model
// instead of a hardcoded default or an operator stop. This file holds the
// data model, defaults, validation, the member-over-Automation resolver and the
// operator setter. The assistant only picks among already-allowed options; the
// hooks that consult it live with the coordinator.

package autonomous

import (
	"fmt"
	"sort"
	"strings"
)

// Hook points the assistant may be enabled for.
const (
	DecisionHookEmptyEdge    = "empty_edge"
	DecisionHookSkipMember   = "skip_member"
	DecisionHookInsertMember = "insert_member"
	DecisionHookBudgetBreach = "budget_breach"
)

// Assistant modes. Shadow records what the assistant would have done without
// acting on it; live acts on a decision at or above the hook's threshold.
const (
	DecisionModeShadow = "shadow"
	DecisionModeLive   = "live"
)

// Conservative default confidence thresholds per hook point. Below the
// threshold the assistant must escalate to the operator instead of deciding.
// All are overridable through DecisionAssistant.Thresholds.
const (
	DefaultDecisionThreshold             = 0.9
	DefaultEmptyEdgeThreshold            = DefaultDecisionThreshold
	DefaultSkipMemberThreshold           = DefaultDecisionThreshold
	DefaultInsertMemberThreshold         = DefaultDecisionThreshold
	DefaultBudgetBreachDecisionThreshold = 0.95
)

// controlSetDecisionAssistant is the Decision kind and audit action of
// SetDecisionAssistant.
const controlSetDecisionAssistant = "automation_set_decision_assistant"

// DecisionAssistant configures the local-model decision assistant on an
// Automation or a member. A nil pointer, or an empty Backend, means off.
type DecisionAssistant struct {
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	// Mode is "shadow" (the default when the assistant is set) or "live".
	Mode string `json:"mode,omitempty"`
	// Hooks lists the enabled hook points; none enabled means no hook consults
	// the assistant.
	Hooks []string `json:"hooks,omitempty"`
	// Thresholds overrides the per-hook confidence threshold, keyed by hook.
	Thresholds map[string]float64 `json:"thresholds,omitempty"`
}

// decisionHookNames lists the valid hook points in a stable order.
var decisionHookNames = []string{
	DecisionHookEmptyEdge,
	DecisionHookSkipMember,
	DecisionHookInsertMember,
	DecisionHookBudgetBreach,
}

// DefaultDecisionThresholds returns a fresh copy of the per-hook defaults.
func DefaultDecisionThresholds() map[string]float64 {
	return map[string]float64{
		DecisionHookEmptyEdge:    DefaultEmptyEdgeThreshold,
		DecisionHookSkipMember:   DefaultSkipMemberThreshold,
		DecisionHookInsertMember: DefaultInsertMemberThreshold,
		DecisionHookBudgetBreach: DefaultBudgetBreachDecisionThreshold,
	}
}

func validDecisionHook(h string) bool {
	for _, n := range decisionHookNames {
		if n == h {
			return true
		}
	}
	return false
}

// enabled reports whether the assistant is set at all.
func (d *DecisionAssistant) enabled() bool {
	return d != nil && d.Backend != ""
}

// EffectiveMode returns the mode, defaulting to shadow.
func (d *DecisionAssistant) EffectiveMode() string {
	if d == nil || d.Mode == "" {
		return DecisionModeShadow
	}
	return d.Mode
}

// HookEnabled reports whether the assistant is set and hook is enabled.
func (d *DecisionAssistant) HookEnabled(hook string) bool {
	if !d.enabled() {
		return false
	}
	for _, h := range d.Hooks {
		if h == hook {
			return true
		}
	}
	return false
}

// Threshold returns the confidence threshold for hook: the configured override,
// else the conservative default.
func (d *DecisionAssistant) Threshold(hook string) float64 {
	if d != nil {
		if t, ok := d.Thresholds[hook]; ok {
			return t
		}
	}
	return DefaultDecisionThresholds()[hook]
}

// validateDecisionAssistant checks a decision assistant setting. nil is valid
// (off). A set assistant needs a backend/model pair the LLM registry accepts,
// known hook names, thresholds in (0,1] and a mode of shadow or live.
func validateDecisionAssistant(d *DecisionAssistant) error {
	if d == nil {
		return nil
	}
	if strings.TrimSpace(d.Backend) == "" {
		return fmt.Errorf("decision assistant: backend is required")
	}
	if strings.TrimSpace(d.Model) == "" {
		return fmt.Errorf("decision assistant: model is required")
	}
	if err := validateModelForBackend(d.Backend, d.Model); err != nil {
		return fmt.Errorf("decision assistant: %w", err)
	}
	switch d.Mode {
	case "", DecisionModeShadow, DecisionModeLive:
	default:
		return fmt.Errorf("decision assistant: unknown mode %q (want shadow or live)", d.Mode)
	}
	for _, h := range d.Hooks {
		if !validDecisionHook(h) {
			return fmt.Errorf("decision assistant: unknown hook %q (want one of %s)", h, strings.Join(decisionHookNames, ", "))
		}
	}
	keys := make([]string, 0, len(d.Thresholds))
	for h := range d.Thresholds {
		keys = append(keys, h)
	}
	sort.Strings(keys)
	for _, h := range keys {
		if !validDecisionHook(h) {
			return fmt.Errorf("decision assistant: threshold for unknown hook %q", h)
		}
		if t := d.Thresholds[h]; !(t > 0 && t <= 1) {
			return fmt.Errorf("decision assistant: threshold for %s is %v, want a value in (0,1]", h, t)
		}
	}
	return nil
}

// EffectiveDecisionAssistant resolves the setting that applies to member: the
// member's own setting wins over the Automation's. It returns nil when neither
// is set (the assistant is off). member may be nil to resolve at the
// Automation level.
func EffectiveDecisionAssistant(a *Automation, member *AutomationMember) *DecisionAssistant {
	if member != nil && member.DecisionAssistant.enabled() {
		return member.DecisionAssistant
	}
	if a != nil && a.DecisionAssistant.enabled() {
		return a.DecisionAssistant
	}
	return nil
}

// cloneDecisionAssistant deep-copies d so a stored setting is never shared.
func cloneDecisionAssistant(d *DecisionAssistant) *DecisionAssistant {
	if d == nil {
		return nil
	}
	out := *d
	out.Hooks = append([]string(nil), d.Hooks...)
	if d.Thresholds != nil {
		out.Thresholds = make(map[string]float64, len(d.Thresholds))
		for k, v := range d.Thresholds {
			out.Thresholds[k] = v
		}
	}
	return &out
}

// SetDecisionAssistant sets or clears the decision assistant of an Automation
// (memberID "") or of one member. A nil da clears the setting. Allowed while
// the Automation is draft, active, paused or blocked; the setting is validated
// on the stored copy inside the store's Update. It records one Decision and one
// audit record. expectedVersion 0 skips the version check.
func (m *Manager) SetDecisionAssistant(id, memberID, actor string, da *DecisionAssistant, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	note := "cleared"
	if da != nil {
		note = fmt.Sprintf("backend=%s model=%s mode=%s hooks=%s", da.Backend, da.Model, da.EffectiveMode(), strings.Join(da.Hooks, ","))
	}
	if memberID != "" {
		note = "member=" + memberID + " " + note
	}
	updated, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if err := checkControlStatus(a, []AutomationStatus{AutomationDraft, AutomationActive, AutomationPaused, AutomationBlocked}); err != nil {
			return err
		}
		if err := validateDecisionAssistant(da); err != nil {
			return fmt.Errorf("automation %s: %w", id, err)
		}
		if memberID == "" {
			a.DecisionAssistant = cloneDecisionAssistant(da)
		} else {
			found := false
			for i := range a.Members {
				if a.Members[i].ID == memberID {
					a.Members[i].DecisionAssistant = cloneDecisionAssistant(da)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("404: automation %s has no member %s", id, memberID)
			}
		}
		controlDecision(a, controlSetDecisionAssistant, actor, note)
		return nil
	})
	if err != nil {
		return nil, err
	}
	details := map[string]any{"automation_id": id, "actor": actor, "version": updated.Version, "cleared": da == nil}
	if memberID != "" {
		details["member_id"] = memberID
	}
	if da != nil {
		details["backend"], details["model"], details["mode"] = da.Backend, da.Model, da.EffectiveMode()
		details["hooks"] = append([]string(nil), da.Hooks...)
	}
	m.controlAudit(controlSetDecisionAssistant, details)
	return updated, nil
}
