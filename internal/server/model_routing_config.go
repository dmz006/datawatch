// Model routing config: the autonomous.model_routing block on GET /api/config
// and PUT /api/config.
//
// The block holds the policy for a worker pinned to a model that no registered
// node serves: refuse or only warn, how long a node's served model list is
// cached, whether the verifier follows its worker's node, how a node whose
// models cannot be read counts, how long a read may take, and whether a refusal
// raises an alert. GET serves every key with its effective value, so a fresh
// config shows the defaults. PUT checks every key before anything is applied,
// so one bad value refuses the whole request; a good value is saved and read
// live by the Automaton manager and the served-models cache, with no restart.
package server

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// modelRoutingKeyPrefix is the config path every autonomous.model_routing.* key
// shares.
const modelRoutingKeyPrefix = "autonomous.model_routing"

// PUT /api/config keys of the model routing block.
const (
	keyModelRoutingRefusalMode               = modelRoutingKeyPrefix + ".refusal_mode"
	keyModelRoutingServedModelsCacheSeconds  = modelRoutingKeyPrefix + ".served_models_cache_seconds"
	keyModelRoutingVerifierFollowsWorkerNode = modelRoutingKeyPrefix + ".verifier_follows_worker_node"
	keyModelRoutingUnknownNodePolicy         = modelRoutingKeyPrefix + ".unknown_node_policy"
	keyModelRoutingProbeTimeoutSeconds       = modelRoutingKeyPrefix + ".probe_timeout_seconds"
	keyModelRoutingAlertOnRefusal            = modelRoutingKeyPrefix + ".alert_on_refusal"
)

// modelRoutingKeys lists the keys of the block in a fixed order, so the first
// bad value reported does not depend on map order.
var modelRoutingKeys = []string{
	keyModelRoutingRefusalMode, keyModelRoutingServedModelsCacheSeconds,
	keyModelRoutingVerifierFollowsWorkerNode, keyModelRoutingUnknownNodePolicy,
	keyModelRoutingProbeTimeoutSeconds, keyModelRoutingAlertOnRefusal,
}

// Upper bound on the two whole-second keys, so a value cannot overflow when it
// is turned into a duration.
const modelRoutingMaxSeconds = 7 * 24 * 60 * 60

// ModelRoutingSurface is the autonomous.model_routing block GET /api/config
// serves: every key with the value in effect.
type ModelRoutingSurface struct {
	RefusalMode               string `json:"refusal_mode"`
	ServedModelsCacheSeconds  int    `json:"served_models_cache_seconds"`
	VerifierFollowsWorkerNode bool   `json:"verifier_follows_worker_node"`
	UnknownNodePolicy         string `json:"unknown_node_policy"`
	ProbeTimeoutSeconds       int    `json:"probe_timeout_seconds"`
	AlertOnRefusal            bool   `json:"alert_on_refusal"`
}

// modelRoutingConfigSurface returns the six values in effect, so an unset key
// shows its default.
func modelRoutingConfigSurface(c config.ModelRoutingConfig) ModelRoutingSurface {
	return ModelRoutingSurface{
		RefusalMode:               c.EffectiveRefusalMode(),
		ServedModelsCacheSeconds:  int(c.EffectiveServedModelsCacheAge().Seconds()),
		VerifierFollowsWorkerNode: c.EffectiveVerifierFollowsWorkerNode(),
		UnknownNodePolicy:         c.EffectiveUnknownNodePolicy(),
		ProbeTimeoutSeconds:       int(c.EffectiveProbeTimeout().Seconds()),
		AlertOnRefusal:            c.EffectiveAlertOnRefusal(),
	}
}

// expandModelRoutingBlock turns a whole-block value, such as
// {"autonomous.model_routing": {"refusal_mode": "warn"}}, into the dotted keys
// the rest of the PUT handles. A value that is not an object, or an object with
// a key the block does not have, is an error naming the key. The dotted keys
// already in patch are kept; a key sent both ways takes the dotted value.
func expandModelRoutingBlock(patch map[string]any) error {
	raw, ok := patch[modelRoutingKeyPrefix]
	if !ok {
		return nil
	}
	obj, isObj := raw.(map[string]any)
	if !isObj {
		return fmt.Errorf("%s: must be an object with the keys %s", modelRoutingKeyPrefix, modelRoutingShortKeys())
	}
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := modelRoutingKeyPrefix + "." + name
		if !isModelRoutingKey(key) {
			return fmt.Errorf("%s: unknown key; use %s", key, modelRoutingShortKeys())
		}
	}
	delete(patch, modelRoutingKeyPrefix)
	for _, name := range names {
		key := modelRoutingKeyPrefix + "." + name
		if _, set := patch[key]; !set {
			patch[key] = obj[name]
		}
	}
	return nil
}

func isModelRoutingKey(key string) bool {
	for _, k := range modelRoutingKeys {
		if k == key {
			return true
		}
	}
	return false
}

func modelRoutingShortKeys() string {
	short := make([]string, len(modelRoutingKeys))
	for i, k := range modelRoutingKeys {
		short[i] = strings.TrimPrefix(k, modelRoutingKeyPrefix+".")
	}
	return strings.Join(short, ", ")
}

// validateModelRoutingPatch checks the model routing keys of a PUT /api/config
// body before anything is applied. A key that is absent is not checked. The
// error names the key and the valid values.
func validateModelRoutingPatch(patch map[string]any) error {
	var scratch config.ModelRoutingConfig
	for _, key := range modelRoutingKeys {
		v, ok := patch[key]
		if !ok {
			continue
		}
		if err := applyModelRoutingKey(&scratch, key, v); err != nil {
			return err
		}
	}
	return scratch.Validate()
}

// applyModelRoutingPatch writes one model routing key into the daemon config.
// It reports whether key belongs to the block. The value was checked by
// validateModelRoutingPatch before the PUT reached this point, so a value that
// does not parse here is left unchanged.
func applyModelRoutingPatch(cfg *config.Config, key string, v any) bool {
	if !isModelRoutingKey(key) {
		return false
	}
	scratch := cfg.Autonomous.ModelRouting.Clone()
	if err := applyModelRoutingKey(&scratch, key, v); err == nil {
		cfg.Autonomous.ModelRouting = scratch
	}
	return true
}

// applyModelRoutingKey parses one key's value and stores it in c. When err is
// not nil, c is unchanged. A null value clears the key back to unset, which
// reads the default.
func applyModelRoutingKey(c *config.ModelRoutingConfig, key string, v any) error {
	switch key {
	case keyModelRoutingRefusalMode:
		if v == nil {
			c.RefusalMode = ""
			return nil
		}
		str, ok := v.(string)
		str = strings.TrimSpace(str)
		if !ok || (str != config.ModelRoutingRefusalRefuse && str != config.ModelRoutingRefusalWarn) {
			return fmt.Errorf("%s: %v is invalid; use %q or %q (default %q)", key, v,
				config.ModelRoutingRefusalRefuse, config.ModelRoutingRefusalWarn, config.ModelRoutingDefaultRefusalMode)
		}
		c.RefusalMode = str
	case keyModelRoutingUnknownNodePolicy:
		if v == nil {
			c.UnknownNodePolicy = ""
			return nil
		}
		str, ok := v.(string)
		str = strings.TrimSpace(str)
		if !ok || (str != config.ModelRoutingUnknownAssumeServes && str != config.ModelRoutingUnknownAssumeNot) {
			return fmt.Errorf("%s: %v is invalid; use %q or %q (default %q)", key, v,
				config.ModelRoutingUnknownAssumeServes, config.ModelRoutingUnknownAssumeNot, config.ModelRoutingDefaultUnknownNodePolicy)
		}
		c.UnknownNodePolicy = str
	case keyModelRoutingServedModelsCacheSeconds:
		if v == nil {
			c.ServedModelsCacheSeconds = nil
			return nil
		}
		n, ok := infraRetryWhole(v)
		if !ok || n < 0 || n > modelRoutingMaxSeconds {
			return fmt.Errorf("%s: %v is invalid; use a whole number from 0 to %d (0 = read from the node every time, default %d)",
				key, v, modelRoutingMaxSeconds, config.ModelRoutingDefaultServedModelsCacheSeconds)
		}
		c.ServedModelsCacheSeconds = &n
	case keyModelRoutingProbeTimeoutSeconds:
		if v == nil {
			c.ProbeTimeoutSeconds = nil
			return nil
		}
		n, ok := infraRetryWhole(v)
		if !ok || n < 1 || n > modelRoutingMaxSeconds {
			return fmt.Errorf("%s: %v is invalid; use a whole number from 1 to %d (default %d)",
				key, v, modelRoutingMaxSeconds, config.ModelRoutingDefaultProbeTimeoutSeconds)
		}
		c.ProbeTimeoutSeconds = &n
	case keyModelRoutingVerifierFollowsWorkerNode, keyModelRoutingAlertOnRefusal:
		target := &c.VerifierFollowsWorkerNode
		if key == keyModelRoutingAlertOnRefusal {
			target = &c.AlertOnRefusal
		}
		if v == nil {
			*target = nil
			return nil
		}
		b, ok := failureEscalationBool(v)
		if !ok {
			return fmt.Errorf("%s: %v is invalid; valid values: true, false", key, v)
		}
		*target = &b
	default:
		return fmt.Errorf("%s: unknown key; use %s", key, modelRoutingShortKeys())
	}
	return nil
}
