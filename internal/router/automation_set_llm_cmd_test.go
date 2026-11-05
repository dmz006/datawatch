package router

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// controlLog is a mutex-guarded record of the requests the fake daemon saw.
// The daemon's handler runs on its own goroutine, so every access goes
// through the lock. It also counts requests that have reached the handler but
// are not yet recorded, so reset can wait for them instead of relying on a
// fixed sleep to separate one input's window from the next.
type controlLog struct {
	mu       sync.Mutex
	cond     *sync.Cond
	calls    []controlCall
	inflight int
}

func newControlLog() *controlLog {
	l := &controlLog{}
	l.cond = sync.NewCond(&l.mu)
	return l
}

// begin marks a request as having reached the handler.
func (l *controlLog) begin() {
	l.mu.Lock()
	l.inflight++
	l.mu.Unlock()
}

// add records the request and marks it as no longer in flight.
func (l *controlLog) add(c controlCall) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, c)
	l.inflight--
	l.cond.Broadcast()
}

// reset waits until every request that reached the handler has been recorded
// (bounded, so a wedged handler cannot hang the test), then clears the log.
func (l *controlLog) reset() {
	deadline := time.AfterFunc(5*time.Second, func() {
		l.mu.Lock()
		l.inflight = 0
		l.cond.Broadcast()
		l.mu.Unlock()
	})
	defer deadline.Stop()
	l.mu.Lock()
	defer l.mu.Unlock()
	for l.inflight > 0 {
		l.cond.Wait()
	}
	l.calls = nil
}

// snapshot returns a copy of the recorded calls taken under the lock.
func (l *controlLog) snapshot() []controlCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]controlCall(nil), l.calls...)
}

// setLLMFixture serves a fake daemon that records each request, and returns
// a router wired to it.
func setLLMFixture(t *testing.T) (*Router, *controlLog) {
	t.Helper()
	calls := newControlLog()
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls.begin()
		raw, _ := io.ReadAll(r.Body)
		c := controlCall{method: r.Method, path: r.URL.Path}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &c.body); err != nil {
				t.Errorf("body %q is not JSON: %v", raw, err)
			}
		}
		calls.add(c)
		_, _ = w.Write([]byte(`{"id":"ab12","status":"running","version":9}`))
	})
	return setupRetryRouter(t, port), calls
}

func runSetLLMCases(t *testing.T, r *Router, calls *controlLog, cases []struct {
	name, in, path string
	want           map[string]any
}) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls.reset()
			resp := r.HandleTestMessage(tc.in)
			seen := calls.snapshot()
			if len(seen) != 1 {
				t.Fatalf("%q made %d calls, want 1 (reply %v)", tc.in, len(seen), resp)
			}
			got := seen[0]
			if got.method != http.MethodPost || got.path != tc.path {
				t.Errorf("request = %s %s, want POST %s", got.method, got.path, tc.path)
			}
			gotJSON, _ := json.Marshal(got.body)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("body = %s, want %s", gotJSON, wantJSON)
			}
			if len(resp) == 0 || !strings.Contains(resp[0], "automation ab12") || !strings.Contains(resp[0], "version 9") {
				t.Errorf("confirmation = %v", resp)
			}
		})
	}
}

func runSetLLMUsage(t *testing.T, r *Router, calls *controlLog, inputs []string, usagePrefix string) {
	t.Helper()
	for _, in := range inputs {
		calls.reset()
		resp := r.HandleTestMessage(in)
		if seen := calls.snapshot(); len(seen) != 0 {
			t.Errorf("%q reached the daemon: %+v", in, seen)
		}
		if len(resp) == 0 || !strings.HasPrefix(resp[0], usagePrefix) {
			t.Errorf("%q reply = %v, want the usage line %q", in, resp, usagePrefix)
		}
	}
}

func TestAutomationCommSetLLM(t *testing.T) {
	r, calls := setLLMFixture(t)
	const path = "/api/automations/ab12/set_llm"

	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"all keys", "automation set-llm ab12 backend=pi model=qwen3.8:27b effort=high planning-backend=claude-code planning-model=opus", path,
			map[string]any{"actor": "comm", "backend": "pi", "model": "qwen3.8:27b", "effort": "high",
				"planning_backend": "claude-code", "planning_model": "opus"}},
		{"one key", "automation set-llm ab12 model=sonnet", path,
			map[string]any{"actor": "comm", "model": "sonnet"}},
		{"underscore spelling", "automation set-llm ab12 planning_backend=pi planning_model=qwen3.8:27b", path,
			map[string]any{"actor": "comm", "planning_backend": "pi", "planning_model": "qwen3.8:27b"}},
		{"empty value clears", "automation set-llm ab12 model= planning-model=", path,
			map[string]any{"actor": "comm", "model": "", "planning_model": ""}},
	})

	t.Run("bad input replies with usage", func(t *testing.T) {
		runSetLLMUsage(t, r, calls, []string{
			"automation set-llm", "automation set-llm ab12",
			"automation set-llm ab12 model", "automation set-llm ab12 bogus=x",
			"automation set-llm ab12 model=x extra",
		}, "usage: automation set-llm")
	})
}

func TestAutomationCommSetVerificationLLM(t *testing.T) {
	r, calls := setLLMFixture(t)
	const path = "/api/automations/ab12/set_verification_llm"

	runSetLLMCases(t, r, calls, []struct {
		name, in, path string
		want           map[string]any
	}{
		{"all keys", "automation set-verification-llm ab12 backend=claude-code model=haiku effort=low", path,
			map[string]any{"actor": "comm", "backend": "claude-code", "model": "haiku", "effort": "low"}},
		{"empty value clears", "automation set-verification-llm ab12 model=", path,
			map[string]any{"actor": "comm", "model": ""}},
	})

	t.Run("bad input replies with usage", func(t *testing.T) {
		runSetLLMUsage(t, r, calls, []string{
			"automation set-verification-llm", "automation set-verification-llm ab12",
			"automation set-verification-llm ab12 model", "automation set-verification-llm ab12 bogus=x",
		}, "usage: automation set-verification-llm")
	})

	t.Run("planning keys are refused", func(t *testing.T) {
		runSetLLMUsage(t, r, calls, []string{
			"automation set-verification-llm ab12 planning-backend=pi",
			"automation set-verification-llm ab12 model=x planning-model=y",
		}, "usage: automation set-verification-llm")
	})
}

// TestPlanningRequeueReplyLine checks the compact comm-channel line produced
// from a set-llm response's top-level `planning` outcome: empty when the key
// is absent or malformed, a human-readable old→new line when the pool moved,
// and the defer / unchanged variants. It also surfaces the reason when present.
func TestPlanningRequeueReplyLine(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resp     string
		exact    string
		contains []string
	}{{
		name:  "no planning key",
		resp:  `{"id":"ab12","status":"active","version":3}`,
		exact: "",
	}, {
		name:  "malformed",
		resp:  `{not json`,
		exact: "",
	}, {
		name:  "unchanged",
		resp:  `{"id":"ab12","planning":{"outcome":"unchanged","reason":"no queued planning run"}}`,
		exact: "planning pool unchanged: no queued planning run",
	}, {
		name:     "requeued shows old and new pool",
		resp:     `{"id":"ab12","planning":{"outcome":"requeued","old_backend":"ollama","old_model":"","new_backend":"pi","new_model":"opus","reason":"pool has idle capacity"}}`,
		contains: []string{"requeued", "ollama", "pi", "opus", "pool has idle capacity"},
	}, {
		name:  "deferred",
		resp:  `{"id":"ab12","planning":{"outcome":"deferred_to_next_run","reason":"planning run already started"}}`,
		exact: "planning deferred to next run: planning run already started",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := planningRequeueReplyLine(tc.resp)
			if tc.exact != "" {
				if got != tc.exact {
					t.Fatalf("got %q, want %q", got, tc.exact)
				}
				return
			}
			for _, sub := range tc.contains {
				if !strings.Contains(got, sub) {
					t.Fatalf("got %q does not contain %q", got, sub)
				}
			}
		})
	}
}
