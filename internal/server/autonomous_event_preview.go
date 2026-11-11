package server

// Subscription preview REST: POST /api/autonomous/subscriptions/preview.
//
// The route answers "what would this subscription filter have caught lately?"
// without storing anything. The body is a filter:
//
//	kinds    list of event kinds; an empty list means every kind, an unknown
//	         kind is a 400
//	project  exact project match; empty means every project
//	owner    exact Automation match; empty means every Automation
//
// The filter is applied to the same merged event log GET
// /api/autonomous/event-log reads, with the same rules live delivery uses:
// an empty field is a wildcard, kinds are alternatives, and project and owner
// must match exactly. Only events from the last 24 hours count.
//
// Response: {"count": n, "window_hours": 24, "sample": [...]}. count is every
// match in the window; sample holds the newest matches, at most 20. The route
// reads and never writes, so it needs only the autonomous read capability.

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
)

const (
	eventPreviewWindow     = 24 * time.Hour
	eventPreviewSampleSize = 20
)

// eventPreviewRequest is the body of the preview route.
type eventPreviewRequest struct {
	Kinds   []string `json:"kinds"`
	Project string   `json:"project"`
	Owner   string   `json:"owner"`
}

// eventPreviewResponse is the body returned by the preview route.
type eventPreviewResponse struct {
	Count       int                    `json:"count"`
	WindowHours int                    `json:"window_hours"`
	Sample      []automataEventLogJSON `json:"sample"`
}

// eventPreviewQuery turns a request into the event log filter, refusing an
// unknown kind.
func eventPreviewQuery(req eventPreviewRequest) (automataEventLogQuery, error) {
	var q automataEventLogQuery
	for _, raw := range req.Kinds {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		k := AutomataEventKind(raw)
		if !automataEventKindKnown(k) {
			return q, &eventPreviewError{"unknown kind \"" + raw + "\""}
		}
		if q.kinds == nil {
			q.kinds = make(map[AutomataEventKind]bool)
		}
		q.kinds[k] = true
	}
	q.project = strings.TrimSpace(req.Project)
	return q, nil
}

type eventPreviewError struct{ msg string }

func (e *eventPreviewError) Error() string { return e.msg }

// eventPreviewBuild filters merged events to the window ending at now and the
// request, then counts them and samples the newest. merged is newest first.
func eventPreviewBuild(merged []AutomataEvent, q automataEventLogQuery, owner string, now time.Time) eventPreviewResponse {
	cutoff := now.Add(-eventPreviewWindow)
	recent := make([]AutomataEvent, 0, len(merged))
	for _, e := range merged {
		if e.Time.Before(cutoff) {
			continue
		}
		if owner != "" && e.Automation != owner {
			continue
		}
		recent = append(recent, e)
	}
	matched := automataEventLogFilter(recent, q)
	resp := eventPreviewResponse{
		Count:       len(matched),
		WindowHours: int(eventPreviewWindow / time.Hour),
		Sample:      []automataEventLogJSON{},
	}
	if len(matched) > eventPreviewSampleSize {
		matched = matched[:eventPreviewSampleSize]
	}
	page := automataEventLogPage(matched, automataEventLogQuery{limit: eventPreviewSampleSize})
	resp.Sample = page.Events
	return resp
}

// handleEventPreview serves POST /api/autonomous/subscriptions/preview.
func (s *Server) handleEventPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
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
	var req eventPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	q, err := eventPreviewQuery(req)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	merged, err := s.collectAutomataEvents(r.Context())
	if err != nil {
		http.Error(w, "event log source failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, eventPreviewBuild(merged, q, strings.TrimSpace(req.Owner), time.Now().UTC()))
}
