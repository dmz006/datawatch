package mcp

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// sseTestListener is a minimal net.Listener for asserting bind retries
// without touching real sockets.
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

// sseFakeClock advances only when sleep is called (injected now/sleep pair).
type sseFakeClock struct {
	t   time.Time
	log []time.Duration
}

func (c *sseFakeClock) now() time.Time { return c.t }
func (c *sseFakeClock) sleep(_ context.Context, d time.Duration) error {
	c.t = c.t.Add(d)
	c.log = append(c.log, d)
	return nil
}

func TestSSEListenerStateDefaultDisabled(t *testing.T) {
	s := &Server{}
	if got := s.SSEListenerState(); got != "disabled" {
		t.Fatalf("SSEListenerState() = %q, want disabled", got)
	}
}

func TestListenWithRetryRecoversAfterTransients(t *testing.T) {
	clock := &sseFakeClock{t: time.Unix(1_000_000, 0)}
	failsLeft := map[string]int{"127.0.0.1:9091": 2} // fails twice, then binds
	listened := map[string]*sseTestListener{}

	s := &Server{}
	listeners, err := s.listenWithRetry(context.Background(),
		[]string{"127.0.0.1:9090", "127.0.0.1:9091"}, 120*time.Second,
		&listenRetryOptions{
			listen: func(_, address string) (net.Listener, error) {
				if failsLeft[address] > 0 {
					failsLeft[address]--
					return nil, errors.New("address already in use")
				}
				l := &sseTestListener{addr: address}
				listened[address] = l
				return l, nil
			},
			sleep: clock.sleep,
			now:   clock.now,
		})
	if err != nil {
		t.Fatalf("listenWithRetry: %v", err)
	}
	if len(listeners) != 2 {
		t.Fatalf("got %d listeners, want 2", len(listeners))
	}
	// Order must match the request order.
	want := []string{"127.0.0.1:9090", "127.0.0.1:9091"}
	for i, ln := range listeners {
		if ln.Addr().String() != want[i] {
			t.Fatalf("listener[%d] addr = %q, want %q", i, ln.Addr().String(), want[i])
		}
	}
	// Backoff delays: 1s then 2s, each applied once.
	wantDelays := []time.Duration{time.Second, 2 * time.Second}
	if len(clock.log) != len(wantDelays) {
		t.Fatalf("sleep log = %v, want %v", clock.log, wantDelays)
	}
	for i, d := range wantDelays {
		if clock.log[i] != d {
			t.Fatalf("sleep[%d] = %v, want %v", i, clock.log[i], d)
		}
	}
	if got := s.SSEListenerState(); got != "listening" {
		t.Fatalf("SSEListenerState() = %q, want listening", got)
	}
}

func TestListenWithRetryGivesUpAtTimeout(t *testing.T) {
	clock := &sseFakeClock{t: time.Unix(1_000_000, 0)}
	both := map[string]*sseTestListener{}

	s := &Server{}
	_, err := s.listenWithRetry(context.Background(),
		[]string{"127.0.0.1:9090", "127.0.0.1:9091"}, 10*time.Second,
		&listenRetryOptions{
			listen: func(_, address string) (net.Listener, error) {
				if address == "127.0.0.1:9090" {
					l := &sseTestListener{addr: address}
					both[address] = l
					return l, nil
				}
				return nil, errors.New("address already in use")
			},
			sleep: clock.sleep,
			now:   clock.now,
		})
	if err == nil {
		t.Fatal("expected an error when the retry window is exhausted")
	}
	if !errors.Is(err, err) || len(err.Error()) == 0 {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1s + 2s + 4s + 8s sleeps, giving up once cumulative >= 10s.
	wantDelays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if len(clock.log) != len(wantDelays) {
		t.Fatalf("sleep log = %v, want %v", clock.log, wantDelays)
	}
	for i, d := range wantDelays {
		if clock.log[i] != d {
			t.Fatalf("sleep[%d] = %v, want %v", i, clock.log[i], d)
		}
	}
	if got := s.SSEListenerState(); got != "failed" {
		t.Fatalf("SSEListenerState() = %q, want failed", got)
	}
	// The listener that did bind must be closed on the give-up path.
	good := both["127.0.0.1:9090"]
	if good == nil || !good.closed {
		t.Fatal("bound listener was not closed on give-up")
	}
}

func TestListenWithRetryDelayCappedAt15s(t *testing.T) {
	clock := &sseFakeClock{t: time.Unix(1_000_000, 0)}

	s := &Server{}
	_, err := s.listenWithRetry(context.Background(),
		[]string{"127.0.0.1:9091"}, 24*time.Second,
		&listenRetryOptions{
			listen: func(_, _ string) (net.Listener, error) {
				return nil, errors.New("address already in use")
			},
			sleep: clock.sleep,
			now:   clock.now,
		})
	if err == nil {
		t.Fatal("expected an error when the retry window is exhausted")
	}
	// 1s, 2s, 4s, 8s, then capped at 15s (not 16s).
	wantDelays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}
	if len(clock.log) != len(wantDelays) {
		t.Fatalf("sleep log = %v, want %v", clock.log, wantDelays)
	}
	for i, d := range wantDelays {
		if clock.log[i] != d {
			t.Fatalf("sleep[%d] = %v, want %v", i, clock.log[i], d)
		}
	}
}

func TestListenWithRetryCancelledContextStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slept := 0

	s := &Server{}
	_, err := s.listenWithRetry(ctx,
		[]string{"127.0.0.1:9091"}, 120*time.Second,
		&listenRetryOptions{
			listen: func(_, _ string) (net.Listener, error) {
				return nil, errors.New("address already in use")
			},
			sleep: func(c context.Context, _ time.Duration) error {
				slept++
				return c.Err()
			},
			now: time.Now,
		})
	if err == nil {
		t.Fatal("expected an error when ctx is cancelled")
	}
	if slept != 1 {
		t.Fatalf("slept %d times, want 1 (must stop after ctx cancel)", slept)
	}
	if got := s.SSEListenerState(); got != "failed" {
		t.Fatalf("SSEListenerState() = %q, want failed", got)
	}
}
