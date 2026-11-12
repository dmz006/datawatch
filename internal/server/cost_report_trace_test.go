package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/trace"
)

// TestCostReport_TraceUsageAdditive — the trace_usage field appears only
// when a trace store holds model requests, and every other field is
// unchanged.
func TestCostReport_TraceUsageAdditive(t *testing.T) {
	s := bl90Server(t)
	get := func() map[string]json.RawMessage {
		rr := httptest.NewRecorder()
		s.handleCostReport(rr, httptest.NewRequest(http.MethodGet, "/api/cost/report", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	before := get()
	if _, ok := before["trace_usage"]; ok {
		t.Fatal("trace_usage present without a store")
	}

	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	st, err := trace.NewStore(t.TempDir(), trace.Settings{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	s.SetTraceStore(st)
	if _, ok := get()["trace_usage"]; ok {
		t.Fatal("trace_usage present for an empty store")
	}
	_, err = st.Append(trace.Event{SessionID: "x", Backend: "pi", Kind: trace.KindModelRequest, Name: "m",
		TokensIn: 11, TokensOut: 4, Timestamp: now})
	if err != nil {
		t.Fatal(err)
	}
	after := get()
	var u trace.UsageSummary
	if err := json.Unmarshal(after["trace_usage"], &u); err != nil {
		t.Fatal(err)
	}
	if u.Total.ModelRequests != 1 || u.Total.TokensIn != 11 || u.ByBackend["pi"].TokensOut != 4 {
		t.Errorf("usage: %+v", u)
	}
	for k, v := range before {
		if k == "since" || k == "Since" || k == "until" || k == "Until" {
			continue
		}
		if string(after[k]) != string(v) {
			t.Errorf("field %s changed: %s -> %s", k, v, after[k])
		}
	}
}
