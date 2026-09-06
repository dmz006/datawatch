// v8.19.8 regression test: handleSessionCurrentStatus must return a
// parseable JSON body on the no-new-output and thin-delta paths.
//
// The previous implementation used http.Error(..., http.StatusNoContent)
// which, per RFC 7231 §3.3, forbids a body on 204 and caused the Go
// server to drop the message. The browser then called r.json() on an
// empty body and surfaced to the operator as:
//
//	"Failed to execute 'json' on 'Response': Unexpected end of JSON input"
//
// These tests pin the contract: status 200, JSON body with no_change:true.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// stubSummarizer implements SummarizerSvc without doing any real work.
type stubSummarizer struct{ short, long string }

func (s *stubSummarizer) Summarize(_ context.Context, _ string) (string, error) { return s.short, nil }
func (s *stubSummarizer) SummarizeDual(_ context.Context, _, _ string) (string, string, error) {
	return s.short, s.long, nil
}
func (s *stubSummarizer) ContextLines() int { return 0 }

// newCurrentStatusServer wires a Server with a Manager (one running session
// at a known log file path, offset at end of file) and a stub summarizer —
// the minimum state for handleSessionCurrentStatus to exercise its
// no-change and thin-delta paths.
func newCurrentStatusServer(t *testing.T, initialLog string, startOffset int64) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "output.log")
	if err := os.WriteFile(logPath, []byte(initialLog), 0600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	sm, err := session.NewManager("h", t.TempDir(), "/bin/true", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	sess := &session.Session{
		ID:              "ab01",
		FullID:          "h-ab01",
		LogFile:         logPath,
		State:           session.StateRunning,
		SummaryLogOffset: startOffset,
	}
	if err := sm.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	srv.SetSummarizerSvc(&stubSummarizer{short: "ok", long: "ok-long"})
	return srv, logPath
}

// TestCurrentStatus_NoNewOutput pins the primary bug fix: the response must
// be 200 with a parseable JSON body {no_change:true}, not 204 with an empty
// body (which the PWA renders as the "json" exception).
func TestCurrentStatus_NoNewOutput(t *testing.T) {
	initial := "only one line\n"
	srv, _ := newCurrentStatusServer(t, initial, int64(len(initial)))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/h-ab01/current-status", nil)
	rr := httptest.NewRecorder()
	srv.handleSessionCurrentStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (pre-v8.19.8 was 204); body=%q", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if body == "" {
		t.Fatalf("empty body — this is the v8.19.7 bug (204 has no body); want JSON {no_change:true}; got %q", rr.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v (this is what the PWA hit pre-v8.19.8); body=%q", err, rr.Body.String())
	}
	if got["no_change"] != true {
		t.Errorf("no_change = %v, want true", got["no_change"])
	}
	if _, ok := got["current_status"]; !ok {
		t.Errorf("current_status field missing — PWA contract requires it; got %v", got)
	}
}

// TestCurrentStatus_ThinDelta — the second branch of the merged condition
// (output exists but < 5 lines) must also return a parseable JSON body.
func TestCurrentStatus_ThinDelta(t *testing.T) {
	initial := line("a") + line("b") + line("c") // 3 lines, < 5-line threshold
	srv, _ := newCurrentStatusServer(t, initial, 0)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/h-ab01/current-status", nil)
	rr := httptest.NewRecorder()
	srv.handleSessionCurrentStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (pre-v8.19.8 was 204)", rr.Code)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("body not JSON: %v; body=%q", err, rr.Body.String())
	}
	if got["no_change"] != true {
		t.Errorf("no_change = %v, want true", got["no_change"])
	}
}

func line(s string) string { return s + "\n" }
