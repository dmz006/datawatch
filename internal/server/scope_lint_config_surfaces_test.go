// Pre-spawn scope lint — REST configuration surface: PUT /api/config
// then GET /api/config must round-trip autonomous.scope_lint for each
// mode, the live manager must receive it, and an unknown mode must be
// refused without changing the stored value.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func scopeLintTestServer(t *testing.T) (*Server, *autonomousConfigPropagationSpy) {
	t.Helper()
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}
	return s, spy
}

func scopeLintGet(t *testing.T, s *Server) string {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous map[string]json.RawMessage `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	raw, ok := got.Autonomous["scope_lint"]
	if !ok {
		t.Fatalf("GET /api/config autonomous map lacks scope_lint")
	}
	var mode string
	if err := json.Unmarshal(raw, &mode); err != nil {
		t.Fatalf("decode scope_lint: %v", err)
	}
	return mode
}

func scopeLintPut(t *testing.T, s *Server, mode string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"autonomous.scope_lint": mode})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func TestScopeLintRESTRoundTrip_EachMode(t *testing.T) {
	for _, mode := range []string{config.ScopeLintOff, config.ScopeLintWarn, config.ScopeLintEnforce} {
		t.Run(mode, func(t *testing.T) {
			s, spy := scopeLintTestServer(t)
			if rr := scopeLintPut(t, s, mode); rr.Code != http.StatusOK {
				t.Fatalf("PUT %s: want 200, got %d body=%s", mode, rr.Code, rr.Body.String())
			}
			if got := scopeLintGet(t, s); got != mode {
				t.Fatalf("GET scope_lint = %q after PUT %q", got, mode)
			}
			var fwd map[string]json.RawMessage
			if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
				t.Fatalf("decode propagated payload: %v", err)
			}
			if string(fwd["scope_lint"]) != `"`+mode+`"` {
				t.Errorf("scope_lint not propagated to manager: %s", spy.lastSetConfig)
			}
		})
	}
}

func TestScopeLintRESTRejectsInvalidMode(t *testing.T) {
	s, spy := scopeLintTestServer(t)
	if rr := scopeLintPut(t, s, config.ScopeLintWarn); rr.Code != http.StatusOK {
		t.Fatalf("seed PUT warn: want 200, got %d", rr.Code)
	}
	calls := spy.calls
	for _, bad := range []string{"bogus", "", "Warn", "strict"} {
		rr := scopeLintPut(t, s, bad)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %q: want 400, got %d body=%s", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "scope_lint") {
			t.Errorf("PUT %q: error does not name scope_lint: %s", bad, rr.Body.String())
		}
	}
	if got := scopeLintGet(t, s); got != config.ScopeLintWarn {
		t.Fatalf("invalid PUT changed scope_lint: GET = %q, want %q", got, config.ScopeLintWarn)
	}
	if spy.calls != calls {
		t.Errorf("invalid PUT reached the live manager (%d extra SetConfig calls)", spy.calls-calls)
	}
}

func TestScopeLintRESTDefaultReadsAsEnforce(t *testing.T) {
	s, _ := scopeLintTestServer(t)
	if got := scopeLintGet(t, s); got != config.ScopeLintEnforce {
		t.Fatalf("unset scope_lint GET = %q, want %q", got, config.ScopeLintEnforce)
	}
}
