package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type fakeRestartMgr struct {
	running int
	drain   []bool
}

func (f *fakeRestartMgr) RestartInterruptions() autonomous.RestartInterruption {
	r := autonomous.RestartInterruption{RunningTasks: []autonomous.InterruptedTask{}, LiveSessions: []string{}}
	for i := 0; i < f.running; i++ {
		r.RunningTasks = append(r.RunningTasks, autonomous.InterruptedTask{PRDID: "p", TaskID: "t"})
	}
	r.RunningTaskCnt = f.running
	return r
}
func (f *fakeRestartMgr) SetLaunchDraining(on bool) { f.drain = append(f.drain, on) }

type stepClock struct {
	now  time.Time
	hook func()
}

func (c *stepClock) Now() time.Time { return c.now }
func (c *stepClock) Sleep(_ context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	if c.hook != nil {
		c.hook()
	}
	return nil
}

func TestRestartSettingsDefaultToImmediateProceed(t *testing.T) {
	s := restartSettingsFrom(config.AutonomousConfig{})
	if s.Policy != "immediate" || s.OnTimeout != "proceed" || s.QuietTimeout != 600*time.Second {
		t.Fatalf("defaults = %+v", s)
	}
}

func TestRunRestartPolicyNoManagerProceeds(t *testing.T) {
	dec := runRestartPolicy(context.Background(), nil, restartSettingsFrom(config.AutonomousConfig{RestartPolicy: "drain"}), autonomous.RealClock(), nil)
	if !dec.Proceed {
		t.Fatalf("no manager means nothing to wait for: %+v", dec)
	}
}

func TestGuardRestartRunsRestartOnlyWhenPolicyAllows(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config.AutonomousConfig
		running int
		want    bool
	}{
		{"immediate with work running", config.AutonomousConfig{}, 2, true},
		{"wait_quiet times out, abort", config.AutonomousConfig{RestartPolicy: "wait_quiet", RestartOnTimeout: "abort", RestartQuietTimeoutSeconds: 1}, 2, false},
		{"wait_quiet times out, proceed", config.AutonomousConfig{RestartPolicy: "wait_quiet", RestartOnTimeout: "proceed", RestartQuietTimeoutSeconds: 1}, 2, true},
		{"wait_quiet already quiet", config.AutonomousConfig{RestartPolicy: "wait_quiet", RestartOnTimeout: "abort"}, 0, true},
	}
	for _, c := range cases {
		mgr := &fakeRestartMgr{running: c.running}
		clk := &stepClock{now: time.Unix(0, 0)}
		dec := runRestartPolicy(context.Background(), mgr, restartSettingsFrom(c.cfg), clk, nil)
		if dec.Proceed != c.want {
			t.Errorf("%s: proceed = %v, want %v (%+v)", c.name, dec.Proceed, c.want, dec)
		}
		if dec.Initial.RunningTaskCnt != c.running {
			t.Errorf("%s: count returned = %d, want %d", c.name, dec.Initial.RunningTaskCnt, c.running)
		}
	}
}

func TestGuardRestartDrainRestartsAfterTasksFinish(t *testing.T) {
	mgr := &fakeRestartMgr{running: 1}
	clk := &stepClock{now: time.Unix(0, 0)}
	clk.hook = func() { mgr.running = 0 }
	dec := runRestartPolicy(context.Background(), mgr, restartSettingsFrom(config.AutonomousConfig{RestartPolicy: "drain"}), clk, nil)
	if !dec.Proceed || len(mgr.drain) != 1 || !mgr.drain[0] {
		t.Fatalf("drain: %+v gate=%v", dec, mgr.drain)
	}
}

func TestGuardRestartCallsRestartOnlyOnProceed(t *testing.T) {
	calls := 0
	mgr := &fakeRestartMgr{running: 0}
	g := guardRestart(func() { calls++ }, func() restartPolicyManager { return mgr },
		func() restartPolicySettings { return restartSettingsFrom(config.AutonomousConfig{}) })
	g()
	if calls != 1 {
		t.Fatalf("restart called %d times, want 1", calls)
	}
}

type fakeGateMgr struct {
	fakeRestartMgr
	res   autonomous.GateDrainResult
	calls int
}

func (f *fakeGateMgr) DrainGates(_ context.Context, _ autonomous.Clock, _ func(string, ...any)) autonomous.GateDrainResult {
	f.calls++
	return f.res
}

func TestRestartGatesGuardReportListsRunningGates(t *testing.T) {
	rep := autonomous.RestartInterruption{
		RunningGates:   []autonomous.RunningGate{{Check: "script:verify.sh", PRDID: "p1", TaskID: "t2", PID: 77}},
		RunningGateCnt: 1,
	}
	out := restartGuardReportWith(rep, "datawatch restart --force") // service-scope-ok fixture string: expected report text, never executed
	if !strings.Contains(out, "running gates (1)") || !strings.Contains(out, "script:verify.sh") || !strings.Contains(out, "pid 77") {
		t.Fatalf("report omits the gate:\n%s", out)
	}
	if !strings.Contains(restartGuardReportWith(autonomous.RestartInterruption{}, "x"), "running gates (0)") {
		t.Fatal("report must show an empty gate list")
	}
}

func TestGateDrainBeforeExitUsesManagerAndToleratesNone(t *testing.T) {
	if res := drainGatesBeforeExit(context.Background(), nil, autonomous.RealClock(), nil); res != (autonomous.GateDrainResult{}) {
		t.Fatalf("nil manager: %+v", res)
	}
	plain := &fakeRestartMgr{}
	if res := drainGatesBeforeExit(context.Background(), plain, autonomous.RealClock(), nil); res != (autonomous.GateDrainResult{}) {
		t.Fatalf("manager without the wait: %+v", res)
	}
	g := &fakeGateMgr{res: autonomous.GateDrainResult{Waited: 2, Interrupted: 1}}
	if res := drainGatesBeforeExit(context.Background(), g, autonomous.RealClock(), nil); res.Waited != 2 || res.Interrupted != 1 || g.calls != 1 {
		t.Fatalf("result = %+v calls=%d", res, g.calls)
	}
}

func TestGatesInterruptedRestartRunsGateWaitBeforeExit(t *testing.T) {
	g := &fakeGateMgr{res: autonomous.GateDrainResult{Waited: 1}}
	exited := 0
	guard := guardRestartForce(func(bool) {
		if g.calls != 1 {
			t.Errorf("restart ran before the gate wait (calls=%d)", g.calls)
		}
		exited++
	}, func() restartPolicyManager { return g }, func() restartPolicySettings { return restartSettingsFrom(config.AutonomousConfig{}) })
	guard(true)
	if exited != 1 || g.calls != 1 {
		t.Fatalf("exited=%d calls=%d", exited, g.calls)
	}
}
