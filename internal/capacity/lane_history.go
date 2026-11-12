// lane_history.go — the ledger's closed-interval history, the source of the
// lane pulse dashboard: every lease that ended (busy) and every queued request
// that left the queue (waiting), with start and end times. The ledger keeps
// only the live leases and waiters in its snapshot, so the history is recorded
// at the moment each interval closes and kept in a bounded ring.
package capacity

import "time"

// LaneIntervalBusy and LaneIntervalWaiting are the Kind values of a LaneInterval.
const (
	LaneIntervalBusy    = "busy"
	LaneIntervalWaiting = "waiting"
)

// laneHistoryMax bounds the history. Older intervals are dropped first.
const laneHistoryMax = 5000

// LaneInterval is one closed stretch of ledger time. For Kind "busy" it is a
// lease from acquire to release; for "waiting" it is a queued request from the
// moment it queued to the moment it was granted or withdrawn. Pools, Node and
// Model are copied from the lease or request.
type LaneInterval struct {
	Kind   string    `json:"kind"`
	Holder string    `json:"holder"`
	PRDID  string    `json:"prd_id"`
	Pools  []string  `json:"pools"`
	Node   string    `json:"node,omitempty"`
	Model  string    `json:"model,omitempty"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
}

// appendIntervalLocked records one closed interval. The caller holds l.mu.
func (l *Ledger) appendIntervalLocked(iv LaneInterval) {
	if !iv.End.After(iv.Start) {
		return
	}
	l.intervals = append(l.intervals, iv)
	if over := len(l.intervals) - laneHistoryMax; over > 0 {
		l.intervals = append([]LaneInterval(nil), l.intervals[over:]...)
	}
}

// recordLeaseEndLocked closes a lease into the history. The caller holds l.mu.
func (l *Ledger) recordLeaseEndLocked(ls *Lease, now time.Time) {
	l.appendIntervalLocked(LaneInterval{
		Kind: LaneIntervalBusy, Holder: ls.Holder, PRDID: ls.PRDID,
		Pools: append([]string(nil), ls.Pools...), Node: ls.Node, Model: ls.Model,
		Start: ls.Acquired, End: now,
	})
}

// recordWaitEndLocked closes a queued request into the history. The caller
// holds l.mu.
func (l *Ledger) recordWaitEndLocked(w *waiter, now time.Time) {
	l.appendIntervalLocked(LaneInterval{
		Kind: LaneIntervalWaiting, Holder: w.req.Holder, PRDID: w.req.PRDID,
		Pools: append([]string(nil), w.req.Pools...), Node: w.req.Node, Model: w.req.Model,
		Start: w.since, End: now,
	})
}

// LaneIntervals returns the closed intervals that end at or after since, oldest
// first, plus the time recording began. A reader must clip the intervals to its
// own window; recording began is when the ledger started keeping history, so a
// reader can tell a real idle stretch from time before the history existed.
func (l *Ledger) LaneIntervals(since time.Time) ([]LaneInterval, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LaneInterval, 0, len(l.intervals))
	for _, iv := range l.intervals {
		if !iv.End.Before(since) {
			out = append(out, iv)
		}
	}
	return out, l.recordingSince
}
