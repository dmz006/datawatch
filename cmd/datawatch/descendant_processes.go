package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// descendantProcess identifies one process that was a descendant of a worker
// at some point while the worker ran. PID and StartTime together uniquely
// identify the process: pids are reused by the kernel, so the start time is
// the guard that keeps a stale entry from pointing at an unrelated process
// that happened to get the same pid later.
type descendantProcess struct {
	PID       int
	StartTime uint64
	Line      string
}

// readProcessStartTime returns field 22 (starttime) of /proc/<pid>/stat, the
// field offset in the time since boot since process creation. The second
// return value is false when the entry cannot be read (the process exited or
// is unreadable to the caller).
//
// The command name (field 2) is parenthesised but may itself contain spaces
// or parentheses, so the fixed-width fields are parsed only after the last
// closing parenthesis, never before it.
func readProcessStartTime(pid int) (uint64, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	s := string(b)
	// s[0] is the pid, s[1] is '(', then the parenthesised command name.
	paren := strings.LastIndexByte(s, ')')
	if paren < 0 || paren+2 >= len(s) {
		return 0, false
	}
	// After the last ')' the first token is field 3 (state), so field 22
	// (starttime) sits at index 22-3 = 19.
	fields := strings.Fields(s[paren+1:])
	if len(fields) < 20 {
		return 0, false
	}
	v, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// descendantLine renders one recorded descendant as 'pid starttime comm argv'
// in the same style as formatRunnerProcessLine, with the start time appended
// so the line alone is enough to verify the entry is still the same process.
// formatRunnerProcessLine would print the pid twice around the start time, so
// the join is mirrored here instead.
func descendantLine(pid int, startTime uint64, comm string, argv []string) string {
	parts := []string{strconv.Itoa(pid), strconv.FormatUint(startTime, 10)}
	if comm != "" {
		parts = append(parts, comm)
	}
	if len(argv) > 2 {
		argv = argv[:2]
	}
	parts = append(parts, argv...)
	return strings.Join(parts, " ")
}

// recordDescendants returns the set of descendants the worker is accounting
// for: every visible pid whose start time can currently be read, joined with
// the previously recorded entries that are still the same live process. A
// prior entry survives without being visible because a process can be
// reparented out of the worker's tree while still running; it is dropped only
// when it is gone or when its pid now carries a different start time (pid
// reuse). Entries are keyed by pid, so a new process holding a reused pid
// simply replaces the stale one.
func recordDescendants(visible map[int]bool, prior map[int]descendantProcess) map[int]descendantProcess {
	rec := make(map[int]descendantProcess, len(visible)+len(prior))
	for pid, old := range prior {
		start, ok := readProcessStartTime(pid)
		if ok && start == old.StartTime {
			rec[pid] = old
		}
	}
	for pid := range visible {
		start, ok := readProcessStartTime(pid)
		if !ok {
			continue
		}
		rec[pid] = descendantProcess{
			PID:       pid,
			StartTime: start,
			Line:      descendantLine(pid, start, readProcessCommName(pid), readProcessCmdline(pid)),
		}
	}
	return rec
}

// descendantPollInterval is how often processGone is re-checked while waiting
// for a signalled process to exit.
const descendantPollInterval = 10 * time.Millisecond

// processGone reports whether pid no longer represents a running process: the
// stat entry is unreadable (exited and reaped) or the process is a zombie or
// dead-state entry, which counts as gone even while it lingers un-reaped.
func processGone(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(b)
	paren := strings.LastIndexByte(s, ')')
	if paren < 0 || paren+2 >= len(s) {
		return false
	}
	fields := strings.Fields(s[paren+1:])
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "Z", "X", "x":
		return true
	}
	return false
}

// waitGone polls processGone every descendantPollInterval until until passes
// and reports whether it happened.
func waitGone(pid int, until time.Time) bool {
	for {
		if processGone(pid) {
			return true
		}
		deadline := time.Now().Add(descendantPollInterval)
		if deadline.After(until) {
			deadline = until
		}
		time.Sleep(time.Until(deadline))
		if !time.Now().Before(until) {
			return processGone(pid)
		}
	}
}

// descendantKillSettle bounds the re-check after SIGKILL, which the kernel
// processes before the process disappears (or turns into a zombie). The
// worker's own children are not reaped by this helper, so the settled state
// is observed through the zombie check in processGone.
const descendantKillSettle = 200 * time.Millisecond

// endRecordedDescendants ends every still-valid recorded descendant. Each
// entry for which stillSame holds is sent SIGTERM, given until the grace
// period to exit, and then SIGKILLed if it is still alive under the same
// identity. Entries whose identity no longer matches (start time no longer
// seen at that pid, i.e. the process exited or its pid was reused) are never
// signalled, so pid reuse can never hit an unrelated process. Only pids that
// are in the recorded map are ever touched; no process is ever looked up by
// name. The results split into the entries that ended and the ones that
// survived the full escalation.
func endRecordedDescendants(
	recorded map[int]descendantProcess,
	grace time.Duration,
	signal func(pid int, sig syscall.Signal) error,
	stillSame func(descendantProcess) bool,
) (ended []descendantProcess, survivors []descendantProcess) {
	deadline := time.Now().Add(grace)
	for _, rec := range recorded {
		if !stillSame(rec) {
			// Never signal a stale identity: the pid may be reused.
			survivors = append(survivors, rec)
			continue
		}
		_ = signal(rec.PID, syscall.SIGTERM)
		if waitGone(rec.PID, deadline) {
			ended = append(ended, rec)
			continue
		}
		if !stillSame(rec) {
			// The identity changed while it was dying; leave it alone.
			survivors = append(survivors, rec)
			continue
		}
		_ = signal(rec.PID, syscall.SIGKILL)
		settle := deadline
		if now := time.Now(); now.Add(descendantKillSettle).After(settle) {
			settle = now.Add(descendantKillSettle)
		}
		if waitGone(rec.PID, settle) {
			ended = append(ended, rec)
		} else {
			survivors = append(survivors, rec)
		}
	}
	return ended, survivors
}

// signalRecordedPID sends sig to one recorded descendant pid. It goes
// through os.FindProcess and Process.Signal instead of syscall.Kill so the
// command still builds for Windows, where syscall.Kill does not exist and
// a signal other than a kill simply returns an error.
// end-reason-ok: signals one recorded descendant pid after its start time matched, not a session
func signalRecordedPID(pid int, sig syscall.Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(sig)
}
