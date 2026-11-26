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

// startupAnswerLostAfter is how long a screen must still be up after its
// answer before the keys are taken to have been lost and may be sent again. It
// is far longer than a redraw takes.
const startupAnswerLostAfter = 5 * time.Second

// startupScreenDeclined reports a screen that is declined, not accepted. Its
// decline is never sent again while it stays up.
func startupScreenDeclined(kind StartupKind) bool {
	return kind == StartupDialogConfirmUnsafe || kind == StartupDialogUpdatePrompt
}

func startupAnswerKey(fullID string, kind StartupKind) string { return fullID + "|" + string(kind) }

// startupScreenHeldLocked reports that the screen has its answer. Callers hold
// m.startupMu.
func (m *Manager) startupScreenHeldLocked(key string, kind StartupKind) bool {
	at, answered := m.startupAnswers[key]
	return answered && (startupScreenDeclined(kind) || m.now().Sub(at) < startupAnswerLostAfter)
}

func (m *Manager) startupScreenHeld(fullID string, kind StartupKind) bool {
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	return m.startupScreenHeldLocked(startupAnswerKey(fullID, kind), kind)
}

// AnswerStartupScreen calls send, which types the answer to the start-up screen
// of this kind on session fullID, unless that screen already has its answer. A
// send that fails leaves the screen unanswered.
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
	m.startupAnswers[key] = m.now() // claimed while the keys go out
	m.startupMu.Unlock()
	err := send()
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	if err != nil {
		delete(m.startupAnswers, key)
		return err
	}
	m.startupAnswers[key] = m.now()
	return nil
}

// startupScreenLeft forgets the answer to a screen once the live pane shows it
// is no longer up, so that a later screen of the same kind is answered. The
// pane is read here: a log line without the dialog's words proves nothing.
func (m *Manager) startupScreenLeft(sess *Session, kind StartupKind) {
	pane := m.liveScreen(sess.TmuxSession)
	if pane == "" {
		return
	}
	if a, up := liveStartupDialog(pane); up && a.Kind == kind {
		return
	}
	m.startupMu.Lock()
	defer m.startupMu.Unlock()
	delete(m.startupAnswers, startupAnswerKey(sess.FullID, kind))
}
