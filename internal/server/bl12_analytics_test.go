// BL12 — analytics REST handler tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func TestBL12_Analytics_DefaultRange(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/analytics", nil)
	rr := httptest.NewRecorder()
	s.handleAnalytics(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["range_days"] == nil {
		t.Errorf("missing range_days: %+v", got)
	}
	if got["buckets"] == nil {
		t.Errorf("missing buckets: %+v", got)
	}
}

func TestBL12_Analytics_30DayRange(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/analytics?range=30d", nil)
	rr := httptest.NewRecorder()
	s.handleAnalytics(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got["range_days"] != float64(30) {
		t.Errorf("range_days=%v want 30", got["range_days"])
	}
}

func TestBL12_Analytics_RecognizesSeededSession(t *testing.T) {
	s := bl90Server(t)
	now := time.Now().UTC()
	_ = s.manager.SaveSession(&session.Session{
		ID: "aa", FullID: "testhost-aa",
		State:     session.StateComplete,
		CreatedAt: now.Add(-30 * time.Minute), UpdatedAt: now,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics?range=7d", nil)
	rr := httptest.NewRecorder()
	s.handleAnalytics(rr, req)
	var got struct {
		Buckets     []map[string]any `json:"buckets"`
		SuccessRate float64          `json:"success_rate"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	total := 0
	for _, b := range got.Buckets {
		total += int(b["session_count"].(float64))
	}
	if total != 1 {
		t.Errorf("expected 1 session in buckets, got %d (buckets=%+v)", total, got.Buckets)
	}
	if got.SuccessRate != 1.0 {
		t.Errorf("success_rate=%v want 1.0", got.SuccessRate)
	}
}

func TestBL12_Analytics_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/analytics", nil)
	rr := httptest.NewRecorder()
	s.handleAnalytics(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestBL12_ParseRangeDays(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 7}, {"7d", 7}, {"30d", 30}, {"junk", 7}, {"500d", 365}, {"-3d", 7},
	}
	for _, c := range cases {
		if got := parseRangeDays(c.in); got != c.want {
			t.Errorf("%q → %d, want %d", c.in, got, c.want)
		}
	}
}
