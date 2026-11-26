package main

import (
	"errors"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

const madeUpLaunchFailure = `launch failed: the agent exited at the start-up dialog "Settings Error", which datawatch declined (sent Esc), before the task was delivered`

// An Automaton worker whose session records a failed launch takes the launch
// failure path whatever its delivery record or output say.
func TestWorkerLaunchFailureIsALaunchFailure(t *testing.T) {
	if err := workerLaunchFailure(&session.Session{State: session.StateFailed}); err != nil {
		t.Errorf("an ordinary failure became a launch failure: %v", err)
	}
	if err := workerLaunchFailure(nil); err != nil {
		t.Errorf("no session: %v", err)
	}
	err := workerLaunchFailure(&session.Session{FullID: "h-1", BackendFamily: "claude-code",
		State: session.StateFailed, LaunchFailure: madeUpLaunchFailure, CompletionSummary: "claude done"})
	if !errors.Is(err, autonomouspkg.ErrSessionKilledBeforeDelivery) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "Settings Error") || !strings.Contains(err.Error(), autonomouspkg.LaunchFailureReason) {
		t.Errorf("err does not say why: %v", err)
	}
}

func TestStateAlertSaysWhyALaunchFailed(t *testing.T) {
	failed := &session.Session{Task: "the task", State: session.StateFailed, LaunchFailure: madeUpLaunchFailure}
	body, line := stateAlertText(failed, session.StateWaitingInput, "waiting_input → failed")
	if body != madeUpLaunchFailure || line != "waiting_input → failed: "+madeUpLaunchFailure {
		t.Errorf("body %q line %q", body, line)
	}
	body, line = stateAlertText(&session.Session{Task: "the task", State: session.StateFailed}, session.StateRunning, "running → failed")
	if body != "the task" || line != "running → failed" {
		t.Errorf("ordinary failure: body %q line %q", body, line)
	}
}
