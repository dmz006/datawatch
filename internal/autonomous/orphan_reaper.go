// Evidence-based finder and reaper for processes left behind by a finished or
// failed Automaton session.
//
// A process is attributed to a session only by evidence read from /proc: its
// working directory, executable or script path lies inside the session
// worktree or scratch folder, its ancestry reaches a known session root pid,
// or its environment carries the session id. A process is never matched by
// name. Each match is ended by exact pid, and the evidence plus the process
// start time are re-verified immediately before every signal so a reused pid
// can never hit an unrelated process.
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// OrphanEvidence describes what proves a process belongs to one session.
type OrphanEvidence struct {
	// SessionID is matched against environment values (exact match).
	SessionID string
	// Dirs are the session worktree and scratch folders. A cwd, exe or
	// script path inside any of them is evidence.
	Dirs []string
	// RootPIDs are live pids known to belong to the session. A process
	// whose ancestry reaches one of them is evidence.
	RootPIDs []int
	// AdoptedBy lists the pids that adopt orphans besides init (pid 1), such as
	// a user-level service manager that acts as subreaper. Path evidence
	// (cwd, exe, script) only counts for a process whose parent is init or one
	// of these, so an operator's live shell or editor that merely sits in a
	// session folder is never touched.
	AdoptedBy []int
}

// OrphanProc is one process proven to belong to a session.
type OrphanProc struct {
	PID       int
	StartTime uint64
	// Command is the short command (name and first two arguments), never
	// the environment.
	Command string
	// Evidence names the kind of proof: cwd, exe, script, ancestry or env.
	Evidence string
}

// OrphanReapResult reports what a reap did.
type OrphanReapResult struct {
	Count     int
	Ended     []OrphanProc
	Survivors []OrphanProc
}

// OrphanReaper finds and ends evidence-matched processes. The zero value uses
// the real /proc, the real clock and a real signal.
type OrphanReaper struct {
	ProcRoot   string
	Now        func() time.Time
	Sleep      func(time.Duration)
	Signal     func(pid int, sig syscall.Signal) error
	Grace      time.Duration // wait after the polite signal
	KillSettle time.Duration // wait after the forced signal
	Poll       time.Duration
	SelfPID    int // never matched; defaults to os.Getpid()
	// UID is the owner a process must have to be signalled; defaults to
	// os.Getuid(). Processes of other users are never matched.
	UID func() int
}

const (
	orphanDefaultGrace  = 2 * time.Second
	orphanDefaultSettle = 500 * time.Millisecond
	orphanDefaultPoll   = 10 * time.Millisecond
	orphanCmdMax        = 120
	orphanMinIDLen      = 4
)

func (r *OrphanReaper) procRoot() string {
	if r.ProcRoot != "" {
		return r.ProcRoot
	}
	return "/proc"
}

func (r *OrphanReaper) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *OrphanReaper) sleep(d time.Duration) {
	if r.Sleep != nil {
		r.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (r *OrphanReaper) signal(pid int, sig syscall.Signal) error {
	if r.Signal != nil {
		return r.Signal(pid, sig)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	// end-reason-ok: signals one pid whose session evidence and start time were just re-verified
	return p.Signal(sig)
}

// signalVerified signals pid only while verify holds. On Linux it first pins
// the process with a pidfd and verifies afterwards, so the check and the signal
// refer to the same process even if the pid is reused in between. With an
// injected Signal, or where pidfd is unavailable, it verifies then signals by
// pid.
func (r *OrphanReaper) signalVerified(pid int, sig syscall.Signal, verify func() bool) bool {
	if r.Signal == nil {
		if fd, ok := openOrphanPidfd(pid); ok {
			defer fd.close()
			if !verify() {
				return false
			}
			_ = fd.signal(sig)
			return true
		}
	}
	if !verify() {
		return false
	}
	_ = r.signal(pid, sig)
	return true
}

func (r *OrphanReaper) uid() int {
	if r.UID != nil {
		return r.UID()
	}
	return os.Getuid()
}

// ownedByUs reports whether the process directory is owned by the reaper's user.
func (r *OrphanReaper) ownedByUs(pid int) bool {
	fi, err := os.Stat(filepath.Join(r.procRoot(), strconv.Itoa(pid)))
	if err != nil {
		return false
	}
	return fileOwnedBy(fi, r.uid())
}

func (r *OrphanReaper) durations() (grace, settle, poll time.Duration) {
	grace, settle, poll = r.Grace, r.KillSettle, r.Poll
	if grace <= 0 {
		grace = orphanDefaultGrace
	}
	if settle <= 0 {
		settle = orphanDefaultSettle
	}
	if poll <= 0 {
		poll = orphanDefaultPoll
	}
	return
}

// procStat holds the fields of /proc/<pid>/stat the reaper needs.
type procStat struct {
	state string
	ppid  int
	start uint64
}

func (r *OrphanReaper) readStat(pid int) (procStat, bool) {
	b, err := os.ReadFile(filepath.Join(r.procRoot(), strconv.Itoa(pid), "stat"))
	if err != nil {
		return procStat{}, false
	}
	s := string(b)
	paren := strings.LastIndexByte(s, ')') // comm may contain ')' or spaces
	if paren < 0 || paren+2 >= len(s) {
		return procStat{}, false
	}
	f := strings.Fields(s[paren+1:])
	if len(f) < 20 {
		return procStat{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	start, err2 := strconv.ParseUint(f[19], 10, 64)
	if err1 != nil || err2 != nil {
		return procStat{}, false
	}
	return procStat{state: f[0], ppid: ppid, start: start}, true
}

func (r *OrphanReaper) gone(pid int) bool {
	st, ok := r.readStat(pid)
	if !ok {
		return true
	}
	switch st.state {
	case "Z", "X", "x":
		return true
	}
	return false
}

func (r *OrphanReaper) readCmdline(pid int) []string {
	b, err := os.ReadFile(filepath.Join(r.procRoot(), strconv.Itoa(pid), "cmdline"))
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
}

func (r *OrphanReaper) readComm(pid int) string {
	b, err := os.ReadFile(filepath.Join(r.procRoot(), strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (r *OrphanReaper) readLink(pid int, name string) string {
	p, err := os.Readlink(filepath.Join(r.procRoot(), strconv.Itoa(pid), name))
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(p, " (deleted)")
}

// shortCommand renders the program (base name) plus up to two arguments. It
// never includes the environment, and it never shows an argument value that
// could carry a secret: flags are shown by name only (the "=value" part is
// dropped), and only a path argument inside the session folders is shown; every
// other argument becomes "<arg>".
func (r *OrphanReaper) shortCommand(pid int, dirs []string) string {
	argv := r.readCmdline(pid)
	if len(argv) == 0 {
		return r.readComm(pid)
	}
	parts := []string{filepath.Base(argv[0])}
	for _, a := range argv[1:min(len(argv), 3)] {
		switch {
		case strings.HasPrefix(a, "-"):
			if i := strings.IndexByte(a, '='); i >= 0 {
				a = a[:i]
			}
			parts = append(parts, a)
		case orphanInside(a, dirs):
			parts = append(parts, a)
		default:
			parts = append(parts, "<arg>")
		}
	}
	s := strings.Join(parts, " ")
	if len(s) > orphanCmdMax {
		s = s[:orphanCmdMax]
	}
	return s
}

// orphanDirs cleans the evidence directories and adds their symlink-resolved
// forms, dropping empty entries and the filesystem root (which would match
// everything).
func orphanDirs(dirs []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" || d == "/" || !filepath.IsAbs(d) || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		c := filepath.Clean(d)
		add(c)
		if rd, err := filepath.EvalSymlinks(c); err == nil {
			add(rd)
		}
	}
	return out
}

func orphanInside(path string, dirs []string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	path = filepath.Clean(path)
	for _, d := range dirs {
		if path == d || strings.HasPrefix(path, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// orphaned reports whether the process was reparented away from its session:
// its parent is init or one of the declared adopters.
func (r *OrphanReaper) orphaned(pid int, ev OrphanEvidence) bool {
	st, ok := r.readStat(pid)
	if !ok {
		return false
	}
	if st.ppid == 1 {
		return true
	}
	for _, p := range ev.AdoptedBy {
		if p > 1 && p == st.ppid {
			return true
		}
	}
	return false
}

// orphanInterpreters are programs whose first argument is a script to run, so a
// script without the exec bit still counts when run through one of them.
var orphanInterpreters = map[string]bool{
	"sh": true, "bash": true, "dash": true, "zsh": true, "ash": true, "ksh": true,
	"python": true, "python3": true, "perl": true, "ruby": true, "node": true,
}

func orphanIsInterpreter(prog string) bool {
	b := filepath.Base(prog)
	if orphanInterpreters[b] {
		return true
	}
	return strings.HasPrefix(b, "python3.") || strings.HasPrefix(b, "python2")
}

// scriptInside reports whether the program (argv[0]) or the script run by an
// interpreter (argv[1]) is a regular file inside dirs. Argv[1] counts when argv[0]
// is a known interpreter or the file is executable; a data file passed to an
// unrelated tool such as grep or tail is not a script.
func (r *OrphanReaper) scriptInside(pid int, dirs []string) bool {
	argv := r.readCmdline(pid)
	cwd := r.readLink(pid, "cwd")
	for i, a := range argv[:min(len(argv), 2)] {
		p := a
		if p == "" || strings.HasPrefix(p, "-") {
			continue
		}
		if !filepath.IsAbs(p) {
			if cwd == "" {
				continue
			}
			p = filepath.Join(cwd, p)
		}
		if !orphanInside(p, dirs) {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if i == 0 || fi.Mode()&0o111 != 0 || orphanIsInterpreter(argv[0]) {
			return true
		}
	}
	return false
}

// evidenceFor returns the kind of evidence proving pid belongs to the
// session, or "" when there is none. It reads only /proc state. Only processes
// owned by the reaper's user qualify. Path evidence additionally requires the
// process to be orphaned; ancestry and environment evidence tie the process to
// the session directly.
func (r *OrphanReaper) evidenceFor(pid int, ev OrphanEvidence, dirs []string) string {
	if !r.ownedByUs(pid) {
		return ""
	}
	if len(dirs) > 0 {
		// A program or script that lives inside the session folders is proof
		// whatever its parent is. A bare cwd is weaker (an operator's shell or
		// editor can sit there), so it counts only once the process was orphaned.
		if orphanInside(r.readLink(pid, "exe"), dirs) {
			return "exe"
		}
		if r.scriptInside(pid, dirs) {
			return "script"
		}
		if r.orphaned(pid, ev) && orphanInside(r.readLink(pid, "cwd"), dirs) {
			return "cwd"
		}
	}
	if len(ev.RootPIDs) > 0 && r.ancestryHits(pid, ev.RootPIDs) {
		return "ancestry"
	}
	if len(ev.SessionID) >= orphanMinIDLen && r.envHasID(pid, ev.SessionID) {
		return "env"
	}
	return ""
}

func (r *OrphanReaper) ancestryHits(pid int, roots []int) bool {
	set := map[int]bool{}
	for _, p := range roots {
		if p > 1 {
			set[p] = true
		}
	}
	cur := pid
	for i := 0; i < 64; i++ {
		st, ok := r.readStat(cur)
		if !ok || st.ppid <= 1 {
			return false
		}
		if set[st.ppid] {
			return true
		}
		cur = st.ppid
	}
	return false
}

// envHasID reports whether any environment value equals the session id. The
// environment is read in memory and never returned or logged.
func (r *OrphanReaper) envHasID(pid int, id string) bool {
	b, err := os.ReadFile(filepath.Join(r.procRoot(), strconv.Itoa(pid), "environ"))
	if err != nil {
		return false
	}
	for _, kv := range strings.Split(string(b), "\x00") {
		if i := strings.IndexByte(kv, '='); i >= 0 && kv[i+1:] == id {
			return true
		}
	}
	return false
}

// Find scans the proc root and returns the live processes proven by evidence
// to belong to the session. Matching is never by name. The reaper's own
// process is never returned.
func (r *OrphanReaper) Find(ev OrphanEvidence) []OrphanProc {
	ents, err := os.ReadDir(r.procRoot())
	if err != nil {
		return nil
	}
	self := r.SelfPID
	if self == 0 {
		self = os.Getpid()
	}
	dirs := orphanDirs(ev.Dirs)
	var out []OrphanProc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		st, ok := r.readStat(pid)
		if !ok || r.gone(pid) {
			continue
		}
		kind := r.evidenceFor(pid, ev, dirs)
		if kind == "" {
			continue
		}
		out = append(out, OrphanProc{PID: pid, StartTime: st.start, Command: r.shortCommand(pid, dirs), Evidence: kind})
	}
	return out
}

// stillSame re-verifies, right now, that pid is the same process (start time)
// and that the evidence still holds.
func (r *OrphanReaper) stillSame(p OrphanProc, ev OrphanEvidence, dirs []string) bool {
	st, ok := r.readStat(p.PID)
	if !ok || st.start != p.StartTime {
		return false
	}
	return r.evidenceFor(p.PID, ev, dirs) != ""
}

func (r *OrphanReaper) waitGone(pid int, until time.Time, poll time.Duration) bool {
	for {
		if r.gone(pid) {
			return true
		}
		if !r.now().Before(until) {
			return false
		}
		r.sleep(poll)
	}
}

// Reap ends each given process by exact pid: polite signal, short wait,
// forced signal. Evidence and start time are re-verified immediately before
// each signal; a process that fails verification is left alone and reported
// as a survivor.
func (r *OrphanReaper) Reap(ev OrphanEvidence, procs []OrphanProc) OrphanReapResult {
	grace, settle, poll := r.durations()
	dirs := orphanDirs(ev.Dirs)
	var res OrphanReapResult
	for _, p := range procs {
		if p.PID <= 1 {
			res.Survivors = append(res.Survivors, p)
			continue
		}
		if r.gone(p.PID) {
			continue // already exited; nothing to signal
		}
		verify := func() bool { return r.stillSame(p, ev, dirs) }
		if !r.signalVerified(p.PID, syscall.SIGTERM, verify) {
			res.Survivors = append(res.Survivors, p)
			continue
		}
		if r.waitGone(p.PID, r.now().Add(grace), poll) {
			res.Ended = append(res.Ended, p)
			continue
		}
		if !r.signalVerified(p.PID, syscall.SIGKILL, verify) {
			res.Survivors = append(res.Survivors, p)
			continue
		}
		if r.waitGone(p.PID, r.now().Add(settle), poll) {
			res.Ended = append(res.Ended, p)
		} else {
			res.Survivors = append(res.Survivors, p)
		}
	}
	res.Count = len(res.Ended)
	return res
}

// EndOrphans finds and ends every process proven to belong to the session.
func (r *OrphanReaper) EndOrphans(ev OrphanEvidence) OrphanReapResult {
	return r.Reap(ev, r.Find(ev))
}

// RecordSessionEndReap appends one decision to the task bound to sessionID
// recording how many processes the session-end reap ended and their short
// commands (never the environment). It reports whether a task was found.
func (m *Manager) RecordSessionEndReap(sessionID string, count int, commands []string) bool {
	prdID, taskID := m.FindTaskBySessionID(sessionID)
	if prdID == "" {
		return false
	}
	note := fmt.Sprintf("session end reap: ended %d process(es) left behind by session %s", count, sessionID)
	if len(commands) > 0 {
		note += ": " + strings.Join(commands, "; ")
	}
	now := time.Now()
	return m.store.MutatePRD(prdID, func(p *PRD) bool {
		p.Decisions = append(p.Decisions, Decision{At: now, Kind: "session_end_reap", Actor: "autonomous", Note: note})
		for si := range p.Story {
			for ti := range p.Story[si].Tasks {
				if p.Story[si].Tasks[ti].ID == taskID {
					p.Story[si].Tasks[ti].History = append(p.Story[si].Tasks[ti].History, TaskHistoryEntry{At: now, Note: note})
				}
			}
		}
		return true
	})
}
