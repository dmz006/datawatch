// autonomous.questions notify toggles — Configuration Accessibility
// round-trip for the three toggles that sit beside the question switch
// (notify_messaging, notify_alert, return_path). They all default on
// (absent key), an explicit false must survive a REST PUT and come back
// out of GET /api/config verbatim — and a non-boolean value is a 400
// that applies nothing.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// getQuestionsNotifyToggles decodes the GET /api/config questions block
// and returns the three effective toggle values in order.
func getQuestionsNotifyToggles(t *testing.T, s *Server) (messaging, alert, returnPath bool) {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["questions"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks questions")
	}
	var q struct {
		NotifyMessaging bool `json:"notify_messaging"`
		NotifyAlert     bool `json:"notify_alert"`
		ReturnPath      bool `json:"return_path"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatalf("decode questions: %v", err)
	}
	return q.NotifyMessaging, q.NotifyAlert, q.ReturnPath
}

func TestQuestionNotifyConfig_RESTGetDefaultsOn(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)

	// Absent keys are on: the effective values are true in the GET
	// surface, all three of them, while the switch itself ships off.
	m, a, r := getQuestionsNotifyToggles(t, s)
	if !m || !a || !r {
		t.Fatalf("GET defaults: notify_messaging=%v notify_alert=%v return_path=%v, want all true", m, a, r)
	}
	if s.cfg.Autonomous.Questions.NotifyMessaging != nil ||
		s.cfg.Autonomous.Questions.NotifyAlert != nil ||
		s.cfg.Autonomous.Questions.ReturnPath != nil {
		t.Fatalf("stored toggles must be absent (nil) by default, got %+v", s.cfg.Autonomous.Questions)
	}
}

func TestQuestionNotifyConfig_RESTPutThenGetRoundTrip(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	keys := []string{
		"autonomous.questions.notify_messaging",
		"autonomous.questions.notify_alert",
		"autonomous.questions.return_path",
	}
	vals := map[string]any{}
	for _, k := range keys {
		vals[k] = false
	}
	putJSON(t, s.handlePutConfig, "/api/config", vals)

	// Explicit false applied to the stored config, effective values off.
	if b := s.cfg.Autonomous.Questions.NotifyMessaging; b == nil || *b {
		t.Fatalf("stored notify_messaging = %v, want explicit false", b)
	}
	if b := s.cfg.Autonomous.Questions.NotifyAlert; b == nil || *b {
		t.Fatalf("stored notify_alert = %v, want explicit false", b)
	}
	if b := s.cfg.Autonomous.Questions.ReturnPath; b == nil || *b {
		t.Fatalf("stored return_path = %v, want explicit false", b)
	}
	m, a, r := getQuestionsNotifyToggles(t, s)
	if m || a || r {
		t.Fatalf("GET after PUT false: notify_messaging=%v notify_alert=%v return_path=%v, want all false", m, a, r)
	}

	// The explicit values must have been written to the config file, not
	// elided on marshal.
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	yml := string(raw)
	for _, frag := range []string{"notify_messaging: false", "notify_alert: false", "return_path: false"} {
		if !strings.Contains(yml, frag) {
			t.Fatalf("config.yaml lacks %q:\n%s", frag, yml)
		}
	}

	// Back on: the explicit true also survives and the file carries it.
	vals = map[string]any{}
	for _, k := range keys {
		vals[k] = true
	}
	putJSON(t, s.handlePutConfig, "/api/config", vals)
	if b := s.cfg.Autonomous.Questions.NotifyMessaging; b == nil || !*b {
		t.Fatalf("stored notify_messaging = %v, want explicit true", b)
	}
	m, a, r = getQuestionsNotifyToggles(t, s)
	if !m || !a || !r {
		t.Fatalf("GET after PUT true: notify_messaging=%v notify_alert=%v return_path=%v, want all true", m, a, r)
	}
	raw, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	yml = string(raw)
	for _, frag := range []string{"notify_messaging: true", "notify_alert: true", "return_path: true"} {
		if !strings.Contains(yml, frag) {
			t.Fatalf("config.yaml lacks %q:\n%s", frag, yml)
		}
	}
}

func TestQuestionNotifyConfig_RESTNonBooleanRejected(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	keys := []string{
		"autonomous.questions.notify_messaging",
		"autonomous.questions.notify_alert",
		"autonomous.questions.return_path",
	}
	for _, k := range keys {
		for _, bad := range []string{`"yes"`, `"true"`, `1`, `"1"`, `null`} {
			body := `{"` + k + `": ` + bad + `}`
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT %s %s: want 400, got %d (%s)", k, bad, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "boolean") {
				t.Errorf("400 for %s=%s should say a boolean is required; got %q", k, bad, rr.Body.String())
			}
			// Rejected: nothing applied, the toggle stays absent and the
			// effective value stays on.
			if s.cfg.Autonomous.Questions.NotifyMessaging != nil ||
				s.cfg.Autonomous.Questions.NotifyAlert != nil ||
				s.cfg.Autonomous.Questions.ReturnPath != nil {
				t.Fatalf("a rejected value was stored: %+v", s.cfg.Autonomous.Questions)
			}
			if m, a, r := getQuestionsNotifyToggles(t, s); !m || !a || !r {
				t.Fatalf("GET after rejected PUT %s=%s: %v %v %v, want all true (on)", k, bad, m, a, r)
			}
		}
	}
}
