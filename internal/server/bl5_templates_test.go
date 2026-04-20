// BL5 — session template CRUD tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBL5_Templates_EmptyList(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
	rr := httptest.NewRecorder()
	s.handleTemplates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Errorf("expected []: got %s", rr.Body.String())
	}
}

func TestBL5_Templates_UpsertGetDelete(t *testing.T) {
	s := bl90Server(t)
	body := `{"name":"web","backend":"claude-code","effort":"thorough","description":"frontend dev"}`
	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleTemplates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/templates/web", nil)
	s.handleTemplates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["effort"] != "thorough" {
		t.Errorf("effort mismatch: %+v", got)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/templates/web", nil)
	s.handleTemplates(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete: %d", rr.Code)
	}
}

func TestBL5_Templates_NameRequired(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	s.handleTemplates(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing name, got %d", rr.Code)
	}
}
