// v5.27.0 — verify the per-session channel ring buffer the PWA Channel
// tab seeds itself from on session-detail open. Pre-v5.27.0 the PWA
// only saw channel messages that arrived *after* the operator opened
// the tab, so a long-running channel looked empty even when
// datawatch-app was showing full activity.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecordChannelHistoryRingBuffer(t *testing.T) {
	s := &Server{channelHist: map[string][]channelHistEntry{}}

	// Push beyond the cap and confirm the oldest fall off.
	for i := 0; i < channelHistoryMax+25; i++ {
		s.recordChannelHistory("sess1", "msg", "incoming")
	}
	if got := len(s.channelHist["sess1"]); got != channelHistoryMax {
		t.Errorf("ring buffer cap: got %d, want %d", got, channelHistoryMax)
	}

	// Empty session_id is dropped silently — channel handlers can't
	// always supply one.
	before := len(s.channelHist[""])
	s.recordChannelHistory("", "ignored", "incoming")
	if len(s.channelHist[""]) != before {
		t.Error("recordChannelHistory accepted empty session_id")
	}
}

func TestHandleChannelHistory(t *testing.T) {
	s := &Server{channelHist: map[string][]channelHistEntry{}}
	s.recordChannelHistory("sessA", "first", "incoming")
	s.recordChannelHistory("sessA", "second", "outgoing")
	s.recordChannelHistory("sessB", "other", "incoming")

	// Missing session_id → 400.
	req := httptest.NewRequest(http.MethodGet, "/api/channel/history", nil)
	rr := httptest.NewRecorder()
	s.handleChannelHistory(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("missing session_id: got %d, want 400", rr.Code)
	}

	// POST → 405.
	req = httptest.NewRequest(http.MethodPost, "/api/channel/history?session_id=sessA", strings.NewReader(""))
	rr = httptest.NewRecorder()
	s.handleChannelHistory(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: got %d, want 405", rr.Code)
	}

	// Happy path — only sessA messages, ordered as recorded.
	req = httptest.NewRequest(http.MethodGet, "/api/channel/history?session_id=sessA", nil)
	rr = httptest.NewRecorder()
	s.handleChannelHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("happy path: got %d, want 200", rr.Code)
	}
	var resp struct {
		SessionID string `json:"session_id"`
		Messages  []struct {
			Text      string `json:"text"`
			Direction string `json:"direction"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID != "sessA" {
		t.Errorf("session_id = %q", resp.SessionID)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("messages: got %d, want 2", len(resp.Messages))
	}
	if resp.Messages[0].Text != "first" || resp.Messages[0].Direction != "incoming" {
		t.Errorf("msg[0] = %+v", resp.Messages[0])
	}
	if resp.Messages[1].Text != "second" || resp.Messages[1].Direction != "outgoing" {
		t.Errorf("msg[1] = %+v", resp.Messages[1])
	}

	// Unknown session_id → 200 with empty list (not 404, so the PWA
	// can fetch unconditionally without surfacing a spurious error).
	req = httptest.NewRequest(http.MethodGet, "/api/channel/history?session_id=nope", nil)
	rr = httptest.NewRecorder()
	s.handleChannelHistory(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("unknown session: got %d, want 200", rr.Code)
	}
	var empty struct {
		Messages []channelHistEntry `json:"messages"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&empty); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Errorf("unknown session messages: got %d, want 0", len(empty.Messages))
	}
}
