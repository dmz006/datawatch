// Per-Automaton comm fan-out with quiet-window coalescing.
//
// WHY: matching lifecycle events can arrive in bursts (a PRD failing
// through several guardrail/executor steps within seconds). Sending one
// message per event floods the comm channel; the daemon's existing
// remote-alert bundler already coalesces on a quiet window, and this
// type is that same idea extracted into the autonomous package —
// keyed per Automaton (PRD) instead of globally — so it is unit-testable
// without a live router or the internal/router / internal/server
// packages (which this package must not import, or the import cycle
// through internal/server would reappear).
//
// Coalescing rule: events for the same key accumulate; each new event
// resets that key's timer; when the timer fires with no further events,
// exactly one send() delivers all accumulated lines under one header.
// Events for different PRDIDs (or one PRDID and the "global" bucket of
// non-PRD events) never mix into a single message.

package autonomous

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// commFanoutGlobalBucket is the bucket key for lifecycle events that
// carry no PRDID — they still get coalesced among themselves, just
// never alongside a specific Automaton's lines.
const commFanoutGlobalBucket = "global"

// commFanoutDefaultWindow is used when the configured window is <= 0:
// long enough that a typical burst of correlated events lands in one
// message, short enough that a genuinely single event is not held up.
const commFanoutDefaultWindow = 5 * time.Second

// CommFanoutSink accumulates matched lifecycle events PER AUTOMATON
// (keyed by ev.PRDID; events without a PRDID go to a "global" bucket)
// and, after the window with no further events, sends ONE message with
// all accumulated lines. Same quiet-window coalescing the daemon's
// existing remote-alert bundler uses, extracted here so it is
// unit-testable without a live router.
type CommFanoutSink struct {
	send   func(text string)
	window time.Duration // default 5s when <= 0
	mu     sync.Mutex
	lines  map[string][]string
	timers map[string]*time.Timer
	closed bool
}

// NewCommFanoutSink builds a comm fan-out sink that delivers coalesced
// messages via send (injected by the caller — never a live router).
// window <= 0 falls back to commFanoutDefaultWindow, so a zero-value
// SinkConfig gets sane coalescing rather than firing on every event.
func NewCommFanoutSink(send func(text string), window time.Duration) *CommFanoutSink {
	if window <= 0 {
		window = commFanoutDefaultWindow
	}
	return &CommFanoutSink{
		send:   send,
		window: window,
		lines:  make(map[string][]string),
		timers: make(map[string]*time.Timer),
	}
}

// Handle is one matched lifecycle event (the installer calls it via
// the subscribe callback). Append DescribeLifecycleEvent(ev); reset the
// per-Automaton timer. On fire: exactly one send() call whose text is
// all accumulated lines joined by newlines under a single header line.
//
// Concurrency: every mutable state transition (append, timer reset,
// bucket hand-off) happens under c.mu, so interleaved events for
// different Automata can never lose or reorder lines. The send itself
// runs OUTSIDE the lock so a slow/panicking transport cannot wedge
// other buckets or deadlock a concurrent Handle.
func (c *CommFanoutSink) Handle(ev LifecycleEvent) {
	key := ev.PRDID
	if key == "" {
		key = commFanoutGlobalBucket
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.lines[key] = append(c.lines[key], DescribeLifecycleEvent(ev))
	if t := c.timers[key]; t != nil {
		t.Stop() // reset: this event extends the quiet window
	}
	c.timers[key] = time.NewTimer(c.window)
	c.mu.Unlock()

	go c.flushBucket(key)
}

// flushBucket delivers bucket key once it has been quiet for the
// window. It is safe to race with Handle: the (bucket -> lines) pair is
// handed off atomically under c.mu, and a later event simply starts a
// fresh timer for the same key.
func (c *CommFanoutSink) flushBucket(key string) {
	c.mu.Lock()
	var lines []string
	if l, ok := c.lines[key]; ok {
		lines = l
		delete(c.lines, key)
	}
	if t := c.timers[key]; t != nil {
		t.Stop()
		delete(c.timers, key)
	}
	closed := c.closed
	c.mu.Unlock()

	if closed || lines == nil {
		return
	}
	c.deliver(lines)
}

// deliver sends one coalesced message for lines. Recovering here keeps
// a panicking injected transport from killing the daemon: a broken
// send hook should lose that one message, not the process.
func (c *CommFanoutSink) deliver(lines []string) {
	if c.send == nil {
		return
	}
	defer func() { recover() }()
	c.send(commFanoutMessage(lines))
}

// commFanoutMessage renders the coalesced message: a single header line
// naming the event count, then every accumulated event line, joined by
// newlines. The shape is fixed so downstream comm clients get a stable
// format to parse/dedupe on.
func commFanoutMessage(lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lifecycle updates (%d)", len(lines))
	for _, line := range lines {
		b.WriteByte('\n')
		b.WriteString(line)
	}
	return b.String()
}

// Close stops coalescing: pending timers are stopped and every
// accumulated bucket is flushed immediately as one message each, so no
// matched event is dropped on shutdown and no timer goroutine outlives
// the sink. Idempotent and panic-free (a panicking send hook is
// recovered in deliver), so the installer's cancel can call it blindly.
func (c *CommFanoutSink) Close() {
	c.mu.Lock()
	lines := c.lines
	c.lines = make(map[string][]string)
	for _, t := range c.timers {
		t.Stop()
	}
	c.timers = make(map[string]*time.Timer)
	c.closed = true
	c.mu.Unlock()

	for _, l := range lines {
		// Same deliver() (with recover) as the timer path, so a broken
		// send hook cannot turn shutdown into a crash.
		c.deliver(l)
	}
}
