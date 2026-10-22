// Task delivery failure end path against a worker that never shows its main
// prompt.
//
// When DeliverOneShotTask exhausts every attempt it ends the worker session
// (the same end path the stall handler uses, so the executor's wait loop sees
// a terminal state) and reports the failure through the callback registered
// with SetOnTaskDeliveryFailed. This test holds the fake pane on the startup
// disclaimer for the whole window and checks the session ends terminal rather
// than waiting for input, the callback fires once with a reason, and the claim
// is released.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestTaskDeliveryFailure_
//
// Each attempt waits the production oneShotPromptWindow (15s), so this test
// takes about 30s of wall time with maxAttempts=2.

package session

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTaskDeliveryFailure_CallsBackAndEndsSession(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, "tdf1")
	tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)

	var (
		mu     sync.Mutex
		gotIDs []string
		gotWhy []string
	)
	mgr.SetOnTaskDeliveryFailed(func(fullID string, reason string) {
		mu.Lock()
		defer mu.Unlock()
		gotIDs = append(gotIDs, fullID)
		gotWhy = append(gotWhy, reason)
	})

	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 100*time.Millisecond)
	if !errors.Is(err, ErrTaskUndelivered) {
		t.Fatalf("DeliverOneShotTask error = %v, want ErrTaskUndelivered", err)
	}

	mu.Lock()
	ids, reasons := append([]string(nil), gotIDs...), append([]string(nil), gotWhy...)
	mu.Unlock()
	if len(ids) != 1 {
		t.Fatalf("delivery-failed callback fired %d times, want exactly 1 (ids=%v)", len(ids), ids)
	}
	if ids[0] != sess.FullID {
		t.Errorf("callback fullID = %q, want %q", ids[0], sess.FullID)
	}
	if reasons[0] == "" || !strings.Contains(strings.ToLower(reasons[0]), "delivery") {
		t.Errorf("callback reason = %q, want a non-empty reason mentioning delivery", reasons[0])
	}

	got, ok := mgr.GetSession(sess.FullID)
	if !ok {
		t.Fatalf("session %s missing after delivery failure", sess.FullID)
	}
	if got.State == StateWaitingInput {
		t.Fatalf("session state = %s after delivery failure, want a terminal state", got.State)
	}
	if got.State != StateKilled {
		t.Errorf("session state = %s, want %s (the Kill end path)", got.State, StateKilled)
	}

	if mgr.TaskDelivered(sess.FullID) {
		t.Error("TaskDelivered = true although the task was never typed")
	}
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Error("claim was not released; a later ready cannot deliver the task")
	}
}
