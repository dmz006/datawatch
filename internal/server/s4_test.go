// Sprint S4 — REST tests for v3.8.0 endpoints.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ----- BL69 splash --------------------------------------------------------

func TestS4_BL69_SplashLogo_404WhenUnset(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/splash/logo", nil)
	rr := httptest.NewRecorder()
	s.handleSplashLogo(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rr.Code)
	}
}

func TestS4_BL69_SplashLogo_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/splash/logo", nil)
	rr := httptest.NewRecorder()
	s.handleSplashLogo(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestS4_BL69_SplashInfo_HasVersion(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/splash/info", nil)
	rr := httptest.NewRecorder()
	s.handleSplashInfo(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["version"] == nil {
		t.Errorf("missing version in: %+v", got)
	}
}

func TestS4_BL69_SplashInfo_LogoURLOnlyWhenConfigured(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/splash/info", nil)
	rr := httptest.NewRecorder()
	s.handleSplashInfo(rr, req)
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if _, ok := got["logo_url"]; ok {
		t.Errorf("logo_url should be absent when SplashLogoPath empty: %+v", got)
	}

	s.cfg.Session.SplashLogoPath = "/path/to/logo.png"
	rr = httptest.NewRecorder()
	s.handleSplashInfo(rr, req)
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["logo_url"] != "/api/splash/logo" {
		t.Errorf("logo_url=%v want /api/splash/logo", got["logo_url"])
	}
}

// ----- BL42 assist --------------------------------------------------------

func TestS4_BL42_Assist_RejectsGet(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/assist", nil)
	rr := httptest.NewRecorder()
	s.handleAssist(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestS4_BL42_Assist_EmptyQuestion(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/assist",
		strings.NewReader(`{"question":""}`))
	rr := httptest.NewRecorder()
	s.handleAssist(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

func TestS4_BL42_Assist_DefaultsBackend(t *testing.T) {
	s := bl90Server(t)
	// No Ollama configured → 500. Verifies the default backend resolution path.
	req := httptest.NewRequest(http.MethodPost, "/api/assist",
		strings.NewReader(`{"question":"hi"}`))
	rr := httptest.NewRecorder()
	s.handleAssist(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("want 500 (no ollama), got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestS4_BL42_Assist_UnsupportedBackend(t *testing.T) {
	s := bl90Server(t)
	s.cfg.Session.AssistantBackend = "junk"
	req := httptest.NewRequest(http.MethodPost, "/api/assist",
		strings.NewReader(`{"question":"hi"}`))
	rr := httptest.NewRecorder()
	s.handleAssist(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for unsupported backend, got %d", rr.Code)
	}
}

// ----- BL31 device aliases ------------------------------------------------

func TestS4_BL31_DeviceAliases_EmptyList(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/device-aliases", nil)
	rr := httptest.NewRecorder()
	s.handleDeviceAliases(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Errorf("expected [], got %s", rr.Body.String())
	}
}

func TestS4_BL31_DeviceAliases_UpsertAndDelete(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/device-aliases",
		strings.NewReader(`{"alias":"prod","server":"prod-host"}`))
	rr := httptest.NewRecorder()
	s.handleDeviceAliases(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/device-aliases", nil)
	s.handleDeviceAliases(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "prod") || !strings.Contains(body, "prod-host") {
		t.Errorf("list missing entry: %s", body)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/device-aliases/prod", nil)
	s.handleDeviceAliases(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete: %d", rr.Code)
	}
}

func TestS4_BL31_DeviceAliases_Validation(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/device-aliases",
		strings.NewReader(`{"alias":""}`))
	rr := httptest.NewRecorder()
	s.handleDeviceAliases(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}
