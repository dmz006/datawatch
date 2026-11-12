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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/scratch"
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
	// Session-end ownership: record the task-capture file under the session's
	// tmux name so the session's end path releases it and the autonomous temp
	// sweep may only ever reach it as this session's own scratch.
	scratch.RecordPath(tmuxSession, f.Name())
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
	// Same ownership rule as writeTaskFile: one-shot scratch, released at
	// session end.
	scratch.RecordPath(tmuxSession, f.Name())
	return f.Name(), nil
}

// errNodeProvider means the pi provider table has no entry for the provider
// the model names, so a compute node cannot be bound to it. pi needs a full
// entry (api, key, models) for a provider; a bare baseUrl does not make one,
// so the launch refuses instead of guessing that entry.
var errNodeProvider = errors.New("pi provider table has no entry for the model's provider")

// nodeAgentDir writes the per-session pi config directory that points the
// model's provider at nodeAddr and returns its path. With no node address it
// returns "" and writes nothing, so the launch keeps pi's own configuration.
func nodeAgentDir(nodeAddr, model, tmuxSession string) (string, error) {
	if nodeAddr == "" {
		return "", nil
	}
	if strings.TrimSpace(model) == "" {
		return "", ErrNoModel
	}
	provider, _, hasProvider := strings.Cut(model, "/")
	if !hasProvider || provider == "" {
		return "", fmt.Errorf("model %q has no provider prefix to bind to node %q", model, nodeAddr)
	}
	source, err := agentSourceDir()
	if err != nil {
		return "", err
	}
	return writeNodeHostAgentDir(source, tmuxSession, provider, nodeBaseURL(nodeAddr))
}

// agentSourceDir is the pi config directory this daemon's own launches read:
// PI_CODING_AGENT_DIR when it is set, otherwise pi's documented default,
// ~/.pi/agent.
func agentSourceDir() (string, error) {
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "agent"), nil
}

// nodeBaseURL is the base URL a provider entry takes for a compute node: the
// address with an http:// scheme when it has none (as internal/server/api.go
// adds it), plus the /v1 suffix of an OpenAI-compatible endpoint (as the
// opencode provider block writes it).
func nodeBaseURL(nodeAddr string) string {
	if !strings.HasPrefix(nodeAddr, "http://") && !strings.HasPrefix(nodeAddr, "https://") {
		nodeAddr = "http://" + nodeAddr
	}
	return strings.TrimSuffix(nodeAddr, "/") + "/v1"
}

// writeNodeHostAgentDir creates the per-session pi config directory under the
// same scratch location as the task files. Every entry of sourceDir is linked
// in, so credentials and sessions stay the source's own, and models.json is
// the source's provider table with only the named provider's baseUrl changed.
func writeNodeHostAgentDir(sourceDir, tmuxSession, provider, baseURL string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(sourceDir, "models.json"))
	if err != nil {
		return "", fmt.Errorf("read pi provider table: %w", err)
	}
	table, err := nodeHostProviderTable(raw, provider, baseURL)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return "", fmt.Errorf("read pi agent directory: %w", err)
	}
	dir, err := os.MkdirTemp("", "datawatch-pi-agent-"+scratchNamePart(tmuxSession)+"-*")
	if err != nil {
		return "", err
	}
	// Session-end ownership: record the per-session worker config directory
	// the moment it exists (released at session end, sweep-covered after).
	scratch.RecordPath(tmuxSession, dir)
	for _, e := range entries {
		if e.Name() == "models.json" {
			continue
		}
		if err := os.Symlink(filepath.Join(sourceDir, e.Name()), filepath.Join(dir, e.Name())); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "models.json"), table, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// nodeHostProviderTable returns pi's provider table with the named provider's
// baseUrl replaced. Every other top-level key and every other provider keeps
// the value the source wrote. The provider must already exist in the source.
func nodeHostProviderTable(raw []byte, provider, baseURL string) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse pi provider table: %w", err)
	}
	providersRaw, ok := doc["providers"]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errNodeProvider, provider)
	}
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(providersRaw, &providers); err != nil {
		return nil, fmt.Errorf("parse pi provider table: %w", err)
	}
	entryRaw, ok := providers[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errNodeProvider, provider)
	}
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(entryRaw, &entry); err != nil || entry == nil {
		return nil, fmt.Errorf("pi provider %q is not an object", provider)
	}
	base, err := json.Marshal(baseURL)
	if err != nil {
		return nil, err
	}
	entry["baseUrl"] = base
	entryOut, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	providers[provider] = entryOut
	providersOut, err := json.Marshal(providers)
	if err != nil {
		return nil, err
	}
	doc["providers"] = providersOut
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// piInvocation prefixes the pi binary with the config-directory setting when a
// per-session agent directory is in use. The assignment applies to the pi
// process alone; nothing else in the shell sees it.
func piInvocation(binary, agentDir string) string {
	if agentDir == "" {
		return binary
	}
	return fmt.Sprintf("PI_CODING_AGENT_DIR='%s' %s", shellEscape(agentDir), binary)
}

// agentDirCleanup removes the per-session agent directory once pi has exited.
func agentDirCleanup(agentDir string) string {
	if agentDir == "" {
		return ""
	}
	return fmt.Sprintf("rm -rf '%s'; ", shellEscape(agentDir))
}

// sendLaunch builds one pi launch command and sends it. With a node address,
// the per-session agent directory is written first and its path is handed to
// build. The directory is removed again if the launch fails; once the command
// runs, its own cleanup removes it. Build errors carry label as a prefix.
func sendLaunch(ctx context.Context, label, tmuxSession, nodeAddr, model string, build func(agentDir string) (string, error)) error {
	agentDir, err := nodeAgentDir(nodeAddr, model, tmuxSession)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	cmd, err := build(agentDir)
	if err != nil {
		removeAgentDir(agentDir)
		return fmt.Errorf("%s: %w", label, err)
	}
	if err := llm.SendKeys(ctx, tmuxSession, cmd); err != nil {
		removeAgentDir(agentDir)
		return err
	}
	return nil
}

// removeAgentDir deletes a per-session agent directory; "" is a no-op.
func removeAgentDir(agentDir string) {
	if agentDir != "" {
		_ = os.RemoveAll(agentDir)
	}
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
	binary string
	model  string // e.g. "ollama/qwen3.8:27b"; empty uses pi's own default
	// thinkingLevel is the value for pi's own --thinking flag
	// ("" = don't pass the flag, pi picks its model default). Allowed:
	// off, minimal, low, medium, high — validated by
	// config.ValidatePiThinkingLevel at the REST/MCP/CLI write edges.
	thinkingLevel string
	sessionName   string
	nodeHost      string // compute node address; empty uses pi's own provider table
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

// SetNodeHost implements the session manager's duck-typed `interface{
// SetNodeHost(string) }` optional interface. nodeAddr is the session's compute
// node address, with or without a scheme. An empty address clears it, so the
// next launch uses pi's own provider table. The backend is shared across
// sessions, so the manager sets this on every launch.
func (b *Backend) SetNodeHost(nodeAddr string) { b.nodeHost = nodeAddr }

// SetThinkingLevel implements the daemon's duck-typed `interface{
// SetThinkingLevel(string) }` optional interface (see the pi-prompt
// registration in cmd/datawatch/main.go). "" = unset (pi's default).
func (b *Backend) SetThinkingLevel(level string) { b.thinkingLevel = level }

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
	return buildInteractiveCmdWithAgentDir(binary, model, "", sessionID, task, projectDir, tmuxSession, "")
}

// buildInteractiveCmdWithAgentDir is buildInteractiveCmd for a launch that may
// run pi on a per-session config directory (see nodeAgentDir). An empty
// agentDir gives exactly the command buildInteractiveCmd has always built.
func buildInteractiveCmdWithAgentDir(binary, model, thinkingLevel, sessionID, task, projectDir, tmuxSession, agentDir string) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", ErrNoModel
	}
	escapedDir := shellEscape(projectDir)
	modelFlag := fmt.Sprintf("--model '%s' ", shellEscape(model))
	thinkingFlag := ""
	if thinkingLevel != "" {
		// thinkingLevel is one of off|minimal|low|medium|high (validated at
		// the config write edges), safe to interpolate.
		thinkingFlag = fmt.Sprintf("--thinking %s ", thinkingLevel)
	}
	sessionFlag := ""
	if sessionID != "" {
		sessionFlag = fmt.Sprintf("--session-id '%s' ", shellEscape(sessionID))
	}
	argExpr, cleanup := taskArgExpr(tmuxSession, task)
	return fmt.Sprintf(`cd '%s' && %s --approve %s%s%s%s; %spi_status=$?; %secho; if [ "$pi_status" -eq 0 ]; then echo 'DATAWATCH_COMPLETE: pi session ended'; else echo "DATAWATCH_PI_FAILED: pi exited with status $pi_status"; fi; exit "$pi_status"`,
		escapedDir, piInvocation(binary, agentDir), modelFlag, thinkingFlag, sessionFlag, argExpr, cleanup, agentDirCleanup(agentDir)), nil
}

// Launch starts a fresh interactive pi session. If SetSessionName was
// called first, that name is used as pi's --session-id so a later
// LaunchResume with the same ID reattaches to it. The command is
// delivered via the shared llm.SendKeys helper, so the shared
// MaxSendKeysCommand length guard applies and a tmux failure surfaces
// its stderr text in the returned error.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	return sendLaunch(ctx, "pi interactive launch", tmuxSession, b.nodeHost, b.model, func(agentDir string) (string, error) {
		return buildInteractiveCmdWithAgentDir(b.binary, b.model, b.thinkingLevel, b.sessionName, task, projectDir, tmuxSession, agentDir)
	})
}

// LaunchResume implements llm.Resumable: resumes (or, per pi's own
// --session-id semantics, creates if missing) the given session ID.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	id := resumeID
	if id == "" {
		id = b.sessionName
	}
	return sendLaunch(ctx, "pi interactive resume", tmuxSession, b.nodeHost, b.model, func(agentDir string) (string, error) {
		return buildInteractiveCmdWithAgentDir(b.binary, b.model, b.thinkingLevel, id, task, projectDir, tmuxSession, agentDir)
	})
}

// PromptBackend runs pi in --print (non-interactive, run-to-completion)
// mode. Always requires a task.
type PromptBackend struct {
	binary string
	model  string
	// thinkingLevel — see Backend.thinkingLevel's doc comment.
	thinkingLevel string
	nodeHost      string // compute node address; empty uses pi's own provider table
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

// SetNodeHost implements the session manager's duck-typed `interface{
// SetNodeHost(string) }` optional interface, as Backend.SetNodeHost does.
func (b *PromptBackend) SetNodeHost(nodeAddr string) { b.nodeHost = nodeAddr }

// SetThinkingLevel implements the daemon's duck-typed `interface{
// SetThinkingLevel(string) }` optional interface: one-shot pi-prompt
// launches receive pi's own --thinking flag with this level.
// "" = unset (pi's default).
func (b *PromptBackend) SetThinkingLevel(level string) { b.thinkingLevel = level }

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
	return sendLaunch(ctx, "pi-prompt launch", tmuxSession, b.nodeHost, b.model, func(agentDir string) (string, error) {
		return buildLaunchCmdWithAgentDir(b.binary, b.model, b.thinkingLevel, task, projectDir, tmuxSession, agentDir)
	})
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
	return buildLaunchCmdWithAgentDir(binary, model, "", task, projectDir, tmuxSession, "")
}

// buildLaunchCmdWithAgentDir is buildLaunchCmd for a launch that may run pi on
// a per-session config directory (see nodeAgentDir). The directory is removed
// right after pi's scratch files, before the outcome checks, which exit. An
// empty agentDir gives exactly the command buildLaunchCmd has always built.
func buildLaunchCmdWithAgentDir(binary, model, thinkingLevel, task, projectDir, tmuxSession, agentDir string) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", ErrNoModel
	}
	escapedDir := shellEscape(projectDir)
	modelFlag := fmt.Sprintf("--model '%s' ", shellEscape(model))
	outPath, errOut := reserveScratchFile(tmuxSession, "out")
	statusPath, errStatus := reserveScratchFile(tmuxSession, "status")
	if errOut != nil || errStatus != nil {
		return agentDirCleanup(agentDir) + "echo 'DATAWATCH_PI_FAILED: could not create scratch file for pi output capture'; exit 1", nil
	}
	thinkingFlag := ""
	if thinkingLevel != "" {
		// thinkingLevel is one of off|minimal|low|medium|high (validated at
		// the config write edges), safe to interpolate.
		thinkingFlag = fmt.Sprintf("--thinking %s ", thinkingLevel)
	}
	argExpr, cleanup := taskArgExpr(tmuxSession, task)
	escapedOut := shellEscape(outPath)
	escapedStatus := shellEscape(statusPath)
	return fmt.Sprintf("cd '%s' && { %s --print --mode text --approve %s%s%s; echo $? > '%s'; } | tee '%s'; "+
		"read dw_status < '%s'; [ -s '%s' ]; dw_has_output=$?; %srm -f '%s' '%s'; %s"+
		"if [ \"${dw_status:-1}\" -ne 0 ]; then printf 'DATAWATCH_PI_FAILED: pi exited with status %%s (model %%s)\\n' \"${dw_status:-1}\" '%s'; exit \"${dw_status:-1}\"; fi; "+
		"if [ \"$dw_has_output\" -ne 0 ]; then echo 'DATAWATCH_PI_FAILED: pi produced no output'; exit 1; fi; "+
		"echo 'DATAWATCH_COMPLETE: pi done'",
		escapedDir, piInvocation(binary, agentDir), modelFlag, thinkingFlag, argExpr, escapedStatus, escapedOut,
		escapedStatus, escapedOut, cleanup, escapedOut, escapedStatus, agentDirCleanup(agentDir), shellEscape(model)), nil
}
