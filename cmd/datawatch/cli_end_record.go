package main

import (
	"fmt"
	"os/exec"

	"github.com/dmz006/datawatch/internal/session"
)

// endThenClosePane is the CLI's own kill, used without the daemon. It writes the
// end record before it closes the pane, and puts the record back when the pane
// could not be closed. A session that had already finished keeps its record.
func endThenClosePane(store *session.Store, sess *session.Session, why session.EndReason) error {
	before := *sess
	from := sess.State
	if !from.IsTerminal() {
		sess.State = session.StateKilled
		session.StampEnd(sess, from, why)
		if err := store.Save(sess); err != nil {
			return fmt.Errorf("save the end record, so the pane is left open: %w", err)
		}
		if err := store.Flush(); err != nil {
			return fmt.Errorf("save the end record, so the pane is left open: %w", err)
		}
	}
	if out, err := exec.Command("tmux", "kill-session", "-t", sess.TmuxSession).CombinedOutput(); err != nil {
		if !from.IsTerminal() {
			*sess = before
			if serr := store.Save(sess); serr != nil {
				fmt.Printf("[warn] restore the record of %s: %v\n", sess.ID, serr)
			}
		}
		return fmt.Errorf("tmux kill-session: %w\n%s", err, out)
	}
	return nil
}
