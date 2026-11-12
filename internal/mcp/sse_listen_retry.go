// SSE listener bind-retry helper (BL444: daemon should keep starting even
// when a TCP bind fails transiently, e.g. a previous instance is still
// releasing its port).
package mcp

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"
)

const sseListenDefaultTimeout = 120 * time.Second

// sseState* are the values stored in Server.sseListenerState (atomic).
const (
	sseListenerDisabled  int32 = 0
	sseListenerRetrying  int32 = 1
	sseListenerListening int32 = 2
	sseListenerFailed    int32 = 3
)

// SSEListenerState reports the thread-safe state of the MCP SSE listener:
// "disabled" (ServeSSE never attempted), "retrying" (a bind is being
// retried), "listening" (all addresses bound and serving), or "failed"
// (retry window exhausted without a successful bind).
func (s *Server) SSEListenerState() string {
	switch s.sseListenerState.Load() {
	case sseListenerRetrying:
		return "retrying"
	case sseListenerListening:
		return "listening"
	case sseListenerFailed:
		return "failed"
	default:
		return "disabled"
	}
}

func (s *Server) setSSEListenerState(v int32) { s.sseListenerState.Store(v) }

// debugLog emits a diagnostic line; kept tiny so retry loops stay readable.
func debugLog(format string, args ...interface{}) { log.Printf(format, args...) }

// defaultSSEListenSleep is the ctx-aware production sleep: waits d on the
// real clock or returns ctx.Err immediately if ctx is cancelled.
func defaultSSEListenSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// listenRetryOptions holds injectable dependencies for listenWithRetry
// (nil fields get production defaults).
type listenRetryOptions struct {
	// listen performs the actual TCP bind (default net.Listen).
	listen func(network, address string) (net.Listener, error)
	// sleep waits between retry attempts, cancellable via ctx
	// (default: timer on the real clock, returns ctx.Err on cancel).
	sleep func(ctx context.Context, d time.Duration) error
	// now is the clock used to measure cumulative elapsed time
	// (default time.Now).
	now func() time.Time
}

func (o *listenRetryOptions) applyDefaults() {
	if o.listen == nil {
		o.listen = net.Listen
	}
	if o.sleep == nil {
		o.sleep = defaultSSEListenSleep
	}
	if o.now == nil {
		o.now = time.Now
	}
}

// listenWithRetry binds every address in addrs. It first tries each
// address exactly once, logging any bind failure ONCE at info level, then
// retries only the still-failing addresses with exponential backoff
// (1s, 2s, 4s, 8s, ... capped at 15s between attempts) until every address
// is bound or the cumulative elapsed time (measured via the injected clock,
// not wall time) reaches timeout. On giving up it closes any listeners that
// did bind and returns the last bind error. Successful listeners are
// returned in the same order as addrs.
func (s *Server) listenWithRetry(ctx context.Context, addrs []string, timeout time.Duration, opts *listenRetryOptions) ([]net.Listener, error) {
	if opts == nil {
		opts = &listenRetryOptions{}
	}
	opts.applyDefaults()
	if timeout <= 0 {
		timeout = sseListenDefaultTimeout
	}

	s.setSSEListenerState(sseListenerRetrying)
	start := opts.now()
	bound := make(map[string]net.Listener, len(addrs))
	var remaining []string
	var lastErr error

	// First pass: attempt every address once.
	for _, addr := range addrs {
		ln, err := opts.listen("tcp", addr)
		if err != nil {
			log.Printf("datawatch MCP SSE: bind %s failed: %v (will retry for up to %s)", addr, err, timeout)
			lastErr = fmt.Errorf("MCP SSE listen %s: %w", addr, err)
			remaining = append(remaining, addr)
			continue
		}
		bound[addr] = ln
	}

	delay := time.Second
	for len(remaining) > 0 {
		elapsed := opts.now().Sub(start)
		if elapsed >= timeout {
			break
		}
		debugLog("datawatch MCP SSE: %d address(es) still unbound after %s, retrying in %s", len(remaining), elapsed, delay)
		if err := opts.sleep(ctx, delay); err != nil {
			log.Printf("datawatch MCP SSE: bind retry interrupted (ctx done): %v", err)
			break
		}
		left := remaining[:0]
		for _, addr := range remaining {
			ln, err := opts.listen("tcp", addr)
			if err != nil {
				lastErr = fmt.Errorf("MCP SSE listen %s: %w", addr, err)
				left = append(left, addr)
				continue
			}
			bound[addr] = ln
		}
		remaining = left
		delay = delay * 2
		if delay > 15*time.Second {
			delay = 15 * time.Second
		}
	}

	if len(remaining) > 0 {
		for _, ln := range bound {
			_ = ln.Close()
		}
		log.Printf("datawatch MCP SSE: gave up after %s; last bind error: %v", opts.now().Sub(start), lastErr)
		s.setSSEListenerState(sseListenerFailed)
		return nil, lastErr
	}

	log.Printf("datawatch MCP SSE: all %d address(es) bound after %s", len(bound), opts.now().Sub(start))
	s.setSSEListenerState(sseListenerListening)

	listeners := make([]net.Listener, 0, len(addrs))
	for _, addr := range addrs {
		if ln, ok := bound[addr]; ok {
			listeners = append(listeners, ln)
		}
	}
	return listeners, nil
}
