// ask_on_change (default false: one alert per change, no question; true: at most
// one open question) on the REST surface: PUT true → GET true, a PUT without the
// key keeps the stored value, a non-boolean is rejected with 400 and nothing is
// stored, and the prefixed form round-trips the /api/config apply path.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestBurnGuardAskOnChangeRestRoundTrip(t *testing.T) {
	s := burnGuardConfigServer(t)

	// The default is false.
	rr := doBurnGuard(s, http.MethodGet, "/api/usage/burn-guard/config", "", s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rr.Code, rr.Body.String())
	}
	var got config.BurnGuardConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AskOnChange {
		t.Fatalf("default ask_on_change = true, want false")
	}

	// PUT true (bare key) → stored, and GET reads it back.
	rr = doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", `{"ask_on_change": true}`, s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT ask_on_change=true = %d: %s", rr.Code, rr.Body.String())
	}
	if !s.cfg.Usage.BurnGuard.AskOnChange {
		t.Fatalf("stored ask_on_change = false, want true")
	}
	rr = doBurnGuard(s, http.MethodGet, "/api/usage/burn-guard/config", "", s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET = %d", rr.Code)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.AskOnChange {
		t.Fatalf("GET ask_on_change = false after PUT true; body = %s", rr.Body.String())
	}

	// A PUT without the key keeps the stored value.
	rr = doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", `{"margin_points": 8}`, s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT margin_points = %d: %s", rr.Code, rr.Body.String())
	}
	if !s.cfg.Usage.BurnGuard.AskOnChange {
		t.Fatalf("a PUT without ask_on_change cleared the stored value")
	}

	// The prefixed form goes through the /api/config apply path.
	s.cfg.Usage.BurnGuard.AskOnChange = false
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"usage.burn_guard.ask_on_change": true}`))
	req.Header.Set("Content-Type", "application/json")
	inner := httptest.NewRecorder()
	s.handlePutConfig(inner, req)
	if inner.Code != http.StatusOK {
		t.Fatalf("PUT /api/config ask_on_change = %d: %s", inner.Code, inner.Body.String())
	}
	if !s.cfg.Usage.BurnGuard.AskOnChange {
		t.Fatalf("prefixed ask_on_change was not applied via /api/config")
	}
}

func TestBurnGuardAskOnChangeRestRejectsNonBool(t *testing.T) {
	s := burnGuardConfigServer(t)
	s.cfg.Usage.BurnGuard.AskOnChange = true

	rr := doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", `{"ask_on_change": "maybe"}`, s.handleBurnGuardConfig)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT ask_on_change=\"maybe\" = %d, want 400 (%s)", rr.Code, rr.Body.String())
	}
	msg := rr.Body.String()
	if !strings.Contains(msg, "ask_on_change") || !strings.Contains(strings.ToLower(msg), "true or false") {
		t.Fatalf("400 message = %q", msg)
	}
	if !s.cfg.Usage.BurnGuard.AskOnChange {
		t.Errorf("a rejected PUT changed the stored value")
	}

	// The same rejection on the /api/config path; nothing stored.
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"usage.burn_guard.ask_on_change": 3}`))
	req.Header.Set("Content-Type", "application/json")
	inner := httptest.NewRecorder()
	s.handlePutConfig(inner, req)
	if inner.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/config ask_on_change=3 = %d, want 400 (%s)", inner.Code, inner.Body.String())
	}
	if !s.cfg.Usage.BurnGuard.AskOnChange {
		t.Errorf("a rejected /api/config PUT changed the stored value")
	}
}
