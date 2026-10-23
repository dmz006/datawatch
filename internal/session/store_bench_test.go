package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Session store write-amplification measurement. Store.Save re-marshals every
// session and rewrites the whole sessions.json, so a single channel-event
// update costs the size of the entire store. See
// docs/plans/2026-10-23-session-store-write-amplification.md.

// storeWriteBudgetEnforced gates the budget assertion in TestStoreWriteBudget.
// Enforced since the store coalesces writes in a background writer.
const storeWriteBudgetEnforced = true

// storeWriteBudgetFactor is the allowed multiple of the changed data.
const storeWriteBudgetFactor = 20

const (
	storeBenchFullSessions  = 4000
	storeBenchSmallSessions = 400
	storeBenchBurstEvents   = 200
	storeBenchBurstSessions = 10
	storeBenchBigTaskBytes  = 100 * 1024
)

// storeBenchSmall reports whether the reduced seed set should be used.
func storeBenchSmall() bool {
	return testing.Short() || os.Getenv("STORE_BENCH_SMALL") == "1"
}

// seedBenchStore builds a Store in dir holding n sessions; every tenth session
// carries a 100 KB task field (bigTask of them in total, returned).
func seedBenchStore(tb testing.TB, dir string, n int) (*Store, int) {
	tb.Helper()
	// Seed write-through so no writer goroutine is armed before the burst.
	st, err := NewStore(filepath.Join(dir, "sessions.json"), WithFlushInterval(0))
	if err != nil {
		tb.Fatalf("NewStore: %v", err)
	}
	big := strings.Repeat("x", storeBenchBigTaskBytes)
	bigCount := 0
	now := time.Now()
	st.Mutate(func() {
		for i := 0; i < n; i++ {
			task := "task " + strconv.Itoa(i)
			if i%10 == 0 {
				task = big
				bigCount++
			}
			id := fmt.Sprintf("%04x", i)
			st.sessions["bench-"+id] = &Session{
				ID:        id,
				FullID:    "bench-" + id,
				Name:      "bench session " + strconv.Itoa(i),
				Task:      task,
				State:     StateRunning,
				Hostname:  "bench",
				CreatedAt: now,
				UpdatedAt: now,
			}
		}
	})
	if err := st.Flush(); err != nil {
		tb.Fatalf("Flush: %v", err)
	}
	// The burst runs against the production, coalescing configuration.
	st.SetFlushInterval(time.Second)
	return st, bigCount
}

// cpuTime returns user+system CPU time consumed by this process so far.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// procBytesWritten returns this process's write(2) byte count from
// /proc/self/io (wchar), or -1 when unavailable.
func procBytesWritten() int64 {
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "wchar:"); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err == nil {
				return n
			}
		}
	}
	return -1
}

type burstResult struct {
	elapsed time.Duration
	cpu     time.Duration
	bytes   int64
}

// runChannelEventBurst mutates a session field under Store.Mutate then Save,
// the way markChannelEventAt does, spread round-robin over the first
// storeBenchBurstSessions sessions. Bytes written come from /proc/self/io,
// falling back to the file size after each Save (every Save rewrites it all).
func runChannelEventBurst(tb testing.TB, st *Store, events int) burstResult {
	tb.Helper()
	targets := make([]*Session, 0, storeBenchBurstSessions)
	for i := 0; i < storeBenchBurstSessions; i++ {
		s, ok := st.Get(fmt.Sprintf("bench-%04x", i))
		if !ok {
			tb.Fatalf("burst target %d missing", i)
		}
		targets = append(targets, s)
	}
	w0 := procBytesWritten()
	var statSum int64
	c0 := cpuTime()
	t0 := time.Now()
	for i := 0; i < events; i++ {
		s := targets[i%len(targets)]
		ts := time.Now()
		st.Mutate(func() { s.LastChannelEventAt = ts })
		if err := st.Save(s); err != nil {
			tb.Fatalf("Save: %v", err)
		}
		if w0 < 0 {
			if fi, err := os.Stat(st.path); err == nil {
				statSum += fi.Size()
			}
		}
	}
	res := burstResult{elapsed: time.Since(t0), cpu: cpuTime() - c0}
	if w0 >= 0 {
		res.bytes = procBytesWritten() - w0
	} else {
		res.bytes = statSum
	}
	return res
}

func storeBenchSessionCount() int {
	if storeBenchSmall() {
		return storeBenchSmallSessions
	}
	return storeBenchFullSessions
}

func TestStoreBenchSeed(t *testing.T) {
	st, big := seedBenchStore(t, t.TempDir(), 50)
	if got := len(st.List()); got != 50 {
		t.Fatalf("sessions = %d, want 50", got)
	}
	if big != 5 {
		t.Fatalf("big-task sessions = %d, want 5", big)
	}
	n := 0
	for _, s := range st.List() {
		if len(s.Task) == storeBenchBigTaskBytes {
			n++
		}
	}
	if n != big {
		t.Fatalf("stored big tasks = %d, want %d", n, big)
	}
	// Reloading from disk must give the same counts.
	re, err := NewStore(st.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(re.List()); got != 50 {
		t.Fatalf("reloaded sessions = %d, want 50", got)
	}
}

func BenchmarkStoreSaveBurst(b *testing.B) {
	n := storeBenchSessionCount()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st, _ := seedBenchStore(b, b.TempDir(), n)
		b.StartTimer()
		res := runChannelEventBurst(b, st, storeBenchBurstEvents)
		b.StopTimer()
		b.ReportMetric(float64(res.elapsed.Milliseconds()), "burst-ms")
		b.ReportMetric(float64(res.cpu.Milliseconds()), "cpu-ms")
		b.ReportMetric(float64(res.bytes), "bytes-written")
		b.Logf("sessions=%d burst=%d elapsed=%s cpu=%s bytes=%d",
			n, storeBenchBurstEvents, res.elapsed, res.cpu, res.bytes)
	}
}

func TestStoreWriteBudget(t *testing.T) {
	// While the budget is not enforced use the small set: the old whole-file
	// rewrite burned gigabytes on the full one.
	n := storeBenchSessionCount()
	if !storeWriteBudgetEnforced {
		n = storeBenchSmallSessions
	}
	st, _ := seedBenchStore(t, t.TempDir(), n)
	one, err := json.Marshal(st.sessions["bench-0001"])
	if err != nil {
		t.Fatal(err)
	}
	changed := int64(storeBenchBurstEvents) * int64(len(one))
	budget := changed * storeWriteBudgetFactor
	res := runChannelEventBurst(t, st, storeBenchBurstEvents)
	t.Logf("sessions=%d burst=%d elapsed=%s cpu=%s bytes=%d changed=%d budget=%d",
		n, storeBenchBurstEvents, res.elapsed, res.cpu, res.bytes, changed, budget)
	if res.bytes <= budget {
		return
	}
	if !storeWriteBudgetEnforced {
		t.Skipf("write budget exceeded (%d > %d bytes) and not enforced: storage still rewrites the whole file", res.bytes, budget)
	}
	t.Fatalf("burst wrote %d bytes, budget %d (%dx %d changed bytes)", res.bytes, budget, storeWriteBudgetFactor, changed)
}
