// UI-prompt escalation: when the safe-dismiss rules (ui_prompt_dismiss.go)
// cannot clear a recognised interstitial prompt — either the dismissal is
// switched off, or the attempts cap is used up and the prompt is still on
// screen — the blocked session escalates to the operator through the question
// channel. The session package cannot import the daemon's question store, so
// the escalation flows out through two hooks (setUIPromptEscalateHook,
// setUIPromptEscalateWithdrawHook) wired in cmd/datawatch — the same seam as
// workerQuestion and uiPromptAudit. Zero value (nil hook) means escalation
// is off and the manager never sends anything.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Trigger names for the escalation event. "attempts exhausted" = the manager
// has spent the configured safe-dismiss attempts and the prompt is still on
// screen after the configured clear bound since the last key. "setting
// off" = session.dismiss_ui_prompts is off (no key would ever be sent), and
// the prompt has been on screen for the delay plus one clear bound.
const (
	UIPromptEscalationTriggerAttemptsExhausted = "attempts_exhausted"
	UIPromptEscalationTriggerSettingOff        = "setting_off"
)

// Withdraw reasons the manager sends with a withdrawal.
const (
	UIPromptEscalationReasonPromptCleared = "prompt_cleared"
	UIPromptEscalationReasonSessionEnded  = "session_ended"
)

// uiPromptEscalationTailLines is how many non-empty screen lines the
// escalation event carries (ANSI-stripped, marker lines neutralised).
const uiPromptEscalationTailLines = 40

// uiPromptEscalationMarkers are the daemon marker line prefixes that must
// never reach the question channel from inside a screen excerpt: a line
// starting with one of them would be scanned back into a worker question,
// a worker answer, or a needs-input signal. The question text is the last
// thing we want a marker to be.
var uiPromptEscalationMarkers = []string{
	"DATAWATCH_QUESTION:",
	"DATAWATCH_ANSWER:",
	"DATAWATCH_NEEDS_INPUT:",
}

// UIPromptEscalateEvent is what the capture tick hands the hook when a
// blocked interstitial prompt should be escalated. It is a pure snapshot —
// the manager never acts on the hook's result; raising, deduplicating and
// withdrawing the question are the daemon's job (see
// internal/autonomous/ui_prompt_escalation.go).
type UIPromptEscalateEvent struct {
	SessionFullID  string
	SessionID      string // short id
	SessionName    string
	Task           string
	PRDID          string
	TaskID         string
	Backend        string
	ProjectDir     string
	PromptKind     string
	SelectedOption string
	// SettingEnabled is session.dismiss_ui_prompts at the moment of
	// escalation.
	SettingEnabled bool
	// DismissAttempts is the number of safe dismiss keys already sent for
	// this occurrence (0 in the "setting off" trigger). LastKey is the key
	// name of the most recent one; LastKeyAt its wall-clock time (nil when
	// nothing was sent yet).
	DismissAttempts int
	LastKey         string
	LastKeyAt       *time.Time
	// SeenSince is the first tick that recognised the prompt.
	SeenSince time.Time
	// Trigger is one of the UIPromptEscalationTrigger* constants.
	Trigger string
	// ScreenTail is the last uiPromptEscalationTailLines non-empty lines of
	// the pane: ANSI-stripped, every marker line neutralised. Empty string
	// when the pane could not be captured.
	ScreenTail string
	// EscalateToken identifies the escalation for this prompt occurrence:
	// it is the dedupe key (question source_line_sha256) and the matching
	// key for the later withdrawal.
	EscalateToken string
	At            time.Time
}

// UIPromptEscalateWithdrawEvent tells the daemon to withdraw the still-open
// escalation question for a prompt occurrence, because the prompt cleared
// or the session ended. When EscalateToken is set, only the question with
// that token is withdrawn; when it is empty, every open escalation question
// of the session is withdrawn (the manager no longer knows which one was
// raised — e.g. the occurrence record is already gone).
type UIPromptEscalateWithdrawEvent struct {
	SessionFullID string
	EscalateToken string
	Reason        string
}

// uiPromptEscalateRoute holds the escalation question hook behind its own
// lock, so a setter called after the capture loop starts never races a
// read. The hook returns the question id it stored (or the still-open
// duplicate it reused) so the manager can attach it to the session's
// UIPrompt record; a non-nil err means the daemon could not carry the
// escalation and already fell back, exactly once, to an alert.
type uiPromptEscalateRoute struct {
	mu   sync.RWMutex
	hook func(UIPromptEscalateEvent) (questionID string, err error)
}

// uiPromptEscalateWithdrawRoute holds the withdrawal hook behind its own
// lock, same seam.
type uiPromptEscalateWithdrawRoute struct {
	mu   sync.RWMutex
	hook func(UIPromptEscalateWithdrawEvent)
}

// SetUIPromptQuestionHook sets the receiver for UI-prompt escalation
// events. It runs on the capture loop, so it must return quickly: the
// daemon side raises against the in-memory question store and never does
// network work in it. The hook returns the question id it stored (or the
// still-open duplicate it reused) and an error when the store could not
// carry it — the daemon has then already sent exactly one fallback alert
// and logged the failure; the manager records the id on the session's
// UIPrompt record and never re-raises the occurrence.
func (m *Manager) SetUIPromptQuestionHook(fn func(UIPromptEscalateEvent) (questionID string, err error)) {
	m.uiPromptEscalate.mu.Lock()
	m.uiPromptEscalate.hook = fn
	m.uiPromptEscalate.mu.Unlock()
}

// SetUIPromptEscalateWithdrawHook sets the receiver for UI-prompt
// escalation withdrawals (prompt cleared, session ended).
func (m *Manager) SetUIPromptEscalateWithdrawHook(fn func(UIPromptEscalateWithdrawEvent)) {
	m.uiPromptEscalateWithdraw.mu.Lock()
	m.uiPromptEscalateWithdraw.hook = fn
	m.uiPromptEscalateWithdraw.mu.Unlock()
}

func (m *Manager) uiPromptEscalateHookFn() func(UIPromptEscalateEvent) (string, error) {
	if m == nil {
		return nil
	}
	m.uiPromptEscalate.mu.RLock()
	defer m.uiPromptEscalate.mu.RUnlock()
	return m.uiPromptEscalate.hook
}

func (m *Manager) uiPromptEscalateWithdrawHookFn() func(UIPromptEscalateWithdrawEvent) {
	if m == nil {
		return nil
	}
	m.uiPromptEscalateWithdraw.mu.RLock()
	defer m.uiPromptEscalateWithdraw.mu.RUnlock()
	return m.uiPromptEscalateWithdraw.hook
}

// UIPromptEscalateToken is the stable identity of one escalation: the
// session, the prompt kind and the option the cursor sits on — not a
// timestamp, so a daemon restart over the same pane re-derives the same
// token and the question store's dedupe keeps the same occurrence to one
// open question. Different prompts (different kind or selected option) of
// the same session get different tokens.
func UIPromptEscalateToken(fullID string, kind InterstitialKind, selected string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"datawatch:ui-prompt-escalation|1|%s|%s|%s", fullID, string(kind), selected)))
	return hex.EncodeToString(sum[:])
}

// maybeEscalateUIPrompt runs on every capture tick for a tick that
// recognised an interstitial prompt, right after the safe-dismiss rules
// ran. It fires the escalation hook exactly once per occurrence (the claim
// is set under uiPromptMu, the same lock the dismissal claim uses) when —
// and only when — one of the two trigger conditions holds. A nil hook
// (escalation not wired) is a silent no-op.
func (m *Manager) maybeEscalateUIPrompt(sess *Session, now time.Time) {
	if m == nil || sess == nil {
		return
	}
	hook := m.uiPromptEscalateHookFn()
	if hook == nil {
		return
	}

	st := m.UIPromptSettings()
	var (
		trigger   string
		seedToken bool
	)

	m.uiPromptMu.Lock()
	rec := m.uiPromptState[sess.FullID]
	if rec != nil {
		switch {
		case st.Enabled && rec.dismissAttempts > 0 && st.MaxAttempts > 0 &&
			rec.dismissAttempts >= st.MaxAttempts && !rec.lastKeyAt.IsZero() &&
			now.Sub(rec.lastKeyAt) >= time.Duration(st.ClearBoundSeconds)*time.Second:
			// The daemon spent all its safe attempts and the prompt is
			// still on screen after the clear bound: escalate.
			trigger = UIPromptEscalationTriggerAttemptsExhausted
		case !st.Enabled &&
			now.Sub(rec.firstSeen) >= time.Duration(st.DelaySeconds+st.ClearBoundSeconds)*time.Second:
			// The dismissal is off, so the prompt will never clear on its
			// own by the daemon: give the operator the configured delay
			// plus one clear bound, then escalate.
			trigger = UIPromptEscalationTriggerSettingOff
		}
		if trigger != "" {
			if rec.escalated {
				// Already escalated this occurrence: never fire twice,
				// not even across ticks or a daemon restart (the record
				// is in-memory; a restart with the prompt still on
				// screen re-derives the same token, and the daemon-side
				// question store dedupes on it).
				trigger = ""
			} else {
				rec.escalated = true
				seedToken = true
			}
		}
	}
	m.uiPromptMu.Unlock()
	if !seedToken {
		return
	}

	var lastKeyAt *time.Time
	if !rec.lastKeyAt.IsZero() {
		at := rec.lastKeyAt
		lastKeyAt = &at
	}
	ev := UIPromptEscalateEvent{
		SessionFullID:   sess.FullID,
		SessionID:       sess.ID,
		SessionName:     sess.Name,
		Task:            sess.Task,
		PRDID:           sess.PRDID,
		TaskID:          sess.TaskID,
		Backend:         sess.BackendFamily,
		ProjectDir:      sess.ProjectDir,
		PromptKind:      string(rec.kind),
		SelectedOption:  rec.selected,
		SettingEnabled:  st.Enabled,
		DismissAttempts: rec.dismissAttempts,
		LastKey:         rec.lastKey,
		LastKeyAt:       lastKeyAt,
		SeenSince:       rec.firstSeen,
		Trigger:         trigger,
		ScreenTail:      m.escalationScreenTail(sess),
		EscalateToken:   UIPromptEscalateToken(sess.FullID, rec.kind, rec.selected),
		At:              now,
	}
	questionID, raiseErr := hook(ev)
	m.bookUIPromptQuestion(sess, questionID, raiseErr)
	m.debugf("ui-prompt: %s: escalated kind=%s trigger=%s attempts=%d token=%s…",
		sess.FullID, string(rec.kind), trigger, rec.dismissAttempts, ev.EscalateToken[:8])
}

// bookUIPromptQuestion records the answer to "where did this escalation
// go": the question id from the hook lands on the in-memory occurrence
// record (UIPromptFor snapshot) and, when present, on the session's
// UIPrompt record (UIPromptInfo.QuestionID, persisted) so REST consumers
// and the PWA can name the open question. A hook error means the daemon
// fell back to its one alert already: log it here too and keep the record
// without a question id. Best-effort — the capture loop is never
// distressed by a tracking failure, and the occurrence is never re-raised
// (the claim was already set above).
func (m *Manager) bookUIPromptQuestion(sess *Session, questionID string, raiseErr error) {
	if m == nil || sess == nil {
		return
	}
	if raiseErr != nil {
		// The daemon answered the failure (its one alert fallback already
		// went out): leave the occurrence with no question id rather than
		// pointing it at a question that was never stored.
		m.uiPromptMu.Lock()
		if r := m.uiPromptState[sess.FullID]; r != nil {
			r.questionID = ""
		}
		m.uiPromptMu.Unlock()
		m.debugf("ui-prompt: %s: escalation question not raised: %v (daemon fell back to its alert)", sess.FullID, raiseErr)
		return
	}
	if questionID == "" {
		return
	}
	m.uiPromptMu.Lock()
	if r := m.uiPromptState[sess.FullID]; r != nil {
		r.questionID = questionID
	}
	m.uiPromptMu.Unlock()
	applied := false
	m.store.Mutate(func() {
		if sess.UIPrompt != nil && sess.UIPrompt.QuestionID == "" {
			sess.UIPrompt.QuestionID = questionID
			sess.UpdatedAt = m.now()
			applied = true
		}
	})
	if !applied {
		return
	}
	if err := m.SaveSession(sess); err != nil {
		m.debugf("ui-prompt: %s: save after question %s: %v", sess.FullID, questionID, err)
	}
	m.debugf("ui-prompt: %s: escalated question %s recorded on the UIPrompt record", sess.FullID, questionID)
}

// escalationScreenTail captures the pane one more time and hands back
// the last uiPromptEscalationTailLines non-empty lines: ANSI-stripped, marker
// lines neutralised (see the uiPromptEscalationMarkers rationale). A
// capture failure or an empty pane yields the empty string — the
// escalation still goes through without a screen excerpt, it is never
// blocked by one.
func (m *Manager) escalationScreenTail(sess *Session) string {
	if m == nil || sess == nil || m.tmux == nil {
		return ""
	}
	capture, err := m.tmux.CapturePaneLiveTail(sess.TmuxSession)
	if err != nil {
		m.debugf("ui-prompt: %s: escalation screen capture failed (%v); escalating without excerpt", sess.FullID, err)
		return ""
	}
	tail := lastNonEmptyLines(StripANSI(capture), uiPromptEscalationTailLines)
	if tail == "" {
		return ""
	}
	lines := strings.Split(tail, "\n")
	for i, line := range lines {
		lines[i] = neutralizePromptMarkerLines(line)
	}
	return strings.Join(lines, "\n")
}

// neutralizePromptMarkerLines defuses a pane line that would be scanned as
// a daemon marker when it reaches the question text: it prefixes the line
// with a single space, which breaks the line-start marker pattern without
// changing the visible text. Non-marker lines come back untouched.
func neutralizePromptMarkerLines(line string) string {
	for _, marker := range uiPromptEscalationMarkers {
		if strings.HasPrefix(line, marker) {
			return " " + line
		}
	}
	return line
}

// emitUIPromptEscalationWithdraw sends one withdrawal for an occurrence's
// escalation, if an escalation was actually sent for it (rec==nil means the
// record is already gone — a session-wide withdrawal instead). A nil hook
// is a silent no-op.
func (m *Manager) emitUIPromptEscalationWithdraw(fullID string, rec *uiPromptRecord, reason string) {
	hook := m.uiPromptEscalateWithdrawHookFn()
	if hook == nil {
		return
	}
	token := ""
	if rec != nil {
		token = UIPromptEscalateToken(fullID, rec.kind, rec.selected)
	}
	hook(UIPromptEscalateWithdrawEvent{
		SessionFullID: fullID,
		EscalateToken: token,
		Reason:        reason,
	})
	m.debugf("ui-prompt: %s: escalation withdraw (reason=%s, occurrence=%v)", fullID, reason, rec != nil)
}

// emitUIPromptEscalationEnd sends the session-end withdrawal: the session
// is over, so every open escalation question of it is withdrawn (rec is
// nil — no occurrence token; the daemon matches on the session and the
// escalation tag there).
func (m *Manager) emitUIPromptEscalationEnd(fullID string) {
	m.emitUIPromptEscalationWithdraw(fullID, nil, UIPromptEscalationReasonSessionEnded)
}
