// BL328 — Async PRD decompose with SSE streaming tests.
//
// TS-645: POST /decompose returns 202 with task_id and stream_url
// TS-646: GET /decompose/stream yields story events then complete event
// TS-647: GET /decompose/status returns in_progress then complete
// TS-648: GET /decompose/stream with Last-Event-ID header re-emits from that point

package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// decomposeTestAPI is a minimal AutonomousAPI fake that implements
// DecomposeStreaming with configurable behavior.
type decomposeTestAPI struct {
	fakeOrchAutonomous
	mu           sync.Mutex
	stories      []map[string]any // stories to yield via cb
	decomposeErr error
	getPRDResult any
	getPRDFound  bool
}

func (a *decomposeTestAPI) DecomposeStreaming(prdID string, cb func(int, int, any)) (any, error) {
	a.mu.Lock()
	stories := a.stories
	err := a.decomposeErr
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if cb != nil {
		for i, s := range stories {
			cb(i, len(stories), s)
		}
	}
	return map[string]any{"id": prdID, "status": "needs_review", "stories": stories}, nil
}

func (a *decomposeTestAPI) GetPRD(id string) (any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.getPRDResult != nil {
		return a.getPRDResult, a.getPRDFound
	}
	return map[string]any{"id": id, "status": "needs_review"}, true
}

// newDecomposeTestServer returns a Server wired with the test API.
func newDecomposeTestServer(api *decomposeTestAPI) *Server {
	return &Server{autonomousMgr: api}
}

// TS-645: POST /decompose returns 202 with task_id and stream_url.
func TestTS645_POST_Decompose_Returns202(t *testing.T) {
	api := &decomposeTestAPI{
		stories: []map[string]any{
			{"id": "s1", "title": "Story One", "description": "desc1"},
			{"id": "s2", "title": "Story Two", "description": "desc2"},
		},
	}
	s := newDecomposeTestServer(api)

	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-test/decompose", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["task_id"] != "prd-test" {
		t.Errorf("task_id: want prd-test, got %v", resp["task_id"])
	}
	wantStreamURL := "/api/autonomous/prds/prd-test/decompose/stream"
	if resp["stream_url"] != wantStreamURL {
		t.Errorf("stream_url: want %q, got %v", wantStreamURL, resp["stream_url"])
	}
}

// TS-647: GET /decompose/status returns the job state.
func TestTS647_GET_Decompose_Status(t *testing.T) {
	api := &decomposeTestAPI{
		stories: []map[string]any{
			{"id": "s1", "title": "Story One", "description": "desc1"},
		},
	}
	s := newDecomposeTestServer(api)

	// Trigger the async job.
	postReq := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-status/decompose", nil)
	postRR := httptest.NewRecorder()
	s.handleAutonomousPRDs(postRR, postReq)
	if postRR.Code != http.StatusAccepted {
		t.Fatalf("POST: expected 202, got %d", postRR.Code)
	}

	// Wait for the goroutine to complete.
	var job *decomposeJob
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := s.decomposeJobs.Load("prd-status"); ok {
			job = v.(*decomposeJob)
			job.mu.RLock()
			st := job.status
			job.mu.RUnlock()
			if st == decomposeJobComplete || st == decomposeJobError {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job == nil {
		t.Fatal("job never stored in decomposeJobs")
	}

	// Poll the status endpoint.
	statusReq := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/prd-status/decompose/status", nil)
	statusRR := httptest.NewRecorder()
	s.handleDecomposeStatus(statusRR, statusReq, "prd-status")

	if statusRR.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d body=%s", statusRR.Code, statusRR.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(statusRR.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "complete" {
		t.Errorf("status: want complete, got %v", resp["status"])
	}
}

// TS-646: GET /decompose/stream yields story events then complete event.
func TestTS646_GET_Decompose_Stream_Events(t *testing.T) {
	api := &decomposeTestAPI{
		stories: []map[string]any{
			{"id": "s1", "title": "Story Alpha", "description": "alpha desc"},
			{"id": "s2", "title": "Story Beta", "description": "beta desc"},
		},
	}
	s := newDecomposeTestServer(api)

	// Start async job.
	postReq := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-stream/decompose", nil)
	postRR := httptest.NewRecorder()
	s.handleAutonomousPRDs(postRR, postReq)
	if postRR.Code != http.StatusAccepted {
		t.Fatalf("POST: expected 202, got %d", postRR.Code)
	}

	// Wait for job to complete before opening stream (simpler than live streaming in test).
	deadline := time.Now().Add(2 * time.Second)
	var job *decomposeJob
	for time.Now().Before(deadline) {
		if v, ok := s.decomposeJobs.Load("prd-stream"); ok {
			job = v.(*decomposeJob)
			job.mu.RLock()
			st := job.status
			job.mu.RUnlock()
			if st == decomposeJobComplete || st == decomposeJobError {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job == nil {
		t.Fatal("job never stored")
	}

	// Now open the SSE stream — job is complete so it should replay all
	// events and return.
	streamReq := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/prd-stream/decompose/stream", nil)
	streamRR := httptest.NewRecorder()

	// Use a test server to get proper streaming via ResponseRecorder.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleDecomposeStream(w, r, "prd-stream")
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("content-type: want text/event-stream, got %q", resp.Header.Get("Content-Type"))
	}

	// Collect event data lines.
	var dataLines []string
	scanner := bufio.NewScanner(resp.Body)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Log("stream timeout (may be ok if complete event closed)")
	}

	// Expect: story×2 + progress×2 + complete = at least 5 data lines.
	if len(dataLines) < 5 {
		t.Errorf("want ≥5 data events, got %d: %v", len(dataLines), dataLines)
	}

	// Check at least one story event present.
	storyFound := false
	completeFound := false
	for _, line := range dataLines {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev["type"] == "story" {
			storyFound = true
		}
		if ev["type"] == "complete" {
			completeFound = true
		}
	}
	if !storyFound {
		t.Errorf("no story event in stream output: %v", dataLines)
	}
	if !completeFound {
		t.Errorf("no complete event in stream output: %v", dataLines)
	}

	_ = streamRR // unused but kept for reference
	_ = streamReq
}

// TS-648: GET /decompose/stream with Last-Event-ID header re-emits from that point.
func TestTS648_GET_Decompose_Stream_LastEventID(t *testing.T) {
	api := &decomposeTestAPI{
		stories: []map[string]any{
			{"id": "s1", "title": "Story One", "description": "d1"},
			{"id": "s2", "title": "Story Two", "description": "d2"},
			{"id": "s3", "title": "Story Three", "description": "d3"},
		},
	}
	s := newDecomposeTestServer(api)

	// Start and wait for job completion.
	postReq := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-leid/decompose", nil)
	postRR := httptest.NewRecorder()
	s.handleAutonomousPRDs(postRR, postReq)
	if postRR.Code != http.StatusAccepted {
		t.Fatalf("POST: expected 202, got %d", postRR.Code)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := s.decomposeJobs.Load("prd-leid"); ok {
			j := v.(*decomposeJob)
			j.mu.RLock()
			st := j.status
			j.mu.RUnlock()
			if st == decomposeJobComplete || st == decomposeJobError {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify total event count first (no Last-Event-ID).
	v, _ := s.decomposeJobs.Load("prd-leid")
	job := v.(*decomposeJob)
	job.mu.RLock()
	totalEvents := len(job.events)
	job.mu.RUnlock()

	if totalEvents == 0 {
		t.Fatal("no events recorded in job")
	}

	// Open stream with Last-Event-ID = 2 → should only get events 3+.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleDecomposeStream(w, r, "prd-leid")
	}))
	defer ts.Close()

	req2, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req2.Header.Set("Last-Event-ID", "2")
	client := &http.Client{}
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("GET with Last-Event-ID: %v", err)
	}
	defer resp2.Body.Close() //nolint:errcheck

	var dataLines []string
	scanner := bufio.NewScanner(resp2.Body)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}

	// With Last-Event-ID=2 and totalEvents events, we expect totalEvents-2 data lines.
	expectedLines := totalEvents - 2
	if len(dataLines) != expectedLines {
		t.Errorf("with Last-Event-ID=2: want %d data lines (total=%d), got %d: %v",
			expectedLines, totalEvents, len(dataLines), dataLines)
	}
}
