// Package claudecode implements the LLM backend for Anthropic's claude-code CLI.
package claudecode

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/gitguard"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/scratch"
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
	// ("claude-sonnet-5"). Empty = claude default.
	model string
	// effort (v5.27.5) — passed to claude as `--effort <value>`.
	// One of: low | medium | high | xhigh | max. Empty = default.
	effort string
	// chrome (v8.8.3) — when non-nil, passes --chrome (true) or
	// --no-chrome (false) to claude at launch. Nil = omit flag.
	chrome *bool
	// excludeAncestorClaudeMd — when true, Launch/LaunchResume pass
	// `--settings {"claudeMdExcludes":[...]}` listing every CLAUDE.md in an
	// ancestor of the project directory, so autonomous workers don't read
	// other sessions' Session:/Task: guardrail lines. Off by default.
	excludeAncestorClaudeMd bool
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

// SetExcludeAncestorClaudeMd — opt-in: hide ancestor-directory CLAUDE.md files
// (never the project's own or ~/.claude/CLAUDE.md) from the launched claude.
// Set explicitly on every launch: the backend is a shared singleton.
func (b *Backend) SetExcludeAncestorClaudeMd(on bool) { b.excludeAncestorClaudeMd = on }

// ancestorClaudeMds returns the absolute path of every existing CLAUDE.md in
// a strict ancestor of projectDir, nearest parent first. projectDir's own
// CLAUDE.md and anything beneath it are never included.
func ancestorClaudeMds(projectDir string) []string {
	dir := filepath.Clean(projectDir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	var out []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		p, ok := claudeMdIn(dir)
		if !ok {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// claudeMdIn returns the path of the CLAUDE.md entry directly inside dir, and
// false when that entry is not contained in dir. gitguard.ContainedEntry does
// the symlink-aware check; the prefix test on the value that is returned is
// repeated here, behind an early return, so the one path handed to os.Stat is
// the one that was checked. A symlinked CLAUDE.md is accepted as the link
// itself, wherever it points.
func claudeMdIn(dir string) (string, bool) {
	entry, err := gitguard.ContainedEntry(dir, filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		return "", false
	}
	sep := string(filepath.Separator)
	dirPrefix := strings.TrimSuffix(filepath.Clean(dir), sep) + sep
	if !strings.HasPrefix(entry, dirPrefix) {
		return "", false
	}
	return filepath.Clean(entry), true
}

// settingsFlag returns the ` --settings '<json>'` flag excluding ancestor
// CLAUDE.md files for projectDir, or "" when disabled or none exist.
func (b *Backend) settingsFlag(projectDir string) string {
	if !b.excludeAncestorClaudeMd {
		return ""
	}
	paths := ancestorClaudeMds(projectDir)
	if len(paths) == 0 {
		return ""
	}
	js, err := json.Marshal(map[string][]string{"claudeMdExcludes": paths})
	if err != nil {
		return ""
	}
	return " --settings " + shellQuote(string(js))
}

// SetChrome (v8.8.3) — passes --chrome or --no-chrome to claude at launch.
// Nil means neither flag is passed (let claude use its default).
func (b *Backend) SetChrome(enabled bool) { b.chrome = &enabled }

func (b *Backend) Name() string                   { return "claude-code" }
func (b *Backend) SupportsInteractiveInput() bool { return true }
func (b *Backend) HasToolAccess() bool            { return true }

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
		// --channels pre-selects option 1 ("I am using this for local development")
		// in the consent dialog that --dangerously-load-development-channels shows.
		// Both flags are variadic and must come before --add-dir.
		flags += " --dangerously-load-development-channels server:" + channelName +
			" --channels server:" + channelName
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
	if b.chrome != nil {
		if *b.chrome {
			flags += " --chrome"
		} else {
			flags += " --no-chrome"
		}
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
	post := b.postFlagsStr() + b.settingsFlag(projectDir)

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
		// Task is delivered via a scratch file ($(cat 'path')) so the typed
		// line stays tmux-send-keys-friendly; rm -f cleanup runs right
		// before the completion marker. See taskArgExpr.
		argExpr, cleanup := taskArgExpr(tmuxSession, task)
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s %s; %secho 'DATAWATCH_COMPLETE: claude done'",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post, argExpr, cleanup)
	}

	if err := llm.SendKeys(ctx, tmuxSession, cmd); err != nil {
		return fmt.Errorf("launch claude-code in %s: %w", tmuxSession, err)
	}
	return nil
}

// ResumeKeepsTask implements llm.ResumeKeepsTask: a resumed claude-code
// conversation already holds the original task in its context, so a
// no-override restart should not resend the full task text.
func (b *Backend) ResumeKeepsTask() bool { return true }

// LaunchResume resumes a prior claude-code conversation using --resume SESSION_ID.
// If resumeID is not a UUID, it's treated as a session name and the deterministic
// UUID is derived from it.
// If --resume fails (e.g. conversation not found for pre-fix sessions), falls back
// to a fresh launch with --session-id so future restarts will work.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	channelName := sessionChannelName(tmuxSession)
	pre := b.preFlagsStr(channelName)
	post := b.postFlagsStr() + b.settingsFlag(projectDir)

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

	// One task-delivery expression (the scratch-file $(cat 'path') from
	// taskArgExpr, or empty when task is "") is reused on both the
	// --resume and the --session-id fallback halves, with a single rm -f
	// cleanup after the fallback — so the typed line carries only the
	// short scratch-file path rather than the task text twice.
	argExpr, cleanup := taskArgExpr(tmuxSession, task)
	resumeCmd := fmt.Sprintf("%s %s --resume %s %s", base, claudeBase, quotedUUID, argExpr)
	fallbackCmd := fmt.Sprintf("%s %s --session-id %s %s", base, claudeBase, quotedUUID, argExpr)

	cmd := fmt.Sprintf("%s || %s; %secho 'DATAWATCH_COMPLETE: claude done'", resumeCmd, fallbackCmd, cleanup)
	if err := llm.SendKeys(ctx, tmuxSession, cmd); err != nil {
		return fmt.Errorf("resume claude-code in %s: %w", tmuxSession, err)
	}
	return nil
}

// writeTaskFile writes task text to a scratch file instead of embedding it
// inline in the shell command sent to the pane. Mirrors the pi backend's
// writeTaskFile for the same reason: typing a multi-KB task spec inline via
// `tmux send-keys` hits tmux's "command too long" limit and makes the
// terminal echo the whole thing back into the pane before the agent's own
// output starts. Reading it back with `$(cat 'file')` keeps the *typed* line
// short while claude still receives the exact same content.
func writeTaskFile(tmuxSession, task string) (string, error) {
	f, err := os.CreateTemp("", "datawatch-claudecode-task-"+strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, tmuxSession)+"-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.WriteString(task); err != nil {
		os.Remove(f.Name()) //nolint:errcheck
		return "", err
	}
	// Session-end ownership: record the task-capture file under the session's
	// tmux name so its end path releases any that the run left behind (the
	// launch command's own rm -f already handles the normal case).
	scratch.RecordPath(tmuxSession, f.Name())
	return f.Name(), nil
}

// taskArgExpr returns the shell expression delivering task text as a quoted
// positional argument — as a single-quoted command substitution of
// cat reading the scratch file, `"$(cat 'path')"` — plus the rm -f cleanup to
// append after the main command. Empty task yields empty strings so callers
// build byte-identical commands to the pre-file-delivery behavior.
func taskArgExpr(tmuxSession, task string) (argExpr, cleanup string) {
	if task == "" {
		return "", ""
	}
	if path, err := writeTaskFile(tmuxSession, task); err == nil {
		quoted := shellQuote(path)
		return fmt.Sprintf("$(cat %s)", quoted), fmt.Sprintf("rm -f %s; ", quoted)
	}
	return "'" + escapeForShell(task) + "'", ""
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
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}

// escapeForShell shell-escapes s for use inside single quotes
// (fallback path in taskArgExpr when the scratch file cannot be written).
func escapeForShell(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
