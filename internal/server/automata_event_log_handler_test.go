package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// eventLogFake is an Automata manager that serves only the reads the event
// log uses. Every other AutonomousAPI method is nil, so a call to one panics.
type eventLogFake struct {
	AutonomousAPI
	rawErrs []automataRawError
	repoCI  autonomous.RepoCIWatchStatus
	cleanup autonomous.CleanupStatus
	safeErr error
	prds    []any
}

// ListPRDs returns the Automata the fake holds, so failed-task reasons reach
// the error feed the same way they do in the daemon.
func (f *eventLogFake) ListPRDs() []any { return f.prds }

func (f *eventLogFake) SupervisorWatchStatus() autonomous.SupervisorWatchStatus {
	return autonomous.SupervisorWatchStatus{}
}
func (f *eventLogFake) InfraRetryStatus(string, int) autonomous.InfraRetryStatus {
	return autonomous.InfraRetryStatus{}
}
func (f *eventLogFake) LaneFillStatus(int) autonomous.LaneFillStatus {
	return autonomous.LaneFillStatus{}
}
func (f *eventLogFake) ListTriageRecords() []*autonomous.TriageRecord { return nil }
func (f *eventLogFake) TriageAlertMinutes() int                       { return 0 }
func (f *eventLogFake) MergeStatus() autonomous.MergeStatus           { return autonomous.MergeStatus{} }
func (f *eventLogFake) MergeSafeToDelete(context.Context) (autonomous.SafeDeleteReport, error) {
	if f.safeErr != nil {
		return autonomous.SafeDeleteReport{}, f.safeErr
	}
	return autonomous.SafeDeleteReport{ComputedAt: "2026-11-09T12:00:00Z"}, nil
}
func (f *eventLogFake) CleanupStatus() autonomous.CleanupStatus         { return f.cleanup }
func (f *eventLogFake) RepoCIWatchStatus() autonomous.RepoCIWatchStatus { return f.repoCI }
func (f *eventLogFake) AutomataRawErrors() []automataRawError           { return f.rawErrs }

// eventLogServer returns a server whose manager is fake.
func eventLogServer(fake AutonomousAPI) *Server {
	return &Server{cfg: config.DefaultConfig(), autonomousMgr: fake}
}

// getEventLog runs GET against the handler with the given raw query.
func getEventLog(t *testing.T, s *Server, rawQuery string) (int, map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/event-log?"+rawQuery, nil)
	s.handleAutomataEventLog(rr, req)
	var body map[string]any
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v; raw %q", err, rr.Body.String())
		}
	}
	return rr.Code, body
}

// sampleEvents builds a small list with distinct times and ids, newest first.
func sampleEvents() []AutomataEvent {
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	return MergeAutomataEvents([]AutomataEvent{
		{ID: "a", Time: base, Kind: AutomataEventWatch, Project: "datawatch", Automaton: "auto-1",
			Summary: "Watch saw a stall", Severity: AutomataSeverityWarn},
		{ID: "b", Time: base.Add(-time.Minute), Kind: AutomataEventRetry, Project: "Other",
			Automation: "chain-9", Summary: "Retry scheduled", Detail: "backoff 30s", Severity: AutomataSeverityInfo},
		{ID: "c", Time: base.Add(-2 * time.Minute), Kind: AutomataEventError, Project: "datawatch",
			Summary: "Permission denied in cleanup", NextAction: "Grant access", Severity: AutomataSeverityError},
		{ID: "d", Time: base.Add(-3 * time.Minute), Kind: AutomataEventCleanup, Project: "datawatch",
			Summary: "Cleanup pass ok", Quiet: true, Severity: AutomataSeverityInfo},
		{ID: "e", Time: base.Add(-4 * time.Minute), Kind: AutomataEventDecision, Project: "datawatch",
			Automaton: "auto-2", Summary: "Lane filled", Severity: AutomataSeverityInfo},
	})
}

func TestAutomataEventLogHandlerQueryDefaults(t *testing.T) {
	q, err := parseAutomataEventLogQuery(url.Values{})
	if err != nil {
		t.Fatalf("empty query: %v", err)
	}
	if q.limit != automataEventLogDefaultLimit || q.kinds != nil || q.project != "" || q.text != "" || !q.cursor.IsZero() {
		t.Fatalf("defaults wrong: %+v", q)
	}
	q, err = parseAutomataEventLogQuery(url.Values{"limit": {"200"}, "kind": {" watch , ,retry "}, "q": {"  Stall "}, "project": {" datawatch "}})
	if err != nil {
		t.Fatalf("valid query: %v", err)
	}
	if q.limit != 200 || !q.kinds[AutomataEventWatch] || !q.kinds[AutomataEventRetry] || len(q.kinds) != 2 {
		t.Fatalf("parsed kinds/limit wrong: %+v", q)
	}
	if q.text != "stall" || q.project != "datawatch" {
		t.Fatalf("text/project not normalised: %+v", q)
	}
	// A blank limit and a comma-only kind list both mean the default.
	q, err = parseAutomataEventLogQuery(url.Values{"limit": {"  "}, "kind": {" , "}})
	if err != nil || q.limit != automataEventLogDefaultLimit || q.kinds != nil {
		t.Fatalf("blank limit or kind: q=%+v err=%v", q, err)
	}
}

func TestAutomataEventLogHandlerRejectsBadInput(t *testing.T) {
	s := eventLogServer(&eventLogFake{})
	for name, query := range map[string]string{
		"limit zero":     "limit=0",
		"limit over max": "limit=201",
		"limit not int":  "limit=abc",
		"bad cursor":     "before=not-a-cursor!!",
		"cursor garbage": "before=bm9wZQ",
		"unknown kind":   "kind=watch,bogus",
		"limit negative": "limit=-3",
	} {
		t.Run(name, func(t *testing.T) {
			code, _ := getEventLog(t, s, query)
			if code != http.StatusBadRequest {
				t.Fatalf("%s: status %d, want 400", query, code)
			}
		})
	}
}

func TestAutomataEventLogHandlerFilters(t *testing.T) {
	merged := sampleEvents()
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"all", url.Values{}.Encode(), []string{"a", "b", "c", "d", "e"}},
		{"kind list", "kind=retry,error", []string{"b", "c"}},
		{"project exact", "project=datawatch", []string{"a", "c", "d", "e"}},
		{"project no partial", "project=data", nil},
		{"text summary case-insensitive", "q=STALL", []string{"a"}},
		{"text detail", "q=backoff", []string{"b"}},
		{"text project", "q=other", []string{"b"}},
		{"text automaton", "q=AUTO-2", []string{"e"}},
		{"text automation", "q=chain-9", []string{"b"}},
		{"kind and project", "kind=cleanup&project=datawatch", []string{"d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vals, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.query, err)
			}
			q, err := parseAutomataEventLogQuery(vals)
			if err != nil {
				t.Fatalf("parse query: %v", err)
			}
			got := automataEventLogPage(merged, q)
			ids := make([]string, 0, len(got.Events))
			for _, e := range got.Events {
				ids = append(ids, e.ID)
			}
			if strings.Join(ids, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("ids = %v, want %v", ids, tc.want)
			}
			if got.Total != len(tc.want) {
				t.Fatalf("total = %d, want %d", got.Total, len(tc.want))
			}
		})
	}
}

func TestAutomataEventLogHandlerPaging(t *testing.T) {
	merged := sampleEvents()
	var seen []string
	cursor := ""
	pages := 0
	for {
		q := automataEventLogQuery{limit: 2}
		if cursor != "" {
			c, err := DecodeAutomataEventCursor(cursor)
			if err != nil {
				t.Fatalf("decode cursor: %v", err)
			}
			q.cursor = c
		}
		resp := automataEventLogPage(merged, q)
		if resp.Total != len(merged) {
			t.Fatalf("total = %d on page %d, want %d (total ignores the cursor)", resp.Total, pages, len(merged))
		}
		for _, e := range resp.Events {
			seen = append(seen, e.ID)
		}
		pages++
		if resp.NextCursor == "" {
			break
		}
		if pages > 10 {
			t.Fatal("paging did not terminate")
		}
		cursor = resp.NextCursor
	}
	if strings.Join(seen, ",") != "a,b,c,d,e" {
		t.Fatalf("paged ids = %v, want a..e once each", seen)
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3 (2+2+1)", pages)
	}
}

func TestAutomataEventLogHandlerPageEndsExactly(t *testing.T) {
	// When the events fit exactly, there must be no next cursor.
	merged := sampleEvents()
	resp := automataEventLogPage(merged, automataEventLogQuery{limit: 5})
	if resp.NextCursor != "" || len(resp.Events) != 5 {
		t.Fatalf("exact fit: cursor=%q events=%d", resp.NextCursor, len(resp.Events))
	}
	empty := automataEventLogPage(nil, automataEventLogQuery{limit: 5})
	if empty.Events == nil || len(empty.Events) != 0 || empty.Total != 0 || empty.NextCursor != "" {
		t.Fatalf("empty log = %+v, want non-nil empty events", empty)
	}
}

func TestAutomataEventLogHandlerWireShape(t *testing.T) {
	s := eventLogServer(&eventLogFake{
		repoCI: autonomous.RepoCIWatchStatus{Mode: config.RepoCIWatchModeOff},
	})
	code, body := getEventLog(t, s, "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, key := range []string{"events", "next_cursor", "total"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("response missing %q: %v", key, body)
		}
	}
	events, ok := body["events"].([]any)
	if !ok || len(events) == 0 {
		t.Fatalf("events = %v, want at least the repo CI watch event", body["events"])
	}
	first, ok := events[0].(map[string]any)
	if !ok {
		t.Fatalf("event is not an object: %v", events[0])
	}
	for _, key := range []string{"id", "time", "kind", "project", "automaton", "automation",
		"summary", "detail", "reason", "next_action", "quiet", "severity"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("event missing %q: %v", key, first)
		}
	}
	if first["kind"] == "" || first["summary"] == "" || first["id"] == "" {
		t.Fatalf("event has empty required fields: %v", first)
	}
}

func TestAutomataEventLogHandlerMergesSources(t *testing.T) {
	// Repo CI watch "off" gives a watch event; the cleanup status always gives
	// a cleanup event. The merged list must carry both kinds.
	s := eventLogServer(&eventLogFake{
		repoCI:  autonomous.RepoCIWatchStatus{Mode: config.RepoCIWatchModeOff},
		cleanup: autonomous.CleanupStatus{},
	})
	code, body := getEventLog(t, s, "limit=200")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	kinds := map[string]bool{}
	for _, raw := range body["events"].([]any) {
		kinds[raw.(map[string]any)["kind"].(string)] = true
	}
	if !kinds["watch"] || !kinds["cleanup"] {
		t.Fatalf("kinds = %v, want watch and cleanup from the merged sources", kinds)
	}
	// The kind filter narrows the merged list.
	code, body = getEventLog(t, s, "kind=watch")
	if code != http.StatusOK {
		t.Fatalf("filtered status %d", code)
	}
	for _, raw := range body["events"].([]any) {
		if k := raw.(map[string]any)["kind"]; k != "watch" {
			t.Fatalf("kind filter leaked %v", k)
		}
	}
}

func TestAutomataEventLogHandlerSourceErrorIsServerError(t *testing.T) {
	s := eventLogServer(&eventLogFake{safeErr: errors.New("worktree scan failed")})
	code, _ := getEventLog(t, s, "")
	if code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: a failed source must not look like an empty log", code)
	}
}

func TestAutomataEventLogHandlerNoBaseRepoIsNotAnError(t *testing.T) {
	s := eventLogServer(&eventLogFake{safeErr: autonomous.ErrMergeNoBaseRepo})
	code, _ := getEventLog(t, s, "")
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200: no base repo means nothing to delete", code)
	}
}

func TestAutomataEventLogHandlerMethodAndDisabled(t *testing.T) {
	s := eventLogServer(&eventLogFake{})
	rr := httptest.NewRecorder()
	s.handleAutomataEventLog(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/event-log", nil))
	if rr.Code != http.StatusMethodNotAllowed || rr.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST: status %d allow %q", rr.Code, rr.Header().Get("Allow"))
	}
	off := &Server{cfg: config.DefaultConfig()}
	rr = httptest.NewRecorder()
	off.handleAutomataEventLog(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/event-log", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("no manager: status %d, want 503", rr.Code)
	}
}

func TestAutomataEventLogHandlerRouteRegistered(t *testing.T) {
	// The route must be on the autonomous read list so a loopback request
	// reaches the handler without a redirect hop, like its siblings.
	if !loopbackPathBypassed("/api/autonomous/event-log") {
		t.Fatal("/api/autonomous/event-log is not under the autonomous loopback prefix")
	}
}

func TestAutomataEventLogHandlerRawErrors(t *testing.T) {
	if _, err := AutomataEventsFromRawErrors(nil); err == nil {
		t.Fatal("nil raw-error source must be an error, not an empty log")
	}
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	src := func() []automataRawError {
		return []automataRawError{
			{Time: now, Source: "cleanup", Raw: "stat /x/removed worktree: permission denied", Project: "datawatch"},
			{Time: now, Source: "merge", Raw: "open /root/secret: permission denied", Automaton: "auto-3"},
			{Time: now.Add(-time.Minute), Source: "checks", Raw: "   "},
		}
	}
	evs, err := AutomataEventsFromRawErrors(src)
	if err != nil {
		t.Fatalf("raw errors: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3", len(evs))
	}
	if !evs[0].Quiet || evs[0].Severity != AutomataSeverityInfo || evs[0].Kind != AutomataEventError {
		t.Fatalf("harmless permission error not quiet info: %+v", evs[0])
	}
	if evs[1].Quiet || evs[1].Severity != AutomataSeverityError || evs[1].NextAction == "" {
		t.Fatalf("real permission error not an error with a next action: %+v", evs[1])
	}
	if evs[2].Severity != AutomataSeverityError || evs[2].NextAction == "" {
		t.Fatalf("empty error text not an error with a next action: %+v", evs[2])
	}
	for _, e := range evs {
		if err := e.Valid(); err != nil {
			t.Fatalf("classified event fails Valid: %v", err)
		}
		if e.ID == "" || !strings.HasPrefix(e.ID, "error:") {
			t.Fatalf("error event id = %q", e.ID)
		}
	}
	if evs[1].Automaton != "auto-3" || evs[0].Project != "datawatch" {
		t.Fatalf("project/automaton not carried: %+v %+v", evs[0], evs[1])
	}
}

// TestAutomataEventLogHandlerErrorFeedWired checks that a manager with a raw
// error feed contributes classified error events to the merged log.
func TestAutomataEventLogHandlerErrorFeedWired(t *testing.T) {
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	fake := &eventLogFake{rawErrs: []automataRawError{
		{Time: now, Source: "merge", Raw: "open /root/secret: permission denied", Automaton: "auto-3"},
	}}
	s := eventLogServer(fake)
	evs, err := s.collectAutomataEvents(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	found := false
	for _, e := range evs {
		if e.Kind == AutomataEventError && strings.HasPrefix(e.ID, "error:merge:") && e.Automaton == "auto-3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("raw error feed not merged into event log: %+v", evs)
	}
	// A manager without the feed must still produce a log, without error events.
	evs, err = eventLogServer(&eventLogFake{}).collectAutomataEvents(context.Background())
	if err != nil {
		t.Fatalf("collect without feed: %v", err)
	}
	for _, e := range evs {
		if e.Kind == AutomataEventError {
			t.Fatalf("error event without a feed: %+v", e)
		}
	}
}

// TestAutomataEventLogHandlerErrorsFromFailedTasks checks the production error
// feed: a failed task's recorded failure reason becomes an error event, while
// content judgments and tasks that are not failed stay out of the log.
func TestAutomataEventLogHandlerErrorsFromFailedTasks(t *testing.T) {
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	prd := &autonomous.PRD{
		ID:           "p1",
		Title:        "Build feed",
		ProjectDir:   "/work/proj",
		AutomationID: "auto-9",
		Story: []autonomous.Story{{Tasks: []autonomous.Task{
			{ID: "t1", Status: autonomous.TaskFailed, FailureReason: &autonomous.FailureReason{
				Category: "infra", Detail: "session timed out", Source: "judge", OccurredAt: now,
			}},
			{ID: "t2", Status: autonomous.TaskFailed, FailureReason: &autonomous.FailureReason{
				Category: "content", Detail: "judged deficient", Source: "judge", OccurredAt: now,
			}},
			{ID: "t3", Status: autonomous.TaskStatus("running"), FailureReason: &autonomous.FailureReason{
				Category: "infra", Detail: "stale reason", Source: "judge", OccurredAt: now,
			}},
		}}},
	}
	s := eventLogServer(&eventLogFake{prds: []any{prd}})
	evs, err := s.collectAutomataEvents(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	var errs []AutomataEvent
	for _, e := range evs {
		if e.Kind == AutomataEventError {
			errs = append(errs, e)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("want 1 error event from the infra failure, got %d: %+v", len(errs), errs)
	}
	e := errs[0]
	if !strings.HasPrefix(e.ID, "error:judge:") || e.Project != "/work/proj" || e.Automaton != "Build feed" || e.Automation != "auto-9" {
		t.Fatalf("error event fields wrong: %+v", e)
	}
	if !strings.Contains(e.Reason, "session timed out") {
		t.Fatalf("reason should carry the raw detail: %q", e.Reason)
	}
}

// TestAutomataEventLogHandlerErrorFeedNewestFirstCapped checks that the error
// feed keeps the newest raw errors when a daemon has more than the limit.
func TestAutomataEventLogHandlerErrorFeedNewestFirstCapped(t *testing.T) {
	base := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	in := []automataRawError{
		{Time: base, Source: "a"},
		{Time: base.Add(2 * time.Hour), Source: "c"},
		{Time: base.Add(time.Hour), Source: "b"},
	}
	got := newestRawErrors(in, 2)
	if len(got) != 2 || got[0].Source != "c" || got[1].Source != "b" {
		t.Fatalf("want newest two (c, b), got %+v", got)
	}
}

// TestAutomataEventLogHandlerRealManagerErrorEntry drives the endpoint with a
// real Automata manager (not a fake). A failed task whose failure reason the
// manager stored must come back from GET /api/autonomous/event-log as a
// classified error entry, and a content judgment on the same Automaton must
// not.
func TestAutomataEventLogHandlerRealManagerErrorEntry(t *testing.T) {
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	projectDir := t.TempDir()
	prd, err := mgr.CreatePRD("spec", projectDir, "", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	failedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{{
		ID: "s1", Title: "Story",
		Tasks: []autonomous.Task{
			{ID: "t-infra", Title: "infra fail", Status: autonomous.TaskFailed, FailureReason: &autonomous.FailureReason{
				Category: "infra", Detail: "verifier session timed out after 30m0s", Source: "verifier", OccurredAt: failedAt,
			}},
			{ID: "t-content", Title: "content fail", Status: autonomous.TaskFailed, FailureReason: &autonomous.FailureReason{
				Category: "content", Detail: "judged deficient", Source: "judge", OccurredAt: failedAt,
			}},
		},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	s := &Server{autonomousMgr: newTestAPI(mgr)}

	code, body := getEventLog(t, s, "kind=error")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	events, _ := body["events"].([]any)
	if len(events) != 1 || body["total"] != float64(1) {
		t.Fatalf("want exactly 1 error event from the real manager, got total=%v events=%v", body["total"], events)
	}
	ev := events[0].(map[string]any)
	if ev["kind"] != string(AutomataEventError) {
		t.Fatalf("kind = %v, want error", ev["kind"])
	}
	if ev["project"] != projectDir {
		t.Fatalf("project = %v, want %q", ev["project"], projectDir)
	}
	if id, _ := ev["id"].(string); !strings.HasPrefix(id, "error:verifier:") {
		t.Fatalf("id = %q, want an error:verifier: id", id)
	}
	if ts, _ := ev["time"].(string); ts != failedAt.Format(time.RFC3339) {
		t.Fatalf("time = %q, want %q", ts, failedAt.Format(time.RFC3339))
	}
	if r, _ := ev["reason"].(string); !strings.Contains(r, "timed out") {
		t.Fatalf("reason should carry the raw detail: %q", r)
	}
	if ev["summary"] == "" || ev["next_action"] == "" {
		t.Fatalf("classified entry needs a summary and next action: %v", ev)
	}

	// The same entry is reachable through the project and text filters.
	code, body = getEventLog(t, s, "project="+url.QueryEscape(projectDir)+"&q=TIMED+OUT")
	if code != http.StatusOK {
		t.Fatalf("filtered status = %d", code)
	}
	if n, _ := body["events"].([]any); len(n) != 1 {
		t.Fatalf("project+text filter should return the one error entry, got %d: %v", len(n), body["events"])
	}
}
