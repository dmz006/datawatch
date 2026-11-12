// Tests for completion detection that must come after the echoed task text.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestCompletionAfterEcho

package session

import (
	"strings"
	"testing"
	"time"
)

const caeMarker = "DATAWATCH_COMPLETE:"

// caeAccepts reports whether a scan of pane for task finds a real completion
// line located after the echoed task text.
func caeAccepts(pane, task string) bool {
	lines := strings.Split(pane, "\n")
	echoEnd := lastTaskEchoIndex(lines, task)
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if !echoGate(i, echoEnd) {
			return false
		}
		if isRealCompletionLine(l, caeMarker) {
			return true
		}
	}
	return false
}

func TestCompletionAfterEcho_MarkerInsideEchoedTaskIsNotComplete(t *testing.T) {
	task := "Implement the widget.\nWhen done print DATAWATCH_COMPLETE: all done with the widget"
	pane := "> Implement the widget.\n" +
		"  When done print\n" +
		"DATAWATCH_COMPLETE: all done with the widget\n"
	if got := lastTaskEchoIndex(strings.Split(pane, "\n"), task); got != 2 {
		t.Fatalf("lastTaskEchoIndex = %d, want 2", got)
	}
	if caeAccepts(pane, task) {
		t.Error("marker inside the echoed task text counted as completion")
	}
}

func TestCompletionAfterEcho_PlaceholderOnlyIsNotComplete(t *testing.T) {
	if caeAccepts(tdEchoed, tdTask) {
		t.Error("echoed placeholder counted as completion")
	}
	// Task text not locatable: placeholder and empty-summary rules still hold.
	for _, l := range []string{
		"DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>",
		"DATAWATCH_COMPLETE:",
		"DATAWATCH_COMPLETE:   ",
	} {
		if caeAccepts(l+"\n", "an unrelated task that is not on screen") {
			t.Errorf("%q counted as completion", l)
		}
	}
}

func TestCompletionAfterEcho_RealSummaryAfterEchoIsComplete(t *testing.T) {
	if !caeAccepts(tdEchoed+"● Added the widget.\nDATAWATCH_COMPLETE: added the widget and its tests\n", tdTask) {
		t.Error("real summary after the echoed task was rejected")
	}
}

func TestCompletionAfterEcho_PersistedOnlyDeliveryTimeSuppresses(t *testing.T) {
	b, _, sess, ended := restartEchoSetup(t, tdEchoed)
	b.mu.Lock()
	inMem := len(b.taskDeliveredAt)
	b.mu.Unlock()
	if inMem != 0 {
		t.Fatalf("in-memory map not empty: %d", inMem)
	}
	got, ok := b.taskDeliveredTime(sess)
	if !ok || time.Since(got) > time.Minute {
		t.Fatalf("taskDeliveredTime = %v,%v; want the persisted recent time", got, ok)
	}
	cur := restartEchoObserve(t, b, sess)
	if cur.State != StateRunning || ended.Load() != 0 {
		t.Errorf("state=%q ended=%d; persisted delivery time did not suppress the echo", cur.State, ended.Load())
	}
}

func TestCompletionAfterEcho_WrappedPlaceholder(t *testing.T) {
	for l, want := range map[string]bool{
		"DATAWATCH_COMPLETE: <one-line plain-text":                           true,
		"summary of what was done>":                                          true,
		"DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>": true,
		"DATAWATCH_COMPLETE: added the widget":                               false,
		"Fixed the bug -> summary":                                           false,
	} {
		if got := isCompletionPlaceholder(l); got != want {
			t.Errorf("isCompletionPlaceholder(%q) = %v, want %v", l, got, want)
		}
	}
	pane := "> Implement the widget.\n  Finish with a line: DATAWATCH_COMPLETE: <one-line plain-text\n  summary of what was done>\n"
	if caeAccepts(pane, tdTask) {
		t.Error("wrapped placeholder counted as completion")
	}
}

func TestCompletionAfterEcho_LegacySessionWithoutDeliveryTimeUnchanged(t *testing.T) {
	m, _ := newTestManagerWithFake(t)
	sess, err := m.Start(t.Context(), "legacy task", "", t.TempDir(), &StartOptions{Backend: "claude-code", OneShot: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.taskDeliveredTime(sess); ok {
		t.Error("session without TaskDeliveredAt reported a delivery time")
	}
	// No task text to locate: existing rules apply and a real line completes.
	if lastTaskEchoIndex([]string{"x", caeMarker + " did it"}, "") != -1 {
		t.Error("empty task located")
	}
	if !caeAccepts("working\nDATAWATCH_COMPLETE: did it\n", "") {
		t.Error("real completion rejected for a legacy session")
	}
}
