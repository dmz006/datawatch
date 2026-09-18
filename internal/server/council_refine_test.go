// Tests for POST /api/council/personas/refine-step (GH#159).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCouncilRefine_RejectsGet(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/council/personas/refine-step", nil)
	rr := httptest.NewRecorder()
	s.handleCouncilPersonaRefineStep(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestCouncilRefine_MissingFields(t *testing.T) {
	s := bl90Server(t)
	cases := []struct {
		body    string
		wantMsg string
	}{
		{`{"step":"focus","instruction":"make it shorter"}`, "current_answer required"},
		{`{"step":"focus","current_answer":"something"}`, "instruction required"},
		{`{"step":"bad","current_answer":"x","instruction":"y"}`, "unknown step"},
		{`{"current_answer":"x","instruction":"y"}`, "unknown step"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/council/personas/refine-step",
			strings.NewReader(c.body))
		rr := httptest.NewRecorder()
		s.handleCouncilPersonaRefineStep(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body=%s: want 400, got %d body=%s", c.body, rr.Code, rr.Body.String())
		}
		var resp map[string]string
		_ = json.NewDecoder(rr.Body).Decode(&resp)
		if !strings.Contains(resp["error"], c.wantMsg) {
			t.Errorf("body=%s: want error containing %q, got %q", c.body, c.wantMsg, resp["error"])
		}
	}
}

func TestCouncilRefine_NoLLM_503(t *testing.T) {
	s := bl90Server(t) // inferenceDisp is nil
	req := httptest.NewRequest(http.MethodPost, "/api/council/personas/refine-step",
		strings.NewReader(`{"step":"focus","current_answer":"AI ethics","instruction":"make it more concise"}`))
	rr := httptest.NewRecorder()
	s.handleCouncilPersonaRefineStep(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 when no dispatcher, got %d body=%s", rr.Code, rr.Body.String())
	}
}
