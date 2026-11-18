// Coverage for the caller-side `Describe` deadline in the image-send path,
// using a fake vision backend that hangs past the timeout (and ignores its
// context):
//
//  1. expandImageTags returns the '@<path>' fallback within bounded wall
//     time when the backend hangs (describe_timeout_seconds caps the wait,
//     the hanging goroutine is dropped, its late result is never used).
//  2. a fast backend still yields the '[image: desc | path: ...]' form.
//  3. a backend error also falls back to '@<path>'.
//  4. handler-level: an image send whose Describe hangs runs off the
//     read loop (connection input worker), so later messages on the same
//     WS connection are processed while the first is still waiting on the
//     slow backend, and once it resolves the inputs reach the sessions in
//     the order they were sent (a same-session follow-up never overtakes
//     the still-expanding image send).
//
// All backends are fakes; no real tmux, network, or vision service.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/gorilla/websocket"
)

// ctxIgnoringSlowVisioner blocks past any caller deadline without honoring
// the context, then returns a description — the classic stuck backend. The
// caller-side deadline, not this sleep, must bound the observed wait.
type ctxIgnoringSlowVisioner struct {
	wait time.Duration
	desc string
}

func (v *ctxIgnoringSlowVisioner) Describe(_ context.Context, _ []byte, _, _ string) (string, error) {
	time.Sleep(v.wait)
	return v.desc, nil
}

// expandImageTags — Describe hangs 3s while the caller deadline is 1s.
// The tag must resolve to the bare '@<path>' form in well under the
// hang: the deadline bounds the caller, the late result is discarded.
func TestDescribeTimeout_HangingBackend_BoundedWallTime(t *testing.T) {
	root := t.TempDir()
	p := writeTempImage(t, root, []byte("fake-image-data"))
	v := &ctxIgnoringSlowVisioner{wait: 3 * time.Second, desc: "stale description"}
	s := expandImageTagsServer(t, root, v)
	s.cfg.Vision.DescribeTimeoutSeconds = 1

	start := time.Now()
	got := s.expandImageTags("look at [image:" + p + "] and fix it")
	elapsed := time.Since(start)

	if want := "look at @" + p + " and fix it"; got != want {
		t.Errorf("hanging backend:\ngot  %q\nwant %q (stale description must not leak in)", got, want)
	}
	if elapsed > 2*time.Second {
		t.Errorf("caller-side deadline not enforced: expandImageTags blocked %s against a 1s deadline while the backend slept 3s", elapsed)
	}
}

// expandImageTags — a fast, healthy backend yields the description form
// with the path preserved, so the session sees both the description and
// the file reference.
func TestDescribeTimeout_FastBackend_DescriptionPreserved(t *testing.T) {
	root := t.TempDir()
	p := writeTempImage(t, root, []byte("fake-image-data"))
	s := expandImageTagsServer(t, root, &fakeVisioner{result: "a red door on a white wall"})

	in := "this is [image:" + p + "] right?"
	want := "this is [image: a red door on a white wall | path: " + p + "] right?"
	if got := s.expandImageTags(in); got != want {
		t.Errorf("fast backend:\ngot  %q\nwant %q", got, want)
	}
}

// expandImageTags — a backend error (describe fails outright) also falls
// back to the bare '@<path>' form rather than leaving the tag in place.
func TestDescribeTimeout_BackendError_FallsBackToAtPath(t *testing.T) {
	root := t.TempDir()
	p := writeTempImage(t, root, []byte("fake-image-data"))
	s := expandImageTagsServer(t, root, &fakeVisioner{err: errors.New("backend exploded")})

	// The lone tag ends at the end of the input, so the bare @-ref gets a trailing space.
	if got, want := s.expandImageTags("[image:"+p+"]"), "@"+p+" "; got != want {
		t.Errorf("backend error:\ngot  %q\nwant %q", got, want)
	}
}

// Handler-level: the WebSocket image-send path must not let a hanging
// vision backend stall the connection. Three messages on one connection:
//
//  1. `send aaa: [image:...] describe for me` — offloaded to the
//     connection's input worker; Describe hangs 1.5s past the 1s
//     deadline, so A receives the '@<path>' fallback at ~1s.
//  2. `send bbb: a plain follow-up` — a different session; it must be
//     delivered while message 1 is still waiting on the slow backend
//     (recorded before A's send-keys), proving the read loop never
//     blocked on the describe.
//  3. `send aaa: a later same-session note` — same session as 1; the
//     read loop holds it until 1 has resolved, so A receives it after its
//     image send: order per session is preserved.
//
// The hanging backend sleeps 1.5s > the 1s deadline; the whole exchange
// completes in ~1.1s, well under the 5s test budget.
func TestDescribeTimeout_WSConnection_NotStalledByHangingBackend(t *testing.T) {
	root := t.TempDir()
	p := writeTempImage(t, root, []byte("fake-image-data"))

	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	fakeT := mgr.WithFakeTmux()
	t.Cleanup(mgr.WaitBackground)

	// Two input-capable (Running) sessions: A gets the slow image send
	// plus its follow-up, B gets the plain send that must not wait on A's
	// backend.
	mgr.SaveSession(&session.Session{ID: "aaa", FullID: "testhost-aaa", State: session.StateRunning, TmuxSession: "fake-a", UpdatedAt: time.Now()}) //nolint:errcheck
	mgr.SaveSession(&session.Session{ID: "bbb", FullID: "testhost-bbb", State: session.StateRunning, TmuxSession: "fake-b", UpdatedAt: time.Now()}) //nolint:errcheck

	s := &Server{
		manager:  mgr,
		hostname: "testhost",
		visioner: &ctxIgnoringSlowVisioner{wait: 1500 * time.Millisecond, desc: "stale description"},
		hub:      NewHub(),
		cfg: &config.Config{
			Vision:  config.VisionConfig{DescribeTimeoutSeconds: 1},
			Session: config.SessionConfig{FileServiceRoot: root},
		},
	}
	go s.hub.Run()
	srv := httptest.NewServer(http.HandlerFunc(s.handleWS))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+mustHost(t, srv.URL)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	send := func(text string) {
		t.Helper()
		data, err := json.Marshal(struct {
			Text string `json:"text"`
		}{Text: text})
		if err != nil {
			t.Fatal(err)
		}
		frame, err := json.Marshal(WSMessage{Type: MsgCommand, Data: data, Timestamp: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			t.Fatalf("write %q: %v", text, err)
		}
	}

	start := time.Now()
	send("send aaa: [image:" + p + "] describe for me") // 1: hangs on the slow backend
	send("send bbb: a plain follow-up")                 // 2: must not wait on 1
	send("send aaa: a later same-session note")         // 3: must land on A after 1

	// Wait for all three inputs to reach the fake tmux (bounded: 1s
	// deadline + slack, far under the 5s budget).
	deadline := time.Now().Add(4 * time.Second)
	var sends []session.FakeTmuxCall
	for {
		sends = nil
		for _, c := range fakeT.CallsSnapshot() {
			if c.Op == "send-keys" {
				sends = append(sends, c)
			}
		}
		if len(sends) >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := time.Since(start)

	wantOrder := []struct {
		tmux  string
		input string
	}{
		{"fake-b", "a plain follow-up"},          // 2: delivered while 1 was still waiting
		{"fake-a", "@" + p + " describe for me"}, // 1: timed-out describe → '@<path>' fallback
		{"fake-a", "a later same-session note"},  // 3: released only after 1 resolved
	}
	if len(sends) != len(wantOrder) {
		t.Fatalf("expected %d send-keys calls, got %d: %+v", len(wantOrder), len(sends), fakeT.CallsSnapshot())
	}
	for i, want := range wantOrder {
		got := sends[i]
		if got.Session != want.tmux {
			t.Errorf("send #%d: tmux session %q, want %q (full call log: %+v)", i+1, got.Session, want.tmux, fakeT.CallsSnapshot())
			continue
		}
		if len(got.Args) == 0 || got.Args[0] != want.input {
			t.Errorf("send #%d: input %v, want %q", i+1, got.Args, want.input)
		}
	}
	if elapsed > 3*time.Second {
		t.Errorf("exchange took %s; the connection should not stall on the hanging backend (1s deadline, 1.5s hang)", elapsed)
	}
}
