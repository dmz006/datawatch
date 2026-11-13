// Script gate runner: executes a project gate check (a verification script
// under .datawatch/verify, or the project's configured gate command) and
// reports whether it passed. It is the one place a gate process is launched,
// so the timeout, process-group kill and output cap behave the same way for
// every caller.
//
// Trust boundary: a script path comes from LLM-written Automaton state, so it
// goes through the same checks as runVerificationScriptCapped and must also
// resolve inside the project's .datawatch/verify folder. A gate command is
// operator configuration (project_gate_command) and runs as configured.
package autonomous

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gateVerifyFolder is the project-relative folder that holds gate scripts.
const gateVerifyFolder = ".datawatch/verify"

// gateWaitDelay bounds how long a gate waits for its output pipes to close
// after the shell has exited. A grandchild that escaped the process-group
// kill must not hold the gate open past its limit.
const gateWaitDelay = 2 * time.Second

// GateCheckKind names how a gate check is executed.
type GateCheckKind string

const (
	// GateCheckScript runs a script under .datawatch/verify with bash.
	GateCheckScript GateCheckKind = "script"
	// GateCheckCommand runs the project gate command with sh -c.
	GateCheckCommand GateCheckKind = "command"
)

// GateCheck is one runnable gate. Script is used for GateCheckScript and
// Command for GateCheckCommand; the other field is ignored.
type GateCheck struct {
	Kind    GateCheckKind
	Script  string
	Command string
}

// GateResult is the outcome of one gate run. Passed is true only for a clean
// exit 0 within the limit. Reason is empty on a pass and names the cause on
// every failure.
type GateResult struct {
	Passed   bool
	ExitCode int
	TimedOut bool
	Output   string
	Reason   string
}

// RunScriptGate runs check with projectDir as the working directory. A
// timeout of 0 means no cap. On timeout the whole process group is killed,
// so no child of the gate outlives it.
func RunScriptGate(ctx context.Context, check GateCheck, projectDir string, timeout time.Duration) GateResult {
	argv, failure := gateArgv(check, projectDir)
	if failure != "" {
		return GateResult{ExitCode: -1, Reason: failure}
	}

	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...) // #nosec G204 -- argv built by gateArgv from validated input
	cmd.Dir = projectDir
	configureGateProcessGroup(cmd)
	cmd.WaitDelay = gateWaitDelay
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf

	runErr := cmd.Run()
	res := GateResult{Output: truncateScriptOutput(buf.Bytes())}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		res.ExitCode = -1
		res.Reason = fmt.Sprintf("timed out: the gate exceeded its %s-second limit", formatGateSeconds(timeout))
		return res
	}
	if runErr == nil {
		res.Passed = true
		return res
	}
	if ctx.Err() != nil {
		res.ExitCode = -1
		res.Reason = "gate was cancelled before it finished"
		return res
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		if res.ExitCode < 0 {
			res.Reason = "gate was killed by a signal"
		} else {
			res.Reason = fmt.Sprintf("gate exited with code %d", res.ExitCode)
		}
		return res
	}
	res.ExitCode = -1
	res.Reason = fmt.Sprintf("gate failed to launch: %v", runErr)
	return res
}

// gateArgv validates check and builds the argv to run. A non-empty failure
// means the gate must not run; it is the Reason to report.
func gateArgv(check GateCheck, projectDir string) (argv []string, failure string) {
	envPrefix := []string{"env", "-u", "TMUX", "-u", "TMUX_TMPDIR"}
	switch check.Kind {
	case GateCheckScript:
		if strings.TrimSpace(check.Script) == "" {
			return nil, "gate script path is empty"
		}
		scriptPath, err := gateScriptPath(check.Script, projectDir)
		if err != nil {
			return nil, err.Error()
		}
		info, statErr := os.Stat(scriptPath)
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, fmt.Sprintf("gate script %q is missing from the project's %s folder", check.Script, gateVerifyFolder)
		}
		return append(envPrefix, "bash", scriptPath), ""
	case GateCheckCommand:
		if strings.TrimSpace(check.Command) == "" {
			return nil, "gate command is empty"
		}
		return append(envPrefix, "sh", "-c", check.Command), ""
	default:
		return nil, fmt.Sprintf("unknown gate check kind %q", string(check.Kind))
	}
}

// gateScriptPath applies the shared verification-script checks and then
// requires the script to resolve inside projectDir/.datawatch/verify. A
// symlink that leads out of that folder is refused too.
func gateScriptPath(scriptRelPath, projectDir string) (string, error) {
	scriptPath, err := resolveVerificationScriptPath(scriptRelPath, projectDir)
	if err != nil {
		return "", err
	}
	folder := filepath.Join(filepath.Clean(projectDir), gateVerifyFolder)
	if !pathWithin(folder, scriptPath) {
		return "", fmt.Errorf("gate script path %q is not inside %s", scriptRelPath, gateVerifyFolder)
	}
	realScript, scriptErr := filepath.EvalSymlinks(scriptPath)
	realFolder, folderErr := filepath.EvalSymlinks(folder)
	if scriptErr == nil && folderErr == nil && !pathWithin(realFolder, realScript) {
		return "", fmt.Errorf("gate script path %q resolves outside %s", scriptRelPath, gateVerifyFolder)
	}
	return scriptPath, nil
}

// pathWithin reports whether target is strictly inside dir (both cleaned).
func pathWithin(dir, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(target))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// formatGateSeconds renders a limit as seconds without a trailing zero run,
// so a one-second limit reads "1" and a half-second limit reads "0.5".
func formatGateSeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// ResolveGateCheck is ResolveGateCheckForAutomaton over a single story.
func ResolveGateCheck(t *Task, story *Story, projectCommand string) (checks []GateCheck, reason string) {
	var stories []Story
	if story != nil {
		stories = []Story{*story}
	}
	return ResolveGateCheckForAutomaton(t, stories, projectCommand)
}

// ResolveGateCheckForAutomaton picks the checks that gate a task, in order
// of preference:
//  1. the task's own VerificationScript, when it is a file under .datawatch/verify;
//  2. the project gate command, when non-empty;
//  3. the VerificationScript of every other task in every story of the
//     Automaton (story order) that is a file under .datawatch/verify; template
//     tasks and final gates are skipped.
//
// When none applies it returns no checks and a reason saying nothing runnable
// was declared. Existence on disk is not checked here; RunScriptGate reports a
// missing script as a failed gate.
func ResolveGateCheckForAutomaton(t *Task, stories []Story, projectCommand string) (checks []GateCheck, reason string) {
	if t != nil && isGateVerifyScript(t.VerificationScript) {
		return []GateCheck{{Kind: GateCheckScript, Script: t.VerificationScript}}, ""
	}
	if cmd := strings.TrimSpace(projectCommand); cmd != "" {
		return []GateCheck{{Kind: GateCheckCommand, Command: cmd}}, ""
	}
	for si := range stories {
		for i := range stories[si].Tasks {
			other := &stories[si].Tasks[i]
			if t != nil && t.ID != "" && other.ID == t.ID {
				continue
			}
			if isTemplateTaskID(other.ID) || other.Kind == TaskKindFinalGate {
				continue
			}
			if isGateVerifyScript(other.VerificationScript) {
				checks = append(checks, GateCheck{Kind: GateCheckScript, Script: other.VerificationScript})
			}
		}
	}
	if len(checks) > 0 {
		return checks, ""
	}
	return nil, "nothing runnable was declared: no task, story or project gate command names a verification script under " + gateVerifyFolder + " or a project gate command"
}

// isGateVerifyScript reports whether p is a repo-relative path to a file
// under gateVerifyFolder, judged on its text alone: not absolute, no parent
// references, and strictly below the folder.
func isGateVerifyScript(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	slashed := filepath.ToSlash(p)
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return false
		}
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	return strings.HasPrefix(clean, gateVerifyFolder+"/") && clean != gateVerifyFolder+"/"
}
