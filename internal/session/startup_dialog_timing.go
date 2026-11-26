// When a key may be sent to a start-up dialog. Claude Code draws a dialog
// before it will keep a key: measured on 2.1.332, a Down sent at once moves the
// cursor, and about 150 ms after the dialog appeared it is drawn again with the
// cursor back on "No, exit" (147 to 155 ms idle; 123 to 184 ms with Claude's
// CPU core shared with busy loops). An Enter that follows then exits Claude.
//
// So a key goes only to a dialog that has stood unchanged for a settle time;
// Enter is pressed only while the cursor is on the accepting option, read just
// before it; and one dialog gets one answer.

package session

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// startupSettle is how long a dialog must stand unchanged before a key is
	// sent to it: over ten times the latest second drawing measured, which
	// leaves room for a slower machine or a slower start than the ones
	// measured. The cursor checks below do not depend on it being enough.
	startupSettle     = 2 * time.Second
	startupSettlePoll = 100 * time.Millisecond
	// startupCursorWait is how long the cursor has to reach the accepting
	// option after Down.
	startupCursorWait = 2 * time.Second
	// startupCursorHold is how long it must then stay there before Enter, so
	// that a second drawing later than the settle time shows itself.
	startupCursorHold = 500 * time.Millisecond
)

var (
	// errStartupNotSettled: the dialog to answer is not standing on the screen;
	// nothing was typed.
	errStartupNotSettled = errors.New("not typed: no unchanged start-up dialog on the screen")
	// errStartupEnterHeld: Enter was not pressed because the screen no longer
	// showed the dialog with the cursor on the accepting option.
	errStartupEnterHeld = fmt.Errorf("%w: Enter not pressed, the cursor is not on the accepting option", ErrPromptOnScreen)
)

// declineChoices start the options that refuse or leave.
var declineChoices = []string{"no", "exit", "cancel", "skip", "don't", "do not", "don’t", "abort", "reject", "quit", "decline", "deny", "continue without"}

// cursorAccepts reports that Enter on screen d would accept the dialog.
func cursorAccepts(d startupScreen) bool {
	switch d.action.Kind {
	case StartupDialogTrustFolder:
		return strings.HasPrefix(d.selected, "yes")
	case StartupDialogBypassAccept:
		return strings.HasPrefix(d.selected, "yes, i accept")
	case StartupDialogDevChannels:
		return strings.HasPrefix(d.selected, "i am using this for local development")
	case StartupDialogMCPTrust:
		return d.selectedNumber == "1" && !startsDecline(d.selected)
	case StartupDialogConfirmContinue:
		// A plain confirmation has no options: Enter is its accept key.
		return !startsDecline(d.selected)
	}
	return false
}

func startsDecline(option string) bool {
	for _, w := range declineChoices {
		if option == w || strings.HasPrefix(option, w+" ") || strings.HasPrefix(option, w+",") {
			return true
		}
	}
	return false
}

// enterAccepts reports that pane still shows the dialog of this kind with the
// cursor on its accepting option.
func enterAccepts(pane string, kind StartupKind) bool {
	d, ok := readStartupScreen(pane)
	return ok && d.action.Kind == kind && cursorAccepts(d)
}

// settledScreen returns the dialog on the live screen once it has stood there,
// unchanged, for the settle time. It returns false at once when there is none,
// and as soon as it changes or goes.
func (m *Manager) settledScreen(fullID, tmuxSession string) (startupScreen, bool) {
	first, ok := readStartupScreen(m.liveScreen(tmuxSession))
	if !ok {
		m.forgetStartupAnswers(fullID, StartupDialogNone)
		return startupScreen{}, false
	}
	m.forgetStartupAnswers(fullID, first.action.Kind)
	if m.startupScreenHeld(fullID, first.action.Kind) {
		return startupScreen{}, false
	}
	for end := time.Now().Add(startupSettle); time.Now().Before(end); {
		time.Sleep(startupSettlePoll)
		now, ok := readStartupScreen(m.liveScreen(tmuxSession))
		if !ok || now.text != first.text {
			return startupScreen{}, false
		}
	}
	return first, true
}

// SettledStartupDialog returns the start-up dialog of session fullID once it
// has stood unchanged for the settle time and has not been answered. It waits
// for that time when there is a dialog.
func (m *Manager) SettledStartupDialog(fullID string) (StartupDialogAction, bool) {
	sess, ok := m.store.Get(fullID)
	if !ok || sess.TmuxSession == "" {
		return StartupDialogAction{Kind: StartupDialogNone}, false
	}
	d, ok := m.settledScreen(fullID, sess.TmuxSession)
	if !ok {
		return StartupDialogAction{Kind: StartupDialogNone}, false
	}
	return d.action, true
}

// guardEnter makes the tmux layer read the screen just before each Enter it
// presses in tmuxSession, and press it only while it accepts the dialog of this
// kind. A tmux layer that cannot is left as it is. Call release when done.
func (m *Manager) guardEnter(tmuxSession string, kind StartupKind) (release func()) {
	t, ok := m.tmux.(*TmuxManager)
	if !ok {
		return func() {}
	}
	t.enterChecks.Store(tmuxSession, func(pane string) bool { return enterAccepts(pane, kind) })
	return func() { t.enterChecks.Delete(tmuxSession) }
}

// GuardStartupEnter is guardEnter for the session fullID.
func (m *Manager) GuardStartupEnter(fullID string, kind StartupKind) (release func()) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return func() {}
	}
	return m.guardEnter(sess.TmuxSession, kind)
}
