// plan_coverage.go — plan-requirement coverage check settings: the
// valid modes, their normalization, and the effective-settings
// resolver used by every consumer (autonomous Manager, server
// handlers) so a raw zero/empty config always resolves to the safe
// defaults: check mode "block" and min ratio 1.0 (full coverage).
package config

import (
	"fmt"
	"strings"
)

// Plan-coverage check modes (AutonomousConfig.PlanCoverageCheck).
const (
	// PlanCoverageOff disables the coverage check.
	PlanCoverageOff = "off"
	// PlanCoverageWarn surfaces a failing report but proceeds.
	PlanCoverageWarn = "warn"
	// PlanCoverageBlock stops decomposition on a failing report.
	// This is the default when the setting is empty/unset.
	PlanCoverageBlock = "block"
)

// ValidPlanCoverageModes lists the accepted check-mode values in
// precedence-free order, for error messages and UI validation.
var ValidPlanCoverageModes = []string{PlanCoverageOff, PlanCoverageWarn, PlanCoverageBlock}

// NormalizePlanCoverageCheck resolves a raw plan-coverage check mode to
// its canonical form. Empty (unset) normalizes to "block" — the safe
// default. Comparison is case-insensitive. Any other value is an error;
// callers must not silently fall back on a typo ("blockk" must never
// quietly mean "block").
func NormalizePlanCoverageCheck(s string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(s))
	if m == "" {
		return PlanCoverageBlock, nil
	}
	switch m {
	case PlanCoverageOff, PlanCoverageWarn, PlanCoverageBlock:
		return m, nil
	default:
		return "", fmt.Errorf("invalid plan_coverage_check %q: must be one of %q, %q, %q (or empty for the %q default)",
			s, PlanCoverageOff, PlanCoverageWarn, PlanCoverageBlock, PlanCoverageBlock)
	}
}

// NormalizePlanCoverageRatio clamps a raw plan-coverage min ratio into
// the valid range (0,1]: zero or negative (unset / invalid) becomes 1.0
// (require full coverage — the safe default) and values above 1 clamp
// to 1.0. Any value in (0,1] passes through unchanged.
func NormalizePlanCoverageRatio(r float64) float64 {
	switch {
	case r <= 0:
		return 1.0
	case r > 1.0:
		return 1.0
	}
	return r
}

// PlanCoverageSettings is the resolved, always-valid pair of
// plan-coverage settings. Construct it via EffectivePlanCoverage or
// AutonomousConfig.EffectivePlanCoverage rather than zero-initializing
// it (the zero value is NOT the defaults: mode "" and ratio 0 are the
// RAW unset values, not the effective ones).
type PlanCoverageSettings struct {
	Mode     string  // "off" | "warn" | "block", always non-empty
	MinRatio float64 // in (0,1]; 1.0 = require full coverage
}

// EffectivePlanCoverage resolves raw config values (check mode string
// and min ratio) to the settings in effect. Empty mode → "block";
// zero/negative ratio → 1.0; ratio > 1 → 1.0. Returns an error if the
// mode is not one of off|warn|block (or empty).
func EffectivePlanCoverage(check string, minRatio float64) (PlanCoverageSettings, error) {
	mode, err := NormalizePlanCoverageCheck(check)
	if err != nil {
		return PlanCoverageSettings{}, err
	}
	return PlanCoverageSettings{Mode: mode, MinRatio: NormalizePlanCoverageRatio(minRatio)}, nil
}

// EffectivePlanCoverage resolves the plan-coverage settings in effect
// for this autonomous config (AutonomousConfig.PlanCoverageCheck and
// .PlanCoverageMinRatio). It is the single canonical entry point for
// consumers: an unset config yields mode "block" and min ratio 1.0.
func (a *AutonomousConfig) EffectivePlanCoverage() (PlanCoverageSettings, error) {
	return EffectivePlanCoverage(a.PlanCoverageCheck, a.PlanCoverageMinRatio)
}
