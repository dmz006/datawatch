// Tests for the scheduled-job result panel MCP tools (post_result,
// schedule_results_list) — parity with the REST result endpoints.

package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	dwserver "github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

func newTestScheduleResultStore(t *testing.T) *session.ScheduleResultStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schedule-results.json")
	store, err := session.NewScheduleResultStore(path)
	if err != nil {
		t.Fatalf("NewScheduleResultStore: %v", err)
	}
	return store
}

// sessLookup returning a fixed session — the injectable session resolver
// the handlers use instead of the daemon's session manager.
func sessLookup(s *session.Session) func(string) (*session.Session, bool) {
	return func(string) (*session.Session, bool) { return s, true }
}

func resultRequest(args map[string]any) mcpsdk.CallToolRequest {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

func panelText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	c, ok := res.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", res.Content[0])
	}
	return c.Text
}

func TestPostResultTool_Success(t *testing.T) {
	store := newTestScheduleResultStore(t)
	sess := &session.Session{ID: "a1b2", FullID: "host-a1b2", ScheduleName: "nightly-build"}
	s := &Server{
		scheduleResultStore: store,
		resultHistoryLimit:  5,
		resultSessionLookup: sessLookup(sess),
	}

	args := map[string]any{
		"session_id": "host-a1b2",
		"title":      "Build succeeded",
		"status":     "ok",
		"summary":    "All green",
		"metrics":    `{"score": 92.5, "builds": 12}`,
		"tables":     `[{"columns":["job","result"],"rows":[["unit","pass"]]}]`,
		"markdown":   "## Detail\nAll good.",
	}
	res, err := s.handlePostResult(context.Background(), resultRequest(args))
	if err != nil {
		t.Fatalf("handlePostResult: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", panelText(t, res))
	}

	// Same stored entry the REST endpoint would have written.
	hist := store.History("nightly-build")
	if len(hist) != 1 {
		t.Fatalf("expected 1 retained entry, got %d", len(hist))
	}
	got := hist[0].Payload
	if got.Title != "Build succeeded" || got.Status != "ok" || got.Summary != "All green" || got.Markdown != "## Detail\nAll good." {
		t.Fatalf("payload mismatch: %+v", got)
	}
	if len(got.Metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(got.Metrics))
	}
	if len(got.Tables) != 1 || len(got.Tables[0].Rows) != 1 {
		t.Fatalf("expected 1 table with 1 row, got %+v", got.Tables)
	}
	if hist[0].SessionID != "host-a1b2" || hist[0].ScheduleName != "nightly-build" {
		t.Fatalf("entry envelope mismatch: %+v", hist[0])
	}

	// Response shape matches GET /api/sessions/{id}/result's JSON.
	var body map[string]any
	if err := json.Unmarshal([]byte(panelText(t, res)), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["ok"] != true || body["stored"] != true || body["schedule_name"] != "nightly-build" || body["full_id"] != "host-a1b2" {
		t.Fatalf("unexpected response body: %v", body)
	}

	// Board slot updated, like the REST endpoint does.
	board := dwserver.HookBoardFor("host-a1b2")
	if board == nil || board.Result == nil || board.Result.Title != "Build succeeded" {
		t.Fatalf("status-board result slot not updated: %+v", board)
	}
}

func TestPostResultTool_SessionWithoutSchedule(t *testing.T) {
	store := newTestScheduleResultStore(t)
	sess := &session.Session{ID: "c3d4", FullID: "host-c3d4"} // no ScheduleName
	s := &Server{
		scheduleResultStore: store,
		resultSessionLookup: sessLookup(sess),
	}

	res, err := s.handlePostResult(context.Background(), resultRequest(map[string]any{
		"session_id": "c3d4",
		"title":      "Ad-hoc run finished",
		"status":     "warn",
	}))
	if err != nil {
		t.Fatalf("handlePostResult: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", panelText(t, res))
	}
	if got := store.Count(); got != 0 {
		t.Fatalf("expected nothing persisted for an unscheduled session, got %d", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(panelText(t, res)), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["stored"] != false {
		t.Fatalf("expected stored=false, got %v", body["stored"])
	}
	if board := dwserver.HookBoardFor("host-c3d4"); board == nil || board.Result == nil {
		t.Fatal("status-board result slot should still be updated")
	}
}

func TestPostResultTool_MissingTitle(t *testing.T) {
	s := &Server{
		scheduleResultStore: newTestScheduleResultStore(t),
		resultSessionLookup: sessLookup(&session.Session{ID: "e5f6", FullID: "host-e5f6", ScheduleName: "nightly-build"}),
	}
	res, err := s.handlePostResult(context.Background(), resultRequest(map[string]any{
		"session_id": "host-e5f6",
		"status":     "ok",
	}))
	if err != nil {
		t.Fatalf("handlePostResult: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a validation error, got: %s", panelText(t, res))
	}
	if !strings.Contains(panelText(t, res), "title") {
		t.Fatalf("error should mention the missing title: %s", panelText(t, res))
	}
}

func TestPostResultTool_StoreUnavailable(t *testing.T) {
	s := &Server{
		// No scheduleResultStore — mirrors a daemon without one.
		resultSessionLookup: sessLookup(&session.Session{ID: "a7b8", FullID: "host-a7b8", ScheduleName: "nightly-build"}),
	}
	res, err := s.handlePostResult(context.Background(), resultRequest(map[string]any{
		"session_id": "host-a7b8",
		"title":      "Build succeeded",
	}))
	if err != nil {
		t.Fatalf("handlePostResult: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a store-unavailable error, got: %s", panelText(t, res))
	}
	if !strings.Contains(panelText(t, res), "result store") {
		t.Fatalf("unexpected error text: %s", panelText(t, res))
	}
}

func TestPostResultTool_SessionNotFound(t *testing.T) {
	s := &Server{
		scheduleResultStore: newTestScheduleResultStore(t),
		resultSessionLookup: func(string) (*session.Session, bool) { return nil, false },
	}
	res, err := s.handlePostResult(context.Background(), resultRequest(map[string]any{
		"session_id": "ffff",
		"title":      "Build succeeded",
	}))
	if err != nil {
		t.Fatalf("handlePostResult: %v", err)
	}
	if !res.IsError || !strings.Contains(panelText(t, res), "not found") {
		t.Fatalf("expected a session-not-found error, got: %s", panelText(t, res))
	}
}

func TestScheduleResultsListTool_StoreUnavailable(t *testing.T) {
	s := &Server{} // store not attached
	res, err := s.handleScheduleResultsList(context.Background(), resultRequest(nil))
	if err != nil {
		t.Fatalf("handleScheduleResultsList: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a store-unavailable error, got: %s", panelText(t, res))
	}
}

func TestScheduleResultsListTool_ListAndHistory(t *testing.T) {
	store := newTestScheduleResultStore(t)
	mustAdd := func(schedule, title, status string) {
		t.Helper()
		if err := store.Add(&session.ScheduleResultEntry{
			Payload:      session.ResultPayload{Title: title, Status: status},
			SessionID:    "s" + schedule,
			ScheduleName: schedule,
		}, 20); err != nil {
			t.Fatalf("Add(%s): %v", schedule, err)
		}
	}
	mustAdd("nightly-build", "run 1", "ok")
	mustAdd("nightly-build", "run 2", "warn")
	mustAdd("weekly-report", "weekly 1", "ok")

	s := &Server{scheduleResultStore: store}

	// Grouped view — same shape as GET /api/schedule-results.
	res, err := s.handleScheduleResultsList(context.Background(), resultRequest(nil))
	if err != nil {
		t.Fatalf("handleScheduleResultsList: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got: %s", panelText(t, res))
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(panelText(t, res)), &rows); err != nil {
		t.Fatalf("grouped list is not JSON: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 schedule summaries, got %d", len(rows))
	}
	byName := map[string]map[string]any{}
	for _, r := range rows {
		byName[r["schedule_name"].(string)] = r
	}
	nb, ok := byName["nightly-build"]
	if !ok {
		t.Fatalf("missing nightly-build summary: %v", rows)
	}
	if nb["count"].(float64) != 2 {
		t.Fatalf("expected count=2, got %v", nb["count"])
	}
	statuses, _ := nb["recent_statuses"].([]any)
	if len(statuses) != 2 || statuses[0] != "ok" || statuses[1] != "warn" {
		t.Fatalf("unexpected recent_statuses: %v", nb["recent_statuses"])
	}
	if latest, ok := nb["latest"].(map[string]any); !ok {
		t.Fatalf("latest is not an object: %v", nb["latest"])
	} else if p, ok := latest["payload"].(map[string]any); !ok || p["title"] != "run 2" {
		t.Fatalf("expected latest.payload.title=run 2, got %v", latest)
	}

	// History view — full retained history oldest→newest.
	hres, err := s.handleScheduleResultsList(context.Background(), resultRequest(map[string]any{
		"schedule_name": "nightly-build",
	}))
	if err != nil {
		t.Fatalf("handleScheduleResultsList(history): %v", err)
	}
	if hres.IsError {
		t.Fatalf("expected success, got: %s", panelText(t, hres))
	}
	var hist []map[string]any
	if err := json.Unmarshal([]byte(panelText(t, hres)), &hist); err != nil {
		t.Fatalf("history is not JSON: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(hist))
	}
	if hist[0]["payload"].(map[string]any)["title"] != "run 1" || hist[1]["payload"].(map[string]any)["title"] != "run 2" {
		t.Fatalf("history order/content mismatch: %v", hist)
	}

	// Unknown schedule — same sentinel as the REST history endpoint.
	ures, err := s.handleScheduleResultsList(context.Background(), resultRequest(map[string]any{
		"schedule_name": "does-not-exist",
	}))
	if err != nil {
		t.Fatalf("handleScheduleResultsList(unknown): %v", err)
	}
	if ures.IsError || !strings.Contains(panelText(t, ures), "does-not-exist") {
		t.Fatalf("expected the not-recorded sentinel, got: %s", panelText(t, ures))
	}
}

// Ensure the tool descriptors register with the expected names.
func TestResultPanelToolDescriptors(t *testing.T) {
	s := &Server{}
	if got := s.toolPostResult().Name; got != "post_result" {
		t.Fatalf("tool name = %q", got)
	}
	props := s.toolPostResult().InputSchema.Properties
	if _, ok := props["session_id"]; !ok {
		t.Fatal("post_result should expose session_id")
	}
	if _, ok := props["title"]; !ok {
		t.Fatal("post_result should expose title")
	}
	if got := s.toolScheduleResultsList().Name; got != "schedule_results_list" {
		t.Fatalf("tool name = %q", got)
	}
}
