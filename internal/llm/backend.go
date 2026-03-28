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
