// Running-gate registry and the exit-time gate wait. The Manager records each
// final gate check while it runs (check, Automaton, task, process id), so a
// restart report can name the gates a restart would touch, and the daemon can
// wait for them to finish before it exits. A gate runs in its own durable
// session and survives the daemon, so the wait is a courtesy, never a
// requirement: it is bounded by autonomous.gate_drain_minutes, and nothing
// here ever signals or kills a gate or a session.

package autonomous

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// RunningGate is one final gate check that is running now.
type RunningGate struct {
	Check  string `json:"check"`
	PRDID  string `json:"prd_id"`
	TaskID string `json:"task_id"`
	PID    int    `json:"pid,omitempty"`
}

// runningGateRegistry is the Manager's set of running gates. The zero value
// is ready to use.
type runningGateRegistry struct {
	mu    sync.Mutex
	seq   int64
	gates map[int64]RunningGate
}

func (r *runningGateRegistry) add(g RunningGate) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gates == nil {
		r.gates = map[int64]RunningGate{}
	}
	r.seq++
	r.gates[r.seq] = g
	return r.seq
}

func (r *runningGateRegistry) setPID(id int64, pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gates[id]; ok {
		g.PID = pid
		r.gates[id] = g
	}
}

func (r *runningGateRegistry) remove(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.gates, id)
}

func (r *runningGateRegistry) list() []RunningGate {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RunningGate, 0, len(r.gates))
	for _, g := range r.gates {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.PRDID != b.PRDID {
			return a.PRDID < b.PRDID
		}
		if a.TaskID != b.TaskID {
			return a.TaskID < b.TaskID
		}
		return a.Check < b.Check
	})
	return out
}

// gatePIDHookKey carries the callback that learns a gate's process id.
type gatePIDHookKey struct{}

// withGatePIDHook returns a context whose gate run reports its process id to
// fn once the gate has started or been re-attached.
func withGatePIDHook(ctx context.Context, fn func(pid int)) context.Context {
	return context.WithValue(ctx, gatePIDHookKey{}, fn)
}

func reportGatePID(ctx context.Context, pid int) {
	if fn, ok := ctx.Value(gatePIDHookKey{}).(func(int)); ok && fn != nil {
		fn(pid)
	}
}

// trackGate records a running gate and returns the context to run it under
// and the function that clears the record when the gate ends.
func (m *Manager) trackGate(ctx context.Context, prdID, taskID, check string) (context.Context, func()) {
	id := m.runningGates.add(RunningGate{Check: check, PRDID: prdID, TaskID: taskID})
	ctx = withGatePIDHook(ctx, func(pid int) { m.runningGates.setPID(id, pid) })
	return ctx, func() { m.runningGates.remove(id) }
}

// RunningGates lists the gate checks running now, sorted by Automaton, task
// and check. The list is never nil.
func (m *Manager) RunningGates() []RunningGate { return m.runningGates.list() }

// GateDrainResult is what the exit-time gate wait did. Waited is how many
// gates it waited for (0 when the wait is off or no gate ran); Interrupted is
// how many were still running when the wait ended, which the exit leaves
// running.
type GateDrainResult struct {
	Waited      int           `json:"gates_waited"`
	Interrupted int           `json:"gates_interrupted"`
	Elapsed     time.Duration `json:"-"`
	TimedOut    bool          `json:"timed_out"`
}

// gateDrainPollInterval is how often the exit wait re-reads the registry.
const gateDrainPollInterval = time.Second

// DrainGates waits up to autonomous.gate_drain_minutes for the running gates
// to finish, polling the registry with clk (nil = wall clock). It returns as
// soon as the last gate ends. While it waits it holds new gates at the launch
// drain gate; the gate stays closed because the daemon is about to exit. A
// zero wait, or no running gate, returns at once (a zero wait with gates
// running reports them as interrupted). A timeout or a cancelled ctx returns
// cleanly: it never blocks the exit and never signals a gate or a session.
func (m *Manager) DrainGates(ctx context.Context, clk Clock, logf func(string, ...any)) GateDrainResult {
	if clk == nil {
		clk = realClock{}
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	wait := time.Duration(config.NormalizeGateDrainMinutes(m.Config().GateDrainMinutes)) * time.Minute
	running := m.RunningGates()
	if len(running) == 0 {
		return GateDrainResult{}
	}
	if wait <= 0 {
		logf("[gates] %d gate(s) running; gate_drain_minutes is 0, not waiting", len(running))
		return GateDrainResult{Interrupted: len(running)}
	}
	res := GateDrainResult{Waited: len(running)}
	logf("[gates] waiting up to %s for %d running gate(s) to finish", wait, len(running))
	m.SetLaunchDraining(true)
	start := clk.Now()
	deadline := start.Add(wait)
	left := len(running)
	for left > 0 {
		remain := deadline.Sub(clk.Now())
		if remain <= 0 {
			res.TimedOut = true
			break
		}
		step := gateDrainPollInterval
		if remain < step {
			step = remain
		}
		if err := clk.Sleep(ctx, step); err != nil {
			break
		}
		left = len(m.RunningGates())
	}
	res.Interrupted = left
	res.Elapsed = clk.Now().Sub(start)
	logf("[gates] waited %s for %d gate(s); %d still running at exit (left running, not signalled)",
		res.Elapsed.Round(time.Second), res.Waited, res.Interrupted)
	return res
}
