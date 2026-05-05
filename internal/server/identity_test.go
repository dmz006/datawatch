package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/identity"
)

// helper: create a Server with an identity manager rooted at a temp file.
func newIdentityTestServer(t *testing.T) (*Server, *identity.Manager) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := identity.NewManager(filepath.Join(dir, "identity.yaml"))
	if err != nil {
		t.Fatalf("identity manager: %v", err)
	}
	s := &Server{}
	s.identityMgr = IdentityManagerAdapter{M: mgr}
	return s, mgr
}

func TestHandleIdentityGetEmpty(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/identity", nil)
	w := httptest.NewRecorder()
	s.handleIdentity(w, req)
	if w.Code != 200 {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	var got identity.Identity
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.IsEmpty() {
		t.Errorf("expected empty: %+v", got)
	}
}

func TestHandleIdentityPutThenGet(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	body, _ := json.Marshal(identity.Identity{
		Role:           "platform engineer",
		NorthStarGoals: []string{"reliability"},
		Values:         []string{"clarity", "safety"},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/identity", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleIdentity(w, req)
	if w.Code != 200 {
		t.Fatalf("PUT status: %d body=%s", w.Code, w.Body.String())
	}
	// GET back
	req = httptest.NewRequest(http.MethodGet, "/api/identity", nil)
	w = httptest.NewRecorder()
	s.handleIdentity(w, req)
	if w.Code != 200 {
		t.Fatalf("GET status: %d", w.Code)
	}
	var got identity.Identity
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Role != "platform engineer" {
		t.Errorf("role: %q", got.Role)
	}
	if len(got.Values) != 2 {
		t.Errorf("values: %v", got.Values)
	}
}

func TestHandleIdentityPatchPreservesOmitted(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	// seed via PUT
	body, _ := json.Marshal(identity.Identity{Role: "engineer", CurrentFocus: "v6"})
	req := httptest.NewRequest(http.MethodPut, "/api/identity", bytes.NewReader(body))
	s.handleIdentity(httptest.NewRecorder(), req)
	// PATCH only values
	patch, _ := json.Marshal(map[string]any{"values": []string{"X"}})
	req = httptest.NewRequest(http.MethodPatch, "/api/identity", bytes.NewReader(patch))
	w := httptest.NewRecorder()
	s.handleIdentity(w, req)
	if w.Code != 200 {
		t.Fatalf("PATCH status: %d body=%s", w.Code, w.Body.String())
	}
	var got identity.Identity
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Role != "engineer" {
		t.Errorf("role lost: %q", got.Role)
	}
	if got.CurrentFocus != "v6" {
		t.Errorf("focus lost: %q", got.CurrentFocus)
	}
	if len(got.Values) != 1 {
		t.Errorf("values not merged: %v", got.Values)
	}
}

func TestHandleIdentityDisabled(t *testing.T) {
	s := &Server{} // identityMgr nil
	req := httptest.NewRequest(http.MethodGet, "/api/identity", nil)
	w := httptest.NewRecorder()
	s.handleIdentity(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status: %d (want 503)", w.Code)
	}
}

func TestHandleIdentityMethodNotAllowed(t *testing.T) {
	s, _ := newIdentityTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/identity", nil)
	w := httptest.NewRecorder()
	s.handleIdentity(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: %d (want 405)", w.Code)
	}
}
