package capacity

import "sync"

// unhealthyMarks holds operator- or daemon-set unhealthy marks, one per node
// name, with the reason given. It has its own mutex so it is safe to read from
// inside the ledger's locked gate without touching Ledger.mu.
type unhealthyMarks struct {
	mu    sync.Mutex
	marks map[string]string
}

func (u *unhealthyMarks) set(node, reason string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.marks == nil {
		u.marks = map[string]string{}
	}
	u.marks[node] = reason
}

func (u *unhealthyMarks) clear(node string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.marks, node)
}

func (u *unhealthyMarks) get(node string) (bool, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	reason, ok := u.marks[node]
	return ok, reason
}

// MarkNodeUnhealthy marks node unhealthy with a reason. Placement skips a
// marked node until ClearNodeUnhealthy is called. Marking again replaces the
// reason. It is safe for concurrent use.
func (l *Ledger) MarkNodeUnhealthy(node, reason string) {
	if node == "" {
		return
	}
	l.unhealthy.set(node, reason)
	l.mu.Lock()
	l.signalChangedLocked()
	l.mu.Unlock()
}

// ClearNodeUnhealthy removes the unhealthy mark from node, if any, and wakes
// waiters so the next poll can place work on it.
func (l *Ledger) ClearNodeUnhealthy(node string) {
	l.unhealthy.clear(node)
	l.mu.Lock()
	l.signalChangedLocked()
	l.mu.Unlock()
}

// NodeUnhealthy reports whether node carries an unhealthy mark and the reason
// it was given.
func (l *Ledger) NodeUnhealthy(node string) (bool, string) {
	return l.unhealthy.get(node)
}
