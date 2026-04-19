// BL37 — diagnose endpoint tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBL37_Diagnose_OK(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/diagnose", nil)
	rr := httptest.NewRecorder()
	s.handleDiagnose(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got DiagnoseResult
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Checks) == 0 {
		t.Errorf("no checks in result: %+v", got)
	}
	// At least session_manager check should appear.
	found := false
	for _, c := range got.Checks {
		if c.Name == "session_manager" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing session_manager check: %+v", got.Checks)
	}
}

func TestBL37_Diagnose_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/diagnose", nil)
	rr := httptest.NewRecorder()
	s.handleDiagnose(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestBL37_AllOK_EmptyTrue(t *testing.T) {
	if !allOK(nil) {
		t.Error("empty checks should be ok")
	}
}

func TestBL37_AllOK_OneFailFails(t *testing.T) {
	cs := []DiagnoseCheck{
		{Name: "a", OK: true},
		{Name: "b", OK: false, Detail: "boom"},
	}
	if allOK(cs) {
		t.Error("one failure should fail composite")
	}
}

func TestBL37_DiagnoseResult_JSONShape(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/diagnose", nil)
	rr := httptest.NewRecorder()
	s.handleDiagnose(rr, req)
	body := rr.Body.String()
	for _, want := range []string{`"hostname"`, `"version"`, `"checks"`, `"timestamp"`, `"ok"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %s: %s", want, body)
		}
	}
}
