package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func putAllocationConfig(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func getAllocationTable(t *testing.T, s *Server) map[string]float64 {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			PlanTierAllocationUSD map[string]float64 `json:"plan_tier_allocation_usd"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.PlanTierAllocationUSD == nil {
		t.Fatalf("GET plan_tier_allocation_usd missing or null: %s", rr.Body.String())
	}
	return got.Autonomous.PlanTierAllocationUSD
}

func TestPlanTierAllocationAPI(t *testing.T) {
	newServer := func(t *testing.T) *Server {
		return &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}
	}

	t.Run("default renders as empty object", func(t *testing.T) {
		if got := getAllocationTable(t, newServer(t)); len(got) != 0 {
			t.Fatalf("default table = %v, want empty", got)
		}
	})

	t.Run("whole object round trip", func(t *testing.T) {
		s := newServer(t)
		rr := putAllocationConfig(t, s, map[string]any{
			"autonomous.plan_tier_allocation_usd": map[string]any{"pro": 20, "max_5x": 100.5},
		})
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		got := getAllocationTable(t, s)
		if got["pro"] != 20 || got["max_5x"] != 100.5 || len(got) != 2 {
			t.Fatalf("GET table = %v, want pro:20 max_5x:100.5", got)
		}
	})

	t.Run("single pair round trip keeps other tiers", func(t *testing.T) {
		s := newServer(t)
		if rr := putAllocationConfig(t, s, map[string]any{
			"autonomous.plan_tier_allocation_usd": map[string]any{"pro": 20},
		}); rr.Code != http.StatusOK {
			t.Fatalf("seed PUT: %d %s", rr.Code, rr.Body.String())
		}
		rr := putAllocationConfig(t, s, map[string]any{"autonomous.plan_tier_allocation_usd.max_20x": 400})
		if rr.Code != http.StatusOK {
			t.Fatalf("pair PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		got := getAllocationTable(t, s)
		if got["pro"] != 20 || got["max_20x"] != 400 || len(got) != 2 {
			t.Fatalf("GET table = %v, want pro:20 max_20x:400", got)
		}
	})

	t.Run("configure style single key write", func(t *testing.T) {
		// The comm-channel configure verb sends the value as a JSON string
		// for non-integers and as a bare number for integers; both land.
		s := newServer(t)
		if rr := putAllocationConfig(t, s, map[string]any{"autonomous.plan_tier_allocation_usd.max_5x": "99.5"}); rr.Code != http.StatusOK {
			t.Fatalf("string pair PUT: %d %s", rr.Code, rr.Body.String())
		}
		if rr := putAllocationConfig(t, s, map[string]any{"autonomous.plan_tier_allocation_usd": "pro=20,api=0"}); rr.Code != http.StatusOK {
			t.Fatalf("compact table PUT: %d %s", rr.Code, rr.Body.String())
		}
		got := getAllocationTable(t, s)
		if got["pro"] != 20 || len(got) != 2 {
			t.Fatalf("compact table replaced the table: %v", got)
		}
		if rr := putAllocationConfig(t, s, map[string]any{"autonomous.plan_tier_allocation_usd.max_5x": "99.5"}); rr.Code != http.StatusOK {
			t.Fatalf("string pair PUT: %d %s", rr.Code, rr.Body.String())
		}
		if got := getAllocationTable(t, s); got["max_5x"] != 99.5 || got["pro"] != 20 {
			t.Fatalf("GET table = %v, want max_5x:99.5 pro:20", got)
		}
	})

	t.Run("bad values return 400 and change nothing", func(t *testing.T) {
		s := newServer(t)
		if rr := putAllocationConfig(t, s, map[string]any{
			"autonomous.plan_tier_allocation_usd": map[string]any{"pro": 20},
		}); rr.Code != http.StatusOK {
			t.Fatalf("seed PUT: %d %s", rr.Code, rr.Body.String())
		}
		for name, body := range map[string]map[string]any{
			"unknown tier object": {"autonomous.plan_tier_allocation_usd": map[string]any{"platinum": 9}},
			"negative object":     {"autonomous.plan_tier_allocation_usd": map[string]any{"pro": -1}},
			"negative compact":    {"autonomous.plan_tier_allocation_usd": "pro=-5"},
			"gibberish":           {"autonomous.plan_tier_allocation_usd": "nonsense"},
			"non-numeric object":  {"autonomous.plan_tier_allocation_usd": map[string]any{"pro": "lots"}},
			"unknown tier pair":   {"autonomous.plan_tier_allocation_usd.platinum": 9},
			"negative pair":       {"autonomous.plan_tier_allocation_usd.pro": -1},
			"non-numeric pair":    {"autonomous.plan_tier_allocation_usd.pro": "lots"},
			"boolean pair":        {"autonomous.plan_tier_allocation_usd.pro": true},
			"bad pair, good key":  {"autonomous.plan_tier_allocation_usd.pro": -1, "autonomous.plan_tier_allocation_usd.api": 5},
		} {
			if rr := putAllocationConfig(t, s, body); rr.Code != http.StatusBadRequest {
				t.Errorf("%s: want 400, got %d body=%s", name, rr.Code, rr.Body.String())
			}
		}
		if got := getAllocationTable(t, s); len(got) != 1 || got["pro"] != 20 {
			t.Fatalf("rejected PUTs leaked into cfg: %v", got)
		}
	})
}
