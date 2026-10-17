package autonomous

import (
	"fmt"
	"strings"
	"sync"
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
	m := &Manager{}
	defer m.lifecycleBus().Close()

	capSink := &textCapture{}
	cancel := InstallLifecycleSinks(m, SinkConfig{
		Kinds:      []string{TaskStatusKind(TaskInProgress)},
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
	m := &Manager{}
	defer m.lifecycleBus().Close()

	capSink := &textCapture{}
	cancel := InstallLifecycleSinks(m, SinkConfig{
		Kinds:      []string{TaskStatusKind(TaskInProgress)},
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
