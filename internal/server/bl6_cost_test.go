// BL6 — cost REST tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func TestBL6_CostSummary_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/cost", nil)
	rr := httptest.NewRecorder()
	s.handleCostSummary(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestBL6_CostSummary_AggregatesAcrossSessions(t *testing.T) {
	s := bl90Server(t)
	_ = s.manager.SaveSession(&session.Session{
		ID: "aa", FullID: "h-aa", LLMBackend: "claude-code",
		TokensIn: 1000, TokensOut: 500, EstCostUSD: 0.011,
	})
	_ = s.manager.SaveSession(&session.Session{
		ID: "bb", FullID: "h-bb", LLMBackend: "ollama",
		TokensIn: 5000, TokensOut: 5000, EstCostUSD: 0,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/cost", nil)
	rr := httptest.NewRecorder()
	s.handleCostSummary(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got session.CostSummary
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Sessions != 2 || got.TotalIn != 6000 {
		t.Errorf("aggregate wrong: %+v", got)
	}
}

func TestBL6_CostSummary_PerSession(t *testing.T) {
	s := bl90Server(t)
	_ = s.manager.SaveSession(&session.Session{
		ID: "aa", FullID: "h-aa", LLMBackend: "claude-code",
		TokensIn: 1000, TokensOut: 500, EstCostUSD: 0.011,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/cost?session=h-aa", nil)
	rr := httptest.NewRecorder()
	s.handleCostSummary(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["session_id"] != "aa" {
		t.Errorf("wrong session: %+v", got)
	}
}

func TestBL6_CostUsage_AccumulatesOnSession(t *testing.T) {
	s := bl90Server(t)
	_ = s.manager.SaveSession(&session.Session{
		ID: "aa", FullID: "h-aa", LLMBackend: "claude-code",
	})
	body := `{"session":"h-aa","tokens_in":2000,"tokens_out":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/cost/usage", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleCostUsage(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, _ := s.manager.GetSession("h-aa")
	if got.TokensIn != 2000 || got.TokensOut != 1000 {
		t.Errorf("not accumulated: %+v", got)
	}
	if got.EstCostUSD <= 0 {
		t.Errorf("expected positive USD, got %v", got.EstCostUSD)
	}
}

func TestBL6_CostUsage_RequiresSession(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/cost/usage", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	s.handleCostUsage(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}
