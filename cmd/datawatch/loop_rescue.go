package main

import (
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// loopSessionView adapts a live *session.Session to the narrow
// autonomous.LoopSession view the loop rescue (internal/autonomous/
// loop_rescue.go) needs:
//
//   - AcceptsInput: true for interactive backends (pi, claude-code,
//     opencode, goose, aider — the tmux pane accepts typed input), false
//     for the one-shot print backends (pi-prompt: `pi --print` accepts no
//     typed input). Same family rule as staleWorkerAppliesTo (stale_worker.go).
//   - SendInput → session.Manager.SendInput (tmux / backend-specific route).
//   - Stop → session.Manager.Kill (kills the tmux pane if present, marks the
//     session killed; works for one-shot print sessions too).
//
// The function fields keep this adaptable in cmd-level tests without a live
// session manager.
type loopSessionView struct {
	fullID string
	input  bool
	sendFn func(text string) error
	stopFn func() error
}

func newLoopSessionView(s *session.Session, mgr *session.Manager) loopSessionView {
	return loopSessionView{
		fullID: s.FullID,
		input:  s.BackendFamily != "pi-prompt",
		sendFn: func(text string) error { return mgr.SendInput(s.FullID, text, "loop-rescue") },
		stopFn: func() error { return mgr.Kill(s.FullID) },
	}
}

// Implement autonomous.LoopSession.
func (v loopSessionView) ID() string               { return v.fullID }
func (v loopSessionView) AcceptsInput() bool       { return v.input }
func (v loopSessionView) SendInput(t string) error { return v.sendFn(t) }
func (v loopSessionView) Stop() error              { return v.stopFn() }

var _ autonomous.LoopSession = loopSessionView{}
