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
	binary         string
	sessionName    string
	provider       string
	model          string
	apiKey         string
	channelEnabled bool
	sessionFullID  string // BL363 T3 — passed to MCP server via --caller-session-id
}

// New creates a goose backend. binary defaults to "goose".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "goose"
	}
	return &Backend{binary: resolveBinary(binary)}
}

// providerKeyEnvVar maps a Goose provider name to the env var that holds its API key.
func providerKeyEnvVar(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "google", "gemini":
		return "GOOGLE_API_KEY"
	default:
		return "GOOSE_API_KEY"
	}
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

func (b *Backend) SetProvider(p string)        { b.provider = p }
func (b *Backend) SetModel(m string)            { b.model = m }
func (b *Backend) SetAPIKey(k string)           { b.apiKey = k }
func (b *Backend) SetChannelEnabled(v bool)     { b.channelEnabled = v }
func (b *Backend) SetSessionFullID(id string)   { b.sessionFullID = id }

// gooseEnvPrefix returns the env var prefix string to prepend to commands,
// sourced from provider/model/apiKey/channel fields set before Launch is called.
func (b *Backend) gooseEnvPrefix() string {
	var parts []string
	if b.provider != "" {
		parts = append(parts, "GOOSE_PROVIDER="+shellQuote(b.provider))
	}
	if b.model != "" {
		parts = append(parts, "GOOSE_MODEL="+shellQuote(b.model))
	}
	if b.apiKey != "" && b.provider != "" {
		envVar := providerKeyEnvVar(b.provider)
		parts = append(parts, envVar+"="+shellQuote(b.apiKey))
	}
	// BL363 T3 — register datawatch as a Goose MCP extension (stdio transport).
	// Goose reads GOOSE_MCP__<NAME>__TYPE/CMD/ARGS at startup to load MCP servers.
	if b.channelEnabled && b.sessionFullID != "" {
		if binaryPath, err := os.Executable(); err == nil {
			parts = append(parts,
				"GOOSE_MCP__DATAWATCH__TYPE=stdio",
				"GOOSE_MCP__DATAWATCH__CMD="+shellQuote(binaryPath),
				"GOOSE_MCP__DATAWATCH__ARGS="+shellQuote("mcp,--caller-session-id,"+b.sessionFullID),
			)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
	env := b.gooseEnvPrefix()
	var cmd string
	if b.sessionName != "" {
		escapedName := strings.ReplaceAll(b.sessionName, "'", `'\''`)
		cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s%s session start --name '%s'",
			escapedDir, env, b.binary, escapedName)
	} else {
		cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s%s session", escapedDir, env, b.binary)
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

// LaunchResume resumes a prior named Goose session by name. If no name is
// available it falls back to a fresh session start.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	env := b.gooseEnvPrefix()
	name := resumeID
	if name == "" {
		name = b.sessionName
	}
	var cmd string
	if name != "" {
		escapedName := strings.ReplaceAll(name, "'", `'\''`)
		cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s%s session resume '%s'",
			escapedDir, env, b.binary, escapedName)
	} else {
		cmd = fmt.Sprintf("cd '%s' && GOOSE_CLI_THEME=plain %s%s session", escapedDir, env, b.binary)
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

// PromptBackend runs goose in non-interactive one-shot mode (goose run --text).
// Always requires a task prompt.
type PromptBackend struct {
	binary         string
	provider       string
	model          string
	apiKey         string
	channelEnabled bool
	sessionFullID  string
}

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

func (b *PromptBackend) SetProvider(p string)      { b.provider = p }
func (b *PromptBackend) SetModel(m string)          { b.model = m }
func (b *PromptBackend) SetAPIKey(k string)         { b.apiKey = k }
func (b *PromptBackend) SetChannelEnabled(v bool)   { b.channelEnabled = v }
func (b *PromptBackend) SetSessionFullID(id string) { b.sessionFullID = id }

func (b *PromptBackend) gooseEnvPrefix() string {
	var parts []string
	if b.provider != "" {
		parts = append(parts, "GOOSE_PROVIDER="+shellQuote(b.provider))
	}
	if b.model != "" {
		parts = append(parts, "GOOSE_MODEL="+shellQuote(b.model))
	}
	if b.apiKey != "" && b.provider != "" {
		envVar := providerKeyEnvVar(b.provider)
		parts = append(parts, envVar+"="+shellQuote(b.apiKey))
	}
	if b.channelEnabled && b.sessionFullID != "" {
		if binaryPath, err := os.Executable(); err == nil {
			parts = append(parts,
				"GOOSE_MCP__DATAWATCH__TYPE=stdio",
				"GOOSE_MCP__DATAWATCH__CMD="+shellQuote(binaryPath),
				"GOOSE_MCP__DATAWATCH__ARGS="+shellQuote("mcp,--caller-session-id,"+b.sessionFullID),
			)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

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
	env := b.gooseEnvPrefix()
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	cmd := fmt.Sprintf(
		"cd '%s' && GOOSE_CLI_THEME=plain %s%s run --text '%s'; echo 'DATAWATCH_COMPLETE: goose done'",
		escapedDir, env, b.binary, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
