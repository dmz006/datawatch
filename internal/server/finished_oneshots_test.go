package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// The finished-one-shots endpoint backs the Settings → About cleanup control.

func TestFinishedOneShots_NoManagerIsUnavailable(t *testing.T) {
	s := &Server{token: "admin-token"}
	rr := httptest.NewRecorder()
	s.handleFinishedOneShots(rr, httptest.NewRequest(http.MethodGet, "/api/sessions/finished-oneshots", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rr.Code)
	}
}

func TestFinishedOneShots_ListIsEmptyWhenNothingIsStuck(t *testing.T) {
	sm, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sm.WaitBackground)
	s := &Server{token: "admin-token", manager: sm}
	rr := httptest.NewRecorder()
	s.handleFinishedOneShots(rr, httptest.NewRequest(http.MethodGet, "/api/sessions/finished-oneshots", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	var body struct {
		Count int      `json:"count"`
		IDs   []string `json:"ids"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v: %s", err, rr.Body.String())
	}
	if body.Count != 0 || body.IDs == nil || len(body.IDs) != 0 {
		t.Fatalf("body = %+v, want count 0 and an empty (non-null) ids list", body)
	}
}

func TestFinishedOneShots_RejectsOtherMethods(t *testing.T) {
	sm, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sm.WaitBackground)
	s := &Server{token: "admin-token", manager: sm}
	rr := httptest.NewRecorder()
	s.handleFinishedOneShots(rr, httptest.NewRequest(http.MethodDelete, "/api/sessions/finished-oneshots", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", rr.Code)
	}
}
