package autonomous

import "testing"

// Pure-function tests for NudgeForDelivery. The function takes the delivery
// state and the task text as plain values, so no session or wait loop is
// needed.

// wantNudgeText pins the delivered-worker nudge wording. A change to
// WorkerNudgeText must be made on purpose, not by accident.
const wantNudgeText = "If you are finished, print DATAWATCH_COMPLETE followed by a one-line summary; otherwise continue with the task."

func TestNudgeForDelivery_UndeliveredSendsTask(t *testing.T) {
	const task = "Fix the parser in internal/xyz"

	text, redelivery := NudgeForDelivery(false, task)
	if text != task || !redelivery {
		t.Fatalf("undelivered worker: got (%q, %v), want (%q, true)", text, redelivery, task)
	}

	// No task text to re-send: the completion-line prompt is the only nudge.
	text, redelivery = NudgeForDelivery(false, "")
	if text != WorkerNudgeText || redelivery {
		t.Fatalf("undelivered worker with empty task: got (%q, %v), want (WorkerNudgeText, false)", text, redelivery)
	}

	// A delivered worker never gets the task re-sent, even when task text exists.
	text, redelivery = NudgeForDelivery(true, "Fix the parser")
	if text != WorkerNudgeText || redelivery {
		t.Fatalf("delivered worker with task: got (%q, %v), want (WorkerNudgeText, false)", text, redelivery)
	}
}

func TestNudgeForDelivery_DeliveredSendsNudge(t *testing.T) {
	text, redelivery := NudgeForDelivery(true, "Fix the parser in internal/xyz")
	if text != WorkerNudgeText || redelivery {
		t.Fatalf("delivered worker: got (%q, %v), want (WorkerNudgeText, false)", text, redelivery)
	}
	if text != wantNudgeText {
		t.Fatalf("delivered nudge wording changed:\n got %q\nwant %q", text, wantNudgeText)
	}
}
