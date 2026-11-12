package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/inference"
)

func admissionFixture(t *testing.T) (*Server, *capacity.Ledger) {
	t.Helper()
	reg := inference.NewRegistry()
	for _, n := range []string{"x", "y"} {
		if err := reg.Add(&inference.LLM{Name: n, Kind: "ollama"}); err != nil {
			t.Fatalf("add llm %s: %v", n, err)
		}
	}
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	s := &Server{inferenceReg: reg}
	s.SetCapacity(led)
	return s, led
}

func getAdmission(t *testing.T, s *Server, q url.Values) (int, capacity.Decision, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleCapacityAdmission(rec, httptest.NewRequest(http.MethodGet, "/api/capacity/admission?"+q.Encode(), nil))
	var d capacity.Decision
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rec.Code, d, rec.Body.String()
}

func TestCapacityAdmission_Admit(t *testing.T) {
	s, led := admissionFixture(t)
	led.SetLimit("llm:x", 1)
	code, d, body := getAdmission(t, s, url.Values{"backend": {"x"}})
	if code != 200 || d.Kind != capacity.DecisionAdmit || d.QueuePosition != 0 {
		t.Fatalf("want admit, got %d %s", code, body)
	}
}

func TestCapacityAdmission_QueuedWithPosition(t *testing.T) {
	s, led := admissionFixture(t)
	led.SetLimit("llm:x", 1)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "t1", Pools: []string{"llm:x"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	code, d, body := getAdmission(t, s, url.Values{"backend": {"x"}})
	if code != 200 || d.Kind != capacity.DecisionQueued || d.QueuePosition != 1 || d.Reason == "" {
		t.Fatalf("want queued at 1, got %d %s", code, body)
	}
}

func TestCapacityAdmission_AlternateAvailable(t *testing.T) {
	s, led := admissionFixture(t)
	led.SetLimit("llm:x", 1)
	led.SetLimit("llm:y", 2)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "t1", Pools: []string{"llm:x"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	code, d, body := getAdmission(t, s, url.Values{"backend": {"x"}})
	if code != 200 || d.Kind != capacity.DecisionAlternateAvailable || len(d.Alternates) != 1 || d.Alternates[0].Pool != "llm:y" {
		t.Fatalf("want alternate llm:y, got %d %s", code, body)
	}
}

func TestCapacityAdmission_BadInput(t *testing.T) {
	s, _ := admissionFixture(t)
	for name, q := range map[string]url.Values{
		"unknown backend": {"backend": {"nope"}},
		"missing backend": {},
		"bad priority":    {"backend": {"x"}, "priority": {"high"}},
		"bad role":        {"backend": {"x"}, "role": {"boss"}},
	} {
		if code, _, body := getAdmission(t, s, q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d body %q", name, code, body)
		}
	}
	rec := httptest.NewRecorder()
	s.handleCapacityAdmission(rec, httptest.NewRequest(http.MethodPost, "/api/capacity/admission", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST must be 405, got %d", rec.Code)
	}
}

func TestCapacityAdmission_NoStateChange(t *testing.T) {
	s, led := admissionFixture(t)
	led.SetLimit("llm:x", 1)
	if err := led.Acquire(context.Background(), capacity.Request{Holder: "t1", Pools: []string{"llm:x"}}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := led.Snapshot()
	for i := 0; i < 3; i++ {
		getAdmission(t, s, url.Values{"backend": {"x"}, "node": {"n1"}, "model": {"m"}})
	}
	if after := led.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("ledger changed\nbefore %+v\nafter  %+v", before, after)
	}
}
