// BL328 — Async PRD decompose with SSE streaming.
//
// Endpoints (all bearer-authenticated):
//
//	POST /api/autonomous/prds/{id}/decompose
//	  → 202 Accepted { "task_id": "<id>", "stream_url": "..." }
//
//	GET /api/autonomous/prds/{id}/decompose/stream
//	  → text/event-stream
//	    data: {"type":"story","index":0,"title":"...","description":"...","id":"..."}
//	    data: {"type":"progress","done":3,"total":7}
//	    data: {"type":"complete","story_count":7}
//	    data: {"type":"error","message":"..."}
//
//	GET /api/autonomous/prds/{id}/decompose/status
//	  → JSON { "status":"pending|in_progress|complete|error", ... }

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
)

// decomposeJobStatus mirrors the status enum used in SSE + polling.
type decomposeJobStatus string

const (
	decomposeJobPending    decomposeJobStatus = "pending"
	decomposeJobInProgress decomposeJobStatus = "in_progress"
	decomposeJobComplete   decomposeJobStatus = "complete"
	decomposeJobError      decomposeJobStatus = "error"
)

// decomposeStory is a story yielded by DecomposeStreaming, stored as
// JSON-serialisable map so the server package stays free of the
// autonomous package's concrete Story type.
type decomposeStory map[string]any

// decomposeEvent is one SSE frame stored for replay on reconnect.
type decomposeEvent struct {
	id   int
	data []byte // pre-encoded JSON
}

// decomposeJob holds the in-memory state of one async decompose run.
type decomposeJob struct {
	mu       sync.RWMutex
	prdID    string
	status   decomposeJobStatus
	progress int          // stories yielded so far
	total    int          // total expected (0 = unknown until complete)
	stories  []decomposeStory
	errMsg   string
	events   []decomposeEvent // append-only replay log
	nextID   int              // monotonic SSE event id

	// notifyCh is closed (and re-created) each time a new event arrives
	// so waiting SSE subscribers wake up.
	notifyCh chan struct{}
}

func newDecomposeJob(prdID string) *decomposeJob {
	return &decomposeJob{
		prdID:    prdID,
		status:   decomposeJobPending,
		notifyCh: make(chan struct{}),
	}
}

// appendEvent appends a pre-encoded JSON payload as the next SSE event
// and closes notifyCh to wake subscribers. Caller holds write lock.
func (j *decomposeJob) appendEvent(data []byte) {
	j.nextID++
	j.events = append(j.events, decomposeEvent{id: j.nextID, data: data})
	// Close the current notify channel to wake waiters, then replace.
	ch := j.notifyCh
	j.notifyCh = make(chan struct{})
	close(ch)
}

// notify returns the current notification channel (no lock needed when
// reading atomically).
func (j *decomposeJob) notify() <-chan struct{} {
	j.mu.RLock()
	ch := j.notifyCh
	j.mu.RUnlock()
	return ch
}

// handleDecomposeAsync handles POST /api/autonomous/prds/{id}/decompose.
// It returns 202 Accepted immediately and launches a background goroutine
// that calls DecomposeStreaming.
func (s *Server) handleDecomposeAsync(w http.ResponseWriter, r *http.Request, prdID string) {
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	// If a job is already in flight for this PRD, return its status.
	if existing, ok := s.decomposeJobs.Load(prdID); ok {
		j := existing.(*decomposeJob)
		j.mu.RLock()
		status := j.status
		j.mu.RUnlock()
		if status == decomposeJobPending || status == decomposeJobInProgress {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task_id":    prdID,
				"stream_url": fmt.Sprintf("/api/autonomous/prds/%s/decompose/stream", prdID),
				"message":    "decompose already in progress",
			})
			return
		}
	}

	job := newDecomposeJob(prdID)
	s.decomposeJobs.Store(prdID, job)

	go func() {
		job.mu.Lock()
		job.status = decomposeJobInProgress
		job.mu.Unlock()

		cb := func(index, total int, storyAny any) {
			// Convert to a map for JSON serialisation.
			var story decomposeStory
			if b, err := json.Marshal(storyAny); err == nil {
				_ = json.Unmarshal(b, &story)
			}

			job.mu.Lock()
			job.stories = append(job.stories, story)
			job.progress = index + 1
			job.total = total

			// story event
			storyPayload := map[string]any{
				"type":        "story",
				"index":       index,
				"title":       story["title"],
				"description": story["description"],
				"id":          story["id"],
			}
			if b, err := json.Marshal(storyPayload); err == nil {
				job.appendEvent(b)
			}
			// progress event
			progressPayload := map[string]any{
				"type":  "progress",
				"done":  index + 1,
				"total": total,
			}
			if b, err := json.Marshal(progressPayload); err == nil {
				job.appendEvent(b)
			}
			job.mu.Unlock()
		}

		_, err := s.autonomousMgr.DecomposeStreaming(prdID, cb)

		job.mu.Lock()
		if err != nil {
			job.status = decomposeJobError
			job.errMsg = err.Error()
			errPayload := map[string]any{"type": "error", "message": err.Error()}
			if b, merr := json.Marshal(errPayload); merr == nil {
				job.appendEvent(b)
			}
		} else {
			job.status = decomposeJobComplete
			donePayload := map[string]any{"type": "complete", "story_count": len(job.stories)}
			if b, merr := json.Marshal(donePayload); merr == nil {
				job.appendEvent(b)
			}
		}
		// Final wake — close notifyCh so stream handlers stop waiting.
		ch := job.notifyCh
		job.notifyCh = make(chan struct{})
		close(ch)
		job.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"task_id":    prdID,
		"stream_url": fmt.Sprintf("/api/autonomous/prds/%s/decompose/stream", prdID),
	})
}

// handleDecomposeStream handles GET /api/autonomous/prds/{id}/decompose/stream.
// It upgrades the connection to text/event-stream and streams events.
// Supports Last-Event-ID for resumption.
func (s *Server) handleDecomposeStream(w http.ResponseWriter, r *http.Request, prdID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Find or wait briefly for the job.
	var job *decomposeJob
	if v, ok := s.decomposeJobs.Load(prdID); ok {
		job = v.(*decomposeJob)
	} else {
		http.Error(w, "no decompose job found for this PRD — POST /decompose first", http.StatusNotFound)
		return
	}

	// Parse Last-Event-ID for resumption.
	lastID := 0
	if leid := r.Header.Get("Last-Event-ID"); leid != "" {
		if n, err := strconv.Atoi(leid); err == nil {
			lastID = n
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// Hint the client to reconnect after 1s on disconnect.
	_, _ = fmt.Fprintf(w, "retry: 1000\n\n")
	flusher.Flush()

	ctx := r.Context()
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	// Cursor tracks how many events from job.events we have sent.
	// We start from lastID (re-emit events after lastID).
	sent := lastID // index into events = sent (events are 1-indexed so events[sent-1] is last sent)

	for {
		// Drain any unsent events.
		job.mu.RLock()
		eventsSnap := job.events
		jobStatus := job.status
		job.mu.RUnlock()

		for sent < len(eventsSnap) {
			ev := eventsSnap[sent]
			_, _ = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.id, ev.data)
			flusher.Flush()
			sent++
		}

		// If job is terminal, we're done.
		if jobStatus == decomposeJobComplete || jobStatus == decomposeJobError {
			return
		}

		// Wait for more events, client disconnect, or keepalive tick.
		notifyCh := job.notify()
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			_, _ = fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case <-notifyCh:
			// New events or terminal state — loop back to drain.
		}
	}
}

// handleDecomposeStatus handles GET /api/autonomous/prds/{id}/decompose/status.
// Returns the current job state as JSON (polling fallback).
func (s *Server) handleDecomposeStatus(w http.ResponseWriter, r *http.Request, prdID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}

	v, ok := s.decomposeJobs.Load(prdID)
	if !ok {
		// No active job — check the PRD itself.
		if prd, exists := s.autonomousMgr.GetPRD(prdID); exists {
			writeJSONOK(w, map[string]any{
				"status": "no_active_job",
				"prd":    prd,
			})
			return
		}
		http.Error(w, "PRD not found", http.StatusNotFound)
		return
	}

	job := v.(*decomposeJob)
	job.mu.RLock()
	resp := map[string]any{
		"status":   string(job.status),
		"progress": job.progress,
		"total":    job.total,
		"stories":  job.stories,
	}
	if job.errMsg != "" {
		resp["error"] = job.errMsg
	}
	job.mu.RUnlock()
	writeJSONOK(w, resp)
}
