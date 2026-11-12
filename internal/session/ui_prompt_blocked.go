// Interstitial-prompt blocking: the screen-capture loop recognises an
// interstitial UI prompt on a claude-code pane (interstitial_prompt.go),
// parks the session on it, restores it when the prompt clears, and exposes
// the prompt for the daemon-driven dismissal (a later phase). Detection
// only — no tmux key is ever sent here, and none of the waiting-input
// consumer side effects fire (no needs-input alert or push, no response
// capture or summarisation) because the transition path this file owns is
// deliberately distinct from the needs-input transition.
package session

import (
	"time"
)

// uiPromptRecord tracks one recognised interstitial-prompt occurrence for
// one session: kind, first/last seen, the option Enter would pick, the safe
// dismiss keys reserved for the dismissal phase, and which state we moved
// the session from. DismissAttempts and LastKeyAt stay at their zero values
// until the dismissal phase actually sends a key.
type uiPromptRecord struct {
	kind            InterstitialKind
	firstSeen       time.Time
	lastSeen        time.Time
	selected        string    // option row the cursor would confirm with Enter
	dismiss         []string  // safe dismiss keys; reserved, never sent in this phase
	changesSetting  bool      // Enter on this prompt would change a setting
	moved           bool      // true when this phase transitioned the session
	fromState       State     // state we transitioned from (restore target)
	dismissAttempts int       // safe dismiss keys already sent for this occurrence
	lastKeyAt       time.Time // when the last dismiss key was sent (zero = none yet)
	lastKey         string    // key name of the last sent dismiss key (guard-passed)
	outcomeRecorded bool      // a final dismissal outcome (dismissed | not_cleared) was booked exactly once
	escalated       bool      // the escalation for this occurrence was fired exactly once (ui_prompt_escalate.go)
	questionID      string    // id of the question the escalation raised (or the open duplicate it reused); "" when the fallback alert carried it
}

// scanInterstitialTick runs one interstitial-prompt scan for the
// screen-capture loop. It is a no-op for non-claude-code sessions and for
// states outside running/waiting_input, so it is safe to run unconditionally
// on every tick; the gate also keeps it out of the rate_limited family.
//
// While the pane shows a recognised prompt it returns true — the caller
// skips the rest of that tick's state detection — so the blocking prompt can
// not be misread as a needs-input question and none of the waiting-input
// consumer callbacks fire.
func (m *Manager) scanInterstitialTick(sess *Session, stateCapture string, now time.Time) bool {
	if sess.BackendFamily != "claude-code" {
		return false
	}
	if sess.State != StateRunning && sess.State != StateWaitingInput {
		return false
	}

	action := ClassifyInterstitialPrompt(stateCapture)
	if action.Kind == InterstitialNone {
		return m.clearUIPrompt(sess, now)
	}
	m.recogniseUIPrompt(sess, action, now)
	// The pane is on an interstitial prompt: run the safe-dismiss path.
	// Detection and park already happened above; a dismissal key is the
	// only key the manager ever sends to one of these prompts (see
	// ui_prompt_dismiss.go).
	m.maybeDismissUIPrompt(sess, action, now)
	// If the dismissal rules could not clear the prompt, the operator
	// needs to know: escalate (exactly once per occurrence; a no-op
	// unless escalation is wired and a trigger condition holds).
	m.maybeEscalateUIPrompt(sess, now)
	return true
}

// recogniseUIPrompt records the occurrence (first tick that recognises) or
// merely refreshes it (later ticks, static pane included), parks a running
// session on the prompt, and — for a session already waiting_input (e.g.
// adopted after a daemon restart) — exposes the recognised prompt on that
// state without a second transition.
func (m *Manager) recogniseUIPrompt(sess *Session, action InterstitialPromptAction, now time.Time) bool {
	m.uiPromptMu.Lock()
	rec, existed := m.uiPromptState[sess.FullID]
	if existed {
		rec.lastSeen = now
		m.uiPromptMu.Unlock()
		return true
	}
	rec = &uiPromptRecord{
		kind:           action.Kind,
		firstSeen:      now,
		lastSeen:       now,
		selected:       action.SelectedOption,
		dismiss:        action.Dismiss,
		changesSetting: action.ChangesSettingOnEnter,
	}
	m.uiPromptState[sess.FullID] = rec
	m.uiPromptMu.Unlock()

	m.moveToBlocked(sess, rec, now)
	return true
}

// moveToBlocked parks the session on the recognised prompt and records the
// change once: one state transition + onStateChange, one session save with
// UIPrompt set, one timeline line. It never fires onNeedsInput (no
// needs-input alert / push) and never captures/summarises a response — the
// pane is blocked by the prompt, it is not a question to the operator.
func (m *Manager) moveToBlocked(sess *Session, rec *uiPromptRecord, now time.Time) {
	// Session pointers are shared with the monitor goroutine and the REST
	// readers; field writes go through store.Mutate so a reader marshalling
	// under the store read lock never races these writes (see
	// Store.Mutate's doc).
	applied := false
	m.store.Mutate(func() {
		if sess.State != StateRunning && sess.State != StateWaitingInput {
			// Moved since the tick read it (e.g. completed): nothing to park.
			return
		}
		info := &UIPromptInfo{
			Kind:           rec.kind,
			Since:          rec.firstSeen,
			SelectedOption: rec.selected,
		}
		if sess.State == StateWaitingInput {
			// Adoption (daemon restarted over a blocked pane) or already parked
			// by another signal: expose the recognised prompt on the same
			// state, do not move the session and do not emit a state change.
			rec.moved = false
		} else {
			rec.moved = true
			rec.fromState = sess.State
			sess.State = StateWaitingInput
			sess.PromptContext = "ui_prompt"
		}
		sess.UIPrompt = info
		sess.UpdatedAt = now
		applied = true
	})
	if !applied {
		return
	}

	if err := m.SaveSession(sess); err != nil {
		m.debugf("ui-prompt: save %s: %v", sess.FullID, err)
	}
	if rec.moved && m.onStateChange != nil {
		m.onStateChange(sess, rec.fromState)
	}
	m.recordUIPromptEvent(sess, rec, "recognised, session blocked")
	m.debugf("ui-prompt: %q %s: recognised kind=%s selected=%q (moved=%v from=%s)",
		sess.TmuxSession, sess.FullID, rec.kind, rec.selected, rec.moved, stateNameOrDefault(rec.fromState))
}

// clearUIPrompt removes the occurrence record and, only when this phase
// parked the session and nothing else moved it since (state is still
// waiting_input), restores the previous state — never a complete/failed/
// killed one. For an adopted parked session (moved=false) the state is left
// alone; the exposed UIPrompt is still cleared from the session record.
func (m *Manager) clearUIPrompt(sess *Session, now time.Time) bool {
	m.uiPromptMu.Lock()
	rec := m.uiPromptState[sess.FullID]
	if rec == nil {
		m.uiPromptMu.Unlock()
		return false
	}
	// If safe keys were sent and the prompt is now gone, book the
	// dismissed outcome NOW: the claim under uiPromptMu is atomic with the
	// delete, so the not_cleared path (which needs the record still live)
	// can never claim it too. See ui_prompt_dismiss.go.
	dismissedNow := false
	if rec.dismissAttempts > 0 && !rec.outcomeRecorded {
		rec.outcomeRecorded = true
		dismissedNow = true
	}
	delete(m.uiPromptState, sess.FullID)
	m.uiPromptMu.Unlock()

	restored := false
	touched := false
	m.store.Mutate(func() {
		if rec.moved && sess.State == StateWaitingInput {
			sess.State = rec.fromState
			restored = true
			touched = true
		}
		if sess.UIPrompt != nil && !sess.State.IsTerminal() {
			sess.UIPrompt = nil
			touched = true
		}
		if touched {
			sess.UpdatedAt = now
		}
	})

	if touched {
		if err := m.SaveSession(sess); err != nil {
			m.debugf("ui-prompt: save %s: %v", sess.FullID, err)
		}
	}
	if restored && m.onStateChange != nil {
		m.onStateChange(sess, StateWaitingInput)
	}
	m.recordUIPromptEvent(sess, rec, "cleared")
	if dismissedNow {
		m.recordUIPromptOutcome(sess, rec, UIPromptOutcomeDismissed)
	}
	// The prompt cleared: if this occurrence was escalated, withdraw the
	// still-open question (the daemon dedupes and is idempotent).
	if rec.escalated {
		m.emitUIPromptEscalationWithdraw(sess.FullID, rec, UIPromptEscalationReasonPromptCleared)
	}
	if restored {
		m.debugf("ui-prompt: %q: cleared kind=%s, restored to %s", sess.TmuxSession, rec.kind, rec.fromState)
	}
	return false
}

// recordUIPromptEvent appends exactly one timestamped ui-prompt line to
// timeline.md (recognised / cleared) — one line per change, never per tick.
// Best-effort: tracking failures must never disturb the capture loop.
func (m *Manager) recordUIPromptEvent(sess *Session, rec *uiPromptRecord, event string) {
	tracker := m.getTracker(sess.FullID)
	if tracker == nil {
		return
	}
	if err := tracker.RecordUIPrompt(string(rec.kind), rec.selected, event); err != nil {
		m.debugf("ui-prompt: timeline %s: %v", sess.FullID, err)
	}
}

// UIPromptFor returns a snapshot of the interstitial prompt currently
// blocking the session, or nil when no recognised prompt is being tracked
// for it. Safe for REST / MCP / server consumers to call on any request.
func (m *Manager) UIPromptFor(fullID string) *UIPromptInfo {
	m.uiPromptMu.Lock()
	defer m.uiPromptMu.Unlock()
	rec := m.uiPromptState[fullID]
	if rec == nil {
		return nil
	}
	info := &UIPromptInfo{
		Kind:            rec.kind,
		Since:           rec.firstSeen,
		SelectedOption:  rec.selected,
		DismissAttempts: rec.dismissAttempts,
		QuestionID:      rec.questionID,
	}
	if !rec.lastKeyAt.IsZero() {
		at := rec.lastKeyAt
		info.LastKeyAt = &at
	}
	return info
}

// stateNameOrDefault renders a possibly-empty State for debug lines.
func stateNameOrDefault(s State) string {
	if s == "" {
		return "n/a"
	}
	return string(s)
}
