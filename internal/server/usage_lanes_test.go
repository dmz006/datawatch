package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// laneFakeLedger is a capacity ledger whose Claude pool holds one lease per holder named.
func laneFakeLedger(t *testing.T, holders ...string) *capacity.Ledger {
	t.Helper()
	l := capacity.New(capacity.Options{})
	l.SetLimit("llm:claude", 10)
	l.SetLimit("llm:pi", 10)
	for _, h := range holders {
		req := capacity.Request{Holder: h, PRDID: "automaton-a", Pools: []string{"llm:claude"}}
		if err := l.Acquire(context.Background(), req, 0, nil, nil); err != nil {
			t.Fatalf("acquire %s: %v", h, err)
		}
	}
	// A lease on a non-Claude pool is never counted.
	other := capacity.Request{Holder: "pi-task", PRDID: "automaton-a", Pools: []string{"llm:pi"}}
	if err := l.Acquire(context.Background(), other, 0, nil, nil); err != nil {
		t.Fatalf("acquire pi-task: %v", err)
	}
	return l
}

// laneFakeRegistry is a registry with one Claude Code entry and one that is not.
func laneFakeRegistry(t *testing.T) *inference.Registry {
	t.Helper()
	reg := inference.NewRegistry()
	for _, l := range []*inference.LLM{
		{Name: "claude", Kind: inference.KindClaudeCode},
		{Name: "pi", Kind: inference.KindPiPrompt},
	} {
		if err := reg.Add(l); err != nil {
			t.Fatalf("registry add %s: %v", l.Name, err)
		}
	}
	return reg
}

// postLaneReading posts body to the readings endpoint and returns the stored reading from the
// reply, after checking that a GET of the record holds the same stamp.
func postLaneReading(t *testing.T, s *Server, body string) usagecalc.Reading {
	t.Helper()
	req := withUsageCaps(httptest.NewRequest(http.MethodPost, "/api/usage/readings", nil),
		federation.CapUsageRead, federation.CapUsageWrite)
	req.Body = io.NopCloser(strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleUsageReadings(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Reading *usagecalc.Reading `json:"reading"`
	}
	decodeJSON(t, rr, &resp)
	if resp.Reading == nil {
		t.Fatalf("POST reply has no reading: %s", rr.Body.String())
	}

	get := withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil),
		federation.CapUsageRead)
	grr := httptest.NewRecorder()
	s.handleUsageReadings(grr, get)
	rec := readUsageRecord(t, grr)
	for _, r := range rec.Readings {
		if r.ID != resp.Reading.ID {
			continue
		}
		if r.LanesSource != resp.Reading.LanesSource || (r.Lanes == nil) != (resp.Reading.Lanes == nil) ||
			(r.Lanes != nil && *r.Lanes != *resp.Reading.Lanes) {
			t.Fatalf("stored reading %+v differs from the reply %+v", r, *resp.Reading)
		}
		return *resp.Reading
	}
	t.Fatalf("reading %s is not in the stored record", resp.Reading.ID)
	return usagecalc.Reading{}
}

func TestUsageLaneStampIgnoresCallerLanes(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")
	s.inferenceReg = laneFakeRegistry(t)
	s.SetCapacity(laneFakeLedger(t, "task-a"))

	got := postLaneReading(t, s, `{"percent":40,"source":"typed","lanes":2}`)
	if got.Lanes == nil || *got.Lanes != 1 || got.LanesSource != usagecalc.LanesLive {
		t.Fatalf("lanes 2 posted with one Claude lease: stored lanes=%v source=%q, want 1 live", got.Lanes, got.LanesSource)
	}

	s.SetCapacity(laneFakeLedger(t, "task-a", "task-b"))
	got = postLaneReading(t, s, `{"percent":41,"source":"typed","lanes":0}`)
	if got.Lanes == nil || *got.Lanes != 2 || got.LanesSource != usagecalc.LanesLive {
		t.Fatalf("lanes 0 posted with two Claude leases: stored lanes=%v source=%q, want 2 live", got.Lanes, got.LanesSource)
	}
}

func TestUsageLaneStampUnavailableWithoutLedger(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")
	s.inferenceReg = laneFakeRegistry(t) // a registry alone is not enough

	got := postLaneReading(t, s, `{"percent":40,"source":"typed","lanes":3}`)
	if got.Lanes != nil || got.LanesSource != usagecalc.LanesUnavailable {
		t.Fatalf("nil ledger: stored lanes=%v source=%q, want no lanes and %q", got.Lanes, got.LanesSource, usagecalc.LanesUnavailable)
	}

	s = newUsageTestServer(t, t.TempDir(), "", "", "", "")
	s.SetCapacity(laneFakeLedger(t, "task-a")) // a ledger alone is not enough
	got = postLaneReading(t, s, `{"percent":40,"source":"typed"}`)
	if got.Lanes != nil || got.LanesSource != usagecalc.LanesUnavailable {
		t.Fatalf("nil registry: stored lanes=%v source=%q, want no lanes and %q", got.Lanes, got.LanesSource, usagecalc.LanesUnavailable)
	}
}

type laneFakeCounter int

func (c laneFakeCounter) Lanes() (int, bool) { return int(c), true }

func TestUsageLaneStampRecordedFromHistory(t *testing.T) {
	s := newUsageTestServer(t, t.TempDir(), "", "", "", "")
	s.inferenceReg = laneFakeRegistry(t)
	s.SetCapacity(laneFakeLedger(t, "task-a")) // one Claude lease now

	// The history saw two lanes ten minutes ago.
	start := time.Now().UTC().Add(-10 * time.Minute)
	h, err := usagecalc.OpenLaneHistory(t.TempDir(), func() time.Time { return start }, laneFakeCounter(2))
	if err != nil {
		t.Fatalf("OpenLaneHistory: %v", err)
	}
	if !h.Sample() {
		t.Fatal("history recorded no sample")
	}
	cache := s.usageSeriesCache()
	cache.mu.Lock()
	cache.lanes = h
	cache.mu.Unlock()

	at := start.Add(30 * time.Second).Truncate(time.Second)
	got := postLaneReading(t, s, `{"percent":40,"source":"typed","lanes":3,"at":"`+at.Format(time.RFC3339)+`"}`)
	if got.Lanes == nil || *got.Lanes != 2 || got.LanesSource != usagecalc.LanesRecorded {
		t.Fatalf("covered earlier reading: stored lanes=%v source=%q, want 2 recorded", got.Lanes, got.LanesSource)
	}

	// An earlier time the history does not cover takes the ledger count now.
	old := start.Add(-time.Hour).Truncate(time.Second)
	got = postLaneReading(t, s, `{"percent":30,"source":"typed","at":"`+old.Format(time.RFC3339)+`"}`)
	if got.Lanes == nil || *got.Lanes != 1 || got.LanesSource != usagecalc.LanesCurrent {
		t.Fatalf("uncovered earlier reading: stored lanes=%v source=%q, want 1 current", got.Lanes, got.LanesSource)
	}
}

func TestUsageLaneCounterNames(t *testing.T) {
	s := &Server{}
	if _, ok := (usageLaneCounter{s: s}).Lanes(); ok {
		t.Fatal("an empty server reported a lane count")
	}
	s.inferenceReg = laneFakeRegistry(t)
	s.SetCapacity(laneFakeLedger(t, "task-a", "task-b"))
	if n, ok := (usageLaneCounter{s: s}).Lanes(); !ok || n != 2 {
		t.Fatalf("Lanes() = %d, %v, want 2, true", n, ok)
	}
}

func TestUsageLaneHistoryStartsOnceAndStops(t *testing.T) {
	dataDir := t.TempDir()
	s := newUsageTestServer(t, dataDir, "", "", "", "")
	s.inferenceReg = laneFakeRegistry(t)
	s.SetCapacity(laneFakeLedger(t, "task-a"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartUsageLaneHistory(ctx)
	s.StartUsageLaneHistory(ctx) // a second start does nothing

	cache := s.usageSeriesCache()
	deadline := time.Now().Add(5 * time.Second)
	for {
		cache.mu.Lock()
		h := cache.lanes
		cache.mu.Unlock()
		if h != nil {
			if samples := h.Samples(); len(samples) == 1 && samples[0].Count == 1 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the sampler recorded no sample with one Claude lane")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
}
