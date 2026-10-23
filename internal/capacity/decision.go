package capacity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DecisionKind is the outcome of a non-blocking admission query.
type DecisionKind string

const (
	// DecisionAdmit means the request would be granted now.
	DecisionAdmit DecisionKind = "admit"
	// DecisionQueued means the request would wait.
	DecisionQueued DecisionKind = "queued"
	// DecisionAlternateAvailable means the request would wait, but a pool of
	// the same kind elsewhere has room.
	DecisionAlternateAvailable DecisionKind = "alternate_available"
)

// Alternate is a pool, node and model elsewhere that could take the work.
type Alternate struct {
	Pool  string `json:"pool"`
	Node  string `json:"node,omitempty"`
	Model string `json:"model,omitempty"`
}

// Decision is the answer to Check.
type Decision struct {
	Kind          DecisionKind `json:"kind"`
	QueuePosition int          `json:"queue_position"` // 1-based; 0 when admitted
	Reason        string       `json:"reason,omitempty"`
	ResetsAt      *time.Time   `json:"resets_at,omitempty"`
	Alternates    []Alternate  `json:"alternates,omitempty"`
}

// Check is the Status-style non-blocking admission query: it reports whether
// req would be admitted now, where it would sit in the wait queue (using the
// same ordering dispatch grants in), and any pool elsewhere that could take
// it. It takes no lease, adds no waiter and records nothing (BL431).
func (l *Ledger) Check(req Request) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.opt.Clock.Now()
	reason := l.blockReasonLocked(req)

	hyp := &waiter{req: req, seq: l.seq + 1, since: now}
	list := append(append([]*waiter(nil), l.waiters...), hyp)
	pos := 0
	var ahead []*waiter
	for i, w := range l.orderListLocked(list, now) {
		if w == hyp {
			pos = i + 1
			break
		}
		ahead = append(ahead, w)
	}
	competing := 0
	for _, w := range ahead {
		if sharesPool(w.req.Pools, req.Pools) && l.blockReasonLocked(w.req) == "" {
			competing++
		}
	}
	if reason == "" && competing == 0 {
		return Decision{Kind: DecisionAdmit}
	}
	d := Decision{Kind: DecisionQueued, QueuePosition: pos, Reason: reason}
	if reason == "" {
		d.Reason = fmt.Sprintf("behind %d earlier request(s) for the same pools", competing)
	}
	var blocked []string
	for _, p := range req.Pools {
		if l.blockReasonLocked(Request{Pools: []string{p}, Interactive: req.Interactive}) != "" {
			blocked = append(blocked, p)
		}
		if l.rateBlockLocked(p, now) != "" {
			if at, ok := l.rateResetLocked(p, now); ok && (d.ResetsAt == nil || at.After(*d.ResetsAt)) {
				at := at
				d.ResetsAt = &at
			}
		}
	}
	if reason != "" {
		d.Alternates = l.alternatesLocked(req, blocked)
		if len(d.Alternates) > 0 {
			d.Kind = DecisionAlternateAvailable
		}
	}
	return d
}

func sharesPool(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// alternatesLocked lists pools of the same kind (the prefix before ':') as
// each blocked pool that have room and no block of their own.
func (l *Ledger) alternatesLocked(req Request, blocked []string) []Alternate {
	known := map[string]bool{}
	for p := range l.limits {
		known[p] = true
	}
	for p := range l.rates {
		known[p] = true
	}
	for _, ls := range l.leases {
		for _, p := range ls.Pools {
			known[p] = true
		}
		if ls.Node != "" {
			known["node:"+ls.Node] = true
		}
	}
	for _, w := range l.waiters {
		for _, p := range w.req.Pools {
			known[p] = true
		}
	}
	for n := range l.lastModel {
		known["node:"+n] = true
	}
	if req.Node != "" {
		known["node:"+req.Node] = true
	}
	own := map[string]bool{}
	for _, p := range req.Pools {
		own[p] = true
	}
	var out []Alternate
	seen := map[string]bool{}
	for _, b := range blocked {
		kind, _, ok := strings.Cut(b, ":")
		if !ok {
			continue
		}
		for c := range known {
			if own[c] || seen[c] || !strings.HasPrefix(c, kind+":") {
				continue
			}
			if l.blockReasonLocked(Request{Pools: []string{c}, Interactive: req.Interactive}) != "" {
				continue
			}
			seen[c] = true
			a := Alternate{Pool: c, Model: req.Model}
			if n, isNode := strings.CutPrefix(c, "node:"); isNode {
				a.Node = n
				if m := l.lastModel[n]; m != "" {
					a.Model = m
				}
			}
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pool < out[j].Pool })
	return out
}

// FormatDecision renders a Decision as one or two operator-readable lines for
// the CLI and comm surfaces.
func FormatDecision(d Decision) string {
	var b strings.Builder
	switch d.Kind {
	case DecisionAdmit:
		b.WriteString("would be admitted now")
		return b.String()
	default:
		fmt.Fprintf(&b, "would be queued at position %d", d.QueuePosition)
	}
	if d.Reason != "" {
		fmt.Fprintf(&b, ": %s", d.Reason)
	}
	if d.ResetsAt != nil {
		fmt.Fprintf(&b, "; resets at %s", d.ResetsAt.UTC().Format(time.RFC3339))
	}
	if len(d.Alternates) > 0 {
		var names []string
		for _, a := range d.Alternates {
			s := a.Pool
			if a.Model != "" {
				s += " (" + a.Model + ")"
			}
			names = append(names, s)
		}
		fmt.Fprintf(&b, "\nalternate available: %s", strings.Join(names, ", "))
	}
	return b.String()
}
