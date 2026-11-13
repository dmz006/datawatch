// Durable run record for a final gate. A gate check is started in its own
// session by a small wrapper that writes the gate's combined output and exit
// code to files under the record directory, so the gate keeps running when
// the daemon exits. The daemon waits on the record (the exit file and the
// recorded process), not on a direct child. A restarted daemon looks for the
// record of the same check in the same project and either re-attaches to the
// live run, delivers a finished-but-undelivered result once, or starts a new
// run.
//
// The mechanics are shared with verification script runs: the same run root
// (SetVerificationRunRoot), the same wrapper, the same pid-reuse guard
// (verifyPIDLiveFn / verifyPIDMatchesRecord) and the same exit-file rules.
// With no run root configured the gate runs as a direct child, exactly as a
// verification script does.
package autonomous

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gateWaitPollInterval is how often a waiter re-reads a running gate's record.
var gateWaitPollInterval = 200 * time.Millisecond

// gateEndGrace bounds how long a waiter lets a gate's process disappear after
// its group was ended at the deadline.
const gateEndGrace = 3 * time.Second

// errGateDurableUnsupported means this platform keeps the direct-child path.
var errGateDurableUnsupported = errors.New("durable gate runs are not supported on this platform")

// gateRunRecord is the run.json content of one gate run. ExitCode is nil
// until the gate ends. Delivered is set once the result has reached a caller,
// so a finished record is consumed at most once.
type gateRunRecord struct {
	CheckID    string    `json:"check_id"`
	ProjectDir string    `json:"project_dir"`
	PID        int       `json:"pid"`
	ProcStart  time.Time `json:"proc_start"`
	StartedAt  time.Time `json:"started_at"`
	TimeoutNS  int64     `json:"timeout_ns,omitempty"`
	OutputFile string    `json:"output_file"`
	ExitFile   string    `json:"exit_file"`
	ExitCode   *int      `json:"exit_code,omitempty"`
	TimedOut   bool      `json:"timed_out,omitempty"`
	Delivered  bool      `json:"delivered,omitempty"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
}

// gateCheckID names a check independently of where it runs.
func gateCheckID(c GateCheck) string {
	switch c.Kind {
	case GateCheckScript:
		return "script:" + c.Script
	case GateCheckCommand:
		return "command:" + c.Command
	}
	return string(c.Kind)
}

// gateRunDirFor is the one run directory of check in projectDir under root.
func gateRunDirFor(root string, c GateCheck, projectDir string) string {
	sum := sha256.Sum256([]byte(gateCheckID(c) + "\x00" + filepath.Clean(projectDir)))
	return filepath.Join(root, "gate-"+hex.EncodeToString(sum[:8]))
}

func writeGateRecord(dir string, rec *gateRunRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, verifyRecordFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, verifyRecordFile))
}

// readGateRecord reads run.json from dir. A record without an exit code
// takes it from the exit file, so a gate that finished while the daemon was
// down is seen as finished.
func readGateRecord(dir string) (gateRunRecord, error) {
	var rec gateRunRecord
	b, err := os.ReadFile(filepath.Join(dir, verifyRecordFile)) // #nosec G304 -- dir is under the run root
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

// gateRecordMatches reports whether rec belongs to check in projectDir and
// has not been delivered.
func gateRecordMatches(rec gateRunRecord, c GateCheck, projectDir string) bool {
	return rec.CheckID == gateCheckID(c) && filepath.Clean(rec.ProjectDir) == filepath.Clean(projectDir) && !rec.Delivered
}

func gateRecordAlive(rec gateRunRecord) bool {
	start := rec.ProcStart
	if start.IsZero() {
		start = rec.StartedAt
	}
	return verifyPIDLiveFn(rec.PID, start)
}

// runGateDurable runs or resumes check through its durable record. handled
// is false when no run root is set or the platform cannot run durably, so the
// caller keeps the direct-child path.
func runGateDurable(ctx context.Context, check GateCheck, argv []string, projectDir string, timeout time.Duration) (GateResult, bool) {
	root := verificationRunRoot()
	if root == "" {
		return GateResult{}, false
	}
	dir := gateRunDirFor(root, check, projectDir)

	if rec, err := readGateRecord(dir); err == nil && gateRecordMatches(rec, check, projectDir) {
		switch {
		case rec.ExitCode != nil:
			return consumeGateRun(dir, rec, timeout), true
		case rec.PID > 0 && gateRecordAlive(rec):
			return waitGateRun(ctx, dir, rec, timeout), true
		case rec.PID > 0:
			// The process is gone and left no exit code: killed by a signal.
			return consumeGateRun(dir, rec, timeout), true
		}
	}

	if err := os.MkdirAll(root, 0o750); err != nil {
		return GateResult{}, false
	}
	if err := os.RemoveAll(dir); err != nil {
		return GateResult{}, false
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return GateResult{}, false
	}
	rec := gateRunRecord{
		CheckID:    gateCheckID(check),
		ProjectDir: filepath.Clean(projectDir),
		StartedAt:  time.Now(),
		TimeoutNS:  int64(timeout),
		OutputFile: filepath.Join(dir, verifyOutputFile),
		ExitFile:   filepath.Join(dir, verifyExitFile),
	}
	pid, err := launchGateProcess(argv, projectDir, rec.OutputFile, rec.ExitFile)
	if errors.Is(err, errGateDurableUnsupported) {
		return GateResult{}, false
	}
	if err != nil {
		return GateResult{ExitCode: -1, Reason: fmt.Sprintf("gate failed to launch: %v", err)}, true
	}
	rec.PID = pid
	rec.ProcStart = rec.StartedAt
	if st, _, ok := procStartAndState(pid); ok {
		rec.ProcStart = st
	}
	_ = writeGateRecord(dir, &rec)
	return waitGateRun(ctx, dir, rec, timeout), true
}

// gateDeadline is the earliest of the record's own limit (from its start) and
// the caller's remaining limit (from now). Zero means no cap.
func gateDeadline(rec gateRunRecord, timeout time.Duration) time.Time {
	var dl time.Time
	if rec.TimeoutNS > 0 {
		dl = rec.StartedAt.Add(time.Duration(rec.TimeoutNS))
	}
	if timeout > 0 {
		if c := time.Now().Add(timeout); dl.IsZero() || c.Before(dl) {
			dl = c
		}
	}
	return dl
}

// waitGateRun waits on a live run until its exit code lands, its deadline
// passes or ctx ends. A cancelled ctx leaves the gate running and reports
// Interrupted, so a daemon stop never fails the gate.
func waitGateRun(ctx context.Context, dir string, rec gateRunRecord, timeout time.Duration) GateResult {
	deadline := gateDeadline(rec, timeout)
	for {
		if next, err := readGateRecord(dir); err == nil {
			rec = next
			if rec.ExitCode != nil {
				return consumeGateRun(dir, rec, timeout)
			}
		}
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return endGateRun(dir, rec, timeout)
			}
			return GateResult{ExitCode: -1, Interrupted: true, Reason: "gate was cancelled before it finished"}
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return endGateRun(dir, rec, timeout)
		}
		if !gateRecordAlive(rec) {
			// The exit file may have landed just before the liveness check.
			if next, err := readGateRecord(dir); err == nil && next.ExitCode != nil {
				return consumeGateRun(dir, next, timeout)
			}
			return consumeGateRun(dir, rec, timeout)
		}
		select {
		case <-ctx.Done():
		case <-time.After(gateWaitPollInterval):
		}
	}
}

// endGateRun ends the gate's whole process group at its deadline and delivers
// the timed-out result.
func endGateRun(dir string, rec gateRunRecord, timeout time.Duration) GateResult {
	killGateGroup(rec.PID)
	end := time.Now().Add(gateEndGrace)
	for time.Now().Before(end) && gateRecordAlive(rec) {
		time.Sleep(20 * time.Millisecond)
	}
	rec.TimedOut = true
	return consumeGateRun(dir, rec, timeout)
}

// consumeGateRun turns a finished (or vanished) record into a result and
// marks the record delivered so it is never consumed again.
func consumeGateRun(dir string, rec gateRunRecord, timeout time.Duration) GateResult {
	out, _ := os.ReadFile(filepath.Join(dir, verifyOutputFile)) // #nosec G304 -- under the run dir
	res := GateResult{Output: truncateScriptOutput(out)}
	limit := timeout
	if rec.TimeoutNS > 0 {
		limit = time.Duration(rec.TimeoutNS)
	}
	switch {
	case rec.TimedOut:
		res.TimedOut, res.ExitCode = true, -1
		res.Reason = fmt.Sprintf("timed out: the gate exceeded its %s-second limit", formatGateSeconds(limit))
	case rec.ExitCode == nil && rec.TimeoutNS > 0 && !time.Now().Before(rec.StartedAt.Add(time.Duration(rec.TimeoutNS))):
		res.TimedOut, res.ExitCode = true, -1
		res.Reason = fmt.Sprintf("timed out: the gate exceeded its %s-second limit", formatGateSeconds(limit))
	case rec.ExitCode == nil:
		res.ExitCode = -1
		res.Reason = "gate was killed by a signal"
	case *rec.ExitCode == 0:
		res.Passed = true
	default:
		res.ExitCode = *rec.ExitCode
		res.Reason = fmt.Sprintf("gate exited with code %d", res.ExitCode)
	}
	rec.Delivered = true
	rec.EndedAt = time.Now()
	if res.TimedOut && rec.ExitCode == nil {
		c := -1
		rec.ExitCode = &c
	}
	_ = writeGateRecord(dir, &rec)
	return res
}

// holdFinalGateForRecord is the boot check for a final gate task found
// running. When its gate has a live run, or a finished result nobody has
// taken yet, it returns true: the task must go back to pending without
// failing or restarting the gate, and runScriptGateTask re-attaches to the
// record. Otherwise any undelivered record of a gone process is retired, so a
// fresh run starts, and it returns false.
func (m *Manager) holdFinalGateForRecord(prd *PRD, t *Task) bool {
	if m == nil || prd == nil || t == nil || t.Kind != TaskKindFinalGate {
		return false
	}
	root := verificationRunRoot()
	if root == "" {
		return false
	}
	checks, _ := ResolveGateCheck(t, storyOfTask(prd, t), m.projectGateCommand(prd))
	projectDir := ResolveTaskProjectDir(prd, t)
	hold := false
	for _, c := range checks {
		dir := gateRunDirFor(root, c, projectDir)
		rec, err := readGateRecord(dir)
		if err != nil || !gateRecordMatches(rec, c, projectDir) {
			continue
		}
		if rec.ExitCode != nil || (rec.PID > 0 && gateRecordAlive(rec)) {
			hold = true
			continue
		}
		rec.Delivered = true
		_ = writeGateRecord(dir, &rec)
	}
	return hold
}
