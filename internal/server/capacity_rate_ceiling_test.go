package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

type rateCapBody struct {
	Pools []struct {
		Name              string    `json:"name"`
		RateRequests      int       `json:"rate_requests"`
		RequestsInWindow  int       `json:"requests_in_window"`
		RateWindowSeconds int64     `json:"rate_window_seconds"`
		RateResetsAt      time.Time `json:"rate_resets_at"`
	} `json:"pools"`
	Waiting []struct {
		Holder   string     `json:"holder"`
		Reason   string     `json:"reason"`
		Cause    string     `json:"cause"`
		ResetsAt *time.Time `json:"resets_at"`
	} `json:"waiting"`
}

func getCapacityBody(t *testing.T, s *Server) (rateCapBody, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var b rateCapBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return b, rec.Body.String()
}

func TestRateCeiling_CapacityJSONShowsCauseReasonAndResetsAt(t *testing.T) {
	led := capacity.New(capacity.Options{PollInterval: 20 * time.Millisecond})
	led.SetRate("llm:x", capacity.RateCeiling{Requests: 1, Window: 3 * time.Second})
	s := &Server{}
	s.SetCapacity(led)

	req := func(h string) capacity.Request { return capacity.Request{Holder: h, Pools: []string{"llm:x"}} }
	if err := led.Acquire(context.Background(), req("a"), time.Second, func() bool { return false }, nil); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	led.Release("a")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = led.Acquire(ctx, req("b"), 10*time.Second, func() bool { return false }, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(led.Snapshot().Waiting) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	b, _ := getCapacityBody(t, s)
	if len(b.Waiting) != 1 {
		t.Fatalf("waiting = %d", len(b.Waiting))
	}
	w := b.Waiting[0]
	if w.Cause != "rate_ceiling" {
		t.Errorf("cause = %q (reason %q)", w.Cause, w.Reason)
	}
	if w.Reason == "" || w.ResetsAt == nil || !w.ResetsAt.After(time.Now().Add(-time.Second)) {
		t.Errorf("reason=%q resets_at=%v", w.Reason, w.ResetsAt)
	}
	if len(b.Pools) != 1 || b.Pools[0].RateRequests != 1 || b.Pools[0].RequestsInWindow != 1 ||
		b.Pools[0].RateWindowSeconds != 3 || b.Pools[0].RateResetsAt.IsZero() {
		t.Errorf("pool rate fields = %+v", b.Pools)
	}
}

func TestRateCeiling_CapacityJSONPoolWithoutCeilingUnchanged(t *testing.T) {
	led := capacity.New(capacity.Options{})
	led.SetLimit("host", 2)
	s := &Server{}
	s.SetCapacity(led)
	_, raw := getCapacityBody(t, s)
	var generic struct {
		Pools []map[string]any `json:"pools"`
	}
	if err := json.Unmarshal([]byte(raw), &generic); err != nil || len(generic.Pools) != 1 {
		t.Fatalf("decode: %v %s", err, raw)
	}
	for _, k := range []string{"rate_requests", "rate_tokens", "rate_window_seconds", "requests_in_window", "tokens_in_window", "rate_resets_at"} {
		if _, ok := generic.Pools[0][k]; ok {
			t.Errorf("pool without ceiling serialised %q", k)
		}
	}
}
