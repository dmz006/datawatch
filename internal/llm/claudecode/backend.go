// Package claudecode implements the LLM backend for Anthropic's claude-code CLI.
package claudecode

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

func init() {
	llm.Register(New("claude"))
}

// Backend runs claude-code in a tmux session.
type Backend struct {
	binaryPath      string
	skipPermissions bool   // pass --dangerously-skip-permissions
	channelEnabled  bool   // pass --channels server:datawatch --dangerously-load-development-channels
	sessionName     string // optional display name (--name flag)
}

// New creates a claude-code backend. binaryPath defaults to "claude".
func New(binaryPath string) llm.Backend {
	if binaryPath == "" {
		binaryPath = "claude"
	}
	return &Backend{binaryPath: binaryPath}
}

// NewWithOptions creates a claude-code backend with options.
func NewWithOptions(binaryPath string, skipPermissions bool, channelEnabled bool) llm.Backend {
	if binaryPath == "" {
		binaryPath = "claude"
	}
	return &Backend{binaryPath: binaryPath, skipPermissions: skipPermissions, channelEnabled: channelEnabled}
}

func (b *Backend) Name() string                  { return "claude-code" }
func (b *Backend) SupportsInteractiveInput() bool { return true }

// SetSessionName sets a display name for the Claude session (--name flag).
func (b *Backend) SetSessionName(name string) { b.sessionName = name }

// deriveSessionUUID generates a deterministic UUID v5 from a session name.
// This allows resuming by the same UUID without needing to capture it.
func deriveSessionUUID(name string) string {
	// UUID v5 namespace (URL namespace from RFC 4122)
	ns := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	h := sha1.New()
	// Parse namespace UUID bytes
	nsClean := strings.ReplaceAll(ns, "-", "")
	nsBytes, _ := hex.DecodeString(nsClean)
	h.Write(nsBytes)
	h.Write([]byte(name))
	sum := h.Sum(nil)
	// Set version 5 and variant bits
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func (b *Backend) Version() string {
	out, err := exec.Command(b.binaryPath, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Launch sends the claude command into the tmux session, running in projectDir.
// It uses --add-dir to grant claude-code permission to the project directory.
// Set NO_COLOR=1 so output is clean text without ANSI escape sequences.
// When task is empty, claude is started in interactive mode (no task argument).
// When task is provided, it is passed as the initial prompt.
// preFlagsStr returns flags that must appear BEFORE --add-dir (variadic flags like --channels).
// channelName overrides the default "datawatch" channel server name (for per-session channels).
func (b *Backend) preFlagsStr(channelName string) string {
	var flags string
	if b.channelEnabled {
		if channelName == "" {
			channelName = "datawatch"
		}
		// --dangerously-load-development-channels is variadic; it must come before --add-dir
		// so --add-dir terminates the variadic argument list.
		flags += " --dangerously-load-development-channels server:" + channelName
	}
	return flags
}

// postFlagsStr returns flags that go after --add-dir.
func (b *Backend) postFlagsStr() string {
	var flags string
	if b.skipPermissions {
		flags += " --dangerously-skip-permissions"
	}
	if b.sessionName != "" {
		flags += " --name " + shellQuote(b.sessionName)
	}
	return flags
}

// sessionChannelName derives the per-session MCP channel name from the tmux session name.
// tmuxSession is "cs-{hostname}-{sessionID}" → channel name is "datawatch-{hostname}-{sessionID}".
func sessionChannelName(tmuxSession string) string {
	// Strip the "cs-" prefix to get the full session ID
	fullID := strings.TrimPrefix(tmuxSession, "cs-")
	if fullID == "" {
		return "datawatch"
	}
	return "datawatch-" + fullID
}

func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	channelName := sessionChannelName(tmuxSession)
	pre := b.preFlagsStr(channelName)
	post := b.postFlagsStr()

	var cmd string
	if task == "" || b.channelEnabled {
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s; echo 'DATAWATCH_COMPLETE: claude done'",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post)
	} else {
		escaped := escapeForShell(task)
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s '%s'; echo 'DATAWATCH_COMPLETE: claude done'",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post, escaped)
	}

	err := exec.CommandContext(ctx,
		"tmux", "send-keys", "-t", tmuxSession, cmd, "Enter",
	).Run()
	if err != nil {
		return fmt.Errorf("launch claude-code in %s: %w", tmuxSession, err)
	}
	return nil
}

// LaunchResume resumes a prior claude-code conversation using --resume SESSION_ID.
// If resumeID is not a UUID, it's treated as a session name and the deterministic
// UUID is derived from it.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	channelName := sessionChannelName(tmuxSession)
	pre := b.preFlagsStr(channelName)
	post := b.postFlagsStr()

	// If resumeID is not a UUID, derive the deterministic UUID from the name
	actualResumeID := resumeID
	if !isUUID(resumeID) {
		actualResumeID = deriveSessionUUID(resumeID)
	}

	var cmd string
	if task == "" {
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s --resume %s; echo 'DATAWATCH_COMPLETE: claude done'",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post,
			shellQuote(actualResumeID))
	} else {
		escaped := escapeForShell(task)
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s --resume %s '%s'; echo 'DATAWATCH_COMPLETE: claude done'",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post,
			shellQuote(actualResumeID), escaped)
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

// isUUID checks if a string looks like a UUID (8-4-4-4-12 hex pattern).
func isUUID(s string) bool {
	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		return false
	}
	expected := []int{8, 4, 4, 4, 12}
	for i, p := range parts {
		if len(p) != expected[i] {
			return false
		}
		for _, c := range p {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func escapeForShell(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
