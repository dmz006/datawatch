package config

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/inference"
)

// Per-LLM billing modes: how one LLM registry entry is paid for.
const (
	// BillingModeAPI: metered pay-per-token spend.
	BillingModeAPI = "api"
	// BillingModeAllowance: a bundled plan allowance that resets on a schedule.
	BillingModeAllowance = "allowance"
	// BillingModeLocal: local inference at zero marginal cost.
	BillingModeLocal = "local"
)

// ValidateBillingMode accepts an empty value (resolved later) or one of the
// three billing modes.
func ValidateBillingMode(mode string) error {
	switch mode {
	case "", BillingModeAPI, BillingModeAllowance, BillingModeLocal:
		return nil
	}
	return fmt.Errorf("billing_mode: unknown value %q (allowed: %s, %s, %s)",
		mode, BillingModeAPI, BillingModeAllowance, BillingModeLocal)
}

// ResolveBillingMode returns the billing mode of an LLM entry. Order: the
// entry's own billing_mode, then the usage.billing profile for the entry's
// kind (bundled_plan -> allowance, api_metered -> api, free_local -> local),
// then the kind default (claude-code allowance; ollama, openwebui and shell
// local; everything else api). cfg may be nil.
func ResolveBillingMode(entry inference.LLM, cfg *Config) string {
	if m := strings.TrimSpace(entry.BillingMode); m != "" {
		return m
	}
	if cfg != nil {
		if p, ok := cfg.Usage.Billing[string(entry.Kind)]; ok {
			switch p.Mode {
			case BillingModeBundledPlan:
				return BillingModeAllowance
			case BillingModeAPIMetered:
				return BillingModeAPI
			case BillingModeFreeLocal:
				return BillingModeLocal
			}
		}
	}
	switch entry.Kind {
	case inference.KindClaudeCode:
		return BillingModeAllowance
	case inference.KindOllama, inference.KindOpenWebUI, inference.KindShell:
		return BillingModeLocal
	}
	return BillingModeAPI
}

// BillingModeHelp explains the three billing modes, one line each. Every
// surface that accepts a billing mode shows this text.
const BillingModeHelp = "billing_mode values:\n" +
	"  api        - metered pay-per-token spend\n" +
	"  allowance  - a bundled plan allowance that resets on a schedule\n" +
	"  local      - local inference at zero marginal cost"

// LLMBillingModeConfigKey recognises the config key of one LLM entry's billing
// mode, written llm.<name>.billing_mode (llms.<name>.billing_mode is accepted
// too). It returns the entry name and the key the daemon's config endpoint
// expects.
func LLMBillingModeConfigKey(key string) (name, restKey string, ok bool) {
	key = strings.TrimSpace(key)
	for _, prefix := range []string{"llm.", "llms."} {
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, ".billing_mode") {
			name = strings.TrimSuffix(strings.TrimPrefix(key, prefix), ".billing_mode")
			if name != "" {
				return name, "llms." + name + ".billing_mode", true
			}
		}
	}
	return "", "", false
}
