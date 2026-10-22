// Package pi implements the LLM backend for pi (https://pi.dev,
// github.com/earendil-works/pi), a minimal open-source coding agent harness.
//
// Two variants are registered, mirroring the opencode/opencode-prompt and
// goose/goose-prompt split already used elsewhere in this codebase:
//
//   - Backend ("pi"): interactive mode — `pi` with no --print. The initial
//     task is delivered as pi's own positional message argument, after
//     which the session stays open for follow-up input/approval, same as
//     claude-code/goose interactive sessions. SupportsInteractiveInput is
//     true; session resume uses pi's own --session-id (create-if-missing),
//     which sidesteps the ID-format mismatch opencode's resume has to work
//     around (opencode's "ses_"-prefixed IDs vs datawatch's own session
//     IDs) — pi accepts an arbitrary caller-supplied ID directly.
//   - PromptBackend ("pi-prompt"): one-shot, non-interactive `pi --print`.
//     Kept as simple as aider's backend: one blocking CLI call that only
//     returns once the agent is genuinely done, followed by a shell-level
//     outcome report: "DATAWATCH_COMPLETE:" on a clean run with output,
//     "DATAWATCH_PI_FAILED:" otherwise. This sidesteps
//     the class of bug found live 2026-10-16 in the opencode-acp backend
//     (completion detection racing against an async SSE/event stream) —
//     there is no async completion signal to race against here, only "did
//     the blocking command return".
//
// A third variant, an RPC-protocol backend analogous to opencode-acp (pi
// supports `--mode rpc`), is deliberately not built yet — see BL466 — since
// it would reintroduce the exact async-completion-detection race class just
// fixed in opencode-acp. Planned, not implemented.
package pi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// ErrNoModel — a pi-family launch was built with no model configured.
//
// Since v9.10.19 every datawatch launch path that can land on a pi-family
// backend must resolve an explicit, non-empty model first (task/story/PRD
// pin, or the autonomous config tier for that session type — planning,
// verification, guardrail — see internal/autonomous/pinned_model.go), or
// fail loudly at that layer. This error is the hard stop when a path still
// manages to ship an empty model: omitting --model would make the pi
// harness silently run on the host's own default model — on a federated
// peer that is the operator-local node model the session exists to
// replace.
var ErrNoModel = errors.New(
	"pi-family backend received an empty model at launch: refusing to omit --model and run on this host's built-in default model — " +
		"set the session's model (task/story/PRD) or the autonomous config tier for this session type")

// writeTaskFile writes task text to a scratch file instead of embedding it
// inline in the shell command sent to the pane. Mirrors
// opencode.writeTaskFile exactly, and for the same reason: typing a
// multi-KB task spec inline via `tmux send-keys` makes the terminal echo
// the whole thing back into the pane before the agent's own output starts.
// Reading it back with `$(cat 'file')` keeps the *typed* line short while
// the agent still receives the exact same content.
func writeTaskFile(tmuxSession, task string) (string, error) {
	f, err := os.CreateTemp("", "datawatch-pi-task-"+scratchNamePart(tmuxSession)+"-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.WriteString(task); err != nil {
		os.Remove(f.Name()) //nolint:errcheck
		return "", err
	}
	return f.Name(), nil
}

// scratchNamePart reduces a tmux session name to characters safe in a
// scratch file name.
func scratchNamePart(tmuxSession string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, tmuxSession)
}

// reserveScratchFile creates an empty, uniquely named scratch file and
// returns its path. Like writeTaskFile, only this short path goes into the
// shell command; the command writes to the file at run time, so the typed
// command stays under the send-keys length limit.
func reserveScratchFile(tmuxSession, purpose string) (string, error) {
	f, err := os.CreateTemp("", "datawatch-pi-"+purpose+"-"+scratchNamePart(tmuxSession)+"-*")
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	return f.Name(), nil
}

func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// taskArgExpr returns the shell expression delivering task text as a
// trailing positional argument, plus any cleanup command to append after
// the main command. Shared by both variants.
func taskArgExpr(tmuxSession, task string) (argExpr, cleanup string) {
	if task == "" {
		return "", ""
	}
	if path, err := writeTaskFile(tmuxSession, task); err == nil {
		escapedPath := shellEscape(path)
		return fmt.Sprintf(" \"$(cat '%s')\"", escapedPath), fmt.Sprintf("rm -f '%s'; ", escapedPath)
	}
	return fmt.Sprintf(" '%s'", shellEscape(task)), ""
}

func versionOf(binary string) string {
	out, err := exec.Command(binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Backend runs pi in interactive mode (no --print): the initial task is
// sent as pi's first turn and the session stays open afterward for
// follow-up input, same shape as claude-code/goose interactive sessions.
type Backend struct {
	binary      string
	model       string // e.g. "ollama/qwen3.8:27b"; empty uses pi's own default
	sessionName string
}

// New creates an interactive pi backend. binary defaults to "pi".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "pi"
	}
	return &Backend{binary: binary}
}

func (b *Backend) Name() string                   { return "pi" }
func (b *Backend) SupportsInteractiveInput() bool { return true }
func (b *Backend) HasToolAccess() bool            { return true }
func (b *Backend) PromptRequired() bool           { return false }

// SetModel implements the session manager's duck-typed `interface{
// SetModel(string) }` optional interface (see internal/session/manager.go).
// Expects pi's own "provider/model" form, e.g. "ollama/qwen3.8:27b".
func (b *Backend) SetModel(model string) { b.model = model }

// SetSessionName implements llm.Nameable. Stored verbatim and passed to pi
// as --session-id, which both creates and resumes a session by an
// arbitrary caller-supplied ID — unlike opencode's own "ses_"-prefixed
// IDs, no ID-format translation is needed.
func (b *Backend) SetSessionName(name string) { b.sessionName = name }

func (b *Backend) Version() string { return versionOf(b.binary) }

// buildInteractiveCmd builds the shell command string sent to tmux for
// both a fresh launch and a resume — the only difference is which session
// ID (if any) accompanies --session-id. Pulled out of Launch/LaunchResume
// so the quoting/flag logic is unit-testable without tmux or the pi
// binary.
//
// Since v9.10.19 model is required: an interactive pi session without an
// explicit --model runs on the host's own default model, which is exactly
// the fallback every launch path must not take (ErrNoModel).
//
// BL468 — a failed interactive run must not look complete: the command
// captures pi's exit status, prints DATAWATCH_COMPLETE only on a zero exit,
// prints the distinct DATAWATCH_PI_FAILED line with the exit status on a
// non-zero exit, and exits with that status. The format string is a raw
// literal so "$pi_status" keeps real double quotes verbatim (the
// acceptance gate matches it as a fixed string).
func buildInteractiveCmd(binary, model, sessionID, task, projectDir, tmuxSession string) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", ErrNoModel
	}
	escapedDir := shellEscape(projectDir)
	modelFlag := fmt.Sprintf("--model '%s' ", shellEscape(model))
	sessionFlag := ""
	if sessionID != "" {
		sessionFlag = fmt.Sprintf("--session-id '%s' ", shellEscape(sessionID))
	}
	argExpr, cleanup := taskArgExpr(tmuxSession, task)
	return fmt.Sprintf(`cd '%s' && %s --approve %s%s%s; %spi_status=$?; echo; if [ "$pi_status" -eq 0 ]; then echo 'DATAWATCH_COMPLETE: pi session ended'; else echo "DATAWATCH_PI_FAILED: pi exited with status $pi_status"; fi; exit "$pi_status"`,
		escapedDir, binary, modelFlag, sessionFlag, argExpr, cleanup), nil
}

// Launch starts a fresh interactive pi session. If SetSessionName was
// called first, that name is used as pi's --session-id so a later
// LaunchResume with the same ID reattaches to it. The command is
// delivered via the shared llm.SendKeys helper, so the shared
// MaxSendKeysCommand length guard applies and a tmux failure surfaces
// its stderr text in the returned error.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	cmd, err := buildInteractiveCmd(b.binary, b.model, b.sessionName, task, projectDir, tmuxSession)
	if err != nil {
		return fmt.Errorf("pi interactive launch: %w", err)
	}
	return llm.SendKeys(ctx, tmuxSession, cmd)
}

// LaunchResume implements llm.Resumable: resumes (or, per pi's own
// --session-id semantics, creates if missing) the given session ID.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	id := resumeID
	if id == "" {
		id = b.sessionName
	}
	cmd, err := buildInteractiveCmd(b.binary, b.model, id, task, projectDir, tmuxSession)
	if err != nil {
		return fmt.Errorf("pi interactive resume: %w", err)
	}
	return llm.SendKeys(ctx, tmuxSession, cmd)
}

// PromptBackend runs pi in --print (non-interactive, run-to-completion)
// mode. Always requires a task.
type PromptBackend struct {
	binary string
	model  string
}

// NewPrompt creates a pi-prompt backend for one-shot task execution.
// binary defaults to "pi".
func NewPrompt(binary string) llm.Backend {
	if binary == "" {
		binary = "pi"
	}
	return &PromptBackend{binary: binary}
}

func (b *PromptBackend) Name() string                   { return "pi-prompt" }
func (b *PromptBackend) SupportsInteractiveInput() bool { return false }
func (b *PromptBackend) HasToolAccess() bool            { return true }
func (b *PromptBackend) PromptRequired() bool           { return true }

// SetModel implements the session manager's duck-typed `interface{
// SetModel(string) }` optional interface, same mechanism opencode-acp and
// goose use for a per-task/story/PRD model override.
func (b *PromptBackend) SetModel(model string) { b.model = model }

func (b *PromptBackend) Version() string { return versionOf(b.binary) }

// Launch runs `pi --print ... '<task>'` as a single blocking command inside
// the tmux session. Once it returns, the command reports the outcome: a
// non-zero exit or empty output prints a DATAWATCH_PI_FAILED line and exits
// non-zero, and only a clean run with output prints DATAWATCH_COMPLETE. --mode
// text keeps the pane human-readable (session-stream visibility) rather than
// raw JSON lines; --approve trusts project-local pi config in the worktree
// non-interactively (there is no human to answer an approval prompt in a
// one-shot task).
func (b *PromptBackend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	cmd, err := buildLaunchCmd(b.binary, b.model, task, projectDir, tmuxSession)
	if err != nil {
		return fmt.Errorf("pi-prompt launch: %w", err)
	}
	return llm.SendKeys(ctx, tmuxSession, cmd)
}

// buildLaunchCmd builds the shell command string sent to tmux. Pulled out
// of Launch so the quoting/flag logic is unit-testable without spawning a
// real tmux session or the pi binary. Task text goes through a scratch
// file, not inline — see taskArgExpr's doc comment: a multi-KB task spec
// (the session manager wraps every task with checkpoint/completion
// framing, so even a short task spec arrives here multi-KB) typed inline
// via tmux send-keys has its embedded newlines submitted as literal Enter
// keypresses mid-command, corrupting the shell command — confirmed live
// 2026-10-16 ("These: command not found" mid-paste) before this fix.
//
// Outcome reporting: pi's stdout is teed into an output scratch file and its
// exit status is written to a status scratch file, so the command can tell
// a clean run from a failed one after pi returns. A non-zero status prints
// DATAWATCH_PI_FAILED with the status and model, then exits with that
// status. A zero status with an empty output file prints DATAWATCH_PI_FAILED
// too. Only a clean run with output reaches DATAWATCH_COMPLETE. The failure
// path exits the shell, so the scratch files are removed before the checks.
//
// Since v9.10.19 model is required: a pi-prompt session without an explicit
// --model would run on the host's own default model, which is exactly the
// fallback every launch path must not take (ErrNoModel). The
// DATAWATCH_PI_FAILED wording for other failures is unchanged.
func buildLaunchCmd(binary, model, task, projectDir, tmuxSession string) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", ErrNoModel
	}
	escapedDir := shellEscape(projectDir)
	modelFlag := fmt.Sprintf("--model '%s' ", shellEscape(model))
	outPath, errOut := reserveScratchFile(tmuxSession, "out")
	statusPath, errStatus := reserveScratchFile(tmuxSession, "status")
	if errOut != nil || errStatus != nil {
		return "echo 'DATAWATCH_PI_FAILED: could not create scratch file for pi output capture'; exit 1", nil
	}
	argExpr, cleanup := taskArgExpr(tmuxSession, task)
	escapedOut := shellEscape(outPath)
	escapedStatus := shellEscape(statusPath)
	return fmt.Sprintf("cd '%s' && { %s --print --mode text --approve %s%s; echo $? > '%s'; } | tee '%s'; "+
		"read dw_status < '%s'; [ -s '%s' ]; dw_has_output=$?; %srm -f '%s' '%s'; "+
		"if [ \"${dw_status:-1}\" -ne 0 ]; then printf 'DATAWATCH_PI_FAILED: pi exited with status %%s (model %%s)\\n' \"${dw_status:-1}\" '%s'; exit \"${dw_status:-1}\"; fi; "+
		"if [ \"$dw_has_output\" -ne 0 ]; then echo 'DATAWATCH_PI_FAILED: pi produced no output'; exit 1; fi; "+
		"echo 'DATAWATCH_COMPLETE: pi done'",
		escapedDir, binary, modelFlag, argExpr, escapedStatus, escapedOut,
		escapedStatus, escapedOut, cleanup, escapedOut, escapedStatus, shellEscape(model)), nil
}
