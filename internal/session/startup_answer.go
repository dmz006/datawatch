// One start-up screen gets one answer. Several parts of the daemon watch a new
// session's screen, and a terminal takes a moment to redraw after an answer: a
// second key sent in that moment lands on whatever screen comes next. Every
// key that answers a start-up screen is sent through AnswerStartupScreen.

package session

import (
	"errors"
	"time"
)

// ErrStartupScreenAnswered is returned when keys for this screen have already
// been sent: nothing was typed.
var ErrStartupScreenAnswered = errors.New("not typed: this start-up screen has already been answered")

// startupAnswerLostAfter is how long the same dialog must still be up after its
// answer before the keys are taken to have been lost and may be sent again. It
// is far longer than a redraw takes.
const startupAnswerLostAfter = 5 * time.Second

// startupScreenDeclined reports a screen that is declined, not accepted. Its
// decline is never sent again while it stays up.
func startupScreenDeclined(kind StartupKind) bool {
	return kind == StartupDialogConfirmUnsafe || kind == StartupDialogUpdatePrompt
}

func startupAnswerKey(fullID string, kind StartupKind) string { return fullID + "|" + string(kind) }

// startupScreenHeldLocked reports that the screen has its answer, or is being
// given it. Callers hold m.startupMu.
func (m *Manager) startupScreenHeldLocked(key string, kind StartupKind) bool {
	if m.startupSending[key] {
		return true
	}
	at, answered := m.startupAnswers[key]
	return answered && (startupScreenDeclined(kind) || time.Since(at) < startupAnswerLostAfter)
}

func (m *Manager) startupScreenHeld(fullID string, kind StartupKind) bool {
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	return m.startupScreenHeldLocked(startupAnswerKey(fullID, kind), kind)
}

// forgetStartupAnswers drops the records of declines on session fullID for
// dialogs the screen no longer shows (every kind but shown), so that a later
// dialog of the kind can be declined. The record of an accept just lapses: one
// reading without the dialog, a half-drawn screen perhaps, must not let it be
// typed again at once.
func (m *Manager) forgetStartupAnswers(fullID string, shown StartupKind) {
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	for _, kind := range []StartupKind{StartupDialogConfirmUnsafe, StartupDialogUpdatePrompt} {
		// One being typed is kept.
		if key := startupAnswerKey(fullID, kind); kind != shown && !m.startupSending[key] {
			delete(m.startupAnswers, key)
		}
	}
}

// AnswerStartupScreen calls send, which types the answer to the start-up screen
// of this kind on session fullID, unless that screen already has its answer. A
// send that typed nothing leaves the screen unanswered; one that typed and then
// held Enter back counts as an answer, so nothing is typed again at once.
func (m *Manager) AnswerStartupScreen(fullID string, kind StartupKind, send func() error) error {
	key := startupAnswerKey(fullID, kind)
	m.startupMu.Lock()
	if m.startupScreenHeldLocked(key, kind) {
		m.startupMu.Unlock()
		return ErrStartupScreenAnswered
	}
	if m.startupAnswers == nil {
		m.startupAnswers = map[string]time.Time{}
	}
	if m.startupSending == nil {
		m.startupSending = map[string]bool{}
	}
	m.startupSending[key] = true // claimed while the keys go out
	m.startupMu.Unlock()
	err := send()
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	delete(m.startupSending, key)
	if err != nil && !errors.Is(err, ErrPromptOnScreen) {
		delete(m.startupAnswers, key)
		return err
	}
	m.startupAnswers[key] = time.Now()
	return err
}
