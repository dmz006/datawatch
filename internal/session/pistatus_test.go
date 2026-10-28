package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pilog"
)

// sampleProgress reads the shared pi session log fixture used across the
// repo (5 assistant turns, bash:3 edit:1 read:1, last tool bash).
func sampleProgress(t *testing.T) pilog.Progress {
	t.Helper()
	p, err := pilog.Read("../../testdata/pi/session-sample.jsonl")
	if err != nil {
		t.Fatalf("pilog.Read: %v", err)
	}
	return p
}

func TestPiStatusLineSampleLog(t *testing.T) {
	p := sampleProgress(t)
	if p.LastEventAt.IsZero() {
		t.Fatal("sample log carries no LastEventAt; fixture changed?")
	}
	now := p.LastEventAt.Add(42 * time.Second)
	got := PiStatusLine(p, now)
	want := "progress: turns=5 tools=bash:3,edit:1,read:1 last=bash idle=42s"
	if got != want {
		t.Errorf("PiStatusLine = %q, want %q", got, want)
	}
}

func TestPiStatusLineEdgeCases(t *testing.T) {
	p := sampleProgress(t)

	// now before LastEventAt: idle clamps at 0, not negative.
	if got := PiStatusLine(p, p.LastEventAt.Add(-time.Minute)); !strings.HasSuffix(got, "idle=0s") {
		t.Errorf("past-now line = %q, want idle=0s", got)
	}

	// No timestamps at all: idle=unknown, turns and tools still present.
	p2 := pilog.Progress{AssistantTurns: 2, ToolCalls: map[string]int{"write": 1}}
	if got := PiStatusLine(p2, time.Now()); got != "progress: turns=2 tools=write:1 idle=unknown" {
		t.Errorf("no-timestamp line = %q", got)
	}

	// Empty snapshot: deterministic minimal line.
	if got := PiStatusLine(pilog.Progress{ToolCalls: map[string]int{}}, time.Now()); got != "progress: turns=0 idle=unknown" {
		t.Errorf("empty line = %q", got)
	}
}

func TestPiStatusLineForNonPiIsEmpty(t *testing.T) {
	p := sampleProgress(t)
	now := p.LastEventAt.Add(time.Minute)
	for _, family := range []string{"claude-code", "opencode", "ollama", ""} {
		if got := PiStatusLineFor(family, p, now); got != "" {
			t.Errorf("family %q: got %q, want empty", family, got)
		}
	}
}

func TestPiStatusLineNeverConfusableWithCompletionMarker(t *testing.T) {
	// The status line is a consumer emission only, but it must never contain
	// (or otherwise disturb) the completion marker logic applied to stored
	// output.
	p := sampleProgress(t)
	got := PiStatusLine(p, p.LastEventAt.Add(12*time.Second+400*time.Millisecond))
	if strings.Contains(got, "DATAWATCH_COMPLETE") {
		t.Errorf("status line %q must not contain the completion marker", got)
	}
	if sum := ExtractCompletionSummary(got); sum != "" {
		t.Errorf("ExtractCompletionSummary(status line) = %q, want empty", sum)
	}

	stored := "some stored output\nDATAWATCH_COMPLETE: all done\n"
	if got := ExtractCompletionSummary(stored); got != "all done" {
		t.Fatalf("ExtractCompletionSummary(stored) = %q, want %q", got, "all done")
	}
	if sum := ExtractCompletionSummary(stored + "\n" + got + ""); sum != "all done" {
		t.Errorf("ExtractCompletionSummary(stored+status) = %q, want %q", sum, "all done")
	}
}

// fakeProgress is a pi snapshot built in-memory (a "fake session"): 7 turns,
// write:2 bash:5, last tool bash.
func fakeProgress() pilog.Progress {
	return pilog.Progress{
		AssistantTurns: 7,
		ToolCalls:      map[string]int{"write": 2, "bash": 5},
		LastToolName:   "bash",
		LastEventAt:    time.Date(2026, 2, 7, 10, 0, 0, 0, time.UTC),
	}
}

var fakeWant = "progress: turns=7 tools=bash:5,write:2 last=bash idle=12s"

func TestPiStatusTickerTickEmitsForPiAndIsAbsentForNonPi(t *testing.T) {
	now := fakeProgress().LastEventAt.Add(12 * time.Second)

	for _, family := range []string{"pi", "pi-prompt"} {
		tk := &PiStatusTicker{
			BackendFamily: family,
			Progress:      func() (pilog.Progress, error) { return fakeProgress(), nil },
		}
		line, ok := tk.Tick(now)
		if !ok || line != fakeWant {
			t.Errorf("family %q: tick = (%q, %v), want (%q, true)", family, line, ok, fakeWant)
		}
	}

	nonPi := &PiStatusTicker{
		BackendFamily: "claude-code",
		Progress:      func() (pilog.Progress, error) { return fakeProgress(), nil },
	}
	if line, ok := nonPi.Tick(now); ok || line != "" {
		t.Errorf("non-pi tick = (%q, %v), want (\"\", false)", line, ok)
	}

	// No progress source: tick is skipped, no panic.
	if line, ok := (&PiStatusTicker{BackendFamily: "pi"}).Tick(now); ok || line != "" {
		t.Errorf("no-source tick = (%q, %v), want (\"\", false)", line, ok)
	}

	// Erroring progress source (log not created yet): tick skipped.
	errTk := &PiStatusTicker{
		BackendFamily: "pi",
		Progress:      func() (pilog.Progress, error) { return pilog.Progress{}, errors.New("no log") },
	}
	if line, ok := errTk.Tick(now); ok || line != "" {
		t.Errorf("error tick = (%q, %v), want (\"\", false)", line, ok)
	}
}

// tickedEmit collects emitted lines safely from Start's goroutine.
type tickedEmit struct {
	mu    sync.Mutex
	lines []string
}

func (e *tickedEmit) add(line string) { e.mu.Lock(); e.lines = append(e.lines, line); e.mu.Unlock() }
func (e *tickedEmit) get() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.lines...)
}

func runBoundedTicker(t *testing.T, family string, window, interval time.Duration) (lines []string, want string) {
	t.Helper()
	fixedNow := fakeProgress().LastEventAt.Add(12 * time.Second)
	e := &tickedEmit{}
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()
	done := make(chan struct{})
	go func() {
		(&PiStatusTicker{
			Interval:      interval,
			BackendFamily: family,
			Progress:      func() (pilog.Progress, error) { return fakeProgress(), nil },
			Emit:          e.add,
			Now:           func() time.Time { return fixedNow },
		}).Start(ctx)
		close(done)
	}()
	<-done
	return e.get(), "progress: turns=7 tools=bash:5,write:2 last=bash idle=12s"
}

// TestPiStatusTickerStartEmitsOnTick proves the status line is emitted on the
// immediate tick and again on each fixed interval for pi sessions.
func TestPiStatusTickerStartEmitsOnTick(t *testing.T) {
	for _, family := range []string{"pi", "pi-prompt"} {
		lines, want := runBoundedTicker(t, family, 70*time.Millisecond, 20*time.Millisecond)
		// window ~70ms with 20ms interval: immediate tick + at least one
		// interval tick.
		if len(lines) < 2 {
			t.Errorf("family %q: got %d lines %v, want >=2", family, len(lines), lines)
		}
		for _, l := range lines {
			if l != want {
				t.Errorf("family %q: line %q, want %q", family, l, want)
			}
		}
	}
}

// TestPiStatusTickerStartAbsentForNonPi proves the same run emits nothing at
// all for non-pi sessions.
func TestPiStatusTickerStartAbsentForNonPi(t *testing.T) {
	lines, _ := runBoundedTicker(t, "claude-code", 70*time.Millisecond, 20*time.Millisecond)
	if len(lines) != 0 {
		t.Errorf("non-pi family: got lines %v, want none", lines)
	}
}

// TestPiStatusTickerDefaultInterval proves Interval 0 falls back to the 60s
// default: within a short window only the immediate tick fires (a small
// fallback interval would have produced more).
func TestPiStatusTickerDefaultInterval(t *testing.T) {
	lines, _ := runBoundedTicker(t, "pi", 50*time.Millisecond, 0)
	if len(lines) != 1 {
		t.Errorf("default interval: got %d lines %v, want 1 (immediate tick only)", len(lines), lines)
	}
}
