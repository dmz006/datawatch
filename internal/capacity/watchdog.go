package capacity

import (
	"log"
	"runtime"
	"strings"
	"time"
)

// DefaultLockWatchdogThreshold is how long a watched Snapshot may wait before
// the wedge is reported.
const DefaultLockWatchdogThreshold = 5 * time.Second

// SnapshotWatched is Snapshot with a watchdog: if it has not returned after
// threshold (zero or less uses DefaultLockWatchdogThreshold), report is called
// once with the time waited and the stacks of goroutines inside this package,
// which include the lock holder. The report fires at most once per wedge
// episode across concurrent callers; the episode ends when a watched Snapshot
// completes. The timer uses the real clock, not the ledger's injected Clock.
func (l *Ledger) SnapshotWatched(threshold time.Duration, report func(waited time.Duration, stacks string)) Status {
	if threshold <= 0 {
		threshold = DefaultLockWatchdogThreshold
	}
	start := time.Now()
	timer := time.AfterFunc(threshold, func() {
		if !l.wedgeReported.CompareAndSwap(false, true) {
			return
		}
		if report != nil {
			report(time.Since(start), capacityStacks())
		}
	})
	defer func() {
		timer.Stop()
		l.wedgeReported.Store(false)
	}()
	return l.Snapshot()
}

// capacityStacks dumps all goroutine stacks, keeping those whose trace
// mentions the capacity package.
func capacityStacks() string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var keep []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "internal/capacity") {
			keep = append(keep, g)
		}
	}
	return strings.Join(keep, "\n\n")
}

// LogLockWedge is the default reporter for SnapshotWatched.
func LogLockWedge(waited time.Duration, stacks string) {
	log.Printf("ERROR capacity ledger lock held for %d seconds\n%s", int(waited.Seconds()), stacks)
}
