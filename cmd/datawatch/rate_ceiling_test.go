package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

func TestRateCeilingCLI_FlagsBuildBody(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	if err := cmd.ParseFlags([]string{"--capacity-rate-requests", "60", "--capacity-rate-tokens", "0", "--capacity-rate-window-seconds", "1800"}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{}
	if err := applyAutonomousRateFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	if body["capacity_rate_requests"] != 60 || body["capacity_rate_tokens"] != 0 || body["capacity_rate_window_seconds"] != 1800 {
		t.Fatalf("body = %v", body)
	}

	cmd = newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags(nil)
	body = map[string]any{}
	if err := applyAutonomousRateFlags(cmd, body); err != nil || len(body) != 0 {
		t.Fatalf("unset flags must add nothing: %v %v", body, err)
	}

	cmd = newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags([]string{"--capacity-rate-tokens", "-5"})
	if err := applyAutonomousRateFlags(cmd, map[string]any{}); err == nil {
		t.Fatal("negative flag value accepted")
	}
	for _, f := range []string{"capacity-rate-requests", "capacity-rate-tokens", "capacity-rate-window-seconds"} {
		if !strings.Contains(newAutonomousConfigSetCmd().Long+newAutonomousConfigSetCmd().UsageString(), f) {
			t.Errorf("config-set help lacks --%s", f)
		}
	}
}

func TestRateCeilingWiring_SyncAndRecordTokens(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "cc", Kind: inference.KindClaudeCode}); err != nil {
		t.Fatal(err)
	}
	led := capacity.New(capacity.Options{})

	syncLLMRates(cfg, reg, led)
	if got := led.RateOf("llm:cc"); got != (capacity.RateCeiling{}) {
		t.Fatalf("default config set a ceiling: %+v", got)
	}

	cfg.Autonomous.CapacityRateTokens = 100
	syncLLMRates(cfg, reg, led)
	got := led.RateOf("llm:cc")
	if got.Tokens != 100 || got.Window != time.Hour {
		t.Fatalf("ceiling = %+v, want 100 tokens over the 1h default window", got)
	}

	cfg.Autonomous.CapacityRateWindowSeconds = 60
	syncLLMRates(cfg, reg, led)
	if w := led.RateOf("llm:cc").Window; w != time.Minute {
		t.Fatalf("window = %v, want 1m", w)
	}

	// Usage events count tokens (even without a dollar amount) against the pool.
	hook := recordUsage(nil, func() *inference.Registry { return reg }, led)
	hook(session.UsageEvent{Backend: "cc", At: time.Now(), TokensIn: 60, TokensOut: 50})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := led.Acquire(ctx, capacity.Request{Holder: "t1", Pools: []string{"llm:cc"}}, 50*time.Millisecond, func() bool { return false }, func(string) {})
	if err == nil {
		t.Fatal("acquire succeeded though the token ceiling is exhausted")
	}

	cfg.Autonomous.CapacityRateTokens = 0
	syncLLMRates(cfg, reg, led)
	if got := led.RateOf("llm:cc"); got != (capacity.RateCeiling{}) {
		t.Fatalf("clearing config left a ceiling: %+v", got)
	}
}
