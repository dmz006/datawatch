// Pre-push secret-scan counters.
//
// Every push path that scans (shadow or enforce, via scanBeforePush in
// push_scan.go) records into this package-level, concurrency-safe set of
// counters, since daemon start (in-memory only — reset on restart). The
// stats package snapshots them into SystemStats.SecretScan through the
// callback wired in cmd/datawatch/main.go.

package git

import (
	"sync/atomic"
	"time"
)

// secretScanNow is the clock the counters use for their timestamps.
// Tests replace it to make last_run_at / last_refusal_at deterministic.
var secretScanNow = time.Now

// SecretScanStats is a point-in-time snapshot of the pre-push
// secret-scan counters: how many scans actually executed, how many
// findings they produced, how many shadow-mode findings were reported
// (scan ran, push proceeded) and how many enforce-mode findings refused
// the push, plus the times of the last of each. All counts are since
// daemon start and reset on restart.
type SecretScanStats struct {
	Runs          int64     `json:"runs"`
	Findings      int64     `json:"findings"`
	ShadowReports int64     `json:"shadow_reports"`
	Refusals      int64     `json:"refusals"`
	LastRunAt     time.Time `json:"last_run_at"`
	LastRefusalAt time.Time `json:"last_refusal_at"`
}

var (
	ssRuns            atomic.Int64
	ssFindings        atomic.Int64
	ssShadowReports   atomic.Int64
	ssRefusals        atomic.Int64
	ssLastRunNano     atomic.Int64
	ssLastRefusalNano atomic.Int64
)

// RecordSecretScanRun counts one scan that actually executed (shadow or
// enforce) and stamps its time. A scan that errored internally still
// counts as a run — it executed, it just found nothing it could report.
func RecordSecretScanRun() {
	ssRuns.Add(1)
	ssLastRunNano.Store(secretScanNow().UnixNano())
}

// RecordSecretScanFindings adds n findings from one scan (no-op for n <= 0).
func RecordSecretScanFindings(n int) {
	if n <= 0 {
		return
	}
	ssFindings.Add(int64(n))
}

// RecordSecretScanShadowReport counts one shadow-mode scan that found
// something and reported it (the push still proceeded).
func RecordSecretScanShadowReport() {
	ssShadowReports.Add(1)
}

// RecordSecretScanRefusal counts one enforce-mode scan that refused the
// push and stamps its time.
func RecordSecretScanRefusal() {
	ssRefusals.Add(1)
	ssLastRefusalNano.Store(secretScanNow().UnixNano())
}

// SecretScanStatsSnapshot returns the current counters. Safe to call
// from any goroutine; it is a consistent-enough snapshot for stats
// purposes (each counter is read independently).
func SecretScanStatsSnapshot() SecretScanStats {
	ss := SecretScanStats{
		Runs:          ssRuns.Load(),
		Findings:      ssFindings.Load(),
		ShadowReports: ssShadowReports.Load(),
		Refusals:      ssRefusals.Load(),
	}
	if n := ssLastRunNano.Load(); n != 0 {
		ss.LastRunAt = time.Unix(0, n).UTC()
	}
	if n := ssLastRefusalNano.Load(); n != 0 {
		ss.LastRefusalAt = time.Unix(0, n).UTC()
	}
	return ss
}

// resetSecretScanStats zeroes every counter and timestamp. Tests only —
// it is unexported on purpose; there is no operator-facing reset.
func resetSecretScanStats() {
	ssRuns.Store(0)
	ssFindings.Store(0)
	ssShadowReports.Store(0)
	ssRefusals.Store(0)
	ssLastRunNano.Store(0)
	ssLastRefusalNano.Store(0)
}
