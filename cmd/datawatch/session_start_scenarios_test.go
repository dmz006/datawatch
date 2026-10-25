package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// The scenarios drive the start tracker the way the wait loop does, one tick per
// fake-clock step, and name the end decision each tick must reach. Panes, states
// and process trees are fixed inputs, so nothing touches tmux or sleeps.

// scenarioLimit is the start-confirm bound the scenarios run with.
const scenarioLimit = 120 * time.Second

// scenarioFailureReason is the failure reason recorded on the task when the
// tracker ends a session.
const scenarioFailureReason = "session never started"

// scenarioEndReason is the end reason the wait loop records on the session when
// the tracker ends it: 121s of silence against the 120s limit.
const scenarioEndReason = "session start detector: waiting_input without assistant output for 121s since its last evidence of work (or first silent tick), limit 120s"

// Pids of the fake process tree: the pane shell, the agent it runs, and the
// command the agent starts.
const (
	scenarioPaneShell = 100
	scenarioAgent     = 200
	scenarioCommand   = 300
)

// scenarioPaneRoots are the pane processes the wait loop walks from.
var scenarioPaneRoots = []int{scenarioPaneShell}

// scenarioProcessNames names the fake processes. The agent is a runner, so the
// command it starts counts as runner work.
var scenarioProcessNames = fakeProcessNames(map[int]string{
	scenarioPaneShell: "bash",
	scenarioAgent:     "claude",
	scenarioCommand:   "go",
})

// scenarioShellPane is a bare shell prompt: no agent, no assistant turn, no tool line.
const scenarioShellPane = "dmz@workstation:~/workspace$ \n"

// scenarioToolCallPane is an agent in a silent tool call: the tool line, its
// running result, and a spinner whose elapsed count moves on every tick.
func scenarioToolCallPane(elapsed time.Duration) string {
	return fmt.Sprintf("❯ start the task\n⏺ Bash(go test ./...)\n  ⎿  Running…\n✻ Running… (%ds · esc to interrupt)\n", int(elapsed.Seconds()))
}

// scenarioAgentTree is a pane shell running claude. When withCommand is set,
// claude also runs a command, which is a child of the runner.
func scenarioAgentTree(withCommand bool) map[int][]int {
	tree := map[int][]int{scenarioPaneShell: {scenarioAgent}}
	if withCommand {
		tree[scenarioAgent] = []int{scenarioCommand}
	}
	return tree
}

// scenarioTick is one tick of a scenario: the inputs the wait loop gathers, and
// the end decision the tick must reach.
type scenarioTick struct {
	sec       int
	state     session.State
	pane      string
	delivered bool
	tree      map[int][]int // the pane's process tree on this tick
	ended     bool          // the session must be ended on this tick
}

// runScenario feeds the ticks to tr in order and checks each tick's end decision.
// It stops at the first ended tick, as the wait loop does, and returns the end
// reason recorded there. It returns "" when no tick ended the session.
func runScenario(t *testing.T, tr *sessionStartTracker, ticks []scenarioTick) string {
	t.Helper()
	for _, tk := range ticks {
		obs := tr.Observe(sessionStartInput{
			Now:       startAt(tk.sec),
			State:     tk.state,
			Pane:      tk.pane,
			Delivered: tk.delivered,
			ChildPIDs: processTreeRunnerDescendants(scenarioPaneRoots, tk.tree, scenarioProcessNames),
		})
		stalled, reason := obs.Stalled()
		if stalled != tk.ended {
			t.Fatalf("t=%ds: ended = %v (WaitingFor %s), want %v", tk.sec, stalled, obs.WaitingFor, tk.ended)
		}
		if stalled {
			if reason != scenarioFailureReason {
				t.Fatalf("t=%ds: failure reason = %q, want %q", tk.sec, reason, scenarioFailureReason)
			}
			return sessionStartEndReason(obs)
		}
	}
	return ""
}

// silentToolCallTicks is six minutes of a delivered task whose agent sits in
// waiting_input. The pane is silent until the tool call starts at 30s, then
// shows the tool line until 360s. The spinner's elapsed count moves on every
// tick when spinning is set and stays at zero when it is not, so a frozen pane
// is covered too. The command the tool call runs is a child of the agent when
// withCommand is set.
func silentToolCallTicks(withCommand, spinning bool) []scenarioTick {
	w := session.StateWaitingInput
	ticks := []scenarioTick{
		{sec: 0, state: w, pane: startSilentPane, delivered: true, tree: scenarioAgentTree(false)},
	}
	for sec := 30; sec <= 360; sec += 30 {
		elapsed := time.Duration(0)
		if spinning {
			elapsed = time.Duration(sec-30) * time.Second
		}
		ticks = append(ticks, scenarioTick{
			sec:       sec,
			state:     w,
			pane:      scenarioToolCallPane(elapsed),
			delivered: true,
			tree:      scenarioAgentTree(withCommand),
		})
	}
	return ticks
}

func TestSessionStartScenarioSilentToolCallSurvives(t *testing.T) {
	for _, limit := range []time.Duration{120 * time.Second, 300 * time.Second} {
		t.Run(fmt.Sprintf("limit %s", limit), func(t *testing.T) {
			for _, variant := range []struct {
				name        string
				withCommand bool
				spinning    bool
			}{
				{name: "tool call in pane", withCommand: false, spinning: true},
				{name: "tool call in pane with a live command", withCommand: true, spinning: true},
				{name: "frozen tool call in pane", withCommand: false, spinning: false},
				{name: "frozen tool call in pane with a live command", withCommand: true, spinning: false},
			} {
				t.Run(variant.name, func(t *testing.T) {
					tr := newSessionStartTracker(limit, time.Time{}, time.Time{})
					if got := runScenario(t, tr, silentToolCallTicks(variant.withCommand, variant.spinning)); got != "" {
						t.Fatalf("session ended during a silent tool call: %q", got)
					}
				})
			}
		})
	}
}

func TestSessionStartScenarioBareShellEnded(t *testing.T) {
	w := session.StateWaitingInput
	// A bare shell with no agent, no tool call, no child process and no channel
	// event. The first silent tick starts the clock, and the session is ended
	// only once the silence exceeds the limit: 120s is still within the bound.
	var ticks []scenarioTick
	for sec := 0; sec <= 120; sec += 30 {
		ticks = append(ticks, scenarioTick{sec: sec, state: w, pane: scenarioShellPane, delivered: true})
	}
	ticks = append(ticks, scenarioTick{sec: 121, state: w, pane: scenarioShellPane, delivered: true, ended: true})

	got := runScenario(t, newSessionStartTracker(scenarioLimit, time.Time{}, time.Time{}), ticks)
	if got != scenarioEndReason {
		t.Fatalf("end reason = %q, want %q", got, scenarioEndReason)
	}
}

func TestSessionStartScenarioAdoptedWorkingSurvives(t *testing.T) {
	w, r := session.StateWaitingInput, session.StateRunning
	// The session was adopted 600s before the first tick, so adoption alone would
	// have the bound long exceeded. Its pane changed at -30s while it was still
	// running, and that change is the last evidence of work. The bound runs from
	// there: the worker survives at 0s and 90s, and is ended at 91s.
	ticks := []scenarioTick{
		{sec: -60, state: r, pane: startStatusPane(1)},
		{sec: -30, state: r, pane: startStatusPane(2)},
		{sec: 0, state: w, pane: startStatusPane(2)},
		{sec: 90, state: w, pane: startStatusPane(2)},
		{sec: 91, state: w, pane: startStatusPane(2), ended: true},
	}

	got := runScenario(t, newSessionStartTracker(scenarioLimit, startAt(-600), time.Time{}), ticks)
	if got != scenarioEndReason {
		t.Fatalf("end reason = %q, want %q", got, scenarioEndReason)
	}
}

func TestSessionStartScenarioAdoptedBareShellEnded(t *testing.T) {
	w := session.StateWaitingInput
	// The adopted shell's last evidence of life was 100s before the first tick.
	// Nothing has happened since, so the bound runs from that evidence: the session
	// ends at 21s, 121s after it, not 121s after the first tick.
	ticks := []scenarioTick{
		{sec: 0, state: w, pane: scenarioShellPane, delivered: true},
		{sec: 10, state: w, pane: scenarioShellPane, delivered: true},
		{sec: 20, state: w, pane: scenarioShellPane, delivered: true},
		{sec: 21, state: w, pane: scenarioShellPane, delivered: true, ended: true},
	}

	got := runScenario(t, newSessionStartTracker(scenarioLimit, startAt(-100), time.Time{}), ticks)
	if got != scenarioEndReason {
		t.Fatalf("end reason = %q, want %q", got, scenarioEndReason)
	}
}
