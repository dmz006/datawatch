package server

// Config surface for the outbound webhook sink and peer forwarding: the
// autonomous.notify.webhook.* and autonomous.notify.forwarding.* keys. Every
// surface (REST GET and PUT /api/config, the MCP config_set tool, the CLI
// `config set` and the comm `configure` verb) reaches these keys through the
// same PUT /api/config path, so the validation and the apply rules live here
// once.
//
// The defaults and the meaning of each key are in
// docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md
// (section 4). A value below 1 means "use the default", the same convention as
// autonomous.notify.pending_events_max.

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// notifyIntRule describes one whole-number key. max is the largest accepted
// value; 0 means no upper bound.
type notifyIntRule struct {
	key string
	max int
}

// notifyIntRules lists the whole-number keys in a fixed order, so a rejected
// PUT always names the same key first.
var notifyIntRules = []notifyIntRule{
	{key: "autonomous.notify.webhook.max_attempts"},
	{key: "autonomous.notify.webhook.jitter_percent", max: 100},
	{key: "autonomous.notify.webhook.attempt_timeout_seconds"},
	{key: "autonomous.notify.webhook.max_body_bytes"},
	{key: "autonomous.notify.webhook.signature_tolerance_seconds"},
	{key: "autonomous.notify.forwarding.max_hops"},
	{key: "autonomous.notify.forwarding.max_attempts"},
	{key: "autonomous.notify.forwarding.jitter_percent", max: 100},
	{key: "autonomous.notify.forwarding.attempt_timeout_seconds"},
}

// notifyBackoffKeys are the delay-schedule keys: a list of whole seconds, each at least 1.
var notifyBackoffKeys = []string{
	"autonomous.notify.webhook.backoff_seconds",
	"autonomous.notify.forwarding.backoff_seconds",
}

// notifyAllowHostKey is the host allow-list. It is the only text key here.
const notifyAllowHostKey = "autonomous.notify.webhook.allow_hosts"

// notifyInt reads a whole number from a JSON value. A JSON number, a Go int or
// a numeric string is accepted; anything fractional or non-numeric is an error.
func notifyInt(v interface{}) (int, error) {
	switch x := v.(type) {
	case float64:
		if x != math.Trunc(x) || x > math.MaxInt32 || x < math.MinInt32 {
			return 0, fmt.Errorf("must be a whole number")
		}
		return int(x), nil
	case int:
		return x, nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, fmt.Errorf("must be a whole number, got %q", x)
		}
		return n, nil
	}
	return 0, fmt.Errorf("must be a whole number")
}

// notifyStringList reads an allow-list value: a JSON array, or a comma-separated
// string (the CLI and the comm channel quote their values). Empty entries are dropped.
func notifyStringList(v interface{}) ([]string, bool) {
	if arr, ok := toStringArray(v); ok {
		out := make([]string, 0, len(arr))
		for _, s := range arr {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
		return out, true
	}
	if s, ok := v.(string); ok {
		return splitCSV(s), true
	}
	return nil, false
}

// notifyBackoffList reads a delay schedule: a JSON array of whole numbers, or a
// comma-separated string such as "1,4,16,64". Every entry must be a whole
// number of seconds, at least 1. An empty list is valid and means the default.
func notifyBackoffList(v interface{}) ([]int, error) {
	var raw []interface{}
	switch x := v.(type) {
	case []interface{}:
		raw = x
	case string:
		for _, p := range splitCSV(x) {
			raw = append(raw, p)
		}
	default:
		return nil, fmt.Errorf("must be a list of whole seconds, or a comma-separated string")
	}
	out := make([]int, 0, len(raw))
	for _, e := range raw {
		n, err := notifyInt(e)
		if err != nil {
			return nil, fmt.Errorf("each entry must be a whole number of seconds, got %v", e)
		}
		if n < 1 {
			return nil, fmt.Errorf("each entry must be at least 1 second, got %d", n)
		}
		out = append(out, n)
	}
	return out, nil
}

// validateLifecycleNotifyPatch checks every notify key in patch before anything
// is applied, so a bad value rejects the whole PUT rather than storing a partial
// write. It is run from handlePutConfig; the apply path trusts its output.
func validateLifecycleNotifyPatch(patch map[string]interface{}) error {
	for _, rule := range notifyIntRules {
		v, ok := patch[rule.key]
		if !ok {
			continue
		}
		n, err := notifyInt(v)
		if err != nil {
			return fmt.Errorf("%s: %v", rule.key, err)
		}
		if rule.max > 0 && n > rule.max {
			return fmt.Errorf("%s: must be at most %d", rule.key, rule.max)
		}
	}
	for _, key := range notifyBackoffKeys {
		v, ok := patch[key]
		if !ok {
			continue
		}
		if _, err := notifyBackoffList(v); err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
	}
	if v, ok := patch[notifyAllowHostKey]; ok {
		hosts, ok := notifyStringList(v)
		if !ok {
			return fmt.Errorf("%s: must be a list of hosts, or a comma-separated string", notifyAllowHostKey)
		}
		for _, h := range hosts {
			if err := autonomous.ValidateWebhookAllowHost(h); err != nil {
				return fmt.Errorf("%s: %v", notifyAllowHostKey, err)
			}
		}
	}
	return nil
}

// applyLifecycleNotifyKey writes one notify key into n. It runs after
// validateLifecycleNotifyPatch, so a value that does not parse here is left
// unchanged rather than stored.
func applyLifecycleNotifyKey(n *config.AutonomousNotifyConfig, key string, v interface{}) {
	switch key {
	case "autonomous.notify.webhook.enabled":
		n.Webhook.Enabled = toBool(v)
	case notifyAllowHostKey:
		if hosts, ok := notifyStringList(v); ok {
			n.Webhook.AllowHosts = hosts
		}
	case "autonomous.notify.webhook.backoff_seconds":
		if list, err := notifyBackoffList(v); err == nil {
			n.Webhook.BackoffSeconds = list
		}
	case "autonomous.notify.forwarding.enabled":
		n.Forwarding.Enabled = toBool(v)
	case "autonomous.notify.forwarding.backoff_seconds":
		if list, err := notifyBackoffList(v); err == nil {
			n.Forwarding.BackoffSeconds = list
		}
	default:
		num, err := notifyInt(v)
		if err != nil {
			return
		}
		switch key {
		case "autonomous.notify.webhook.max_attempts":
			n.Webhook.MaxAttempts = num
		case "autonomous.notify.webhook.jitter_percent":
			n.Webhook.JitterPercent = num
		case "autonomous.notify.webhook.attempt_timeout_seconds":
			n.Webhook.AttemptTimeoutSeconds = num
		case "autonomous.notify.webhook.max_body_bytes":
			n.Webhook.MaxBodyBytes = num
		case "autonomous.notify.webhook.signature_tolerance_seconds":
			n.Webhook.SignatureToleranceSeconds = num
		case "autonomous.notify.forwarding.max_hops":
			n.Forwarding.MaxHops = num
		case "autonomous.notify.forwarding.max_attempts":
			n.Forwarding.MaxAttempts = num
		case "autonomous.notify.forwarding.jitter_percent":
			n.Forwarding.JitterPercent = num
		case "autonomous.notify.forwarding.attempt_timeout_seconds":
			n.Forwarding.AttemptTimeoutSeconds = num
		}
	}
}

// webhookNotifySurface builds the autonomous.notify.webhook GET surface. Every
// key is always present. The numeric and list values are the effective ones, so
// a read after a PUT of 0 shows the default that is in force.
func webhookNotifySurface(w config.AutonomousNotifyWebhookConfig) map[string]interface{} {
	hosts := w.AllowHosts
	if hosts == nil {
		hosts = []string{}
	}
	return map[string]interface{}{
		"enabled":                     w.Enabled,
		"allow_hosts":                 hosts,
		"max_attempts":                w.EffectiveMaxAttempts(),
		"backoff_seconds":             w.EffectiveBackoffSeconds(),
		"jitter_percent":              w.EffectiveJitterPercent(),
		"attempt_timeout_seconds":     w.EffectiveAttemptTimeoutSeconds(),
		"max_body_bytes":              w.EffectiveMaxBodyBytes(),
		"signature_tolerance_seconds": w.EffectiveSignatureToleranceSeconds(),
	}
}

// forwardingNotifySurface builds the autonomous.notify.forwarding GET surface,
// with the same rules as webhookNotifySurface.
func forwardingNotifySurface(f config.AutonomousNotifyForwardingConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled":                 f.Enabled,
		"max_hops":                f.EffectiveMaxHops(),
		"max_attempts":            f.EffectiveMaxAttempts(),
		"backoff_seconds":         f.EffectiveBackoffSeconds(),
		"jitter_percent":          f.EffectiveJitterPercent(),
		"attempt_timeout_seconds": f.EffectiveAttemptTimeoutSeconds(),
	}
}
