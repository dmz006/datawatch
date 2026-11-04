// Model routing config: the autonomous.model_routing block.
//
// Autonomous workers are routed to compute nodes through the capacity ledger.
// A worker pinned to a model that no registered node serves fails later with a
// model-not-found error. This block holds the policy that makes that visible
// and refuses early: whether to refuse or only warn, how long a node's served
// model list is cached, whether the verifier follows the worker's node, how a
// node whose models cannot be read counts, how long a probe may take, and
// whether a refusal raises an alert.
//
// Booleans and numbers are pointers so an explicit false or 0 survives a YAML
// and JSON round trip and stays distinct from an unset key; the routing logic
// reads the block through the Effective* accessors so an unset key always
// reads its default.
package config

import (
	"fmt"
	"time"
)

// Values of autonomous.model_routing.refusal_mode.
const (
	// ModelRoutingRefusalRefuse refuses a worker whose model no node serves.
	ModelRoutingRefusalRefuse = "refuse"
	// ModelRoutingRefusalWarn lets the worker start and only records a warning.
	ModelRoutingRefusalWarn = "warn"
)

// Values of autonomous.model_routing.unknown_node_policy: how a node whose
// served models cannot be read counts.
const (
	// ModelRoutingUnknownAssumeServes counts the node as serving the model.
	ModelRoutingUnknownAssumeServes = "assume_serves"
	// ModelRoutingUnknownAssumeNot counts the node as not serving the model.
	ModelRoutingUnknownAssumeNot = "assume_not"
)

// Defaults for the autonomous.model_routing keys.
const (
	// ModelRoutingDefaultRefusalMode is the refusal_mode when unset.
	ModelRoutingDefaultRefusalMode = ModelRoutingRefusalRefuse
	// ModelRoutingDefaultServedModelsCacheSeconds is how long a node's served
	// model list is cached when served_models_cache_seconds is unset.
	ModelRoutingDefaultServedModelsCacheSeconds = 300
	// ModelRoutingDefaultVerifierFollowsWorkerNode is the default for
	// verifier_follows_worker_node.
	ModelRoutingDefaultVerifierFollowsWorkerNode = true
	// ModelRoutingDefaultUnknownNodePolicy is the unknown_node_policy when unset.
	ModelRoutingDefaultUnknownNodePolicy = ModelRoutingUnknownAssumeServes
	// ModelRoutingDefaultProbeTimeoutSeconds is the probe timeout when
	// probe_timeout_seconds is unset.
	ModelRoutingDefaultProbeTimeoutSeconds = 8
	// ModelRoutingDefaultAlertOnRefusal is the default for alert_on_refusal.
	ModelRoutingDefaultAlertOnRefusal = true
)

// ModelRoutingConfig is the autonomous.model_routing block.
type ModelRoutingConfig struct {
	// RefusalMode is "refuse" (default) or "warn".
	RefusalMode string `yaml:"refusal_mode,omitempty" json:"refusal_mode,omitempty"`
	// ServedModelsCacheSeconds is how long a node's served model list is
	// cached. nil reads as 300; an explicit 0 means read from the node every
	// time. Validate rejects a negative value.
	ServedModelsCacheSeconds *int `yaml:"served_models_cache_seconds,omitempty" json:"served_models_cache_seconds,omitempty"`
	// VerifierFollowsWorkerNode sends the verifier to the node its worker ran
	// on. nil reads as true.
	VerifierFollowsWorkerNode *bool `yaml:"verifier_follows_worker_node,omitempty" json:"verifier_follows_worker_node,omitempty"`
	// UnknownNodePolicy is "assume_serves" (default) or "assume_not": how a
	// node whose served models cannot be read counts.
	UnknownNodePolicy string `yaml:"unknown_node_policy,omitempty" json:"unknown_node_policy,omitempty"`
	// ProbeTimeoutSeconds is how long a read of a node's served models may
	// take. nil reads as 8; Validate rejects anything below 1.
	ProbeTimeoutSeconds *int `yaml:"probe_timeout_seconds,omitempty" json:"probe_timeout_seconds,omitempty"`
	// AlertOnRefusal raises an alert when a worker is refused. nil reads as true.
	AlertOnRefusal *bool `yaml:"alert_on_refusal,omitempty" json:"alert_on_refusal,omitempty"`
}

// IsZero reports whether no key of the block is set. yaml.v3 consults it for
// the parent's omitempty tag.
func (c ModelRoutingConfig) IsZero() bool {
	return c.RefusalMode == "" && c.ServedModelsCacheSeconds == nil &&
		c.VerifierFollowsWorkerNode == nil && c.UnknownNodePolicy == "" &&
		c.ProbeTimeoutSeconds == nil && c.AlertOnRefusal == nil
}

// EffectiveRefusalMode returns "refuse" or "warn"; unset or anything else
// reads as "refuse".
func (c ModelRoutingConfig) EffectiveRefusalMode() string {
	if c.RefusalMode == ModelRoutingRefusalWarn {
		return ModelRoutingRefusalWarn
	}
	return ModelRoutingDefaultRefusalMode
}

// EffectiveServedModelsCacheAge returns how long a node's served model list
// is cached. Zero means read from the node every time; a negative value reads
// as the default.
func (c ModelRoutingConfig) EffectiveServedModelsCacheAge() time.Duration {
	secs := ModelRoutingDefaultServedModelsCacheSeconds
	if c.ServedModelsCacheSeconds != nil && *c.ServedModelsCacheSeconds >= 0 {
		secs = *c.ServedModelsCacheSeconds
	}
	return time.Duration(secs) * time.Second
}

// EffectiveVerifierFollowsWorkerNode reports whether the verifier follows the
// worker's node (default true).
func (c ModelRoutingConfig) EffectiveVerifierFollowsWorkerNode() bool {
	return boolOrDefault(c.VerifierFollowsWorkerNode, ModelRoutingDefaultVerifierFollowsWorkerNode)
}

// EffectiveUnknownNodePolicy returns "assume_serves" or "assume_not"; unset or
// anything else reads as "assume_serves".
func (c ModelRoutingConfig) EffectiveUnknownNodePolicy() string {
	if c.UnknownNodePolicy == ModelRoutingUnknownAssumeNot {
		return ModelRoutingUnknownAssumeNot
	}
	return ModelRoutingDefaultUnknownNodePolicy
}

// EffectiveProbeTimeout returns how long a read of a node's served models may
// take; unset or below 1 second reads as the default.
func (c ModelRoutingConfig) EffectiveProbeTimeout() time.Duration {
	secs := ModelRoutingDefaultProbeTimeoutSeconds
	if c.ProbeTimeoutSeconds != nil && *c.ProbeTimeoutSeconds >= 1 {
		secs = *c.ProbeTimeoutSeconds
	}
	return time.Duration(secs) * time.Second
}

// EffectiveAlertOnRefusal reports whether a refusal raises an alert (default
// true).
func (c ModelRoutingConfig) EffectiveAlertOnRefusal() bool {
	return boolOrDefault(c.AlertOnRefusal, ModelRoutingDefaultAlertOnRefusal)
}

// Clone returns a deep copy so a mirror of the block never aliases the
// daemon config's pointers.
func (c ModelRoutingConfig) Clone() ModelRoutingConfig {
	out := c
	if c.ServedModelsCacheSeconds != nil {
		v := *c.ServedModelsCacheSeconds
		out.ServedModelsCacheSeconds = &v
	}
	if c.ProbeTimeoutSeconds != nil {
		v := *c.ProbeTimeoutSeconds
		out.ProbeTimeoutSeconds = &v
	}
	out.VerifierFollowsWorkerNode = cloneBoolPtr(c.VerifierFollowsWorkerNode)
	out.AlertOnRefusal = cloneBoolPtr(c.AlertOnRefusal)
	return out
}

// Validate rejects an unknown refusal mode or unknown node policy, a negative
// cache age and a probe timeout below 1, naming the key. An unset key is
// valid.
func (c ModelRoutingConfig) Validate() error {
	if c.RefusalMode != "" && c.RefusalMode != ModelRoutingRefusalRefuse && c.RefusalMode != ModelRoutingRefusalWarn {
		return fmt.Errorf("autonomous.model_routing.refusal_mode: %q is invalid; use %q or %q",
			c.RefusalMode, ModelRoutingRefusalRefuse, ModelRoutingRefusalWarn)
	}
	if c.UnknownNodePolicy != "" && c.UnknownNodePolicy != ModelRoutingUnknownAssumeServes && c.UnknownNodePolicy != ModelRoutingUnknownAssumeNot {
		return fmt.Errorf("autonomous.model_routing.unknown_node_policy: %q is invalid; use %q or %q",
			c.UnknownNodePolicy, ModelRoutingUnknownAssumeServes, ModelRoutingUnknownAssumeNot)
	}
	if c.ServedModelsCacheSeconds != nil && *c.ServedModelsCacheSeconds < 0 {
		return fmt.Errorf("autonomous.model_routing.served_models_cache_seconds: %d is invalid; use a whole number >= 0 (0 = read from the node every time, default %d)",
			*c.ServedModelsCacheSeconds, ModelRoutingDefaultServedModelsCacheSeconds)
	}
	if c.ProbeTimeoutSeconds != nil && *c.ProbeTimeoutSeconds < 1 {
		return fmt.Errorf("autonomous.model_routing.probe_timeout_seconds: %d is invalid; use a whole number >= 1 (default %d)",
			*c.ProbeTimeoutSeconds, ModelRoutingDefaultProbeTimeoutSeconds)
	}
	return nil
}
