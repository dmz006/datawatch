package resources

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WatchdogInterval is how often the group's resident memory is polled.
const WatchdogInterval = 500 * time.Millisecond

// Status says which memory-ceiling mechanism applies on this host.
type Status string

const (
	// StatusScope: a systemd user scope enforces the ceiling.
	StatusScope Status = "scope"
	// StatusWatchdog: the /proc watchdog enforces the ceiling.
	StatusWatchdog Status = "watchdog"
	// StatusNone: no ceiling can be enforced.
	StatusNone Status = "none"
)

// NoCeilingMessage is the text for the Events entry the caller must raise
// at start when Status is StatusNone.
const NoCeilingMessage = "no memory ceiling because this host has neither a systemd user scope nor /proc"

// ErrNoCeiling is returned by Watch when the status is StatusNone.
var ErrNoCeiling = errors.New(NoCeilingMessage)

// SelectStatus picks the mechanism from the scope probe and /proc presence.
func SelectStatus(scopeAvailable, procExists bool) Status {
	switch {
	case scopeAvailable:
		return StatusScope
	case procExists:
		return StatusWatchdog
	default:
		return StatusNone
	}
}

// NeedsEvent reports whether the caller must raise an Events entry at start.
func (s Status) NeedsEvent() bool { return s == StatusNone }

// Message is a human sentence for the status.
func (s Status) Message() string {
	switch s {
	case StatusScope:
		return "memory ceiling enforced by a systemd user scope"
	case StatusWatchdog:
		return "memory ceiling enforced by the /proc watchdog"
	default:
		return NoCeilingMessage
	}
}

// ProcInfo is one process of the watched group.
type ProcInfo struct {
	PID   int
	Name  string
	RSSKB int64 // current resident memory (VmRSS)
	HWMKB int64 // peak resident memory (VmHWM)
}

// ProcReader reads process-group memory. Tests inject a fake.
type ProcReader interface {
	// Exists reports whether process accounting is available.
	Exists() bool
	// Group lists the live members of process group pgid.
	Group(pgid int) ([]ProcInfo, error)
}

// Sample is one poll of the group.
type Sample struct {
	TotalRSSKB   int64
	LargestPID   int
	Largest      string
	LargestRSSKB int64
}

// TakeSample sums the group's resident memory and finds the largest process.
func TakeSample(procs []ProcInfo) Sample {
	var s Sample
	for _, p := range procs {
		s.TotalRSSKB += p.RSSKB
		if p.RSSKB > s.LargestRSSKB || s.LargestPID == 0 {
			s.LargestRSSKB, s.LargestPID, s.Largest = p.RSSKB, p.PID, p.Name
		}
	}
	return s
}

// Result is the outcome of one watched run.
type Result struct {
	Status      Status
	Killed      bool
	PeakKB      int64  // peak group memory seen (max of summed RSS / VmHWM)
	LargestName string // name of the largest process at the peak
	LargestPID  int
}

// ShouldKill decides whether the group is over the ceiling. A non-positive
// ceiling never kills.
func ShouldKill(totalRSSKB int64, ceilingMB int) bool {
	return ceilingMB > 0 && totalRSSKB > int64(ceilingMB)*1024
}

// SafeKillTarget reports whether pgid may be signalled: it must be a real
// group, never init's and never the daemon's own group.
func SafeKillTarget(pgid, daemonPgid int) bool {
	return pgid > 1 && pgid != daemonPgid
}

type tracker struct {
	ceilingMB int
	res       Result
}

// observe records one poll and returns true if the group is over the ceiling.
func (t *tracker) observe(procs []ProcInfo) bool {
	s := TakeSample(procs)
	var hwm int64
	for _, p := range procs {
		hwm += p.HWMKB
	}
	peak := s.TotalRSSKB
	if hwm > peak {
		peak = hwm
	}
	if peak > t.res.PeakKB {
		t.res.PeakKB = peak
		t.res.LargestName, t.res.LargestPID = s.Largest, s.LargestPID
	}
	return ShouldKill(s.TotalRSSKB, t.ceilingMB)
}

// WatchGroup polls pgid until done is closed or the ceiling is exceeded.
// On excess it calls kill(pgid) once — a group-only signal — after
// SafeKillTarget approves the target. interval <= 0 uses WatchdogInterval.
func WatchGroup(done <-chan struct{}, r ProcReader, pgid, daemonPgid, ceilingMB int,
	interval time.Duration, kill func(pgid int) error) Result {
	if interval <= 0 {
		interval = WatchdogInterval
	}
	t := &tracker{ceilingMB: ceilingMB, res: Result{Status: StatusWatchdog}}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if procs, err := r.Group(pgid); err == nil && t.observe(procs) {
			if SafeKillTarget(pgid, daemonPgid) && kill(pgid) == nil {
				t.res.Killed = true
			}
			return t.res
		}
		select {
		case <-done:
			return t.res
		case <-tick.C:
		}
	}
}

// Watch runs cmd in its own process group under the /proc watchdog. When
// the host has no /proc it returns StatusNone and ErrNoCeiling without
// starting the command; the caller raises an Events entry. The caller owns
// the scope probe: call Watch only when ScopeAvailable() is false.
func Watch(ctx context.Context, cmd *exec.Cmd, ceilingMB int) (Result, error) {
	r := ProcFS{Root: "/proc"}
	st := SelectStatus(false, r.Exists())
	if st == StatusNone {
		return Result{Status: StatusNone}, ErrNoCeiling
	}
	if ceilingMB <= 0 {
		return Result{Status: st}, errors.New("watchdog: ceiling must be a positive MB number")
	}
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return Result{Status: st}, err
	}
	pgid := cmd.Process.Pid // Setpgid with Pgid 0 makes the child its own leader
	dpg := daemonGroup()
	done := make(chan struct{})
	var res Result
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res = WatchGroup(done, r, pgid, dpg, ceilingMB, WatchdogInterval, killGroup)
	}()
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitErr:
	case <-ctx.Done():
		if SafeKillTarget(pgid, dpg) {
			_ = killGroup(pgid)
		}
		err = <-waitErr
	}
	close(done)
	wg.Wait()
	return res, err
}

// ProcFS reads a /proc-style tree rooted at Root.
type ProcFS struct{ Root string }

// Exists reports whether Root looks like a /proc tree.
func (p ProcFS) Exists() bool {
	_, err := os.Stat(filepath.Join(p.Root, "self", "status"))
	return err == nil
}

// Group lists processes whose process group is pgid.
func (p ProcFS) Group(pgid int) ([]ProcInfo, error) {
	ents, err := os.ReadDir(p.Root)
	if err != nil {
		return nil, err
	}
	var out []ProcInfo
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		g, ok := readPgrp(filepath.Join(p.Root, e.Name(), "stat"))
		if !ok || g != pgid {
			continue
		}
		info, ok := readStatus(filepath.Join(p.Root, e.Name(), "status"))
		if !ok {
			continue
		}
		info.PID = pid
		out = append(out, info)
	}
	return out, nil
}

// readPgrp parses the process group from a stat file; comm may contain
// spaces and parentheses, so split after the last ')'.
func readPgrp(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 3 { // state ppid pgrp
		return 0, false
	}
	g, err := strconv.Atoi(f[2])
	return g, err == nil
}

func readStatus(path string) (ProcInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return ProcInfo{}, false
	}
	defer func() { _ = f.Close() }()
	var p ProcInfo
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "Name":
			p.Name = v
		case "VmRSS":
			p.RSSKB = parseKB(v)
		case "VmHWM":
			p.HWMKB = parseKB(v)
		}
	}
	return p, true
}

func parseKB(v string) int64 {
	n, _ := strconv.ParseInt(strings.Fields(v + " ")[0], 10, 64)
	return n
}

// String formats a result for logs.
func (r Result) String() string {
	return fmt.Sprintf("status=%s killed=%t peak_kb=%d largest=%s(%d)",
		r.Status, r.Killed, r.PeakKB, r.LargestName, r.LargestPID)
}
