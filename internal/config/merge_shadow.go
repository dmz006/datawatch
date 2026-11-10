package config

import "time"

// MergeShadowConfig is autonomous.merge_shadow, the knobs for the shadow merge
// plan: the record of what merging an Automaton's branch would do, kept per
// project so the operator can compare the plan with the real merge afterwards.
//
// A field whose default is not the zero value is a pointer: nil means the key
// is absent and the Effective* helper returns the default. No field carries
// omitempty, so an explicit false or value survives a YAML/JSON round trip and
// a PUT /api/config write instead of being elided on marshal.
type MergeShadowConfig struct {
	// Enabled is the master switch for the shadow merge plan (default true).
	Enabled *bool `yaml:"enabled" json:"enabled"`
	// GateTimeout bounds how long a merge gate may wait before it gives up, as
	// a Go duration string such as "20m" (default 20 minutes). An empty,
	// unparseable or non-positive value falls back to the default.
	GateTimeout *string `yaml:"gate_timeout" json:"gate_timeout"`
	// PlanKeep is the number of shadow merge plans kept per project (default
	// 200). Older plans are pruned past this count. A value below 1 falls back
	// to the default.
	PlanKeep *int `yaml:"plan_keep" json:"plan_keep"`
}

// Defaults for autonomous.merge_shadow. Pinned by merge_shadow_test.go, so a
// changed default is a deliberate edit to this block and its tests.
const (
	DefaultMergeShadowEnabled     = true
	DefaultMergeShadowGateTimeout = 20 * time.Minute
	DefaultMergeShadowPlanKeep    = 200
)

// EffectiveEnabled reports whether the shadow merge plan is on. Absent is on.
func (m MergeShadowConfig) EffectiveEnabled() bool {
	if m.Enabled == nil {
		return DefaultMergeShadowEnabled
	}
	return *m.Enabled
}

// EffectiveGateTimeout returns the merge gate timeout, or the default when the
// key is absent, unparseable or not positive.
func (m MergeShadowConfig) EffectiveGateTimeout() time.Duration {
	if m.GateTimeout == nil {
		return DefaultMergeShadowGateTimeout
	}
	d, err := time.ParseDuration(*m.GateTimeout)
	if err != nil || d <= 0 {
		return DefaultMergeShadowGateTimeout
	}
	return d
}

// EffectivePlanKeep returns how many plans are kept per project, or the default
// when the key is absent or below 1.
func (m MergeShadowConfig) EffectivePlanKeep() int {
	if m.PlanKeep == nil || *m.PlanKeep < 1 {
		return DefaultMergeShadowPlanKeep
	}
	return *m.PlanKeep
}
