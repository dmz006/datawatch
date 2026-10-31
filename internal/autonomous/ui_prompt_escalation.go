// UI prompt escalation, daemon side: the handler for the session package's
// escalation hook (internal/session/ui_prompt_escalate.go). When the
// safe-dismiss rules could not clear a recognised interstitial prompt —
// the attempts cap used up with the prompt still on screen, or the
// dismissal off and the prompt past the delay plus one clear bound — the
// session is stalled and the operator gets exactly one question on the
// question channel: who and what is blocked, what the prompt is, what the
// daemon already tried, and a short screen excerpt (ANSI-stripped, marker
// lines neutralised, capped under the question text cap). The question is
// informational: priority high, risk low, the default is hold — it tells
// the operator, it does not change anything on the daemon side. It is
// raised at most once per prompt occurrence (the store dedupes on the
// occurrence token), never re-raised while the original is still open,
// withdrawn when the prompt clears or the session ends, and — when the
// channel is off or the raise failed — replaced by exactly one
// high-priority alert.
package autonomous

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/session"
)

// UIPromptEscalationDeps is what the escalation handler needs. Store,
// ChannelOn and FallbackAlert are required. Audit, Notify and Logf may be
// nil.
type UIPromptEscalationDeps struct {
	// Store is the question store a REST raise uses (RaiseUnlessOpenSourceLine, Withdraw).
	Store *QuestionStore
	// ChannelOn reports whether the operator question channel is on right
	// now (config autonomous.questions.enabled).
	ChannelOn func() bool
	// Audit receives the escalation audit entries.
	Audit func(action string, details map[string]any)
	// Notify is the raise fan-out a REST raise runs after its response
	// (WebSocket event, messaging group, question alert). It is called once
	// per question actually raised, never for a duplicate.
	Notify func(Question)
	// FallbackAlert replaces the question when the channel is off or the
	// raise failed: exactly one high-priority alert (the caller picks the
	// level and wires the session id; it is called once per failed event).
	FallbackAlert func(sessionID, title, body string)
	// Logf receives the one-line diagnostics.
	Logf func(format string, args ...any)
}

func (d UIPromptEscalationDeps) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
	}
}

// Question attributes of an escalation question. The label "information"
// and risk low are the shipped vocabulary; the priority is high so it rides
// the high-priority raise path even though it asks nothing.
const (
	uiPromptEscalationLabel    = "information"
	uiPromptEscalationAsker    = "datawatch"
	uiPromptEscalationDefault  = "hold"
	uiPromptEscalationTag      = "ui prompt escalation"
	uiPromptEscalationPriority = "high"
	uiPromptEscalationRisk     = "low"
)

// ErrUIPromptEscalationChannelOff is returned by RaiseUIPromptEscalation when
// the question channel is off; the one high-priority alert fallback has
// already been sent.
var ErrUIPromptEscalationChannelOff = errors.New("ui prompt escalation: question channel off")

// ErrUIPromptEscalationNoStore is returned when this daemon has no question
// store wired; the alert fallback path applies identically.
var ErrUIPromptEscalationNoStore = errors.New("ui prompt escalation: no question store")

// RaiseUIPromptEscalation raises the escalation question for one event and
// returns it alongside raised=true and a nil error, so the caller — the
// session package hook — can record the question id on the session's
// UIPrompt record. It is idempotent per occurrence: the store refuses a
// second raise for an open question with the same session id and
// occurrence token; a still-open question is then returned with
// raised=false and a nil error, logged once, never alerting or
// re-notifying again. When the channel is off, the store is missing, or
// the store refuses the write, the one fallback alert is sent and a
// non-nil error is returned so the caller logs the failure; in those
// cases no question is stored and nothing is notified.
func RaiseUIPromptEscalation(d UIPromptEscalationDeps, ev session.UIPromptEscalateEvent) (Question, bool, error) {
	if d.ChannelOn != nil && !d.ChannelOn() {
		d.fallback(ev, "question channel off")
		return Question{}, false, ErrUIPromptEscalationChannelOff
	}
	q := d.buildQuestion(ev)
	if d.Store == nil {
		d.fallback(ev, "no question store")
		return Question{}, false, ErrUIPromptEscalationNoStore
	}
	raised, isNew, err := d.Store.RaiseUnlessOpenSourceLine(q)
	if err != nil {
		if errors.Is(err, ErrInvalidQuestion) {
			d.audit(ev, audit.ActionUIPromptEscalationRefused)
		} else {
			d.logf("[autonomous] ui prompt escalation for %s not stored: %v", ev.SessionFullID, err)
		}
		d.fallback(ev, "raise failed: "+err.Error())
		return Question{}, false, err
	}
	if !isNew {
		d.logf("[autonomous] ui prompt escalation for %s: duplicate of open question %s, not re-raised",
			ev.SessionFullID, raised.ID)
		return raised, false, nil
	}
	d.audit(ev, audit.ActionUIPromptEscalationRaised)
	d.logf("[autonomous] ui prompt escalation question %s raised for %s (kind %s, trigger %s)",
		raised.ID, ev.SessionFullID, ev.PromptKind, ev.Trigger)
	if d.Notify != nil {
		d.Notify(raised)
	}
	return raised, true, nil
}

// WithdrawUIPromptEscalation withdraws the still-open escalation question
// of one occurrence when the prompt cleared or the session ended. It
// matches on the session and (when the event names one) the occurrence
// token, so a question that is already answered or withdrawn is not
// touched again — the store's state machine refuses the transition, and
// this side only logs it.
func WithdrawUIPromptEscalation(d UIPromptEscalationDeps, ev session.UIPromptEscalateWithdrawEvent) {
	if d.Store == nil {
		return
	}
	cands := d.Store.List(QuestionFilter{
		State: QuestionOpen,
		Asker: uiPromptEscalationAsker,
		Text:  uiPromptEscalationTag,
	})
	withdrawn := 0
	for _, q := range cands {
		if q.SessionID != ev.SessionFullID {
			continue
		}
		if ev.EscalateToken != "" && q.SourceLineSHA256 != ev.EscalateToken {
			continue
		}
		if _, err := d.Store.Withdraw(q.ID, uiPromptEscalationAsker, ev.Reason); err != nil {
			d.logf("[autonomous] ui prompt escalation withdraw of %s failed (already answered?): %v", q.ID, err)
			continue
		}
		withdrawn++
	}
	if withdrawn == 0 {
		return
	}
	if d.Audit != nil {
		d.Audit(audit.ActionUIPromptEscalationWithdrawn, map[string]any{
			"session_id": ev.SessionFullID,
			"token":      ev.EscalateToken,
			"reason":     ev.Reason,
			"count":      withdrawn,
		})
	}
	d.logf("[autonomous] %d ui prompt escalation question(s) withdrawn for %s (%s)",
		withdrawn, ev.SessionFullID, ev.Reason)
}

func (d UIPromptEscalationDeps) audit(ev session.UIPromptEscalateEvent, action string) {
	if d.Audit == nil {
		return
	}
	d.Audit(action, map[string]any{
		"session_id":      ev.SessionFullID,
		"kind":            ev.PromptKind,
		"trigger":         ev.Trigger,
		"selected_option": ev.SelectedOption,
		"token":           ev.EscalateToken,
	})
}

// fallback is the one high-priority alert that replaces the question when
// the channel is off or the raise failed. It is exactly one alert per
// escalation event that reaches it; no dedupe is needed because the
// session-side hook fires at most once per occurrence.
func (d UIPromptEscalationDeps) fallback(ev session.UIPromptEscalateEvent, why string) {
	d.logf("[autonomous] ui prompt escalation for %s: %s, falling back to an alert", ev.SessionFullID, why)
	if d.FallbackAlert == nil {
		return
	}
	what := ""
	if ev.SelectedOption != "" {
		what = " (cursor on " + ev.SelectedOption + ")"
	}
	d.FallbackAlert(ev.SessionFullID,
		"Session "+ev.SessionFullID+" is blocked on a UI prompt and needs operator attention",
		fmt.Sprintf(
			"Automaton escalation: session %s (backend %s, task %s) is parked on an interstitial UI prompt of kind %q%s.\n"+
				"What the daemon did: %s\n"+
				"Answer the prompt in the session's pane; the daemon will not press any key on it.\n"+
				"(The question channel could not carry this escalation: %s.)",
			ev.SessionFullID, ev.Backend, ev.Task, ev.PromptKind, what,
			daemonActionsLine(ev), why))
}

func daemonActionsLine(ev session.UIPromptEscalateEvent) string {
	if ev.SettingEnabled {
		last := ""
		if ev.LastKey != "" && ev.LastKeyAt != nil {
			last = ", last " + ev.LastKey + " at " + ev.LastKeyAt.UTC().Format(time.RFC3339)
		}
		return fmt.Sprintf("%d dismiss key(s)", ev.DismissAttempts) + last + " were sent and the prompt is still on screen"
	}
	return "none — session.dismiss_ui_prompts is off, so no dismiss key was ever sent"
}

func (d UIPromptEscalationDeps) buildQuestion(ev session.UIPromptEscalateEvent) Question {
	q := Question{
		Label:            uiPromptEscalationLabel,
		AskedBy:          uiPromptEscalationAsker,
		AskerKind:        QuestionAskerSystem,
		SessionID:        ev.SessionFullID,
		Context:          uiPromptEscalationContext(ev),
		PRDID:            ev.PRDID,
		TaskID:           ev.TaskID,
		Text:             uiPromptEscalationText(ev),
		Default:          uiPromptEscalationDefault,
		Risk:             uiPromptEscalationRisk,
		Priority:         uiPromptEscalationPriority,
		ReturnPath:       false,
		Blocking:         true,
		Source:           QuestionSourceMarker,
		SourceLineSHA256: ev.EscalateToken,
		Options: []QuestionOption{
			{ID: "ack", Label: "Acknowledged — the operator will look at the session"},
			{ID: "hold", Label: "Leave the session where it is"},
		},
	}
	return q
}

func uiPromptEscalationContext(ev session.UIPromptEscalateEvent) string {
	switch {
	case ev.TaskID != "":
		return QuestionContextTask
	case ev.PRDID != "":
		return QuestionContextPrd
	default:
		return QuestionContextNone
	}
}

// uiPromptEscalationText assembles the question text: the tag line (what
// this is), the session identity, the prompt, what the daemon did, and the
// screen excerpt. Total length is capped at the question text cap; the
// screen tail is what is trimmed, on a UTF-8 boundary.
func uiPromptEscalationText(ev session.UIPromptEscalateEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automaton daemon escalation (%s): a worker is blocked on a UI prompt and needs operator attention.\n", uiPromptEscalationTag)
	fmt.Fprintf(&b, "Session: %s", ev.SessionFullID)
	if ev.SessionName != "" {
		fmt.Fprintf(&b, " (name %q)", ev.SessionName)
	}
	if ev.Task != "" {
		fmt.Fprintf(&b, "; task %s", ev.Task)
	}
	if ev.Backend != "" {
		fmt.Fprintf(&b, "; backend %s", ev.Backend)
	}
	if ev.ProjectDir != "" {
		fmt.Fprintf(&b, "; directory %s", ev.ProjectDir)
	}
	fmt.Fprintf(&b, "\nPrompt: kind %q, cursor on %q, on screen since %s UTC.\n",
		ev.PromptKind, ev.SelectedOption, ev.SeenSince.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Daemon so far: %s.\n", daemonActionsLine(ev))
	fmt.Fprintf(&b, "Worker identity: %s\n", workerIdentity(ev))
	b.WriteString("\nScreen (last 40 non-empty lines, marker lines neutralised):\n")
	fixed := b.String()
	if tail := ev.ScreenTail; tail != "" {
		if room := questionTextMaxBytes - len(fixed) - 1; room > 0 {
			b.WriteString(trimToUTF8Boundary(tail[:minInt(len(tail), room)]))
			b.WriteString("\n")
		}
	}
	out := b.String()
	if len(out) > questionTextMaxBytes {
		out = trimToUTF8Boundary(out[:questionTextMaxBytes])
	}
	return out
}

func trimToUTF8Boundary(s string) string {
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// workerIdentity formats the Automaton/task line (or its absence) of the
// question text.
func workerIdentity(ev session.UIPromptEscalateEvent) string {
	if ev.PRDID != "" && ev.TaskID != "" {
		return "Automaton " + ev.PRDID + ", task " + ev.TaskID
	}
	if ev.TaskID != "" {
		return "task " + ev.TaskID
	}
	return "none (interactive session — this is not a worker)"
}
