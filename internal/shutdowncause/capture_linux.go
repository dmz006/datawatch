//go:build linux

package shutdowncause

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// maxChainDepth bounds the ParentChain walk.
const maxChainDepth = 8

// linuxSiginfo mirrors the kernel siginfo_t layout for signals delivered
// with a sender (SIGTERM et al.): the first members of the sender union
// are si_pid and si_uid. Total size must match siginfo_t (128 bytes).
type linuxSiginfo struct {
	Signo int32
	Errno int32
	Code  int32
	SiPid int32
	SiUid int32
	Pad   [108]byte
}

func sigmaskOf(raw []syscall.Signal) unix.Sigset_t {
	var set unix.Sigset_t
	for _, s := range raw {
		set.Val[s/64] |= 1 << (uint32(s) % 64)
	}
	return set
}

// systemdJobTimeout bounds the systemctl probes.
const systemdJobTimeout = 2 * time.Second

// daemonUnit is the unit name probed by systemdStopJob.
const daemonUnit = "datawatch"

var (
	pendingMu      sync.Mutex
	pendingSiginfo = map[string]Cause{}
)

// Install blocks the given signals and watches them with a signalfd
// (golang.org/x/sys/unix) so that the kernel can hand us the full
// siginfo, including si_pid/si_uid. It returns a channel that delivers
// each captured signal; pass the received signal to Capture to obtain
// the full Cause (Source=signalinfo). Capture without a pending siginfo
// falls back to the /proc-based scan automatically.
//
// Limitation: this is only observably effective when the signals are
// held in the whole process (for example blocked before the Go runtime
// spawns threads). In a process where the Go runtime already handles
// SIGTERM/SIGINT itself, a per-thread mask cannot intercept delivery,
// so prefer the procscan fallback path that Capture already provides.
func Install(signals ...os.Signal) (<-chan os.Signal, error) {
	var raw []syscall.Signal
	names := map[int]os.Signal{}
	for _, s := range signals {
		sig, ok := s.(syscall.Signal)
		if !ok {
			continue
		}
		raw = append(raw, sig)
		names[int(sig)] = s
	}
	if len(names) == 0 {
		return nil, errors.New("shutdowncause: Install: no installable signals given")
	}
	set := sigmaskOf(raw)
	if err := unix.PthreadSigmask(unix.SIG_BLOCK, &set, nil); err != nil {
		return nil, fmt.Errorf("shutdowncause: Install: block signals: %w", err)
	}
	fd, err := unix.Signalfd(-1, &set, unix.SFD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("shutdowncause: Install: signalfd: %w", err)
	}
	ch := make(chan os.Signal, len(names)+1)
	go func() {
		defer close(ch)
		defer func() { _ = unix.Close(fd) }()
		infoSize := int(unsafe.Sizeof(linuxSiginfo{}))
		buf := make([]byte, infoSize)
		for {
			n, rerr := unix.Read(fd, buf)
			if n < infoSize || rerr != nil {
				return
			}
			cause, sig, ok := siginfoCause(buf[:n], names)
			if !ok {
				continue
			}
			pendingMu.Lock()
			pendingSiginfo[sig.String()] = cause
			pendingMu.Unlock()
			select {
			case ch <- sig:
			default:
				// Nobody is reading; drop the delivery. The signal is
				// held by the signalfd, so it will not kill the process.
			}
		}
	}()
	return ch, nil
}

func takePending(sigName string) (Cause, bool) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	c, ok := pendingSiginfo[sigName]
	if ok {
		delete(pendingSiginfo, sigName)
	}
	return c, ok
}

// siginfoCause converts a raw siginfo_t buffer (as would be read from a
// signalfd) into a Cause and the matching signal. ok is false if the
// buffer is too short to be a siginfo_t.
func siginfoCause(buf []byte, names map[int]os.Signal) (Cause, os.Signal, bool) {
	size := int(unsafe.Sizeof(linuxSiginfo{}))
	if len(buf) < size {
		return Cause{}, nil, false
	}
	si := *(*linuxSiginfo)(unsafe.Pointer(&buf[0]))
	sig, known := names[int(si.Signo)]
	if !known {
		sig = syscall.Signal(si.Signo)
	}
	cause := Cause{
		Signal: sig.String(),
		Source: SourceSignalInfo,
		Time:   time.Now(),
	}
	if si.SiPid > 0 {
		cause.SenderPID = int(si.SiPid)
		cause.SenderUID = strconv.FormatUint(uint64(si.SiUid), 10)
		if u, uerr := user.LookupId(cause.SenderUID); uerr == nil {
			cause.SenderUser = u.Username
		}
	}
	cause.ParentChain = buildParentChain()
	return cause, sig, true
}

func capturePlatform(sig os.Signal) Cause {
	name := ""
	if sig != nil {
		name = sig.String()
	}
	if c, ok := takePending(name); ok {
		return c
	}
	// Fallback: snapshot the process table at the moment of the signal.
	c := procScanCause()
	c.ParentChain = buildParentChain()
	return c
}

// CandidatesFor re-runs the live procscan for the given daemon pid and
// returns the candidate processes, for tests that verify (without a live
// signal) whether a process would be attributed as the stop sender.
func CandidatesFor(daemonPID int) []Candidate {
	raw := scanCandidates(daemonPID)
	out := make([]Candidate, 0, len(raw))
	for _, r := range raw {
		out = append(out, Candidate{PID: r.pid, Cmdline: r.cmdline})
	}
	return out
}

// candidateVerbs are command names treated as plausible stop senders.
var candidateVerbs = map[string]bool{
	"systemctl": true,
	"service":   true,
	"kill":      true,
	"pkill":     true,
	"killall":   true,
	"datawatch": true,
}

type candidateProc struct {
	pid     int
	uid     string
	user    string
	cmdline string
	// namesPID is true when the cmdline names the daemon's own pid as a
	// whole token: direct evidence, stronger than a unit-name match.
	namesPID bool
}

func procScanCause() Cause {
	var c Cause
	c.Source = SourceProcScan
	ownPID := os.Getpid()
	candidates := scanCandidates(ownPID)
	if len(candidates) > 0 {
		top := candidates[0]
		c.SenderPID = top.pid
		c.SenderUID = top.uid
		c.SenderUser = top.user
		c.Cmdline = top.cmdline
	}
	if job, ok := systemdStopJob(); ok {
		c.ServiceStopJob = job
		if c.SenderPID == 0 {
			c.Source = SourceServiceManager
		}
	}
	return c
}

// procSource is the process table as seen by the procscan: the candidate
// pids, a cmdline lookup and an identity lookup per pid. The default
// implementation reads the live proc filesystem; tests install a scoped one.
type procSource interface {
	pids() []int
	cmdline(pid int) (string, bool)
	identity(pid int) (uid, userName string)
}

// liveProcSource reads the live proc filesystem.
type liveProcSource struct{}

func (liveProcSource) pids() []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, aerr := strconv.Atoi(e.Name())
		if aerr != nil {
			continue
		}
		out = append(out, pid)
	}
	return out
}

func (liveProcSource) cmdline(pid int) (string, bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return "", false
	}
	return strings.Trim(strings.ReplaceAll(string(raw), "\x00", " "), " "), true
}

func (liveProcSource) identity(pid int) (string, string) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return "", ""
	}
	uid := ""
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			uid = parts[1]
		}
		break
	}
	if uid == "" {
		return "", ""
	}
	userName := ""
	if u, uerr := user.LookupId(uid); uerr == nil {
		userName = u.Username
	}
	return uid, userName
}

var (
	procSourceMu     sync.RWMutex
	activeProcSource procSource = liveProcSource{}
)

func currentProcSource() procSource {
	procSourceMu.RLock()
	defer procSourceMu.RUnlock()
	return activeProcSource
}

// installProcSource replaces the source used by the scan (test seam) and
// returns the previous one; resetProcSource restores the live default.
func installProcSource(s procSource) procSource {
	procSourceMu.Lock()
	defer procSourceMu.Unlock()
	prev := activeProcSource
	activeProcSource = s
	return prev
}

func resetProcSource() { installProcSource(liveProcSource{}) }

// scanCandidates lists process-table entries whose cmdline contains a
// service-control verb (or `datawatch stop|restart`) and names the
// datawatch unit or the daemon's own pid.
func scanCandidates(ownPID int) []candidateProc {
	src := currentProcSource()
	ownPIDStr := strconv.Itoa(ownPID)
	var out []candidateProc
	for _, pid := range src.pids() {
		if pid == ownPID {
			continue
		}
		cmdline, ok := src.cmdline(pid)
		if !ok {
			continue
		}
		if cmdline == "" || !looksLikeStopCommand(cmdline, ownPIDStr) {
			continue
		}
		uid, user := src.identity(pid)
		namesPID := hasPIDToken(strings.Fields(cmdline), ownPIDStr)
		out = append(out, candidateProc{pid: pid, uid: uid, user: user, cmdline: cmdline, namesPID: namesPID})
	}
	rankCandidates(out)
	return out
}

// rankCandidates orders candidates so the most specific evidence comes
// first: processes that name the daemon's own pid (for example
// `kill <pid>`) rank above processes that only name the datawatch unit
// (for example a `systemctl stop datawatch-other` for an unrelated unit
// whose name merely contains the word). Within each group lower pids
// come first, so the order is deterministic.
func rankCandidates(c []candidateProc) {
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].namesPID != c[j].namesPID {
			return c[i].namesPID
		}
		return c[i].pid < c[j].pid
	})
}

// hasPIDToken reports whether any field names the pid as a whole token:
// exact equality, or the "name=<pid>" form (as in `--pid=123`). Plain
// substring matching is deliberately avoided so that arguments containing
// merely the same digits inside a larger number do not match.
func hasPIDToken(fields []string, pid string) bool {
	for _, f := range fields {
		if f == pid || strings.HasSuffix(f, "="+pid) {
			return true
		}
	}
	return false
}

func looksLikeStopCommand(cmdline, ownPID string) bool {
	fields := strings.Fields(cmdline)
	var verb string
	for _, f := range fields {
		if base := filepath.Base(f); candidateVerbs[base] {
			verb = base
			break
		}
	}
	if verb == "" {
		return false
	}
	// Signal senders only count when they name our own pid.
	if verb == "kill" || verb == "pkill" || verb == "killall" {
		return hasPIDToken(fields, ownPID)
	}
	// systemctl / service / datawatch control verbs must carry a stop
	// intent, so that "datawatch serve", "systemctl show datawatch" and
	// the like do not match.
	if hasPIDToken(fields, ownPID) {
		return true
	}
	if !strings.Contains(strings.ToLower(cmdline), daemonUnit) {
		return false
	}
	for _, f := range fields {
		if f == "stop" || f == "restart" {
			return true
		}
	}
	return false
}

func procIdentity(pid int) (uid string, userName string) {
	return currentProcSource().identity(pid)
}

// buildParentChain walks the daemon's ancestry (including itself) up to
// maxChainDepth deep. Missing /proc entries shorten the chain.
func buildParentChain() []ProcInfo {
	chain := make([]ProcInfo, 0, maxChainDepth)
	pid := os.Getpid()
	for i := 0; i < maxChainDepth; i++ {
		if pid <= 0 {
			break
		}
		chain = append(chain, procInfoOf(pid))
		ppid := parentPID(pid)
		if ppid <= 0 || ppid == pid {
			break
		}
		pid = ppid
	}
	return chain
}

func procInfoOf(pid int) ProcInfo {
	info := ProcInfo{PID: pid}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err == nil {
		info.Cmdline = strings.Trim(strings.ReplaceAll(string(raw), "\x00", " "), " ")
	}
	_, user := procIdentity(pid)
	info.User = user
	return info
}

func parentPID(pid int) int {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "PPid:") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			v, _ := strconv.Atoi(parts[1])
			return v
		}
	}
	return 0
}

// systemdStopJob asks systemd whether a job is currently running for the
// datawatch unit (user scope first, then the system scope). It resolves
// `systemctl` by name via exec.LookPath and returns "", false when the
// binary is absent or no active job is reported.
func systemdStopJob() (string, bool) {
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return "", false
	}
	scopes := [][]string{{"--user"}, {}}
	for _, scope := range scopes {
		if job, ok := querySystemdJob(path, scope); ok {
			return job, true
		}
	}
	return "", false
}

func querySystemdJob(bin string, scope []string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), systemdJobTimeout)
	defer cancel()
	args := append([]string{}, scope...)
	args = append(args, "show", daemonUnit, "-p", "ActiveState", "-p", "Job")
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Job=") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Job="))
			if v != "" && !strings.EqualFold(v, "n/a") {
				return v, true
			}
		}
	}
	return "", false
}
