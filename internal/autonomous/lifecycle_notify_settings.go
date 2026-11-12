package autonomous

// Live settings for the outbound webhook sink and peer forwarding, built from
// the autonomous.notify.webhook.* and autonomous.notify.forwarding.* config
// keys. The daemon wires the closures that call these on every delivery, so a
// PUT /api/config change applies to the next event without a restart.
//
// Design: docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md
// (sections 2 and 4). The defaults are in internal/config (config.DefaultNotify*).

import (
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// secondsToDurations converts a list of whole seconds to durations.
func secondsToDurations(secs []int) []time.Duration {
	out := make([]time.Duration, 0, len(secs))
	for _, s := range secs {
		out = append(out, time.Duration(s)*time.Second)
	}
	return out
}

// WebhookSettingsFromConfig returns the live webhook sink settings for n. Every
// value goes through the config Effective helpers, so a zero or unset field
// reads as the recommended default. The allow-list is copied, so a later
// config write cannot change a list a delivery is already using.
func WebhookSettingsFromConfig(n config.AutonomousNotifyConfig) WebhookSettings {
	w := n.Webhook
	return WebhookSettings{
		Enabled:            w.Enabled,
		AllowHosts:         append([]string(nil), w.AllowHosts...),
		MaxAttempts:        w.EffectiveMaxAttempts(),
		Backoff:            secondsToDurations(w.EffectiveBackoffSeconds()),
		JitterPercent:      w.EffectiveJitterPercent(),
		Timeout:            time.Duration(w.EffectiveAttemptTimeoutSeconds()) * time.Second,
		MaxBodyBytes:       w.EffectiveMaxBodyBytes(),
		SignatureTolerance: time.Duration(w.EffectiveSignatureToleranceSeconds()) * time.Second,
	}
}

// ForwardSettingsFromConfig returns the live peer forwarding settings for n.
// The envelope cap is the webhook sink's, because a forwarded envelope is built
// the same way and the receiving daemon refuses one over its own cap.
func ForwardSettingsFromConfig(n config.AutonomousNotifyConfig) ForwardSettings {
	f := n.Forwarding
	return ForwardSettings{
		Enabled:       f.Enabled,
		MaxHops:       f.EffectiveMaxHops(),
		MaxAttempts:   f.EffectiveMaxAttempts(),
		Backoff:       secondsToDurations(f.EffectiveBackoffSeconds()),
		JitterPercent: f.EffectiveJitterPercent(),
		Timeout:       time.Duration(f.EffectiveAttemptTimeoutSeconds()) * time.Second,
		MaxBodyBytes:  n.Webhook.EffectiveMaxBodyBytes(),
	}
}

// ValidateWebhookAllowHost checks one entry of autonomous.notify.webhook.allow_hosts.
// An entry is a bare host (matches that host on any port) or a host:port (matches
// only that port). A scheme, a path, a query, a wildcard or userinfo is refused
// with a message that names the problem; anything else that is not a valid host
// or host:port is refused as malformed. The matcher (WebhookHostAllowed) applies
// the same parse, so an accepted entry always matches what it names.
func ValidateWebhookAllowHost(entry string) error {
	e := strings.TrimSpace(entry)
	switch {
	case e == "":
		return fmt.Errorf("an allow-list entry is empty")
	case strings.Contains(e, "://"):
		return fmt.Errorf("allow-list entry %q has a scheme; enter a bare host or host:port", e)
	case strings.Contains(e, "@"):
		return fmt.Errorf("allow-list entry %q has userinfo; enter a bare host or host:port", e)
	case strings.ContainsAny(e, "/?#"):
		return fmt.Errorf("allow-list entry %q has a path or query; enter a bare host or host:port", e)
	case strings.Contains(e, "*"):
		return fmt.Errorf("allow-list entry %q has a wildcard; wildcards are not allowed, enter an exact host or host:port", e)
	}
	if _, _, ok := parseWebhookHostPort(e); !ok {
		return fmt.Errorf("allow-list entry %q is not a valid host or host:port", e)
	}
	return nil
}
