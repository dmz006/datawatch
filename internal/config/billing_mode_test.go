package config

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func TestResolveBillingModeOwnValue(t *testing.T) {
	for _, m := range []string{BillingModeAPI, BillingModeAllowance, BillingModeLocal} {
		e := inference.LLM{Kind: inference.KindClaudeCode, BillingMode: m}
		if got := ResolveBillingMode(e, nil); got != m {
			t.Errorf("own %q: got %q", m, got)
		}
	}
}

func TestResolveBillingModeProfile(t *testing.T) {
	cfg := &Config{}
	cfg.Usage.Billing = map[string]BillingProfile{
		"claude-code": {Mode: BillingModeAPIMetered},
		"ollama":      {Mode: BillingModeBundledPlan},
		"gemini":      {Mode: BillingModeFreeLocal},
	}
	cases := map[inference.Kind]string{
		inference.KindClaudeCode: BillingModeAPI,
		inference.KindOllama:     BillingModeAllowance,
		inference.KindGemini:     BillingModeLocal,
	}
	for k, want := range cases {
		if got := ResolveBillingMode(inference.LLM{Kind: k}, cfg); got != want {
			t.Errorf("profile %s: got %q want %q", k, got, want)
		}
	}
	// Own value beats profile.
	if got := ResolveBillingMode(inference.LLM{Kind: inference.KindClaudeCode, BillingMode: BillingModeLocal}, cfg); got != BillingModeLocal {
		t.Errorf("own should beat profile, got %q", got)
	}
}

func TestResolveBillingModeKindDefaults(t *testing.T) {
	cases := map[inference.Kind]string{
		inference.KindClaudeCode: BillingModeAllowance,
		inference.KindOllama:     BillingModeLocal,
		inference.KindOpenWebUI:  BillingModeLocal,
		inference.KindShell:      BillingModeLocal,
		inference.KindClaude:     BillingModeAPI,
		inference.KindGemini:     BillingModeAPI,
	}
	for k, want := range cases {
		if got := ResolveBillingMode(inference.LLM{Kind: k}, nil); got != want {
			t.Errorf("kind %s: got %q want %q", k, got, want)
		}
		if got := ResolveBillingMode(inference.LLM{Kind: k}, &Config{}); got != want {
			t.Errorf("kind %s empty cfg: got %q want %q", k, got, want)
		}
	}
}

func TestValidateBillingMode(t *testing.T) {
	for _, ok := range []string{"", BillingModeAPI, BillingModeAllowance, BillingModeLocal} {
		if err := ValidateBillingMode(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	err := ValidateBillingMode("free")
	if err == nil {
		t.Fatal("invalid value accepted")
	}
	for _, w := range []string{"api", "allowance", "local", "free"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q missing %q", err, w)
		}
	}
}
