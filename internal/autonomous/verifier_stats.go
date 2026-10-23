package autonomous

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Verifier run outcome categories counted by VerifierStats.
const (
	VerifierCatPass      = "pass"
	VerifierCatContent   = "content"
	VerifierCatInfra     = "infra"
	VerifierCatWatchdog  = "watchdog"
	VerifierCatTimeout   = "timeout"
	VerifierCatCapacity  = "capacity"
	VerifierCatEscalated = "escalated"
)

// VerifierCategories lists every category in a stable order.
var VerifierCategories = []string{
	VerifierCatPass, VerifierCatContent, VerifierCatInfra, VerifierCatWatchdog,
	VerifierCatTimeout, VerifierCatCapacity, VerifierCatEscalated,
}

// VerifierStatsView is the JSON shape of the rolling verifier aggregates, as
// carried in the capacity response under "verifier".
type VerifierStatsView struct {
	Runs         int            `json:"runs"`
	WaitTotalSec float64        `json:"wait_total_seconds"`
	WaitAvgSec   float64        `json:"wait_avg_seconds"`
	WaitMaxSec   float64        `json:"wait_max_seconds"`
	RunTotalSec  float64        `json:"run_total_seconds"`
	RunAvgSec    float64        `json:"run_avg_seconds"`
	RunMaxSec    float64        `json:"run_max_seconds"`
	Categories   map[string]int `json:"categories"`
	WatchdogEnds int            `json:"watchdog_ends"`
	Escalations  int            `json:"escalations"`
	Since        time.Time      `json:"since"`
}

// VerifierStats keeps rolling aggregates of verifier queue wait, run time and
// outcome category since the daemon started.
type VerifierStats struct {
	mu    sync.Mutex
	now   func() time.Time
	since time.Time
	v     VerifierStatsView
}

// NewVerifierStats returns an empty aggregate; a nil clock means time.Now.
func NewVerifierStats(now func() time.Time) *VerifierStats {
	if now == nil {
		now = time.Now
	}
	s := &VerifierStats{now: now}
	s.reset()
	return s
}

func (s *VerifierStats) reset() {
	s.since = s.now()
	s.v = VerifierStatsView{Categories: map[string]int{}}
}

// DefaultVerifierStats is the process-wide aggregate the verifier records into.
var DefaultVerifierStats = NewVerifierStats(nil)

// Record adds one verifier run. wait is the time from requesting the lease to
// being granted, run is grant to result; either is zero when that phase never
// happened (for example an admission failure has no run time).
func (s *VerifierStats) Record(wait, run time.Duration, category string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if wait < 0 {
		wait = 0
	}
	if run < 0 {
		run = 0
	}
	ws, rs := wait.Seconds(), run.Seconds()
	s.v.Runs++
	s.v.WaitTotalSec += ws
	s.v.RunTotalSec += rs
	if ws > s.v.WaitMaxSec {
		s.v.WaitMaxSec = ws
	}
	if rs > s.v.RunMaxSec {
		s.v.RunMaxSec = rs
	}
	s.v.Categories[category]++
	switch category {
	case VerifierCatWatchdog:
		s.v.WatchdogEnds++
	case VerifierCatEscalated:
		s.v.Escalations++
	}
}

// Snapshot returns a copy of the aggregates; averages are zero with no runs.
func (s *VerifierStats) Snapshot() VerifierStatsView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.v
	out.Since = s.since
	out.Categories = make(map[string]int, len(VerifierCategories))
	for _, c := range VerifierCategories {
		out.Categories[c] = s.v.Categories[c]
	}
	if out.Runs > 0 {
		out.WaitAvgSec = out.WaitTotalSec / float64(out.Runs)
		out.RunAvgSec = out.RunTotalSec / float64(out.Runs)
	}
	return out
}

// VerifierCategoryFor maps a verifier result to a category. escalated wins
// over the outcome, as the run used the fallback backend.
func VerifierCategoryFor(ok bool, resultCategory, summary string, escalated bool) string {
	s := strings.ToLower(summary)
	switch {
	case escalated:
		return VerifierCatEscalated
	case strings.Contains(s, "no-progress watchdog"):
		return VerifierCatWatchdog
	case strings.Contains(s, "timed out") || strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded"):
		return VerifierCatTimeout
	case strings.Contains(s, "verifier: capacity:"):
		return VerifierCatCapacity
	case ok:
		return VerifierCatPass
	case resultCategory == "infra":
		return VerifierCatInfra
	default:
		return VerifierCatContent
	}
}

func fmtVerifierSeconds(sec float64) string {
	d := time.Duration(sec * float64(time.Second)).Round(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
}

// FormatVerifierLine renders the one-line status text, for example
// "verifiers: 12 runs, avg wait 38s, avg run 6m, infra 3, watchdog 1".
func FormatVerifierLine(v VerifierStatsView) string {
	if v.Runs == 0 {
		return "verifiers: no runs yet\n"
	}
	avgW, avgR := v.WaitAvgSec, v.RunAvgSec
	if avgW == 0 && v.WaitTotalSec > 0 {
		avgW = v.WaitTotalSec / float64(v.Runs)
	}
	if avgR == 0 && v.RunTotalSec > 0 {
		avgR = v.RunTotalSec / float64(v.Runs)
	}
	return fmt.Sprintf("verifiers: %d runs, avg wait %s, avg run %s, infra %d, watchdog %d\n",
		v.Runs, fmtVerifierSeconds(avgW), fmtVerifierSeconds(avgR),
		v.Categories[VerifierCatInfra], v.Categories[VerifierCatWatchdog])
}
