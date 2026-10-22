// plan_tier + effective-inflight exposure on the REST surface:
//   - GET /api/llms surfaces plan_tier, effective_max_inflight, inflight_source
//   - PUT /api/llms validates plan_tier (bad tier → 4xx)
//   - applyConfigPatch accepts autonomous.plan_tier_max_inflight in object
//     and compact-string forms and rejects unknown tiers

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

type llmViewDecoded struct {
	inference.LLM
	PlanTier             string `json:"plan_tier"`
	MaxInflight          *int   `json:"max_inflight"`
	EffectiveMaxInflight int    `json:"effective_max_inflight"`
	InflightSource       string `json:"inflight_source"`
}

func TestREST_LLMSGetSurfacesPlanTierAndEffectiveInflight(t *testing.T) {
	srv := newLLMServer(t)

	// Create an LLM with a plan tier but no explicit cap.
	body, _ := json.Marshal(map[string]any{
		"name": "tiered", "kind": "claude-code", "plan_tier": "max_5x",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/llms", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create: status %d body: %s", w.Code, w.Body.String())
	}

	// Configure the tier table so max_5x maps to 2.
	if srv.cfg == nil {
		srv.cfg = &config.Config{}
	}
	srv.cfg.Autonomous.PlanTierMaxInflight = map[string]int{"max_5x": 2}

	req = httptest.NewRequest(http.MethodGet, "/api/llms/tiered", nil)
	req.URL.Path = "/api/llms/tiered"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status %d body: %s", w.Code, w.Body.String())
	}
	var got llmViewDecoded
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.PlanTier != "max_5x" {
		t.Errorf("plan_tier = %q, want max_5x", got.PlanTier)
	}
	if got.EffectiveMaxInflight != 2 {
		t.Errorf("effective_max_inflight = %d, want 2", got.EffectiveMaxInflight)
	}
	if got.InflightSource != "plan_tier" {
		t.Errorf("inflight_source = %q, want plan_tier", got.InflightSource)
	}

	// Explicit cap overrides the tier table, and the source flips.
	body, _ = json.Marshal(map[string]any{"max_inflight": 9})
	req = httptest.NewRequest(http.MethodPut, "/api/llms/tiered", bytes.NewReader(body))
	req.URL.Path = "/api/llms/tiered"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: status %d body: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/llms/tiered", nil)
	req.URL.Path = "/api/llms/tiered"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	var got2 llmViewDecoded
	if err := json.NewDecoder(w.Body).Decode(&got2); err != nil {
		t.Fatalf("decode get 2: %v", err)
	}
	if got2.EffectiveMaxInflight != 9 || got2.InflightSource != "explicit" {
		t.Errorf("after explicit cap: cap=%d source=%q, want 9/explicit",
			got2.EffectiveMaxInflight, got2.InflightSource)
	}
	if got2.PlanTier != "max_5x" {
		t.Errorf("plan_tier should survive update (unchanged): got %q", got2.PlanTier)
	}
}

func TestREST_LLMSUpdateRejectsInvalidPlanTier(t *testing.T) {
	srv := newLLMServer(t)
	body, _ := json.Marshal(map[string]any{"name": "tt", "kind": "claude-code"})
	req := httptest.NewRequest(http.MethodPost, "/api/llms", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create: status %d body: %s", w.Code, w.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"plan_tier": "platinum"})
	req = httptest.NewRequest(http.MethodPut, "/api/llms/tt", bytes.NewReader(body))
	req.URL.Path = "/api/llms/tt"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code < 400 {
		t.Fatalf("bad tier: status %d, want 4xx (body: %s)", w.Code, w.Body.String())
	}
}

func TestParsePlanTierTable_SupportedShapes(t *testing.T) {
	// Object form (REST/PWA raw JSON — interface values after decoding).
	got, err := parsePlanTierTable(map[string]interface{}{"max_5x": float64(2), "api": 0})
	if err != nil || got["max_5x"] != 2 || got["api"] != 0 {
		t.Fatalf("object form: %v %v", got, err)
	}
	// JSON object as a string (CLI `config set` / comm-channel `configure`).
	got, err = parsePlanTierTable(`{"pro":1,"max_20x":3}`)
	if err != nil || got["pro"] != 1 || got["max_20x"] != 3 {
		t.Fatalf("string JSON form: %v %v", got, err)
	}
	// Compact list.
	got, err = parsePlanTierTable("pro=1,max_5x=2")
	if err != nil || got["max_5x"] != 2 {
		t.Fatalf("compact form: %v %v", got, err)
	}
	// Bad tier and bad limit are rejected.
	if _, err = parsePlanTierTable(map[string]interface{}{"platinum": float64(2)}); err == nil {
		t.Fatal("unknown tier accepted, want error")
	}
	if _, err = parsePlanTierTable("pro=-1"); err == nil {
		t.Fatal("negative limit accepted, want error")
	}
	if _, err = parsePlanTierTable("nonsense"); err == nil {
		t.Fatal("gibberish accepted, want error")
	}
}

func TestApplyConfigPatch_PlanTierMaxInflight(t *testing.T) {
	// Object form (what JSON decoding produces: map[string]interface{}
	// with float64/int/json.Number values).
	cfg := &config.Config{}
	patch := map[string]interface{}{
		"autonomous.plan_tier_max_inflight": map[string]interface{}{
			"max_5x": float64(2), "api": 0, "pro": json.Number("1"),
		},
	}
	if err := applyConfigPatch(cfg, patch); err != nil {
		t.Fatalf("object form: %v", err)
	}
	got := cfg.Autonomous.PlanTierMaxInflight
	if got["max_5x"] != 2 || got["api"] != 0 || got["pro"] != 1 {
		t.Errorf("object form: table = %v, want map[max_5x:2 api:0 pro:1]", got)
	}

	// Compact string form "tier=N,tier=N".
	cfg2 := &config.Config{}
	if err := applyConfigPatch(cfg2, map[string]interface{}{
		"autonomous.plan_tier_max_inflight": "pro=1,max_20x=3",
	}); err != nil {
		t.Fatalf("string form: %v", err)
	}
	if cfg2.Autonomous.PlanTierMaxInflight["max_20x"] != 3 {
		t.Errorf("string form: table = %v", cfg2.Autonomous.PlanTierMaxInflight)
	}

	// Unknown tier and negative values: applyConfigPatch skips the value
	// silently (the 400 comes from handlePutConfig's pre-apply guard, which
	// calls parsePlanTierTable directly — covered below). Assert that the
	// bad value never lands in the config.
	cfg3 := &config.Config{}
	applyConfigPatch(cfg3, map[string]interface{}{
		"autonomous.plan_tier_max_inflight": map[string]interface{}{"platinum": float64(2)},
	})
	if cfg3.Autonomous.PlanTierMaxInflight != nil {
		t.Errorf("unknown tier landed in config: %v", cfg3.Autonomous.PlanTierMaxInflight)
	}
	cfg4 := &config.Config{}
	applyConfigPatch(cfg4, map[string]interface{}{
		"autonomous.plan_tier_max_inflight": "pro=-1",
	})
	if cfg4.Autonomous.PlanTierMaxInflight != nil {
		t.Errorf("negative cap landed in config: %v", cfg4.Autonomous.PlanTierMaxInflight)
	}

	// The guard itself (what produces the 400) must reject bad values.
	if _, err := parsePlanTierTable(map[string]interface{}{"platinum": float64(2)}); err == nil {
		t.Fatal("parsePlanTierTable accepted unknown tier, want error")
	}
	if _, err := parsePlanTierTable("pro=-1"); err == nil {
		t.Fatal("parsePlanTierTable accepted negative limit, want error")
	}
}

// REST /api/config round-trip for the tier table: PUT (object form) must be
// accepted, readable back via GET, and a PUT with an unknown tier must be
// rejected with 400 leaving the good value intact.
func TestREST_ConfigRoundTrip_PlanTierMaxInflight(t *testing.T) {
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}

	b, _ := json.Marshal(map[string]any{"autonomous.plan_tier_max_inflight": map[string]any{"max_5x": 2, "api": 0}})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Bad tier in a second PUT: whole-PUT rejection, previous value intact.
	bad, _ := json.Marshal(map[string]any{"autonomous.plan_tier_max_inflight": map[string]any{"platinum": 9}})
	rr = httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(bad)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT bad tier: want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.Autonomous.PlanTierMaxInflight["platinum"] != 0 || len(s.cfg.Autonomous.PlanTierMaxInflight) != 2 {
		t.Errorf("rejected PUT leaked into cfg: %v", s.cfg.Autonomous.PlanTierMaxInflight)
	}
	if s.cfg.Autonomous.PlanTierMaxInflight["max_5x"] != 2 {
		t.Errorf("good value lost: %v", s.cfg.Autonomous.PlanTierMaxInflight)
	}

	rr = httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			PlanTierMaxInflight map[string]int `json:"plan_tier_max_inflight"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.PlanTierMaxInflight["max_5x"] != 2 || got.Autonomous.PlanTierMaxInflight["api"] != 0 {
		t.Errorf("GET table = %v, want max_5x:2 api:0", got.Autonomous.PlanTierMaxInflight)
	}
}

// The compact "tier=N,tier=N" wire form (CLI / comm channel) must be
// accepted by the same PUT surface and land as a real int table.
func TestREST_ConfigPut_PlanTierTableCompactForm(t *testing.T) {
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}
	b, _ := json.Marshal(map[string]any{"autonomous.plan_tier_max_inflight": "pro=1,max_20x=4"})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := s.cfg.Autonomous.PlanTierMaxInflight; got["pro"] != 1 || got["max_20x"] != 4 {
		t.Errorf("cfg table = %v, want pro:1 max_20x:4", got)
	}
}
