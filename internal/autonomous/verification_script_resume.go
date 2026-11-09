// Boot-safe verification script runs. A verification or final gate that
// starts a script first looks at that script's durable run record (see
// verification_script_record.go) and resumes from it instead of blindly
// running the script again:
//
//   - the record's process is alive (same pid, same start time) -> attach a
//     waiter to the record and take its result when the run ends;
//   - the record has an exit code and has not been delivered -> consume it
//     directly as the script result, once;
//   - anything else (missing or unreadable record, a stale pid that is no
//     longer the recorded process, or a record already delivered) -> run the
//     script from the start, exactly once.
//
// The pid check guards against pid reuse: a pid is trusted only when its
// live /proc start time matches the StartedAt the daemon recorded.
//
// The seams verifyRecordReadFn and verifyPIDLiveFn are package variables so
// tests can drive the decision with a fake record reader and a fake
// process-liveness check.
package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// verifyPIDStartSkew bounds how far a live process's start time may differ
// from the StartedAt the daemon recorded. The process starts a few
// milliseconds after the record's StartedAt; /proc start times are whole
// clock ticks on top of a whole-second boot time, so a small window is
// needed. A reused pid starts at an unrelated time and falls outside it.
const verifyPIDStartSkew = 3 * time.Second

// verifyClockTicksPerSecond is USER_HZ, the unit of /proc/<pid>/stat start
// times. It is 100 on every common Linux build.
const verifyClockTicksPerSecond = 100

// verifyWaitPollInterval is how often a waiter re-reads an alive record.
var verifyWaitPollInterval = 500 * time.Millisecond

var (
	// verifyRecordReadFn reads the run record in dir. It returns an error when
	// the record is missing or unreadable.
	verifyRecordReadFn = readVerifyRunRecord
	// verifyPIDLiveFn reports whether pid is a live process that started at
	// startedAt (pid reuse is rejected).
	verifyPIDLiveFn = verifyPIDMatchesRecord
	// verifyKillGroupFn ends a run's process group when the run outlives its
	// timeout while a waiter is attached to it.
	verifyKillGroupFn = killVerifyGroup
)

// readVerifyRunRecord reads run.json from dir. When the record has no exit
// code yet but the wrapper already wrote the exit file, the exit code is
// taken from that file, so a run that finished while the daemon was down is
// seen as finished.
func readVerifyRunRecord(dir string) (verifyRunRecord, error) {
	var rec verifyRunRecord
	b, err := os.ReadFile(filepath.Join(dir, verifyRecordFile)) // #nosec G304 -- dir is under the verify run root
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, err
	}
	if rec.ExitCode == nil {
		if eb, eerr := os.ReadFile(filepath.Join(dir, verifyExitFile)); eerr == nil { // #nosec G304
			if code, cerr := strconv.Atoi(strings.TrimSpace(string(eb))); cerr == nil {
				rec.ExitCode = &code
			}
		}
	}
	return rec, nil
}

// verifyPIDMatchesRecord reports whether pid is a live, non-zombie process
// whose start time is the recorded startedAt, within verifyPIDStartSkew. A
// pid of zero or less (a run started under the watchdog has none recorded)
// is never trusted.
func verifyPIDMatchesRecord(pid int, startedAt time.Time) bool {
	if pid <= 0 || startedAt.IsZero() {
		return false
	}
	started, state, ok := procStartAndState(pid)
	if !ok || state == 'Z' || state == 'X' {
		return false
	}
	d := started.Sub(startedAt)
	if d < 0 {
		d = -d
	}
	return d <= verifyPIDStartSkew
}

// procStartAndState returns the wall-clock start time and the state letter of
// pid from /proc. ok is false when /proc is unavailable or unparseable (for
// example on a non-Linux host), which makes the pid untrusted.
func procStartAndState(pid int) (time.Time, byte, bool) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)) // #nosec G304 -- numeric pid
	if err != nil {
		return time.Time{}, 0, false
	}
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return time.Time{}, 0, false
	}
	// Fields after the ")" of the comm name start at field 3 (state). The
	// start time is field 22, index 19 in that slice.
	fields := strings.Fields(s[i+1:])
	if len(fields) < 20 || len(fields[0]) == 0 {
		return time.Time{}, 0, false
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return time.Time{}, 0, false
	}
	boot, ok := bootTime()
	if !ok {
		return time.Time{}, 0, false
	}
	started := boot.Add(time.Duration(ticks) * (time.Second / verifyClockTicksPerSecond))
	return started, fields[0][0], true
}

// bootTime reads the system boot time from the btime line of /proc/stat.
func bootTime() (time.Time, bool) {
	b, err := os.ReadFile("/proc/stat") // #nosec G304 -- fixed path
	if err != nil {
		return time.Time{}, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, found := strings.CutPrefix(line, "btime "); found {
			secs, perr := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			if perr != nil {
				return time.Time{}, false
			}
			return time.Unix(secs, 0), true
		}
	}
	return time.Time{}, false
}

// verifyRunTimeoutErr is the error reported for a run that exceeded
// verificationScriptTimeout.
func verifyRunTimeoutErr() error {
	return fmt.Errorf("verification script timed out after %s", verificationScriptTimeout)
}

// resumeVerificationScriptRun decides how a run of scriptPath starts from its
// durable record. handled=false means the caller must run the script from the
// start; handled=true returns the run's output, exit code and error.
func resumeVerificationScriptRun(scriptPath string) (out []byte, exitCode int, err error, handled bool) {
	root := verificationRunRoot()
	if root == "" {
		return nil, 0, nil, false
	}
	dir := verifyRunDirFor(root, scriptPath)
	rec, rerr := verifyRecordReadFn(dir)
	if rerr != nil || rec.ScriptPath != scriptPath || rec.Delivered {
		return nil, 0, nil, false
	}
	if rec.ExitCode != nil {
		out, code, cerr := consumeVerifyRun(dir, rec)
		return out, code, cerr, true
	}
	if !verifyPIDLiveFn(rec.PID, rec.StartedAt) {
		return nil, 0, nil, false // stale pid: run the script again, once
	}
	out, code, werr := waitVerifyRun(dir, rec)
	return out, code, werr, true
}

// consumeVerifyRun returns the output and result of a finished record and
// marks it delivered so it is never consumed again.
func consumeVerifyRun(dir string, rec verifyRunRecord) ([]byte, int, error) {
	out, _ := os.ReadFile(filepath.Join(dir, verifyOutputFile)) // #nosec G304 -- under the run dir
	if rec.TimedOut {
		out = append(out, []byte(fmt.Sprintf("\nverification script timed out after %s and was ended\n", verificationScriptTimeout))...)
		return out, -1, verifyRunTimeoutErr()
	}
	code := 0
	if rec.ExitCode != nil {
		code = *rec.ExitCode
	}
	markVerifyRunDelivered(dir, rec)
	return out, code, nil
}

// markVerifyRunDelivered records that a run's result reached a caller. A
// write failure only means the record may be consumed once more.
func markVerifyRunDelivered(dir string, rec verifyRunRecord) {
	rec.Delivered = true
	_ = writeVerifyRecord(dir, &rec)
}

// waitVerifyRun waits on an alive run until its record carries an exit code,
// then consumes it. The run's own timeout is enforced from its recorded
// StartedAt, so a run that outlived the daemon still ends on time.
func waitVerifyRun(dir string, rec verifyRunRecord) ([]byte, int, error) {
	cur := rec
	for {
		if next, err := verifyRecordReadFn(dir); err == nil {
			cur = next
			if cur.ExitCode != nil {
				return consumeVerifyRun(dir, cur)
			}
		}
		if time.Since(rec.StartedAt) >= verificationScriptTimeout {
			verifyKillGroupFn(cur.PID)
			out, _ := os.ReadFile(filepath.Join(dir, verifyOutputFile)) // #nosec G304
			return append(out, []byte(fmt.Sprintf("\nverification script timed out after %s and was ended\n", verificationScriptTimeout))...), -1, verifyRunTimeoutErr()
		}
		if !verifyPIDLiveFn(cur.PID, cur.StartedAt) {
			// The process ended. Its exit file may have landed just before this
			// check, so read the record once more before giving up.
			if next, err := verifyRecordReadFn(dir); err == nil && next.ExitCode != nil {
				return consumeVerifyRun(dir, next)
			}
			out, _ := os.ReadFile(filepath.Join(dir, verifyOutputFile)) // #nosec G304
			return out, -1, errors.New("verification script run ended without an exit code")
		}
		time.Sleep(verifyWaitPollInterval)
	}
}

// verificationScriptRunAlive reports whether the task-level verification
// script at script (relative to projectDir) has a run whose process is still
// alive. A finished, delivered or stale record is not alive.
func verificationScriptRunAlive(projectDir, script string) bool {
	scriptPath, ok := verificationScriptAbsPath(projectDir, script)
	if !ok {
		return false
	}
	root := verificationRunRoot()
	if root == "" {
		return false
	}
	rec, err := verifyRecordReadFn(verifyRunDirFor(root, scriptPath))
	if err != nil || rec.ScriptPath != scriptPath || rec.Delivered || rec.ExitCode != nil {
		return false
	}
	return verifyPIDLiveFn(rec.PID, rec.StartedAt)
}

// retireVerificationScriptRun marks a run's record delivered without a
// result, so a later attempt of the same script can never consume an
// unfinished verdict left behind by an earlier attempt.
func retireVerificationScriptRun(projectDir, script string) {
	scriptPath, ok := verificationScriptAbsPath(projectDir, script)
	if !ok {
		return
	}
	root := verificationRunRoot()
	if root == "" {
		return
	}
	dir := verifyRunDirFor(root, scriptPath)
	rec, err := verifyRecordReadFn(dir)
	if err != nil || rec.Delivered {
		return
	}
	markVerifyRunDelivered(dir, rec)
}

// verificationScriptAbsPath resolves script under projectDir the way
// runVerificationScriptCapped does, and reports false for an empty, absolute
// or escaping path.
func verificationScriptAbsPath(projectDir, script string) (string, bool) {
	if script == "" || filepath.IsAbs(script) {
		return "", false
	}
	scriptPath := filepath.Clean(filepath.Join(projectDir, script))
	rel, err := filepath.Rel(filepath.Clean(projectDir), scriptPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return scriptPath, true
}

// holdVerifyingForScriptRun is the boot-resume check for a verifying task that
// would otherwise be reset. When the task has no recorded verdict and its
// verification script run is still alive, the task is kept verifying with its
// capacity lease re-adopted and true is returned. Otherwise the task's
// undelivered record is retired, so no later attempt can consume a verdict
// left by this one, and false is returned so the caller resets the task.
func (m *Manager) holdVerifyingForScriptRun(prd *PRD, t *Task) bool {
	if prd == nil || t == nil || t.VerificationScript == "" || t.Verification != nil {
		return false
	}
	projectDir := m.resolveTaskProjectDir(prd, t)
	if verificationScriptRunAlive(projectDir, t.VerificationScript) {
		m.adoptCapacity(prd, t)
		log.Printf("[autonomous] boot-resume: prd=%s task %s kept verifying: verification script %s is still running; a waiter attaches to its run, no worker reset, no retry counted", prd.ID, t.ID, t.VerificationScript)
		return true
	}
	retireVerificationScriptRun(projectDir, t.VerificationScript)
	return false
}
