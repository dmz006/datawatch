package server

// Subscription preview REST: POST /api/autonomous/subscriptions/preview.
//
// The route answers "what would this subscription filter have caught lately?"
// without storing anything. The body is a filter:
//
//	kinds    list of event kinds; an empty list means every kind, an unknown
//	         kind is a 400. A kind is either an Events log kind (watch,
//	         decision, retry, check, cleanup, alert, error) or a lifecycle kind
//	         (the kinds GET /api/autonomous/event-kinds lists, such as
//	         task.failed): the picker lists lifecycle kinds, so both are
//	         accepted
//	project  exact project match; empty means every project
//	owner    exact Automation match; empty means every Automation
//
// Events log kinds are counted from the same merged event log GET
// /api/autonomous/event-log reads, with the same rules live delivery uses:
// an empty field is a wildcard, kinds are alternatives, and project and owner
// must match exactly. Only events from the last 24 hours count.
//
// Lifecycle kinds are counted from the lifecycle events this daemon recorded
// in memory (autonomous.Manager.RecentLifecycleEvents), so they cover the
// events since the daemon started: "lifecycle_since" in the response is when
// recording began. owner matches a lifecycle event's Automation id or
// Automaton id; project is not recorded on a lifecycle event, so it narrows
// the Events log kinds only. A request that names both kinds of kind gets the
// two counts added.
//
// Response: {"count": n, "window_hours": 24, "sample": [...]}. count is every
// match in the window; sample holds the newest matches, at most 20. The route
// reads and never writes, so it needs only the autonomous read capability.

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
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
	// LifecycleSince is when this daemon began recording lifecycle events;
	// set only when the request named a lifecycle kind.
	LifecycleSince *time.Time `json:"lifecycle_since,omitempty"`
}

// eventPreviewLifecycleSource is what the preview reads lifecycle events from;
// autonomous.Manager satisfies it.
type eventPreviewLifecycleSource interface {
	RecentLifecycleEvents(since time.Time) ([]autonomous.LifecycleEvent, time.Time)
}

// eventPreviewQuery turns a request into the event log filter, refusing an
// unknown kind.
func eventPreviewQuery(req eventPreviewRequest) (automataEventLogQuery, []string, error) {
	var q automataEventLogQuery
	var lifecycle []string
	for _, raw := range req.Kinds {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		k := AutomataEventKind(raw)
		if !automataEventKindKnown(k) {
			if eventPreviewListedKind(raw) {
				lifecycle = append(lifecycle, raw)
				continue
			}
			return q, nil, &eventPreviewError{"unknown kind \"" + raw + "\""}
		}
		if q.kinds == nil {
			q.kinds = make(map[AutomataEventKind]bool)
		}
		q.kinds[k] = true
	}
	q.project = strings.TrimSpace(req.Project)
	return q, lifecycle, nil
}

// eventPreviewFromLifecycle counts the recorded lifecycle events of kinds in
// the window ending at now, optionally for one owner, and returns the count and
// the newest matches in the log's wire form. events is newest first.
func eventPreviewFromLifecycle(events []autonomous.LifecycleEvent, kinds []string, owner string, now time.Time) (int, []automataEventLogJSON) {
	want := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	cutoff := now.Add(-eventPreviewWindow)
	var out []automataEventLogJSON
	count := 0
	for _, ev := range events {
		if !want[ev.Kind] || ev.At.Before(cutoff) {
			continue
		}
		if owner != "" && ev.AutomationID != owner && ev.PRDID != owner {
			continue
		}
		count++
		if len(out) < eventPreviewSampleSize {
			summary := ev.Kind
			if ev.NewStatus != "" {
				summary += " (" + ev.NewStatus + ")"
			}
			out = append(out, automataEventLogJSON{
				ID: ev.ID, Time: ev.At.UTC(), Kind: AutomataEventKind(ev.Kind),
				Automaton: ev.PRDID, Automation: ev.AutomationID,
				Summary: summary, Detail: ev.Details, Severity: AutomataSeverityInfo,
			})
		}
	}
	return count, out
}

// eventPreviewListedKind reports whether kind is one GET
// /api/autonomous/event-kinds lists, which is what the picker offers. That is
// every registered kind, including the listed kinds a subscription filter does
// not accept (task.looping), because the picker shows a count for each.
func eventPreviewListedKind(kind string) bool {
	for _, info := range autonomous.ListEventKinds() {
		if info.Kind == kind {
			return true
		}
	}
	return false
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
	q, lifecycle, err := eventPreviewQuery(req)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	owner := strings.TrimSpace(req.Owner)
	// Lifecycle kinds only: the Events log has nothing to say about them, so
	// it is not read. An empty kinds list still reads the Events log alone.
	var resp eventPreviewResponse
	if q.kinds != nil || len(lifecycle) == 0 {
		merged, err := s.collectAutomataEvents(r.Context())
		if err != nil {
			http.Error(w, "event log source failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		resp = eventPreviewBuild(merged, q, owner, now)
	} else {
		resp = eventPreviewResponse{WindowHours: int(eventPreviewWindow / time.Hour), Sample: []automataEventLogJSON{}}
	}
	if len(lifecycle) > 0 {
		src, ok := s.autonomousMgr.(eventPreviewLifecycleSource)
		if !ok {
			http.Error(w, "lifecycle event history is not available on this daemon", http.StatusServiceUnavailable)
			return
		}
		events, since := src.RecentLifecycleEvents(now.Add(-eventPreviewWindow))
		n, sample := eventPreviewFromLifecycle(events, lifecycle, owner, now)
		resp.Count += n
		resp.Sample = append(resp.Sample, sample...)
		sort.SliceStable(resp.Sample, func(i, j int) bool { return resp.Sample[i].Time.After(resp.Sample[j].Time) })
		if len(resp.Sample) > eventPreviewSampleSize {
			resp.Sample = resp.Sample[:eventPreviewSampleSize]
		}
		if !since.IsZero() {
			u := since.UTC()
			resp.LifecycleSince = &u
		}
	}
	writeJSONOK(w, resp)
}
