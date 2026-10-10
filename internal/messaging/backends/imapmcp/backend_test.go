package imapmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

func TestBackendName(t *testing.T) {
	b := New("http://localhost:8765", "", "", "")
	if b.Name() != "imap_mcp" {
		t.Fatalf("Name() = %q, want %q", b.Name(), "imap_mcp")
	}
}

func TestSend(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/messages/send") {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		writeJSON(w, map[string]string{"status": "sent"})
	}))
	defer srv.Close()

	b := New(srv.URL, "personal", "test", "")
	if err := b.Send("user@example.com", "hello world"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if gotBody["to"] != "user@example.com" {
		t.Errorf("to = %q, want user@example.com", gotBody["to"])
	}
	if gotBody["body"] != "hello world" {
		t.Errorf("body = %q, want hello world", gotBody["body"])
	}
	if !strings.HasPrefix(gotBody["subject"], "test") {
		t.Errorf("subject = %q, want prefix 'test'", gotBody["subject"])
	}
}

// TestSend_ConfiguredToOverridesRouterGroupID is the regression test for
// the bounce incident: the generic comm Router always calls
// Send(r.groupID, text), and for this backend r.groupID is just the
// literal label "imap_mcp" (not a real address) -- confirmed live,
// 2026-10-10, as the cause of repeated SMTP 5.1.1 bounces. When
// imap_mcp.to is configured, it must win regardless of what recipient
// Send is called with.
func TestSend_ConfiguredToOverridesRouterGroupID(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, map[string]string{"status": "sent"})
	}))
	defer srv.Close()

	b := New(srv.URL, "", "test", "operator@example.com")
	// "imap_mcp" here stands in for the literal groupID label the
	// Router actually passes -- the whole point is that it must be
	// ignored in favor of the configured address.
	if err := b.Send("imap_mcp", "status update"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if gotBody["to"] != "operator@example.com" {
		t.Errorf("to = %q, want operator@example.com (configured To must override the passed recipient)", gotBody["to"])
	}
}

// TestSend_EmptyToFallsBackToPassedRecipient confirms the pre-fix
// behavior is preserved when imap_mcp.to is left unset, rather than the
// fix silently dropping messages or hard-failing on a missing config.
func TestSend_EmptyToFallsBackToPassedRecipient(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, map[string]string{"status": "sent"})
	}))
	defer srv.Close()

	b := New(srv.URL, "", "test", "")
	if err := b.Send("whatever-was-passed", "hi"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if gotBody["to"] != "whatever-was-passed" {
		t.Errorf("to = %q, want whatever-was-passed (no To configured -- fall back to the passed recipient)", gotBody["to"])
	}
}

func TestSendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "smtp error", http.StatusBadGateway)
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	err := b.Send("u@e.com", "hi")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error %q should mention 502", err.Error())
	}
}

func TestSubscribeInboundCommand(t *testing.T) {
	// Build a fake SSE event that mirrors what imap-mcp would send.
	cmd := verifiedCommand{Account: "personal", From: "ops@example.com"}
	cmd.Command.Verb = "status"
	cmd.Command.Args = ""
	cmd.Command.Nonce = "abc123"

	payload, _ := json.Marshal(cmd)
	event := sseEvent{Type: "inbound.command", Account: "personal", Payload: payload}
	eventJSON, _ := json.Marshal(event)
	sseBody := fmt.Sprintf("data: %s\n\n", eventJSON)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody) //nolint:errcheck
		// Close immediately to let stream() return.
	}))
	defer srv.Close()

	b := New(srv.URL, "personal", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got []messaging.Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Subscribe(ctx, func(m messaging.Message) { //nolint:errcheck
			got = append(got, m)
			cancel() // got one message, done
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Subscribe did not return within 5s")
	}

	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	m := got[0]
	if m.Sender != "ops@example.com" {
		t.Errorf("Sender = %q, want ops@example.com", m.Sender)
	}
	if m.Text != "status" {
		t.Errorf("Text = %q, want 'status'", m.Text)
	}
	if m.ID != "abc123" {
		t.Errorf("ID = %q, want abc123", m.ID)
	}
	if m.Backend != "imap_mcp" {
		t.Errorf("Backend = %q, want imap_mcp", m.Backend)
	}
}

func TestSubscribeIgnoresNonCommand(t *testing.T) {
	// Only inbound.command should be dispatched; message.synced should be dropped.
	event := sseEvent{Type: "message.synced", Account: "personal"}
	eventJSON, _ := json.Marshal(event)
	sseBody := fmt.Sprintf("data: %s\n\n", eventJSON)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody) //nolint:errcheck
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var count int
	b.Subscribe(ctx, func(_ messaging.Message) { count++ }) //nolint:errcheck
	if count != 0 {
		t.Errorf("got %d messages from non-command event, want 0", count)
	}
}

func TestHandleSSELine(t *testing.T) {
	b := New("http://localhost:8765", "", "datawatch", "")

	t.Run("valid inbound.command", func(t *testing.T) {
		cmd := verifiedCommand{Account: "acct", From: "a@b.com"}
		cmd.Command.Verb = "deploy"
		cmd.Command.Args = `{"env":"prod"}`
		cmd.Command.Nonce = "n1"
		payload, _ := json.Marshal(cmd)
		e := sseEvent{Type: "inbound.command", Payload: payload}
		line, _ := json.Marshal(e)

		var got messaging.Message
		b.handleSSELine(string(line), func(m messaging.Message) { got = m })
		if got.Sender != "a@b.com" {
			t.Errorf("sender = %q", got.Sender)
		}
		if !strings.HasPrefix(got.Text, "deploy") {
			t.Errorf("text = %q, want deploy prefix", got.Text)
		}
	})

	t.Run("inbound.rejected is ignored", func(t *testing.T) {
		e := sseEvent{Type: "inbound.rejected"}
		line, _ := json.Marshal(e)
		var called bool
		b.handleSSELine(string(line), func(_ messaging.Message) { called = true })
		if called {
			t.Error("handler should not be called for inbound.rejected")
		}
	})

	t.Run("malformed json is ignored", func(t *testing.T) {
		var called bool
		b.handleSSELine("{not json", func(_ messaging.Message) { called = true })
		if called {
			t.Error("handler should not be called on malformed JSON")
		}
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// GH#203 — imap-mcp >= 0.5.3 requires a bearer token on every request
// except GET /api/health. These tests cover: the header is sent on all
// three calls, and 401/403 are handled (clear error, no hot-loop).

func TestSend_SendsAuthHeader(t *testing.T) {
	t.Cleanup(func() { SetToken("") })
	SetToken("tok-abc123")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, map[string]string{"status": "sent"})
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	if err := b.Send("u@e.com", "hi"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if gotAuth != "Bearer tok-abc123" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok-abc123")
	}
}

func TestSelfID_SendsAuthHeader(t *testing.T) {
	t.Cleanup(func() { SetToken("") })
	SetToken("tok-xyz")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, []map[string]any{{"name": "personal", "default": true}})
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	if got := b.SelfID(); got == "" {
		t.Fatal("SelfID() returned empty, expected a resolved address")
	}
	if gotAuth != "Bearer tok-xyz" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok-xyz")
	}
}

func TestStream_SendsAuthHeader(t *testing.T) {
	t.Cleanup(func() { SetToken("") })
	SetToken("tok-stream")

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = b.stream(ctx, func(messaging.Message) {})

	if gotAuth != "Bearer tok-stream" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok-stream")
	}
}

func TestSend_NoTokenConfigured_NoAuthHeader(t *testing.T) {
	t.Cleanup(func() { SetToken("") })
	SetToken("") // explicit: imap-mcp <= 0.5.2 / auth disabled

	var gotAuth string
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, sawHeader = r.Header.Get("Authorization"), r.Header.Get("Authorization") != ""
		writeJSON(w, map[string]string{"status": "sent"})
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	if err := b.Send("u@e.com", "hi"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if sawHeader {
		t.Errorf("Authorization header sent (%q) when no token is configured", gotAuth)
	}
}

func TestSend_401IsReportedAsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="imap-mcp"`)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized: missing token"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	err := b.Send("u@e.com", "hi")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var authErr *authError
	if !errors.As(err, &authErr) {
		t.Fatalf("error %v is not an *authError", err)
	}
	if authErr.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", authErr.status)
	}
	if !strings.Contains(err.Error(), "missing token") {
		t.Errorf("error %q should surface imap-mcp's own message", err.Error())
	}
}

func TestStream_403ReturnsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden: token lacks scope read"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	err := b.stream(context.Background(), func(messaging.Message) {})
	var authErr *authError
	if !errors.As(err, &authErr) {
		t.Fatalf("stream() error %v is not an *authError", err)
	}
	if authErr.status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", authErr.status)
	}
	if !strings.Contains(err.Error(), "lacks scope read") {
		t.Errorf("error %q should surface imap-mcp's own message", err.Error())
	}
}

func TestSubscribe_AuthErrorDoesNotHotLoop(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	b := New(srv.URL, "", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	b.Subscribe(ctx, func(messaging.Message) {}) //nolint:errcheck

	// maxBackoff is 60s, so within a 500ms window a correct implementation
	// connects exactly once and then waits -- a hot loop (no backoff, or
	// starting the normal 2s exponential ramp) would reconnect multiple
	// times well within 500ms.
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("server hit %d times in 500ms after a 401 — expected exactly 1 (should back off at maxBackoff, not hot-loop)", n)
	}
}

func TestNextBackoff(t *testing.T) {
	cases := []struct{ cur, max, want time.Duration }{
		{2 * time.Second, 60 * time.Second, 4 * time.Second},
		{32 * time.Second, 60 * time.Second, 60 * time.Second},
		{60 * time.Second, 60 * time.Second, 60 * time.Second},
	}
	for _, c := range cases {
		if got := nextBackoff(c.cur, c.max); got != c.want {
			t.Errorf("nextBackoff(%v, %v) = %v, want %v", c.cur, c.max, got, c.want)
		}
	}
}
