package config

import (
	"encoding/json"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
	"gopkg.in/yaml.v3"
)

func f64(v float64) *float64 { return &v }

// Two fake LLM entries on different providers, different models and rates:
// one model carries its own rates (override), the others fall back to the
// per-backend cost_rates entry.
func TestResolveModelCostRate_OverrideAndFallback(t *testing.T) {
	backend := map[string]CostRateConfig{
		"claude-code": {InPerK: 0.003, OutPerK: 0.015},
		"ollama":      {InPerK: 0, OutPerK: 0},
	}
	claude := inference.LLM{Name: "fake-claude", Kind: "claude", Models: []inference.EnabledModel{
		{Model: "opus-x", InPerK: f64(0.015), OutPerK: f64(0.075), CacheReadPerK: f64(0.0015), CacheWritePerK: f64(0.01875)},
		{Model: "sonnet-x"},
	}}
	ollama := inference.LLM{Name: "fake-ollama", Kind: "ollama", Models: []inference.EnabledModel{
		{Node: "gpu-a", Model: "qwen-x", OutPerK: f64(0.0002)},
		{Node: "gpu-a", Model: "llama-x"},
	}}

	got := ResolveModelCostRate(claude, "opus-x", "claude-code", backend)
	if got != (ModelCostRate{0.015, 0.075, 0.0015, 0.01875, "model"}) {
		t.Fatalf("override: %+v", got)
	}
	got = ResolveModelCostRate(claude, "sonnet-x", "claude-code", backend)
	if got != (ModelCostRate{0.003, 0.015, 0, 0, "backend"}) {
		t.Fatalf("fallback: %+v", got)
	}
	got = ResolveModelCostRate(ollama, "qwen-x", "ollama", backend)
	if got != (ModelCostRate{0, 0.0002, 0, 0, "model"}) {
		t.Fatalf("partial override: %+v", got)
	}
	got = ResolveModelCostRate(ollama, "llama-x", "ollama", backend)
	if got != (ModelCostRate{0, 0, 0, 0, "backend"}) {
		t.Fatalf("ollama fallback: %+v", got)
	}
	got = ResolveModelCostRate(ollama, "unknown", "nope", backend)
	if got.Source != "" {
		t.Fatalf("no rate: %+v", got)
	}
}

// The rate fields round-trip through JSON (REST/MCP/web) and YAML.
func TestEnabledModelRateFieldsRoundTrip(t *testing.T) {
	in := inference.EnabledModel{Model: "m", InPerK: f64(0.001), CacheWritePerK: f64(0.002)}
	b, _ := json.Marshal(in)
	if string(b) != `{"model":"m","in_per_k":0.001,"cache_write_per_k":0.002}` {
		t.Fatalf("json: %s", b)
	}
	var back inference.EnabledModel
	if err := json.Unmarshal(b, &back); err != nil || *back.InPerK != 0.001 || back.OutPerK != nil {
		t.Fatalf("json back: %+v %v", back, err)
	}
	y, _ := yaml.Marshal(in)
	var yb inference.EnabledModel
	if err := yaml.Unmarshal(y, &yb); err != nil || *yb.CacheWritePerK != 0.002 {
		t.Fatalf("yaml: %s %v", y, err)
	}
}
