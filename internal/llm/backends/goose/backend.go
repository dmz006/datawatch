// Package goose implements the LLM backend for Block's Goose agent (https://github.com/block/goose).
package goose

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

func init() {
	llm.Register(New("goose"))
	llm.Register(NewPrompt("goose"))
}

// Backend runs goose in interactive TUI session mode.
type Backend struct {
	binary      string
	sessionName string
}

// New creates a goose backend. binary defaults to "goose".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "goose"
	}
	return &Backend{binary: resolveBinary(binary)}
}

// resolveBinary returns the given binary if it is an absolute path or found in
// PATH, otherwise checks common Goose install locations.
func resolveBinary(binary string) string {
	if filepath.IsAbs(binary) {
		return binary
	}
	if _, err := exec.LookPath(binary); err == nil {
		return binary
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", binary),
		filepath.Join(home, ".config", "goose", "bin", binary),
		filepath.Join("/usr/local/bin", binary),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return binary
}

func (b *Backend) Name() string                  { return "goose" }
func (b *Backend) SupportsInteractiveInput() bool { return true }
func (b *Backend) PromptRequired() bool           { return false }

// SetSessionName stores the name used to start or resume a named Goose session.
func (b *Backend) SetSessionName(name string) {
	// Goose session names must not contain colons or path separators.
	name = strings.NewReplacer(":", "-", "/", "-", "\\", "-").Replace(name)
	b.sessionName = name
}

func (b *Backend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	var cmd string
	if b.sessionName != "" {
		escapedName := strings.ReplaceAll(b.sessionName, "'", `'\''`)
		cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s session start --name '%s'",
			escapedDir, b.binary, escapedName)
	} else {
		cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s session", escapedDir, b.binary)
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

// LaunchResume resumes a prior named Goose session by name. If no name is
// available it falls back to a fresh session start.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	name := resumeID
	if name == "" {
		name = b.sessionName
	}
	var cmd string
	if name != "" {
		escapedName := strings.ReplaceAll(name, "'", `'\''`)
		cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s session resume '%s'",
			escapedDir, b.binary, escapedName)
	} else {
		cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s session", escapedDir, b.binary)
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

// PromptBackend runs goose in non-interactive one-shot mode (goose run --text).
// Always requires a task prompt.
type PromptBackend struct{ binary string }

// NewPrompt creates a goose-prompt backend for one-shot task execution.
func NewPrompt(binary string) llm.Backend {
	if binary == "" {
		binary = "goose"
	}
	return &PromptBackend{binary: resolveBinary(binary)}
}

func (b *PromptBackend) Name() string                  { return "goose-prompt" }
func (b *PromptBackend) SupportsInteractiveInput() bool { return false }
func (b *PromptBackend) PromptRequired() bool           { return true }
func (b *PromptBackend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func (b *PromptBackend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	if task == "" {
		return fmt.Errorf("goose-prompt requires a task")
	}
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	cmd := fmt.Sprintf(
		"cd '%s' && GOOSE_CLI_THEME=plain %s run --text '%s'; echo 'DATAWATCH_COMPLETE: goose done'",
		escapedDir, b.binary, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
