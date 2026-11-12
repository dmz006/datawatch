// Deterministic tests for the SSE listen-retry helper (BL444). All external
// dependencies are injected: a listen stub and a fake clock (now + sleep),
// so nothing in this file calls time.Sleep or waits on real wall-clock time.
// Real binds, where used at all, go through net.Listen on 127.0.0.1 port 0
// so no fixed port is ever assumed to be free.
package mcp

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// sseTestListener is a minimal in-memory net.Listener for asserting
// listener bookkeeping without any real socket.
type sseTestListener struct {
	addr   string
	closed bool
}

func (l *sseTestListener) Accept() (net.Conn, error) { return nil, errors.New("not implemented") }
func (l *sseTestListener) Close() error {
	l.closed = true
	return nil
}
func (l *sseTestListener) Addr() net.Addr { return sseTestAddr(l.addr) }

type sseTestAddr string

func (a sseTestAddr) Network() string { return "tcp" }
func (a sseTestAddr) String() string  { return string(a) }

// sseRealListener wraps a real listener (always on 127.0.0.1 port 0 in
// these tests) with a close flag so give-up paths can be asserted.
type sseRealListener struct {
	net.Listener
	closed bool
}

func (l *sseRealListener) Close() error { l.closed = true; return l.Listener.Close() }

// sseFakeClock advances only when the injected sleep callback is invoked,
// so test time is fully manual and deterministic.
type sseFakeClock struct {
	t      time.Time
	delays []time.Duration
}

func (c *sseFakeClock) now() time.Time { return c.t }
func (c *sseFakeClock) sleep(_ context.Context, d time.Duration) error {
	c.delays = append(c.delays, d)
	c.t = c.t.Add(d)
	return nil
}

func (c *sseFakeClock) total() time.Duration {
	var sum time.Duration
	for _, d := range c.delays {
		sum += d
	}
	return sum
}

// newTestServer returns a standalone Server; the retry helper only touches
// the sseListenerState atomic, so no other fields are needed.
func newSSERetryTestServer() *Server { return &Server{} }

// realisticBindErr mimics what net.Listen returns for a busy port.
func realisticBindErr() error {
	return &net.OpError{Op: "listen", Net: "tcp", Err: errors.New("address already in use")}
}

func TestSSEListenerStateDefaultDisabled(t *testing.T) {
	if got := newSSERetryTestServer().SSEListenerState(); got != "disabled" {
		t.Fatalf("SSEListenerState() = %q, want disabled", got)
	}
}

func TestSSEListenRetryImmediateSuccess(t *testing.T) {
	t.Run("stub listener", func(t *testing.T) {
		clock := &sseFakeClock{t: time.Unix(1_000_000, 0)}
		s := newSSERetryTestServer()

		listeners, err := s.listenWithRetry(context.Background(),
			[]string{"127.0.0.1:12345"}, 0,
			&listenRetryOptions{
				listen: func(_, address string) (net.Listener, error) {
					return &sseTestListener{addr: address}, nil
				},
				sleep: clock.sleep,
				now:   clock.now,
			})
		if err != nil {
			t.Fatalf("listenWithRetry: %v", err)
		}
		if len(listeners) != 1 || listeners[0].Addr().String() != "127.0.0.1:12345" {
			t.Fatalf("listeners = %v, want one on 127.0.0.1:12345", listeners)
		}
		if len(clock.delays) != 0 {
			t.Fatalf("recorded backoff sleeps %v, want none (immediate success)", clock.delays)
		}
		if got := s.SSEListenerState(); got != "listening" {
			t.Fatalf("SSEListenerState() = %q, want listening", got)
		}
	})

	t.Run("real listener on port 0", func(t *testing.T) {
		clock := &sseFakeClock{t: time.Unix(1_000_000, 0)}
		s := newSSERetryTestServer()

		// One real bind, port 0 so nothing is assumed about fixed ports.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("real net.Listen: %v", err)
		}
		defer func() { _ = ln.Close() }()

		listeners, err := s.listenWithRetry(context.Background(),
			[]string{"127.0.0.1:12345"}, 120*time.Second,
			&listenRetryOptions{
				listen: func(_, _ string) (net.Listener, error) { return ln, nil },
				sleep:  clock.sleep,
				now:    clock.now,
			})
		if err != nil {
			t.Fatalf("listenWithRetry: %v", err)
		}
		if len(listeners) != 1 {
			t.Fatalf("got %d listeners, want 1", len(listeners))
		}
		if len(clock.delays) != 0 {
			t.Fatalf("recorded backoff sleeps %v, want none", clock.delays)
		}
		if got := s.SSEListenerState(); got != "listening" {
			t.Fatalf("SSEListenerState() = %q, want listening", got)
		}
	})
}

func TestSSEListenRetrySuccessAfterRetries(t *testing.T) {
	clock := &sseFakeClock{t: time.Unix(1_000_000, 0)}
	s := newSSERetryTestServer()

	// Event log proves ordering: the first failed bind must be surfaced
	// (attempt logged) before any backoff sleep happens.
	var events []string
	failsLeft := 2 // fail twice, then bind for real on 127.0.0.1:0
	realBound := &sseRealListener{}

	listeners, err := s.listenWithRetry(context.Background(),
		[]string{"127.0.0.1:12345"}, 120*time.Second,
		&listenRetryOptions{
			listen: func(_, _ string) (net.Listener, error) {
				if failsLeft > 0 {
					failsLeft--
					events = append(events, "listen-fail")
					return nil, realisticBindErr()
				}
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("real net.Listen: %v", err)
				}
				realBound.Listener = ln
				t.Cleanup(func() { _ = ln.Close() }) // owned by the test, closed at test end
				events = append(events, "listen-ok")
				return realBound, nil
			},
			sleep: func(ctx context.Context, d time.Duration) error {
				events = append(events, "sleep")
				return clock.sleep(ctx, d)
			},
			now: clock.now,
		})
	if err != nil {
		t.Fatalf("listenWithRetry: %v", err)
	}
	if len(listeners) != 1 {
		t.Fatalf("got %d listeners, want 1", len(listeners))
	}

	// Exact ordering: the first failed bind surfaces before any backoff
	// sleep, sleeps separate the attempts, success comes last.
	wantEvents := []string{"listen-fail", "sleep", "listen-fail", "sleep", "listen-ok"}
	if len(events) != len(wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
	for i, want := range wantEvents {
		if events[i] != want {
			t.Fatalf("events[%d] = %q, want %q (full: %v)", i, events[i], want, events)
		}
	}

	// Delays: exponential from the injected 1s base — 1s then 2s.
	wantDelays := []time.Duration{time.Second, 2 * time.Second}
	if len(clock.delays) != len(wantDelays) {
		t.Fatalf("recorded delays %v, want %v", clock.delays, wantDelays)
	}
	for i, want := range wantDelays {
		if clock.delays[i] != want {
			t.Fatalf("delay[%d] = %v, want %v", i, clock.delays[i], want)
		}
	}
	if got := s.SSEListenerState(); got != "listening" {
		t.Fatalf("SSEListenerState() = %q, want listening", got)
	}
}

func TestSSEListenRetryGivesUpAfterTotal(t *testing.T) {
	clock := &sseFakeClock{t: time.Unix(1_000_000, 0)}
	s := newSSERetryTestServer()

	bindErr := realisticBindErr()
	_, err := s.listenWithRetry(context.Background(),
		[]string{"127.0.0.1:12345"}, 0, // timeout 0 => default total window (120s)
		&listenRetryOptions{
			listen: func(_, _ string) (net.Listener, error) { return nil, bindErr },
			sleep:  clock.sleep, // advances the fake clock
			now:    clock.now,
		})

	if err == nil {
		t.Fatal("expected a non-nil error after the retry window is exhausted")
	}
	if !errors.Is(err, bindErr) {
		t.Fatalf("error %v does not wrap the last bind error %v", err, bindErr)
	}
	if got := s.SSEListenerState(); got != "failed" {
		t.Fatalf("SSEListenerState() = %q, want failed", got)
	}

	// Delays: 1s, 2s, 4s, 8s, then capped at 15s for the rest of the
	// window. With a 120s window the fake clock reaches exactly 120s
	// after 1+2+4+8+8x15 - 15 = 120; see the slack comment below.
	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		15 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second,
		15 * time.Second, 15 * time.Second, 15 * time.Second,
	}
	if len(clock.delays) != len(want) {
		t.Fatalf("recorded delays %v, want %v", clock.delays, want)
	}
	for i, d := range want {
		if clock.delays[i] != d {
			t.Fatalf("delay[%d] = %v, want %v (full log: %v)", i, clock.delays[i], d, clock.delays)
		}
	}
	// Total recorded backoff must track the 120s window; allow slack so a
	// slightly different cap-rounding boundary still passes.
	if total := clock.total(); total < 110*time.Second || total > 135*time.Second {
		t.Fatalf("total backoff %v not close to the 120s window", total)
	}
}

func TestSSEListenRetryCancelledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel up front: the injected sleep honors ctx.Done and reports
	// ctx.Err without waiting, so the helper must return promptly.
	cancel()

	clock := &sseFakeClock{t: time.Unix(1_000_000, 0)}
	slept := 0
	s := newSSERetryTestServer()

	_, err := s.listenWithRetry(ctx,
		[]string{"127.0.0.1:12345"}, 120*time.Second,
		&listenRetryOptions{
			listen: func(_, _ string) (net.Listener, error) { return nil, realisticBindErr() },
			sleep: func(c context.Context, d time.Duration) error {
				slept++
				if err := c.Err(); err != nil {
					return err // ctx-aware sleep: no real waiting at all
				}
				return clock.sleep(c, d)
			},
			now: clock.now,
		})
	if err == nil {
		t.Fatal("expected an error when ctx is cancelled during backoff")
	}
	if slept != 1 {
		t.Fatalf("slept %d times, want exactly 1 (must stop after ctx cancel)", slept)
	}
	if got := s.SSEListenerState(); got != "failed" {
		t.Fatalf("SSEListenerState() = %q, want failed", got)
	}
}
