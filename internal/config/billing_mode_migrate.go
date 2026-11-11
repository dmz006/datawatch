package config

import (
	"strings"

	"github.com/dmz006/datawatch/internal/inference"
)

// MigrateBillingModes is a load-time pass that writes billing_mode on every
// LLM entry that has none, using ResolveBillingMode so behaviour does not
// change: the Claude entry becomes allowance, local engines become local, and
// other entries keep the mode their usage.billing profile implies (api when
// there is none). An entry that already has a billing_mode is never touched,
// so a second run changes nothing. Returns the number of entries written.
// cfg may be nil.
func MigrateBillingModes(reg *inference.Registry, cfg *Config) int {
	if reg == nil {
		return 0
	}
	touched := 0
	for _, l := range reg.List() {
		if l == nil || strings.TrimSpace(l.BillingMode) != "" {
			continue
		}
		cp := *l
		cp.BillingMode = ResolveBillingMode(cp, cfg)
		if err := reg.Update(&cp); err != nil {
			continue
		}
		touched++
	}
	return touched
}
