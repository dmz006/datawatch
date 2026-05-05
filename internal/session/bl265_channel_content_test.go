// BL265 / v6.11.19 — content-aware channel state detection.
// Operator: "It didn't work for capturing session state, are you
// getting it from the actual message? Debug and get it working,
// this is one of the most important features, knowing when jobs
// are done or blocked and or input."

package session

import (
	"testing"
	"time"
)

func TestBL265_DetectChannelStateSignal_Complete(t *testing.T) {
	completionPhrases := []string{
		"Task complete",
		"All done!",
		"task completed successfully",
		"I've completed the task as requested.",
		"The work is complete; here's a summary.",
		"All tasks completed.",
		"successfully completed the migration",
	}
	for _, p := range completionPhrases {
		if got := detectChannelStateSignal(p); got != "complete" {
			t.Errorf("detect(%q) = %q, want complete", p, got)
		}
	}
}

func TestBL265_DetectChannelStateSignal_Input(t *testing.T) {
	inputPhrases := []string{
		"Should I proceed with the deployment?",
		"Do you want me to continue?",
		"Please confirm before I delete the file.",
		"Awaiting your input on the next step.",
		"I need your input on the design choice.",
		"Can you clarify what you mean by 'fast'?",
		"What would you like me to do next?",
		"Are we good to ship?", // trailing ?
	}
	for _, p := range inputPhrases {
		if got := detectChannelStateSignal(p); got != "input" {
			t.Errorf("detect(%q) = %q, want input", p, got)
		}
	}
}

func TestBL265_DetectChannelStateSignal_Blocked(t *testing.T) {
	blockedPhrases := []string{
		"I'm blocked on the missing credentials.",
		"I am stuck on the API rate limit.",
		"Hit an error trying to connect.",
		"Unable to proceed without the secret key.",
		"Cannot proceed; the build is broken.",
	}
	for _, p := range blockedPhrases {
		if got := detectChannelStateSignal(p); got != "blocked" {
			t.Errorf("detect(%q) = %q, want blocked", p, got)
		}
	}
}

func TestBL265_DetectChannelStateSignal_Generic(t *testing.T) {
	genericPhrases := []string{
		"Running tests now",
		"Compiling the new module.",
		"Looks good so far.",
		"",
	}
	for _, p := range genericPhrases {
		if got := detectChannelStateSignal(p); got != "" {
			t.Errorf("detect(%q) = %q, want empty (generic)", p, got)
		}
	}
}

func TestBL265_ChannelMessage_TaskComplete_TransitionsToComplete(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now(),
	})

	mgr.MarkChannelActivityFromText("testhost-aa01", "Task complete. All tests pass.")

	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateComplete {
		t.Errorf("state: %s (want Complete)", got.State)
	}
}

func TestBL265_ChannelMessage_QuestionMark_TransitionsToWaiting(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now(),
	})

	mgr.MarkChannelActivityFromText("testhost-aa01", "Should I proceed with the migration?")

	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateWaitingInput {
		t.Errorf("state: %s (want WaitingInput)", got.State)
	}
}

func TestBL265_ChannelMessage_GenericActivity_StillBumpsRunning(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateWaitingInput, UpdatedAt: time.Now(),
	})

	// Generic activity (no specific signal) — should still wake from waiting.
	mgr.MarkChannelActivityFromText("testhost-aa01", "Running tests now.")

	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateRunning {
		t.Errorf("state: %s (want Running)", got.State)
	}
}

func TestBL265_ChannelMessage_BlockedDoesNotChangeState(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now(),
	})

	mgr.MarkChannelActivityFromText("testhost-aa01", "I'm stuck on the credentials.")

	got, _ := mgr.store.Get("testhost-aa01")
	// Blocked is logged but doesn't transition (too risky from text alone).
	if got.State != StateRunning {
		t.Errorf("state: %s (want unchanged Running for blocked phrase)", got.State)
	}
}

func TestBL265_ChannelMessage_NeverOverridesRateLimited(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateRateLimited, UpdatedAt: time.Now(),
	})

	// Even a "task complete" phrase shouldn't override rate-limited.
	mgr.MarkChannelActivityFromText("testhost-aa01", "Task complete!")

	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateRateLimited {
		t.Errorf("state: %s (want unchanged RateLimited)", got.State)
	}
}

func TestBL265_ChannelMessage_AlreadyCompleteIsNoOp(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateComplete, UpdatedAt: time.Now(),
	})

	// "input"-flavoured message after complete shouldn't resurrect to WaitingInput.
	mgr.MarkChannelActivityFromText("testhost-aa01", "Should I do anything else?")

	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateComplete {
		t.Errorf("state: %s (want unchanged Complete)", got.State)
	}
}

func TestBL265_EmitChatMessage_AssistantTaskComplete_Transitions(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now(),
	})

	mgr.EmitChatMessage("testhost-aa01", "assistant", "Task complete. PR ready for review.", false)

	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateComplete {
		t.Errorf("state after assistant 'Task complete': %s (want Complete)", got.State)
	}
}

func TestBL265_BackwardCompat_MarkChannelActivity_NoText(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateWaitingInput, UpdatedAt: time.Now(),
	})

	// v6.11.18 callers (no content) → falls through to generic activity.
	mgr.MarkChannelActivity("testhost-aa01")

	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateRunning {
		t.Errorf("state: %s (want Running)", got.State)
	}
}
