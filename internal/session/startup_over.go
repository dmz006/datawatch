// When a session's start-up is over: it shows its main prompt or works, its
// task is known to have arrived, or it has ended. Nothing looks for a start-up
// dialog from then on.

package session

import "strings"

// paneShowsSessionStarted reports a screen drawn by a running session: its
// input box, or a turn in progress. A dialog at the bottom is not one, and a
// shell prompt is not an input box, even one drawn with the same character.
func paneShowsSessionStarted(pane string) bool {
	if _, dialog := liveStartupDialog(pane); dialog {
		return false
	}
	lines := nonEmptyTail(StripANSI(pane), 8)
	for i, l := range lines {
		if i >= len(lines)-3 && isSpinnerLine(l) {
			return true
		}
		if !strings.HasPrefix(l, "❯") || numberedChoiceRow.MatchString(l) {
			continue
		}
		below := strings.Join(lines[i+1:], "\n")
		framed := i > 0 && isBoxSeparator(lines[i-1]) && len(lines) > i+1 && strings.Contains(below, "──")
		if framed || containsAnyPhrase(below, "⏵⏵", "? for shortcuts") {
			return true
		}
	}
	return false
}

// StartupOver reports that session fullID is past start-up. A delivery that is
// claimed but not yet seen to arrive does not count: a dialog may still be up.
func (m *Manager) StartupOver(fullID string) bool {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return true
	}
	snap := m.store.Snapshot(sess)
	if snap.State.IsTerminal() || snap.TaskVerifiedAt != nil ||
		(snap.TaskDeliveredAt != nil && !snap.TaskDeliveryUnverified) {
		return true
	}
	return snap.TmuxSession != "" && paneShowsSessionStarted(m.liveScreen(snap.TmuxSession))
}
