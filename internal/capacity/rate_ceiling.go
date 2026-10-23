package capacity

import (
	"fmt"
	"sort"
	"time"
)

// RateCeiling caps a pool's usage inside a rolling window, on top of its
// concurrency limit. A zero Requests or Tokens leaves that dimension unset;
// a ceiling with no dimension or a non-positive Window is no ceiling.
type RateCeiling struct {
	Requests int           // lease grants per window; 0 = unset
	Tokens   int64         // tokens recorded per window; 0 = unset
	Window   time.Duration // rolling window length
}

func (c RateCeiling) active() bool {
	return c.Window > 0 && (c.Requests > 0 || c.Tokens > 0)
}

// rateEntry is one recorded use, stamped with the ledger clock. Like the
// budget ledger's entries, a rolling window counts those newer than now-window
// and the oldest counted entry decides when usage next drops.
type rateEntry struct {
	At  time.Time
	Req int
	Tok int64
}

// SetRate sets a pool's rate ceiling and re-dispatches. The zero value (or any
// inactive ceiling) removes it and forgets the recorded usage.
func (l *Ledger) SetRate(pool string, c RateCeiling) {
	l.mu.Lock()
	if !c.active() {
		delete(l.rates, pool)
		delete(l.rateUse, pool)
	} else {
		l.rates[pool] = c
	}
	l.dispatchLocked()
	l.mu.Unlock()
}

// RateOf returns a pool's rate ceiling (zero value = none).
func (l *Ledger) RateOf(pool string) RateCeiling {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rates[pool]
}

// RecordTokens records n tokens used against the pool's rolling window at the
// ledger clock time and re-dispatches. It is a no-op for a pool without a
// rate ceiling.
func (l *Ledger) RecordTokens(pool string, n int64) {
	if n <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.rates[pool]; !ok {
		return
	}
	l.rateUse[pool] = append(l.rateUse[pool], rateEntry{At: l.opt.Clock.Now(), Tok: n})
	l.dispatchLocked()
}

// recordRequestsLocked records one request against every ceilinged pool in
// pools that carries a requests ceiling.
func (l *Ledger) recordRequestsLocked(pools []string, now time.Time) {
	for _, p := range pools {
		if c, ok := l.rates[p]; ok && c.Requests > 0 {
			l.rateUse[p] = append(l.rateUse[p], rateEntry{At: now, Req: 1})
		}
	}
}

// rateWindowLocked returns the entries still inside the window at now, oldest
// first, with their request and token totals. Expired entries are dropped.
func (l *Ledger) rateWindowLocked(pool string, now time.Time) (es []rateEntry, reqs int, toks int64) {
	c, ok := l.rates[pool]
	if !ok {
		return nil, 0, 0
	}
	cut := now.Add(-c.Window)
	for _, e := range l.rateUse[pool] {
		if !e.At.After(cut) {
			continue
		}
		es = append(es, e)
		reqs += e.Req
		toks += e.Tok
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].At.Before(es[j].At) })
	l.rateUse[pool] = es
	return es, reqs, toks
}

// rateFreeAt finds when the dimension (selected by val) next drops below its
// ceiling: the time the oldest entry that must leave does so.
func rateFreeAt(es []rateEntry, total, ceiling int64, window time.Duration, val func(rateEntry) int64) (time.Time, bool) {
	remaining := total
	for _, e := range es {
		v := val(e)
		if v <= 0 {
			continue
		}
		remaining -= v
		if remaining < ceiling {
			return e.At.Add(window), true
		}
	}
	return time.Time{}, false
}

func reqOf(e rateEntry) int64 { return int64(e.Req) }
func tokOf(e rateEntry) int64 { return e.Tok }

// rateBlockLocked returns the reason a pool's rate ceiling blocks now, or "".
func (l *Ledger) rateBlockLocked(pool string, now time.Time) string {
	c, ok := l.rates[pool]
	if !ok {
		return ""
	}
	es, reqs, toks := l.rateWindowLocked(pool, now)
	type dim struct {
		name       string
		used, ceil int64
		val        func(rateEntry) int64
	}
	dims := []dim{
		{"requests", int64(reqs), int64(c.Requests), reqOf},
		{"tokens", toks, c.Tokens, tokOf},
	}
	for _, d := range dims {
		if d.ceil <= 0 || d.used < d.ceil {
			continue
		}
		msg := fmt.Sprintf("pool %s rate ceiling: %s %d/%d in %s", pool, d.name, d.used, d.ceil, c.Window)
		if at, ok := rateFreeAt(es, d.used, d.ceil, c.Window, d.val); ok {
			msg += fmt.Sprintf(", resets at %s", at.UTC().Format(time.RFC3339))
		}
		return msg
	}
	return ""
}

// rateResetLocked reports when usage in the pool's window next drops: when a
// dimension is at its ceiling, the time every full dimension is free again;
// otherwise when the oldest counted entry leaves the window.
func (l *Ledger) rateResetLocked(pool string, now time.Time) (time.Time, bool) {
	c, ok := l.rates[pool]
	if !ok {
		return time.Time{}, false
	}
	es, reqs, toks := l.rateWindowLocked(pool, now)
	var at time.Time
	found := false
	pick := func(t time.Time) {
		if !found || t.After(at) {
			at, found = t, true
		}
	}
	if c.Requests > 0 && reqs >= c.Requests {
		if t, ok := rateFreeAt(es, int64(reqs), int64(c.Requests), c.Window, reqOf); ok {
			pick(t)
		}
	}
	if c.Tokens > 0 && toks >= c.Tokens {
		if t, ok := rateFreeAt(es, toks, c.Tokens, c.Window, tokOf); ok {
			pick(t)
		}
	}
	if found {
		return at, true
	}
	if len(es) > 0 {
		return es[0].At.Add(c.Window), true
	}
	return time.Time{}, false
}

// RateResetAt reports the time the pool's rate window next frees usage; false
// when the pool has no ceiling or nothing is counted.
func (l *Ledger) RateResetAt(pool string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rateResetLocked(pool, l.opt.Clock.Now())
}

// rateWakeLocked returns how long until a rate ceiling blocking one of the
// waiter's pools frees, so Acquire can wake at the reset instead of waiting
// out a full poll interval. False when no pool is rate-blocked.
func (l *Ledger) rateWakeLocked(w *waiter, now time.Time) (time.Duration, bool) {
	var best time.Duration
	found := false
	for _, p := range w.req.Pools {
		if l.rateBlockLocked(p, now) == "" {
			continue
		}
		if at, ok := l.rateResetLocked(p, now); ok {
			if d := at.Sub(now); !found || d < best {
				best, found = d, true
			}
		}
	}
	return best, found
}
