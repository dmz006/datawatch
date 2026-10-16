package main

import "github.com/dmz006/datawatch/internal/session"

// needsTUITaskDelivery reports whether a one-shot session needs its task
// delivered via send_input once it reaches waiting_input. TUI backends do
// not call /api/channel/ready (no channel bridge JS), so the task must be
// injected with send_input. Excluded: claude-code (channel bridge),
// opencode-acp (ACP structured API), subprocess (shell task wrapping),
// opencode (task delivered at launch), and the prompt-mode backends
// pi-prompt, goose-prompt, and aider, which take their task at launch
// rather than via send_input into a tmux shell.
func needsTUITaskDelivery(sess *session.Session) bool {
	return sess.State == session.StateWaitingInput &&
		sess.OneShot && sess.Task != "" &&
		sess.BackendFamily != "claude-code" &&
		sess.BackendFamily != "opencode-acp" &&
		sess.BackendFamily != "opencode" &&
		sess.BackendFamily != "subprocess" &&
		sess.BackendFamily != "pi-prompt" &&
		sess.BackendFamily != "goose-prompt" &&
		sess.BackendFamily != "aider" &&
		sess.BackendFamily != ""
}
