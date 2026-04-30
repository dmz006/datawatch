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
	// permissionMode (v5.27.5) — passed to claude as
	// `--permission-mode <value>`. Empty = let claude pick its
	// default. The "plan" mode is the design-without-writing
	// flavour useful for PRD decomposition + design-review
	// sessions. Mutually exclusive with skipPermissions: when
	// permissionMode is non-empty, --dangerously-skip-permissions
	// is suppressed (the operator's explicit mode wins).
	permissionMode string
	// model (v5.27.5) — passed to claude as `--model <value>`.
	// Either an alias ("sonnet", "opus", "haiku") or a full name
	// ("claude-sonnet-4-6"). Empty = claude default.
	model string
	// effort (v5.27.5) — passed to claude as `--effort <value>`.
	// One of: low | medium | high | xhigh | max. Empty = default.
	effort string
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

// SetPermissionMode (v5.27.5) — operator override forwarded to
// `--permission-mode` at launch. Caller's responsibility to validate
// the value against claude's known modes; an unknown value will
// just make claude error out at launch with its own message.
func (b *Backend) SetPermissionMode(mode string) { b.permissionMode = mode }

// SetModel (v5.27.5) — alias or full name forwarded to `--model`.
func (b *Backend) SetModel(model string) { b.model = model }

// SetEffort (v5.27.5) — effort level forwarded to `--effort`.
func (b *Backend) SetEffort(effort string) { b.effort = effort }

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
	// v5.27.5 — explicit permission mode wins over the legacy
	// --dangerously-skip-permissions shortcut. Operators who
	// configure both intend the explicit mode (e.g. "plan" for
	// PRD design sessions) — silently dropping skipPermissions
	// avoids the conflict claude would otherwise complain about.
	switch {
	case b.permissionMode != "":
		flags += " --permission-mode " + shellQuote(b.permissionMode)
	case b.skipPermissions:
		flags += " --dangerously-skip-permissions"
	}
	if b.model != "" {
		flags += " --model " + shellQuote(b.model)
	}
	if b.effort != "" {
		flags += " --effort " + shellQuote(b.effort)
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

	// Derive a deterministic session ID from the tmux session name so that
	// LaunchResume can later --resume the same conversation by deriving the
	// same UUID from the datawatch full_id (tmuxSession minus the "cs-" prefix).
	fullID := strings.TrimPrefix(tmuxSession, "cs-")
	if fullID != "" {
		sessionUUID := deriveSessionUUID(fullID)
		post += " --session-id " + shellQuote(sessionUUID)
	}

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
// If --resume fails (e.g. conversation not found for pre-fix sessions), falls back
// to a fresh launch with --session-id so future restarts will work.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	channelName := sessionChannelName(tmuxSession)
	pre := b.preFlagsStr(channelName)
	post := b.postFlagsStr()

	// If resumeID is not a UUID, derive the deterministic UUID from the name
	actualResumeID := resumeID
	if !isUUID(resumeID) {
		actualResumeID = deriveSessionUUID(resumeID)
	}

	// Build resume command and fresh-launch fallback.
	// If --resume exits non-zero (conversation not found), fall back to a fresh
	// launch with --session-id so the deterministic UUID is established for
	// future restarts.
	quotedDir := shellQuote(projectDir)
	quotedUUID := shellQuote(actualResumeID)
	base := fmt.Sprintf("cd %s && NO_COLOR=1", quotedDir)
	claudeBase := fmt.Sprintf("%s%s --add-dir %s%s", b.binaryPath, pre, quotedDir, post)

	var resumeCmd, fallbackCmd string
	if task == "" {
		resumeCmd = fmt.Sprintf("%s %s --resume %s", base, claudeBase, quotedUUID)
		fallbackCmd = fmt.Sprintf("%s %s --session-id %s", base, claudeBase, quotedUUID)
	} else {
		escaped := escapeForShell(task)
		resumeCmd = fmt.Sprintf("%s %s --resume %s '%s'", base, claudeBase, quotedUUID, escaped)
		fallbackCmd = fmt.Sprintf("%s %s --session-id %s '%s'", base, claudeBase, quotedUUID, escaped)
	}

	cmd := fmt.Sprintf("%s || %s; echo 'DATAWATCH_COMPLETE: claude done'", resumeCmd, fallbackCmd)
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
