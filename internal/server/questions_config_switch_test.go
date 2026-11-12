// autonomous.questions.enabled — Configuration Accessibility round-trip for
// the switch that gates the /api/questions endpoints, mirroring the
// gate_disagreement surfaces: GET /api/config must show it (off by
// default), a PUT true round-trips into the stored config and back out,
// a PUT back to false closes it again, and a non-boolean value is a 400
// that applies nothing (no restart needed for any of it).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func getQuestionsEnabled(t *testing.T, s *Server) bool {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["questions"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks questions")
	}
	var q struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatalf("decode questions: %v", err)
	}
	return q.Enabled
}

func TestQuestionsConfig_GetConfigDefaultIsOff(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if got := getQuestionsEnabled(t, s); got {
		t.Fatal("GET /api/config autonomous.questions.enabled = true, want false by default")
	}
	if got := s.cfg.Autonomous.Questions.Enabled; got {
		t.Fatal("stored autonomous.questions.enabled = true, want false by default")
	}
}

func TestQuestionsConfig_PutThenGetRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.enabled": true})
	if !s.cfg.Autonomous.Questions.Enabled {
		t.Fatal("stored autonomous.questions.enabled = false, want true after PUT true")
	}
	if got := getQuestionsEnabled(t, s); !got {
		t.Fatal("GET after PUT true = false, want true")
	}

	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.enabled": false})
	if s.cfg.Autonomous.Questions.Enabled {
		t.Fatal("stored autonomous.questions.enabled = true, want false after PUT false")
	}
	if got := getQuestionsEnabled(t, s); got {
		t.Fatal("GET after PUT false = true, want false (an explicit false was elided)")
	}

	// The explicit value must have been written to the config file, not
	// elided on marshal.
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	yml := string(raw)
	if !strings.Contains(yml, "questions:") || !strings.Contains(yml, "enabled: false") {
		t.Fatalf("config.yaml lacks the explicit questions switch (enabled: false):\n%s", yml)
	}
}

func TestQuestionsConfig_NonBooleanRejected(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	for _, bad := range []string{`"yes"`, `"true"`, `1`, `"1"`, `null`} {
		body := `{"autonomous.questions.enabled": ` + bad + `}`
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s: want 400, got %d (%s)", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "boolean") {
			t.Errorf("400 message should say a boolean is required; got %q", rr.Body.String())
		}
	}
	if got := s.cfg.Autonomous.Questions.Enabled; got {
		t.Fatal("a rejected value must not be applied")
	}
	if got := getQuestionsEnabled(t, s); got {
		t.Fatal("GET after rejected PUT = true, want false")
	}
}
