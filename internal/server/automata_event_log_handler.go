package server

// Automata event log REST read: GET /api/autonomous/event-log.
//
// The route merges the event adapters (watch findings, lane fill decisions,
// infrastructure retries, recorded checks, safe-to-delete, cleanup, repo CI
// watch and raw error classification) into one newest-first list, then
// filters and pages it. Query parameters:
//
//	kind     comma list of event kinds; an unknown kind is a 400
//	project  exact project match
//	q        case-insensitive text over summary, detail, project,
//	         automaton and automation
//	limit    page size, 1..200, default 50; anything else is a 400
//	before   opaque cursor from the previous page's next_cursor; a bad
//	         cursor is a 400
//
// Response: {"events": [...], "next_cursor": "...", "total": n}. next_cursor
// is empty on the last page. total counts every event that matches the
// filters, ignoring the cursor. Read scope is the same autonomous read
// capability the other autonomous read routes require.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	automataEventLogDefaultLimit = 50
	automataEventLogMaxLimit     = 200
	// automataEventLogSourceLimit bounds the rows read from the sources that
	// take a limit (lane fill decisions and infrastructure retries).
	automataEventLogSourceLimit = 500
)

// automataRawError is one raw error a source reported, before classification.
type automataRawError struct {
	Time       time.Time
	Source     string
	Raw        string
	Project    string
	Automaton  string
	Automation string
}

// automataRawErrorsReader is implemented by a manager that records raw errors.
// A manager without it has no error feed, so the error adapter is skipped.
type automataRawErrorsReader interface {
	AutomataRawErrors() []automataRawError
}

// automataRawErrorsSource returns the raw errors recorded by the daemon,
// newest first.
type automataRawErrorsSource func() []automataRawError

// AutomataEventsFromRawErrors classifies each raw error through the shared
// error rule and returns it as an error-path event. A nil src is an error,
// not an empty log.
func AutomataEventsFromRawErrors(src automataRawErrorsSource) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataSourceNil
	}
	raws := src()
	out := make([]AutomataEvent, 0, len(raws))
	for i, r := range raws {
		c := automataEventErrorClassify(r.Raw, r.Source)
		out = append(out, AutomataEvent{
			ID:         fmt.Sprintf("error:%s:%s:%d", r.Source, r.Time.UTC().Format(time.RFC3339Nano), i),
			Time:       r.Time.UTC(),
			Kind:       AutomataEventError,
			Project:    r.Project,
			Automaton:  r.Automaton,
			Automation: r.Automation,
			Summary:    c.Summary,
			Reason:     c.Reason,
			NextAction: c.NextAction,
			Quiet:      c.Quiet,
			Severity:   c.Severity,
		})
	}
	return out, nil
}

// automataEventLogSources holds one source per adapter. A nil source means
// this daemon does not have that feed, so the adapter is skipped.
type automataEventLogSources struct {
	watch     automataWatchFindingsSource
	decisions automataLaneDecisionsSource
	retries   automataInfraRetryTasksSource
	checks    automataChecksFindingsSource
	safe      automataSafeDeleteReportSource
	cleanup   automataCleanupStatusSource
	repoCI    automataRepoCIWatchStateSource
	errors    automataRawErrorsSource
}

// automataEventLogSourcesFor builds the sources from the Automata manager's
// existing read methods, so the log shows what the status routes show.
func (s *Server) automataEventLogSourcesFor(ctx context.Context) automataEventLogSources {
	var src automataEventLogSources
	mgr := s.autonomousMgr
	if w, ok := mgr.(supervisorWatchReader); ok {
		src.watch = func() []*autonomous.SupervisorWatchFinding { return w.SupervisorWatchStatus().Findings }
	}
	if mgr != nil {
		src.decisions = func(limit int) []autonomous.LaneFillDecision { return mgr.LaneFillStatus(limit).Decisions }
	}
	if api, ok := mgr.(infraRetryAPI); ok {
		src.retries = func(limit int) []autonomous.InfraRetryTask { return api.InfraRetryStatus("", limit).Tasks }
	}
	if tr, ok := mgr.(triageReader); ok {
		src.checks = func() []autonomous.StatusFinding {
			return autonomous.BuildChecksStatus(tr.ListTriageRecords(), "", "").Findings
		}
	}
	if mr, ok := mgr.(mergeReader); ok {
		src.safe = func() (autonomous.SafeDeleteReport, error) {
			c, cancel := context.WithTimeout(ctx, mergeSafeDeleteTimeout)
			defer cancel()
			rep, err := mr.MergeSafeToDelete(c)
			if errors.Is(err, autonomous.ErrMergeNoBaseRepo) {
				// No base repo means nothing to delete, not a failed log. The
				// adapter needs a computed_at time, so stamp the empty report.
				return autonomous.SafeDeleteReport{ComputedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
			}
			return rep, err
		}
	}
	if cr, ok := mgr.(cleanupStatusReader); ok {
		src.cleanup = cr.CleanupStatus
	}
	if rc, ok := mgr.(repoCIWatchReader); ok {
		src.repoCI = rc.RepoCIWatchStatus
	}
	if re, ok := mgr.(automataRawErrorsReader); ok {
		src.errors = re.AutomataRawErrors
	}
	return src
}

// collectAutomataEvents runs every present adapter and merges the results
// into one newest-first list. A source error stops the read: a partial log
// would hide what failed.
func (s *Server) collectAutomataEvents(ctx context.Context) ([]AutomataEvent, error) {
	src := s.automataEventLogSourcesFor(ctx)
	var steps []func() ([]AutomataEvent, error)
	if src.watch != nil {
		steps = append(steps, func() ([]AutomataEvent, error) { return AutomataEventsFromSupervisorWatch(src.watch) })
	}
	if src.decisions != nil {
		steps = append(steps, func() ([]AutomataEvent, error) {
			return AutomataEventsFromLaneFillDecisions(src.decisions, automataEventLogSourceLimit)
		})
	}
	if src.retries != nil {
		steps = append(steps, func() ([]AutomataEvent, error) {
			return AutomataEventsFromInfraRetries(src.retries, automataEventLogSourceLimit)
		})
	}
	if src.checks != nil {
		steps = append(steps, func() ([]AutomataEvent, error) { return AutomataEventsFromChecks(src.checks) })
	}
	if src.safe != nil {
		steps = append(steps, func() ([]AutomataEvent, error) { return AutomataEventsFromSafeToDeleteReport(src.safe) })
	}
	if src.cleanup != nil {
		steps = append(steps, func() ([]AutomataEvent, error) { return AutomataEventsFromCleanupStatus(src.cleanup) })
	}
	if src.repoCI != nil {
		steps = append(steps, func() ([]AutomataEvent, error) { return AutomataEventsFromRepoCIWatch(src.repoCI) })
	}
	if src.errors != nil {
		steps = append(steps, func() ([]AutomataEvent, error) { return AutomataEventsFromRawErrors(src.errors) })
	}
	lists := make([][]AutomataEvent, 0, len(steps))
	for _, step := range steps {
		evs, err := step()
		if err != nil {
			return nil, err
		}
		lists = append(lists, evs)
	}
	return MergeAutomataEvents(lists...), nil
}

// automataEventLogQuery is the parsed, validated query string.
type automataEventLogQuery struct {
	kinds   map[AutomataEventKind]bool // nil means every kind
	project string
	text    string // lower-cased; empty means no text filter
	limit   int
	cursor  AutomataEventCursor
}

// automataEventKindKnown reports whether k is one of the defined kinds.
func automataEventKindKnown(k AutomataEventKind) bool {
	switch k {
	case AutomataEventWatch, AutomataEventDecision, AutomataEventRetry,
		AutomataEventCheck, AutomataEventCleanup, AutomataEventAlert, AutomataEventError:
		return true
	}
	return false
}

// parseAutomataEventLogQuery validates the query string. An absent or blank
// limit means the default; a limit that is not a whole number from 1 to the
// maximum is refused, as is an unknown kind or a malformed cursor.
func parseAutomataEventLogQuery(v url.Values) (automataEventLogQuery, error) {
	var q automataEventLogQuery
	q.limit = automataEventLogDefaultLimit
	if raw := strings.TrimSpace(v.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > automataEventLogMaxLimit {
			return q, fmt.Errorf("limit must be a whole number from 1 to %d", automataEventLogMaxLimit)
		}
		q.limit = n
	}
	cursor, err := DecodeAutomataEventCursor(strings.TrimSpace(v.Get("before")))
	if err != nil {
		return q, errors.New("before must be a cursor from a previous next_cursor")
	}
	q.cursor = cursor
	if raw := strings.TrimSpace(v.Get("kind")); raw != "" {
		q.kinds = make(map[AutomataEventKind]bool)
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			k := AutomataEventKind(part)
			if !automataEventKindKnown(k) {
				return q, fmt.Errorf("unknown kind %q", part)
			}
			q.kinds[k] = true
		}
		if len(q.kinds) == 0 {
			q.kinds = nil
		}
	}
	q.project = strings.TrimSpace(v.Get("project"))
	q.text = strings.ToLower(strings.TrimSpace(v.Get("q")))
	return q, nil
}

// automataEventMatchesText reports whether the lower-cased needle appears in
// any of the text fields the q parameter searches.
func automataEventMatchesText(e AutomataEvent, needle string) bool {
	for _, f := range []string{e.Summary, e.Detail, e.Project, e.Automaton, e.Automation} {
		if strings.Contains(strings.ToLower(f), needle) {
			return true
		}
	}
	return false
}

// automataEventLogFilter keeps the events that match the kind, project and
// text filters. Order is preserved.
func automataEventLogFilter(events []AutomataEvent, q automataEventLogQuery) []AutomataEvent {
	out := make([]AutomataEvent, 0, len(events))
	for _, e := range events {
		if q.kinds != nil && !q.kinds[e.Kind] {
			continue
		}
		if q.project != "" && e.Project != q.project {
			continue
		}
		if q.text != "" && !automataEventMatchesText(e, q.text) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// automataEventLogJSON is the wire form of one event. Every field is always
// present, so a client never has to tell a missing field from an empty one.
type automataEventLogJSON struct {
	ID         string                `json:"id"`
	Time       time.Time             `json:"time"`
	Kind       AutomataEventKind     `json:"kind"`
	Project    string                `json:"project"`
	Automaton  string                `json:"automaton"`
	Automation string                `json:"automation"`
	Summary    string                `json:"summary"`
	Detail     string                `json:"detail"`
	Reason     string                `json:"reason"`
	NextAction string                `json:"next_action"`
	Quiet      bool                  `json:"quiet"`
	Severity   AutomataEventSeverity `json:"severity"`
}

// automataEventLogResponse is the body of GET /api/autonomous/event-log.
type automataEventLogResponse struct {
	Events     []automataEventLogJSON `json:"events"`
	NextCursor string                 `json:"next_cursor"`
	Total      int                    `json:"total"`
}

// automataEventLogPage filters, then pages, a merged newest-first list. It
// reads limit+1 events to learn whether another page exists.
func automataEventLogPage(merged []AutomataEvent, q automataEventLogQuery) automataEventLogResponse {
	filtered := automataEventLogFilter(merged, q)
	window := AutomataEventsBefore(filtered, q.cursor, q.limit+1)
	next := ""
	if len(window) > q.limit {
		window = window[:q.limit]
		last := window[len(window)-1]
		next = AutomataEventCursor{Time: last.Time, ID: last.ID}.Encode()
	}
	resp := automataEventLogResponse{
		Events:     make([]automataEventLogJSON, 0, len(window)),
		NextCursor: next,
		Total:      len(filtered),
	}
	for _, e := range window {
		resp.Events = append(resp.Events, automataEventLogJSON{
			ID:         e.ID,
			Time:       e.Time.UTC(),
			Kind:       e.Kind,
			Project:    e.Project,
			Automaton:  e.Automaton,
			Automation: e.Automation,
			Summary:    e.Summary,
			Detail:     e.Detail,
			Reason:     e.Reason,
			NextAction: e.NextAction,
			Quiet:      e.Quiet,
			Severity:   e.Severity,
		})
	}
	return resp
}

// handleAutomataEventLog serves GET /api/autonomous/event-log.
func (s *Server) handleAutomataEventLog(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	q, err := parseAutomataEventLogQuery(r.URL.Query())
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	merged, err := s.collectAutomataEvents(r.Context())
	if err != nil {
		http.Error(w, "event log source failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, automataEventLogPage(merged, q))
}
