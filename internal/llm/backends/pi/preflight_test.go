package pi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

type fakeDoer struct {
	status int
	err    error
	req    *http.Request
	body   string
	block  bool
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.req = req
	b, _ := io.ReadAll(req.Body)
	f.body = string(b)
	if f.block {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader("x"))}, nil
}

func TestPreflightOK(t *testing.T) {
	f := &fakeDoer{status: 200}
	if err := Preflight(context.Background(), f, "http://node:1/v1/", "m1"); err != nil {
		t.Fatal(err)
	}
	if f.req.URL.String() != "http://node:1/v1/chat/completions" {
		t.Fatalf("url %s", f.req.URL)
	}
	if !strings.Contains(f.body, `"max_tokens":1`) || !strings.Contains(f.body, `"m1"`) {
		t.Fatalf("body %s", f.body)
	}
	if _, ok := f.req.Context().Deadline(); !ok {
		t.Fatal("no deadline")
	}
}

func TestPreflightFailures(t *testing.T) {
	cases := map[string]*fakeDoer{
		"error":  {err: errors.New("boom")},
		"non2xx": {status: 503},
	}
	for name, f := range cases {
		err := Preflight(context.Background(), f, "http://node:1/v1", "m1")
		if err == nil || !strings.Contains(err.Error(), "http://node:1/v1") || !strings.Contains(err.Error(), "m1") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestPreflightTimeout(t *testing.T) {
	f := &fakeDoer{block: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Preflight(ctx, f, "http://node:1/v1", "m1")
	if err == nil || !strings.Contains(err.Error(), "m1") {
		t.Fatalf("%v", err)
	}
}

// TestPreflightTwentySecondLimit checks the probe's own deadline is the
// twenty second bound, applied through the request context.
func TestPreflightTwentySecondLimit(t *testing.T) {
	if preflightTimeout != 20*time.Second {
		t.Fatalf("preflightTimeout = %v, want 20s", preflightTimeout)
	}
	f := &fakeDoer{status: 200}
	before := time.Now()
	if err := Preflight(context.Background(), f, "http://node:1/v1", "m1"); err != nil {
		t.Fatal(err)
	}
	dl, ok := f.req.Context().Deadline()
	if !ok {
		t.Fatal("request has no deadline")
	}
	if got := dl.Sub(before); got > 21*time.Second || got < 19*time.Second {
		t.Fatalf("deadline is %v after start, want about 20s", got)
	}
}

// TestPreflightParentDeadlineWins checks a shorter caller deadline cuts the
// probe short and the error still names the node and model.
func TestPreflightParentDeadlineWins(t *testing.T) {
	f := &fakeDoer{block: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Preflight(ctx, f, "http://node:1/v1", "m1")
	if time.Since(start) > 5*time.Second {
		t.Fatalf("probe ran %v despite a 30ms caller deadline", time.Since(start))
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "http://node:1/v1") || !strings.Contains(err.Error(), "m1") {
		t.Fatalf("err = %v, want node url and model", err)
	}
}

// TestPreflightStatusSnippetInError checks a non-2xx answer carries the
// status code and the start of the body.
func TestPreflightStatusSnippetInError(t *testing.T) {
	f := &fakeDoer{status: 502}
	err := Preflight(context.Background(), f, "http://node:1/v1", "m1")
	if err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("err = %v, want status 502", err)
	}
}

// TestPreflightMarksAndClearsLedger drives the launch gate with a failing
// then a succeeding fake client against a real ledger: the failure marks the
// node unhealthy and placement skips it, and the next good probe clears the
// mark so placement can use the node again.
func TestPreflightMarksAndClearsLedger(t *testing.T) {
	led := capacity.New(capacity.Options{PollInterval: time.Minute})
	led.SetLimit("node:a", 1)
	led.SetLimit("node:b", 1)
	addr := map[string]string{"node-b:11434": "b"}
	SetLaunchGate(led, func(a string) string { return addr[a] }, func(string) bool { return true })
	t.Cleanup(func() {
		SetLaunchGate(nil, nil, nil)
		setPreflightClient(&fakeDoer{status: 200})
	})

	acquire := func(holder string) string {
		t.Helper()
		req := capacity.Request{
			Holder: holder, PRDID: "prd-1", Pools: []string{"host"}, Model: "m1",
			NodeChoices: []capacity.NodeChoice{{Name: "a", Priority: 1}, {Name: "b", Priority: 5}},
		}
		if err := led.Acquire(context.Background(), req, 0, nil, nil); err != nil {
			t.Fatalf("%s: Acquire: %v", holder, err)
		}
		ls := led.LeaseOf(holder)
		if ls == nil {
			t.Fatalf("%s: no lease", holder)
		}
		t.Cleanup(func() { led.Release(holder) })
		return ls.Node
	}

	setPreflightClient(&fakeDoer{err: errors.New("connection refused")})
	err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/m1")
	if !errors.Is(err, ErrLaunchDeferred) {
		t.Fatalf("err = %v, want ErrLaunchDeferred", err)
	}
	bad, why := led.NodeUnhealthy("b")
	if !bad || !strings.Contains(why, "connection refused") {
		t.Fatalf("NodeUnhealthy(b) = %v %q, want marked with the probe error", bad, why)
	}
	if got := acquire("t1"); got != "a" {
		t.Fatalf("lease node = %q, want a while b is marked", got)
	}

	setPreflightClient(&fakeDoer{status: 200})
	if err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/m1"); err != nil {
		t.Fatal(err)
	}
	if bad, _ := led.NodeUnhealthy("b"); bad {
		t.Fatal("mark still set after a succeeding probe")
	}
	if got := acquire("t2"); got != "b" {
		t.Fatalf("lease node = %q, want b after the mark cleared", got)
	}
}
