// Manifest v3 managed-service supervisor.
//
// A ServiceSupervisor runs one kind: service plugin whose lifecycle
// declares the "managed" mode as a plain os/exec child process, probes
// its health endpoint and restarts it per the manifest restart policy.
//
// Reuse audit: this reuses the PATTERN of internal/agents — the typed
// state string (agents.NodeState pending/running/done/failed), the
// single goroutine reconcile loop driven by a context plus a poll ticker
// (Orchestrator.runNode) and graceful-then-force termination
// (DockerDriver.Terminate's "stop it, then make sure it is gone"). No
// Docker/k8s driver code is called; the child is an ordinary subprocess,
// and its environment comes from resolvePluginEnv exactly like hook
// plugins so ${secret:...} env values work the same way.

package plugins

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secrets"
)

// ServiceState is the supervisor's runtime view of a managed service.
type ServiceState string

const (
	StateStopped    ServiceState = "stopped"
	StateStarting   ServiceState = "starting"
	StateHealthy    ServiceState = "healthy"
	StateUnhealthy  ServiceState = "unhealthy"
	StateCrashed    ServiceState = "crashed"
	StateRestarting ServiceState = "restarting"
	StateFailed     ServiceState = "failed" // gave up; no further restarts
	StateStopping   ServiceState = "stopping"
)

// Service lifecycle event actions passed to ServiceSupervisor.OnEvent.
const (
	EventServiceStart      = "plugin_service_start"
	EventServiceStop       = "plugin_service_stop"
	EventServiceRestart    = "plugin_service_restart"
	EventServiceHealthFail = "plugin_service_health_fail"
)

// Supervisor defaults; every one can be overridden on the struct before Start.
const (
	defaultServiceStopTimeout   = 10 * time.Second
	defaultServiceHealthEvery   = 5 * time.Second
	defaultServiceStartupGrace  = 30 * time.Second
	defaultServiceStableAfter   = 60 * time.Second
	defaultServiceMaxRestarts   = 5
	defaultServiceBackoffBase   = time.Second
	defaultServiceBackoffMax    = 30 * time.Second
	defaultServiceHealthFails   = 3
	defaultServiceProbeTimeout  = 3 * time.Second
	defaultServiceOutputLimit   = 64 * 1024
	defaultServiceWaitIODelay   = 2 * time.Second
	defaultServiceRestartPolicy = RestartOnFailure
)

// ServiceEvent is emitted on service lifecycle transitions.
type ServiceEvent struct {
	Plugin  string
	Action  string
	Details map[string]any
}

// ServiceStatus is a point-in-time snapshot of a supervised service.
type ServiceStatus struct {
	Plugin              string       `json:"plugin"`
	State               ServiceState `json:"state"`
	PID                 int          `json:"pid,omitempty"`
	Restarts            int          `json:"restarts"`             // total restarts since Start
	ConsecutiveFailures int          `json:"consecutive_failures"` // failures since last stable run
	LastError           string       `json:"last_error,omitempty"`
	LastExit            int          `json:"last_exit"` // exit code; -1 = killed by signal / unknown
	StartedAt           time.Time    `json:"started_at,omitempty"`
	HealthyAt           time.Time    `json:"healthy_at,omitempty"`
}

// ServiceSupervisor runs and supervises one managed service-kind plugin.
//
// Health: when Connection.HealthPath is set, the supervisor GETs
// BaseURL+HealthPath every HealthInterval; the first 2xx marks the
// service healthy. No 2xx within StartupGrace is a health failure (the
// child is killed and the failure goes through the restart policy), as
// are HealthFailThreshold consecutive failed probes after the service was
// healthy. When HealthPath is empty there is nothing to probe, so a
// process that is still running StartupGrace after launch is treated as
// healthy.
//
// Restarts: "never" gives up on any unexpected exit; "on-failure"
// (the default) restarts after a non-zero exit, a signal or a health
// failure and treats a clean exit 0 as an intentional stop; "always"
// restarts after any exit. Restarts back off exponentially
// (BackoffBase doubling, capped at BackoffMax). After MaxRestarts
// consecutive failures without a stable run the supervisor gives up
// (StateFailed). A run that stays healthy for StableAfter resets the
// consecutive-failure counter.
//
// The exported tunables must be set before Start and not changed while
// the service is supervised.
type ServiceSupervisor struct {
	HealthInterval      time.Duration
	StartupGrace        time.Duration
	StableAfter         time.Duration
	HealthFailThreshold int
	MaxRestarts         int
	BackoffBase         time.Duration
	BackoffMax          time.Duration
	StopTimeout         time.Duration
	HTTPClient          *http.Client
	// OnEvent, when set, receives lifecycle events. It is never called
	// while the supervisor's mutex is held, but it is called from the
	// supervision goroutine, so it must not block for long.
	OnEvent func(ev ServiceEvent)

	name          string
	entry         string
	dir           string
	args          []string
	env           []string
	healthURL     string
	authHeader    string
	restartPolicy string
	output        *serviceOutput

	mu         sync.Mutex
	state      ServiceState
	running    bool          // supervision loop active
	stopping   bool          // Stop requested; the loop must not restart
	done       chan struct{} // closed by Stop to end the loop
	loopDone   chan struct{} // closed when the loop has exited and the child is reaped
	stopReason string
	pid        int
	restarts   int
	failures   int
	lastErr    string
	lastExit   int
	startedAt  time.Time
	healthyAt  time.Time
}

// NewServiceSupervisor builds a supervisor for p. p must be kind: service
// with a lifecycle that supports the "managed" mode and declares an entry.
// store resolves ${secret:...} env values and the connection auth token;
// it may be nil.
func NewServiceSupervisor(p *Plugin, store secrets.Store) (*ServiceSupervisor, error) {
	if p == nil {
		return nil, errors.New("plugin is nil")
	}
	if p.Kind != KindService {
		return nil, fmt.Errorf("plugin %q is kind %q, not %q", p.Name, p.Kind, KindService)
	}
	l := p.Lifecycle
	if l == nil {
		return nil, fmt.Errorf("plugin %q has no lifecycle block", p.Name)
	}
	managed := false
	for _, m := range l.SupportedModes {
		if m == LifecycleManaged {
			managed = true
			break
		}
	}
	if !managed {
		return nil, fmt.Errorf("plugin %q lifecycle does not support mode %q", p.Name, LifecycleManaged)
	}
	if strings.TrimSpace(l.Entry) == "" {
		return nil, fmt.Errorf("plugin %q lifecycle.entry is empty", p.Name)
	}

	entry := l.Entry
	if !filepath.IsAbs(entry) {
		entry = filepath.Join(p.Dir, entry)
	}
	policy := l.RestartPolicy
	if policy == "" {
		policy = defaultServiceRestartPolicy
	}
	stopTimeout := defaultServiceStopTimeout
	if l.StopTimeoutMs > 0 {
		stopTimeout = time.Duration(l.StopTimeoutMs) * time.Millisecond
	}

	s := &ServiceSupervisor{
		HealthInterval:      defaultServiceHealthEvery,
		StartupGrace:        defaultServiceStartupGrace,
		StableAfter:         defaultServiceStableAfter,
		HealthFailThreshold: defaultServiceHealthFails,
		MaxRestarts:         defaultServiceMaxRestarts,
		BackoffBase:         defaultServiceBackoffBase,
		BackoffMax:          defaultServiceBackoffMax,
		StopTimeout:         stopTimeout,
		HTTPClient:          &http.Client{Timeout: defaultServiceProbeTimeout},
		name:                p.Name,
		entry:               entry,
		dir:                 p.Dir,
		args:                append([]string(nil), l.Args...),
		env:                 resolvePluginEnv(p, store),
		restartPolicy:       policy,
		output:              &serviceOutput{limit: defaultServiceOutputLimit},
		state:               StateStopped,
	}
	if c := p.Connection; c != nil {
		if c.HealthPath != "" {
			s.healthURL = strings.TrimRight(c.BaseURL, "/") + "/" + strings.TrimLeft(c.HealthPath, "/")
		}
		if c.Auth != nil && c.Auth.Token != "" && store != nil {
			tok, err := secrets.ResolveRefAs(c.Auth.Token, store, secrets.CallerCtx{Type: "plugin", Name: p.Name})
			if err != nil {
				return nil, fmt.Errorf("plugin %q connection.auth.token: %w", p.Name, err)
			}
			s.authHeader = "Bearer " + tok
		}
	}
	return s, nil
}

// Name returns the supervised plugin's name.
func (s *ServiceSupervisor) Name() string { return s.name }

// State returns the current service state.
func (s *ServiceSupervisor) State() ServiceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Status returns a snapshot of the supervisor's runtime state.
func (s *ServiceSupervisor) Status() ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ServiceStatus{
		Plugin:              s.name,
		State:               s.state,
		PID:                 s.pid,
		Restarts:            s.restarts,
		ConsecutiveFailures: s.failures,
		LastError:           s.lastErr,
		LastExit:            s.lastExit,
		StartedAt:           s.startedAt,
		HealthyAt:           s.healthyAt,
	}
}

// Output returns the tail of the child's combined stdout/stderr.
func (s *ServiceSupervisor) Output() string { return s.output.String() }

// Start launches the service and begins supervising it. ctx only bounds
// the launch itself: the supervisor owns the child process for its whole
// life (it is not tied to ctx), and supervision ends only via Stop or by
// giving up. Start returns an error when the service is already
// supervised or the first launch fails. A supervisor may be started again
// after Stop or after it gave up.
func (s *ServiceSupervisor) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("plugin service %q is already running", s.name)
	}
	s.running = true
	s.stopping = false
	s.stopReason = ""
	s.done = make(chan struct{})
	s.loopDone = make(chan struct{})
	s.state = StateStarting
	s.restarts, s.failures = 0, 0
	s.lastErr, s.lastExit = "", 0
	s.healthyAt = time.Time{}
	s.mu.Unlock()

	cmd, exitCh, err := s.launch()
	if err != nil {
		s.mu.Lock()
		s.running = false
		s.state = StateFailed
		s.lastErr = err.Error()
		close(s.loopDone)
		s.mu.Unlock()
		return err
	}
	go s.run(cmd, exitCh)
	return nil
}

// Stop gracefully stops the service: SIGTERM to the child's process
// group, up to the stop timeout, then SIGKILL, then waits for reaping.
// It is idempotent and safe on a supervisor that was never started. If
// ctx expires first Stop returns ctx.Err(); the shutdown still completes
// in the background (bounded by the stop timeout).
func (s *ServiceSupervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	loopDone := s.loopDone
	if !s.stopping {
		s.stopping = true
		s.stopReason = "requested"
		s.state = StateStopping
		close(s.done)
	}
	s.mu.Unlock()

	select {
	case <-loopDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// launch starts one child process and returns it with a channel that
// receives its cmd.Wait result.
func (s *ServiceSupervisor) launch() (*exec.Cmd, <-chan error, error) {
	cmd := exec.Command(s.entry, s.args...) // #nosec G204 -- entry/args come from the operator-installed plugin manifest
	cmd.Dir = s.dir
	cmd.Env = s.env
	cmd.Stdout = s.output
	cmd.Stderr = s.output
	cmd.WaitDelay = defaultServiceWaitIODelay
	setServiceProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start plugin service %q: %w", s.name, err)
	}
	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()

	s.mu.Lock()
	s.pid = cmd.Process.Pid
	s.startedAt = time.Now()
	s.healthyAt = time.Time{}
	if !s.stopping {
		s.state = StateStarting
	}
	attempt := s.restarts
	s.mu.Unlock()
	s.emit(EventServiceStart, map[string]any{"pid": cmd.Process.Pid, "restarts": attempt})
	return cmd, exitCh, nil
}

// attemptResult describes how one supervised child run ended.
type attemptResult struct {
	stopped    bool // Stop was requested; the child has been terminated
	failed     bool // counts as a failure for the restart policy
	stable     bool // the run stayed healthy for StableAfter
	exitCode   int
	err        string
	healthFail bool
}

// run is the single supervision loop: it watches the current child,
// applies the restart policy when it ends, and exits on Stop or give-up.
func (s *ServiceSupervisor) run(cmd *exec.Cmd, exitCh <-chan error) {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.pid = 0
		close(s.loopDone)
		s.mu.Unlock()
	}()

	for {
		res := s.watch(cmd, exitCh)
		for {
			if res.stopped {
				s.finishStopped()
				return
			}
			backoff, ok := s.afterExit(res)
			if !ok {
				return
			}
			t := time.NewTimer(backoff)
			select {
			case <-s.done:
				t.Stop()
				s.finishStopped()
				return
			case <-t.C:
			}
			var err error
			if cmd, exitCh, err = s.launch(); err == nil {
				break
			}
			res = attemptResult{failed: true, exitCode: -1, err: err.Error()}
		}
	}
}

// finishStopped records a Stop-requested shutdown.
func (s *ServiceSupervisor) finishStopped() {
	s.mu.Lock()
	s.state = StateStopped
	reason := s.stopReason
	s.mu.Unlock()
	s.emit(EventServiceStop, map[string]any{"reason": reason})
}

// afterExit records an unexpected end of a run (exit, health failure or
// failed launch) and applies the restart policy. It returns the backoff
// before the next launch, or ok=false when the supervisor is done.
func (s *ServiceSupervisor) afterExit(res attemptResult) (backoff time.Duration, ok bool) {
	s.mu.Lock()
	s.state = StateCrashed
	s.lastExit = res.exitCode
	if res.err != "" {
		s.lastErr = res.err
	}
	if res.stable {
		s.failures = 0
	}
	if res.failed {
		s.failures++
	}
	restart := s.shouldRestart(res)
	giveUp := restart && s.failures > s.MaxRestarts
	if !restart || giveUp {
		if !res.failed && s.restartPolicy == RestartOnFailure {
			s.state = StateStopped // on-failure: a clean exit is intentional
		} else {
			s.state = StateFailed
		}
		state, failures := s.state, s.failures
		s.mu.Unlock()
		reason := "exited"
		if giveUp {
			reason = "max_restarts"
		}
		s.emit(EventServiceStop, map[string]any{
			"reason": reason, "state": string(state), "exit_code": res.exitCode,
			"error": res.err, "consecutive_failures": failures,
		})
		return 0, false
	}
	s.state = StateRestarting
	s.restarts++
	backoff = s.backoff(s.failures)
	restarts := s.restarts
	s.mu.Unlock()
	s.emit(EventServiceRestart, map[string]any{
		"restarts": restarts, "backoff_ms": backoff.Milliseconds(),
		"exit_code": res.exitCode, "error": res.err, "health_fail": res.healthFail,
	})
	return backoff, true
}

// shouldRestart applies the restart policy; caller holds s.mu.
func (s *ServiceSupervisor) shouldRestart(res attemptResult) bool {
	switch s.restartPolicy {
	case RestartNever:
		return false
	case RestartAlways:
		return true
	default: // RestartOnFailure
		return res.failed
	}
}

// backoff returns the delay before restart number n (1-based).
func (s *ServiceSupervisor) backoff(n int) time.Duration {
	d := s.BackoffBase
	for i := 1; i < n && d < s.BackoffMax; i++ {
		d *= 2
	}
	if s.BackoffMax > 0 && d > s.BackoffMax {
		d = s.BackoffMax
	}
	return d
}

// watch supervises one child until it exits, fails health or Stop is
// requested, and always returns with the child reaped.
func (s *ServiceSupervisor) watch(cmd *exec.Cmd, exitCh <-chan error) attemptResult {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := time.NewTicker(s.HealthInterval)
	defer ticker.Stop()
	grace := time.NewTimer(s.StartupGrace)
	defer grace.Stop()

	healthy := false
	var healthySince time.Time
	probeFails := 0

	healthFailure := func(reason string, err error) attemptResult {
		msg := reason
		if err != nil {
			msg += ": " + err.Error()
		}
		s.emit(EventServiceHealthFail, map[string]any{"reason": reason, "error": errString(err), "url": s.healthURL})
		code := s.terminate(cmd, exitCh)
		return attemptResult{failed: true, exitCode: code, err: "health check failed: " + msg, healthFail: true,
			stable: healthy && time.Since(healthySince) >= s.StableAfter}
	}
	markHealthy := func() {
		healthy = true
		healthySince = time.Now()
		probeFails = 0
		s.mu.Lock()
		if !s.stopping {
			s.state = StateHealthy
		}
		s.healthyAt = healthySince
		s.mu.Unlock()
	}

	for {
		select {
		case <-s.done:
			cancel()
			s.terminate(cmd, exitCh)
			return attemptResult{stopped: true}

		case err := <-exitCh:
			code, failed := classifyExit(err)
			msg := ""
			if failed {
				msg = "process exited: " + errString(err)
			}
			return attemptResult{failed: failed, exitCode: code, err: msg,
				stable: healthy && time.Since(healthySince) >= s.StableAfter}

		case <-grace.C:
			if healthy {
				continue
			}
			if s.healthURL == "" {
				markHealthy()
				continue
			}
			return healthFailure("startup_deadline", fmt.Errorf("no 2xx from health endpoint within %s", s.StartupGrace))

		case <-ticker.C:
			if healthy && !healthySince.IsZero() && time.Since(healthySince) >= s.StableAfter {
				s.mu.Lock()
				s.failures = 0
				s.mu.Unlock()
			}
			if s.healthURL == "" {
				continue
			}
			err := s.probe(ctx)
			if err == nil {
				if !healthy || probeFails > 0 {
					if !healthy {
						markHealthy()
					} else {
						probeFails = 0
						s.mu.Lock()
						if !s.stopping {
							s.state = StateHealthy
						}
						s.mu.Unlock()
					}
				}
				continue
			}
			if !healthy {
				continue // still inside the startup grace window
			}
			probeFails++
			s.mu.Lock()
			if !s.stopping {
				s.state = StateUnhealthy
			}
			s.lastErr = "health probe: " + err.Error()
			s.mu.Unlock()
			if probeFails >= s.HealthFailThreshold {
				return healthFailure("probe_failures", fmt.Errorf("%d consecutive failed probes, last: %w", probeFails, err))
			}
		}
	}
}

// probe performs one health check; nil means a 2xx response.
func (s *ServiceSupervisor) probe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.healthURL, nil)
	if err != nil {
		return err
	}
	if s.authHeader != "" {
		req.Header.Set("Authorization", s.authHeader)
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("health endpoint returned %s", resp.Status)
	}
	return nil
}

// terminate stops the child gracefully, then forcefully, and waits for
// it to be reaped. Returns the child's exit code (-1 for signal/unknown).
func (s *ServiceSupervisor) terminate(cmd *exec.Cmd, exitCh <-chan error) int {
	_ = signalServiceProcessGroup(cmd, false)
	t := time.NewTimer(s.StopTimeout)
	defer t.Stop()
	select {
	case err := <-exitCh:
		code, _ := classifyExit(err)
		return code
	case <-t.C:
	}
	_ = signalServiceProcessGroup(cmd, true)
	err := <-exitCh
	code, _ := classifyExit(err)
	return code
}

// emit delivers an event to OnEvent. Must not be called with s.mu held.
func (s *ServiceSupervisor) emit(action string, details map[string]any) {
	if s.OnEvent == nil {
		return
	}
	s.OnEvent(ServiceEvent{Plugin: s.name, Action: action, Details: details})
}

// classifyExit maps a cmd.Wait error to (exit code, failed). A clean exit
// is (0, false); a non-zero exit or a signal is a failure.
func classifyExit(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true // ExitCode is -1 when killed by a signal
	}
	return -1, true
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// serviceOutput is a bounded buffer keeping the tail of a child's output.
type serviceOutput struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (o *serviceOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf = append(o.buf, p...)
	if over := len(o.buf) - o.limit; over > 0 {
		o.buf = append(o.buf[:0], o.buf[over:]...)
	}
	return len(p), nil
}

func (o *serviceOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(o.buf)
}
