// UIPrompt dismissal — the safe dismiss path, driven from the existing
// capture tick (see ui_prompt_blocked.go for detection and the blocked
// state). When the pane is classified as an interstitial UI prompt, the
// manager may press the prompt's SAFE dismiss key (Escape today) to close
// it so the worker resumes, under the ui_prompt.* settings.
//
// Safety rules — all enforced in one place here:
//
//  1. Re-capture immediately before sending, and send only if the SAME
//     kind is still showing;
//  2. The key comes ONLY from the classifier's Dismiss list (see
//     interstitial_prompt.go) AND passes the IsSafeInterstitialKey allowlist
//     AND the send-site guard below. The guard is a function, not just the
//     table: it independently rejects Enter / C-m, a newline, a carriage
//     return and every digit, and it maps no key name but Esc to a
//     literal. If the guard fails, NOTHING is sent and the attempt is
//     refused, counted and audited;
//  3. One key per attempt, exactly like DeclineStartupDialog
//     (SendKeysLiteral, one keystroke, no retry inside the send), and a
//     FAILED send still counts as an attempt;
//  4. attempts < ui_prompt_max_attempts AND (no key sent yet, or at
//     least ui_prompt_clear_bound_seconds since the last key) AND the
//     prompt has been on screen at least
//     ui_prompt_dismiss_delay_seconds;
//  5. ui_prompt_dismiss is the master switch: off means the manager
//     NEVER sends any key for these prompts.
//
// Record + count:
//   - a send records a timeline line: "dismissed with Esc (prompt:
//     <kind>, attempt N)";
//   - on the next tick when the prompt no longer classifies, the clear
//     path (already restores state) records the dismissed outcome:
//     counter + audit;
//   - when the attempts cap is exhausted with the prompt still up, the
//     daemon gives up for this occurrence: "key did not clear the prompt"
//     timeline line, not_cleared counter + audit, recorded once;
//   - a refused key is counted and audited as refused.
//
// Counters are in-memory on the Manager (reset at daemon restart) and are
// exposed through the daemon (ui_prompts in GET /api/stats and the
// get_stats MCP tool) and as a Prometheus metric.

package session

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
)

// UIPrompt dismissal outcomes.
const (
	UIPromptOutcomeDismissed  = "dismissed"
	UIPromptOutcomeNotCleared = "not_cleared"
	UIPromptOutcomeRefused    = "refused"
)

// UIPromptAuditEvent carries one outcome to the daemon's audit hook (see
// SetUIPromptAuditHook). The session package cannot import the daemon's
// audit writer, so outcomes flow out through this hook — the same seam as
// SetWorkerQuestionHook.
type UIPromptAuditEvent struct {
	Outcome        string // dismissed | not_cleared | refused
	SessionFullID  string
	Backend        string // session backend family (claude-code for the known kinds)
	Kind           string // InterstitialPromptKind
	Key            string // the key name for a send/refusal ("" if none was offered)
	Attempt        int    // attempt number the outcome refers to
	SelectedOption string // the option selected in the prompt, when the pane showed one
}

// uiPromptAuditRoute is a Manager field (set via SetUIPromptAuditHook),
// mirroring workerQuestionRoute.
type uiPromptAuditRoute struct {
	mu   sync.RWMutex
	hook func(UIPromptAuditEvent)
}

// SetUIPromptAuditHook receives UI prompt dismissal outcomes for the
// daemon's audit log and metrics. It can be called from the capture
// goroutine; the hook is invoked synchronously at the moment the
// manager decides the outcome of one dismissal — the daemon decides what
// to do inside the hook (e.g. write to the audit log and its CEF mirror).
func (m *Manager) SetUIPromptAuditHook(fn func(UIPromptAuditEvent)) {
	m.uiPromptAudit.mu.Lock()
	m.uiPromptAudit.hook = fn
	m.uiPromptAudit.mu.Unlock()
}

func (m *Manager) uiPromptAuditHook() func(UIPromptAuditEvent) {
	if m == nil {
		return nil
	}
	m.uiPromptAudit.mu.RLock()
	h := m.uiPromptAudit.hook
	m.uiPromptAudit.mu.RUnlock()
	return h
}

// uiPromptOutcomeCounts is one outcome row of the in-memory dismissal
// counters.
type uiPromptOutcomeCounts struct {
	dismissed  int
	notCleared int
	refused    int
}

func (c *uiPromptOutcomeCounts) add(outcome string) {
	switch outcome {
	case UIPromptOutcomeDismissed:
		c.dismissed++
	case UIPromptOutcomeNotCleared:
		c.notCleared++
	case UIPromptOutcomeRefused:
		c.refused++
	}
}

// UIPromptKindStats is one prompt kind's outcome counts (session package
// copy of stats.UIPromptKindStats: the daemon main maps between the two,
// the same way config and session hold their copies of the defaults).
type UIPromptKindStats struct {
	Dismissed  int `json:"dismissed"`
	NotCleared int `json:"not_cleared"`
	Refused    int `json:"refused"`
}

// uiPromptStatsState is a Manager field: in-memory, mutex-guarded,
// zero-valued and ready to use (no init needed — byKind is allocated on
// first count).
type uiPromptStatsState struct {
	mu     sync.Mutex
	total  uiPromptOutcomeCounts
	byKind map[string]*uiPromptOutcomeCounts
}

func (s *uiPromptStatsState) add(kind, outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total.add(outcome)
	if s.byKind == nil {
		s.byKind = map[string]*uiPromptOutcomeCounts{}
	}
	ck := s.byKind[kind]
	if ck == nil {
		ck = &uiPromptOutcomeCounts{}
		s.byKind[kind] = ck
	}
	ck.add(outcome)
}

// UIPromptStatsSnapshot returns the in-memory dismissal counters (reset
// at daemon restart).
func (m *Manager) UIPromptStatsSnapshot() (dismissed, notCleared, refused int, byKind map[string]UIPromptKindStats) {
	if m == nil {
		return 0, 0, 0, nil
	}
	m.uiPromptDismissStats.mu.Lock()
	defer m.uiPromptDismissStats.mu.Unlock()
	return m.uiPromptDismissStats.total.dismissed,
		m.uiPromptDismissStats.total.notCleared,
		m.uiPromptDismissStats.total.refused,
		m.snapshotKindLocked()
}

func (m *Manager) snapshotKindLocked() map[string]UIPromptKindStats {
	if len(m.uiPromptDismissStats.byKind) == 0 {
		return nil
	}
	out := make(map[string]UIPromptKindStats, len(m.uiPromptDismissStats.byKind))
	for kind, c := range m.uiPromptDismissStats.byKind {
		out[kind] = UIPromptKindStats{Dismissed: c.dismissed, NotCleared: c.notCleared, Refused: c.refused}
	}
	return out
}

func (m *Manager) countUIPromptOutcome(kind, outcome string) {
	m.uiPromptDismissStats.add(kind, outcome)
}

// recordUIPromptOutcome books ONE final outcome for a dismissal: the
// in-memory counter (total and by-kind), the not_cleared timeline line
// (a dismissed outcome rides on the existing "cleared" timeline line) and
// the audit event through the hook.
func (m *Manager) recordUIPromptOutcome(sess *Session, rec *uiPromptRecord, outcome string) {
	kind := string(rec.kind)
	m.countUIPromptOutcome(kind, outcome)
	if outcome == UIPromptOutcomeNotCleared {
		m.recordUIPromptEvent(sess, rec, "key did not clear the prompt")
	}
	h := m.uiPromptAuditHook()
	if h != nil {
		h(UIPromptAuditEvent{
			Outcome:        outcome,
			SessionFullID:  sess.FullID,
			Backend:        sess.BackendFamily,
			Kind:           kind,
			Key:            rec.lastKey,
			Attempt:        rec.dismissAttempts,
			SelectedOption: rec.selected,
		})
	}
}

// dismissKeyLiteral is the SINGLE send-site guard: it turns a dismiss key
// name (as classified) into the exact literal to send, and returns "" if
// nothing may be sent. It is deliberately stricter than the table:
// even if a future table entry listed a dangerous name, the guard here
// refuses to map it.
//
//   - the name must be non-empty and on the classifier's allowlist
//     (IsSafeInterstitialKey);
//   - Enter, Return, C-m, a newline, a carriage return, a TAB, a SPACE and
//     any digit are REFUSED outright, no matter what the table says;
//   - the only name mapped to a byte sequence is Esc, and it maps to the
//     single ESC byte — exactly what DeclineStartupDialog sends.
func dismissKeyLiteral(name string) string {
	k := strings.ToLower(strings.TrimSpace(name))
	if k == "" {
		return ""
	}
	if !IsSafeInterstitialKey(k) {
		return ""
	}
	// Independent rejection set, checked before any allowlist logic.
	switch k {
	case "enter", "return", "c-m", "c-m ", "ctrl+m", "ctrl-m",
		"newline", "carriage return", "tab", "space", " ", "\t", "\r",
		"\n", "\r\n", "\n\r", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return ""
	}
	for _, r := range k {
		if unicode.IsDigit(r) || r == '\r' || r == '\n' || r == '\t' {
			return ""
		}
	}
	// Only Esc is mapped to a literal byte; any other safe name has no
	// literal this file knows how to send safely, so nothing is sent.
	if k != "esc" {
		return ""
	}
	return "\x1b"
}

// maybeDismissUIPrompt is called from the capture tick once a tick has
// classified the pane as an interstitial prompt (and booked/refreshed the
// blocked record). It applies the dismissal rules and, when (and only
// when) every gate passes, sends exactly one safe key.
func (m *Manager) maybeDismissUIPrompt(sess *Session, action InterstitialPromptAction, now time.Time) {
	st := m.UIPromptSettings()
	if !st.Enabled {
		return // master switch off — the manager never sends a key.
	}

	kind := string(action.Kind)

	m.uiPromptMu.Lock()
	rec := m.uiPromptState[sess.FullID]
	if rec == nil {
		m.uiPromptMu.Unlock()
		return
	}
	attempts := rec.dismissAttempts
	outcomeBooked := rec.outcomeRecorded
	m.uiPromptMu.Unlock()

	// The attempts cap is used up and the prompt is STILL on screen with
	// at least one key already sent: the daemon has given up on keys for
	// this occurrence. Book once — never spam a tick loop.
	if attempts > 0 && attempts >= st.MaxAttempts && !outcomeBooked {
		if m.bookUIPromptOutcome(sess.FullID) {
			m.recordUIPromptOutcome(sess, rec, UIPromptOutcomeNotCleared)
		}
		return
	}
	if outcomeBooked || attempts >= st.MaxAttempts {
		return
	}

	// The prompt must have been on screen the configured delay first.
	if now.Sub(rec.firstSeen) < time.Duration(st.DelaySeconds)*time.Second {
		return
	}
	// And — unless no key was sent yet — the clear bound after the last one.
	if !rec.lastKeyAt.IsZero() && now.Sub(rec.lastKeyAt) < time.Duration(st.ClearBoundSeconds)*time.Second {
		return
	}

	// Choose the key from the classifier's Dismiss list and run it
	// through the send-site guard BEFORE touching the pane.
	var keyName, firstUnsafe string
	for _, candidate := range rec.dismiss {
		if lit := dismissKeyLiteral(candidate); lit != "" {
			keyName = candidate
			break
		}
		if firstUnsafe == "" {
			firstUnsafe = candidate
		}
	}
	if keyName == "" {
		// The guard failed: nothing is sent. Refused = counted and
		// audited, and the attempt is consumed so a broken entry cannot
		// audit-spam.
		m.uiPromptMu.Lock()
		r := m.uiPromptState[sess.FullID]
		refusedAttempt := 0
		if r != nil {
			r.dismissAttempts++
			refusedAttempt = r.dismissAttempts
		}
		m.uiPromptMu.Unlock()
		m.countUIPromptOutcome(kind, UIPromptOutcomeRefused)
		h := m.uiPromptAuditHook()
		if h != nil {
			h(UIPromptAuditEvent{
				Outcome:        UIPromptOutcomeRefused,
				SessionFullID:  sess.FullID,
				Backend:        sess.BackendFamily,
				Kind:           kind,
				Key:            firstUnsafe,
				Attempt:        refusedAttempt,
				SelectedOption: rec.selected,
			})
		}
		m.debugf("ui-prompt: %s: dismiss key %q refused by the send-site guard; nothing sent", sess.FullID, firstUnsafe)
		return
	}

	// Rule 1: re-capture right before the send and only send if the SAME
	// kind is still showing. A capture failure or a pane that moved on in
	// the meantime means "no send" — never send on a stale classification.
	capture, err := m.tmux.CapturePaneLiveTail(sess.TmuxSession)
	if err != nil {
		m.debugf("ui-prompt: %s: cannot re-capture before dismiss (%v); not sending", sess.FullID, err)
		return
	}
	if ClassifyInterstitialPrompt(capture).Kind != action.Kind {
		m.debugf("ui-prompt: %s: pane moved off %s before the send; not sending", sess.FullID, action.Kind)
		return
	}

	// Rule 3: ONE key, sent exactly like DeclineStartupDialog. This is the
	// only place a dismissal keystroke is ever issued.
	literal := dismissKeyLiteral(keyName)
	if err := m.tmux.SendKeysLiteral(sess.TmuxSession, literal); err != nil {
		m.debugf("ui-prompt: %s: send %q failed: %v (still an attempt)", sess.FullID, literal, err)
	}

	// Count the attempt even when the send failed, and book the key +
	// time so the clear bound applies to the next one.
	m.uiPromptMu.Lock()
	r := m.uiPromptState[sess.FullID]
	attempt := 0
	if r != nil {
		r.dismissAttempts++
		r.lastKey = keyName
		r.lastKeyAt = now
		attempt = r.dismissAttempts
	}
	m.uiPromptMu.Unlock()

	line := fmt.Sprintf("dismissed with Esc (prompt: %s, attempt %d)", kind, attempt)
	m.recordUIPromptEvent(sess, rec, line)
}

// bookUIPromptOutcome claims the outcome slot for a session's current
// record exactly once (dismissed and not_cleared both write it).
func (m *Manager) bookUIPromptOutcome(fullID string) bool {
	m.uiPromptMu.Lock()
	defer m.uiPromptMu.Unlock()
	rec := m.uiPromptState[fullID]
	if rec == nil || rec.outcomeRecorded {
		return false
	}
	rec.outcomeRecorded = true
	return true
}
