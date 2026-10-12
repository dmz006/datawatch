// Package llm defines the LLMBackend interface for pluggable AI coding assistants.
// Current implementation: claude-code (Anthropic Claude Code CLI).
// Future: aider, continue.dev, OpenAI Codex CLI, custom scripts.
package llm

import "context"

// Backend is the interface all LLM coding assistant backends must implement.
type Backend interface {
	// Name returns the backend identifier (e.g. "claude-code").
	Name() string

	// Launch starts the LLM assistant for the given task inside the given tmux session.
	// The tmux session must exist before Launch is called.
	// projectDir is the working directory to run in.
	// logFile receives all output via tmux pipe-pane.
	Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error

	// SupportsInteractiveInput returns true if the backend accepts stdin while running.
	SupportsInteractiveInput() bool

	// Version returns the backend's version string if detectable, else empty string.
	Version() string
}

// Resumable is an optional interface backends can implement to support resuming
// a prior LLM session by ID (e.g. opencode -s SESSION_ID, claude --resume SESSION_ID).
type Resumable interface {
	LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error
}

// Nameable is an optional interface backends can implement to accept a display
// name for the session (e.g. claude --name <name>).
type Nameable interface {
	SetSessionName(name string)
}

// PromptRequirer is an optional interface indicating the backend requires a non-empty task.
// When true, the web UI enforces a filled prompt field before starting a session.
type PromptRequirer interface {
	PromptRequired() bool
}

// MessageSender is an optional interface for backends that handle input via their own
// API (e.g. HTTP) instead of tmux send-keys. When implemented, the session manager
// routes SendInput through SendMessage instead of tmux.
type MessageSender interface {
	SendMessage(tmuxSession, text string) error
}

// ToolCapable is an optional interface a backend implements to declare
// that it has real file/tool access while running — a coding agent CLI
// (reads files, runs commands, edits code) rather than just a chat UI
// driven inside a tmux pane. ollama/openwebui run as tmux sessions too
// (session.Manager drives any llm.Backend the same way) but are plain
// chat — they never implement this interface, so a type assertion
// against it is the capability check.
//
// B124 (2026-10-12) — found live debugging why BL416's PRD verifier
// never got real tool access despite being pointed at a tool-capable
// backend: the routing decision (needsSessionJudgment,
// cmd/datawatch/session_judgment.go) used to check a hand-maintained
// switch of kind-name literals in a different package
// (inference.IsSessionBackendKind), which had already drifted from
// reality (it omitted plain "opencode", which this package's own
// opencode.Backend — a real tmux CLI agent — registers under). Tying
// the check to this interface instead means a future backend becomes
// eligible as a tool-capable verifier purely by implementing
// HasToolAccess() returning true — no separate list to keep in sync.
type ToolCapable interface {
	HasToolAccess() bool
}
