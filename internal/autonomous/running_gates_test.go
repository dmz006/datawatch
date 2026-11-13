package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newGateDrainManager(t *testing.T, minutes int) *Manager {
	t.Helper()
	cfg := DefaultConfig()
	cfg.GateDrainMinutes = minutes
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestRestartGatesReportListsRunningGates(t *testing.T) {
	m := newGateDrainManager(t, 10)
	empty := m.RestartInterruptions()
	if empty.RunningGates == nil || empty.RunningGateCnt != 0 {
		t.Fatalf("no gates: want empty non-nil list, got %#v / %d", empty.RunningGates, empty.RunningGateCnt)
	}
	ctx, done := m.trackGate(context.Background(), "prd1", "task9", "script:verify.sh")
	reportGatePID(ctx, 4242)
	rep := m.RestartInterruptions()
	if rep.RunningGateCnt != 1 || len(rep.RunningGates) != 1 {
		t.Fatalf("want 1 running gate, got %d (%v)", rep.RunningGateCnt, rep.RunningGates)
	}
	g := rep.RunningGates[0]
	if g.Check != "script:verify.sh" || g.PRDID != "prd1" || g.TaskID != "task9" || g.PID != 4242 {
		t.Fatalf("gate = %+v", g)
	}
	if txt := RestartInterruptionText(rep); !strings.Contains(txt, "script:verify.sh") || !strings.Contains(txt, "pid 4242") {
		t.Fatalf("report text omits the gate:\n%s", txt)
	}
	done()
	if rep := m.RestartInterruptions(); rep.RunningGateCnt != 0 || len(rep.RunningGates) != 0 {
		t.Fatalf("gate not cleared: %+v", rep.RunningGates)
	}
}

func TestGateDrainReturnsAsSoonAsLastGateEnds(t *testing.T) {
	m := newGateDrainManager(t, 10)
	_, done1 := m.trackGate(context.Background(), "p", "t1", "c1")
	_, done2 := m.trackGate(context.Background(), "p", "t2", "c2")
	clk := &restartFakeClock{now: time.Unix(1000, 0)}
	clk.onSleep = func(n int) {
		switch n {
		case 2:
			done1()
		case 4:
			done2()
		}
	}
	res := m.DrainGates(context.Background(), clk, nil)
	if res.Waited != 2 || res.Interrupted != 0 || res.TimedOut {
		t.Fatalf("result = %+v", res)
	}
	if clk.sleeps != 4 {
		t.Fatalf("slept %d times, want 4 (returns when the last gate ends)", clk.sleeps)
	}
	if !m.LaunchDraining() {
		t.Fatal("new gates must be held while waiting")
	}
}

func TestGateDrainTimesOutCleanlyAndReportsGatesInterrupted(t *testing.T) {
	m := newGateDrainManager(t, 2)
	_, done1 := m.trackGate(context.Background(), "p", "t1", "c1")
	_, done2 := m.trackGate(context.Background(), "p", "t2", "c2")
	defer done2()
	clk := &restartFakeClock{now: time.Unix(1000, 0)}
	clk.onSleep = func(n int) {
		if n == 3 {
			done1() // one ends, one never does
		}
	}
	var logs []string
	res := m.DrainGates(context.Background(), clk, func(f string, a ...any) { logs = append(logs, f) })
	if res.Waited != 2 || res.Interrupted != 1 || !res.TimedOut {
		t.Fatalf("result = %+v", res)
	}
	if res.Elapsed < 2*time.Minute || res.Elapsed > 2*time.Minute+time.Second {
		t.Fatalf("elapsed = %s, want about the 2-minute limit", res.Elapsed)
	}
	if len(m.RunningGates()) != 1 {
		t.Fatal("the unfinished gate must stay registered (never signalled)")
	}
	if len(logs) < 2 {
		t.Fatalf("expected a start and an end log line, got %v", logs)
	}
}

func TestGateDrainZeroMinutesAndNoGatesDoNotWait(t *testing.T) {
	off := newGateDrainManager(t, 0)
	clk := &restartFakeClock{now: time.Unix(1, 0)}
	if res := off.DrainGates(context.Background(), clk, nil); res != (GateDrainResult{}) || clk.sleeps != 0 {
		t.Fatalf("no gates: %+v sleeps=%d", res, clk.sleeps)
	}
	_, done := off.trackGate(context.Background(), "p", "t", "c")
	defer done()
	res := off.DrainGates(context.Background(), clk, nil)
	if res.Waited != 0 || res.Interrupted != 1 || clk.sleeps != 0 || off.LaunchDraining() {
		t.Fatalf("0 minutes with a gate running: %+v sleeps=%d draining=%v", res, clk.sleeps, off.LaunchDraining())
	}
}

func TestGateDrainCancelledContextReturnsCleanly(t *testing.T) {
	m := newGateDrainManager(t, 10)
	_, done := m.trackGate(context.Background(), "p", "t", "c")
	defer done()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := m.DrainGates(ctx, &restartFakeClock{now: time.Unix(1, 0)}, nil)
	if res.Waited != 1 || res.Interrupted != 1 {
		t.Fatalf("result = %+v", res)
	}
}
