package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type hostLoadFixture struct {
	pid  int
	root string
}

// startHostLoadFixture starts a harmless sleeping script in its own session
// inside a fake worktree under a temp directory. It is ended by exact pid and
// process group on cleanup.
func startHostLoadFixture(t *testing.T) hostLoadFixture {
	t.Helper()
	t.Setenv("TMUX", "")
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", "")
	if err := os.Unsetenv("TMUX_TMPDIR"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	wt := filepath.Join(root, "worktree")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(wt, "work.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Dir = wt
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start fixture: %v", err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL) // own session: group id equals pid
	})
	for deadline := time.Now().Add(10 * time.Second); !hostLoadAlive(pid) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	return hostLoadFixture{pid: pid, root: wt}
}

func hostLoadAlive(pid int) bool {
	b, err := os.ReadFile("/proc/" + itoaHL(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i >= 0 && i+2 < len(s) && s[i+2] != 'Z'
}

func itoaHL(n int) string { return strconv.Itoa(n) }

type hostLoadHarness struct {
	now    time.Time
	load   float64
	alerts []string
	check  *HostLoadCheck
}

func newHostLoadHarness(cfg HostLoadConfig, sessions []HostLoadSession) *hostLoadHarness {
	h := &hostLoadHarness{now: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	h.check = &HostLoadCheck{
		Config:   func() HostLoadConfig { return cfg },
		Now:      func() time.Time { return h.now },
		Load:     func() (float64, error) { return h.load, nil },
		Cores:    func() int { return 4 },
		Sessions: func() []HostLoadSession { return sessions },
		Alert:    func(title, msg string) { h.alerts = append(h.alerts, title+": "+msg) },
		Reaper:   &OrphanReaper{Grace: time.Second},
		// The fixture is idle, so name every group rather than only the busiest.
		TopGroups: 100000,
	}
	return h
}

func TestHostLoadDefaults(t *testing.T) {
	var c HostLoadConfig
	if c.EffectiveMode() != HostLoadModeAlert || c.EffectiveMultiple() != 1.5 || c.EffectiveMinutes() != 10 {
		t.Fatalf("defaults wrong: %v %v %v", c.EffectiveMode(), c.EffectiveMultiple(), c.EffectiveMinutes())
	}
}

func TestHostLoadSustainedBreachAlertsOncePerEpisode(t *testing.T) {
	h := newHostLoadHarness(HostLoadConfig{}, nil)
	h.load = 7 // limit is 1.5 * 4 = 6
	if r := h.check.Check(); !r.Breached || r.Alerted {
		t.Fatalf("first breach must not alert yet: %+v", r)
	}
	h.now = h.now.Add(9 * time.Minute)
	if r := h.check.Check(); r.Alerted {
		t.Fatal("alerted before the configured minutes")
	}
	h.now = h.now.Add(2 * time.Minute)
	if r := h.check.Check(); !r.Alerted || len(h.alerts) != 1 {
		t.Fatalf("expected one alert: %+v %v", r, h.alerts)
	}
	h.now = h.now.Add(time.Minute)
	h.check.Check()
	if len(h.alerts) != 1 {
		t.Fatal("alert repeated within one episode")
	}
	// Recovery then a new episode.
	h.load = 1
	h.check.Check()
	h.load = 9
	h.check.Check()
	h.now = h.now.Add(11 * time.Minute)
	h.check.Check()
	if len(h.alerts) != 2 {
		t.Fatalf("expected a second alert after recovery, got %d", len(h.alerts))
	}
}

func TestHostLoadDipResetsSustainedWindow(t *testing.T) {
	h := newHostLoadHarness(HostLoadConfig{Minutes: 5}, nil)
	h.load = 10
	h.check.Check()
	h.now = h.now.Add(4 * time.Minute)
	h.load = 2
	h.check.Check()
	h.load = 10
	h.now = h.now.Add(2 * time.Minute)
	if r := h.check.Check(); r.Alerted {
		t.Fatal("window must restart after a dip")
	}
}

func TestHostLoadOffModeDoesNothing(t *testing.T) {
	h := newHostLoadHarness(HostLoadConfig{Mode: HostLoadModeOff}, nil)
	h.load = 100
	h.check.Check()
	h.now = h.now.Add(time.Hour)
	if r := h.check.Check(); r.Breached || len(h.alerts) != 0 {
		t.Fatalf("off mode acted: %+v", r)
	}
}

func TestHostLoadAlertNamesGroupsAndOwner(t *testing.T) {
	fx := startHostLoadFixture(t)
	for _, running := range []bool{true, false} {
		sess := []HostLoadSession{{ID: "sess-1234", Running: running, WorktreeDirs: []string{fx.root}}}
		h := newHostLoadHarness(HostLoadConfig{Minutes: 1}, sess)
		h.load = 50
		h.check.Check()
		h.now = h.now.Add(2 * time.Minute)
		r := h.check.Check()
		if !r.Alerted || len(h.alerts) != 1 {
			t.Fatalf("no alert: %+v", r)
		}
		state := "ended"
		if running {
			state = "running"
		}
		want := "worktree folder of " + state + " Automaton session sess-1234"
		if !strings.Contains(h.alerts[0], want) {
			t.Fatalf("alert %q lacks %q", h.alerts[0], want)
		}
		if !hostLoadAlive(fx.pid) {
			t.Fatal("alert mode must not end anything")
		}
	}
}

func TestHostLoadReapEndsOnlyEndedSessionProcesses(t *testing.T) {
	running := startHostLoadFixture(t)
	ended := startHostLoadFixture(t)
	sess := []HostLoadSession{
		{ID: "run-0001", Running: true, WorktreeDirs: []string{running.root}},
		{ID: "end-0002", Running: false, WorktreeDirs: []string{ended.root}},
	}
	h := newHostLoadHarness(HostLoadConfig{Mode: HostLoadModeReap, Minutes: 1}, sess)
	h.load = 50
	h.check.Check()
	h.now = h.now.Add(2 * time.Minute)
	r := h.check.Check()
	if !r.Alerted || r.Reaped.Count < 1 {
		t.Fatalf("reap did not end the ended-session process: %+v", r.Reaped)
	}
	if hostLoadAlive(ended.pid) {
		t.Fatal("ended-session process still alive")
	}
	if !hostLoadAlive(running.pid) {
		t.Fatal("running-session process was ended")
	}
}

func TestHostLoadReapIgnoresUnprovenProcess(t *testing.T) {
	fx := startHostLoadFixture(t)
	other := t.TempDir()
	sess := []HostLoadSession{{ID: "end-0003", Running: false, WorktreeDirs: []string{other}}}
	h := newHostLoadHarness(HostLoadConfig{Mode: HostLoadModeReap, Minutes: 1}, sess)
	h.load = 50
	h.check.Check()
	h.now = h.now.Add(2 * time.Minute)
	h.check.Check()
	if !hostLoadAlive(fx.pid) {
		t.Fatal("process outside the session folders was ended")
	}
}

// Monitoring snapshot tests (Status) — the check feeds GET /api/stats, the
// autonomous status (and the autonomous_status MCP tool) and the
// datawatch_host_load_* Prometheus gauges from this one snapshot.

func TestHostLoadStatusTracksFakeLoad(t *testing.T) {
	h := newHostLoadHarness(HostLoadConfig{Minutes: 1}, nil)
	// A reading over the limit (1.5 x 4 = 6) records immediately; the alert
	// still waits for the sustained window, but the status shows the breach.
	h.load = 7
	if r := h.check.Check(); !r.Breached || r.Alerted {
		t.Fatalf("first over-limit reading: %+v", r)
	}
	st := h.check.Status()
	if st.Mode != string(HostLoadModeAlert) || st.LastLoad != 7 || st.LastLimit != 6 || !st.Breached {
		t.Fatalf("status after over-limit reading: %+v", st)
	}
	if !st.LastCheckAt.Equal(h.now) {
		t.Fatalf("LastCheckAt = %v, want %v", st.LastCheckAt, h.now)
	}
	// A recovered reading flips the status back under the limit.
	h.load = 1
	h.check.Check()
	if st = h.check.Status(); st.Breached || st.LastLoad != 1 {
		t.Fatalf("status after recovery: %+v", st)
	}
}

func TestHostLoadStatusCountsReapedOrphans(t *testing.T) {
	fx := startHostLoadFixture(t)
	sess := []HostLoadSession{{ID: "end-0200", Running: false, WorktreeDirs: []string{fx.root}}}
	h := newHostLoadHarness(HostLoadConfig{Mode: HostLoadModeReap, Minutes: 1}, sess)
	h.load = 50
	h.check.Check()
	h.now = h.now.Add(2 * time.Minute)
	if r := h.check.Check(); r.Reaped.Count < 1 {
		t.Fatalf("reap did not end the ended-session process: %+v", r)
	}
	st := h.check.Status()
	if st.Mode != string(HostLoadModeReap) || st.OrphansReaped < 1 || !st.Breached {
		t.Fatalf("status after reap: %+v", st)
	}
}

func TestHostLoadStatusOffModeIsEmpty(t *testing.T) {
	h := newHostLoadHarness(HostLoadConfig{Mode: HostLoadModeOff}, nil)
	h.load = 50
	h.check.Check()
	st := h.check.Status()
	if st.Mode != string(HostLoadModeOff) || !st.LastCheckAt.IsZero() || st.LastLoad != 0 || st.LastLimit != 0 {
		t.Fatalf("off status must carry no readings: %+v", st)
	}
}

func TestManagerStatusCarriesHostLoad(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// No provider: the field stays absent (no host load check wired).
	if got := m.Status().HostLoad; got != nil {
		t.Fatalf("HostLoad without provider = %+v, want nil", got)
	}
	snap := HostLoadStatus{Mode: string(HostLoadModeReap), LastLoad: 7, LastLimit: 6, Breached: true, OrphansReaped: 2}
	m.SetHostLoadProvider(func() HostLoadStatus { return snap })
	if got := m.Status().HostLoad; got == nil || *got != snap {
		t.Fatalf("HostLoad with provider = %+v, want %+v", got, snap)
	}
}
