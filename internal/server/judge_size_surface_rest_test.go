// REST surface for autonomous.judge_size_threshold_lines: GET shows the stored
// value (default 400, 0 stays 0), PUT accepts a whole number of 0 or more and
// refuses a negative or fractional value with a 400 that names the key.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func judgeSizeThresholdGet(t *testing.T, s *Server) string {
	t.Helper()
	return string(getAutonomousConfigMap(t, s)["judge_size_threshold_lines"])
}

func TestJudgeSizeSurface_RESTGetDefaultIs400(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if got := judgeSizeThresholdGet(t, s); got != "400" {
		t.Fatalf("GET /api/config autonomous.judge_size_threshold_lines = %q, want 400", got)
	}
}

func TestJudgeSizeSurface_RESTPutZeroThenGetReturnsZero(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.judge_size_threshold_lines": 0})
	if got := judgeSizeThresholdGet(t, s); got != "0" {
		t.Fatalf("GET after PUT 0 = %q, want 0 (zero must not map to the default)", got)
	}
	if s.cfg.Autonomous.JudgeSizeThresholdLines != 0 {
		t.Fatalf("stored value = %d, want 0", s.cfg.Autonomous.JudgeSizeThresholdLines)
	}
}

func TestJudgeSizeSurface_RESTPutValueThenGetReturnsValue(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.judge_size_threshold_lines": 250})
	if got := judgeSizeThresholdGet(t, s); got != "250" {
		t.Fatalf("GET after PUT 250 = %q, want 250", got)
	}
}

func TestJudgeSizeSurface_RESTPutRejectsInvalid(t *testing.T) {
	for _, bad := range []any{-1, -250, 2.5, "400", true} {
		s, _, _ := newMaxConcurrentTasksServer(t)
		b, _ := json.Marshal(map[string]any{"autonomous.judge_size_threshold_lines": bad})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: want 400, got %d body=%s", b, rr.Code, rr.Body.String())
			continue
		}
		if !strings.Contains(rr.Body.String(), "autonomous.judge_size_threshold_lines") {
			t.Errorf("PUT %s: error %q does not name the key", b, rr.Body.String())
		}
		if got := judgeSizeThresholdGet(t, s); got != "400" {
			t.Errorf("PUT %s: rejected value changed the stored threshold to %s", b, got)
		}
	}
}
