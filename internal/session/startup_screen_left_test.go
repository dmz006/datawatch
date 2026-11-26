// Delivery 1's tests name the way an answered screen used to be forgotten. The
// combined build forgets it as main does, so here the name stands for main's
// rule on a live reading: a decline is forgotten once its dialog is not shown,
// and the record of an accept only lapses with time.

package session

func (m *Manager) startupScreenLeft(sess *Session, _ StartupKind) {
	pane := m.liveScreen(sess.TmuxSession)
	if pane == "" {
		return
	}
	shown := StartupDialogNone
	if a, up := liveStartupDialog(pane); up {
		shown = a.Kind
	}
	m.forgetStartupAnswers(sess.FullID, shown)
}
