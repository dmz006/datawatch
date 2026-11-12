//go:build !windows

package autonomous

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dmz006/datawatch/internal/resources"
)

// verifyRecordWrapper runs "$@" with combined output sent to $1 and writes
// the exit code to $2 (atomically) when it ends. bash is resolved by name.
const verifyRecordWrapper = `out=$1; ex=$2; shift 2; "$@" >"$out" 2>&1; rc=$?; printf '%s' "$rc" >"$ex.tmp" && mv "$ex.tmp" "$ex"; exit "$rc"`

// runVerifyRecorded runs argv with a durable run record. handled is false
// when no record root is configured, so the caller keeps its direct path.
func runVerifyRecorded(argv []string, dir, scriptPath string, l *verifyLaunch) (out []byte, exitCode int, runErr error, handled bool) {
	root := verificationRunRoot()
	if root == "" {
		return nil, 0, nil, false
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, 0, nil, false
	}
	// One directory per script (see verifyRunDirFor). The caller has already
	// decided that no live run owns it, so a fresh run replaces whatever a
	// previous, delivered or stale, run left behind.
	runDir := verifyRunDirFor(root, scriptPath)
	if err := os.RemoveAll(runDir); err != nil {
		return nil, 0, nil, false
	}
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		return nil, 0, nil, false
	}
	outFile := filepath.Join(runDir, verifyOutputFile)
	exitFile := filepath.Join(runDir, verifyExitFile)

	full := append([]string{"-c", verifyRecordWrapper, "bash", outFile, exitFile}, argv...)
	cmd := exec.Command("bash", full...) // #nosec G204 -- argv built by verifyScriptArgv from a validated script path
	cmd.Dir = dir

	ctx, cancel := context.WithTimeout(context.Background(), verificationScriptTimeout)
	defer cancel()

	rec := &verifyRunRecord{ScriptPath: scriptPath, StartedAt: time.Now(), OutputFile: outFile, ExitFile: exitFile}
	watch := l != nil && l.Path == VerifyCeilingWatchdog && l.CeilingMB > 0
	var waitErr error
	timedOut := false

	if watch {
		// resources.Watch starts the command in its own process group.
		rec.PID = -1
		_ = writeVerifyRecord(runDir, rec)
		res, err := resources.Watch(ctx, cmd, l.CeilingMB)
		if err == resources.ErrNoCeiling {
			l.Path, l.CeilingMB = VerifyCeilingNone, 0
			watch = false
		} else {
			waitErr = err
			timedOut = ctx.Err() != nil
			if res.Killed {
				l.Killed, l.PeakMB, l.KilledProcess = true, int(res.PeakKB/1024), res.LargestName
				appendFile(outFile, fmt.Sprintf("\nverification script passed its %s MB memory ceiling and was ended\n", strconv.Itoa(l.CeilingMB)))
			}
		}
	}
	if !watch {
		cmd = exec.Command("bash", full...) // #nosec G204 -- see above
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return nil, -1, err, true
		}
		rec.PID = cmd.Process.Pid
		_ = writeVerifyRecord(runDir, rec)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case waitErr = <-done:
		case <-ctx.Done():
			timedOut = true
			// end-reason-ok: kills a verification script run's own OS process group at its deadline, not a datawatch session
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			waitErr = <-done
		}
	}

	out, _ = os.ReadFile(outFile) // #nosec G304 -- path under the run dir we created
	rec.EndedAt = time.Now()
	rec.TimedOut = timedOut
	if b, rerr := os.ReadFile(exitFile); rerr == nil { // #nosec G304
		if code, cerr := strconv.Atoi(strings.TrimSpace(string(b))); cerr == nil {
			rec.ExitCode = &code
		}
	}
	if rec.ExitCode == nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			c := ee.ExitCode()
			rec.ExitCode = &c
		}
	}
	// The result is returned to this caller right below, so the record is
	// delivered: a restarted daemon must not consume it a second time.
	rec.Delivered = true
	_ = writeVerifyRecord(runDir, rec)
	if timedOut {
		out = append(out, []byte(fmt.Sprintf("\nverification script timed out after %s and was ended\n", verificationScriptTimeout))...)
		return out, -1, fmt.Errorf("verification script timed out after %s", verificationScriptTimeout), true
	}
	if rec.ExitCode != nil {
		return out, *rec.ExitCode, nil, true
	}
	if waitErr != nil {
		return out, -1, waitErr, true
	}
	return out, 0, nil, true
}

// killVerifyGroup ends the process group led by pid. It is the only way the
// daemon ends a script run it is waiting on after a restart.
func killVerifyGroup(pid int) {
	if pid > 0 {
		// end-reason-ok: ends the process group of a verification script run the daemon was waiting on after a restart, not a datawatch session
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

func appendFile(path, s string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(s)
}
