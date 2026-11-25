// session.finished_idle_close_minutes is off by default, is reported by
// GET /api/config, is set live by PUT /api/config (0 turns it off again) and
// ignores a negative value.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigFinishedIdleCloseMinutes(t *testing.T) {
	s := newFinaliseIntervalServer(t)
	get := func() int {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		var got struct {
			Session struct {
				Minutes *int `json:"finished_idle_close_minutes"`
			} `json:"session"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Session.Minutes == nil {
			t.Fatal("GET /api/config does not report session.finished_idle_close_minutes")
		}
		return *got.Session.Minutes
	}
	put := func(v int) {
		t.Helper()
		if rr := finaliseIntervalPut(s, map[string]any{"session.finished_idle_close_minutes": v}); rr.Code != http.StatusOK {
			t.Fatalf("PUT %d: %d %s", v, rr.Code, rr.Body.String())
		}
	}
	if got := get(); got != 0 {
		t.Fatalf("default = %d, want 0 (off)", got)
	}
	for _, tc := range []struct{ put, want int }{{12, 12}, {-3, 12}, {0, 0}} {
		put(tc.put)
		if got := get(); got != tc.want || s.cfg.Session.FinishedIdleCloseMinutes != tc.want {
			t.Errorf("after PUT %d: GET=%d live=%d, want %d", tc.put, got, s.cfg.Session.FinishedIdleCloseMinutes, tc.want)
		}
	}
}
