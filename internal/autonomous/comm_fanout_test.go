package autonomous

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Comm fan-out (quiet-window coalescing) tests, following the
// lifecycle_event_test.go patterns: a bare &Manager{} bus plus
// InstallLifecycleSinks with an injected capture sink, and the
// package's shared waitFor() to settle.
//
// Timing: the window is 150ms and every settle wait below goes through
// waitFor's 5s deadline — comfortably >= 4x the window — so the tests
// do not flap on slow CI. No parallel subtests: the capture sink and
// the bus timers are shared per-test state.

const commFanoutTestWindow = 150 * time.Millisecond

// newCommFanoutManager builds a real Manager (store + bus) whose Automata
// opt into task.in_progress — a kind the global (default) tier does NOT
// deliver — so these tests exercise the resolver's Automaton tier: the
// comm fan-out subscribes to every notify kind and delivers only what
// each event's Automaton resolved set allows.
func newCommFanoutManager(t *testing.T, prdIDs ...string) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.lifecycleBus().Close() })
	for _, id := range prdIDs {
		if err := m.Store().SavePRD(&PRD{ID: id, NotifyEvents: []string{TaskStatusKind(TaskInProgress)}}); err != nil {
			t.Fatalf("SavePRD(%s): %v", id, err)
		}
	}
	return m
}

// textCapture records every comm send() argument so tests can assert
// an exact send count AND the exact lines inside each send.
type textCapture struct {
	mu    sync.Mutex
	texts []string
}

func (c *textCapture) record(text string) {
	c.mu.Lock()
	c.texts = append(c.texts, text)
	c.mu.Unlock()
}

func (c *textCapture) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...)
}

func commFanoutFire(m *Manager, prdID, taskID string) {
	m.emit(LifecycleEvent{
		Kind:      TaskStatusKind(TaskInProgress),
		Level:     LevelTask,
		PRDID:     prdID,
		TaskID:    taskID,
		NewStatus: string(TaskInProgress),
	})
}

func mustContain(t *testing.T, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Fatalf("send is missing %q; got:\n%s", w, text)
		}
	}
}

func mustNotContain(t *testing.T, where, text string, banned ...string) {
	t.Helper()
	for _, b := range banned {
		if strings.Contains(text, b) {
			t.Fatalf("%s leaked a line from another Automaton (%q):\n%s", where, b, text)
		}
	}
}

// TestCommFanoutCoalesces: a burst of 5 events for the SAME PRD within
// one quiet window must produce exactly ONE comm send whose text is the
// shared header plus all 5 distinct lines.
func TestCommFanoutCoalesces(t *testing.T) {
	m := newCommFanoutManager(t, "prd-fanout")

	capSink := &textCapture{}
	cancel := InstallLifecycleSinks(m, SinkConfig{
		CommSink:   capSink.record,
		CommWindow: commFanoutTestWindow,
	})
	defer cancel()

	const n = 5
	const prdID = "prd-fanout"
	markers := make([]string, n)
	for i := 0; i < n; i++ {
		// Distinct task id per event: no content overlap can hide a loss.
		markers[i] = fmt.Sprintf("fanout-%d", i)
		commFanoutFire(m, prdID, markers[i])
	}

	wantHeader := fmt.Sprintf("Lifecycle updates (%d)", n)
	waitFor(t, "one coalesced comm send with all 5 lines", func() bool {
		texts := capSink.snapshot()
		if len(texts) != 1 {
			return false
		}
		if !strings.Contains(texts[0], wantHeader) {
			return false
		}
		for _, mk := range markers {
			if !strings.Contains(texts[0], mk) {
				return false
			}
		}
		return true
	})

	texts := capSink.snapshot()
	if len(texts) != 1 {
		t.Fatalf("captured %d sends, want exactly 1:\n%q", len(texts), texts)
	}
	mustContain(t, texts[0], wantHeader, "PRD "+prdID, markers[0], markers[1], markers[2], markers[3], markers[4])
}

// TestCommFanoutSeparateAutomata: interleaved bursts for PRD A (2
// events) and PRD B (3 events) must produce exactly TWO sends, and each
// send must contain only its own Automaton's lines (per-Automaton
// isolation: one burst must not coalesce into the other's message).
func TestCommFanoutSeparateAutomata(t *testing.T) {
	m := newCommFanoutManager(t, "prd-fanout-a", "prd-fanout-b")

	capSink := &textCapture{}
	cancel := InstallLifecycleSinks(m, SinkConfig{
		CommSink:   capSink.record,
		CommWindow: commFanoutTestWindow,
	})
	defer cancel()

	// Interleaved: A, B, A, B, B — burst order deliberately mixed.
	commFanoutFire(m, "prd-fanout-a", "ab-1")
	commFanoutFire(m, "prd-fanout-b", "bb-1")
	commFanoutFire(m, "prd-fanout-a", "ab-2")
	commFanoutFire(m, "prd-fanout-b", "bb-2")
	commFanoutFire(m, "prd-fanout-b", "bb-3")

	waitFor(t, "two coalesced comm sends", func() bool {
		return len(capSink.snapshot()) == 2
	})

	texts := capSink.snapshot()
	if len(texts) != 2 {
		t.Fatalf("captured %d sends, want exactly 2:\n%q", len(texts), texts)
	}

	foundA, foundB := false, false
	for _, text := range texts {
		isA := strings.Contains(text, "ab-1") || strings.Contains(text, "ab-2")
		isB := strings.Contains(text, "bb-1") || strings.Contains(text, "bb-2") || strings.Contains(text, "bb-3")
		if isA {
			foundA = true
			mustContain(t, text, "Lifecycle updates (2)", "PRD prd-fanout-a", "ab-1", "ab-2")
			mustNotContain(t, "PRD A send", text, "PRD prd-fanout-b", "bb-1", "bb-2", "bb-3")
		}
		if isB {
			foundB = true
			mustContain(t, text, "Lifecycle updates (3)", "PRD prd-fanout-b", "bb-1", "bb-2", "bb-3")
			mustNotContain(t, "PRD B send", text, "PRD prd-fanout-a", "ab-1", "ab-2")
		}
		if isA && isB {
			t.Fatalf("one send mixed both Automata' lines (no per-Automaton isolation):\n%s", text)
		}
	}
	if !foundA || !foundB {
		t.Fatalf("expected one send each for PRD A and PRD B, got:\n%q", texts)
	}
}

// TestCommFanoutPanickingSinkRecoveredAndSinkStaysUsable: the send hook
// is injected — if it panics, deliver() must recover around it: that
// one message is lost, but the process and the sink must keep working,
// and later events are delivered normally.
func TestCommFanoutPanickingSinkRecoveredAndSinkStaysUsable(t *testing.T) {
	m := newCommFanoutManager(t, "prd-flaky")

	capSink := &textCapture{}
	var panics int32
	flakySink := func(text string) {
		if atomic.AddInt32(&panics, 1) == 1 {
			panic("injected transport failure")
		}
		capSink.record(text)
	}
	cancel := InstallLifecycleSinks(m, SinkConfig{
		CommSink:   flakySink,
		CommWindow: commFanoutTestWindow,
	})
	defer cancel()

	// First event: its delivery panics inside send — it must be
	// recovered (no crashed daemon), so the waiter keeps going.
	commFanoutFire(m, "prd-flaky", "flaky-1")
	waitFor(t, "the panicking delivery to have been attempted", func() bool {
		return atomic.LoadInt32(&panics) == 1
	})

	// The sink must still be usable: the next event is coalesced and
	// delivered exactly as if nothing had happened.
	commFanoutFire(m, "prd-flaky", "flaky-2")
	waitFor(t, "the post-panic delivery", func() bool {
		for _, text := range capSink.snapshot() {
			if strings.Contains(text, "flaky-2") {
				return true
			}
		}
		return false
	})

	// The panicking sink lost its own message; none may have leaked.
	for _, text := range capSink.snapshot() {
		if strings.Contains(text, "flaky-1") {
			t.Errorf("the message lost to a panicking sink leaked through:\n%s", text)
		}
	}
}
