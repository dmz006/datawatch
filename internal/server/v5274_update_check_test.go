// v5.27.4 — tests for the read-only GET /api/update/check endpoint
// (datawatch#25). Pairs with handleUpdate (POST /api/update) which
// stays untouched and continues to check + install atomically.
//
// The new endpoint exists so mobile + PWA clients can implement a
// "check → confirm → install" UX without firing the install on the
// first call.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleUpdateCheck_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	s.latestVersion = func() (string, error) { return Version, nil }

	req := httptest.NewRequest(http.MethodPost, "/api/update/check", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateCheck(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want 405; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleUpdateCheck_NoLatestFnReturns501(t *testing.T) {
	s := bl90Server(t)
	// latestVersion intentionally nil — daemon was started without
	// the update funcs wired (e.g. air-gapped deployment).

	req := httptest.NewRequest(http.MethodGet, "/api/update/check", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateCheck(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d want 501; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleUpdateCheck_UpToDate(t *testing.T) {
	s := bl90Server(t)
	s.latestVersion = func() (string, error) { return Version, nil }

	req := httptest.NewRequest(http.MethodGet, "/api/update/check", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "up_to_date" {
		t.Errorf("status=%q want up_to_date", got["status"])
	}
	if got["current_version"] != Version {
		t.Errorf("current_version=%q want %q", got["current_version"], Version)
	}
	if got["latest_version"] != Version {
		t.Errorf("latest_version=%q want %q", got["latest_version"], Version)
	}
}

func TestHandleUpdateCheck_UpdateAvailable(t *testing.T) {
	s := bl90Server(t)
	s.latestVersion = func() (string, error) { return "99.99.99", nil }

	req := httptest.NewRequest(http.MethodGet, "/api/update/check", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["status"] != "update_available" {
		t.Errorf("status=%q want update_available", got["status"])
	}
	if got["latest_version"] != "99.99.99" {
		t.Errorf("latest_version=%q want 99.99.99", got["latest_version"])
	}
	if got["current_version"] != Version {
		t.Errorf("current_version=%q want %q", got["current_version"], Version)
	}
}

func TestHandleUpdateCheck_LatestFnError(t *testing.T) {
	s := bl90Server(t)
	s.latestVersion = func() (string, error) { return "", errors.New("github rate limited") }

	req := httptest.NewRequest(http.MethodGet, "/api/update/check", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateCheck(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500; body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleUpdateCheck_NoSideEffects(t *testing.T) {
	// The whole point of the endpoint: must NOT call installUpdate
	// even when an update is available. Wire installUpdate to a fn
	// that fails the test if invoked.
	s := bl90Server(t)
	s.latestVersion = func() (string, error) { return "99.99.99", nil }
	s.installUpdate = func(_ string, _ func(int64, int64)) error {
		t.Fatal("installUpdate must not be called from /api/update/check")
		return nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/update/check", nil)
	rr := httptest.NewRecorder()
	s.handleUpdateCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rr.Code)
	}
}
