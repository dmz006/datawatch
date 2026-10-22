// Tests for the structured result panel REST endpoints:
// POST /api/sessions/{id}/result, GET /api/schedule-results,
// GET /api/schedule-results/{name}.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

// newResultPanelServer builds a Server wired with a fresh schedule result
// store, a session manager, and the admin token (like newFedTestServer).
func newResultPanelServer(t *testing.T) (*Server, *session.ScheduleResultStore) {
	t.Helper()
	s, store, gs := newFedTestServer(t)
	if s == nil || store == nil || gs == nil {
		t.Fatalf("newFedTestServer returned nil components")
	}
	srs, err := session.NewScheduleResultStore(t.TempDir() + "/schedule_results.json")
	if err != nil {
		t.Fatalf("schedule result store: %v", err)
	}
	s.SetScheduleResultStore(srs)
	return s, srs
}

func postResult(t *testing.T, s *Server, path, token, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleSessionSubpath(w, r)
	})).ServeHTTP(rr, req)
	return rr.Code
}

// seedSession creates a session in the manager with the given ids.
func seedSession(t *testing.T, s *Server, id, fullID, scheduleName string) *session.Session {
	t.Helper()
	sess := &session.Session{
		ID:           id,
		FullID:       fullID,
		ScheduleName: scheduleName,
		State:        session.StateRunning,
	}
	if err := s.manager.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession(%s): %v", id, err)
	}
	return sess
}

const resultBody = `{"title": "Nightly sweep", "status": "ok", "summary": "all green", "metrics": {"pass": 5, "fail": 0}}`

func TestSessionResultEndpoint_PostOK(t *testing.T) {
	s, srs := newResultPanelServer(t)
	seedSession(t, s, "aa", "testhost-aa", "nightly")

	code := postResult(t, s, "/api/sessions/testhost-aa/result", "admin-token", resultBody)
	if code != http.StatusOK {
		t.Fatalf("POST code=%d, expected 200", code)
	}

	hist := srs.History("nightly")
	if len(hist) != 1 {
		t.Fatalf("history len=%d, expected 1", len(hist))
	}
	entry := hist[0]
	if entry.Payload.Title != "Nightly sweep" {
		t.Errorf("title = %q", entry.Payload.Title)
	}
	if entry.Payload.Status != "ok" {
		t.Errorf("status = %q", entry.Payload.Status)
	}
	if entry.SessionID != "testhost-aa" {
		t.Errorf("session id = %q", entry.SessionID)
	}
	if entry.ScheduleName != "nightly" {
		t.Errorf("schedule = %q", entry.ScheduleName)
	}

	// Board result must be set regardless of schedule membership.
	board := HookBoardFor("testhost-aa")
	if board == nil || board.Result == nil {
		t.Fatalf("board result not set; board=%+v", board)
	}
	if board.Result.Title != "Nightly sweep" {
		t.Errorf("board title = %q", board.Result.Title)
	}
}

func TestSessionResultEndpoint_UnknownSession404(t *testing.T) {
	s, _ := newResultPanelServer(t)
	code := postResult(t, s, "/api/sessions/nope/result", "admin-token", resultBody)
	if code != http.StatusNotFound {
		t.Fatalf("code=%d, expected 404", code)
	}
}

func TestSessionResultEndpoint_BadPayload400(t *testing.T) {
	s, _ := newResultPanelServer(t)
	seedSession(t, s, "bb", "testhost-bb", "nightly")

	cases := []string{
		`{"summary": "no title"}`,                 // missing required title
		`{"title": "x", "status": "bogus"}`,       // invalid status
		`{"title": "", "summary": "empty title"}`, // empty title
		`not-json`, // not JSON
	}
	for _, body := range cases {
		code := postResult(t, s, "/api/sessions/testhost-bb/result", "admin-token", body)
		if code != http.StatusBadRequest {
			t.Errorf("body=%q: code=%d, expected 400", body, code)
		}
	}
}

func TestSessionResultEndpoint_SessionTokenMustMatchOwnSession(t *testing.T) {
	s, _ := newResultPanelServer(t)
	seedSession(t, s, "cc", "testhost-cc", "sched-cc")
	seedSession(t, s, "dd", "testhost-dd", "sched-dd")

	st, err := auth.NewSessionTokenStore(t.TempDir() + "/toks")
	if err != nil {
		t.Fatalf("token store: %v", err)
	}
	// cc's session credential carries the result-write capability.
	tokCC, err := st.Mint("testhost-cc", []string{federation.CapSessionsResultWrite, federation.CapAutonomousRead})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	s.SetSessionTokenStore(st)

	// Posting to its OWN session: allowed.
	code := postResult(t, s, "/api/sessions/testhost-cc/result", tokCC, resultBody)
	if code != http.StatusOK {
		t.Fatalf("own-session post code=%d, expected 200", code)
	}
	// Posting to ANOTHER session: forbidden.
	code = postResult(t, s, "/api/sessions/testhost-dd/result", tokCC, resultBody)
	if code != http.StatusForbidden {
		t.Fatalf("cross-session post code=%d, expected 403", code)
	}
}

func TestSessionResultEndpoint_NoScheduleNameStillBoards(t *testing.T) {
	s, srs := newResultPanelServer(t)
	seedSession(t, s, "ee", "testhost-ee", "") // unscheduled session

	code := postResult(t, s, "/api/sessions/testhost-ee/result", "admin-token", resultBody)
	if code != http.StatusOK {
		t.Fatalf("code=%d, expected 200", code)
	}
	if srs.Count() != 0 {
		t.Errorf("expected 0 schedules stored, got %d", srs.Count())
	}
	board := HookBoardFor("testhost-ee")
	if board == nil || board.Result == nil || board.Result.Title != "Nightly sweep" {
		t.Errorf("board result missing: %+v", board)
	}
}

func TestScheduleResultsEndpoint_ListAndHistory(t *testing.T) {
	s, _ := newResultPanelServer(t)
	seedSession(t, s, "ff", "testhost-ff", "sched-a")
	seedSession(t, s, "gg", "testhost-gg", "sched-b")

	postResult(t, s, "/api/sessions/testhost-ff/result", "admin-token", `{"title": "A1", "status": "ok"}`)
	postResult(t, s, "/api/sessions/testhost-ff/result", "admin-token", `{"title": "A2", "status": "warn"}`)
	postResult(t, s, "/api/sessions/testhost-gg/result", "admin-token", `{"title": "B1", "status": "error"}`)

	// GET /api/schedule-results — grouped, name-sorted.
	req := httptest.NewRequest(http.MethodGet, "/api/schedule-results", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleScheduleResultsList)).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list code=%d body=%s", rr.Code, rr.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len=%d, expected 2: %v", len(list), list)
	}
	if list[0]["schedule_name"] != "sched-a" || list[1]["schedule_name"] != "sched-b" {
		t.Errorf("sort order wrong: %v", list)
	}
	if list[0]["count"] != float64(2) {
		t.Errorf("sched-a count=%v, expected 2", list[0]["count"])
	}
	statuses, _ := list[0]["recent_statuses"].([]any)
	if len(statuses) != 2 || statuses[0] != "ok" || statuses[1] != "warn" {
		t.Errorf("sched-a recent_statuses=%v", statuses)
	}
	latest, _ := list[0]["latest"].(map[string]any)
	if latest["payload"].(map[string]any)["title"] != "A2" {
		t.Errorf("sched-a latest=%v", latest)
	}

	// GET /api/schedule-results/sched-a — full history, oldest first.
	req = httptest.NewRequest(http.MethodGet, "/api/schedule-results/sched-a", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr = httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleScheduleResultsDispatch)).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("history code=%d body=%s", rr.Code, rr.Body.String())
	}
	var hist []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &hist); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("history len=%d, expected 2", len(hist))
	}
	if hist[0]["payload"].(map[string]any)["title"] != "A1" || hist[1]["payload"].(map[string]any)["title"] != "A2" {
		t.Errorf("history order wrong: %v", hist)
	}

	// Unknown schedule → 404.
	req = httptest.NewRequest(http.MethodGet, "/api/schedule-results/nope", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr = httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleScheduleResultsDispatch)).ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown schedule code=%d, expected 404", rr.Code)
	}
}

func TestScheduleResultsEndpoint_FedPeerGating(t *testing.T) {
	s, _ := newResultPanelServer(t)
	seedSession(t, s, "hh", "testhost-hh", "sched-h")
	postResult(t, s, "/api/sessions/testhost-hh/result", "admin-token", `{"title": "H1", "status": "ok"}`)

	// A peer whose caps do NOT include sessions:result_write cannot post.
	if err := s.serverStore.Add(&multiserver.Entry{
		Name:         "peer-nocap",
		URL:          "http://peer-nocap:8080",
		Token:        "peer-nocap-token",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"federation-peer"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/testhost-hh/result", strings.NewReader(resultBody))
	req.Header.Set("Authorization", "Bearer peer-nocap-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleSessionSubpath)).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("peer post code=%d, expected 403", rr.Code)
	}

	// ... and the same peer cannot read the results panel either.
	rreq := httptest.NewRequest(http.MethodGet, "/api/schedule-results", nil)
	rreq.Header.Set("Authorization", "Bearer peer-nocap-token")
	rrr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleScheduleResultsList)).ServeHTTP(rrr, rreq)
	if rrr.Code != http.StatusForbidden {
		t.Errorf("peer list code=%d, expected 403", rrr.Code)
	}
}
