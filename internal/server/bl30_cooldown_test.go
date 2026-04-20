// BL30 — cooldown REST tests.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBL30_Cooldown_GetEmpty(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/cooldown", nil)
	rr := httptest.NewRecorder()
	s.handleCooldown(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestBL30_Cooldown_PostThenClear(t *testing.T) {
	s := bl90Server(t)
	body := fmt.Sprintf(`{"until_unix_ms":%d,"reason":"test"}`,
		time.Now().Add(30*time.Second).UnixMilli())
	req := httptest.NewRequest(http.MethodPost, "/api/cooldown", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleCooldown(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("post: %d %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["active"] != true {
		t.Errorf("expected active=true, got %+v", got)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/cooldown", nil)
	s.handleCooldown(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rr.Code)
	}
}

func TestBL30_Cooldown_RejectPastTime(t *testing.T) {
	s := bl90Server(t)
	body := fmt.Sprintf(`{"until_unix_ms":%d}`, time.Now().Add(-time.Hour).UnixMilli())
	req := httptest.NewRequest(http.MethodPost, "/api/cooldown", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleCooldown(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for past time, got %d", rr.Code)
	}
}
