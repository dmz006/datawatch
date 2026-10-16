package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// 2026-10-16 — root-caused live: a one-shot ACP task that reached
// session.idle without emitting DATAWATCH_COMPLETE had no human to send
// the next message and no mechanism to nudge itself forward, so it sat
// until opencode's own server died on its own. These tests cover the
// fix: streamEventsOnce sends a bounded number of continuation nudges on
// session.idle, then stops and lets the existing exhaustion path take
// over.

// sseIdleEvents builds an SSE body with n repeated session.idle events,
// one per "data:" line, matching the shape streamEventsOnce expects.
func sseIdleEvents(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(`data: {"type":"session.idle","properties":{}}` + "\n\n")
	}
	return b.String()
}

func TestStreamEventsOnce_NudgesForwardOnIdle(t *testing.T) {
	var messageCount atomic.Int32
	var lastBody strings.Builder
	var mu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseIdleEvents(2)) // fewer than maxIdleNudges — all should get a nudge
	})
	mux.HandleFunc("/session/sess1/message", func(w http.ResponseWriter, r *http.Request) {
		messageCount.Add(1)
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		mu.Lock()
		lastBody.Write(buf[:n])
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	st := &acpSessionState{baseURL: srv.URL, sessionID: "sess1"}
	reconnect := streamEventsOnce(context.Background(), srv.URL, t.TempDir()+"/out.log", "cs-test", st)

	if !reconnect {
		t.Errorf("expected reconnect=true (stream ended without session.completed), got false")
	}
	if got := messageCount.Load(); got != 2 {
		t.Errorf("expected 2 continuation nudges sent, got %d", got)
	}
	st.mu.Lock()
	if st.idleNudges != 2 {
		t.Errorf("idleNudges = %d, want 2", st.idleNudges)
	}
	st.mu.Unlock()
	mu.Lock()
	body := lastBody.String()
	mu.Unlock()
	if !strings.Contains(body, "DATAWATCH_COMPLETE") {
		t.Errorf("nudge body should reference DATAWATCH_COMPLETE, got: %s", body)
	}
}

func TestStreamEventsOnce_StopsNudgingAfterMax(t *testing.T) {
	var messageCount atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseIdleEvents(maxIdleNudges+5)) // well past the cap
	})
	mux.HandleFunc("/session/sess1/message", func(w http.ResponseWriter, r *http.Request) {
		messageCount.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	st := &acpSessionState{baseURL: srv.URL, sessionID: "sess1"}
	streamEventsOnce(context.Background(), srv.URL, t.TempDir()+"/out.log", "cs-test", st)

	if got := messageCount.Load(); got != int32(maxIdleNudges) {
		t.Errorf("expected exactly %d nudges (capped), got %d", maxIdleNudges, got)
	}
}

func TestStreamEventsOnce_NoNudgeAfterCompletion(t *testing.T) {
	var messageCount atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Completed first, then an idle event — must not nudge after completion.
		fmt.Fprint(w, `data: {"type":"session.completed","properties":{}}`+"\n\n")
		fmt.Fprint(w, sseIdleEvents(1))
	})
	mux.HandleFunc("/session/sess1/message", func(w http.ResponseWriter, r *http.Request) {
		messageCount.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	st := &acpSessionState{baseURL: srv.URL, sessionID: "sess1"}
	reconnect := streamEventsOnce(context.Background(), srv.URL, t.TempDir()+"/out.log", "cs-test", st)

	if reconnect {
		t.Errorf("expected reconnect=false after session.completed, got true")
	}
	if got := messageCount.Load(); got != 0 {
		t.Errorf("expected no nudge after completion, got %d message(s) sent", got)
	}
}
