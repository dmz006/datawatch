// Decision assistant engine: builds the bounded evidence packet, calls the
// configured model, parses and guards its structured answer, and records every
// outcome as a normal Decision plus an audit record. The assistant only picks
// among the options the hook passes in; it can never widen them.

package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultDecisionModelTimeout bounds one assistant model call.
const DefaultDecisionModelTimeout = 60 * time.Second

// Byte caps for each free-text field of the evidence packet.
const (
	evidenceFailureCap  = 1024
	evidenceVerdictCap  = 1024
	evidenceProgressCap = 512
	evidenceSpendCap    = 512
	evidenceOptionCap   = 128
	evidenceMaxOptions  = 16
	answerReasonCap     = 500
)

// Outcomes of decide.
const (
	AssistantDecided     = "decided"
	AssistantEscalate    = "escalate"
	AssistantWouldDecide = "would_decide"
)

// controlAssistantDecisionPrefix prefixes the Decision kind and audit action of
// one assistant outcome; the hook point name follows it.
const controlAssistantDecisionPrefix = "assistant_"

// DecisionEvidence is the bounded packet handed to the model. Every text field
// is truncated to a fixed byte cap by bounded().
type DecisionEvidence struct {
	MemberID      string `json:"member_id,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
	Verdict       string `json:"verdict,omitempty"`
	Progress      string `json:"progress,omitempty"`
	Spend         string `json:"spend,omitempty"`
	// HumanSignOff lists options a guardrail profile or quality gate marked as
	// needing human sign-off; the guard rejects them whatever the model says.
	HumanSignOff []string `json:"human_sign_off,omitempty"`
}

// AssistantAnswer is the structured answer the model must return.
type AssistantAnswer struct {
	Option     string  `json:"option"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// AssistantResult is the outcome of decide. Only Outcome == AssistantDecided
// may be acted on; would_decide is a shadow-mode record.
type AssistantResult struct {
	Outcome    string
	Hook       string
	Option     string
	Confidence float64
	Reason     string
	Shadow     bool
}

// Actionable reports whether the result may be acted on.
func (r AssistantResult) Actionable() bool { return r.Outcome == AssistantDecided }

// truncateBytes cuts s to at most n bytes on a rune boundary.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// bounded returns a copy of e with every text field truncated to its cap.
func (e DecisionEvidence) bounded() DecisionEvidence {
	e.MemberID = truncateBytes(e.MemberID, evidenceOptionCap)
	e.FailureReason = truncateBytes(e.FailureReason, evidenceFailureCap)
	e.Verdict = truncateBytes(e.Verdict, evidenceVerdictCap)
	e.Progress = truncateBytes(e.Progress, evidenceProgressCap)
	e.Spend = truncateBytes(e.Spend, evidenceSpendCap)
	signOff := make([]string, 0, len(e.HumanSignOff))
	for i, s := range e.HumanSignOff {
		if i >= evidenceMaxOptions {
			break
		}
		signOff = append(signOff, truncateBytes(s, evidenceOptionCap))
	}
	e.HumanSignOff = signOff
	return e
}

// boundedOptions caps the option list and each option's length.
func boundedOptions(allowed []string) []string {
	out := make([]string, 0, len(allowed))
	for i, o := range allowed {
		if i >= evidenceMaxOptions {
			break
		}
		out = append(out, truncateBytes(o, evidenceOptionCap))
	}
	return out
}

// buildAssistantPrompt renders the packet and options for the model.
func buildAssistantPrompt(hook string, e DecisionEvidence, allowed []string) string {
	e = e.bounded()
	var b strings.Builder
	fmt.Fprintf(&b, "You triage one automation decision (%s). Pick exactly one option from the list.\n", hook)
	b.WriteString("Reply with only JSON: {\"option\": \"<one option>\", \"confidence\": <0..1>, \"reason\": \"<one sentence>\"}.\n")
	fmt.Fprintf(&b, "Member: %s\nFailure reason: %s\nVerdict: %s\nProgress: %s\nSpend: %s\nOptions:\n", e.MemberID, e.FailureReason, e.Verdict, e.Progress, e.Spend)
	for _, o := range boundedOptions(allowed) {
		fmt.Fprintf(&b, "- %s\n", o)
	}
	return b.String()
}

// evidenceSummary is the short evidence line stored in the Decision note.
func evidenceSummary(e DecisionEvidence) string {
	e = e.bounded()
	return fmt.Sprintf("member=%s failure=%q verdict=%q progress=%q spend=%q",
		e.MemberID, truncateBytes(e.FailureReason, 120), truncateBytes(e.Verdict, 120),
		truncateBytes(e.Progress, 80), truncateBytes(e.Spend, 80))
}

// parseAssistantAnswer parses the model's reply. It rejects non-JSON, missing
// fields, a confidence outside [0,1] and any option not in allowed.
func parseAssistantAnswer(raw string, allowed []string) (AssistantAnswer, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	var wire struct {
		Option     *string  `json:"option"`
		Confidence *float64 `json:"confidence"`
		Reason     *string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s), &wire); err != nil {
		return AssistantAnswer{}, fmt.Errorf("answer is not valid JSON: %w", err)
	}
	if wire.Option == nil || wire.Confidence == nil || wire.Reason == nil {
		return AssistantAnswer{}, errors.New("answer is missing option, confidence or reason")
	}
	c := *wire.Confidence
	if !(c >= 0 && c <= 1) {
		return AssistantAnswer{}, fmt.Errorf("confidence %v is outside [0,1]", c)
	}
	reason := strings.TrimSpace(*wire.Reason)
	if reason == "" || strings.TrimSpace(*wire.Option) == "" {
		return AssistantAnswer{}, errors.New("answer has an empty option or reason")
	}
	opt := *wire.Option
	found := false
	for _, a := range allowed {
		if a == opt {
			found = true
			break
		}
	}
	if !found {
		return AssistantAnswer{}, fmt.Errorf("option %q is not in the allowed set", opt)
	}
	return AssistantAnswer{Option: opt, Confidence: c, Reason: truncateBytes(reason, answerReasonCap)}, nil
}

// normalizeOption lowercases an option and collapses separators to underscores
// so "Cancel In-Flight" and "cancel_in_flight" compare alike.
func normalizeOption(o string) string {
	var b strings.Builder
	lastSep := true
	for _, r := range strings.ToLower(o) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSep = false
		} else if !lastSep {
			b.WriteByte('_')
			lastSep = true
		}
	}
	return strings.TrimSuffix(b.String(), "_")
}

// guardAssistantOption rejects an option the assistant may never pick: it
// cancels in-flight work, removes a member, raises a BudgetPolicy ceiling, or
// is marked as needing human sign-off. It applies even when the option was
// listed as allowed by mistake.
func guardAssistantOption(option string, humanSignOff []string) error {
	n := normalizeOption(option)
	if strings.Contains(n, "cancel_in_flight") {
		return fmt.Errorf("option %q cancels in-flight work, which needs the operator", option)
	}
	if strings.Contains(n, "remove_member") {
		return fmt.Errorf("option %q removes a member, which needs the operator", option)
	}
	touchesBudget := strings.Contains(n, "ceiling") || strings.Contains(n, "budget") || strings.Contains(n, "limit")
	for _, w := range []string{"raise", "increase", "bump", "lift", "extend", "expand", "higher", "more", "uncap", "remove", "disable", "set"} {
		if touchesBudget && containsWord(n, w) {
			return fmt.Errorf("option %q changes a budget ceiling, which needs the operator", option)
		}
	}
	for _, s := range humanSignOff {
		if ns := normalizeOption(s); ns != "" && ns == n {
			return fmt.Errorf("option %q is marked as needing human sign-off", option)
		}
	}
	return nil
}

func containsWord(normalized, w string) bool {
	for _, p := range strings.Split(normalized, "_") {
		if p == w {
			return true
		}
	}
	return false
}

// SetDecisionAssistantModelFn wires the model call the assistant uses; the
// production wiring passes the LLM registry backend/model pair it was
// configured with. Nil = unreachable, so every decision escalates.
func (m *Manager) SetDecisionAssistantModelFn(fn func(ctx context.Context, backend, model, prompt string) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisionModelFn = fn
}

// SetDecisionAssistantTimeout overrides the per-call model timeout; zero
// restores DefaultDecisionModelTimeout.
func (m *Manager) SetDecisionAssistantTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisionTimeout = d
}

// callAssistantModel runs the model call under the per-call timeout. The call
// runs in its own goroutine so a model that ignores the context cannot hold
// the caller past the timeout.
func (m *Manager) callAssistantModel(ctx context.Context, backend, model, prompt string) (string, error) {
	m.mu.Lock()
	fn, timeout := m.decisionModelFn, m.decisionTimeout
	m.mu.Unlock()
	if fn == nil {
		return "", errors.New("no decision assistant model is wired")
	}
	if timeout <= 0 {
		timeout = DefaultDecisionModelTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type reply struct {
		out string
		err error
	}
	ch := make(chan reply, 1)
	go func() {
		out, err := fn(cctx, backend, model, prompt)
		ch <- reply{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-cctx.Done():
		return "", fmt.Errorf("model call timed out: %w", cctx.Err())
	}
}

// decide asks the assistant configured for member memberID ("" = the
// Automation) of Automation automationID to pick one of allowed at hook. It
// returns decided, escalate (assistant or hook off, low confidence, an
// unparseable answer, timeout, unreachable model or guard rejection) or, in
// shadow mode, would_decide, which must not be acted on. When the assistant is
// off nothing is recorded; every other outcome is a Decision plus an audit
// record.
func (m *Manager) decide(ctx context.Context, automationID, memberID, hook string, packet DecisionEvidence, allowed []string) AssistantResult {
	a, ok := m.automations.Get(automationID)
	if !ok {
		return AssistantResult{Outcome: AssistantEscalate, Hook: hook, Reason: "automation not found"}
	}
	var member *AutomationMember
	for i := range a.Members {
		if a.Members[i].ID == memberID {
			member = &a.Members[i]
		}
	}
	da := EffectiveDecisionAssistant(a, member)
	if !da.HookEnabled(hook) {
		return AssistantResult{Outcome: AssistantEscalate, Hook: hook, Reason: "decision assistant is off or the hook is disabled"}
	}
	shadow := da.EffectiveMode() == DecisionModeShadow
	res := AssistantResult{Outcome: AssistantEscalate, Hook: hook, Shadow: shadow}
	allowed = boundedOptions(allowed)

	raw, err := m.callAssistantModel(ctx, da.Backend, da.Model, buildAssistantPrompt(hook, packet, allowed))
	switch {
	case err != nil:
		res.Reason = "model unavailable: " + err.Error()
	default:
		ans, perr := parseAssistantAnswer(raw, allowed)
		if perr != nil {
			res.Reason = "unusable answer: " + perr.Error()
			break
		}
		res.Option, res.Confidence = ans.Option, ans.Confidence
		if gerr := guardAssistantOption(ans.Option, packet.HumanSignOff); gerr != nil {
			res.Reason = "guard rejected: " + gerr.Error()
			break
		}
		if th := da.Threshold(hook); ans.Confidence < th {
			res.Reason = fmt.Sprintf("confidence %.2f is below the %.2f threshold: %s", ans.Confidence, th, ans.Reason)
			break
		}
		res.Reason = ans.Reason
		res.Outcome = AssistantDecided
		if shadow {
			res.Outcome = AssistantWouldDecide
		}
	}
	m.recordAssistantOutcome(automationID, memberID, da, packet, res)
	return res
}

// recordAssistantOutcome stores res as a Decision (Actor assistant:<model>,
// Kind per hook) on the Automation and writes the audit record.
func (m *Manager) recordAssistantOutcome(automationID, memberID string, da *DecisionAssistant, packet DecisionEvidence, res AssistantResult) {
	kind := controlAssistantDecisionPrefix + res.Hook
	actor := "assistant:" + da.Model
	note := fmt.Sprintf("%s outcome=%s option=%q confidence=%.2f evidence: %s reason: %s",
		map[bool]string{true: "[SHADOW — not acted on]", false: "[live]"}[res.Shadow],
		res.Outcome, res.Option, res.Confidence, evidenceSummary(packet), truncateBytes(res.Reason, answerReasonCap))
	if memberID != "" {
		note = "member=" + memberID + " " + note
	}
	updated, err := m.automations.Update(automationID, 0, func(a *Automation) error {
		a.Decisions = trimDecisions(append(a.Decisions, Decision{
			At: time.Now(), Kind: kind, Backend: da.Backend, Model: da.Model, Actor: actor, Note: note,
		}))
		return nil
	})
	details := map[string]any{
		"automation_id": automationID, "actor": actor, "hook": res.Hook, "outcome": res.Outcome,
		"option": res.Option, "confidence": res.Confidence, "shadow": res.Shadow, "reason": truncateBytes(res.Reason, answerReasonCap),
	}
	if memberID != "" {
		details["member_id"] = memberID
	}
	if err == nil {
		details["version"] = updated.Version
	}
	m.controlAudit(kind, details)
}
