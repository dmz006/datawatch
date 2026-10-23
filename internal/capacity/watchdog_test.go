package capacity

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchdogNormalSnapshotNoReport(t *testing.T) {
	l := New(Options{})
	var n atomic.Int32
	l.SnapshotWatched(50*time.Millisecond, func(time.Duration, string) { n.Add(1) })
	time.Sleep(100 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatalf("report called %d times", n.Load())
	}
}

// wedgedLedger returns a ledger whose External callback blocks while the
// gate channel is open.
func wedgedLedger() (*Ledger, chan struct{}) {
	gate := make(chan struct{})
	l := New(Options{External: func(string) int { <-gate; return 0 }})
	l.SetLimit("p", 1)
	return l, gate
}

func runWedge(t *testing.T, l *Ledger, gate chan struct{}, calls int, reports *atomic.Int32, stacks chan string) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.SnapshotWatched(50*time.Millisecond, func(_ time.Duration, s string) {
				reports.Add(1)
				select {
				case stacks <- s:
				default:
				}
			})
		}()
	}
	time.Sleep(300 * time.Millisecond)
	close(gate)
	wg.Wait()
}

func TestWatchdogWedgeReportsOnceAndRearms(t *testing.T) {
	var reports atomic.Int32
	stacks := make(chan string, 4)

	l, gate := wedgedLedger()
	runWedge(t, l, gate, 4, &reports, stacks)
	if reports.Load() != 1 {
		t.Fatalf("first wedge: reports = %d, want 1", reports.Load())
	}
	if s := <-stacks; !strings.Contains(s, "internal/capacity") {
		t.Fatalf("stacks do not mention capacity package: %q", s)
	}
	if l.wedgeReported.Load() {
		t.Fatal("flag not cleared after release")
	}

	// A second, separate wedge on the same ledger reports again.
	gate2 := make(chan struct{})
	l.opt.External = func(string) int { <-gate2; return 0 }
	runWedge(t, l, gate2, 2, &reports, stacks)
	if reports.Load() != 2 {
		t.Fatalf("second wedge: reports = %d, want 2", reports.Load())
	}
}

func TestWatchdogLogLockWedge(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	LogLockWedge(7*time.Second+300*time.Millisecond, "stack text")
	out := buf.String()
	if !strings.Contains(out, "capacity ledger lock held for 7 seconds") || !strings.Contains(out, "stack text") {
		t.Fatalf("unexpected log output: %q", out)
	}
}
