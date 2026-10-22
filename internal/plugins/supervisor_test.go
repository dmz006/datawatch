//go:build !windows

package plugins

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The supervisor tests drive a real child process (testdata/svchelper)
// instead of mocking os/exec, so process groups, signals, reaping and
// the HTTP health probe are all exercised for real.

var (
	svcHelperDir  string
	svcHelperOnce sync.Once
	svcHelperPath string
	svcHelperErr  error
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "svchelper-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdir temp:", err)
		os.Exit(1)
	}
	svcHelperDir = dir
	defer os.RemoveAll(dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// buildSvcHelper compiles testdata/svchelper once per test binary.
func buildSvcHelper(t *testing.T) string {
	t.Helper()
	svcHelperOnce.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			svcHelperErr = fmt.Errorf("go toolchain not found: %w", err)
			return
		}
		out := filepath.Join(svcHelperDir, "svchelper")
		cmd := exec.Command(goBin, "build", "-o", out, "./testdata/svchelper") // #nosec G204 -- test-only build of an in-repo helper
		if b, err := cmd.CombinedOutput(); err != nil {
			svcHelperErr = fmt.Errorf("build svchelper: %v\n%s", err, b)
			return
		}
		svcHelperPath = out
	})
	if svcHelperErr != nil {
		t.Skipf("svchelper unavailable: %v", svcHelperErr)
	}
	return svcHelperPath
}

// freePort returns a currently unused 127.0.0.1 TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// servicePlugin builds a managed service plugin running svchelper on port.
func servicePlugin(t *testing.T, port int, policy string, stopTimeoutMs int, args ...string) *Plugin {
	t.Helper()
	return &Plugin{
		Manifest: Manifest{
			Name: "svc-test",
			Kind: KindService,
			Connection: &PluginConnection{
				BaseURL:    "http://127.0.0.1:" + strconv.Itoa(port),
				HealthPath: "/healthz",
			},
			Lifecycle: &PluginLifecycle{
				SupportedModes: []string{LifecycleManaged},
				Entry:          buildSvcHelper(t),
				Args:           append([]string{"-port", strconv.Itoa(port)}, args...),
				RestartPolicy:  policy,
				StopTimeoutMs:  stopTimeoutMs,
			},
		},
		Dir: t.TempDir(),
	}
}

// eventLog records supervisor events; safe for concurrent use.
type eventLog struct {
	mu     sync.Mutex
	events []ServiceEvent
}

func (l *eventLog) add(ev ServiceEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, ev)
}

func (l *eventLog) count(action string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, ev := range l.events {
		if ev.Action == action {
			n++
		}
	}
	return n
}

func (l *eventLog) last(action string) (ServiceEvent, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.events) - 1; i >= 0; i-- {
		if l.events[i].Action == action {
			return l.events[i], true
		}
	}
	return ServiceEvent{}, false
}

// pids returns every child PID reported by a start event.
func (l *eventLog) pids() []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []int
	for _, ev := range l.events {
		if ev.Action == EventServiceStart {
			if pid, ok := ev.Details["pid"].(int); ok {
				out = append(out, pid)
			}
		}
	}
	return out
}

// newTestSupervisor builds a supervisor with test-sized timings and an
// event log, and stops it at test end.
func newTestSupervisor(t *testing.T, p *Plugin) (*ServiceSupervisor, *eventLog) {
	t.Helper()
	s, err := NewServiceSupervisor(p, nil)
	if err != nil {
		t.Fatalf("NewServiceSupervisor: %v", err)
	}
	s.HealthInterval = 50 * time.Millisecond
	s.StartupGrace = 2 * time.Second
	s.StableAfter = time.Minute
	s.MaxRestarts = 10
	s.BackoffBase = 50 * time.Millisecond
	s.BackoffMax = 200 * time.Millisecond
	s.HTTPClient.Timeout = 500 * time.Millisecond
	log := &eventLog{}
	s.OnEvent = log.add
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	})
	return s, log
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForState polls until the supervisor reaches want.
func waitForState(t *testing.T, s *ServiceSupervisor, want ServiceState, timeout time.Duration) ServiceStatus {
	t.Helper()
	var st ServiceStatus
	waitFor(t, timeout, "state "+string(want), func() bool {
		st = s.Status()
		return st.State == want
	})
	return st
}

// waitForLoopExit waits until the supervision loop has ended on its own.
func waitForLoopExit(t *testing.T, s *ServiceSupervisor, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, "supervision loop to exit", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.running
	})
}

// assertGone fails when pid still names a live (or unreaped) process.
func assertGone(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		t.Fatalf("invalid pid %d", pid)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("pid %d still exists: kill(pid, 0) = %v, want ESRCH", pid, err)
	}
}

func stopWithin(t *testing.T, s *ServiceSupervisor, timeout time.Duration) time.Duration {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	begin := time.Now()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	return time.Since(begin)
}

func TestServiceSupervisor_StartBecomesHealthy(t *testing.T) {
	port := freePort(t)
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartOnFailure, 2000))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st := waitForState(t, s, StateHealthy, 5*time.Second)
	if st.PID <= 0 || st.HealthyAt.IsZero() || st.StartedAt.IsZero() {
		t.Errorf("healthy status incomplete: %+v", st)
	}
	if st.Restarts != 0 || st.ConsecutiveFailures != 0 {
		t.Errorf("restarts/failures = %d/%d, want 0/0", st.Restarts, st.ConsecutiveFailures)
	}
	ev, ok := log.last(EventServiceStart)
	if !ok {
		t.Fatalf("no %s event", EventServiceStart)
	}
	if ev.Plugin != "svc-test" || ev.Details["pid"] != st.PID {
		t.Errorf("start event = %+v, want plugin svc-test pid %d", ev, st.PID)
	}
	if err := s.Start(context.Background()); err == nil {
		t.Error("second Start while running: want error")
	}
	if n := log.count(EventServiceHealthFail); n != 0 {
		t.Errorf("health_fail events = %d, want 0", n)
	}
}

func TestServiceSupervisor_HealthyToCrashedOnExit(t *testing.T) {
	port := freePort(t)
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartNever, 2000,
		"-exit-after", "600ms", "-exit-code", "1"))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := waitForState(t, s, StateHealthy, 5*time.Second).PID
	st := waitForState(t, s, StateCrashed, 5*time.Second)
	if st.LastExit != 1 {
		t.Errorf("LastExit = %d, want 1", st.LastExit)
	}
	if !strings.Contains(st.LastError, "exit status 1") {
		t.Errorf("LastError = %q, want it to mention exit status 1", st.LastError)
	}
	if st.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", st.ConsecutiveFailures)
	}
	waitForLoopExit(t, s, 2*time.Second)
	assertGone(t, pid)
	ev, ok := log.last(EventServiceStop)
	if !ok {
		t.Fatalf("no %s event after crash", EventServiceStop)
	}
	if ev.Details["reason"] != "exited" || ev.Details["exit_code"] != 1 {
		t.Errorf("stop event details = %v, want reason=exited exit_code=1", ev.Details)
	}
}

func TestServiceSupervisor_OnFailureRestartsAfterNonZeroExit(t *testing.T) {
	port := freePort(t)
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartOnFailure, 2000,
		"-exit-after", "500ms", "-exit-code", "3"))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	first := waitForState(t, s, StateHealthy, 5*time.Second).PID
	waitFor(t, 10*time.Second, "healthy again after a restart", func() bool {
		st := s.Status()
		return st.State == StateHealthy && st.Restarts >= 1 && st.PID != first
	})
	if n := log.count(EventServiceRestart); n < 1 {
		t.Fatalf("restart events = %d, want >= 1", n)
	}
	ev, _ := log.last(EventServiceRestart)
	if ev.Details["exit_code"] != 3 {
		t.Errorf("restart event exit_code = %v, want 3", ev.Details["exit_code"])
	}
	if ev.Details["health_fail"] != false {
		t.Errorf("restart event health_fail = %v, want false", ev.Details["health_fail"])
	}
	stopWithin(t, s, 5*time.Second)
	if st := s.Status(); st.State != StateStopped {
		t.Errorf("state after Stop = %s, want stopped", st.State)
	}
	for _, pid := range log.pids() {
		assertGone(t, pid)
	}
}

func TestServiceSupervisor_OnFailureCleanExitDoesNotRestart(t *testing.T) {
	port := freePort(t)
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartOnFailure, 2000,
		"-exit-after", "400ms", "-exit-code", "0"))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := waitForState(t, s, StateHealthy, 5*time.Second).PID
	waitForLoopExit(t, s, 5*time.Second)
	st := s.Status()
	if st.State != StateStopped {
		t.Errorf("state = %s, want stopped (clean exit under on-failure)", st.State)
	}
	if st.LastExit != 0 || st.Restarts != 0 || st.ConsecutiveFailures != 0 {
		t.Errorf("status = %+v, want exit 0, no restarts, no failures", st)
	}
	if n := log.count(EventServiceRestart); n != 0 {
		t.Errorf("restart events = %d, want 0", n)
	}
	if n := log.count(EventServiceStart); n != 1 {
		t.Errorf("start events = %d, want 1", n)
	}
	assertGone(t, pid)
}

func TestServiceSupervisor_AlwaysRestartsAfterCleanExit(t *testing.T) {
	port := freePort(t)
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartAlways, 2000,
		"-exit-after", "300ms", "-exit-code", "0"))
	s.MaxRestarts = 1 // clean exits are not failures, so this must not trigger give-up

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 10*time.Second, "two restarts", func() bool { return log.count(EventServiceRestart) >= 2 })
	st := s.Status()
	if st.State == StateFailed || st.ConsecutiveFailures != 0 {
		t.Errorf("status = %+v, want no failures under always + exit 0", st)
	}
	ev, _ := log.last(EventServiceRestart)
	if ev.Details["exit_code"] != 0 {
		t.Errorf("restart exit_code = %v, want 0", ev.Details["exit_code"])
	}
	stopWithin(t, s, 5*time.Second)
	if st := s.Status(); st.State != StateStopped {
		t.Errorf("state after Stop = %s, want stopped", st.State)
	}
	for _, pid := range log.pids() {
		assertGone(t, pid)
	}
}

func TestServiceSupervisor_NeverDoesNotRestart(t *testing.T) {
	for _, tc := range []struct {
		code string
		want ServiceState
	}{
		{"1", StateCrashed},
		{"0", StateStopped},
	} {
		t.Run("exit_"+tc.code, func(t *testing.T) {
			port := freePort(t)
			s, log := newTestSupervisor(t, servicePlugin(t, port, RestartNever, 2000,
				"-exit-after", "300ms", "-exit-code", tc.code))

			if err := s.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			waitForLoopExit(t, s, 5*time.Second)
			if st := s.Status(); st.State != tc.want {
				t.Errorf("state = %s, want %s", st.State, tc.want)
			}
			// Several backoff periods later there is still exactly one launch.
			time.Sleep(4 * s.BackoffBase)
			if n := log.count(EventServiceRestart); n != 0 {
				t.Errorf("restart events = %d, want 0", n)
			}
			if n := log.count(EventServiceStart); n != 1 {
				t.Errorf("start events = %d, want 1", n)
			}
			for _, pid := range log.pids() {
				assertGone(t, pid)
			}
		})
	}
}

func TestServiceSupervisor_HealthNeverPassesGivesUp(t *testing.T) {
	for _, mode := range []string{"fail", "never"} {
		t.Run(mode, func(t *testing.T) {
			port := freePort(t)
			s, log := newTestSupervisor(t, servicePlugin(t, port, RestartOnFailure, 2000, "-health", mode))
			s.StartupGrace = 400 * time.Millisecond
			s.MaxRestarts = 2

			if err := s.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			waitForLoopExit(t, s, 10*time.Second)
			st := s.Status()
			if st.State != StateFailed {
				t.Errorf("state = %s, want failed", st.State)
			}
			if st.Restarts != s.MaxRestarts {
				t.Errorf("Restarts = %d, want exactly MaxRestarts (%d)", st.Restarts, s.MaxRestarts)
			}
			if !strings.Contains(st.LastError, "health check failed") {
				t.Errorf("LastError = %q, want a health failure", st.LastError)
			}
			if n := log.count(EventServiceHealthFail); n != s.MaxRestarts+1 {
				t.Errorf("health_fail events = %d, want %d", n, s.MaxRestarts+1)
			}
			if n := log.count(EventServiceRestart); n != s.MaxRestarts {
				t.Errorf("restart events = %d, want %d", n, s.MaxRestarts)
			}
			ev, ok := log.last(EventServiceStop)
			if !ok || ev.Details["reason"] != "max_restarts" {
				t.Errorf("final stop event = %+v, want reason max_restarts", ev)
			}
			hf, _ := log.last(EventServiceHealthFail)
			if hf.Details["reason"] != "startup_deadline" {
				t.Errorf("health_fail reason = %v, want startup_deadline", hf.Details["reason"])
			}
			pids := log.pids()
			if len(pids) != s.MaxRestarts+1 {
				t.Errorf("launched %d processes, want %d", len(pids), s.MaxRestarts+1)
			}
			for _, pid := range pids {
				assertGone(t, pid)
			}
		})
	}
}

func TestServiceSupervisor_HealthyThenProbeFailures(t *testing.T) {
	// A service that becomes healthy and then stops answering is killed
	// after HealthFailThreshold failed probes and restarted.
	port := freePort(t)
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartOnFailure, 2000, "-fail-after", "500ms"))
	s.HealthFailThreshold = 2

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, s, StateHealthy, 5*time.Second)
	waitFor(t, 5*time.Second, "probe_failures health event", func() bool {
		ev, ok := log.last(EventServiceHealthFail)
		return ok && ev.Details["reason"] == "probe_failures"
	})
	waitFor(t, 5*time.Second, "restart after probe failures", func() bool { return log.count(EventServiceRestart) >= 1 })
	ev, _ := log.last(EventServiceRestart)
	if ev.Details["health_fail"] != true {
		t.Errorf("restart health_fail = %v, want true", ev.Details["health_fail"])
	}
}

func TestServiceSupervisor_GracefulStop(t *testing.T) {
	port := freePort(t)
	const stopMs = 3000
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartOnFailure, stopMs, "-term-delay", "100ms"))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := waitForState(t, s, StateHealthy, 5*time.Second).PID
	took := stopWithin(t, s, 10*time.Second)
	if took >= stopMs*time.Millisecond/2 {
		t.Errorf("graceful Stop took %s, want well under the %dms budget", took, stopMs)
	}
	st := s.Status()
	if st.State != StateStopped || st.PID != 0 {
		t.Errorf("status after Stop = %+v, want stopped with no pid", st)
	}
	ev, ok := log.last(EventServiceStop)
	if !ok || ev.Details["reason"] != "requested" {
		t.Errorf("stop event = %+v, want reason requested", ev)
	}
	if n := log.count(EventServiceRestart); n != 0 {
		t.Errorf("restart events = %d, want 0 after a requested stop", n)
	}
	assertGone(t, pid)
}

func TestServiceSupervisor_StopForceKillsAfterTimeout(t *testing.T) {
	port := freePort(t)
	const stopMs = 400
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartOnFailure, stopMs, "-ignore-sigterm"))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := waitForState(t, s, StateHealthy, 5*time.Second).PID
	took := stopWithin(t, s, 10*time.Second)
	if took < stopMs*time.Millisecond {
		t.Errorf("Stop took %s, want at least the %dms SIGTERM grace", took, stopMs)
	}
	if took > stopMs*time.Millisecond+2*time.Second {
		t.Errorf("Stop took %s, want force-kill shortly after %dms", took, stopMs)
	}
	if st := s.State(); st != StateStopped {
		t.Errorf("state = %s, want stopped", st)
	}
	if n := log.count(EventServiceStop); n != 1 {
		t.Errorf("stop events = %d, want 1", n)
	}
	assertGone(t, pid)
}

func TestServiceSupervisor_StopIsIdempotent(t *testing.T) {
	port := freePort(t)
	s, log := newTestSupervisor(t, servicePlugin(t, port, RestartOnFailure, 2000))

	// Stop without Start is a no-op.
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	if st := s.State(); st != StateStopped {
		t.Errorf("state before Start = %s, want stopped", st)
	}
	if n := len(log.pids()) + log.count(EventServiceStop); n != 0 {
		t.Errorf("events before Start = %d, want 0", n)
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := waitForState(t, s, StateHealthy, 5*time.Second).PID
	stopWithin(t, s, 5*time.Second)
	stopWithin(t, s, time.Second)
	if n := log.count(EventServiceStop); n != 1 {
		t.Errorf("stop events after Stop+Stop = %d, want 1", n)
	}
	if st := s.State(); st != StateStopped {
		t.Errorf("state = %s, want stopped", st)
	}
	assertGone(t, pid)

	// A stopped supervisor can be started again.
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("restart after Stop: %v", err)
	}
	if st := waitForState(t, s, StateHealthy, 5*time.Second); st.Restarts != 0 {
		t.Errorf("Restarts after fresh Start = %d, want 0", st.Restarts)
	}
	stopWithin(t, s, 5*time.Second)
}

func TestServiceSupervisor_StartFailsForMissingEntry(t *testing.T) {
	p := servicePlugin(t, freePort(t), RestartOnFailure, 2000)
	p.Lifecycle.Entry = "does-not-exist"
	s, log := newTestSupervisor(t, p)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("Start with a missing entry: want error")
	}
	if st := s.State(); st != StateFailed {
		t.Errorf("state = %s, want failed", st)
	}
	if n := log.count(EventServiceStart); n != 0 {
		t.Errorf("start events = %d, want 0", n)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Errorf("Stop after failed Start: %v", err)
	}
}

func TestServiceSupervisor_StartHonorsCanceledContext(t *testing.T) {
	s, _ := newTestSupervisor(t, servicePlugin(t, freePort(t), RestartOnFailure, 2000))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Start(canceled) = %v, want context.Canceled", err)
	}
	if st := s.State(); st != StateStopped {
		t.Errorf("state = %s, want stopped", st)
	}
}

func TestNewServiceSupervisor_Validation(t *testing.T) {
	managed := func() *Plugin {
		return &Plugin{
			Manifest: Manifest{
				Name: "svc",
				Kind: KindService,
				Connection: &PluginConnection{
					BaseURL:    "http://127.0.0.1:9/",
					HealthPath: "/healthz",
				},
				Lifecycle: &PluginLifecycle{
					SupportedModes: []string{LifecycleManaged},
					Entry:          "bin/svc",
				},
			},
			Dir: "/opt/plugins/svc",
		}
	}

	reject := map[string]func(p *Plugin) *Plugin{
		"nil plugin":       func(*Plugin) *Plugin { return nil },
		"automation kind":  func(p *Plugin) *Plugin { p.Kind = KindAutomation; return p },
		"nil lifecycle":    func(p *Plugin) *Plugin { p.Lifecycle = nil; return p },
		"attach_only only": func(p *Plugin) *Plugin { p.Lifecycle.SupportedModes = []string{LifecycleAttachOnly}; return p },
		"empty entry":      func(p *Plugin) *Plugin { p.Lifecycle.Entry = "  "; return p },
	}
	for name, mutate := range reject {
		t.Run(name, func(t *testing.T) {
			if s, err := NewServiceSupervisor(mutate(managed()), nil); err == nil {
				t.Errorf("NewServiceSupervisor accepted %s: %+v", name, s)
			}
		})
	}

	t.Run("defaults", func(t *testing.T) {
		p := managed()
		p.Lifecycle.SupportedModes = []string{LifecycleAttachOnly, LifecycleManaged}
		s, err := NewServiceSupervisor(p, nil)
		if err != nil {
			t.Fatalf("NewServiceSupervisor: %v", err)
		}
		if s.Name() != "svc" || s.State() != StateStopped {
			t.Errorf("name/state = %q/%s", s.Name(), s.State())
		}
		if s.entry != "/opt/plugins/svc/bin/svc" {
			t.Errorf("entry = %q, want it resolved against Dir", s.entry)
		}
		if s.healthURL != "http://127.0.0.1:9/healthz" {
			t.Errorf("healthURL = %q", s.healthURL)
		}
		if s.restartPolicy != RestartOnFailure {
			t.Errorf("restartPolicy = %q, want %q", s.restartPolicy, RestartOnFailure)
		}
		if s.StopTimeout != defaultServiceStopTimeout {
			t.Errorf("StopTimeout = %s, want %s", s.StopTimeout, defaultServiceStopTimeout)
		}
	})

	t.Run("lifecycle overrides", func(t *testing.T) {
		p := managed()
		p.Lifecycle.Entry = "/usr/local/bin/svc"
		p.Lifecycle.RestartPolicy = RestartNever
		p.Lifecycle.StopTimeoutMs = 1500
		s, err := NewServiceSupervisor(p, nil)
		if err != nil {
			t.Fatalf("NewServiceSupervisor: %v", err)
		}
		if s.entry != "/usr/local/bin/svc" || s.restartPolicy != RestartNever || s.StopTimeout != 1500*time.Millisecond {
			t.Errorf("entry/policy/stop = %q/%q/%s", s.entry, s.restartPolicy, s.StopTimeout)
		}
	})
}

func TestServiceSupervisor_Backoff(t *testing.T) {
	s := &ServiceSupervisor{BackoffBase: 100 * time.Millisecond, BackoffMax: time.Second}
	for n, want := range map[int]time.Duration{
		1: 100 * time.Millisecond,
		2: 200 * time.Millisecond,
		3: 400 * time.Millisecond,
		4: 800 * time.Millisecond,
		5: time.Second,
		9: time.Second,
	} {
		if got := s.backoff(n); got != want {
			t.Errorf("backoff(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestServiceOutput_KeepsTail(t *testing.T) {
	o := &serviceOutput{limit: 8}
	_, _ = o.Write([]byte("hello "))
	_, _ = o.Write([]byte("world!"))
	if got := o.String(); got != "o world!" {
		t.Errorf("tail = %q, want %q", got, "o world!")
	}
}
