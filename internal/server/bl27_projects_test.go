// BL27 — projects CRUD tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBL27_Projects_EmptyList(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rr := httptest.NewRecorder()
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Errorf("expected empty array, got %s", rr.Body.String())
	}
}

func TestBL27_Projects_UpsertGetDelete(t *testing.T) {
	s := bl90Server(t)
	body := `{"name":"foo","dir":"/home/op/foo","default_backend":"claude-code"}`
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Get
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/projects/foo", nil)
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["dir"] != "/home/op/foo" {
		t.Errorf("dir mismatch: %+v", got)
	}

	// Delete
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/projects/foo", nil)
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Get again → 404
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/projects/foo", nil)
	s.handleProjects(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("post-delete get want 404, got %d", rr.Code)
	}
}

func TestBL27_Projects_RequiresAbsoluteDir(t *testing.T) {
	s := bl90Server(t)
	body := `{"name":"x","dir":"relative/x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleProjects(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for relative dir, got %d", rr.Code)
	}
}

func TestBL27_Projects_MissingNameOrDir(t *testing.T) {
	s := bl90Server(t)
	body := `{"name":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleProjects(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for empty name, got %d", rr.Code)
	}
}
