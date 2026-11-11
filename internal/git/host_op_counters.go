// Git host operation counters.
//
// Every host-facing operation (push, pull request, merge, tag, CI read)
// records its outcome here. The counters keep the outcomes of the last
// window in memory only, with no store, and are reset on restart. The stats
// package snapshots them into SystemStats.GitHosts through the callback
// wired in cmd/datawatch/main.go.

package git

import (
	"sync"
	"time"
)

// Host operation kinds reported by HostOpStatsSnapshot.
const (
	HostOpPush        = "push"
	HostOpPullRequest = "pull_request"
	HostOpMerge       = "merge"
	HostOpTag         = "tag"
	HostOpCIRead      = "ci_read"
)

// HostOpWindow is how far back the per-kind counts look.
const HostOpWindow = time.Hour

// hostOpNow is the clock the counters use. Tests replace it.
var hostOpNow = time.Now

type hostOpEvent struct {
	at   time.Time
	kind string
	ok   bool
}

var (
	hostOpMu      sync.Mutex
	hostOpEvents  []hostOpEvent
	hostOpLastOK  time.Time
	hostOpLastSet bool
)

// HostOpStats is a point-in-time snapshot of the host operation counters.
// Operations holds the count per kind over Window; Failed counts the failed
// operations over the same window; LastSuccessAt is zero before any success.
type HostOpStats struct {
	Window        time.Duration  `json:"window_seconds"`
	Operations    map[string]int `json:"operations"`
	Failed        int            `json:"failed"`
	LastSuccessAt time.Time      `json:"last_success_at"`
}

// RecordHostOp records one finished host operation of kind. ok is false for
// a failed operation. Events older than HostOpWindow are dropped.
func RecordHostOp(kind string, ok bool) {
	now := hostOpNow()
	hostOpMu.Lock()
	defer hostOpMu.Unlock()
	pruneHostOpsLocked(now)
	hostOpEvents = append(hostOpEvents, hostOpEvent{at: now, kind: kind, ok: ok})
	if ok {
		hostOpLastOK = now
		hostOpLastSet = true
	}
}

func pruneHostOpsLocked(now time.Time) {
	cut := now.Add(-HostOpWindow)
	i := 0
	for i < len(hostOpEvents) && hostOpEvents[i].at.Before(cut) {
		i++
	}
	if i > 0 {
		hostOpEvents = append([]hostOpEvent(nil), hostOpEvents[i:]...)
	}
}

// HostOpStatsSnapshot returns the counters as of now.
func HostOpStatsSnapshot() HostOpStats {
	now := hostOpNow()
	hostOpMu.Lock()
	defer hostOpMu.Unlock()
	pruneHostOpsLocked(now)
	out := HostOpStats{
		Window: HostOpWindow,
		Operations: map[string]int{
			HostOpPush: 0, HostOpPullRequest: 0, HostOpMerge: 0, HostOpTag: 0, HostOpCIRead: 0,
		},
	}
	for _, e := range hostOpEvents {
		out.Operations[e.kind]++
		if !e.ok {
			out.Failed++
		}
	}
	if hostOpLastSet {
		out.LastSuccessAt = hostOpLastOK
	}
	return out
}

// resetHostOpCounters clears the counters (tests).
func resetHostOpCounters() {
	hostOpMu.Lock()
	hostOpEvents = nil
	hostOpLastOK = time.Time{}
	hostOpLastSet = false
	hostOpMu.Unlock()
}
