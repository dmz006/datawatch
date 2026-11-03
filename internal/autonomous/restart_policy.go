// Restart policy: what a requested daemon restart does about running work.
// RestartInterruptions reports what a restart would interrupt right now;
// ApplyRestartPolicy decides, from that report and the configured policy,
// whether the restart proceeds. The systemctl restart itself is not here.

package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// InterruptedTask is one task a restart would interrupt: its worker is
// executing, or its result is being verified.
type InterruptedTask struct {
	PRDID     string `json:"prd_id"`
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	SessionID string `json:"session_id,omitempty"`
}

// RestartInterruption is the read-only answer to "what would a restart
// interrupt now": the running tasks and the live sessions, with their ids.
type RestartInterruption struct {
	RunningTasks    []InterruptedTask `json:"running_tasks"`
	LiveSessions    []string          `json:"live_sessions"`
	RunningTaskCnt  int               `json:"running_task_count"`
	LiveSessionCnt  int               `json:"live_session_count"`
	Policy          string            `json:"policy"`
	LaunchDraining  bool              `json:"launch_draining"`
	QuietTimeoutSec int               `json:"quiet_timeout_seconds"`
	OnTimeout       string            `json:"on_timeout"`
}

// Quiet reports whether no task is running.
func (r RestartInterruption) Quiet() bool { return r.RunningTaskCnt == 0 }

// BuildRestartInterruption counts the tasks in an executing or verifying
// state and the live sessions. It is pure: the Manager supplies the PRDs and
// the session list.
func BuildRestartInterruption(prds []*PRD, sessions []SessionInfo) RestartInterruption {
	out := RestartInterruption{RunningTasks: []InterruptedTask{}, LiveSessions: []string{}}
	for _, p := range prds {
		if p == nil {
			continue
		}
		for _, s := range p.Story {
			for _, t := range s.Tasks {
				switch t.Status {
				case TaskInProgress, TaskRunningTests, TaskVerifying:
					out.RunningTasks = append(out.RunningTasks, InterruptedTask{
						PRDID: p.ID, TaskID: t.ID, Status: string(t.Status), SessionID: t.SessionID,
					})
				}
			}
		}
	}
	sort.Slice(out.RunningTasks, func(i, j int) bool {
		a, b := out.RunningTasks[i], out.RunningTasks[j]
		if a.PRDID != b.PRDID {
			return a.PRDID < b.PRDID
		}
		return a.TaskID < b.TaskID
	})
	for _, si := range sessions {
		if si.Alive && si.ID != "" {
			out.LiveSessions = append(out.LiveSessions, si.ID)
		}
	}
	sort.Strings(out.LiveSessions)
	out.RunningTaskCnt = len(out.RunningTasks)
	out.LiveSessionCnt = len(out.LiveSessions)
	return out
}

// RestartInterruptions reports what a restart now would interrupt, with the
// configured policy filled in. Read-only.
func (m *Manager) RestartInterruptions() RestartInterruption {
	var prds []*PRD
	m.store.WithLock(func() {
		for _, p := range m.store.prds {
			prds = append(prds, p)
		}
	})
	m.mu.Lock()
	listFn, cfg := m.sessionListerFn, m.cfg
	m.mu.Unlock()
	var sessions []SessionInfo
	if listFn != nil {
		sessions = listFn()
	}
	out := BuildRestartInterruption(prds, sessions)
	out.Policy = config.NormalizeRestartPolicy(cfg.RestartPolicy)
	out.QuietTimeoutSec = config.NormalizeRestartQuietTimeoutSeconds(cfg.RestartQuietTimeoutSeconds)
	out.OnTimeout = config.NormalizeRestartOnTimeout(cfg.RestartOnTimeout)
	out.LaunchDraining = m.launchDraining.Load()
	return out
}

// SetLaunchDraining turns the restart drain gate on or off. While on, a task
// that has not started waits at the door of executeOne.
func (m *Manager) SetLaunchDraining(on bool) { m.launchDraining.Store(on) }

// LaunchDraining reports whether the drain gate is on.
func (m *Manager) LaunchDraining() bool { return m.launchDraining.Load() }

// launchDrainPoll is how often a held task re-checks the drain gate.
const launchDrainPoll = 250 * time.Millisecond

// waitLaunchDrain blocks while the drain gate is on. It returns ctx's error
// when the executor is cancelled first.
func (m *Manager) waitLaunchDrain(ctx context.Context) error {
	for m.launchDraining.Load() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(launchDrainPoll):
		}
	}
	return nil
}

// Clock is the time source ApplyRestartPolicy waits on; tests inject a fake.
type Clock interface {
	Now() time.Time
	// Sleep waits d, or returns ctx's error when ctx ends first.
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// RealClock is the wall clock.
func RealClock() Clock { return realClock{} }

// restartPollInterval is how often the wait loops re-count running tasks.
const restartPollInterval = 2 * time.Second

// RestartPolicyDeps is everything ApplyRestartPolicy touches, so a test
// drives it with fakes and no real restart.
type RestartPolicyDeps struct {
	Policy         string
	QuietTimeout   time.Duration
	OnTimeout      string
	PollInterval   time.Duration // 0 => restartPollInterval
	Clock          Clock         // nil => wall clock
	Count          func() RestartInterruption
	SetLaunchDrain func(on bool) // nil => no gate (drain then only waits)
	Logf           func(format string, args ...any)
}

// RestartDecision is what ApplyRestartPolicy returns: whether to restart, and
// the count at the start and at the decision.
type RestartDecision struct {
	Proceed  bool
	Policy   string
	Reason   string
	TimedOut bool
	Waited   time.Duration
	// Initial is the count when the request arrived; Final is the count when
	// the decision was made (the same value under immediate).
	Initial RestartInterruption
	Final   RestartInterruption
}

// ApplyRestartPolicy applies the policy to a requested restart. It logs the
// count it found and returns it, whatever the policy. immediate returns at
// once; wait_quiet polls until zero tasks run or the timeout passes, then
// proceeds or aborts per OnTimeout; drain closes the launch gate and polls
// until zero tasks run, or until ctx ends (which aborts and reopens the gate).
func ApplyRestartPolicy(ctx context.Context, d RestartPolicyDeps) RestartDecision {
	clk := d.Clock
	if clk == nil {
		clk = realClock{}
	}
	logf := d.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	poll := d.PollInterval
	if poll <= 0 {
		poll = restartPollInterval
	}
	policy := config.NormalizeRestartPolicy(d.Policy)
	start := clk.Now()
	init := d.Count()
	dec := RestartDecision{Policy: policy, Initial: init, Final: init}
	logf("[restart] policy=%s: a restart now would interrupt %d running task(s) %v and %d live session(s) %v",
		policy, init.RunningTaskCnt, taskIDs(init), init.LiveSessionCnt, init.LiveSessions)

	finish := func(proceed bool, reason string) RestartDecision {
		dec.Proceed, dec.Reason = proceed, reason
		dec.Waited = clk.Now().Sub(start)
		logf("[restart] policy=%s: %s (waited %s; %d running task(s), %d live session(s) at decision)",
			policy, reason, dec.Waited.Round(time.Second), dec.Final.RunningTaskCnt, dec.Final.LiveSessionCnt)
		return dec
	}

	switch policy {
	case config.RestartPolicyWaitQuiet:
		deadline := start.Add(d.QuietTimeout)
		for !dec.Final.Quiet() {
			remain := deadline.Sub(clk.Now())
			if remain <= 0 {
				dec.TimedOut = true
				if config.NormalizeRestartOnTimeout(d.OnTimeout) == config.RestartOnTimeoutAbort {
					return finish(false, "quiet timeout reached, restart aborted")
				}
				return finish(true, "quiet timeout reached, restarting anyway")
			}
			step := poll
			if remain < step {
				step = remain
			}
			if err := clk.Sleep(ctx, step); err != nil {
				return finish(false, "wait cancelled, restart aborted")
			}
			dec.Final = d.Count()
		}
		return finish(true, "quiet")
	case config.RestartPolicyDrain:
		if d.SetLaunchDrain != nil {
			d.SetLaunchDrain(true)
		}
		for !dec.Final.Quiet() {
			if err := clk.Sleep(ctx, poll); err != nil {
				if d.SetLaunchDrain != nil {
					d.SetLaunchDrain(false)
				}
				return finish(false, "drain cancelled, restart aborted")
			}
			dec.Final = d.Count()
		}
		// The gate stays closed: the daemon is about to restart.
		return finish(true, "drained")
	}
	return finish(true, "immediate")
}

func taskIDs(r RestartInterruption) []string {
	ids := make([]string, 0, len(r.RunningTasks))
	for _, t := range r.RunningTasks {
		ids = append(ids, fmt.Sprintf("%s/%s", t.PRDID, t.TaskID))
	}
	return ids
}

// RestartInterruptionText renders the report for the CLI and comm channel.
func RestartInterruptionText(r RestartInterruption) string {
	s := fmt.Sprintf("A restart now would interrupt %d running task(s) and %d live session(s).\nPolicy: %s (quiet timeout %ds, on timeout %s)%s\n",
		r.RunningTaskCnt, r.LiveSessionCnt, r.Policy, r.QuietTimeoutSec, r.OnTimeout, map[bool]string{true: ", new tasks held", false: ""}[r.LaunchDraining])
	for _, t := range r.RunningTasks {
		s += fmt.Sprintf("  task %s/%s %s", t.PRDID, t.TaskID, t.Status)
		if t.SessionID != "" {
			s += " session " + t.SessionID
		}
		s += "\n"
	}
	for _, id := range r.LiveSessions {
		s += "  session " + id + "\n"
	}
	return s
}

// RestartInterruptions and the drain gate on the API, so the REST layer reads
// them through the same façade as the other autonomous reads.
func (a *API) RestartInterruptions() RestartInterruption { return a.M.RestartInterruptions() }

// ParseRestartInterruption decodes the body of
// GET /api/autonomous/restart-interruptions.
func ParseRestartInterruption(b []byte) (RestartInterruption, error) {
	var r RestartInterruption
	if err := json.Unmarshal(b, &r); err != nil {
		return RestartInterruption{}, fmt.Errorf("reading the restart interruption report: %w", err)
	}
	return r, nil
}
