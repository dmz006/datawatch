// Package alertrules implements per-pod alert rules and observer-driven
// autoscaling for datawatch v8.1 (S14b).
//
// AlertRule — a named condition evaluated against observer envelopes every
// 30 s. When the condition is satisfied for a given window the rule fires:
//   - action=alert  → emits a system alert via the alerts.Store
//   - action=scale_up/scale_down → calls the ScaleNode stub
//
// Rules are persisted in <data_dir>/alert-rules.yaml.
package alertrules

import "time"

// Condition is one metric threshold check evaluated against an envelope.
type Condition struct {
	// Metric is one of: cpu_pct | mem_pct | gpu_pct | rss_bytes |
	// net_rx_bps | net_tx_bps
	Metric string `yaml:"metric" json:"metric"`

	// Operator is one of: > | < | >= | <=
	Operator string `yaml:"operator" json:"operator"`

	// Threshold is the value to compare the metric against.
	Threshold float64 `yaml:"threshold" json:"threshold"`
}

// Action describes what the evaluator should do when a rule fires.
type Action struct {
	// Kind is one of: alert | scale_up | scale_down
	Kind string `yaml:"kind" json:"kind"`

	// ScaleTarget is the compute node name (for scale_up/scale_down actions).
	ScaleTarget string `yaml:"scale_target,omitempty" json:"scale_target,omitempty"`

	// ScaleAmount is the number of container instances to add or remove.
	// Defaults to 1.
	ScaleAmount int `yaml:"scale_amount,omitempty" json:"scale_amount,omitempty"`
}

// AlertRule is the full operator-defined rule persisted in YAML.
type AlertRule struct {
	// Name is the unique, human-readable identifier for this rule.
	Name string `yaml:"name" json:"name"`

	// Description is an optional prose summary of the rule.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Condition holds the metric/operator/threshold triple.
	Condition Condition `yaml:"condition" json:"condition"`

	// SourceFilter, when non-empty, restricts evaluation to envelopes
	// whose Source field equals this value. Empty = all envelopes.
	SourceFilter string `yaml:"source_filter,omitempty" json:"source_filter,omitempty"`

	// WindowSeconds is the look-back window for metric aggregation.
	// Defaults to 60 when zero.
	WindowSeconds int `yaml:"window_seconds,omitempty" json:"window_seconds,omitempty"`

	// Action describes what to do when the condition is met.
	Action Action `yaml:"action" json:"action"`

	// Enabled controls whether the evaluator considers this rule.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// CooldownSeconds is the minimum number of seconds between two
	// consecutive firings of this rule. Defaults to 300 when zero.
	CooldownSeconds int `yaml:"cooldown_seconds,omitempty" json:"cooldown_seconds,omitempty"`

	CreatedAt time.Time `yaml:"created_at" json:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at" json:"updated_at"`
}

// Firing is one historical record of a rule evaluation that crossed
// its threshold. The last 100 are kept in memory (ring buffer).
type Firing struct {
	RuleName     string    `json:"rule_name"`
	EnvID        string    `json:"envelope_id"`
	Source       string    `json:"source"`
	Value        float64   `json:"value"`
	FiredAt      time.Time `json:"fired_at"`
	Action       string    `json:"action"`
	ActionResult string    `json:"action_result,omitempty"`
}
