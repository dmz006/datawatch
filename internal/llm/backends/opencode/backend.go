// Package opencode implements the LLM backend for opencode (https://github.com/sst/opencode).
package opencode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/scratch"
)

// standaloneFlagCache memoizes, per binary path, whether `<binary> run
// --help` advertises --standalone — opencode 2.x's `run` defaults to a
// shared per-machine background service unless told otherwise (2026-10-15
// live investigation), but datawatch doesn't bundle opencode: an operator
// may be on any version, including ones predating this flag, where `run`
// was already private by default. A capability probe (not a hardcoded
// version-number check) avoids guessing exactly which release introduced
// it and degrades safely to the old behavior when the flag doesn't exist.
var standaloneFlagCache sync.Map // binary string -> bool

func supportsStandaloneFlag(binary string) bool {
	if v, ok := standaloneFlagCache.Load(binary); ok {
		return v.(bool)
	}
	out, _ := exec.Command(binary, "run", "--help").CombinedOutput() //nolint:errcheck
	supported := strings.Contains(string(out), "--standalone")
	standaloneFlagCache.Store(binary, supported)
	return supported
}

// writeTaskFile writes task text to a scratch file instead of embedding it
// inline in the shell command sent to the pane.
//
// 2026-10-15 — operator-reported: the PWA session view for an opencode
// task showed screen after screen of the task spec's own text and no
// visible sign of real work. Root cause: Launch/LaunchResume used to
// build `opencode run '<entire task spec, verbatim>'` and deliver it via
// `tmux send-keys`, which types the whole command into the pty — bash's
// own terminal echo renders every character of a multi-KB task spec back
// into the pane before opencode's own output ever starts. Writing the
// text to a file and having the shell read it (`$(cat 'file')`) keeps
// the *typed* line short — the terminal only has to echo a few dozen
// characters — while opencode still receives the exact same content.
func writeTaskFile(tmuxSession, task string) (string, error) {
	f, err := os.CreateTemp("", "datawatch-opencode-task-"+strings.Map(func(r rune) rune {
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
	// tmux name so its end path releases any the run left behind.
	scratch.RecordPath(tmuxSession, f.Name())
	return f.Name(), nil
}

// Backend runs opencode in a tmux session.
type Backend struct{ binary string }

// New creates an opencode backend. binary defaults to "opencode".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "opencode"
	}
	return &Backend{binary: resolveBinary(binary)}
}

// resolveBinary returns the given binary if it's an absolute path or found in PATH,
// otherwise checks common install locations.
func resolveBinary(binary string) string {
	if filepath.IsAbs(binary) {
		return binary
	}
	if _, err := exec.LookPath(binary); err == nil {
		return binary
	}
	// Check common install locations
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".opencode", "bin", binary),
		filepath.Join(home, ".local", "bin", binary),
		filepath.Join("/usr/local/bin", binary),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return binary
}

func (b *Backend) Name() string                   { return "opencode" }
func (b *Backend) SupportsInteractiveInput() bool { return false }
func (b *Backend) PromptRequired() bool           { return false }
func (b *Backend) HasToolAccess() bool            { return true }

// PromptBackend runs opencode in single-prompt mode (-p). Always requires a task.
type PromptBackend struct{ binary string }

// NewPrompt creates an opencode-prompt backend that runs opencode -p.
func NewPrompt(binary string) llm.Backend {
	if binary == "" {
		binary = "opencode"
	}
	return &PromptBackend{binary: resolveBinary(binary)}
}

func (b *PromptBackend) Name() string                   { return "opencode-prompt" }
func (b *PromptBackend) SupportsInteractiveInput() bool { return false }
func (b *PromptBackend) PromptRequired() bool           { return true }
func (b *PromptBackend) HasToolAccess() bool            { return true }
func (b *PromptBackend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (b *PromptBackend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	if task == "" {
		return fmt.Errorf("opencode-prompt requires a task (prompt mode)")
	}
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	// Use --format json piped through a Python one-liner to extract readable text.
	// Default format writes directly to terminal with ANSI escape sequences that
	// get cleared, making output invisible in pipe-pane capture.
	// Task text goes through a scratch file, not inline — see
	// writeTaskFile's doc comment (same fix as Backend.buildRunCmd). The
	// cleanup (rm) has to happen AFTER the whole pipeline, not inside the
	// message-argument position — splicing a ';' in there would
	// prematurely terminate the 'opencode run | python3' pipe.
	msgExpr := ""
	cleanup := ""
	if path, err := writeTaskFile(tmuxSession, task); err == nil {
		escapedPath := strings.ReplaceAll(path, "'", `'\''`)
		msgExpr = fmt.Sprintf("\"$(cat '%s')\"", escapedPath)
		cleanup = fmt.Sprintf("rm -f '%s'; ", escapedPath)
	} else {
		escaped := strings.ReplaceAll(task, "'", `'\''`)
		msgExpr = fmt.Sprintf("'%s'", escaped)
	}
	// --standalone — see buildRunCmd's doc comment: opencode 2.x's `run`
	// otherwise shares one per-machine background service across
	// concurrent invocations instead of giving each task its own server.
	// Probed via supportsStandaloneFlag, not assumed.
	standaloneFlag := ""
	if supportsStandaloneFlag(b.binary) {
		standaloneFlag = "--standalone "
	}
	cmd := fmt.Sprintf(
		`cd '%s' && echo '[opencode-prompt] starting…' && %s run %s--format json %s 2>/dev/null `+
			`| python3 -c "import sys,json;[print(e['part']['text'],end='',flush=True) if e.get('type')=='text' else print('['+e['type']+']',flush=True) for line in sys.stdin for e in [json.loads(line)] if e.get('type') in ('text','step_start','step_finish','tool_call')]" `+
			`; %secho; sleep 1; echo 'DATAWATCH_COMPLETE: opencode done'`,
		escapedDir, b.binary, standaloneFlag, msgExpr, cleanup)
	return llm.SendKeys(ctx, tmuxSession, cmd)
}
func (b *Backend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildRunCmd builds the shell command to type into the tmux pane. When a
// task is given, its text is written to a scratch file rather than
// embedded inline — see writeTaskFile's doc comment for why — so the
// *typed* line (and therefore the terminal's own echo of it) stays short
// regardless of the task spec's real size. Falls back to the old inline
// form only if the file write itself fails.
func (b *Backend) buildRunCmd(escapedDir, task, tmuxSession string) string {
	if task == "" {
		return fmt.Sprintf("cd '%s' && %s", escapedDir, b.binary)
	}
	// Use non-interactive 'opencode run' mode when a task is provided (e.g. PRD
	// worker sessions). TUI send-keys delivery fails on task text containing '('
	// characters — opencode's eval treats the text as a shell command and errors.
	// --standalone (2026-10-15, live investigation) — opencode 2.x's `run`
	// defaults to routing through one shared per-machine background
	// service (`opencode serve --service`) instead of a private server per
	// invocation. Concurrent tasks would otherwise share that one process
	// instead of being independent. --standalone restores one private
	// server per task, matching this backend's actual isolation
	// assumption. `opencode serve --port N` (the ACP backend) is already
	// unaffected — serve always starts its own private server regardless.
	// Probed, not assumed (see supportsStandaloneFlag): older opencode
	// versions don't have this flag and were already private by default.
	standaloneFlag := ""
	if supportsStandaloneFlag(b.binary) {
		standaloneFlag = "--standalone "
	}
	if path, err := writeTaskFile(tmuxSession, task); err == nil {
		escapedPath := strings.ReplaceAll(path, "'", `'\''`)
		return fmt.Sprintf("cd '%s' && %s run %s\"$(cat '%s')\"; rm -f '%s'; echo 'DATAWATCH_COMPLETE: opencode done'",
			escapedDir, b.binary, standaloneFlag, escapedPath, escapedPath)
	}
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	return fmt.Sprintf("cd '%s' && %s run %s'%s'; echo 'DATAWATCH_COMPLETE: opencode done'",
		escapedDir, b.binary, standaloneFlag, escaped)
}

// Launch starts a fresh opencode session. The typed command is delivered
// via the shared llm.SendKeys helper: the shared MaxSendKeysCommand
// length guard applies and a tmux failure carries its stderr text in the
// returned error instead of a bare "exit status 1".
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	cmd := b.buildRunCmd(escapedDir, task, tmuxSession)
	return llm.SendKeys(ctx, tmuxSession, cmd)
}

// LaunchResume resumes a prior opencode session.
// Opencode session IDs use a "ses_" prefix; datawatch session IDs (e.g.
// "local-node-c905") are not valid opencode session IDs, so we fall
// back to a fresh launch (no -s flag).
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	cmd := b.buildRunCmd(escapedDir, task, tmuxSession)
	return llm.SendKeys(ctx, tmuxSession, cmd)
}
