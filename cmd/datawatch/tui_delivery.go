package main

import (
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/session"
)

// needsTUITaskDelivery reports whether a one-shot session in waiting_input
// state needs its task delivered into the tmux shell via send_input.
//
// The decision is capability-based: it is taken from the backend's own
// SupportsInteractiveInput() capability via llm.Get(sess.BackendFamily),
// not from a backend-name list. A registered backend that does not accept
// interactive input (prompt-mode backends such as pi-prompt, goose-prompt,
// aider, and TUI backends that take the task at launch such as opencode)
// can therefore NEVER have its task typed into a shell, regardless of name.
// A 2026-10-16 incident — in which a pi-prompt session received a
// duplicated task typed into its shell because its name was missing from
// the old hardcoded exclusion list — is the reason this gate is
// capability-based.
//
// Exceptions that are never delivered via send_input, regardless of
// capability:
//   - "claude-code": the task arrives on its own /api/channel/ready
//     channel bridge; sending it here would double-deliver.
//   - "opencode-acp": communication is structured ACP messaging, not
//     shell input.
//   - "subprocess": a shell-task wrapper, not an interactive TUI.
//
// If the backend family is not registered in this process (e.g.
// "subprocess"-style families or unknown names), the gate returns true,
// preserving the historical behavior for unregistered families.
func needsTUITaskDelivery(sess *session.Session) bool {
	if sess.State != session.StateWaitingInput ||
		!sess.OneShot || sess.Task == "" || sess.BackendFamily == "" {
		return false
	}

	// Non-TUI exceptions: never type the task into the shell for these.
	switch sess.BackendFamily {
	case "claude-code": // task arrives on its own /api/channel/ready bridge; sending here would double-deliver
		return false
	case "opencode-acp": // structured ACP messaging, not shell input
		return false
	case "subprocess": // shell-task wrapper, not an interactive TUI
		return false
	}

	// Capability-based decision: ask the backend itself.
	b, err := llm.Get(sess.BackendFamily)
	if err != nil {
		// Family not registered in this process; preserve the
		// historical behavior of delivering via send_input.
		return true
	}
	return b.SupportsInteractiveInput()
}
