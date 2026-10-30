// question_worker_stats_test.go — GET /api/questions/stats: the worker
// question marker's counters and the open marker and blocking questions, read
// with the autonomous read capability, served whether or not the channel is on.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

func questionStatsGET(t *testing.T, s *Server) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleQuestionStats(rec, httptest.NewRequest(http.MethodGet, "/api/questions/stats", nil))
	var body map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v: %s", err, rec.Body.String())
		}
	}
	return rec.Code, body
}

func TestQuestionStatsCountsOpenMarkerAndBlockingQuestions(t *testing.T) {
	s, store := polledServer(t, true)
	on := true
	s.cfg.Autonomous.Questions.WorkerMarker = &on

	base := autonomous.Question{
		Text: "Which store?", Default: "hold", AskedBy: "worker-ab12", AskerKind: autonomous.QuestionAskerSession,
		Context: autonomous.QuestionContextNone, Priority: autonomous.QuestionPriorityNormal,
		Options: []autonomous.QuestionOption{{ID: "mem", Label: "Memory"}, {ID: "disk", Label: "Disk"}},
	}
	marker := base
	marker.Source, marker.SessionID, marker.SourceLineSHA256 = autonomous.QuestionSourceMarker, "host-ab12", strings.Repeat("b", 64)
	blocking := marker
	blocking.Blocking, blocking.SourceLineSHA256 = true, strings.Repeat("a", 64)
	rest := base
	rest.Source = autonomous.QuestionSourceRest
	for _, q := range []autonomous.Question{marker, blocking, rest} {
		if _, err := store.Raise(q); err != nil {
			t.Fatalf("Raise: %v", err)
		}
	}

	code, body := questionStatsGET(t, s)
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	want := map[string]float64{"open_marker_questions": 2, "open_blocking_questions": 1}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
	if body["questions_enabled"] != true || body["worker_marker_enabled"] != true {
		t.Errorf("switches = %v/%v, want both on", body["questions_enabled"], body["worker_marker_enabled"])
	}
	for _, k := range []string{"raised", "duplicate", "refused", "parked", "resumed"} {
		if _, ok := body[k]; !ok {
			t.Errorf("body lacks the %s counter: %v", k, body)
		}
	}
}

func TestQuestionStatsServedWhileChannelIsOff(t *testing.T) {
	s, _ := polledServer(t, false)
	code, body := questionStatsGET(t, s)
	if code != http.StatusOK {
		t.Fatalf("want 200 while the channel is off, got %d", code)
	}
	if body["questions_enabled"] != false || body["worker_marker_enabled"] != false {
		t.Errorf("switches = %v/%v, want both off", body["questions_enabled"], body["worker_marker_enabled"])
	}
	if body["open_marker_questions"] != float64(0) {
		t.Errorf("open_marker_questions = %v, want 0", body["open_marker_questions"])
	}
}

func TestQuestionStatsWithoutConfigOrStore(t *testing.T) {
	code, body := questionStatsGET(t, &Server{})
	if code != http.StatusOK || body["questions_enabled"] != false {
		t.Fatalf("bare server: %d %v, want 200 with the channel off", code, body)
	}
}

func TestQuestionStatsMethodAndCapability(t *testing.T) {
	s, _ := polledServer(t, true)
	rec := httptest.NewRecorder()
	s.handleQuestionStats(rec, httptest.NewRequest(http.MethodPost, "/api/questions/stats", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: want 405, got %d", rec.Code)
	}

	deny := httptest.NewRequest(http.MethodGet, "/api/questions/stats", nil)
	deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
	rec = httptest.NewRecorder()
	s.handleQuestionStats(rec, deny)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body.String())
	}

	allow := httptest.NewRequest(http.MethodGet, "/api/questions/stats", nil)
	allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
	rec = httptest.NewRecorder()
	s.handleQuestionStats(rec, allow)
	if rec.Code != http.StatusOK {
		t.Fatalf("with the read capability: want 200, got %d", rec.Code)
	}
}

// The exact route must reach the stats handler, not be read as a question id.
func TestQuestionStatsRouteIsNotAQuestionID(t *testing.T) {
	mux := http.NewServeMux()
	s, _ := polledServer(t, true)
	mux.HandleFunc("/api/questions", s.handleQuestions)
	mux.HandleFunc("/api/questions/", s.handleQuestions)
	mux.HandleFunc("/api/questions/stats", s.handleQuestionStats)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/questions/stats", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "worker_marker_enabled") {
		t.Fatalf("GET /api/questions/stats: %d %q, want the stats body", rec.Code, rec.Body.String())
	}
}

