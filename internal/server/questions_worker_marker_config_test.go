// autonomous.questions.worker_marker — Configuration Accessibility
// round trip: the switch defaults off, an explicit true and false survive a
// REST PUT, come back out of GET /api/config, and reach the config file; a
// non-boolean value is a 400 that applies nothing.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func getQuestionsWorkerMarker(t *testing.T, s *Server) bool {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["questions"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks questions")
	}
	var q map[string]json.RawMessage
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatalf("decode questions: %v", err)
	}
	v, ok := q["worker_marker"]
	if !ok {
		t.Fatal("GET /api/config questions block lacks worker_marker")
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		t.Fatalf("decode worker_marker %s: %v", v, err)
	}
	return b
}

func TestQuestionsWorkerMarkerConfig_RESTGetDefaultsOff(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if getQuestionsWorkerMarker(t, s) {
		t.Fatal("GET default worker_marker = true, want false")
	}
	if s.cfg.Autonomous.Questions.WorkerMarker != nil {
		t.Fatalf("stored worker_marker must be absent (nil) by default, got %v", *s.cfg.Autonomous.Questions.WorkerMarker)
	}
}

func TestQuestionsWorkerMarkerConfig_RESTPutThenGetRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)
	const key = "autonomous.questions.worker_marker"

	for _, val := range []bool{true, false} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{key: val})
		if b := s.cfg.Autonomous.Questions.WorkerMarker; b == nil || *b != val {
			t.Fatalf("stored worker_marker = %v, want explicit %v", b, val)
		}
		if got := getQuestionsWorkerMarker(t, s); got != val {
			t.Fatalf("GET after PUT %v = %v", val, got)
		}
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("read config.yaml: %v", err)
		}
		want := "worker_marker: false"
		if val {
			want = "worker_marker: true"
		}
		if !strings.Contains(string(raw), want) {
			t.Fatalf("config.yaml lacks %q:\n%s", want, raw)
		}
	}
}

func TestQuestionsWorkerMarkerConfig_RESTNonBooleanRejected(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	const key = "autonomous.questions.worker_marker"
	for _, bad := range []string{`"yes"`, `"true"`, `1`, `"1"`, `null`} {
		body := `{"` + key + `": ` + bad + `}`
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s: want 400, got %d (%s)", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "boolean") {
			t.Errorf("400 for %s should say a boolean is required; got %q", bad, rr.Body.String())
		}
		if s.cfg.Autonomous.Questions.WorkerMarker != nil {
			t.Fatalf("a rejected value was stored: %v", *s.cfg.Autonomous.Questions.WorkerMarker)
		}
		if getQuestionsWorkerMarker(t, s) {
			t.Fatalf("GET after rejected PUT %s = true, want false", bad)
		}
	}
}
